package plans

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/voxpupuli/enterprise-console/internal/nodescope"
	"github.com/voxpupuli/enterprise-console/internal/taskscope"
)

type noGroups struct{}

func (noGroups) For(context.Context, []int64) (nodescope.Scope, error) {
	return nodescope.Of("n1"), nil
}

// allowOnly makes the test's caller hold exactly the tasks named.
func (a *api) allowOnly(patterns ...string) {
	scope, err := taskscope.Resolve(context.Background(), []taskscope.Grant{{Tasks: patterns, Groups: []int64{1}}}, noGroups{})
	if err != nil {
		a.t.Fatal(err)
	}
	a.tasks = scope
}

// "Saving a plan with a task the caller may not run".
func TestSavedPlans_AnEditorCannotSaveATaskTheyMayNotRun(t *testing.T) {
	a := newAPI(t)
	a.allowOnly("login")

	code, body, raw := a.create("Restart", runStep, taskStep)
	if code != http.StatusForbidden || body["step"] != float64(2) || body["task"] != "service" || !strings.Contains(raw, "step 2 (service)") {
		t.Fatalf("create = %d %s, want 403 naming step 2 and service", code, raw)
	}
	_, list, _ := a.do("GET", "/api/v1/orchestrator/saved-plans", nil)
	for _, item := range list["items"].([]any) {
		if item.(map[string]any)["name"] == a.name("Restart") {
			t.Error("a refused plan was saved")
		}
	}

	// A plan of runs, and of an allowed task, is saved.
	if code, _, raw := a.create("Runs", runStep, runStep); code != http.StatusCreated {
		t.Errorf("a plan of runs = %d %s", code, raw)
	}
	loginStep := map[string]any{"kind": "task", "taskName": "login", "params": map[string]any{"user": "u"}}
	if code, _, raw := a.create("Login", loginStep); code != http.StatusCreated {
		t.Errorf("an allowed task = %d %s", code, raw)
	}
}

// "Reading a plan with a task outside the reader's list", "Running that
// plan" is the orchestrator's refusal; "Editing that plan".
func TestSavedPlans_AReaderSeesAStepTheyMayNotRun(t *testing.T) {
	a := newAPI(t)
	code, created, raw := a.create("Shared", runStep, taskStep)
	if code != http.StatusCreated {
		t.Fatalf("an unrestricted editor's create = %d %s", code, raw)
	}
	id := idOf(created)

	// Another caller, allowed only the login task.
	a.allowOnly("login")
	code, read, raw := a.do("GET", "/api/v1/orchestrator/saved-plans/"+id, nil)
	if code != http.StatusOK {
		t.Fatalf("read = %d %s", code, raw)
	}
	steps := read["steps"].([]any)
	if len(steps) != 2 {
		t.Fatalf("a reader sees %d steps, want every step", len(steps))
	}
	run, task := steps[0].(map[string]any), steps[1].(map[string]any)
	if run["notPermitted"] != nil || run["problem"] != nil {
		t.Errorf("a run step is marked: %v", run)
	}
	if task["notPermitted"] != true || !strings.Contains(task["problem"].(string), `"service"`) || task["taskName"] != "service" {
		t.Errorf("the task step = %v, want it shown and marked as one the reader may not run", task)
	}
	if task["needs"] != nil {
		t.Errorf("the catalogue was read for a task the reader may not run: %v", task)
	}

	// Saving it back without removing the step is refused.
	code, body, raw := a.do("PUT", "/api/v1/orchestrator/saved-plans/"+id, map[string]any{"name": a.name("Shared"), "steps": []any{runStep, taskStep}})
	if code != http.StatusForbidden || body["step"] != float64(2) || body["task"] != "service" {
		t.Errorf("editing it = %d %s, want 403 naming step 2 and service", code, raw)
	}
	// Removing the step lets them save.
	if code, _, raw := a.do("PUT", "/api/v1/orchestrator/saved-plans/"+id, map[string]any{"name": a.name("Shared"), "steps": []any{runStep}}); code != http.StatusOK {
		t.Errorf("saving without the step = %d %s", code, raw)
	}

	// The unrestricted caller still sees the plan as it was saved.
	a.tasks = taskscope.Unrestricted()
	_, again, _ := a.do("GET", "/api/v1/orchestrator/saved-plans/"+id, nil)
	if again["stepCount"] != float64(1) {
		t.Errorf("the plan after the reader's edit = %v", again)
	}
}
