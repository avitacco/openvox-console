package agentrequests

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/voxpupuli/enterprise-console/internal/nodescope"
)

func newMux(e *env, scope nodescope.Scope) *http.ServeMux {
	h := NewHandlers(e.svc, func(*http.Request) (nodescope.Scope, error) { return scope, nil },
		func(*http.Request) (Approver, error) { return e.approver, nil }, nil)
	mux := http.NewServeMux()
	h.Register(mux, func(_ string, next http.HandlerFunc) http.HandlerFunc { return next })
	return mux
}

func call(mux *http.ServeMux, method, path, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec
}

func TestHandlers_ListGetApproveAndNeverShowASecret(t *testing.T) {
	e := newEnv(t, true)
	ctx := context.Background()
	e.starter.sensitive = map[string]any{"password": "hunter2"}
	req, err := e.svc.Submit(ctx, e.caller, e.taskInput())
	if err != nil {
		t.Fatal(err)
	}
	mux := newMux(e, nodescope.Everything())
	id := strconv.FormatInt(req.ID, 10)

	for _, path := range []string{"/api/v1/agent-requests", "/api/v1/agent-requests/" + id, "/api/v1/agent-requests/pending-count"} {
		rec := call(mux, "GET", path, "")
		if rec.Code != http.StatusOK || strings.Contains(rec.Body.String(), "hunter2") {
			t.Errorf("GET %s = %d %s", path, rec.Code, rec.Body.String())
		}
	}
	rec := call(mux, "POST", "/api/v1/agent-requests/"+id+"/approve", `{"note":"go"}`)
	if rec.Code != http.StatusOK || strings.Contains(rec.Body.String(), "hunter2") {
		t.Fatalf("approve = %d %s", rec.Code, rec.Body.String())
	}
	var got Request
	_ = json.Unmarshal(rec.Body.Bytes(), &got)
	if got.Status != StatusApproved || got.JobID == nil {
		t.Errorf("approved = %+v", got)
	}
	if rec := call(mux, "POST", "/api/v1/agent-requests/"+id+"/approve", ""); rec.Code != http.StatusConflict {
		t.Errorf("approving twice = %d", rec.Code)
	}
}

func TestHandlers_AReaderOutsideTheNodesDoesNotSeeTheRequest(t *testing.T) {
	e := newEnv(t, true)
	ctx := context.Background()
	req, _ := e.svc.Submit(ctx, e.caller, e.taskInput())
	id := strconv.FormatInt(req.ID, 10)
	mux := newMux(e, nodescope.Of("some-other-node"))

	if rec := call(mux, "GET", "/api/v1/agent-requests/"+id, ""); rec.Code != http.StatusNotFound {
		t.Errorf("get = %d, want 404", rec.Code)
	}
	if rec := call(mux, "GET", "/api/v1/agent-requests", ""); strings.Contains(rec.Body.String(), req.Targets[0]) {
		t.Errorf("list shows a node outside the reader's scope: %s", rec.Body.String())
	}
	if rec := call(mux, "POST", "/api/v1/agent-requests/"+id+"/approve", ""); rec.Code != http.StatusNotFound || len(e.starter.started) != 0 {
		t.Errorf("approve = %d, started %v", rec.Code, e.starter.started)
	}
	// A plan that chooses its own nodes is shown only to a fleet-wide reader.
	cp, _ := e.svc.Submit(ctx, e.caller, Input{Kind: "codeplan", Plan: "a::b"})
	if rec := call(mux, "GET", "/api/v1/agent-requests/"+strconv.FormatInt(cp.ID, 10), ""); rec.Code != http.StatusNotFound {
		t.Errorf("scoped reader saw a code plan request: %d", rec.Code)
	}
}

func TestHandlers_RefusalsAreStatusCodes(t *testing.T) {
	e := newEnv(t, true)
	ctx := context.Background()
	req, _ := e.svc.Submit(ctx, e.caller, e.taskInput())
	id := strconv.FormatInt(req.ID, 10)
	e.approver.Name = "owner"
	mux := newMux(e, nodescope.Everything())
	if rec := call(mux, "POST", "/api/v1/agent-requests/"+id+"/approve", ""); rec.Code != http.StatusForbidden {
		t.Errorf("owner approve = %d, want 403: %s", rec.Code, rec.Body.String())
	}
	if rec := call(mux, "POST", "/api/v1/agent-requests/"+id+"/reject", `{"note":"`+strings.Repeat("x", 2000)+`"}`); rec.Code != http.StatusBadRequest {
		t.Errorf("long note = %d, want 400", rec.Code)
	}
	if rec := call(mux, "GET", "/api/v1/agent-requests?status=bogus", ""); rec.Code != http.StatusBadRequest {
		t.Errorf("bad status = %d", rec.Code)
	}
	if rec := call(mux, "GET", "/api/v1/agent-requests/999999999", ""); rec.Code != http.StatusNotFound {
		t.Errorf("missing = %d", rec.Code)
	}
}
