package agentapi

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/voxpupuli/enterprise-console/internal/agentrequests"
	"github.com/voxpupuli/enterprise-console/internal/nodescope"
	"github.com/voxpupuli/enterprise-console/internal/orchestrator"
	"github.com/voxpupuli/enterprise-console/internal/rbac"
	"github.com/voxpupuli/enterprise-console/internal/taskscope"
	"github.com/voxpupuli/enterprise-console/internal/testdb"
)

type revoked map[string]bool

func (r revoked) IsRevoked(jti string) bool { return r[jti] }

// fakeStarter accepts every check and counts what it is asked to start: an
// agent must never get one started.
type fakeStarter struct{ started atomic.Int32 }

func (f *fakeStarter) Targets() *orchestrator.TargetResolver {
	return orchestrator.NewTargetResolver(nil, nil)
}
func (f *fakeStarter) CheckRun(context.Context, orchestrator.RunRequest) error     { return nil }
func (f *fakeStarter) CheckPatch(context.Context, orchestrator.PatchRequest) error { return nil }
func (f *fakeStarter) CheckTask(context.Context, orchestrator.TaskRequest) (orchestrator.TaskCheck, error) {
	return orchestrator.TaskCheck{Environment: "production", StoredParams: json.RawMessage(`{}`)}, nil
}
func (f *fakeStarter) CheckPlan(_ context.Context, r orchestrator.PlanRequest) ([]orchestrator.TaskCheck, error) {
	return make([]orchestrator.TaskCheck, len(r.Steps)), nil
}
func (f *fakeStarter) StartRun(context.Context, orchestrator.RunRequest) (orchestrator.Job, error) {
	f.started.Add(1)
	return orchestrator.Job{}, nil
}
func (f *fakeStarter) StartPatch(context.Context, orchestrator.PatchRequest) (orchestrator.Job, error) {
	f.started.Add(1)
	return orchestrator.Job{}, nil
}
func (f *fakeStarter) StartTask(context.Context, orchestrator.TaskRequest) (orchestrator.Job, error) {
	f.started.Add(1)
	return orchestrator.Job{}, nil
}
func (f *fakeStarter) StartPlan(context.Context, orchestrator.PlanRequest) (orchestrator.Job, error) {
	f.started.Add(1)
	return orchestrator.Job{}, nil
}

type rig struct {
	t        *testing.T
	srv      *httptest.Server
	h        *Handler
	issuer   *rbac.Issuer
	verifier *rbac.Verifier
	revoked  revoked
	starter  *fakeStarter
	ids      map[string]int64 // jti -> agent_tokens id
}

func newRig(t *testing.T, ratePerMinute int) *rig {
	t.Helper()
	pool := testdb.Pool(t)
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	rv := revoked{}
	issuer := rbac.NewIssuer(key, "k1")
	verifier := rbac.NewVerifier(map[string]*ecdsa.PublicKey{"k1": &key.PublicKey}, "k1", rv)
	starter := &fakeStarter{}
	svc := agentrequests.NewService(agentrequests.NewStore(pool), starter, nil,
		func(context.Context, int64) (agentrequests.Authorization, error) {
			return agentrequests.Authorization{}, nil
		}, nil)

	r := &rig{t: t, issuer: issuer, verifier: verifier, revoked: rv, starter: starter, ids: map[string]int64{}}
	h := New(Config{
		Verifier: verifier, Requests: svc, RatePerMinute: ratePerMinute, Version: "test",
		Caller: func(_ *http.Request, claims *rbac.Claims) (agentrequests.Caller, error) {
			return agentrequests.Caller{TokenID: r.ids[claims.ID], TokenName: strings.TrimPrefix(claims.Subject, "agent:"),
				Scope: nodescope.Everything(), Tasks: taskscope.Unrestricted()}, nil
		},
	})
	// The console's own router, with one real endpoint behind real authorization.
	internal := http.NewServeMux()
	internal.HandleFunc("GET /api/v1/nodes", verifier.Authorize("nodes:read", func(w http.ResponseWriter, req *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"items":[{"certname":"web01"}],"name":%q}`, req.URL.Query().Get("name"))
	}))
	h.SetInternal(internal)
	mux := http.NewServeMux()
	h.Register(mux)
	mux.Handle("/api/v1/", internal)
	r.h = h
	r.srv = httptest.NewServer(mux)
	t.Cleanup(r.srv.Close)
	t.Cleanup(func() {
		for _, id := range r.ids {
			_, _ = pool.Exec(context.Background(), `DELETE FROM agent_tokens WHERE id = $1`, id)
		}
	})
	return r
}

func (r *rig) token(permissions ...string) string {
	r.t.Helper()
	name := fmt.Sprintf("bot-%d", time.Now().UnixNano())
	signed, tok, err := r.issuer.IssueAgentToken(rbac.AgentToken{Name: name, Permissions: permissions, ExpiresAt: time.Now().Add(time.Hour)}, time.Now())
	if err != nil {
		r.t.Fatal(err)
	}
	var id int64
	pool := testdb.Pool(r.t)
	if err := pool.QueryRow(context.Background(), `INSERT INTO agent_tokens (name, jti, owner, permissions, expires_at)
		VALUES ($1, $2, 'owner', $3, now() + interval '1 hour') RETURNING id`, name, tok.JTI, permissions).Scan(&id); err != nil {
		r.t.Fatal(err)
	}
	r.ids[tok.JTI] = id
	return signed
}

func (r *rig) rpc(token, body string, headers ...string) (int, map[string]any, http.Header) {
	r.t.Helper()
	req, _ := http.NewRequest("POST", r.srv.URL+"/mcp", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	for i := 0; i+1 < len(headers); i += 2 {
		req.Header.Set(headers[i], headers[i+1])
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		r.t.Fatal(err)
	}
	defer resp.Body.Close()
	var out map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&out)
	return resp.StatusCode, out, resp.Header
}

func (r *rig) call(token, tool, args string) (map[string]any, string) {
	r.t.Helper()
	code, out, _ := r.rpc(token, fmt.Sprintf(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":%q,"arguments":%s}}`, tool, args))
	if code != 200 {
		r.t.Fatalf("tools/call %s = %d %v", tool, code, out)
	}
	if out["error"] != nil {
		return out, fmt.Sprint(out["error"])
	}
	result := out["result"].(map[string]any)
	content := result["content"].([]any)[0].(map[string]any)["text"].(string)
	return result, content
}

func toolNames(out map[string]any) []string {
	var names []string
	for _, t := range out["result"].(map[string]any)["tools"].([]any) {
		names = append(names, t.(map[string]any)["name"].(string))
	}
	return names
}

func TestEndpoint_OnlyAgentTokensAreAccepted(t *testing.T) {
	r := newRig(t, 100)
	const ping = `{"jsonrpc":"2.0","id":1,"method":"ping"}`
	if code, _, hdr := r.rpc("", ping); code != 401 || hdr.Get("WWW-Authenticate") == "" {
		t.Errorf("no token = %d", code)
	}
	user, _, _ := r.issuer.IssueToken("alice", rbac.TokenTypeAccess, []string{"nodes:read"}, time.Hour)
	if code, _, _ := r.rpc(user, ping); code != 401 {
		t.Errorf("a user token = %d, want 401", code)
	}
	svc, _, _ := r.issuer.IssueToken("svc", rbac.TokenTypeService, []string{"nodes:read"}, time.Hour)
	if code, _, _ := r.rpc(svc, ping); code != 401 {
		t.Errorf("a service token = %d, want 401", code)
	}
	good := r.token("nodes:read")
	if code, _, _ := r.rpc(good, ping); code != 200 {
		t.Errorf("an agent token = %d", code)
	}
	// Revoked.
	claims, _ := r.verifier.VerifyAgent(good)
	r.revoked[claims.ID] = true
	if code, _, _ := r.rpc(good, ping); code != 401 {
		t.Errorf("a revoked token = %d, want 401", code)
	}
	// And an agent token is refused by a REST endpoint over real HTTP.
	other := r.token("nodes:read")
	req, _ := http.NewRequest("GET", r.srv.URL+"/api/v1/nodes", nil)
	req.Header.Set("Authorization", "Bearer "+other)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 401 {
		t.Errorf("REST endpoint with an agent token = %d, want 401", resp.StatusCode)
	}
}

func TestEndpoint_OriginAndMethod(t *testing.T) {
	r := newRig(t, 100)
	tok := r.token("nodes:read")
	if code, _, _ := r.rpc(tok, `{"jsonrpc":"2.0","id":1,"method":"ping"}`, "Origin", "https://evil.example"); code != 403 {
		t.Errorf("foreign origin = %d, want 403", code)
	}
	resp, _ := http.Get(r.srv.URL + "/mcp")
	resp.Body.Close()
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("GET /mcp = %d", resp.StatusCode)
	}
}

func TestProtocol_InitializeAndToolListFollowThePermissions(t *testing.T) {
	r := newRig(t, 100)
	readOnly := r.token("nodes:read")
	code, out, _ := r.rpc(readOnly, `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-03-26"}}`)
	res := out["result"].(map[string]any)
	if code != 200 || res["protocolVersion"] != "2025-03-26" || res["serverInfo"].(map[string]any)["name"] != "openvox-console" {
		t.Errorf("initialize = %d %v", code, out)
	}
	// A notification gets no body.
	if code, _, _ := r.rpc(readOnly, `{"jsonrpc":"2.0","method":"notifications/initialized"}`); code != 202 {
		t.Errorf("notification = %d, want 202", code)
	}

	_, out, _ = r.rpc(readOnly, `{"jsonrpc":"2.0","id":2,"method":"tools/list"}`)
	names := strings.Join(toolNames(out), ",")
	if !strings.Contains(names, "list_nodes") || strings.Contains(names, "request_") || strings.Contains(names, "list_jobs") {
		t.Errorf("a nodes:read token is offered %s", names)
	}
	runner := r.token("nodes:read", "orchestrator:read", "orchestrator:run")
	_, out, _ = r.rpc(runner, `{"jsonrpc":"2.0","id":2,"method":"tools/list"}`)
	names = strings.Join(toolNames(out), ",")
	for _, want := range []string{"request_task", "request_puppet_run", "request_plan", "request_patch_run", "request_code_plan", "list_jobs", "get_request"} {
		if !strings.Contains(names, want) {
			t.Errorf("a full token is not offered %s (%s)", want, names)
		}
	}
	// A tool the token may not use does not exist for it.
	_, msg := r.call(readOnly, "request_task", `{"task":"a::b","targets":["x"]}`)
	if !strings.Contains(msg, "unknown tool") {
		t.Errorf("calling a tool without the permission = %s", msg)
	}
	if code, out, _ := r.rpc(readOnly, `{"jsonrpc":"2.0","id":3,"method":"nope"}`); code != 200 || out["error"] == nil {
		t.Errorf("unknown method = %d %v", code, out)
	}
}

func TestReadTools_GoThroughTheConsolesOwnEndpoints(t *testing.T) {
	r := newRig(t, 100)
	tok := r.token("nodes:read")
	result, body := r.call(tok, "list_nodes", `{"name":"web"}`)
	if result["isError"] != false || !strings.Contains(body, `"certname":"web01"`) || !strings.Contains(body, `"name":"web"`) {
		t.Errorf("list_nodes = %v %s", result, body)
	}
	// An unknown or mistyped argument is an error the agent can read.
	if _, body := r.call(tok, "list_nodes", `{"bogus":1}`); !strings.Contains(body, "unknown argument") {
		t.Errorf("unknown argument = %s", body)
	}
	if result, body := r.call(tok, "list_nodes", `{"page":"two"}`); result["isError"] != true || !strings.Contains(body, "whole number") {
		t.Errorf("bad type = %v %s", result, body)
	}
	// A token without the endpoint's permission is refused by the endpoint
	// itself, even where a tool exists for it.
	jobs := r.token("orchestrator:read")
	if result, body := r.call(jobs, "get_node", `{"name":"web01"}`); result != nil && !strings.Contains(body, "unknown tool") {
		t.Errorf("get_node without nodes:read = %s", body)
	}
}

func TestChangeTools_StoreARequestAndStartNothing(t *testing.T) {
	r := newRig(t, 100)
	tok := r.token("nodes:read", "orchestrator:read", "orchestrator:run")

	kinds := map[string]string{
		"request_task":       `{"task":"package::install","targets":["web01"],"params":{"name":"git"}}`,
		"request_puppet_run": `{"targets":["web01"],"options":{"noop":true}}`,
		"request_patch_run":  `{"targets":["web01"],"options":{"scope":"security"}}`,
		"request_plan":       `{"name":"p","targets":["web01"],"steps":[{"kind":"run"}]}`,
	}
	var firstID float64
	for tool, args := range kinds {
		result, body := r.call(tok, tool, args)
		if result["isError"] != false {
			t.Fatalf("%s = %s", tool, body)
		}
		var got map[string]any
		_ = json.Unmarshal([]byte(body), &got)
		if got["status"] != "pending" || got["requestId"] == nil || !strings.Contains(fmt.Sprint(got["message"]), "approv") {
			t.Errorf("%s answered %s", tool, body)
		}
		firstID = got["requestId"].(float64)
	}
	if n := r.starter.started.Load(); n != 0 {
		t.Fatalf("change tools started %d jobs", n)
	}
	// Its own request can be read back; another token's cannot.
	_, body := r.call(tok, "get_request", fmt.Sprintf(`{"requestId":%d}`, int64(firstID)))
	if !strings.Contains(body, `"status": "pending"`) {
		t.Errorf("get_request = %s", body)
	}
	other := r.token("orchestrator:run")
	if _, body := r.call(other, "get_request", fmt.Sprintf(`{"requestId":%d}`, int64(firstID))); !strings.Contains(body, "request not found") {
		t.Errorf("another token read a request: %s", body)
	}
	if _, body := r.call(tok, "list_requests", `{}`); strings.Count(body, "requestId") != 4 {
		t.Errorf("list_requests = %s", body)
	}
	// Target choices are exclusive.
	if result, body := r.call(tok, "request_task", `{"task":"a::b","targets":["x"],"query":"nodes {}"}`); result["isError"] != true || !strings.Contains(body, "exactly one") {
		t.Errorf("two target kinds = %s", body)
	}
	// With nothing matched, nothing is stored.
	if result, _ := r.call(tok, "request_task", `{"task":"a::b","targets":[]}`); result["isError"] != true {
		t.Error("an empty target list was accepted")
	}
	// A plan from code is refused cleanly where code plans are unavailable.
	if result, body := r.call(tok, "request_code_plan", `{"plan":"ops::x"}`); result["isError"] != true || !strings.Contains(body, "not available") {
		t.Errorf("code plan without code = %s", body)
	}
}

func TestLimits_RateBodyAndResult(t *testing.T) {
	r := newRig(t, 3)
	a, b := r.token("nodes:read"), r.token("nodes:read")
	const ping = `{"jsonrpc":"2.0","id":1,"method":"ping"}`
	for i := 0; i < 3; i++ {
		if code, _, _ := r.rpc(a, ping); code != 200 {
			t.Fatalf("call %d = %d", i, code)
		}
	}
	code, out, hdr := r.rpc(a, ping)
	if code != 429 || hdr.Get("Retry-After") == "" || !strings.Contains(fmt.Sprint(out["error"]), "retry") {
		t.Errorf("over the rate = %d %v %v", code, out, hdr)
	}
	if code, _, _ := r.rpc(b, ping); code != 200 {
		t.Errorf("another token was limited: %d", code)
	}

	r2 := newRig(t, 100)
	tok := r2.token("nodes:read")
	big := fmt.Sprintf(`{"jsonrpc":"2.0","id":1,"method":"ping","params":{"pad":%q}}`, strings.Repeat("x", MaxBodyBytes))
	if code, _, _ := r2.rpc(tok, big); code != http.StatusRequestEntityTooLarge {
		t.Errorf("oversized body = %d", code)
	}
	if got := truncate(strings.Repeat("y", MaxResultBytes+10)); !strings.Contains(got, "was cut") || len(got) > MaxResultBytes+400 {
		t.Errorf("truncate = %d bytes", len(got))
	}
}
