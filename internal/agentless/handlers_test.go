package agentless

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/voxpupuli/enterprise-console/internal/auditlog"
	"github.com/voxpupuli/enterprise-console/internal/nodescope"
)

type fakeChecker struct {
	result CheckResult
	called []string
}

func (f *fakeChecker) Check(_ context.Context, name string) (CheckResult, error) {
	f.called = append(f.called, name)
	return f.result, nil
}

type apiRig struct {
	t       *testing.T
	h       *Handlers
	mux     *http.ServeMux
	store   *Store
	checker *fakeChecker
	mu      sync.Mutex
	events  []auditlog.Event
	scope   nodescope.Scope
	known   map[string]bool
	changed int
}

func newAPI(t *testing.T) *apiRig {
	store := newStore(t)
	r := &apiRig{t: t, store: store, checker: &fakeChecker{}, scope: nodescope.Everything(), known: map[string]bool{}}
	r.h = NewHandlers(store, r.checker, func(_ *http.Request, e auditlog.Event) { r.mu.Lock(); r.events = append(r.events, e); r.mu.Unlock() },
		func(*http.Request) string { return "alice" })
	r.h.SetScope(func(*http.Request) (nodescope.Scope, error) { return r.scope, nil })
	r.h.SetNodeKnown(func(_ context.Context, name string) (bool, error) { return r.known[name], nil })
	r.h.SetChanged(func() { r.changed++ })
	r.mux = http.NewServeMux()
	r.h.Register(r.mux, func(_ string, next http.HandlerFunc) http.HandlerFunc { return next })
	return r
}

func (r *apiRig) do(method, path string, body any) (int, string) {
	var rd *bytes.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	} else {
		rd = bytes.NewReader(nil)
	}
	rec := httptest.NewRecorder()
	r.mux.ServeHTTP(rec, httptest.NewRequest(method, path, rd))
	return rec.Code, rec.Body.String()
}

func (r *apiRig) create(name, password string) map[string]any {
	r.t.Helper()
	code, body := r.do("POST", "/api/v1/agentless/targets", map[string]any{"name": name, "address": "10.0.0.9", "username": "ops", "password": password})
	if code != http.StatusCreated {
		r.t.Fatalf("create %s = %d %s", name, code, body)
	}
	var out map[string]any
	_ = json.Unmarshal([]byte(body), &out)
	return out
}

func (r *apiRig) allText() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	b, _ := json.Marshal(r.events)
	return string(b)
}

func TestAPI_SecretsAreNeverReturnedOrAudited(t *testing.T) {
	r := newAPI(t)
	const pw, pass = "Sup3r-Secret-pw", "pass-phrase-9"
	key, _ := testKey(t, "")
	var all strings.Builder

	code, body := r.do("POST", "/api/v1/agentless/targets", map[string]any{"name": "t1", "address": "10.0.0.1", "username": "ops", "password": pw})
	all.WriteString(body)
	if code != http.StatusCreated || !strings.Contains(body, `"hasPassword":true`) {
		t.Fatalf("create = %d %s", code, body)
	}
	_, body = r.do("GET", "/api/v1/agentless/targets", nil)
	all.WriteString(body)
	var t1 map[string]any
	code, body = r.do("GET", "/api/v1/agentless/targets", nil)
	_ = json.Unmarshal([]byte(body), &map[string]any{})
	id := int(listFirst(t, body)["id"].(float64))
	_ = t1
	_, body = r.do("GET", "/api/v1/agentless/targets/"+strconv.Itoa(id), nil)
	all.WriteString(body)
	_, body = r.do("PUT", "/api/v1/agentless/targets/"+strconv.Itoa(id), map[string]any{"privateKey": key, "passphrase": nil})
	all.WriteString(body)
	_, body = r.do("POST", "/api/v1/agentless/credentials", map[string]any{"name": "shared", "username": "ops", "password": pass})
	all.WriteString(body)
	_, body = r.do("GET", "/api/v1/agentless/credentials", nil)
	all.WriteString(body)

	all.WriteString(r.allText())
	for _, secret := range []string{pw, pass, "PRIVATE KEY", key[len(key)/2 : len(key)/2+20]} {
		if strings.Contains(all.String(), secret) {
			t.Errorf("a secret (%.12s…) appears in a response or an audit entry", secret)
		}
	}
	if !strings.Contains(r.allText(), `"secretsSet":["password"]`) {
		t.Errorf("the audit entry does not say which secret was set: %s", r.allText())
	}
}

func listFirst(t *testing.T, body string) map[string]any {
	t.Helper()
	var out struct{ Items []map[string]any }
	if err := json.Unmarshal([]byte(body), &out); err != nil || len(out.Items) == 0 {
		t.Fatalf("list = %s (%v)", body, err)
	}
	return out.Items[0]
}

func TestAPI_Validation(t *testing.T) {
	r := newAPI(t)
	code, body := r.do("POST", "/api/v1/agentless/targets", map[string]any{"name": "t", "address": "10.0.0.1", "username": "ops", "password": "x", "port": 99999})
	if code != http.StatusBadRequest || !strings.Contains(body, `"field":"port"`) {
		t.Errorf("bad port = %d %s", code, body)
	}
	code, body = r.do("POST", "/api/v1/agentless/targets", map[string]any{"name": "t", "address": "h", "username": "ops", "privateKey": "-----BEGIN junk"})
	if code != http.StatusBadRequest || !strings.Contains(body, `"field":"privateKey"`) || strings.Contains(body, "junk") {
		t.Errorf("bad key = %d %s", code, body)
	}
	r.create("dup", "x")
	if code, body = r.do("POST", "/api/v1/agentless/targets", map[string]any{"name": "dup", "address": "h", "username": "u", "password": "x"}); code != http.StatusConflict || !strings.Contains(body, "name") {
		t.Errorf("duplicate = %d %s", code, body)
	}
}

func TestAPI_ANameThatIsAlreadyANodeIsRefused(t *testing.T) {
	r := newAPI(t)
	r.known["web01.example.com"] = true
	code, body := r.do("POST", "/api/v1/agentless/targets", map[string]any{"name": "web01.example.com", "address": "h", "username": "u", "password": "x"})
	if code != http.StatusConflict || !strings.Contains(body, "node with that name already exists") {
		t.Errorf("= %d %s", code, body)
	}
	if r.changed != 0 {
		t.Error("the router was told of a target that was not created")
	}
}

func TestAPI_NodeScope(t *testing.T) {
	r := newAPI(t)
	mine := r.create("mine", "x")
	theirs := r.create("theirs", "x")
	r.scope = nodescope.Of("mine")

	_, body := r.do("GET", "/api/v1/agentless/targets", nil)
	if !strings.Contains(body, `"mine"`) || strings.Contains(body, "theirs") {
		t.Errorf("list = %s", body)
	}
	// A target outside the scope reads the same as one that does not exist.
	idTheirs := strconv.Itoa(int(theirs["id"].(float64)))
	c1, b1 := r.do("GET", "/api/v1/agentless/targets/"+idTheirs, nil)
	c2, b2 := r.do("GET", "/api/v1/agentless/targets/999999", nil)
	if c1 != http.StatusNotFound || c1 != c2 || b1 != b2 {
		t.Errorf("hidden = %d %s, missing = %d %s", c1, b1, c2, b2)
	}
	for _, c := range []struct{ method, path string }{
		{"PUT", "/api/v1/agentless/targets/" + idTheirs}, {"DELETE", "/api/v1/agentless/targets/" + idTheirs},
		{"POST", "/api/v1/agentless/targets/" + idTheirs + "/check"}, {"POST", "/api/v1/agentless/targets/" + idTheirs + "/host-key/accept"},
		{"POST", "/api/v1/agentless/targets/" + idTheirs + "/host-key/replace"},
	} {
		if code, _ := r.do(c.method, c.path, map[string]any{"fingerprint": "x"}); code != http.StatusNotFound {
			t.Errorf("%s %s = %d, want 404", c.method, c.path, code)
		}
	}
	if len(r.checker.called) != 0 {
		t.Error("a hidden target was checked")
	}
	// Creating one needs the name to be inside the scope.
	code, body := r.do("POST", "/api/v1/agentless/targets", map[string]any{"name": "outside", "address": "h", "username": "u", "password": "x"})
	if code != http.StatusForbidden || !strings.Contains(body, "outside") {
		t.Errorf("create outside scope = %d %s", code, body)
	}
	if code, _ := r.do("POST", "/api/v1/agentless/targets", map[string]any{"name": "mine2", "address": "h", "username": "u", "password": "x"}); code != http.StatusForbidden {
		t.Errorf("a name not pinned into the scope = %d", code)
	}
	// Shared credentials are for callers who may manage every node.
	for _, c := range []struct{ method, path string }{
		{"GET", "/api/v1/agentless/credentials"}, {"POST", "/api/v1/agentless/credentials"},
	} {
		if code, _ := r.do(c.method, c.path, map[string]any{"name": "c", "username": "u", "password": "x"}); code != http.StatusForbidden {
			t.Errorf("%s %s by a scoped caller = %d, want 403", c.method, c.path, code)
		}
	}
	idMine := strconv.Itoa(int(mine["id"].(float64)))
	if code, _ := r.do("PUT", "/api/v1/agentless/targets/"+idMine, map[string]any{"credentialId": 1}); code != http.StatusForbidden {
		t.Errorf("a scoped caller using a shared credential = %d, want 403", code)
	}
	if code, _ := r.do("PUT", "/api/v1/agentless/targets/"+idMine, map[string]any{"address": "10.9.9.9"}); code != http.StatusOK {
		t.Errorf("a scoped caller changing their own target = %d", code)
	}
}

func TestAPI_CheckRecordsTheResultAndHostKeyFlow(t *testing.T) {
	r := newAPI(t)
	tg := r.create("kt", "x")
	id := strconv.Itoa(int(tg["id"].(float64)))
	tid := int64(tg["id"].(float64))

	k := wireKey(t)
	fp, _ := r.store.ProposeKey(context.Background(), tid, k)
	r.checker.result = CheckResult{Stage: StageHostKey, Reason: "host key: the host key has not been trusted yet", Presented: fp, NewKey: true}
	code, body := r.do("POST", "/api/v1/agentless/targets/"+id+"/check", nil)
	var res checkResponse
	_ = json.Unmarshal([]byte(body), &res)
	if code != http.StatusOK || res.OK || res.Stage != StageHostKey || res.Target.HostKey.Proposed != fp || res.Target.Check.OK == nil || *res.Target.Check.OK {
		t.Fatalf("check = %d %s", code, body)
	}

	// Accepting needs the fingerprint that was shown.
	if code, _ := r.do("POST", "/api/v1/agentless/targets/"+id+"/host-key/accept", map[string]any{"fingerprint": "SHA256:wrong"}); code != http.StatusConflict {
		t.Errorf("accept a fingerprint not shown = %d", code)
	}
	if code, _ := r.do("POST", "/api/v1/agentless/targets/"+id+"/host-key/accept", map[string]any{}); code != http.StatusBadRequest {
		t.Errorf("accept with no fingerprint = %d", code)
	}
	code, body = r.do("POST", "/api/v1/agentless/targets/"+id+"/host-key/accept", map[string]any{"fingerprint": fp})
	if code != http.StatusOK || !strings.Contains(body, fp) {
		t.Fatalf("accept = %d %s", code, body)
	}

	r.checker.result = CheckResult{OK: true, LoggedInAs: "ops"}
	code, body = r.do("POST", "/api/v1/agentless/targets/"+id+"/check", nil)
	_ = json.Unmarshal([]byte(body), &res)
	if code != http.StatusOK || !res.OK || res.LoggedInAs != "ops" || res.Target.Check.OK == nil || !*res.Target.Check.OK {
		t.Errorf("check = %d %s", code, body)
	}

	// A changed key is replaced only explicitly, naming both fingerprints in the audit.
	k2 := wireKey(t)
	fp2, _ := r.store.FlagChangedKey(context.Background(), tid, k2)
	if code, _ := r.do("POST", "/api/v1/agentless/targets/"+id+"/host-key/accept", map[string]any{"fingerprint": fp2}); code != http.StatusBadRequest {
		t.Errorf("accept over a pin = %d, want a refusal pointing at replace", code)
	}
	if code, _ := r.do("POST", "/api/v1/agentless/targets/"+id+"/host-key/replace", map[string]any{"fingerprint": fp2}); code != http.StatusOK {
		t.Fatalf("replace = %d", code)
	}
	text := r.allText()
	if !strings.Contains(text, "agentless.hostKey.replaced") || !strings.Contains(text, fp) || !strings.Contains(text, fp2) {
		t.Errorf("the replacement audit does not carry both fingerprints: %s", text)
	}
}

func TestAPI_SharedCredentialsAndDeletion(t *testing.T) {
	r := newAPI(t)
	code, body := r.do("POST", "/api/v1/agentless/credentials", map[string]any{"name": "ops-shared", "username": "ops", "password": "pw-shared-1"})
	if code != http.StatusCreated {
		t.Fatalf("create credential = %d %s", code, body)
	}
	var c Credential
	_ = json.Unmarshal([]byte(body), &c)
	code, body = r.do("POST", "/api/v1/agentless/targets", map[string]any{"name": "uses", "address": "h", "credentialId": c.ID})
	if code != http.StatusCreated || !strings.Contains(body, `"credentialName":"ops-shared"`) {
		t.Fatalf("create target with credential = %d %s", code, body)
	}
	var tg Target
	_ = json.Unmarshal([]byte(body), &tg)
	code, body = r.do("DELETE", "/api/v1/agentless/credentials/"+strconv.FormatInt(c.ID, 10), nil)
	if code != http.StatusConflict || !strings.Contains(body, "uses") {
		t.Errorf("delete in use = %d %s", code, body)
	}
	if code, _ := r.do("DELETE", "/api/v1/agentless/targets/"+strconv.FormatInt(tg.ID, 10), nil); code != http.StatusNoContent {
		t.Errorf("delete target = %d", code)
	}
	if r.changed < 2 {
		t.Errorf("the router was told %d times, want after each create and delete", r.changed)
	}
	if code, _ := r.do("DELETE", "/api/v1/agentless/credentials/"+strconv.FormatInt(c.ID, 10), nil); code != http.StatusNoContent {
		t.Errorf("delete credential = %d", code)
	}
}
