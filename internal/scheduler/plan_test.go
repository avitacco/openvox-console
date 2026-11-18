package scheduler

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/voxpupuli/enterprise-console/internal/nodescope"
	"github.com/voxpupuli/enterprise-console/internal/orchestrator"
	"github.com/voxpupuli/enterprise-console/internal/sealer"
)

func runStep(options string) orchestrator.PlanStep {
	step := orchestrator.PlanStep{Kind: orchestrator.JobKindRun}
	if options != "" {
		step.Options = json.RawMessage(options)
	}
	return step
}

func taskStep(task, params string) orchestrator.PlanStep {
	return orchestrator.PlanStep{Kind: orchestrator.JobKindTask, TaskName: task, Params: json.RawMessage(params)}
}

// planOn is a one-off plan on named nodes, an hour from now.
func (h *harness) planOn(steps []orchestrator.PlanStep, nodes ...string) Input {
	return Input{Name: "weekly patch", Kind: orchestrator.JobKindPlan, PlanName: "Patch web tier", Steps: steps,
		Targets: nodes, StartAt: h.in(time.Hour)}
}

func (h *harness) planSteps(planJobID int64) []orchestrator.Job {
	h.t.Helper()
	steps, err := h.jobs.GetPlanSteps(context.Background(), planJobID)
	if err != nil {
		h.t.Fatal(err)
	}
	return steps
}

func TestPlan_StoredWithItsStepsAndFiredAsAPlan(t *testing.T) {
	h := newHarness(t)
	sched := h.create(h.planOn([]orchestrator.PlanStep{
		taskStep("mod::echo", `{"message":"draining"}`),
		runStep(`{"noop":true}`),
	}, "web1", "web2"))

	if sched.Kind != orchestrator.JobKindPlan || sched.PlanName != "Patch web tier" || len(sched.Steps) != 2 {
		t.Fatalf("schedule = %+v", sched)
	}
	if s := sched.Steps[0]; s.TaskName != "mod::echo" || s.Environment != "production" || !strings.Contains(string(s.Params), "draining") {
		t.Errorf("step 1 = %+v", s)
	}
	if s := sched.Steps[1]; s.Kind != orchestrator.JobKindRun || !strings.Contains(string(s.Options), "noop") {
		t.Errorf("step 2 = %+v", s)
	}
	if found := h.activityContaining(`created schedule "weekly patch" (plan "Patch web tier" (2 steps)`); len(found) != 1 {
		t.Errorf("activity = %v", h.activity)
	}

	fired := h.fireAt(sched.ID, h.in(time.Hour))
	f := wantOutcome(t, fired, OutcomeStarted, "")
	plan := h.waitFinished(*f.JobID)
	if plan.Kind != orchestrator.JobKindPlan || plan.PlanName != "Patch web tier" || plan.Status != orchestrator.StatusSucceeded ||
		plan.TriggeredBy != h.owner.Name || plan.Schedule == nil || plan.Schedule.ID != sched.ID {
		t.Errorf("plan job = %+v", plan)
	}
	steps := h.planSteps(plan.ID)
	if len(steps) != 2 || steps[0].TaskName != "mod::echo" || steps[1].RunOptions == nil || !steps[1].RunOptions.Noop {
		t.Fatalf("steps = %+v", steps)
	}
	if got := targetNames(steps[1]); len(got) != 2 {
		t.Errorf("step targets = %v, want both nodes", got)
	}
	if found := h.activityContaining(`started by schedule "weekly patch"`); len(found) == 0 {
		t.Errorf("the plan's activity entry does not name the schedule: %v", h.activity)
	}
	// The plan name defaults to the schedule's.
	unnamed := h.planOn([]orchestrator.PlanStep{runStep("")}, "web1")
	unnamed.PlanName, unnamed.StartAt = "", h.in(3*time.Hour)
	if got := h.create(unnamed); got.PlanName != "weekly patch" {
		t.Errorf("plan name = %q, want the schedule's name", got.PlanName)
	}
}

func TestPlan_SensitiveValuesAreSealedByStep(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	steps := []orchestrator.PlanStep{
		runStep(""),
		taskStep("mod::secret", `{"user":"deploy","password":"hunter2"}`),
		taskStep("mod::secret", `{"user":"backup","password":"swordfish"}`),
	}
	sched := h.create(h.planOn(steps, "web1"))

	encoded, _ := json.Marshal(h.get(sched.ID))
	if strings.Contains(string(encoded), "hunter2") || strings.Contains(string(encoded), "swordfish") || !strings.Contains(string(encoded), orchestrator.RedactedValue) {
		t.Errorf("schedule as read = %s; want both passwords redacted", encoded)
	}
	var raw, sealed []byte
	if err := h.store.pool.QueryRow(ctx, `SELECT steps::text, sealed_params FROM scheduled_jobs WHERE id = $1`, sched.ID).Scan(&raw, &sealed); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "hunter2") || strings.Contains(string(sealed), "hunter2") || len(sealed) == 0 {
		t.Errorf("stored steps = %s, sealed = %d bytes; the passwords must be stored only sealed", raw, len(sealed))
	}

	// The owner saves a change, sending back what a read gave them.
	redacted := `{"user":"deploy","password":"` + orchestrator.RedactedValue + `"}`
	change := h.planOn([]orchestrator.PlanStep{runStep(""), taskStep("mod::secret", redacted), taskStep("mod::secret", `{"user":"backup"}`)}, "web1")
	change.StartAt = h.in(2 * time.Hour)
	if _, err := h.svc.Replace(ctx, sched.ID, change, h.caller()); err != nil {
		t.Fatalf("the owner's change = %v, want the stored passwords kept", err)
	}
	f := wantOutcome(t, h.fireAt(sched.ID, h.in(2*time.Hour)), OutcomeStarted, "")
	h.waitFinished(*f.JobID)
	sent := strings.Join(h.transport.sent(), "\n")
	if !strings.Contains(sent, "hunter2") || !strings.Contains(sent, "swordfish") || strings.Contains(sent, orchestrator.RedactedValue) {
		t.Errorf("sent = %s; want each step's own password", sent)
	}
	for _, step := range h.planSteps(*f.JobID) {
		if strings.Contains(string(step.Params), "hunter2") || strings.Contains(string(step.Params), "swordfish") {
			t.Errorf("a step's stored params hold a password: %s", step.Params)
		}
	}

	// A step that moved does not inherit the value that was at its new
	// position's task... unless the task there is the same; one whose
	// task changed must be given its value again.
	moved := h.planOn([]orchestrator.PlanStep{taskStep("mod::secret", `{"user":"deploy"}`), runStep("")}, "web1")
	moved.StartAt = h.in(5 * time.Hour)
	_, err := h.svc.Replace(ctx, sched.ID, moved, h.caller())
	var stepErr *orchestrator.PlanStepError
	if !errors.As(err, &stepErr) || stepErr.Step != 1 || !strings.Contains(err.Error(), "password") {
		t.Errorf("a step at a position that held no value = %v, want step 1 refused for its password", err)
	}

	// Someone else saving it must type the values.
	again := h.planOn([]orchestrator.PlanStep{runStep(""), taskStep("mod::secret", `{"user":"deploy"}`), taskStep("mod::secret", `{"user":"backup"}`)}, "web1")
	again.StartAt = h.in(5 * time.Hour)
	if _, err := h.svc.Replace(ctx, sched.ID, again, h.other(nodescope.Everything())); !errors.As(err, &stepErr) || stepErr.Step != 2 {
		t.Errorf("another user's change without the values = %v, want step 2 refused", err)
	}
	if got := h.get(sched.ID); got.Owner != h.owner.Name {
		t.Errorf("owner = %s after refused changes, want it unchanged", got.Owner)
	}
}

func TestPlan_SensitiveValuesNeedAKeyAndAreBoundToTheSchedule(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	secret := []orchestrator.PlanStep{taskStep("mod::secret", `{"user":"deploy","password":"hunter2"}`)}
	a := h.create(h.planOn(secret, "web1"))
	b := h.create(h.planOn([]orchestrator.PlanStep{taskStep("mod::secret", `{"user":"deploy","password":"other"}`)}, "web1"))
	if _, err := h.store.pool.Exec(ctx,
		`UPDATE scheduled_jobs SET sealed_params = (SELECT sealed_params FROM scheduled_jobs WHERE id = $1) WHERE id = $2`, a.ID, b.ID); err != nil {
		t.Fatal(err)
	}
	wantOutcome(t, h.fireAt(b.ID, h.in(time.Hour)), OutcomeSkipped, "could not be read")
	if sent := strings.Join(h.transport.sent(), "\n"); strings.Contains(sent, "hunter2") {
		t.Error("a sealed value copied from another schedule was opened and sent")
	}

	h.svc.sealer = nil
	later := h.planOn(secret, "web1")
	later.StartAt = h.in(3 * time.Hour)
	_, err := h.svc.Create(ctx, later, h.caller())
	wantField(t, err, "steps", "CONSOLE_SECRETS_KEY")
	later.Steps = []orchestrator.PlanStep{taskStep("mod::echo", `{}`)}
	if _, err := h.svc.Create(ctx, later, h.caller()); err != nil {
		t.Errorf("a plan without sensitive parameters = %v, want it saved without a key", err)
	}
	key, _ := sealer.New(testKey())
	h.svc.sealer = key
}

func TestPlan_AStepThatCanNoLongerRunSkipsTheFiring(t *testing.T) {
	h := newHarness(t)
	sched := h.create(h.planOn([]orchestrator.PlanStep{runStep(""), taskStep("mod::echo", `{}`)}, "web1"))
	h.catalog.remove("mod::echo")

	fired := h.fireAt(sched.ID, h.in(time.Hour))
	wantOutcome(t, fired, OutcomeSkipped, "step 2 (mod::echo)")
	if jobs := h.ownJobs(); len(jobs) != 0 {
		t.Errorf("%d jobs were started", len(jobs))
	}
	if len(h.transport.sent()) != 0 {
		t.Error("something was sent to a node")
	}
	if found := h.activityContaining("schedule.skipped"); len(found) != 1 || !strings.Contains(found[0], "step 2") {
		t.Errorf("activity = %v", found)
	}
}

func TestPlan_OverlapAndOwnersScope(t *testing.T) {
	h := newHarness(t)
	in := h.planOn([]orchestrator.PlanStep{runStep(""), runStep("")})
	in.Query, in.Repeat = "nodes[certname] { }", &Repeat{1, UnitHour}
	h.matches("web1", "db1")
	sched := h.create(in)

	// The owner's scope has narrowed since: every step runs on what is
	// left of it.
	h.mu.Lock()
	h.authority[h.owner.Name] = nodescope.Of("web1")
	h.mu.Unlock()
	h.hold()
	f := wantOutcome(t, h.fireAt(sched.ID, h.in(time.Hour)), OutcomeStarted, "")
	for _, step := range h.planSteps(*f.JobID) {
		if got := targetNames(step); len(got) != 1 || got[0] != "web1" {
			t.Errorf("step targets = %v, want web1 only", got)
		}
	}

	// Still running an hour on: the next firing is skipped.
	wantOutcome(t, h.fireAt(sched.ID, h.in(2*time.Hour)), OutcomeSkipped, "still running")
	h.release()
	h.waitFinished(*f.JobID)
	wantOutcome(t, h.fireAt(sched.ID, h.in(3*time.Hour)), OutcomeStarted, "")
}
