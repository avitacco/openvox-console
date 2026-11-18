package orchestrator

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/voxpupuli/enterprise-console/internal/nodescope"
	"github.com/voxpupuli/enterprise-console/internal/taskcatalog"
	"github.com/voxpupuli/enterprise-console/internal/taskscope"
)

// Plan limits.
const (
	MaxPlanSteps      = 20
	MaxPlanNameLength = 100
)

// InvalidPlanError is a plan the console refuses for its own shape: no
// steps, too many, a step that is neither a run nor a task.
type InvalidPlanError struct{ Reason string }

func (e *InvalidPlanError) Error() string { return e.Reason }

// PlanStepError is one step of a plan that would be refused as a run or
// task of its own. Step counts from 1, as steps are shown.
type PlanStepError struct {
	Step int
	What string
	Err  error
}

func (e *PlanStepError) Error() string {
	return fmt.Sprintf("step %d (%s): %v", e.Step, e.What, e.Err)
}
func (e *PlanStepError) Unwrap() error { return e.Err }

// PlanRequest is a request to run a plan: steps carried out in order on
// one set of nodes, resolved once when the plan starts.
type PlanRequest struct {
	StartRequest
	Name  string
	Steps []PlanStep
}

// what names a step for a message.
func (s PlanStep) what() string {
	if s.Kind == JobKindTask {
		if s.TaskName == "" {
			return "task"
		}
		return s.TaskName
	}
	return "run"
}

// preparedStep is a plan step checked and ready: the row its job is
// stored as, and for a task the payload nodes are sent.
type preparedStep struct {
	row     PlanStep
	payload *taskcatalog.Resolved
	check   TaskCheck
}

func checkPlanShape(name string, steps []PlanStep) error {
	if len([]rune(name)) > MaxPlanNameLength {
		return &InvalidPlanError{Reason: fmt.Sprintf("a plan's name may be at most %d characters", MaxPlanNameLength)}
	}
	if len(steps) == 0 {
		return &InvalidPlanError{Reason: "at least one step is required"}
	}
	if len(steps) > MaxPlanSteps {
		return &InvalidPlanError{Reason: fmt.Sprintf("a plan may have at most %d steps", MaxPlanSteps)}
	}
	return nil
}

// checkStepShape refuses a step that mixes a run's fields with a
// task's, so nothing a caller wrote on a step is silently ignored.
func checkStepShape(step PlanStep) error {
	switch step.Kind {
	case JobKindRun:
		if step.TaskName != "" || len(step.Params) > 0 || step.TimeoutSeconds != 0 || step.Environment != "" {
			return &InvalidPlanError{Reason: `a run step takes only "options"`}
		}
	case JobKindTask:
		if len(step.Options) > 0 && string(step.Options) != "null" {
			return &InvalidPlanError{Reason: `a task step does not take "options"`}
		}
		if step.TaskName == "" {
			return &InvalidPlanError{Reason: "task name is required"}
		}
	default:
		return &InvalidPlanError{Reason: fmt.Sprintf(`a step's kind must be %q or %q`, JobKindRun, JobKindTask)}
	}
	return nil
}

// prepareSteps checks every step as the run or task it is.
//
// Each task step is first matched by name against what the caller may
// run, then against the nodes it may run on (see taskNodes), in step
// order, before its task is read from the catalogue. It returns the nodes
// every task step may run on: the plan runs on a target narrowed to
// them, so that all its steps share one set of nodes.
func (s *Starter) prepareSteps(ctx context.Context, steps []PlanStep, tasks taskscope.Scope, resolved []string, listed bool) ([]preparedStep, nodescope.Scope, error) {
	allowed := nodescope.Everything()
	prepared := make([]preparedStep, len(steps))
	for i, step := range steps {
		fail := func(err error) ([]preparedStep, nodescope.Scope, error) {
			return nil, nodescope.Scope{}, &PlanStepError{Step: i + 1, What: step.what(), Err: err}
		}
		if err := checkStepShape(step); err != nil {
			return fail(err)
		}
		if step.Kind == JobKindRun {
			if err := s.refuseAgentless(ctx, resolved, "a run step"); err != nil {
				return fail(err)
			}
			options, err := decodeRunOptions(step.Options)
			if err == nil {
				err = s.checkEnvironmentExists(ctx, options.Environment)
			}
			if err != nil {
				return fail(err)
			}
			row := PlanStep{Kind: JobKindRun, Environment: options.Environment}
			if !options.IsZero() {
				encoded, err := json.Marshal(options)
				if err != nil {
					return nil, nodescope.Scope{}, fmt.Errorf("encode run options: %w", err)
				}
				row.Params = encoded
			}
			prepared[i] = preparedStep{row: row}
			continue
		}
		if err := checkTaskAllowed(tasks, step.TaskName); err != nil {
			return fail(err)
		}
		var err error
		if allowed, err = taskNodes(tasks, step.TaskName, resolved, listed, allowed); err != nil {
			return fail(err)
		}
		task, err := prepareTask(ctx, s.catalog, step.Environment, step.TaskName, step.Params, step.TimeoutSeconds)
		if err != nil {
			return fail(err)
		}
		payload := task.payload
		prepared[i] = preparedStep{
			row:     PlanStep{Kind: JobKindTask, TaskName: step.TaskName, Environment: payload.Environment, Params: task.storedParams},
			payload: &payload,
			check:   TaskCheck{Environment: payload.Environment, StoredParams: task.storedParams, Sensitive: task.sensitive},
		}
	}
	return prepared, allowed, nil
}

func (s *Starter) preparePlan(ctx context.Context, req PlanRequest, allowEmpty bool) ([]string, *TargetSource, []preparedStep, error) {
	if err := checkPlanShape(req.Name, req.Steps); err != nil {
		return nil, nil, nil, err
	}
	certnames, source, err := s.resolveTargets(ctx, req.StartRequest, allowEmpty)
	if err != nil {
		return nil, nil, nil, err
	}
	steps, allowed, err := s.prepareSteps(ctx, req.Steps, req.Tasks, certnames, source == nil)
	if err != nil {
		return nil, nil, nil, err
	}
	// A query or group is narrowed to the nodes every task step may run
	// on; a node list was refused above if it named any other.
	if source != nil {
		certnames = nodescope.Filter(allowed, certnames, func(c string) string { return c })
		if len(certnames) == 0 && !allowEmpty {
			return nil, nil, nil, ErrNoNodesMatched
		}
	}
	return certnames, source, steps, nil
}

// CheckPlan reports whether req would be started, without starting it,
// and what checking each step found (the zero TaskCheck for a run
// step). A target that resolves to no nodes passes.
func (s *Starter) CheckPlan(ctx context.Context, req PlanRequest) ([]TaskCheck, error) {
	_, _, steps, err := s.preparePlan(ctx, req, true)
	if err != nil {
		return nil, err
	}
	checks := make([]TaskCheck, len(steps))
	for i, step := range steps {
		checks[i] = step.check
	}
	return checks, nil
}

// StartPlan checks req, records the plan and its steps, and dispatches
// the first step. Refusals are as StartRun's and StartTask's, with
// *InvalidPlanError for the plan's shape and *PlanStepError naming a
// step that would be refused.
func (s *Starter) StartPlan(ctx context.Context, req PlanRequest) (Job, error) {
	certnames, source, steps, err := s.preparePlan(ctx, req, false)
	if err != nil {
		return Job{}, err
	}
	rows := make([]PlanStep, len(steps))
	payloads := map[int]taskcatalog.Resolved{}
	for i, step := range steps {
		rows[i] = step.row
		if step.payload != nil {
			payloads[i] = *step.payload
		}
	}
	plan, err := s.store.CreatePlanFrom(ctx, req.Name, rows, certnames, req.Actor, JobOrigin{Source: source, Schedule: req.Schedule})
	if err != nil {
		return Job{}, err
	}
	s.dispatcher.DispatchPlan(ctx, plan, payloads)
	return plan, nil
}

// SensitiveNeed is a sensitive parameter of a step's task: a value a
// stored plan does not hold and whoever runs it must give.
type SensitiveNeed struct {
	Param    string `json:"param"`
	Required bool   `json:"required"`
}

// CheckPlanTemplate checks steps as a plan to keep rather than to run:
// each step must be acceptable as a run or task except that a sensitive
// task parameter is neither required nor kept. It returns the steps as
// they may be stored, with every sensitive value removed.
//
// There is no target, so a task step is checked against tasks by name
// only: whether the caller may run it on some node, not which.
func (s *Starter) CheckPlanTemplate(ctx context.Context, name string, steps []PlanStep, tasks taskscope.Scope) ([]PlanStep, error) {
	if err := checkPlanShape(name, steps); err != nil {
		return nil, err
	}
	stored := make([]PlanStep, len(steps))
	for i, step := range steps {
		fail := func(err error) ([]PlanStep, error) {
			return nil, &PlanStepError{Step: i + 1, What: step.what(), Err: err}
		}
		if err := checkStepShape(step); err != nil {
			return fail(err)
		}
		if step.Kind == JobKindRun {
			options, err := decodeRunOptions(step.Options)
			if err == nil {
				err = s.checkEnvironmentExists(ctx, options.Environment)
			}
			if err != nil {
				return fail(err)
			}
			stored[i] = PlanStep{Kind: JobKindRun}
			if !options.IsZero() {
				encoded, err := json.Marshal(options)
				if err != nil {
					return nil, fmt.Errorf("encode run options: %w", err)
				}
				stored[i].Options = encoded
			}
			continue
		}
		if err := checkTaskAllowed(tasks, step.TaskName); err != nil {
			return fail(err)
		}
		kept, err := s.checkTaskTemplate(ctx, step)
		if err != nil {
			return fail(err)
		}
		stored[i] = kept
	}
	return stored, nil
}

// checkTaskTemplate validates a task step with the task's sensitive
// parameters set aside: their values are dropped from what was given,
// and they are not asked for.
func (s *Starter) checkTaskTemplate(ctx context.Context, step PlanStep) (PlanStep, error) {
	if s.catalog == nil {
		return PlanStep{}, &InvalidTaskError{Err: taskcatalog.ErrNotConfigured}
	}
	environment := step.Environment
	if environment == "" {
		environment = DefaultTaskEnvironment
	}
	if err := checkTaskTimeout(step.TaskName, step.TimeoutSeconds); err != nil {
		return PlanStep{}, err
	}
	task, err := s.catalog.Task(ctx, environment, step.TaskName)
	if err != nil {
		return PlanStep{}, &InvalidTaskError{Err: err}
	}

	given, err := decodeParamObject(step.Params)
	if err != nil {
		return PlanStep{}, &InvalidTaskError{Err: &taskcatalog.ValidationError{Task: step.TaskName, Problems: []taskcatalog.Problem{{Param: "(parameters)", Message: err.Error()}}}}
	}
	open := task
	if task.Metadata.Parameters != nil {
		open.Metadata.Parameters = map[string]taskcatalog.Parameter{}
		for name, decl := range task.Metadata.Parameters {
			if decl.Sensitive {
				delete(given, name)
				continue
			}
			open.Metadata.Parameters[name] = decl
		}
	}
	kept, err := json.Marshal(given)
	if err != nil {
		return PlanStep{}, fmt.Errorf("encode parameters: %w", err)
	}
	if _, err := taskcatalog.Validate(open, kept); err != nil {
		return PlanStep{}, &InvalidTaskError{Err: err}
	}
	out := PlanStep{Kind: JobKindTask, TaskName: step.TaskName, Environment: environment, TimeoutSeconds: step.TimeoutSeconds}
	if len(given) > 0 {
		out.Params = kept
	}
	return out, nil
}

// StepNeeds lists the sensitive parameters of a task step's task, by
// name. problem is set, and needs empty, when the task can no longer be
// found or read: a stored step outlives the code it names.
func (s *Starter) StepNeeds(ctx context.Context, step PlanStep) (needs []SensitiveNeed, problem string) {
	if step.Kind != JobKindTask {
		return nil, ""
	}
	if s.catalog == nil {
		return nil, taskcatalog.ErrNotConfigured.Error()
	}
	environment := step.Environment
	if environment == "" {
		environment = DefaultTaskEnvironment
	}
	task, err := s.catalog.Task(ctx, environment, step.TaskName)
	if err != nil {
		return nil, err.Error()
	}
	for name, decl := range task.Metadata.Parameters {
		if decl.Sensitive {
			needs = append(needs, SensitiveNeed{Param: name, Required: taskcatalog.InputFor(decl).Required})
		}
	}
	sort.Slice(needs, func(i, j int) bool { return needs[i].Param < needs[j].Param })
	return needs, ""
}

// decodeParamObject reads a step's parameters as a JSON object, keeping
// numbers as written.
func decodeParamObject(raw json.RawMessage) (map[string]any, error) {
	params := map[string]any{}
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || string(trimmed) == "null" {
		return params, nil
	}
	dec := json.NewDecoder(bytes.NewReader(trimmed))
	dec.UseNumber()
	if err := dec.Decode(&params); err != nil {
		return nil, fmt.Errorf("parameters must be a JSON object: %w", err)
	}
	if params == nil {
		params = map[string]any{}
	}
	return params, nil
}

// Step states, as a plan's steps are read. They are derived: a step's
// stored status is one of the three a job has.
const (
	StepWaiting   = "waiting"
	StepRunning   = "running"
	StepSucceeded = "succeeded"
	StepFailed    = "failed"
	StepNotRun    = "not_run"
)

// notRunPrefix starts the reason recorded on every node of a step that
// was closed because an earlier step failed.
const notRunPrefix = "not run: step "

func notRunReason(failedStep int) string {
	return fmt.Sprintf("%s%d failed", notRunPrefix, failedStep)
}

// PlanRef names the plan a step belongs to.
type PlanRef struct {
	ID    int64  `json:"id"`
	Name  string `json:"name"`
	Steps int    `json:"steps"`
}

// PlanStepView is one step of a plan as its job is read.
type PlanStepView struct {
	ID          int64           `json:"id"`
	StepOrder   int             `json:"stepOrder"`
	Kind        JobKind         `json:"kind"`
	TaskName    string          `json:"taskName,omitempty"`
	Environment string          `json:"environment,omitempty"`
	Params      json.RawMessage `json:"params,omitempty"`
	RunOptions  *RunOptions     `json:"runOptions,omitempty"`
	State       string          `json:"state"`
	StartedAt   *time.Time      `json:"startedAt,omitempty"`
	FinishedAt  *time.Time      `json:"finishedAt,omitempty"`
	Targets     []JobTarget     `json:"targets"`
}

// stepViews derives each step's state and times from the plan and the
// steps before it. A step's job row is created with the plan, so its
// own started_at says nothing: a step starts when the one before it
// finishes.
func stepViews(plan Job, steps []Job) []PlanStepView {
	views := make([]PlanStepView, len(steps))
	startsAt := &plan.StartedAt
	reached := true
	for i, step := range steps {
		v := PlanStepView{
			ID: step.ID, StepOrder: i, Kind: step.Kind, TaskName: step.TaskName, Environment: step.Environment,
			RunOptions: step.RunOptions, FinishedAt: step.FinishedAt, Targets: step.Targets,
		}
		if step.StepOrder != nil {
			v.StepOrder = *step.StepOrder
		}
		if step.Kind == JobKindTask {
			v.Params = step.Params
		}
		switch {
		case step.Status == StatusSucceeded:
			v.State = StepSucceeded
		case step.Status == StatusFailed && wasNotRun(step):
			v.State = StepNotRun
		case step.Status == StatusFailed:
			v.State = StepFailed
		case reached:
			v.State = StepRunning
		default:
			v.State = StepWaiting
		}
		if reached && v.State != StepNotRun {
			v.StartedAt = startsAt
		}
		if v.State == StepNotRun || v.State == StepWaiting {
			v.FinishedAt = nil
		}
		reached = reached && v.State == StepSucceeded
		startsAt = step.FinishedAt
		views[i] = v
	}
	return views
}

// wasNotRun reports whether step was closed without being sent to any
// node.
func wasNotRun(step Job) bool {
	if len(step.Targets) == 0 {
		return false
	}
	for _, t := range step.Targets {
		if !strings.HasPrefix(t.ErrorDetail, notRunPrefix) {
			return false
		}
	}
	return true
}

// IsPlanShapeError reports whether err refuses a plan for its shape or
// for one of its steps.
func IsPlanShapeError(err error) bool {
	var shape *InvalidPlanError
	var step *PlanStepError
	return errors.As(err, &shape) || errors.As(err, &step)
}
