package codeplans

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/voxpupuli/enterprise-console/internal/auditlog"
	"github.com/voxpupuli/enterprise-console/internal/nodescope"
	"github.com/voxpupuli/enterprise-console/internal/orchestrator"
	"github.com/voxpupuli/enterprise-console/internal/taskcatalog"
	"github.com/voxpupuli/enterprise-console/internal/taskscope"
	"github.com/voxpupuli/enterprise-console/internal/testdb"
)

// memSource serves plans from memory.
type memSource map[string]string

func (m memSource) File(_ string, name string) (File, error) {
	src, ok := m[name]
	if !ok {
		return File{}, ErrPlanNotFound
	}
	return File{Name: name, Data: []byte(src)}, nil
}

type catalog struct{}

func taskFile() taskcatalog.File {
	var f taskcatalog.File
	f.Filename = "echo.sh"
	f.SHA256 = strings.Repeat("a", 64)
	f.SizeBytes = 10
	f.URI.Path = "/puppet/v3/file_content/tasks/mod/echo.sh"
	return f
}

func (catalog) Task(_ context.Context, environment, name string) (taskcatalog.Task, error) {
	switch name {
	case "mod::echo", "mod::other":
		return taskcatalog.Task{Name: name, Environment: environment, Files: []taskcatalog.File{taskFile()},
			Metadata: taskcatalog.Metadata{Parameters: map[string]taskcatalog.Parameter{
				"message": {Type: "Optional[String]"}, "count": {Type: "Optional[Integer]"},
			}}}, nil
	case "mod::secret":
		return taskcatalog.Task{Name: name, Environment: environment, Files: []taskcatalog.File{taskFile()},
			Metadata: taskcatalog.Metadata{Parameters: map[string]taskcatalog.Parameter{
				"password": {Type: "String[1]", Sensitive: true},
			}}}, nil
	}
	return taskcatalog.Task{}, &taskcatalog.NotFoundError{Kind: "task", Name: name}
}

// transport answers every task: success with {"on": node, "params": ...},
// or failure for a node named bad-*; it can also block.
type transport struct {
	mu     sync.Mutex
	asked  []string
	params map[string]map[string]any // node -> last params seen
	hold   chan struct{}
}

func (tr *transport) Dispatch(ctx context.Context, certname string, payload []byte, _ time.Duration) ([]byte, error) {
	var req struct {
		Params struct {
			Task   string         `json:"task"`
			Params map[string]any `json:"params"`
		} `json:"params"`
	}
	_ = json.Unmarshal(payload, &req)
	tr.mu.Lock()
	tr.asked = append(tr.asked, certname)
	if tr.params == nil {
		tr.params = map[string]map[string]any{}
	}
	tr.params[certname] = req.Params.Params
	hold := tr.hold
	tr.mu.Unlock()
	if hold != nil {
		select {
		case <-hold:
		case <-ctx.Done():
		}
	}
	// A task that prints its own sensitive parameter is recorded as it
	// printed it; this one does not.
	shown := req.Params.Params
	if req.Params.Task == "mod::secret" {
		shown = nil
	}
	exit, result := 0, fmt.Sprintf(`{"on":%q,"task":%q,"params":%s}`, certname, req.Params.Task, mustJSON(shown))
	if strings.HasPrefix(certname, "bad-") {
		exit, result = 1, `{"_error":{"msg":"it broke"}}`
	}
	return json.Marshal(map[string]any{"result": map[string]any{
		"output": map[string]any{"stdout": result, "stderr": "", "exitcode": exit}, "result": json.RawMessage(result)}})
}

func mustJSON(v any) string { b, _ := json.Marshal(v); return b2s(b) }
func b2s(b []byte) string   { return string(bytes.TrimSpace(b)) }

func (tr *transport) askedNodes() []string {
	tr.mu.Lock()
	defer tr.mu.Unlock()
	return slices.Clone(tr.asked)
}

type rig struct {
	t      *testing.T
	store  *orchestrator.Store
	tr     *transport
	co     *Coordinator
	src    memSource
	mu     sync.Mutex
	audits []auditlog.Event
	acts   []string
	actor  string
}

func newRig(t *testing.T, plans memSource) *rig {
	t.Helper()
	pool := testdb.Pool(t)
	store := orchestrator.NewStore(pool)
	tr := &transport{}
	d := orchestrator.NewDispatcher(store, tr, nil)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go d.Run(ctx)
	h := orchestrator.NewHandlers(store, d, func(*http.Request) string { return "cp" }, func(*http.Request, auditlog.Event) {})
	h.SetTaskCatalog(catalog{})
	r := &rig{t: t, store: store, tr: tr, src: plans, actor: "cp-" + time.Now().Format("150405.000000000")}
	r.co = NewCoordinator(plans, h.Starter(), store, nil)
	r.co.pollEvery = 10 * time.Millisecond
	r.co.SetAuditRecorder(func(e auditlog.Event) { r.mu.Lock(); r.audits = append(r.audits, e); r.mu.Unlock() })
	r.co.SetActivityRecorder(func(a, _, s string) { r.mu.Lock(); r.acts = append(r.acts, a); r.mu.Unlock() })
	t.Cleanup(r.co.Wait)
	return r
}

var _ = httptest.NewRecorder

func (r *rig) request(plan string, params map[string]any) Request {
	return Request{Plan: plan, Environment: "production", Parameters: params,
		Start: orchestrator.StartRequest{Scope: nodescope.Everything(), Tasks: taskscope.Unrestricted(), Actor: r.actor}}
}

// start runs a plan to its end and returns the finished job.
func (r *rig) start(req Request) (orchestrator.Job, error) {
	r.t.Helper()
	job, err := r.co.Start(context.Background(), req)
	if err != nil {
		return orchestrator.Job{}, err
	}
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		got, gerr := r.store.GetJob(context.Background(), job.ID)
		if gerr != nil {
			r.t.Fatal(gerr)
		}
		if got.Status != orchestrator.StatusRunning {
			return got, nil
		}
		time.Sleep(15 * time.Millisecond)
	}
	r.t.Fatal("the plan never finished")
	return orchestrator.Job{}, nil
}

func uniq(p string) string { return fmt.Sprintf("%s-%d", p, time.Now().UnixNano()) }

func states(j orchestrator.Job) string {
	var s []string
	for _, e := range j.Trace {
		s = append(s, e.Path+":"+e.State)
	}
	return strings.Join(s, " ")
}

func TestRun_AResultFeedsTheNextStep(t *testing.T) {
	n1, n2 := uniq("rf-a"), uniq("rf-b")
	r := newRig(t, memSource{"ops::demo": `
parameters:
  targets: {type: TargetSpec}
  msg: {type: String, default: hello}
steps:
  - name: first
    task: mod::echo
    targets: $targets
    parameters: {message: $msg, count: 2}
  - name: second
    task: mod::other
    targets: $first.ok_targets
    parameters:
      message: "saw ${first.results[0].value.on}"
  - message: "ran on ${first.targets} ok=${first.ok}"
return:
  count: $first.results
  targets: $second.targets
`})
	job, err := r.start(r.request("ops::demo", map[string]any{"targets": []any{n1, n2}}))
	if err != nil {
		t.Fatal(err)
	}
	if job.Status != orchestrator.StatusSucceeded {
		t.Fatalf("job = %s: %s (%s)", job.Status, job.Error, states(job))
	}
	if states(job) != "1:succeeded 2:succeeded 3:succeeded" {
		t.Errorf("trace = %s", states(job))
	}
	// The first step's params reached the nodes, and the second saw the first's result.
	if got := r.tr.params[n1]["message"]; got != "saw "+n1 {
		t.Errorf("second step's message = %v", got)
	}
	if got := job.Trace[2].Message; got != fmt.Sprintf(`ran on ["%s","%s"] ok=true`, n1, n2) {
		t.Errorf("message = %q", got)
	}
	var result map[string]any
	_ = json.Unmarshal(job.Result, &result)
	if res := result["targets"].([]any); len(res) != 2 || len(result["count"].([]any)) != 2 {
		t.Errorf("result = %s", job.Result)
	}
	// Step jobs are children, in order, and the first one's targets are the nodes.
	if job.Trace[0].JobID == nil || job.Trace[1].JobID == nil {
		t.Fatalf("trace = %+v", job.Trace)
	}
	c1, _ := r.store.GetJob(context.Background(), *job.Trace[0].JobID)
	if c1.ParentJobID == nil || *c1.ParentJobID != job.ID || *c1.StepOrder != 1 || c1.Kind != orchestrator.JobKindTask {
		t.Errorf("step job = %+v", c1)
	}
	// The record: parameters stored, audited, in the activity log.
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.audits) != 2 || r.audits[0].Action != "codeplan.triggered" || r.audits[1].Action != "codeplan.succeeded" {
		t.Errorf("audits = %+v", r.audits)
	}
	if !slices.Equal(r.acts, []string{"codeplan.triggered", "codeplan.succeeded"}) {
		t.Errorf("activity = %v", r.acts)
	}
}

func TestRun_AFailedStepEndsThePlanAndLaterStepsAreNotRun(t *testing.T) {
	good, bad := uniq("fs-good"), uniq("bad-fs")
	r := newRig(t, memSource{"ops::demo": `
parameters:
  targets: {type: TargetSpec}
steps:
  - name: a
    task: mod::echo
    targets: $targets
  - name: b
    task: mod::other
    targets: $targets
  - message: never
`})
	job, err := r.start(r.request("ops::demo", map[string]any{"targets": []any{good, bad}}))
	if err != nil {
		t.Fatal(err)
	}
	if job.Status != orchestrator.StatusFailed || !strings.Contains(job.Error, bad) || states(job) != "1:failed 2:not_run 3:not_run" {
		t.Errorf("job = %s %q %s", job.Status, job.Error, states(job))
	}
	// Nothing more was sent: each node was asked once, by step 1 only.
	if got := r.tr.askedNodes(); len(got) != 2 {
		t.Errorf("the nodes were asked %v", got)
	}
}

func TestRun_ASensitiveParameterNeverReachesARecord(t *testing.T) {
	n := uniq("sens")
	r := newRig(t, memSource{"ops::demo": `
parameters:
  targets: {type: TargetSpec}
  token: {type: "Sensitive[String]"}
steps:
  - name: use
    task: mod::secret
    targets: $targets
    parameters: {password: $token}
  - message: "the token is ${token}"
return: {t: $token, ok: $use.ok}
`})
	const secret = "hunter2-Value"
	job, err := r.start(r.request("ops::demo", map[string]any{"targets": []any{n}, "token": secret}))
	if err != nil {
		t.Fatal(err)
	}
	if job.Status != orchestrator.StatusSucceeded {
		t.Fatalf("job = %s %s", job.Status, job.Error)
	}
	// The task got the real value; no record has it.
	if got := r.tr.params[n]["password"]; got != secret {
		t.Errorf("the task received %v", got)
	}
	var stored []string
	b, _ := json.Marshal(job)
	stored = append(stored, string(b))
	child, _ := r.store.GetJob(context.Background(), *job.Trace[0].JobID)
	cb, _ := json.Marshal(child)
	stored = append(stored, string(cb))
	r.mu.Lock()
	ab, _ := json.Marshal(r.audits)
	r.mu.Unlock()
	stored = append(stored, string(ab))
	for i, s := range stored {
		if strings.Contains(s, secret) {
			t.Errorf("record %d holds the secret: %s", i, s)
		}
	}
	if job.Trace[1].Message != "the token is [redacted]" || !strings.Contains(string(job.Result), Redacted) {
		t.Errorf("message %q result %s", job.Trace[1].Message, job.Result)
	}
	if !strings.Contains(string(job.Params), Redacted) {
		t.Errorf("stored parameters = %s", job.Params)
	}
}

func TestRun_StepsAreCheckedAsTheyAreReached(t *testing.T) {
	mine, theirs := uniq("sc-mine"), uniq("sc-theirs")
	r := newRig(t, memSource{"ops::demo": `
parameters:
  targets: {type: TargetSpec}
steps:
  - name: a
    task: mod::echo
    targets: $targets
  - name: b
    task: mod::echo
    targets: [` + theirs + `]
`})
	req := r.request("ops::demo", map[string]any{"targets": []any{mine}})
	req.Start.Scope = nodescope.Of(mine)
	job, err := r.start(req)
	if err != nil {
		t.Fatal(err)
	}
	if job.Status != orchestrator.StatusFailed || states(job) != "1:succeeded 2:failed" || !strings.Contains(job.Error, "step 2") || !strings.Contains(job.Error, theirs) {
		t.Errorf("job = %s %q %s", job.Status, job.Error, states(job))
	}
	if slices.Contains(r.tr.askedNodes(), theirs) {
		t.Error("a node outside the caller's groups was sent a task")
	}
}

func TestStart_RefusalsBeforeAnythingStarts(t *testing.T) {
	mine, theirs := uniq("rs-mine"), uniq("rs-theirs")
	r := newRig(t, memSource{
		"ops::demo":         "parameters:\n  targets: {type: TargetSpec}\nsteps:\n  - name: a\n    task: mod::echo\n    targets: $targets\n",
		"ops::notask":       "steps:\n  - task: mod::nope\n    targets: [a]\n",
		"ops::broken":       "steps:\n  - command: whoami\n    targets: [a]\n",
		"ops::loop1":        "steps:\n  - plan: ops::loop2\n",
		"ops::loop2":        "steps:\n  - plan: ops::loop1\n",
		"ops::calls_broken": "steps:\n  - plan: ops::broken\n",
	})
	cases := map[string]struct {
		req  Request
		want string
	}{
		"a plan that does not exist": {r.request("ops::missing", nil), "was not found"},
		"an unusable plan":           {r.request("ops::broken", nil), "commands are not supported"},
		"an unusable plan it calls":  {r.request("ops::calls_broken", nil), "commands are not supported"},
		"a task that does not exist": {r.request("ops::notask", nil), "step 1"},
		"plans that call each other": {r.request("ops::loop1", nil), "circle"},
		"a missing parameter":        {r.request("ops::demo", nil), "targets is required"},
		"a parameter the plan lacks": {r.request("ops::demo", map[string]any{"targets": []any{mine}, "extra": 1}), "extra"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			before := len(r.tr.askedNodes())
			_, err := r.co.Start(context.Background(), c.req)
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("err = %v, want %q", err, c.want)
			}
			if len(r.tr.askedNodes()) != before {
				t.Error("something was sent")
			}
		})
	}
	// A node outside the caller's groups, named as a target, refuses the request.
	req := r.request("ops::demo", map[string]any{"targets": []any{mine, theirs}})
	req.Start.Scope = nodescope.Of(mine)
	_, err := r.co.Start(context.Background(), req)
	var out *orchestrator.OutOfScopeError
	if !asErr(err, &out) || len(out.Outside) != 1 || out.Outside[0] != theirs {
		t.Errorf("out of scope: %v", err)
	}
	// A task the caller's roles do not allow is refused, naming the step.
	scope, _ := taskscope.Resolve(context.Background(), []taskscope.Grant{{Tasks: []string{"mod::other"}, Fleet: true}}, nil)
	req = r.request("ops::demo", map[string]any{"targets": []any{mine}})
	req.Start.Tasks = scope
	_, err = r.co.Start(context.Background(), req)
	var step *orchestrator.PlanStepError
	if !asErr(err, &step) || step.Step != 1 {
		t.Errorf("a task the roles do not allow: %v", err)
	}
}

func TestRun_ANestedPlanReturnsItsValueAndTracesItsSteps(t *testing.T) {
	n := uniq("np")
	r := newRig(t, memSource{
		"ops::outer": `
parameters:
  targets: {type: TargetSpec}
steps:
  - name: inner
    plan: ops::inner
    parameters:
      nodes: $targets
      word: hello
  - message: "inner said ${inner.said}"
return: $inner
`,
		"ops::inner": `
parameters:
  nodes: {type: TargetSpec}
  word: {type: String}
steps:
  - name: e
    task: mod::echo
    targets: $nodes
    parameters: {message: $word}
  - message: "done"
return:
  said: $e.value.params.message
  ok: $e.ok
`,
	})
	job, err := r.start(r.request("ops::outer", map[string]any{"targets": []any{n}}))
	if err != nil {
		t.Fatal(err)
	}
	if job.Status != orchestrator.StatusSucceeded {
		t.Fatalf("job = %s %q %s", job.Status, job.Error, states(job))
	}
	// The whole run is one trace, the inner plan's steps a level down.
	var rows []string
	for _, e := range job.Trace {
		rows = append(rows, fmt.Sprintf("%s/%d/%s/%s", e.Path, e.Depth, e.Plan, e.State))
	}
	if got := strings.Join(rows, " "); got != "1/0/ops::outer/succeeded 1.1/1/ops::inner/succeeded 1.2/1/ops::inner/succeeded 2/0/ops::outer/succeeded" {
		t.Errorf("trace = %s", got)
	}
	var res map[string]any
	_ = json.Unmarshal(job.Result, &res)
	if job.Trace[3].Message != "inner said hello" || res["said"] != "hello" || res["ok"] != true {
		t.Errorf("message %q result %s", job.Trace[3].Message, job.Result)
	}
	// The task step is a child of the outer plan job.
	c, _ := r.store.GetJob(context.Background(), *job.Trace[1].JobID)
	if c.ParentJobID == nil || *c.ParentJobID != job.ID {
		t.Errorf("the nested step's job has parent %v", c.ParentJobID)
	}
}

func TestRun_ASensitiveValueCannotBePassedToAnOpenParameter(t *testing.T) {
	n := uniq("sv")
	r := newRig(t, memSource{
		"ops::outer": "parameters:\n  targets: {type: TargetSpec}\n  token: {type: \"Sensitive[String]\"}\nsteps:\n  - plan: ops::inner\n    parameters: {nodes: $targets, word: $token}\n",
		"ops::inner": "parameters:\n  nodes: {type: TargetSpec}\n  word: {type: String}\nsteps:\n  - message: \"${word}\"\n",
	})
	job, err := r.start(r.request("ops::outer", map[string]any{"targets": []any{n}, "token": "topsecret1"}))
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(job)
	if job.Status != orchestrator.StatusFailed || !strings.Contains(job.Error, "not Sensitive") || strings.Contains(string(b), "topsecret1") {
		t.Errorf("job = %s %q", job.Status, job.Error)
	}
}

func TestRun_AStepWithNoTargetsIsAnEmptyResultNotAFailure(t *testing.T) {
	n := uniq("zt")
	r := newRig(t, memSource{"ops::demo": `
parameters:
  targets: {type: TargetSpec}
steps:
  - name: first
    task: mod::echo
    targets: $targets
  - name: failed_ones
    task: mod::other
    targets: $first.error_targets
  - message: "nothing failed: ${failed_ones.ok}"
`})
	job, err := r.start(r.request("ops::demo", map[string]any{"targets": []any{n}}))
	if err != nil {
		t.Fatal(err)
	}
	if job.Status != orchestrator.StatusSucceeded || job.Trace[1].JobID != nil || job.Trace[2].Message != "nothing failed: true" {
		t.Errorf("job = %s %s %q", job.Status, states(job), job.Trace[2].Message)
	}
	if got := r.tr.askedNodes(); len(got) != 1 {
		t.Errorf("asked %v", got)
	}
}

func TestStart_PlansCalledTooDeepAreRefused(t *testing.T) {
	src := memSource{}
	for i := 0; i <= MaxDepth+1; i++ {
		next := fmt.Sprintf("ops::p%d", i+1)
		body := "steps:\n  - message: end\n"
		if i <= MaxDepth {
			body = "steps:\n  - plan: " + next + "\n"
		}
		src[fmt.Sprintf("ops::p%d", i)] = body
	}
	r := newRig(t, src)
	_, err := r.co.Start(context.Background(), r.request("ops::p0", nil))
	if err == nil || !strings.Contains(err.Error(), "deep") {
		t.Errorf("err = %v", err)
	}
}

func TestRun_TheStepLimitFailsThePlan(t *testing.T) {
	var b strings.Builder
	b.WriteString("steps:\n")
	for i := 0; i < MaxSteps+5; i++ {
		b.WriteString("  - message: hi\n")
	}
	r := newRig(t, memSource{"ops::long": b.String()})
	job, err := r.start(r.request("ops::long", nil))
	if err != nil {
		t.Fatal(err)
	}
	if job.Status != orchestrator.StatusFailed || !strings.Contains(job.Error, fmt.Sprintf("limit of %d steps", MaxSteps)) {
		t.Errorf("job = %s %q", job.Status, job.Error)
	}
}

func TestRun_TheTimeLimitFailsThePlan(t *testing.T) {
	n := uniq("tl")
	r := newRig(t, memSource{"ops::slow": "parameters:\n  targets: {type: TargetSpec}\nsteps:\n  - task: mod::echo\n    targets: $targets\n"})
	r.co.maxDuration = 200 * time.Millisecond
	r.tr.hold = make(chan struct{})
	t.Cleanup(func() { close(r.tr.hold) })
	job, err := r.start(r.request("ops::slow", map[string]any{"targets": []any{n}}))
	if err != nil {
		t.Fatal(err)
	}
	if job.Status != orchestrator.StatusFailed || !strings.Contains(job.Error, "time limit") {
		t.Errorf("job = %s %q", job.Status, job.Error)
	}
}

func TestRun_ALongRunKeepsItsHeartbeat(t *testing.T) {
	n := uniq("hb")
	r := newRig(t, memSource{"ops::slow": "parameters:\n  targets: {type: TargetSpec}\nsteps:\n  - task: mod::echo\n    targets: $targets\n"})
	r.tr.hold = make(chan struct{})
	job, err := r.co.Start(context.Background(), r.request("ops::slow", map[string]any{"targets": []any{n}}))
	if err != nil {
		t.Fatal(err)
	}
	first := *job.HeartbeatAt
	time.Sleep(120 * time.Millisecond)
	mid, _ := r.store.GetJob(context.Background(), job.ID)
	if mid.Status != orchestrator.StatusRunning || mid.HeartbeatAt == nil || !mid.HeartbeatAt.After(first) {
		t.Errorf("mid-run: %s heartbeat %v (started %v)", mid.Status, mid.HeartbeatAt, first)
	}
	if len(mid.Trace) != 1 || mid.Trace[0].State != "running" || mid.Trace[0].JobID == nil {
		t.Errorf("the trace while it runs = %+v", mid.Trace)
	}
	close(r.tr.hold)
	r.co.Wait()
}

func TestStart_ATargetGroupIsResolvedOnceWithinTheCallersScope(t *testing.T) {
	in, out := uniq("tg-in"), uniq("tg-out")
	pool := testdb.Pool(t)
	store := orchestrator.NewStore(pool)
	tr := &transport{}
	d := orchestrator.NewDispatcher(store, tr, nil)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go d.Run(ctx)
	h := orchestrator.NewHandlers(store, d, func(*http.Request) string { return "cp" }, func(*http.Request, auditlog.Event) {})
	h.SetTaskCatalog(catalog{})
	h.SetTargetSources(nil, func(_ context.Context, id int64) (string, []string, error) {
		if id != 7 {
			return "", nil, orchestrator.ErrGroupNotFound
		}
		return "web", []string{in, out}, nil
	})
	co := NewCoordinator(memSource{"ops::demo": "parameters:\n  targets: {type: TargetSpec}\nsteps:\n  - name: a\n    task: mod::echo\n    targets: $targets\nreturn: $a.targets\n"}, h.Starter(), store, nil)
	co.pollEvery = 10 * time.Millisecond
	t.Cleanup(co.Wait)

	req := Request{Plan: "ops::demo", Environment: "production", Parameters: map[string]any{"targets": map[string]any{"group": json.Number("7")}},
		Start: orchestrator.StartRequest{Scope: nodescope.Of(in), Tasks: taskscope.Unrestricted(), Actor: "cp-grp"}}
	job, err := co.Start(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	co.Wait()
	got, _ := store.GetJob(context.Background(), job.ID)
	if got.Status != orchestrator.StatusSucceeded || !slices.Equal(tr.askedNodes(), []string{in}) {
		t.Errorf("job %s, asked %v: the group should have been narrowed to the caller's node", got.Status, tr.askedNodes())
	}
	// A group that matches nothing the caller may act on is refused, as is one that is gone.
	req.Start.Scope = nodescope.Of("somebody-else")
	if _, err := co.Start(context.Background(), req); err == nil || !strings.Contains(err.Error(), "matched no nodes") {
		t.Errorf("a group with none of the caller's nodes: %v", err)
	}
	req.Parameters = map[string]any{"targets": map[string]any{"group": json.Number("9")}}
	if _, err := co.Start(context.Background(), req); err == nil {
		t.Error("a group that does not exist was accepted")
	}
}
