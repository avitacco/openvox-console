package rbac

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"
)

func createAgentTokenVia(t *testing.T, h *Handlers, body string) (*httptest.ResponseRecorder, agentTokenResponse) {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/agent-tokens", strings.NewReader(body)).WithContext(requestAs("admin").Context())
	rec := httptest.NewRecorder()
	h.createAgentToken(rec, req)
	var out agentTokenResponse
	if rec.Code == http.StatusCreated {
		if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
			t.Fatal(err)
		}
	}
	return rec, out
}

func TestCheckAgentToken(t *testing.T) {
	now := time.Now()
	max := 90 * 24 * time.Hour
	good := AgentToken{Name: "bot", Permissions: []string{"nodes:read"}, ExpiresAt: now.Add(24 * time.Hour)}
	if err := CheckAgentToken(good, now, max); err != nil {
		t.Fatalf("a valid token was refused: %v", err)
	}
	for name, mutate := range map[string]func(*AgentToken){
		"no name":            func(t *AgentToken) { t.Name = " " },
		"no permissions":     func(t *AgentToken) { t.Permissions = nil },
		"approve permission": func(t *AgentToken) { t.Permissions = []string{PermAgentRequestsApprove} },
		"admin permission":   func(t *AgentToken) { t.Permissions = []string{"rbac:admin"} },
		"no expiry":          func(t *AgentToken) { t.ExpiresAt = time.Time{} },
		"expires too soon":   func(t *AgentToken) { t.ExpiresAt = now.Add(time.Minute) },
		"expires too late":   func(t *AgentToken) { t.ExpiresAt = now.Add(91 * 24 * time.Hour) },
		"bad task pattern":   func(t *AgentToken) { t.TaskPatterns = []string{"not a pattern!"} },
	} {
		tok := good
		mutate(&tok)
		if err := CheckAgentToken(tok, now, max); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

func TestAgentTokens_IssueListAndRevoke(t *testing.T) {
	h, store, captured := newTestHandlers(t)
	name := "agent-" + strconv.FormatInt(time.Now().UnixNano(), 10)
	rec, created := createAgentTokenVia(t, h, `{"name":"`+name+`","permissions":["nodes:read","orchestrator:run"],"nodeGroups":[],"taskPatterns":["pkg::*"],"expiresInDays":30}`)
	if rec.Code != http.StatusCreated || created.Token == "" {
		t.Fatalf("create = %d %s", rec.Code, rec.Body.String())
	}
	t.Cleanup(func() { _ = store.DeleteAgentToken(context.Background(), created.ID) })
	if created.Owner != "admin" || created.ExpiresAt.Before(time.Now().Add(29*24*time.Hour)) {
		t.Errorf("token = %+v", created.AgentToken)
	}

	// The token carries exactly what was asked: agent type, scope, tasks, expiry.
	claims, err := h.verifier.VerifyAgent(created.Token)
	if err != nil {
		t.Fatal(err)
	}
	groups, fleet := claims.NodeGroupsFor("nodes:read")
	if fleet || len(groups) != 0 {
		t.Errorf("a token with no node groups should cover no nodes: groups=%v fleet=%v", groups, fleet)
	}
	if len(claims.TaskGrants) != 1 || claims.TaskGrants[0].Tasks[0] != "pkg::*" {
		t.Errorf("task grants = %+v", claims.TaskGrants)
	}

	// Listing shows the token but never the signed value.
	lrec := httptest.NewRecorder()
	h.listAgentTokens(lrec, requestAs("admin"))
	if strings.Contains(lrec.Body.String(), created.Token) || !strings.Contains(lrec.Body.String(), name) {
		t.Errorf("listing = %s", lrec.Body.String())
	}

	// A request it has waiting, and one already decided.
	var waiting, decided int64
	for status, into := range map[string]*int64{"pending": &waiting, "approved": &decided} {
		if err := store.pool.QueryRow(context.Background(), `INSERT INTO agent_requests (token_id, token_name, kind, summary, body, status, expires_at)
			VALUES ($1, $2, 'run', 'x', '{}', $3, now() + interval '1 hour') RETURNING id`, created.ID, name, status).Scan(into); err != nil {
			t.Fatal(err)
		}
	}

	// Revoking refuses it from then on and removes the row.
	req := httptest.NewRequest(http.MethodDelete, "/", nil).WithContext(requestAs("admin").Context())
	req.SetPathValue("id", strconv.FormatInt(created.ID, 10))
	drec := httptest.NewRecorder()
	h.revokeAgentToken(drec, req)
	if drec.Code != http.StatusNoContent {
		t.Fatalf("revoke = %d %s", drec.Code, drec.Body.String())
	}
	if _, err := h.verifier.VerifyAgent(created.Token); err == nil {
		t.Error("a revoked agent token still verified")
	}
	if _, err := store.GetAgentToken(context.Background(), created.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("row after revoke: %v", err)
	}
	// Its requests are kept as records, and the one still waiting can no
	// longer be approved: it is failed, with the reason.
	for id, want := range map[int64]string{waiting: "failed", decided: "approved"} {
		var status, failure string
		var token *int64
		if err := store.pool.QueryRow(context.Background(), `SELECT status, failure, token_id FROM agent_requests WHERE id = $1`, id).Scan(&status, &failure, &token); err != nil {
			t.Fatal(err)
		}
		if status != want || token != nil || (want == "failed" && !strings.Contains(failure, "revoked")) {
			t.Errorf("request %d after revoke: status %s failure %q token %v, want %s", id, status, failure, token, want)
		}
		_, _ = store.pool.Exec(context.Background(), `DELETE FROM agent_requests WHERE id = $1`, id)
	}
	var actions []string
	for _, e := range captured.write {
		actions = append(actions, e.Action)
	}
	if strings.Join(actions, ",") != "agent_token.created,agent_token.revoked" {
		t.Errorf("audit = %v", actions)
	}
}

func TestAgentTokens_CreationIsRefusedWhenUnsafe(t *testing.T) {
	h, _, _ := newTestHandlers(t)
	for name, body := range map[string]string{
		"no expiry":     `{"name":"x","permissions":["nodes:read"]}`,
		"too long":      `{"name":"x","permissions":["nodes:read"],"expiresInDays":9999}`,
		"approve right": `{"name":"x","permissions":["agent-requests:approve"],"expiresInDays":5}`,
	} {
		if rec, _ := createAgentTokenVia(t, h, body); rec.Code != http.StatusBadRequest {
			t.Errorf("%s: status %d %s, want 400", name, rec.Code, rec.Body.String())
		}
	}
}

func TestAgentTokens_OnlyTheAgentEndpointAcceptsThem(t *testing.T) {
	h, store, _ := newTestHandlers(t)
	_, created := createAgentTokenVia(t, h, `{"name":"guard-`+strconv.FormatInt(time.Now().UnixNano(), 10)+`","permissions":["nodes:read"],"expiresInDays":1}`)
	t.Cleanup(func() { _ = store.DeleteAgentToken(context.Background(), created.ID) })

	reached := false
	endpoint := h.verifier.Authorize("nodes:read", func(w http.ResponseWriter, r *http.Request) { reached = true })
	call := func(ctx context.Context) int {
		reached = false
		req := httptest.NewRequest(http.MethodGet, "/", nil).WithContext(ctx)
		req.Header.Set("Authorization", "Bearer "+created.Token)
		rec := httptest.NewRecorder()
		endpoint(rec, req)
		return rec.Code
	}
	if code := call(context.Background()); code != http.StatusUnauthorized || reached {
		t.Errorf("a REST endpoint accepted an agent token: %d reached=%v", code, reached)
	}
	if code := call(AllowAgentTokens(context.Background())); code != http.StatusOK || !reached {
		t.Errorf("the agent endpoint's own request was refused: %d", code)
	}

	// And the agent endpoint's verifier refuses every other kind of token.
	user, _, err := h.issuer.IssueToken("alice", TokenTypeAccess, []string{"nodes:read"}, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := h.verifier.VerifyAgent(user); err == nil {
		t.Error("VerifyAgent accepted a user token")
	}
	svc, _, _ := h.issuer.IssueToken("svc", TokenTypeService, []string{"nodes:read"}, time.Hour)
	if _, err := h.verifier.VerifyAgent(svc); err == nil {
		t.Error("VerifyAgent accepted a service token")
	}
}
