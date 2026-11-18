package scheduler

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/voxpupuli/enterprise-console/internal/nodescope"
	"github.com/voxpupuli/enterprise-console/internal/orchestrator"
	"github.com/voxpupuli/enterprise-console/internal/taskscope"
)

// api serves the schedule routes for the harness, as whichever caller
// is set, recording the permission each route was gated by.
type api struct {
	h      *harness
	mux    *http.ServeMux
	caller Caller
	gates  map[string]string
}

func newAPI(t *testing.T) *api {
	t.Helper()
	a := &api{h: newHarness(t), mux: http.NewServeMux(), gates: map[string]string{}}
	a.caller = a.h.caller()
	handlers := NewHandlers(a.h.svc, func(*http.Request) Owner { return a.caller.Owner }, nil)
	handlers.SetScope(func(*http.Request) (nodescope.Scope, error) { return a.caller.Scope, nil })
	handlers.Register(a.mux, func(permission string, next http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			a.gates[r.Pattern] = permission
			next(w, r)
		}
	})
	return a
}

func (a *api) do(method, path string, body any) (int, map[string]any, string) {
	var payload bytes.Buffer
	if body != nil {
		_ = json.NewEncoder(&payload).Encode(body)
	}
	rec := httptest.NewRecorder()
	a.mux.ServeHTTP(rec, httptest.NewRequest(method, path, &payload))
	var decoded map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &decoded)
	return rec.Code, decoded, rec.Body.String()
}

func (a *api) runBody(nodes ...string) map[string]any {
	return map[string]any{"name": "nightly", "kind": "run", "targets": nodes, "startAt": a.h.in(time.Hour).Format(time.RFC3339)}
}

func TestAPI_CreateReadAndShape(t *testing.T) {
	a := newAPI(t)
	a.h.groups[4] = []string{"web1"}

	status, created, raw := a.do(http.MethodPost, "/api/v1/orchestrator/schedules", map[string]any{
		"name": "weekly no-op", "kind": "run", "options": map[string]any{"noop": true}, "group": 4,
		"startAt": a.h.in(time.Hour).Format(time.RFC3339), "repeat": map[string]any{"every": 1, "unit": "week"},
	})
	if status != http.StatusCreated {
		t.Fatalf("create = %d %s", status, raw)
	}
	for _, key := range []string{"id", "name", "kind", "options", "group", "startAt", "repeat", "nextRunAt", "enabled", "state", "owner", "ownerKind"} {
		if _, ok := created[key]; !ok {
			t.Errorf("created schedule has no %q: %s", key, raw)
		}
	}
	if created["state"] != StateActive || created["owner"] != a.h.owner.Name {
		t.Errorf("created = %s", raw)
	}
	id := int64(created["id"].(float64))
	path := fmt.Sprintf("/api/v1/orchestrator/schedules/%d", id)

	// Fired once, skipped once, and a claim left with no outcome.
	a.h.fireAt(id, a.h.in(time.Hour))
	delete(a.h.groups, 4)
	a.h.fireAt(id, a.h.in(time.Hour+7*24*time.Hour))
	next := a.h.in(time.Hour + 21*24*time.Hour)
	firingID, _, err := a.h.store.Claim(context.Background(), id, a.h.in(time.Hour+14*24*time.Hour), &next)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.h.store.pool.Exec(context.Background(), `UPDATE scheduled_job_firings SET fired_at = now() - interval '11 minutes' WHERE id = $1`, firingID); err != nil {
		t.Fatal(err)
	}

	status, got, raw := a.do(http.MethodGet, path, nil)
	if status != http.StatusOK {
		t.Fatalf("get = %d %s", status, raw)
	}
	firings, _ := got["firings"].([]any)
	if len(firings) != 3 {
		t.Fatalf("firings = %s, want three", raw)
	}
	outcome := func(i int) string { return firings[i].(map[string]any)["outcome"].(string) }
	if outcome(0) != OutcomeInterrupted || outcome(1) != OutcomeSkipped || outcome(2) != OutcomeStarted {
		t.Errorf("outcomes newest first = %s %s %s; want interrupted, skipped, started", outcome(0), outcome(1), outcome(2))
	}
	if firings[2].(map[string]any)["jobId"] == nil || firings[1].(map[string]any)["detail"] == "" {
		t.Errorf("firings = %s; want the started one linked to its job and the skipped one explained", raw)
	}

	status, listed, raw := a.do(http.MethodGet, "/api/v1/orchestrator/schedules", nil)
	// The caller is fleet-wide and the database is shared with other
	// tests, so the list may hold others' schedules: find this one.
	var item map[string]any
	for _, candidate := range listed["items"].([]any) {
		if c := candidate.(map[string]any); c["owner"] == a.h.owner.Name {
			item = c
		}
	}
	if status != http.StatusOK || item == nil {
		t.Fatalf("list = %d %s", status, raw)
	}
	if item["lastFiring"].(map[string]any)["outcome"] != OutcomeInterrupted || item["nextRunAt"] == nil || item["name"] != "weekly no-op" {
		t.Errorf("listed = %s; want the name, next start and last firing", raw)
	}
}

func TestAPI_RefusalsNameTheFieldAndStoreNothing(t *testing.T) {
	a := newAPI(t)
	a.caller.Scope = nodescope.Of("web1")
	base := func(change map[string]any) map[string]any {
		body := a.runBody("web1")
		for k, v := range change {
			body[k] = v
		}
		return body
	}
	cases := []struct {
		name    string
		body    map[string]any
		status  int
		field   string
		mention string
	}{
		{"no name", base(map[string]any{"name": ""}), 400, "name", "required"},
		{"start passed", base(map[string]any{"startAt": a.h.in(-time.Hour).Format(time.RFC3339)}), 400, "startAt", "passed"},
		{"too frequent", base(map[string]any{"repeat": map[string]any{"every": 2, "unit": "minute"}}), 400, "repeat", "5 minutes"},
		{"an unknown kind", base(map[string]any{"kind": "patch"}), 400, "kind", "plan"},
		{"a plan with no steps", base(map[string]any{"kind": "plan"}), 400, "steps", "at least one step"},
		{"a plan with a bad step", base(map[string]any{"kind": "plan", "steps": []map[string]any{{"kind": "run"}, {"kind": "task", "taskName": "mod::nope"}}}), 400, "steps", "step 2"},
		{"node outside scope", base(map[string]any{"targets": []string{"web1", "db1"}}), 403, "target", "db1"},
		{"two kinds of target", base(map[string]any{"query": "nodes[certname] { }"}), 400, "target", "only one"},
		{"subquery", base(map[string]any{"targets": nil, "query": `nodes[certname] { facts { name = "x" } }`}), 400, "target", "subqueries"},
		{"unknown run option", base(map[string]any{"options": map[string]any{"force": true}}), 400, "options", "force"},
		{"unknown task", base(map[string]any{"kind": "task", "task": "mod::nope"}), 400, "task", "mod::nope"},
		{"bad task parameter", base(map[string]any{"kind": "task", "task": "mod::echo", "params": map[string]any{"count": "x"}}), 400, "task", "count"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			status, body, raw := a.do(http.MethodPost, "/api/v1/orchestrator/schedules", c.body)
			if status != c.status || body["field"] != c.field || !strings.Contains(raw, c.mention) {
				t.Errorf("= %d %s; want %d on field %q mentioning %q", status, raw, c.status, c.field, c.mention)
			}
		})
	}
	// A refused task parameter is also named in "fields", as the
	// on-demand task request names it.
	_, body, raw := a.do(http.MethodPost, "/api/v1/orchestrator/schedules",
		base(map[string]any{"kind": "task", "task": "mod::echo", "params": map[string]any{"count": "x"}}))
	if fields, _ := body["fields"].(map[string]any); fields["count"] == nil {
		t.Errorf("bad task parameter = %s; want fields.count", raw)
	}
	// A node outside scope is named, as the on-demand refusal names it.
	_, body, raw = a.do(http.MethodPost, "/api/v1/orchestrator/schedules", base(map[string]any{"targets": []string{"web1", "db1"}}))
	if outside, _ := body["outOfScope"].([]any); len(outside) != 1 || outside[0] != "db1" {
		t.Errorf("out of scope = %s; want outOfScope [db1]", raw)
	}

	// The caller here is scoped, so the list is their own schedules.
	if _, listed, _ := a.do(http.MethodGet, "/api/v1/orchestrator/schedules", nil); len(listed["items"].([]any)) != 0 {
		t.Error("a refused request stored a schedule")
	}
	if status, _, _ := a.do(http.MethodGet, "/api/v1/orchestrator/schedules/not-a-number", nil); status != http.StatusBadRequest {
		t.Errorf("a malformed id = %d, want 400", status)
	}
}

func TestAPI_OwnershipAndVisibility(t *testing.T) {
	a := newAPI(t)
	owner := a.caller
	_, created, _ := a.do(http.MethodPost, "/api/v1/orchestrator/schedules", a.runBody("web1"))
	path := fmt.Sprintf("/api/v1/orchestrator/schedules/%d", int64(created["id"].(float64)))

	// A scoped caller who is not the owner gets, for everything, the
	// answer a missing schedule gets.
	a.caller = a.h.other(nodescope.Of("web1"))
	for _, call := range []struct{ method, suffix string }{
		{http.MethodGet, ""}, {http.MethodPut, ""}, {http.MethodDelete, ""}, {http.MethodPost, "/pause"}, {http.MethodPost, "/resume"},
	} {
		status, _, body := a.do(call.method, path+call.suffix, a.runBody("web1"))
		missingStatus, _, missing := a.do(call.method, "/api/v1/orchestrator/schedules/999999999"+call.suffix, a.runBody("web1"))
		if status != http.StatusNotFound || missingStatus != status || body != missing {
			t.Errorf("%s %s by a scoped non-owner = %d %s; missing = %d %s; want identical 404s", call.method, call.suffix, status, body, missingStatus, missing)
		}
	}

	if _, listed, raw := a.do(http.MethodGet, "/api/v1/orchestrator/schedules", nil); len(listed["items"].([]any)) != 0 {
		t.Errorf("a scoped non-owner lists %s, want nothing", raw)
	}

	// A fleet-wide caller pauses and resumes it: the owner is unchanged.
	admin := a.h.other(nodescope.Everything())
	a.caller = admin
	status, paused, raw := a.do(http.MethodPost, path+"/pause", nil)
	if status != http.StatusOK || paused["state"] != StatePaused || paused["owner"] != owner.Owner.Name {
		t.Errorf("pause = %d %s; want paused, owner unchanged", status, raw)
	}
	status, resumed, raw := a.do(http.MethodPost, path+"/resume", nil)
	if status != http.StatusOK || resumed["state"] != StateActive || resumed["owner"] != owner.Owner.Name {
		t.Errorf("resume = %d %s; want active, owner unchanged", status, raw)
	}
	// Saving a change makes them the owner.
	status, saved, raw := a.do(http.MethodPut, path, a.runBody("web1", "web2"))
	if status != http.StatusOK || saved["owner"] != admin.Owner.Name {
		t.Errorf("replace = %d %s; want the saver as owner", status, raw)
	}
	// Which the former owner, if scoped, can then no longer see.
	a.caller = Caller{Owner: owner.Owner, Scope: nodescope.Of("web1"), Tasks: taskscope.Unrestricted()}
	if status, _, _ := a.do(http.MethodGet, path, nil); status != http.StatusNotFound {
		t.Errorf("the former owner reads it = %d, want 404", status)
	}

	a.caller = admin
	if status, _, _ := a.do(http.MethodDelete, path, nil); status != http.StatusNoContent {
		t.Errorf("delete = %d, want 204", status)
	}
	if status, _, _ := a.do(http.MethodGet, path, nil); status != http.StatusNotFound {
		t.Errorf("get after delete = %d, want 404", status)
	}

	want := map[string]string{
		"GET /api/v1/orchestrator/schedules":              "orchestrator:read",
		"GET /api/v1/orchestrator/schedules/{id}":         "orchestrator:read",
		"POST /api/v1/orchestrator/schedules":             "orchestrator:run",
		"PUT /api/v1/orchestrator/schedules/{id}":         "orchestrator:run",
		"DELETE /api/v1/orchestrator/schedules/{id}":      "orchestrator:run",
		"POST /api/v1/orchestrator/schedules/{id}/pause":  "orchestrator:run",
		"POST /api/v1/orchestrator/schedules/{id}/resume": "orchestrator:run",
	}
	for route, permission := range want {
		if a.gates[route] != permission {
			t.Errorf("%s is gated by %q, want %q", route, a.gates[route], permission)
		}
	}
}

// A schedule's sensitive value never appears in any response.
func TestAPI_SensitiveValuesAreNeverReturned(t *testing.T) {
	a := newAPI(t)
	body := map[string]any{"name": "rotate", "kind": "task", "task": "mod::secret", "targets": []string{"web1"},
		"params": map[string]any{"user": "deploy", "password": "hunter2"}, "startAt": a.h.in(time.Hour).Format(time.RFC3339)}
	_, created, raw := a.do(http.MethodPost, "/api/v1/orchestrator/schedules", body)
	path := fmt.Sprintf("/api/v1/orchestrator/schedules/%d", int64(created["id"].(float64)))
	_, _, got := a.do(http.MethodGet, path, nil)
	_, _, listed := a.do(http.MethodGet, "/api/v1/orchestrator/schedules", nil)
	for name, response := range map[string]string{"create": raw, "get": got, "list": listed} {
		if strings.Contains(response, "hunter2") || !strings.Contains(response, orchestrator.RedactedValue) {
			t.Errorf("%s response = %s; want the password redacted", name, response)
		}
	}
}
