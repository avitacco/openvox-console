package orchestrator

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/voxpupuli/enterprise-console/internal/auditlog"
	"github.com/voxpupuli/enterprise-console/internal/nodescope"
	"github.com/voxpupuli/enterprise-console/internal/taskscope"
)

// Node groups the tests grant on. Group 1 is the web tier, 2 the
// database tier, 3 both.
var tierGroups = map[int64][]string{
	1: {"web1"},
	2: {"db1"},
	3: {"web1", "db1"},
}

type tierNodes map[int64][]string

func (g tierNodes) For(_ context.Context, ids []int64) (nodescope.Scope, error) {
	var nodes []string
	for _, id := range ids {
		nodes = append(nodes, g[id]...)
	}
	return nodescope.Of(nodes...), nil
}

// allowing makes the harness's callers hold exactly grants.
func allowing(t *testing.T, h *taskHarness, grants ...taskscope.Grant) *auditCapture {
	t.Helper()
	scope, err := taskscope.Resolve(context.Background(), grants, tierNodes(tierGroups))
	if err != nil {
		t.Fatal(err)
	}
	h.handlers.SetTaskScope(func(*http.Request) (taskscope.Scope, error) { return scope, nil })
	captured := &auditCapture{}
	h.handlers.SetAuditWrite(captured.record)
	return captured
}

type auditCapture struct {
	mu     sync.Mutex
	events []auditlog.Event
}

func (c *auditCapture) record(_ *http.Request, e auditlog.Event) {
	c.mu.Lock()
	c.events = append(c.events, e)
	c.mu.Unlock()
}

func (c *auditCapture) all() []auditlog.Event {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]auditlog.Event(nil), c.events...)
}

func permissionHarness(t *testing.T) *taskHarness {
	t.Helper()
	f := &fakeSources{rows: certRows("web1", "db1"), groups: map[int64][]string{9: {"web1", "db1"}, 10: {"db1"}}}
	return planHarness(t, f, okResponse)
}

func errorBody(t *testing.T, rec *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode %q: %v", rec.Body.String(), err)
	}
	return body
}

// "A task outside the allowlist": refused, naming the task, nothing
// created, and audited without the parameters.
func TestTaskPermission_ATaskOutsideTheAllowlistIsRefused(t *testing.T) {
	h := permissionHarness(t)
	audit := allowing(t, h, taskscope.Grant{Tasks: []string{"mod::echo"}, Groups: []int64{3}})
	before := h.jobCount(t)

	rec := h.post(t, h.handlers.triggerTask, map[string]any{"targets": []string{"web1"}, "task": "verify", "params": map[string]any{"message": "hunter2"}})
	body := errorBody(t, rec)
	if rec.Code != http.StatusForbidden || body["task"] != "verify" {
		t.Fatalf("response = %d %s, want 403 naming verify", rec.Code, rec.Body)
	}
	if h.jobCount(t) != before {
		t.Error("a refused task created a job")
	}
	events := audit.all()
	if len(events) != 1 || events[0].Action != "task.denied" || events[0].ResourceID != "verify" || events[0].After != nil {
		t.Fatalf("audit = %+v, want one task.denied naming verify and no step", events)
	}
	if strings.Contains(strings.ToLower(rec.Body.String()+jsonString(events[0])), "hunter2") {
		t.Error("a parameter value reached the response or the audit entry")
	}

	// The allowed task starts.
	if rec := h.post(t, h.handlers.triggerTask, map[string]any{"targets": []string{"web1"}, "task": "mod::echo"}); rec.Code != http.StatusOK {
		t.Errorf("an allowed task = %d %s", rec.Code, rec.Body)
	}
}

func jsonString(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}

// "Existence is not revealed": a task that exists and one that does not
// are refused alike.
func TestTaskPermission_ExistenceIsNotRevealed(t *testing.T) {
	h := permissionHarness(t)
	allowing(t, h, taskscope.Grant{Tasks: []string{"mod::echo"}, Groups: []int64{3}})

	answer := func(task string) (int, string) {
		rec := h.post(t, h.handlers.triggerTask, map[string]any{"targets": []string{"web1"}, "task": task})
		return rec.Code, strings.ReplaceAll(rec.Body.String(), task, "TASK")
	}
	codeA, bodyA := answer("verify")       // exists, not allowed
	codeB, bodyB := answer("nope::absent") // does not exist
	if codeA != http.StatusForbidden || codeA != codeB || bodyA != bodyB {
		t.Errorf("an existing task gets %d %s, a missing one %d %s", codeA, bodyA, codeB, bodyB)
	}
}

// "Nodes the task is not allowed on", and the pairing: the task is
// allowed by name for this caller, but not on this node.
func TestTaskPermission_NamedNodesOutsideTheTasksNodesAreRefused(t *testing.T) {
	h := permissionHarness(t)
	// mod::echo on the web tier only; anything on the database tier.
	allowing(t, h,
		taskscope.Grant{Tasks: []string{"mod::echo"}, Groups: []int64{1}},
		taskscope.Grant{Tasks: []string{taskscope.Everything}, Groups: []int64{2}},
	)
	before := h.jobCount(t)

	// verify is allowed (on the database tier) but not on a web node.
	rec := h.post(t, h.handlers.triggerTask, map[string]any{"targets": []string{"web1"}, "task": "verify"})
	if rec.Code != http.StatusForbidden || !reflect.DeepEqual(refusal(t, rec).OutOfScope, []string{"web1"}) {
		t.Errorf("verify on a web node = %d %s, want 403 outside scope naming web1", rec.Code, rec.Body)
	}
	// The node is inside the caller's groups through another role, so the
	// refusal says it is the task that is not allowed there, not that the
	// node is hidden from them.
	if body := errorBody(t, rec); body["task"] != "verify" || !strings.Contains(rec.Body.String(), `your roles do not allow the task`) {
		t.Errorf("the refusal does not name the task: %s", rec.Body)
	}
	// A list with one allowed and one not refuses the whole request.
	rec = h.post(t, h.handlers.triggerTask, map[string]any{"targets": []string{"web1", "db1"}, "task": "verify"})
	if rec.Code != http.StatusForbidden || !reflect.DeepEqual(refusal(t, rec).OutOfScope, []string{"web1"}) {
		t.Errorf("a mixed list = %d %s", rec.Code, rec.Body)
	}
	if h.jobCount(t) != before {
		t.Error("a refused request created a job")
	}
	// mod::echo is allowed on both tiers through the two grants.
	if rec := h.post(t, h.handlers.triggerTask, map[string]any{"targets": []string{"web1", "db1"}, "task": "mod::echo"}); rec.Code != http.StatusOK {
		t.Errorf("mod::echo on both tiers = %d %s", rec.Code, rec.Body)
	}
}

// "A query or group is narrowed", and narrowing to nothing starts nothing.
func TestTaskPermission_AGroupOrQueryIsNarrowed(t *testing.T) {
	h := permissionHarness(t)
	allowing(t, h, taskscope.Grant{Tasks: []string{"mod::echo"}, Groups: []int64{1}})

	for name, target := range map[string]map[string]any{
		"a group": {"group": 9},
		"a query": {"query": "nodes[certname] { }"},
	} {
		body := map[string]any{"task": "mod::echo"}
		for k, v := range target {
			body[k] = v
		}
		job := decodeJob(t, h.post(t, h.handlers.triggerTask, body))
		if got := jobTargets(job); !reflect.DeepEqual(got, []string{"web1"}) {
			t.Errorf("%s: targets = %v, want the web node only", name, got)
		}
	}

	// A group with nothing the task may run on.
	before := h.jobCount(t)
	rec := h.post(t, h.handlers.triggerTask, map[string]any{"task": "mod::echo", "group": 10})
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "no nodes matched") {
		t.Errorf("narrowed to nothing = %d %s", rec.Code, rec.Body)
	}
	if h.jobCount(t) != before {
		t.Error("a request narrowed to nothing created a job")
	}
}

// "A plan with a task the caller may not run": refused whole, naming the
// step and the task, with the step in the audit entry.
func TestTaskPermission_APlanWithATaskTheCallerMayNotRun(t *testing.T) {
	h := permissionHarness(t)
	audit := allowing(t, h, taskscope.Grant{Tasks: []string{"mod::echo"}, Groups: []int64{3}})
	before := h.jobCount(t)

	rec := h.post(t, h.handlers.triggerPlan, map[string]any{
		"name": "refused", "targets": []string{"web1"},
		"steps": []map[string]any{{"kind": "run"}, {"kind": "task", "taskName": "verify"}, {"kind": "run"}},
	})
	body := errorBody(t, rec)
	if rec.Code != http.StatusForbidden || body["step"] != float64(2) || body["task"] != "verify" || !strings.Contains(rec.Body.String(), "step 2 (verify)") {
		t.Fatalf("response = %d %s, want 403 naming step 2 and verify", rec.Code, rec.Body)
	}
	if h.jobCount(t) != before {
		t.Error("a refused plan created jobs")
	}
	events := audit.all()
	if len(events) != 1 || events[0].ResourceID != "verify" || !strings.Contains(jsonString(events[0].After), `"step":2`) {
		t.Errorf("audit = %+v, want one entry naming verify and step 2", events)
	}
}

// Puppet runs are not tasks.
func TestTaskPermission_AllowlistsDoNotLimitPuppetRuns(t *testing.T) {
	h := permissionHarness(t)
	allowing(t, h, taskscope.Grant{Tasks: []string{"nothing::allowed"}, Groups: []int64{3}})

	if rec := h.post(t, h.handlers.triggerRun, map[string]any{"targets": []string{"web1"}}); rec.Code != http.StatusOK {
		t.Errorf("a run = %d %s", rec.Code, rec.Body)
	}
	rec := h.post(t, h.handlers.triggerPlan, map[string]any{"name": "runs", "targets": []string{"web1"}, "steps": []map[string]any{{"kind": "run"}, {"kind": "run"}}})
	if rec.Code != http.StatusOK {
		t.Errorf("a plan of runs = %d %s", rec.Code, rec.Body)
	}
}

// "Nodes outside a step's task scope".
func TestTaskPermission_APlanNamingANodeOutsideAStepsScope(t *testing.T) {
	h := permissionHarness(t)
	allowing(t, h,
		taskscope.Grant{Tasks: []string{"mod::echo"}, Groups: []int64{3}},
		taskscope.Grant{Tasks: []string{"verify"}, Groups: []int64{2}},
	)
	before := h.jobCount(t)
	rec := h.post(t, h.handlers.triggerPlan, map[string]any{
		"name": "named", "targets": []string{"web1", "db1"},
		"steps": []map[string]any{{"kind": "task", "taskName": "mod::echo"}, {"kind": "task", "taskName": "verify"}},
	})
	body := errorBody(t, rec)
	if rec.Code != http.StatusForbidden || body["step"] != float64(2) || !reflect.DeepEqual(refusal(t, rec).OutOfScope, []string{"web1"}) {
		t.Errorf("response = %d %s, want 403 naming step 2 and web1", rec.Code, rec.Body)
	}
	if h.jobCount(t) != before {
		t.Error("a refused plan created jobs")
	}
}

// "A query is narrowed for every step": every step of the plan runs on
// the same nodes, the ones every task step may run on.
func TestTaskPermission_APlanIsNarrowedToTheNodesEveryStepMayRunOn(t *testing.T) {
	h := permissionHarness(t)
	allowing(t, h,
		taskscope.Grant{Tasks: []string{"mod::echo"}, Groups: []int64{3}}, // both tiers
		taskscope.Grant{Tasks: []string{"verify"}, Groups: []int64{1}},    // web only
	)
	plan := decodeJob(t, h.post(t, h.handlers.triggerPlan, map[string]any{
		"name": "narrowed", "group": 9,
		"steps": []map[string]any{{"kind": "task", "taskName": "mod::echo"}, {"kind": "run"}, {"kind": "task", "taskName": "verify"}},
	}))
	waitForJobStatus(t, h.store, plan.ID, StatusSucceeded)
	steps, err := h.store.GetPlanSteps(context.Background(), plan.ID)
	if err != nil || len(steps) != 3 {
		t.Fatalf("steps = %v, %v", steps, err)
	}
	for i, step := range steps {
		if got := jobTargets(step); !reflect.DeepEqual(got, []string{"web1"}) {
			t.Errorf("step %d runs on %v, want the web node only", i+1, got)
		}
	}
}

// The two-role example from the design: the pairing is what stops a
// narrow role on one tier being widened by a broad role on another.
func TestTaskPermission_TwoRolesDoNotWidenEachOther(t *testing.T) {
	h := permissionHarness(t)
	allowing(t, h,
		taskscope.Grant{Tasks: []string{"mod::echo"}, Groups: []int64{1}},
		taskscope.Grant{Tasks: []string{taskscope.Everything}, Groups: []int64{2}},
	)
	if rec := h.post(t, h.handlers.triggerTask, map[string]any{"targets": []string{"db1"}, "task": "verify"}); rec.Code != http.StatusOK {
		t.Errorf("anything on the database tier = %d %s", rec.Code, rec.Body)
	}
	if rec := h.post(t, h.handlers.triggerTask, map[string]any{"targets": []string{"web1"}, "task": "verify"}); rec.Code != http.StatusForbidden {
		t.Errorf("a task one role does not allow on the web tier = %d %s", rec.Code, rec.Body)
	}
}

// An unrestricted caller, and a Starter given no scope at all.
func TestTaskPermission_UnrestrictedAndTheZeroScope(t *testing.T) {
	h := permissionHarness(t)
	// The harness default is unrestricted: every existing caller keeps
	// working without being given a scope.
	if rec := h.post(t, h.handlers.triggerTask, map[string]any{"targets": []string{"web1"}, "task": "verify"}); rec.Code != http.StatusOK {
		t.Errorf("an unrestricted caller = %d %s", rec.Code, rec.Body)
	}

	// Code that builds a request and forgets to say what its caller may
	// run is refused: it fails closed.
	_, err := h.handlers.Starter().StartTask(context.Background(), TaskRequest{
		StartRequest: StartRequest{Target: Target{Nodes: []string{"web1"}}, Scope: nodescope.Everything(), Actor: "x"},
		Task:         "verify",
	})
	var denied *TaskNotPermittedError
	if !asError(err, &denied) || denied.Task != "verify" {
		t.Errorf("a request with no task scope = %v, want TaskNotPermittedError", err)
	}
}

func asError[T error](err error, target *T) bool {
	for err != nil {
		if e, ok := err.(T); ok {
			*target = e
			return true
		}
		u, ok := err.(interface{ Unwrap() error })
		if !ok {
			return false
		}
		err = u.Unwrap()
	}
	return false
}

// A saved plan is checked by name only, as there is no target.
func TestTaskPermission_APlanTemplateIsCheckedByName(t *testing.T) {
	s := &Starter{catalog: fakeCatalog{}, targets: NewTargetResolver(nil, nil)}
	scope, _ := taskscope.Resolve(context.Background(), []taskscope.Grant{{Tasks: []string{"mod::echo"}, Groups: []int64{1}}}, tierNodes(tierGroups))

	if _, err := s.CheckPlanTemplate(context.Background(), "ok", []PlanStep{{Kind: JobKindRun}, {Kind: JobKindTask, TaskName: "mod::echo"}}, scope); err != nil {
		t.Errorf("an allowed template = %v", err)
	}
	_, err := s.CheckPlanTemplate(context.Background(), "no", []PlanStep{{Kind: JobKindRun}, {Kind: JobKindTask, TaskName: "verify"}}, scope)
	var step *PlanStepError
	var denied *TaskNotPermittedError
	if !asError(err, &step) || step.Step != 2 || !asError(err, &denied) || denied.Task != "verify" {
		t.Errorf("a template with a task the caller may not run = %v, want step 2 and verify", err)
	}
}
