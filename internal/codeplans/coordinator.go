package codeplans

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/voxpupuli/enterprise-console/internal/auditlog"
	"github.com/voxpupuli/enterprise-console/internal/orchestrator"
	"github.com/voxpupuli/enterprise-console/internal/taskscope"
)

// Limits on a run.
const (
	MaxSteps    = 200
	MaxDepth    = 5
	MaxDuration = 24 * time.Hour
	// MaxStepTargets bounds what one task step may name.
	MaxStepTargets = 1000
)

// TaskStarter is what a plan needs of the orchestrator: to check a task by
// name before the run, to start a step's task, and to resolve targets.
// *orchestrator.Starter is one.
type TaskStarter interface {
	StartTask(ctx context.Context, req orchestrator.TaskRequest) (orchestrator.Job, error)
	CheckTaskName(ctx context.Context, tasks taskscope.Scope, environment, name string) error
	Targets() *orchestrator.TargetResolver
}

// Jobs is the store a run is recorded in. *orchestrator.Store is one.
type Jobs interface {
	CreateCodePlanJob(ctx context.Context, name, environment string, params json.RawMessage, triggeredBy string, origin orchestrator.JobOrigin) (orchestrator.Job, error)
	SaveTrace(ctx context.Context, jobID int64, trace []orchestrator.TraceEntry) error
	FinishCodePlan(ctx context.Context, jobID int64, status string, trace []orchestrator.TraceEntry, result json.RawMessage, reason string) (bool, error)
	GetJob(ctx context.Context, id int64) (orchestrator.Job, error)
	TouchJob(ctx context.Context, jobID int64) error
}

// Source finds a plan's file. *Loader is one.
type Source interface {
	File(env, name string) (File, error)
}

// RunError is a run the console refuses before it starts.
type RunError struct{ Reason string }

func (e *RunError) Error() string { return e.Reason }

// Request is a request to run a plan.
type Request struct {
	Plan        string
	Environment string
	// Parameters are as the caller gave them, decoded with numbers as
	// json.Number.
	Parameters map[string]any
	// Start is who is asking and what they may reach: their node scope and
	// task permissions, held for the whole run.
	Start orchestrator.StartRequest
}

// Coordinator runs plans from code. A run is followed by the instance that
// started it, as a patch run is: if that instance stops, the reaper fails
// the job once its heartbeat is stale.
type Coordinator struct {
	source  Source
	starter TaskStarter
	jobs    Jobs
	logger  *slog.Logger

	recorder      func(action, actor, summary string)
	auditRecorder func(auditlog.Event)

	// pollEvery is how often a step's job is read, overridable in tests.
	pollEvery   time.Duration
	maxDuration time.Duration

	mu     sync.Mutex
	runCtx context.Context
	wg     sync.WaitGroup
}

// NewCoordinator builds a Coordinator. logger may be nil.
func NewCoordinator(source Source, starter TaskStarter, jobs Jobs, logger *slog.Logger) *Coordinator {
	if logger == nil {
		logger = slog.Default()
	}
	return &Coordinator{source: source, starter: starter, jobs: jobs, logger: logger,
		pollEvery: 500 * time.Millisecond, maxDuration: MaxDuration, runCtx: context.Background()}
}

// SetActivityRecorder records a run's start and end in the activity log.
func (c *Coordinator) SetActivityRecorder(f func(action, actor, summary string)) { c.recorder = f }

// SetAuditRecorder records a run's start and end as audit events.
func (c *Coordinator) SetAuditRecorder(f func(auditlog.Event)) { c.auditRecorder = f }

// Run holds ctx as the context every run is followed under.
func (c *Coordinator) Run(ctx context.Context) {
	c.mu.Lock()
	c.runCtx = ctx
	c.mu.Unlock()
	<-ctx.Done()
}

// Wait waits for every run started so far.
func (c *Coordinator) Wait() { c.wg.Wait() }

func (c *Coordinator) context() context.Context {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.runCtx
}

// load reads and checks one plan.
func (c *Coordinator) load(env, name string) (*Plan, error) {
	file, err := c.source.File(env, name)
	switch {
	case errors.Is(err, ErrPlanNotFound):
		return nil, &RunError{Reason: fmt.Sprintf("plan %s was not found in environment %s", name, env)}
	case err != nil:
		return nil, err
	}
	plan, err := Parse(file)
	if err != nil {
		return nil, &RunError{Reason: fmt.Sprintf("plan %s cannot be run: %v", name, err)}
	}
	return plan, nil
}

// check loads plan name and every plan it calls, refusing a plan that
// cannot be used, a cycle and a nesting deeper than the limit.
func (c *Coordinator) check(ctx context.Context, req Request, name string, stack []string, plans map[string]*Plan) error {
	for _, s := range stack {
		if s == name {
			return &RunError{Reason: fmt.Sprintf("plans call each other in a circle: %s", strings.Join(append(stack, name), " -> "))}
		}
	}
	if len(stack) >= MaxDepth {
		return &RunError{Reason: fmt.Sprintf("plans are called more than %d deep: %s", MaxDepth, strings.Join(append(stack, name), " -> "))}
	}
	plan, ok := plans[name]
	if !ok {
		var err error
		if plan, err = c.load(req.Environment, name); err != nil {
			return err
		}
		plans[name] = plan
	}
	for _, step := range plan.Steps {
		if step.Kind == KindPlan {
			if err := c.check(ctx, req, step.Plan, append(append([]string(nil), stack...), name), plans); err != nil {
				return err
			}
		}
	}
	return nil
}

// checkTasks refuses a task step that names a task the caller may not run
// or that does not exist, in the plan or any it calls. It is made after the
// targets are known to be the caller's, so that what a caller learns about
// a node never depends on the rest of the request.
func (c *Coordinator) checkTasks(ctx context.Context, req Request, plans map[string]*Plan) error {
	names := make([]string, 0, len(plans))
	for n := range plans {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		for _, step := range plans[n].Steps {
			if step.Kind != KindTask {
				continue
			}
			if err := c.starter.CheckTaskName(ctx, req.Start.Tasks, req.Environment, step.Task); err != nil {
				return &orchestrator.PlanStepError{Step: step.Index, What: step.Task, Err: err}
			}
		}
	}
	return nil
}

// Start checks a request and, if it can be run, records the run and
// follows it in the background. Refusals are *RunError, *ParamError,
// *orchestrator.OutOfScopeError, *orchestrator.InvalidTargetError and
// *orchestrator.PlanStepError.
func (c *Coordinator) Start(ctx context.Context, req Request) (orchestrator.Job, error) {
	if !environmentName.MatchString(req.Environment) {
		return orchestrator.Job{}, &RunError{Reason: fmt.Sprintf("invalid environment name %q: only letters, digits and underscores are allowed", req.Environment)}
	}
	plans := map[string]*Plan{}
	if err := c.check(ctx, req, req.Plan, nil, plans); err != nil {
		return orchestrator.Job{}, err
	}
	plan := plans[req.Plan]
	env, err := plan.Bind(req.Parameters)
	if err != nil {
		return orchestrator.Job{}, err
	}
	// Target parameters are resolved once, now, within the caller's scope.
	for _, p := range plan.Parameters {
		if !p.Target || env[p.Name] == nil {
			continue
		}
		names, err := c.resolveTargets(ctx, req, p.Name, env[p.Name])
		if err != nil {
			return orchestrator.Job{}, err
		}
		list := make([]any, len(names))
		for i, n := range names {
			list[i] = n
		}
		env[p.Name] = list
	}

	if err := c.checkTasks(ctx, req, plans); err != nil {
		return orchestrator.Job{}, err
	}

	given := redactedParameters(plan, req.Parameters)
	params, _ := json.Marshal(map[string]any{"parameters": given, "sha256": plan.SHA256})
	job, err := c.jobs.CreateCodePlanJob(ctx, req.Plan, req.Environment, params, req.Start.Actor, orchestrator.JobOrigin{})
	if err != nil {
		return orchestrator.Job{}, err
	}

	names := make([]string, 0, len(req.Parameters))
	for n := range req.Parameters {
		names = append(names, n)
	}
	sort.Strings(names)
	if c.recorder != nil {
		c.recorder("codeplan.triggered", req.Start.Actor, fmt.Sprintf("started plan %s in %s", req.Plan, req.Environment))
	}
	c.audit("codeplan.triggered", job, req.Start.Actor, map[string]any{"plan": req.Plan, "environment": req.Environment, "parameters": names, "sha256": plan.SHA256})

	runCtx := c.context()
	c.wg.Add(1)
	go func() {
		defer c.wg.Done()
		r := &run{c: c, ctx: runCtx, job: job, req: req, plans: plans, deadline: time.Now().Add(c.maxDuration)}
		r.follow(plan, env)
	}()
	return job, nil
}

func (c *Coordinator) audit(action string, job orchestrator.Job, actor string, after any) {
	if c.auditRecorder == nil {
		return
	}
	c.auditRecorder(auditlog.Event{Action: action, Actor: actor, ResourceType: "job", ResourceID: strconv.FormatInt(job.ID, 10), After: after})
}

// redactedParameters are the parameters as they are stored: a Sensitive
// one is a placeholder, whatever it was given as.
func redactedParameters(plan *Plan, given map[string]any) map[string]any {
	out := make(map[string]any, len(given))
	secret := map[string]bool{}
	for _, p := range plan.Parameters {
		secret[p.Name] = p.Sensitive
	}
	for k, v := range given {
		if secret[k] && v != nil {
			out[k] = Redacted
		} else {
			out[k] = v
		}
	}
	return out
}

// resolveTargets turns a target parameter into node names for the caller.
func (c *Coordinator) resolveTargets(ctx context.Context, req Request, param string, v any) ([]string, error) {
	spec, err := ParseTargetSpec(v)
	if err != nil {
		return nil, &ParamError{Name: param, Reason: "is not a valid target: " + err.Error()}
	}
	target := orchestrator.Target{Nodes: spec.Nodes, Query: spec.Query, Group: spec.Group}
	names, _, err := c.starter.Targets().Resolve(ctx, target, req.Start.Scope)
	if err != nil {
		return nil, err
	}
	if len(names) == 0 {
		return nil, &ParamError{Name: param, Reason: "matched no nodes you may act on"}
	}
	return names, nil
}

// run is one plan run being followed.
type run struct {
	c        *Coordinator
	ctx      context.Context
	job      orchestrator.Job
	req      Request
	plans    map[string]*Plan
	deadline time.Time

	trace    []orchestrator.TraceEntry
	seq      int
	stepsRun int
	jobStep  int
}

// stopped is why a run ends before its steps do.
type stopped struct{ reason string }

func (e *stopped) Error() string { return e.reason }

func (r *run) follow(plan *Plan, env Env) {
	ret, err := r.execPlan(plan, env, 0, nil)
	status, reason := orchestrator.StatusSucceeded, ""
	var result json.RawMessage
	if err != nil {
		status, reason = orchestrator.StatusFailed, err.Error()
	} else if plan.HasReturn || ret != nil {
		if b, merr := json.Marshal(Redact(ret)); merr == nil {
			result = b
		}
	}
	if r.ctx.Err() != nil {
		return // shutting down: the reaper fails what is left
	}
	ended, ferr := r.c.jobs.FinishCodePlan(context.WithoutCancel(r.ctx), r.job.ID, status, r.trace, result, reason)
	if ferr != nil {
		r.c.logger.Error("failed to finish code plan", "error", ferr, "jobID", r.job.ID)
		return
	}
	if !ended {
		return
	}
	summary := fmt.Sprintf("plan %s %s", r.req.Plan, status)
	if reason != "" {
		summary += ": " + reason
	}
	if r.c.recorder != nil {
		r.c.recorder("codeplan."+status, r.req.Start.Actor, summary)
	}
	r.c.audit("codeplan."+status, r.job, r.req.Start.Actor, nil)
}

func (r *run) save() {
	if err := r.c.jobs.SaveTrace(context.WithoutCancel(r.ctx), r.job.ID, r.trace); err != nil {
		r.c.logger.Warn("failed to save plan trace", "error", err, "jobID", r.job.ID)
	}
}

func pathString(path []int) string {
	parts := make([]string, len(path))
	for i, p := range path {
		parts[i] = strconv.Itoa(p)
	}
	return strings.Join(parts, ".")
}

// execPlan runs one plan's steps in order and returns its value. On a
// failure the steps after it, in this plan, are recorded as not run.
func (r *run) execPlan(plan *Plan, env Env, depth int, path []int) (any, error) {
	for i, step := range plan.Steps {
		stepPath := append(append([]int(nil), path...), step.Index)
		if err := r.runStep(plan, step, env, depth, stepPath); err != nil {
			for _, later := range plan.Steps[i+1:] {
				r.seq++
				r.trace = append(r.trace, orchestrator.TraceEntry{
					Seq: r.seq, Path: pathString(append(append([]int(nil), path...), later.Index)), Depth: depth, Plan: plan.Name,
					Name: later.Name, Description: later.Description, Kind: later.Kind, Task: later.Task, PlanCalled: later.Plan, State: "not_run",
				})
			}
			r.save()
			return nil, err
		}
	}
	if !plan.HasReturn {
		return nil, nil
	}
	v, err := Eval(plan.Return, env)
	if err != nil {
		return nil, fmt.Errorf("return: %w", err)
	}
	return v, nil
}

func (r *run) begin(plan *Plan, step Step, depth int, path []int) *orchestrator.TraceEntry {
	r.seq++
	now := time.Now().UTC()
	r.trace = append(r.trace, orchestrator.TraceEntry{
		Seq: r.seq, Path: pathString(path), Depth: depth, Plan: plan.Name, Name: step.Name, Description: step.Description,
		Kind: step.Kind, Task: step.Task, PlanCalled: step.Plan, State: "running", StartedAt: &now,
	})
	r.save()
	return &r.trace[len(r.trace)-1]
}

func (r *run) end(i int, state, message string, jobID *int64, errText string) {
	now := time.Now().UTC()
	e := &r.trace[i]
	e.State, e.FinishedAt, e.Message, e.Error = state, &now, message, errText
	if jobID != nil {
		e.JobID = jobID
	}
	r.save()
}

func (r *run) runStep(plan *Plan, step Step, env Env, depth int, path []int) error {
	if err := r.ctx.Err(); err != nil {
		return &stopped{reason: "the console stopped following this run"}
	}
	if r.stepsRun >= MaxSteps {
		return &stopped{reason: fmt.Sprintf("the plan reached its limit of %d steps", MaxSteps)}
	}
	if time.Now().After(r.deadline) {
		return &stopped{reason: fmt.Sprintf("the plan reached its time limit of %s", r.c.maxDuration)}
	}
	r.stepsRun++
	r.begin(plan, step, depth, path)
	idx := len(r.trace) - 1
	failStep := func(format string, a ...any) error {
		msg := fmt.Sprintf(format, a...)
		r.end(idx, "failed", "", nil, msg)
		return fmt.Errorf("step %s (%s): %s", pathString(path), stepLabel(step), msg)
	}

	switch step.Kind {
	case KindMessage:
		v, err := Eval(step.Message, env)
		if err != nil {
			return failStep("%v", err)
		}
		r.end(idx, "succeeded", text(v), nil, "")
		return nil

	case KindTask:
		names, err := targetNames(step.Targets, env)
		if err != nil {
			return failStep("targets: %v", err)
		}
		params, err := evalParams(step.Parameters, env)
		if err != nil {
			return failStep("%v", err)
		}
		if len(names) == 0 {
			// Nothing to run on - an earlier step's successes were none -
			// is an empty result, as Bolt gives, not a failure.
			if step.Name != "" {
				env[step.Name] = StepResult(nil)
			}
			r.end(idx, "succeeded", "no targets, nothing was run", nil, "")
			return nil
		}
		raw, err := json.Marshal(Unwrap(params))
		if err != nil {
			return failStep("parameters: %v", err)
		}
		r.jobStep++
		job, err := r.c.starter.StartTask(r.ctx, orchestrator.TaskRequest{
			StartRequest: orchestrator.StartRequest{Target: orchestrator.Target{Nodes: names}, Scope: r.req.Start.Scope, Tasks: r.req.Start.Tasks, Actor: r.req.Start.Actor},
			Task:         step.Task, Environment: r.req.Environment, Params: raw,
			Parent: &orchestrator.StepOf{JobID: r.job.ID, Step: r.jobStep},
		})
		if err != nil {
			return failStep("%v", err)
		}
		jid := job.ID
		r.trace[idx].JobID = &jid
		r.save()
		done, err := r.wait(job.ID)
		if err != nil {
			return failStep("%v", err)
		}
		results := stepResults(names, done)
		if step.Name != "" {
			env[step.Name] = StepResult(results)
		}
		var failed []string
		for _, res := range results {
			if !res.OK {
				failed = append(failed, res.Target)
			}
		}
		if len(failed) > 0 {
			r.end(idx, "failed", "", &jid, "failed on "+strings.Join(failed, ", "))
			return fmt.Errorf("step %s (%s) failed on %s", pathString(path), stepLabel(step), strings.Join(failed, ", "))
		}
		r.end(idx, "succeeded", "", &jid, "")
		return nil

	case KindPlan:
		callee := r.plans[step.Plan]
		args, err := evalParams(step.Parameters, env)
		if err != nil {
			return failStep("%v", err)
		}
		inner, err := bindCall(callee, args)
		if err != nil {
			return failStep("%v", err)
		}
		ret, err := r.execPlan(callee, inner, depth+1, path)
		if err != nil {
			r.end(idx, "failed", "", nil, firstSentence(err.Error()))
			return err
		}
		if step.Name != "" {
			env[step.Name] = ret
		}
		r.end(idx, "succeeded", "", nil, "")
		return nil
	}
	return failStep("unknown step kind %q", step.Kind)
}

func stepLabel(s Step) string {
	switch {
	case s.Name != "":
		return s.Name
	case s.Kind == KindTask:
		return s.Task
	case s.Kind == KindPlan:
		return s.Plan
	}
	return s.Kind
}

func firstSentence(s string) string {
	if len(s) > 300 {
		return s[:300] + "…"
	}
	return s
}

func evalParams(params []NamedNode, env Env) (map[string]any, error) {
	out := make(map[string]any, len(params))
	for _, p := range params {
		v, err := Eval(p.Node, env)
		if err != nil {
			return nil, fmt.Errorf("parameter %s: %w", p.Key, err)
		}
		out[p.Key] = v
	}
	return out, nil
}

// bindCall binds the arguments of a plan step to the plan it calls. A
// Sensitive value may only go to a Sensitive parameter: passing one to a
// parameter that would print it is refused.
func bindCall(callee *Plan, args map[string]any) (Env, error) {
	declared := map[string]Param{}
	for _, p := range callee.Parameters {
		declared[p.Name] = p
	}
	plain := make(map[string]any, len(args))
	for k, v := range args {
		if _, secret := v.(Sensitive); secret {
			if d, ok := declared[k]; ok && !d.Sensitive {
				return nil, fmt.Errorf("parameter %s is not Sensitive in %s, so a Sensitive value cannot be passed to it", k, callee.Name)
			}
		}
		plain[k] = Unwrap(v)
	}
	env, err := callee.Bind(plain)
	if err != nil {
		return nil, fmt.Errorf("calling %s: %w", callee.Name, err)
	}
	for _, p := range callee.Parameters {
		if p.Target && env[p.Name] != nil {
			spec, err := ParseTargetSpec(env[p.Name])
			if err != nil || spec.Group != nil || spec.Query != "" {
				return nil, fmt.Errorf("calling %s: parameter %s must be node names here; a group or query is only resolved when a plan is started", callee.Name, p.Name)
			}
			list := make([]any, len(spec.Nodes))
			for i, n := range spec.Nodes {
				list[i] = n
			}
			env[p.Name] = list
		}
	}
	return env, nil
}

// targetNames evaluates a step's targets to node names.
func targetNames(n Node, env Env) ([]string, error) {
	v, err := Eval(n, env)
	if err != nil {
		return nil, err
	}
	var names []string
	switch x := v.(type) {
	case string:
		names = []string{x}
	case []any:
		for _, e := range x {
			s, ok := e.(string)
			if !ok {
				return nil, errors.New("a list of targets holds node names")
			}
			names = append(names, s)
		}
	case nil:
		return nil, errors.New("there are no targets")
	default:
		return nil, errors.New("targets are a node name or a list of node names")
	}
	if len(names) > MaxStepTargets {
		return nil, fmt.Errorf("a step may name at most %d targets", MaxStepTargets)
	}
	seen := map[string]bool{}
	out := names[:0]
	for _, n := range names {
		if strings.TrimSpace(n) == "" {
			return nil, errors.New("a target name is empty")
		}
		if !seen[n] {
			seen[n] = true
			out = append(out, n)
		}
	}
	return out, nil
}

// wait follows a step's job to its end, recording the plan as alive.
func (r *run) wait(jobID int64) (orchestrator.Job, error) {
	ticker := time.NewTicker(r.c.pollEvery)
	defer ticker.Stop()
	for {
		j, err := r.c.jobs.GetJob(context.WithoutCancel(r.ctx), jobID)
		if err != nil {
			return orchestrator.Job{}, fmt.Errorf("reading the step's job: %w", err)
		}
		if j.Status != orchestrator.StatusRunning {
			return j, nil
		}
		if time.Now().After(r.deadline) {
			return orchestrator.Job{}, &stopped{reason: fmt.Sprintf("the plan reached its time limit of %s", r.c.maxDuration)}
		}
		if err := r.c.jobs.TouchJob(context.WithoutCancel(r.ctx), r.job.ID); err != nil {
			r.c.logger.Warn("failed to record plan heartbeat", "error", err, "jobID", r.job.ID)
		}
		select {
		case <-r.ctx.Done():
			return orchestrator.Job{}, &stopped{reason: "the console stopped following this run"}
		case <-ticker.C:
		}
	}
}

// stepResults builds a task step's per-target results, in the order the
// targets were named, from its finished job.
func stepResults(names []string, job orchestrator.Job) []TargetResult {
	by := map[string]orchestrator.JobTarget{}
	for _, t := range job.Targets {
		by[t.Certname] = t
	}
	out := make([]TargetResult, 0, len(names))
	for _, n := range names {
		t, ok := by[n]
		if !ok {
			out = append(out, TargetResult{Target: n, Error: "the target was not run"})
			continue
		}
		res := TargetResult{Target: n, OK: t.Status == orchestrator.StatusSucceeded, Error: t.ErrorDetail}
		if len(t.Result) > 0 {
			dec := json.NewDecoder(bytes.NewReader(t.Result))
			dec.UseNumber()
			var v any
			if dec.Decode(&v) == nil {
				res.Value = v
			}
		}
		out = append(out, res)
	}
	return out
}
