package plans

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
	"time"

	"github.com/voxpupuli/enterprise-console/internal/auditlog"
	"github.com/voxpupuli/enterprise-console/internal/orchestrator"
	"github.com/voxpupuli/enterprise-console/internal/taskcatalog"
	"github.com/voxpupuli/enterprise-console/internal/taskscope"
	"github.com/voxpupuli/enterprise-console/internal/testdb"
)

// fakeCatalog has one plain task and one with a sensitive parameter, in
// "production". removed takes a task out of the deployed code.
type fakeCatalog struct {
	mu      sync.Mutex
	removed map[string]bool
}

func (c *fakeCatalog) Task(_ context.Context, environment, name string) (taskcatalog.Task, error) {
	c.mu.Lock()
	gone := c.removed[name]
	c.mu.Unlock()
	if environment != "production" {
		return taskcatalog.Task{}, &taskcatalog.NotFoundError{Kind: "environment", Name: environment}
	}
	task := taskcatalog.Task{Name: name, Environment: environment}
	switch {
	case gone:
	case name == "service":
		task.Metadata.Parameters = map[string]taskcatalog.Parameter{
			"name":   {Type: "String"},
			"action": {Type: "Enum[start, stop, restart]"},
		}
		return task, nil
	case name == "login":
		task.Metadata.Parameters = map[string]taskcatalog.Parameter{
			"user":     {Type: "String"},
			"password": {Type: "String[1]", Sensitive: true},
			"token":    {Type: "Optional[String]", Sensitive: true},
		}
		return task, nil
	}
	return taskcatalog.Task{}, &taskcatalog.NotFoundError{Kind: "task", Name: name}
}

// api serves the saved-plan routes over a real store, recording the
// permission each route was gated by and everything written to the
// activity and audit logs.
type api struct {
	t       *testing.T
	mux     *http.ServeMux
	catalog *fakeCatalog
	tag     string
	actor   string
	gates   map[string]string
	// tasks is what the caller may run; unrestricted unless a test says.
	tasks taskscope.Scope

	mu       sync.Mutex
	activity []string
	audit    []auditlog.Event
}

func newAPI(t *testing.T) *api {
	t.Helper()
	pool := testdb.Pool(t)
	a := &api{t: t, mux: http.NewServeMux(), catalog: &fakeCatalog{}, gates: map[string]string{},
		tag: strconv.FormatInt(time.Now().UnixNano(), 36), actor: "plan-editor", tasks: taskscope.Unrestricted()}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM plans WHERE name LIKE '%' || $1`, a.tag)
	})
	orch := orchestrator.NewHandlers(orchestrator.NewStore(pool), nil, func(*http.Request) string { return a.actor }, nil)
	orch.SetTaskCatalog(a.catalog)
	svc := NewService(NewStore(pool), orch.Starter())
	svc.SetRecorders(func(action, actor, summary string) {
		a.mu.Lock()
		a.activity = append(a.activity, action+" "+actor+" "+summary)
		a.mu.Unlock()
	}, func(e auditlog.Event) {
		a.mu.Lock()
		a.audit = append(a.audit, e)
		a.mu.Unlock()
	})
	handlers := NewHandlers(svc, func(*http.Request) string { return a.actor }, nil)
	handlers.SetTaskScope(func(*http.Request) (taskscope.Scope, error) { return a.tasks, nil })
	handlers.Register(a.mux, func(permission string, next http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			a.gates[r.Pattern] = permission
			next(w, r)
		}
	})
	return a
}

// name makes a plan name unique to this test run: the plans table is
// shared with whatever else uses the test database.
func (a *api) name(s string) string { return s + " " + a.tag }

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

func (a *api) create(name string, steps ...map[string]any) (int, map[string]any, string) {
	return a.do("POST", "/api/v1/orchestrator/saved-plans", map[string]any{"name": a.name(name), "description": "for tests", "steps": steps})
}

func idOf(body map[string]any) string { return strconv.FormatInt(int64(body["id"].(float64)), 10) }

var (
	runStep  = map[string]any{"kind": "run", "options": map[string]any{"noop": true}}
	taskStep = map[string]any{"kind": "task", "taskName": "service", "params": map[string]any{"name": "nginx", "action": "restart"}, "timeoutSeconds": 120}
)

func TestSavedPlans_SaveListReadChangeDelete(t *testing.T) {
	a := newAPI(t)

	code, created, raw := a.create("Patch web tier", taskStep, runStep)
	if code != http.StatusCreated {
		t.Fatalf("create = %d %s", code, raw)
	}
	steps := created["steps"].([]any)
	if len(steps) != 2 || steps[0].(map[string]any)["taskName"] != "service" || steps[1].(map[string]any)["kind"] != "run" {
		t.Fatalf("steps = %v, want the task then the run", steps)
	}
	if created["createdBy"] != "plan-editor" || created["stepCount"] != float64(2) {
		t.Errorf("created = %v", created)
	}
	id := idOf(created)

	_, list, _ := a.do("GET", "/api/v1/orchestrator/saved-plans", nil)
	found := false
	for _, item := range list["items"].([]any) {
		p := item.(map[string]any)
		if p["name"] == a.name("Patch web tier") {
			found = p["stepCount"] == float64(2) && p["description"] == "for tests" && p["steps"] == nil
		}
	}
	if !found {
		t.Errorf("list = %v, want the plan with its description and step count and no steps", list)
	}

	// Changing it: reorder, by someone else.
	a.actor = "second-editor"
	code, changed, raw := a.do("PUT", "/api/v1/orchestrator/saved-plans/"+id, map[string]any{"name": a.name("Patch web tier"), "steps": []any{runStep, taskStep}})
	if code != http.StatusOK || changed["steps"].([]any)[0].(map[string]any)["kind"] != "run" || changed["updatedBy"] != "second-editor" || changed["createdBy"] != "plan-editor" {
		t.Errorf("replace = %d %s", code, raw)
	}

	if code, _, _ := a.do("DELETE", "/api/v1/orchestrator/saved-plans/"+id, nil); code != http.StatusNoContent {
		t.Errorf("delete = %d", code)
	}
	for _, call := range [][2]string{{"GET", ""}, {"PUT", ""}, {"DELETE", ""}} {
		var body any
		if call[0] == "PUT" {
			body = map[string]any{"name": a.name("x"), "steps": []any{runStep}}
		}
		if code, _, _ := a.do(call[0], "/api/v1/orchestrator/saved-plans/"+id, body); code != http.StatusNotFound {
			t.Errorf("%s after delete = %d, want 404", call[0], code)
		}
	}

	a.mu.Lock()
	defer a.mu.Unlock()
	log := strings.Join(a.activity, "\n")
	for _, want := range []string{
		`plan.created plan-editor created plan "` + a.name("Patch web tier") + `" (2 steps)`,
		`plan.updated second-editor changed plan`,
		`plan.deleted second-editor deleted plan`,
	} {
		if !strings.Contains(log, want) {
			t.Errorf("activity lacks %q:\n%s", want, log)
		}
	}
	if len(a.audit) != 3 || a.audit[1].Action != "plan.updated" || a.audit[1].Actor != "second-editor" || a.audit[1].ResourceType != "plan" || a.audit[1].ResourceID != id {
		t.Errorf("audit = %+v", a.audit)
	}
}

func TestSavedPlans_Permissions(t *testing.T) {
	a := newAPI(t)
	_, created, _ := a.create("Gated", runStep)
	id := idOf(created)
	a.do("GET", "/api/v1/orchestrator/saved-plans", nil)
	a.do("GET", "/api/v1/orchestrator/saved-plans/"+id, nil)
	a.do("PUT", "/api/v1/orchestrator/saved-plans/"+id, map[string]any{"name": a.name("Gated"), "steps": []any{runStep}})
	a.do("DELETE", "/api/v1/orchestrator/saved-plans/"+id, nil)
	want := map[string]string{
		"GET /api/v1/orchestrator/saved-plans":         "orchestrator:read",
		"GET /api/v1/orchestrator/saved-plans/{id}":    "orchestrator:read",
		"POST /api/v1/orchestrator/saved-plans":        "orchestrator:run",
		"PUT /api/v1/orchestrator/saved-plans/{id}":    "orchestrator:run",
		"DELETE /api/v1/orchestrator/saved-plans/{id}": "orchestrator:run",
	}
	for route, permission := range want {
		if a.gates[route] != permission {
			t.Errorf("%s gated by %q, want %q", route, a.gates[route], permission)
		}
	}
}

func TestSavedPlans_Refusals(t *testing.T) {
	a := newAPI(t)
	if code, _, raw := a.create("Taken", runStep); code != http.StatusCreated {
		t.Fatalf("create = %d %s", code, raw)
	}
	_, other, _ := a.create("Other", runStep)

	many := make([]map[string]any, orchestrator.MaxPlanSteps+1)
	for i := range many {
		many[i] = runStep
	}
	cases := []struct {
		name    string
		plan    string
		steps   []map[string]any
		field   any
		step    any
		mention string
	}{
		{"a name already in use", "Taken", []map[string]any{runStep}, "name", nil, "already exists"},
		{"no steps", "Empty", nil, "steps", nil, "at least one step"},
		{"too many steps", "Long", many, "steps", nil, "at most 20"},
		{"an unknown task", "Unknown", []map[string]any{runStep, {"kind": "task", "taskName": "nope"}}, nil, float64(2), "nope"},
		{"an invalid parameter", "Bad param", []map[string]any{{"kind": "task", "taskName": "service", "params": map[string]any{"name": "nginx", "action": "explode"}}}, nil, float64(1), "action"},
		{"an unknown environment on a run step", "Bad env", []map[string]any{{"kind": "run", "options": map[string]any{"environment": "no such"}}}, nil, float64(1), "environment"},
		{"an unknown environment on a task step", "Bad task env", []map[string]any{{"kind": "task", "taskName": "service", "environment": "nowhere"}}, nil, float64(1), "nowhere"},
	}
	for _, tc := range cases {
		code, body, raw := a.create(tc.plan, tc.steps...)
		if code != http.StatusBadRequest || !strings.Contains(raw, tc.mention) {
			t.Errorf("%s: %d %s, want 400 mentioning %q", tc.name, code, raw, tc.mention)
			continue
		}
		if tc.field != nil && body["field"] != tc.field {
			t.Errorf("%s: field = %v, want %v", tc.name, body["field"], tc.field)
		}
		if tc.step != nil && body["step"] != tc.step {
			t.Errorf("%s: step = %v, want %v", tc.name, body["step"], tc.step)
		}
	}
	// The invalid parameter also names the field, for the editor.
	_, body, _ := a.create("Bad param again", map[string]any{"kind": "task", "taskName": "service", "params": map[string]any{"name": "nginx", "action": "explode"}})
	if fields, _ := body["fields"].(map[string]any); fields["action"] == nil {
		t.Errorf("fields = %v, want action", body["fields"])
	}

	// Renaming onto another plan's name is refused the same way.
	code, body, raw := a.do("PUT", "/api/v1/orchestrator/saved-plans/"+idOf(other), map[string]any{"name": a.name("Taken"), "steps": []any{runStep}})
	if code != http.StatusBadRequest || body["field"] != "name" {
		t.Errorf("rename onto a taken name = %d %s", code, raw)
	}
	// And a blank name.
	if code, body, _ := a.do("POST", "/api/v1/orchestrator/saved-plans", map[string]any{"name": "  ", "steps": []any{runStep}}); code != http.StatusBadRequest || body["field"] != "name" {
		t.Errorf("blank name = %d %v", code, body)
	}
	if code, body, _ := a.do("POST", "/api/v1/orchestrator/saved-plans", map[string]any{"name": a.name("Wordy"), "description": strings.Repeat("d", MaxDescriptionLength+1), "steps": []any{runStep}}); code != http.StatusBadRequest || body["field"] != "description" {
		t.Errorf("long description = %d %v", code, body)
	}
}

func TestSavedPlans_SensitiveValuesAreNotKept(t *testing.T) {
	a := newAPI(t)
	const secret = "hunter2-do-not-keep"
	code, created, raw := a.create("Login", map[string]any{"kind": "task", "taskName": "login", "params": map[string]any{"user": "deploy", "password": secret, "token": secret}})
	if code != http.StatusCreated {
		t.Fatalf("create = %d %s", code, raw)
	}
	if strings.Contains(raw, secret) {
		t.Errorf("the response carries the sensitive value: %s", raw)
	}
	step := created["steps"].([]any)[0].(map[string]any)
	if params := step["params"].(map[string]any); len(params) != 1 || params["user"] != "deploy" {
		t.Errorf("params = %v, want user alone", params)
	}
	needs := step["needs"].([]any)
	if len(needs) != 2 || needs[0].(map[string]any)["param"] != "password" || needs[0].(map[string]any)["required"] != true || needs[1].(map[string]any)["required"] != false {
		t.Errorf("needs = %v, want password (required) and token (optional)", needs)
	}

	// Nothing of it reached the database.
	var stored string
	if err := testdb.Pool(t).QueryRow(context.Background(), `SELECT steps::text FROM plans WHERE id = $1`, int64(created["id"].(float64))).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(stored, secret) || strings.Contains(stored, "password") {
		t.Errorf("stored steps hold the sensitive parameter: %s", stored)
	}

	// Saved without the value at all, though the task requires it.
	if code, _, raw := a.create("Login later", map[string]any{"kind": "task", "taskName": "login", "params": map[string]any{"user": "deploy"}}); code != http.StatusCreated {
		t.Errorf("saving without the sensitive value = %d %s", code, raw)
	}

	// A task that has since gone is reported on the step, and the plan
	// is still read.
	a.catalog.mu.Lock()
	a.catalog.removed = map[string]bool{"login": true}
	a.catalog.mu.Unlock()
	code, read, raw := a.do("GET", "/api/v1/orchestrator/saved-plans/"+idOf(created), nil)
	if code != http.StatusOK || read["steps"].([]any)[0].(map[string]any)["problem"] == nil {
		t.Errorf("read with the task gone = %d %s", code, raw)
	}
}
