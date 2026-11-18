package orchestrator

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/voxpupuli/enterprise-console/internal/nodescope"
	"github.com/voxpupuli/enterprise-console/internal/pagination"
	"github.com/voxpupuli/enterprise-console/internal/taskcatalog"
	"github.com/voxpupuli/enterprise-console/internal/taskscope"
)

// okResponse answers a run and a task alike with success.
func okResponse(req requestData) ([]byte, error) {
	if req.Action == actionRunTask {
		return taskResponse(`{}`, "", 0, `{}`)(req)
	}
	return json.Marshal(wireResponse{Result: &responseData{Output: outputData{ExitCode: 0}}})
}

// sentRequests decodes what the transport was asked to send, in order.
func sentRequests(h *taskHarness) []requestData {
	var out []requestData
	for _, p := range h.transport.all() {
		var req requestData
		_ = json.Unmarshal(p, &req)
		out = append(out, req)
	}
	return out
}

func planHarness(t *testing.T, f *fakeSources, respond func(requestData) ([]byte, error)) *taskHarness {
	t.Helper()
	h := newTaskHarness(t, respond)
	if f != nil {
		h.handlers.SetTargetSources(f.query, f.group)
	}
	return h
}

func TestPlanStep_DecodesTheOldAndTheNewShape(t *testing.T) {
	var steps []PlanStep
	err := json.Unmarshal([]byte(`[
		{"kind":"run"},
		{"kind":"task","taskName":"mod::echo","params":{"message":"hi"}},
		{"kind":"run","options":{"noop":true}},
		{"kind":"task","taskName":"mod::echo","environment":"staging","timeoutSeconds":90}
	]`), &steps)
	if err != nil {
		t.Fatal(err)
	}
	if steps[1].TaskName != "mod::echo" || string(steps[1].Params) != `{"message":"hi"}` {
		t.Errorf("old task step = %+v", steps[1])
	}
	if string(steps[2].Options) != `{"noop":true}` || steps[3].Environment != "staging" || steps[3].TimeoutSeconds != 90 {
		t.Errorf("new steps = %+v, %+v", steps[2], steps[3])
	}
}

func TestPlan_RunStepOptionsAndTaskStepEnvironmentReachTheNode(t *testing.T) {
	h := planHarness(t, nil, okResponse)
	plan := decodeJob(t, h.post(t, h.handlers.triggerPlan, map[string]any{
		"name": "options-plan", "targets": []string{"a.example.com"},
		"steps": []map[string]any{
			{"kind": "run", "options": map[string]any{"noop": true}},
			{"kind": "task", "taskName": "mod::echo", "environment": "staging", "timeoutSeconds": 90},
			{"kind": "run"},
		},
	}))
	waitForJobStatus(t, h.store, plan.ID, StatusSucceeded)

	sent := sentRequests(h)
	if len(sent) != 3 {
		t.Fatalf("%d requests sent, want 3", len(sent))
	}
	if sent[0].Action != actionRunWithOptions || !strings.Contains(string(sent[0].Params), `"noop":true`) {
		t.Errorf("step 1 sent %s %s, want a no-op run", sent[0].Action, sent[0].Params)
	}
	var task taskcatalog.Resolved
	_ = json.Unmarshal(sent[1].Params, &task)
	if sent[1].Action != actionRunTask || task.Environment != "staging" || task.TimeoutSeconds != 90 {
		t.Errorf("step 2 sent %s env %q timeout %d, want the task from staging with a 90 second timeout", sent[1].Action, task.Environment, task.TimeoutSeconds)
	}
	if sent[2].Action != actionRun {
		t.Errorf("step 3 sent %s, want a plain run", sent[2].Action)
	}

	read, err := h.store.GetJobWithSteps(context.Background(), plan.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(read.Steps) != 3 || read.Steps[0].RunOptions == nil || !read.Steps[0].RunOptions.Noop || read.Steps[1].Environment != "staging" {
		t.Errorf("steps as read = %+v", read.Steps)
	}
	for i, step := range read.Steps {
		if step.State != StepSucceeded || step.StepOrder != i || step.StartedAt == nil || step.FinishedAt == nil {
			t.Errorf("step %d = state %q order %d started %v finished %v", i+1, step.State, step.StepOrder, step.StartedAt, step.FinishedAt)
		}
	}
}

func TestPlan_ByGroupAndByQuery(t *testing.T) {
	f := &fakeSources{rows: certRows("web1", "db1"), groups: map[int64][]string{7: {"web1", "web2"}}}
	h := planHarness(t, f, okResponse)
	steps := []map[string]any{{"kind": "run"}, {"kind": "run"}}

	plan := decodeJob(t, h.post(t, h.handlers.triggerPlan, map[string]any{"name": "group-plan", "group": 7, "steps": steps}))
	if plan.TargetSource == nil || plan.TargetSource.Kind != TargetKindGroup {
		t.Errorf("plan targetSource = %+v, want the group", plan.TargetSource)
	}
	waitForJobStatus(t, h.store, plan.ID, StatusSucceeded)
	stored, err := h.store.GetPlanSteps(context.Background(), plan.ID)
	if err != nil || len(stored) != 2 {
		t.Fatalf("steps: %v, %v", stored, err)
	}
	for _, step := range stored {
		if got := jobTargets(step); !reflect.DeepEqual(got, []string{"web1", "web2"}) {
			t.Errorf("step targets = %v, want the group's nodes on every step", got)
		}
	}
	h.mu.Lock()
	activity := strings.Join(h.activity, "\n")
	h.mu.Unlock()
	if !strings.Contains(activity, `plan.triggered triggered plan "group-plan" (2 steps) against web1, web2`) {
		t.Errorf("activity = %s", activity)
	}

	// A scoped caller's query is narrowed; a named node outside scope
	// refuses the plan whole.
	h.handlers.SetScope(scopedTo("web1", "web2"))
	narrowed := decodeJob(t, h.post(t, h.handlers.triggerPlan, map[string]any{"name": "query-plan", "query": "nodes[certname] { }", "steps": steps}))
	stored, _ = h.store.GetPlanSteps(context.Background(), narrowed.ID)
	if got := jobTargets(stored[0]); !reflect.DeepEqual(got, []string{"web1"}) {
		t.Errorf("narrowed targets = %v, want web1 only", got)
	}
	before := h.jobCount(t)
	named := h.post(t, h.handlers.triggerPlan, map[string]any{"name": "named", "targets": []string{"web1", "db1"}, "steps": steps})
	if named.Code != http.StatusForbidden || !reflect.DeepEqual(refusal(t, named).OutOfScope, []string{"db1"}) {
		t.Errorf("named node outside scope = %d %s", named.Code, named.Body)
	}
	sub := h.post(t, h.handlers.triggerPlan, map[string]any{"name": "sub", "query": `nodes[certname] { facts { name = "role" } }`, "steps": steps})
	if sub.Code != http.StatusBadRequest || !strings.Contains(sub.Body.String(), "subqueries") {
		t.Errorf("subquery = %d %s", sub.Code, sub.Body)
	}
	if after := h.jobCount(t); after != before {
		t.Errorf("refused plans created %d jobs", after-before)
	}
}

func TestPlan_RefusedWholeNamingTheStep(t *testing.T) {
	h := planHarness(t, nil, okResponse)
	target := []string{"a.example.com"}
	many := make([]map[string]any, MaxPlanSteps+1)
	for i := range many {
		many[i] = map[string]any{"kind": "run"}
	}
	cases := []struct {
		name    string
		steps   any
		mention string
		step    float64
	}{
		{"an unknown task third", []map[string]any{{"kind": "run"}, {"kind": "run"}, {"kind": "task", "taskName": "mod::nope"}}, "step 3 (mod::nope)", 3},
		{"a bad parameter", []map[string]any{{"kind": "task", "taskName": "mod::echo", "params": map[string]any{"count": "many"}}}, "step 1", 1},
		{"an unknown run option", []map[string]any{{"kind": "run"}, {"kind": "run", "options": map[string]any{"trace": true}}}, "step 2 (run)", 2},
		{"a required sensitive value left out", []map[string]any{{"kind": "run"}, {"kind": "task", "taskName": "mod::secret", "params": map[string]any{"user": "u"}}}, "step 2 (mod::secret)", 2},
		{"a task field on a run step", []map[string]any{{"kind": "run", "taskName": "mod::echo"}}, "step 1", 1},
		{"options on a task step", []map[string]any{{"kind": "task", "taskName": "mod::echo", "options": map[string]any{"noop": true}}}, "step 1", 1},
		{"an unknown kind", []map[string]any{{"kind": "plan"}}, "step 1", 1},
		{"no steps", []map[string]any{}, "at least one step", 0},
		{"too many steps", many, "at most 20 steps", 0},
	}
	for _, tc := range cases {
		rec := h.post(t, h.handlers.triggerPlan, map[string]any{"name": "refused", "targets": target, "steps": tc.steps})
		var body map[string]any
		_ = json.Unmarshal(rec.Body.Bytes(), &body)
		if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), tc.mention) {
			t.Errorf("%s: %d %s, want 400 mentioning %q", tc.name, rec.Code, rec.Body, tc.mention)
		}
		if tc.step != 0 && body["step"] != tc.step {
			t.Errorf("%s: step = %v, want %v", tc.name, body["step"], tc.step)
		}
	}
	// A bad parameter still carries the field map a form reads.
	rec := h.post(t, h.handlers.triggerPlan, map[string]any{"name": "refused", "targets": target,
		"steps": []map[string]any{{"kind": "task", "taskName": "mod::echo", "params": map[string]any{"count": "many"}}}})
	var body struct {
		Fields map[string]string `json:"fields"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	if body.Fields["count"] == "" {
		t.Errorf("fields = %v, want count", body.Fields)
	}
	if n := h.jobCount(t); n != 0 {
		t.Errorf("%d jobs created by refused plans", n)
	}
	if len(h.transport.all()) != 0 {
		t.Error("something was sent for a refused plan")
	}
}

func TestPlan_AFailedStepClosesTheRest(t *testing.T) {
	var calls atomic.Int32
	h := planHarness(t, nil, func(req requestData) ([]byte, error) {
		// The second request is step 2 on its first node: it fails.
		if calls.Add(1) == 2 {
			return json.Marshal(wireResponse{Result: &responseData{Output: outputData{ExitCode: 4}}})
		}
		return okResponse(req)
	})
	plan := decodeJob(t, h.post(t, h.handlers.triggerPlan, map[string]any{
		"name": "fails-at-two", "targets": []string{"a.example.com"},
		"steps": []map[string]any{{"kind": "run"}, {"kind": "run"}, {"kind": "task", "taskName": "mod::echo"}, {"kind": "run"}},
	}))
	waitForJobStatus(t, h.store, plan.ID, StatusFailed)

	read, err := h.store.GetJobWithSteps(context.Background(), plan.ID)
	if err != nil {
		t.Fatal(err)
	}
	var states []string
	for _, step := range read.Steps {
		states = append(states, step.State)
	}
	if want := []string{StepSucceeded, StepFailed, StepNotRun, StepNotRun}; !reflect.DeepEqual(states, want) {
		t.Fatalf("states = %v, want %v", states, want)
	}
	third := read.Steps[2]
	if third.Targets[0].ErrorDetail != "not run: step 2 failed" || third.StartedAt != nil || third.FinishedAt != nil {
		t.Errorf("closed step = %+v", third)
	}
	if got := len(h.transport.all()); got != 2 {
		t.Errorf("%d requests sent, want 2: nothing goes to a node for a closed step", got)
	}

	// No step is left open.
	steps, _ := h.store.GetPlanSteps(context.Background(), plan.ID)
	for i, step := range steps {
		if step.Status == StatusRunning || step.FinishedAt == nil {
			t.Errorf("step %d left open: %s", i+1, step.Status)
		}
		for _, target := range step.Targets {
			if target.Status == StatusPending || target.Status == StatusRunning {
				t.Errorf("step %d node %s left %s", i+1, target.Certname, target.Status)
			}
		}
	}
	h.mu.Lock()
	activity := strings.Join(h.activity, "\n")
	h.mu.Unlock()
	if !strings.Contains(activity, "plan.failed") || !strings.Contains(activity, "step 2 failed") {
		t.Errorf("activity = %s", activity)
	}
}

func TestPlan_StepStatesWhileRunning(t *testing.T) {
	plan := Job{Kind: JobKindPlan, Status: StatusRunning}
	done := plan.StartedAt.Add(60e9)
	steps := []Job{
		{ID: 1, Kind: JobKindRun, Status: StatusSucceeded, FinishedAt: &done},
		{ID: 2, Kind: JobKindRun, Status: StatusRunning},
		{ID: 3, Kind: JobKindRun, Status: StatusRunning},
	}
	views := stepViews(plan, steps)
	if views[0].State != StepSucceeded || views[1].State != StepRunning || views[2].State != StepWaiting {
		t.Errorf("states = %s, %s, %s", views[0].State, views[1].State, views[2].State)
	}
	if views[1].StartedAt == nil || !views[1].StartedAt.Equal(done) || views[2].StartedAt != nil {
		t.Errorf("a step starts when the one before it finishes: %v, %v", views[1].StartedAt, views[2].StartedAt)
	}
}

func TestPlan_ReadWithStepsListedOnce(t *testing.T) {
	h := planHarness(t, nil, okResponse)
	plan := decodeJob(t, h.post(t, h.handlers.triggerPlan, map[string]any{
		"name": "three-steps", "targets": []string{"a.example.com", "b.example.com"},
		"steps": []map[string]any{{"kind": "run"}, {"kind": "task", "taskName": "mod::echo"}, {"kind": "run"}},
	}))
	waitForJobStatus(t, h.store, plan.ID, StatusSucceeded)

	get := func(id int64) Job {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.SetPathValue("id", strconv.FormatInt(id, 10))
		rec := httptest.NewRecorder()
		h.handlers.getJob(rec, req)
		return decodeJob(t, rec)
	}
	read := get(plan.ID)
	if len(read.Steps) != 3 || read.Steps[1].Kind != JobKindTask || read.Steps[1].TaskName != "mod::echo" || len(read.Steps[0].Targets) != 2 {
		t.Fatalf("steps = %+v", read.Steps)
	}

	list := func(query string) pagination.Page[Job] {
		rec := httptest.NewRecorder()
		h.handlers.listJobs(rec, httptest.NewRequest(http.MethodGet, "/?triggeredBy="+h.actor+query, nil))
		var page pagination.Page[Job]
		if err := json.Unmarshal(rec.Body.Bytes(), &page); err != nil {
			t.Fatal(err)
		}
		return page
	}
	if page := list(""); page.Total != 1 || page.Items[0].ID != plan.ID {
		t.Errorf("job list = %d entries, want the plan alone", page.Total)
	} else if listed := page.Items[0]; listed.TargetCount != 2 || !reflect.DeepEqual(listed.TargetPreview, []string{"a.example.com", "b.example.com"}) {
		t.Errorf("listed plan shows %d nodes %v, want its steps' two", listed.TargetCount, listed.TargetPreview)
	}
	if page := list("&parent=" + strconv.FormatInt(plan.ID, 10)); page.Total != 3 {
		t.Errorf("parent filter = %d entries, want the three steps", page.Total)
	}

	step := get(read.Steps[1].ID)
	if step.Plan == nil || step.Plan.ID != plan.ID || step.Plan.Name != "three-steps" || step.Plan.Steps != 3 || step.StepOrder == nil || *step.StepOrder != 1 {
		t.Errorf("step read = plan %+v order %v", step.Plan, step.StepOrder)
	}

	// A scoped reader sees each step with their nodes only.
	h.handlers.SetScope(scopedTo("a.example.com"))
	scoped := get(plan.ID)
	if !scoped.TargetsHidden || len(scoped.Steps) != 3 {
		t.Fatalf("scoped plan = hidden %v, %d steps", scoped.TargetsHidden, len(scoped.Steps))
	}
	for _, s := range scoped.Steps {
		if len(s.Targets) != 1 || s.Targets[0].Certname != "a.example.com" {
			t.Errorf("scoped step targets = %+v", s.Targets)
		}
	}
	h.handlers.SetScope(nodescope.Unscoped)
}

func TestCheckPlanTemplate(t *testing.T) {
	s := &Starter{catalog: fakeCatalog{}, targets: NewTargetResolver(nil, nil)}
	ctx := context.Background()

	stored, err := s.CheckPlanTemplate(ctx, "kept", []PlanStep{
		{Kind: JobKindRun, Options: json.RawMessage(`{"noop":true}`)},
		{Kind: JobKindTask, TaskName: "mod::secret", Params: json.RawMessage(`{"user":"deploy","password":"hunter2"}`)},
		{Kind: JobKindTask, TaskName: "mod::secret", Params: json.RawMessage(`{"user":"deploy"}`)},
		{Kind: JobKindRun},
	}, taskscope.Unrestricted())
	if err != nil {
		t.Fatalf("CheckPlanTemplate() error: %v", err)
	}
	encoded, _ := json.Marshal(stored)
	if strings.Contains(string(encoded), "hunter2") || strings.Contains(string(encoded), "password") {
		t.Errorf("a sensitive value was kept: %s", encoded)
	}
	if string(stored[0].Options) != `{"noop":true}` || string(stored[1].Params) != `{"user":"deploy"}` || stored[1].Environment != "production" || len(stored[3].Options) != 0 {
		t.Errorf("stored steps = %s", encoded)
	}

	needs, problem := s.StepNeeds(ctx, stored[1])
	if problem != "" || !reflect.DeepEqual(needs, []SensitiveNeed{{Param: "password", Required: true}}) {
		t.Errorf("needs = %+v, problem %q", needs, problem)
	}
	if needs, problem := s.StepNeeds(ctx, PlanStep{Kind: JobKindTask, TaskName: "mod::gone"}); problem == "" || needs != nil {
		t.Errorf("a task that has gone: needs %v problem %q", needs, problem)
	}
	if needs, problem := s.StepNeeds(ctx, stored[0]); needs != nil || problem != "" {
		t.Errorf("a run step needs nothing: %v %q", needs, problem)
	}

	for name, tc := range map[string]struct {
		steps   []PlanStep
		step    int
		mention string
	}{
		"an unknown task":        {[]PlanStep{{Kind: JobKindRun}, {Kind: JobKindTask, TaskName: "mod::nope"}}, 2, "mod::nope"},
		"a wrong parameter type": {[]PlanStep{{Kind: JobKindTask, TaskName: "mod::echo", Params: json.RawMessage(`{"count":"x"}`)}}, 1, "count"},
		"a missing plain value":  {[]PlanStep{{Kind: JobKindTask, TaskName: "mod::secret"}}, 1, "user"},
		"an unknown environment": {[]PlanStep{{Kind: JobKindTask, TaskName: "mod::echo", Environment: "nowhere"}}, 1, "nowhere"},
		"a bad timeout":          {[]PlanStep{{Kind: JobKindTask, TaskName: "mod::echo", TimeoutSeconds: -1}}, 1, "timeoutSeconds"},
		"an invalid run option":  {[]PlanStep{{Kind: JobKindRun, Options: json.RawMessage(`{"environment":"bad name"}`)}}, 1, "environment"},
	} {
		_, err := s.CheckPlanTemplate(ctx, "refused", tc.steps, taskscope.Unrestricted())
		var stepErr *PlanStepError
		if !errors.As(err, &stepErr) || stepErr.Step != tc.step || !strings.Contains(err.Error(), tc.mention) {
			t.Errorf("%s: err = %v, want step %d mentioning %q", name, err, tc.step, tc.mention)
		}
		if _, body := RefusalResponse(err); body["step"] != tc.step {
			t.Errorf("%s: refusal body = %v", name, body)
		}
	}
	if _, err := s.CheckPlanTemplate(ctx, strings.Repeat("n", MaxPlanNameLength+1), []PlanStep{{Kind: JobKindRun}}, taskscope.Unrestricted()); !IsPlanShapeError(err) {
		t.Errorf("a long name: err = %v", err)
	}
}

func TestCheckPlan_ReturnsWhatEachStepHolds(t *testing.T) {
	s := &Starter{catalog: fakeCatalog{}, targets: NewTargetResolver(nil, nil)}
	checks, err := s.CheckPlan(context.Background(), PlanRequest{
		StartRequest: StartRequest{Target: Target{Nodes: []string{"a.example.com"}}, Scope: nodescope.Everything(), Tasks: taskscope.Unrestricted()},
		Name:         "checked",
		Steps: []PlanStep{
			{Kind: JobKindRun},
			{Kind: JobKindTask, TaskName: "mod::secret", Params: json.RawMessage(`{"user":"u","password":"s3cret"}`)},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(checks) != 2 || checks[0].Sensitive != nil || checks[1].Sensitive["password"] != "s3cret" || strings.Contains(string(checks[1].StoredParams), "s3cret") {
		t.Errorf("checks = %+v", checks)
	}
}
