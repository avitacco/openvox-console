package taskcatalog

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/voxpupuli/enterprise-console/internal/rbac"
)

type noRevocations struct{}

func (noRevocations) IsRevoked(string) bool { return false }

// apiServer registers Handlers behind the real RBAC middleware and returns
// a function issuing tokens carrying the given permissions.
func apiServer(t *testing.T, client *Client) (*httptest.Server, func(perms ...string) string) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	issuer := rbac.NewIssuer(key, "test")
	verifier := rbac.NewVerifier(map[string]*ecdsa.PublicKey{"test": &key.PublicKey}, "test", noRevocations{})
	mux := http.NewServeMux()
	NewHandlers(client).Register(mux, verifier.Authorize)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	token := func(perms ...string) string {
		tok, _, err := issuer.IssueToken("tester", "access", perms, time.Minute)
		if err != nil {
			t.Fatal(err)
		}
		return tok
	}
	return srv, token
}

func get(t *testing.T, srv *httptest.Server, path, token string, out any) int {
	t.Helper()
	req, _ := http.NewRequest(http.MethodGet, srv.URL+path, nil)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if out != nil {
		_ = json.NewDecoder(resp.Body).Decode(out)
	}
	return resp.StatusCode
}

func TestAPIListEnvironments(t *testing.T) {
	srv, token := apiServer(t, newTLSClient(t, fakePuppetServer(t)))
	var body struct{ Environments []string }
	if code := get(t, srv, "/api/v1/tasks/environments", token("orchestrator:read"), &body); code != http.StatusOK {
		t.Fatalf("status = %d", code)
	}
	if len(body.Environments) != 1 || body.Environments[0] != "production" {
		t.Errorf("environments = %v", body.Environments)
	}
}

func TestAPIListTasksDefaultsToProduction(t *testing.T) {
	srv, token := apiServer(t, newTLSClient(t, fakePuppetServer(t)))
	var body struct {
		Environment string
		Tasks       []TaskSummary
	}
	if code := get(t, srv, "/api/v1/tasks", token("orchestrator:read"), &body); code != http.StatusOK {
		t.Fatalf("status = %d", code)
	}
	if body.Environment != "production" || len(body.Tasks) != 8 {
		t.Errorf("body = %+v", body)
	}
}

func TestAPIListTasksUnknownEnvironment(t *testing.T) {
	srv, token := apiServer(t, newTLSClient(t, fakePuppetServer(t)))
	if code := get(t, srv, "/api/v1/tasks?environment=nope", token("orchestrator:read"), nil); code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", code)
	}
}

func TestAPIGetTask(t *testing.T) {
	srv, token := apiServer(t, newTLSClient(t, fakePuppetServer(t)))
	var d TaskDetail
	if code := get(t, srv, "/api/v1/tasks/task_fixture/echo?environment=production", token("orchestrator:read"), &d); code != http.StatusOK {
		t.Fatalf("status = %d", code)
	}
	if d.Name != "task_fixture::echo" || d.Description == "" || len(d.Parameters) != 4 || d.AcceptsAnyParameters {
		t.Fatalf("detail = %+v", d)
	}
	byName := map[string]ParamDetail{}
	for _, p := range d.Parameters {
		byName[p.Name] = p
	}
	if c := byName["count"]; string(c.Default) != "1" || c.Input.Kind != "integer" || c.Input.Required {
		t.Errorf("count = %+v", c)
	}
	if m := byName["message"]; !m.Input.Required || m.Type != "String[1]" {
		t.Errorf("message = %+v", m)
	}
	if m := byName["mode"]; m.Input.Kind != "enum" || len(m.Input.Options) != 2 {
		t.Errorf("mode = %+v", m)
	}
}

func TestAPIGetTaskSensitiveAndInit(t *testing.T) {
	srv, token := apiServer(t, newTLSClient(t, fakePuppetServer(t)))
	var d TaskDetail
	get(t, srv, "/api/v1/tasks/task_fixture/secret", token("orchestrator:read"), &d)
	if len(d.Parameters) != 1 || !d.Parameters[0].Sensitive {
		t.Errorf("secret = %+v", d)
	}
	var init TaskDetail
	if code := get(t, srv, "/api/v1/tasks/task_fixture/init", token("orchestrator:read"), &init); code != http.StatusOK || init.Name != "task_fixture" {
		t.Errorf("init: status %d, detail %+v", code, init)
	}
	var plain TaskDetail
	get(t, srv, "/api/v1/tasks/task_fixture/plain", token("orchestrator:read"), &plain)
	if !plain.AcceptsAnyParameters {
		t.Errorf("plain = %+v, want acceptsAnyParameters", plain)
	}
}

func TestAPIGetTaskNotFound(t *testing.T) {
	srv, token := apiServer(t, newTLSClient(t, fakePuppetServer(t)))
	if code := get(t, srv, "/api/v1/tasks/task_fixture/nope", token("orchestrator:read"), nil); code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", code)
	}
}

func TestAPINotConfigured(t *testing.T) {
	srv, token := apiServer(t, nil)
	for _, path := range []string{"/api/v1/tasks/environments", "/api/v1/tasks", "/api/v1/tasks/a/b"} {
		var body struct{ Error string }
		if code := get(t, srv, path, token("orchestrator:read"), &body); code != http.StatusServiceUnavailable || body.Error == "" {
			t.Errorf("GET %s = %d %q, want 503 with an error", path, code, body.Error)
		}
	}
}

func TestAPIUnreachable(t *testing.T) {
	srv, token := apiServer(t, newClient("https://127.0.0.1:1", http.DefaultClient))
	var body struct{ Error string }
	if code := get(t, srv, "/api/v1/tasks", token("orchestrator:read"), &body); code != http.StatusBadGateway {
		t.Errorf("status = %d, want 502", code)
	}
	if body.Error == "" || !strings.Contains(body.Error, "openvoxserver") {
		t.Errorf("error = %q, want it to name openvoxserver", body.Error)
	}
}

func TestAPIRequiresAuthentication(t *testing.T) {
	srv, token := apiServer(t, newTLSClient(t, fakePuppetServer(t)))
	for _, path := range []string{"/api/v1/tasks/environments", "/api/v1/tasks", "/api/v1/tasks/task_fixture/echo"} {
		if code := get(t, srv, path, "", nil); code != http.StatusUnauthorized {
			t.Errorf("GET %s without a token = %d, want 401", path, code)
		}
		if code := get(t, srv, path, token("nodes:read"), nil); code != http.StatusForbidden {
			t.Errorf("GET %s without orchestrator:read = %d, want 403", path, code)
		}
	}
}
