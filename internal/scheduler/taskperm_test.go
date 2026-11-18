package scheduler

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/voxpupuli/enterprise-console/internal/nodescope"
	"github.com/voxpupuli/enterprise-console/internal/orchestrator"
	"github.com/voxpupuli/enterprise-console/internal/taskscope"
)

// allowOnly limits a caller to the named tasks on every node.
func allowOnly(t *testing.T, patterns ...string) taskscope.Scope {
	t.Helper()
	scope, err := taskscope.Resolve(context.Background(), []taskscope.Grant{{Tasks: patterns, Fleet: true}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	return scope
}

func (h *harness) taskOn(task string, nodes ...string) Input {
	return Input{Name: "nightly task", Kind: orchestrator.JobKindTask, Task: task, Targets: nodes, StartAt: h.in(time.Hour)}
}

// "Saving a schedule for a task the caller may not run".
func TestTaskPermission_ASavedScheduleIsRefusedForATaskItsSaverMayNotRun(t *testing.T) {
	h := newHarness(t)
	limited := h.caller()
	limited.Tasks = allowOnly(t, "mod::secret")

	_, err := h.svc.Create(context.Background(), h.taskOn("mod::echo", "web1"), limited)
	var denied *orchestrator.TaskNotPermittedError
	if !errors.As(err, &denied) || denied.Task != "mod::echo" {
		t.Fatalf("Create() = %v, want a refusal naming mod::echo", err)
	}
	if all, _ := h.svc.List(context.Background(), h.caller()); len(all) != 0 {
		t.Errorf("a refused schedule was saved: %v", all)
	}

	// A Puppet run is not a task.
	if _, err := h.svc.Create(context.Background(), h.runOn("web1"), limited); err != nil {
		t.Errorf("a run schedule for a task-limited caller = %v", err)
	}
	// And the allowed task is saved.
	allowed := h.taskOn("mod::secret", "web1")
	allowed.Params = []byte(`{"user":"u","password":"p"}`)
	if _, err := h.svc.Create(context.Background(), allowed, limited); err != nil {
		t.Errorf("an allowed task = %v", err)
	}
}

// "The owner loses the task": nothing is started, and the firing says why.
func TestTaskPermission_AScheduleIsSkippedWhenItsOwnerLosesTheTask(t *testing.T) {
	h := newHarness(t)
	h.mu.Lock()
	h.authority[h.owner.Name] = nodescope.Everything()
	h.mu.Unlock()
	sched := h.create(h.taskOn("mod::echo", "web1"))

	// The owner's role is narrowed after the schedule was saved.
	h.mu.Lock()
	h.tasks[h.owner.Name] = allowOnly(t, "mod::secret")
	h.mu.Unlock()

	fired := wantOutcome(t, h.fireAt(sched.ID, h.in(time.Hour)), OutcomeSkipped, "mod::echo")
	if fired.JobID != nil {
		t.Errorf("a skipped firing started job %d", *fired.JobID)
	}
	if !strings.Contains(fired.Detail, "not allowed") {
		t.Errorf("detail = %q, want it to say the task is not allowed", fired.Detail)
	}
	if jobs := h.ownJobs(); len(jobs) != 0 {
		t.Errorf("%d jobs were started", len(jobs))
	}
}

// "A plan schedule": the skipped firing names the step and the task.
func TestTaskPermission_AScheduledPlanIsSkippedNamingTheStep(t *testing.T) {
	h := newHarness(t)
	sched := h.create(h.planOn([]orchestrator.PlanStep{runStep(""), taskStep("mod::echo", `{}`), runStep("")}, "web1"))

	h.mu.Lock()
	h.tasks[h.owner.Name] = allowOnly(t, "mod::secret")
	h.mu.Unlock()

	wantOutcome(t, h.fireAt(sched.ID, h.in(time.Hour)), OutcomeSkipped, "step 2 (mod::echo)")
	if jobs := h.ownJobs(); len(jobs) != 0 {
		t.Errorf("%d jobs were started", len(jobs))
	}
}

// A schedule saved by a caller who may not run its plan's tasks is refused
// naming the step.
func TestTaskPermission_ASavedPlanScheduleNamesTheStep(t *testing.T) {
	h := newHarness(t)
	limited := h.caller()
	limited.Tasks = allowOnly(t, "mod::secret")
	in := h.planOn([]orchestrator.PlanStep{runStep(""), taskStep("mod::echo", `{}`)}, "web1")

	_, err := h.svc.Create(context.Background(), in, limited)
	var step *orchestrator.PlanStepError
	var denied *orchestrator.TaskNotPermittedError
	if !errors.As(err, &step) || step.Step != 2 || !errors.As(err, &denied) || denied.Task != "mod::echo" {
		t.Errorf("Create() = %v, want a refusal naming step 2 and mod::echo", err)
	}
}

// "Another caller replaces the schedule": replacing makes them the owner,
// so they are checked as the owner will be.
func TestTaskPermission_ReplacingAScheduleChecksTheReplacer(t *testing.T) {
	h := newHarness(t)
	sched := h.create(h.taskOn("mod::echo", "web1"))

	replacer := h.other(nodescope.Everything())
	replacer.Tasks = allowOnly(t, "mod::secret")
	_, err := h.svc.Replace(context.Background(), sched.ID, h.taskOn("mod::echo", "web1"), replacer)
	var denied *orchestrator.TaskNotPermittedError
	if !errors.As(err, &denied) || denied.Task != "mod::echo" {
		t.Fatalf("Replace() = %v, want a refusal naming mod::echo", err)
	}
	if got := h.get(sched.ID); got.Owner != h.owner.Name {
		t.Errorf("the schedule's owner is now %q after a refused replace", got.Owner)
	}
}

// The owner's roles still cover the node, but no longer allow the task
// there: the skipped firing names the task, not just the node.
func TestTaskPermission_ASkippedFiringNamesTheTaskWhenOnlyTheNodeIsRefused(t *testing.T) {
	h := newHarness(t)
	sched := h.create(h.taskOn("mod::echo", "web1"))

	// Allowed on the database node only, by name allowed everywhere.
	scope, err := taskscope.Resolve(context.Background(), []taskscope.Grant{{Tasks: []string{"mod::echo"}, Groups: []int64{2}}}, dbOnly{})
	if err != nil {
		t.Fatal(err)
	}
	h.mu.Lock()
	h.tasks[h.owner.Name] = scope
	h.mu.Unlock()

	wantOutcome(t, h.fireAt(sched.ID, h.in(time.Hour)), OutcomeSkipped, `"mod::echo"`)
}

type dbOnly struct{}

func (dbOnly) For(context.Context, []int64) (nodescope.Scope, error) { return nodescope.Of("db1"), nil }
