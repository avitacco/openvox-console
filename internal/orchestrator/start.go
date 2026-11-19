package orchestrator

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/voxpupuli/enterprise-console/internal/nodescope"
	"github.com/voxpupuli/enterprise-console/internal/taskcatalog"
	"github.com/voxpupuli/enterprise-console/internal/taskscope"
)

// ScheduleRef names the schedule that started a job.
type ScheduleRef struct {
	// ID is zero on a job whose schedule has since been deleted; the
	// name is kept.
	ID   int64  `json:"id,omitempty"`
	Name string `json:"name"`
}

// AgentRef names the approved agent request a job was started from: the
// request, the agent token that made it, and the person who approved it.
type AgentRef struct {
	RequestID  int64  `json:"requestId"`
	Token      string `json:"token"`
	ApprovedBy string `json:"approvedBy"`
}

// StartRequest is everything about a job except what it does: which
// nodes, on whose authority, and what started it. It is the same for a
// request made over HTTP and for a schedule firing - the only
// differences between the two are where Scope and Actor come from.
type StartRequest struct {
	Target Target
	// Scope is the acting caller's node scope for orchestrator:run.
	Scope nodescope.Scope
	// Tasks is which tasks the caller may run, and where. The zero value
	// allows none: every caller that starts a task must say what its
	// caller may run, so that forgetting to fails closed. A caller with
	// no limit says taskscope.Unrestricted().
	Tasks taskscope.Scope
	// Actor is recorded as the job's "triggered by".
	Actor string
	// Schedule is set when a schedule is firing.
	Schedule *ScheduleRef
	// Agent is set when an approved agent request is starting the job.
	Agent *AgentRef
}

// RunRequest is a request to run Puppet.
type RunRequest struct {
	StartRequest
	// Options is a RunOptions as JSON; absent, null or empty is a plain
	// run. It is decoded strictly - see decodeRunOptions.
	Options json.RawMessage
}

// TaskRequest is a request to run a task.
type TaskRequest struct {
	StartRequest
	Task           string
	Environment    string
	Params         json.RawMessage
	TimeoutSeconds int
	// Parent makes the job a step of another job, a code plan.
	Parent *StepOf
}

// StepOf places a task job as a step of a code plan.
type StepOf struct {
	JobID int64
	Step  int
}

// TaskCheck is what checking a task request found, for a caller that
// stores the request to start it later.
type TaskCheck struct {
	// Environment is the one the task was resolved from: the request's,
	// or the default.
	Environment string
	// StoredParams are the parameters as they may be stored and shown:
	// sensitive values redacted.
	StoredParams json.RawMessage
	// Sensitive holds the real values of the parameters the task's
	// metadata marks sensitive, by name.
	Sensitive map[string]any
}

// Starter checks a request and starts its job. Handlers and the
// scheduler both go through it, so a scheduled job is checked and
// started exactly as an on-demand one is.
type Starter struct {
	store        *Store
	dispatcher   *Dispatcher
	catalog      TaskCatalog
	environments EnvironmentLister
	targets      *TargetResolver
	patches      *PatchCoordinator
	agentless    AgentlessLookup
}

// PatchRequest is a request to patch nodes.
type PatchRequest struct {
	StartRequest
	// Options is a PatchOptions as JSON; absent, null or empty takes the
	// defaults. It is decoded strictly - see decodePatchOptions.
	Options json.RawMessage
}

// CheckPatch reports whether req would be started, without starting it.
func (s *Starter) CheckPatch(ctx context.Context, req PatchRequest) error {
	_, _, _, err := s.preparePatch(ctx, req, true)
	return err
}

func (s *Starter) preparePatch(ctx context.Context, req PatchRequest, allowEmpty bool) ([]string, *TargetSource, PatchOptions, error) {
	certnames, source, err := s.resolveTargets(ctx, req.StartRequest, allowEmpty)
	if err != nil {
		return nil, nil, PatchOptions{}, err
	}
	if err := s.refuseAgentless(ctx, certnames, "a patch run"); err != nil {
		return nil, nil, PatchOptions{}, err
	}
	options, err := decodePatchOptions(req.Options)
	if err != nil {
		return nil, nil, PatchOptions{}, err
	}
	return certnames, source, options, nil
}

// StartPatch checks req, records the patch run and follows it. Refusals
// are *OutOfScopeError, *InvalidTargetError, *InvalidPatchOptionsError
// and *NodesBeingPatchedError.
func (s *Starter) StartPatch(ctx context.Context, req PatchRequest) (Job, error) {
	certnames, source, options, err := s.preparePatch(ctx, req, false)
	if err != nil {
		return Job{}, err
	}
	if s.patches == nil {
		return Job{}, errors.New("patching is not available on this console instance")
	}
	job, err := s.store.CreatePatchJob(ctx, options, certnames, req.Actor, JobOrigin{Source: source, Schedule: req.Schedule, Agent: req.Agent})
	if err != nil {
		return Job{}, err
	}
	s.patches.Start(job)
	return job, nil
}

// Targets is the resolver requests are resolved with, for the preview.
func (s *Starter) Targets() *TargetResolver { return s.targets }

// resolveTargets resolves req's target. A node list outside scope and a
// malformed target are refused here, before anything else about the
// request is looked at, so what a caller learns about a node outside
// their groups never depends on the rest of the request.
func (s *Starter) resolveTargets(ctx context.Context, req StartRequest, allowEmpty bool) ([]string, *TargetSource, error) {
	certnames, source, err := s.targets.Resolve(ctx, req.Target, req.Scope)
	if err != nil {
		return nil, nil, err
	}
	if len(certnames) == 0 && !allowEmpty {
		return nil, nil, ErrNoNodesMatched
	}
	return certnames, source, nil
}

func (s *Starter) prepareRun(ctx context.Context, req RunRequest, allowEmpty bool) ([]string, *TargetSource, RunOptions, error) {
	certnames, source, err := s.resolveTargets(ctx, req.StartRequest, allowEmpty)
	if err != nil {
		return nil, nil, RunOptions{}, err
	}
	if err := s.refuseAgentless(ctx, certnames, "a Puppet run"); err != nil {
		return nil, nil, RunOptions{}, err
	}
	options, err := decodeRunOptions(req.Options)
	if err == nil {
		err = s.checkEnvironmentExists(ctx, options.Environment)
	}
	if err != nil {
		return nil, nil, RunOptions{}, err
	}
	return certnames, source, options, nil
}

// CheckRun reports whether req would be started, without starting it. A
// target that resolves to no nodes passes: the fleet may not match yet.
func (s *Starter) CheckRun(ctx context.Context, req RunRequest) error {
	_, _, _, err := s.prepareRun(ctx, req, true)
	return err
}

// StartRun checks req, records the job and dispatches it.
//
// Refusals are *OutOfScopeError, *InvalidTargetError and
// *InvalidRunOptionsError; anything else is a failure of something the
// console depends on.
func (s *Starter) StartRun(ctx context.Context, req RunRequest) (Job, error) {
	certnames, source, options, err := s.prepareRun(ctx, req, false)
	if err != nil {
		return Job{}, err
	}
	job, err := s.store.CreateRunJobFrom(ctx, options, certnames, req.Actor, JobOrigin{Source: source, Schedule: req.Schedule, Agent: req.Agent})
	if err != nil {
		return Job{}, err
	}
	s.dispatcher.DispatchRun(ctx, job)
	return job, nil
}

func (s *Starter) prepareTask(ctx context.Context, req TaskRequest, allowEmpty bool) ([]string, *TargetSource, preparedTask, error) {
	if req.Task == "" {
		return nil, nil, preparedTask{}, &InvalidTargetError{Reason: "task name is required"}
	}
	certnames, source, err := s.resolveTargets(ctx, req.StartRequest, allowEmpty)
	if err != nil {
		return nil, nil, preparedTask{}, err
	}
	// Whether the caller may run the task at all is decided from its name
	// before the catalogue is read, so that a task they may not run and
	// one that does not exist are refused alike.
	if err := checkTaskAllowed(req.Tasks, req.Task); err != nil {
		return nil, nil, preparedTask{}, err
	}
	allowed, err := taskNodes(req.Tasks, req.Task, certnames, source == nil, nodescope.Everything())
	if err != nil {
		return nil, nil, preparedTask{}, err
	}
	if source != nil {
		certnames = nodescope.Filter(allowed, certnames, func(c string) string { return c })
		if len(certnames) == 0 && !allowEmpty {
			return nil, nil, preparedTask{}, ErrNoNodesMatched
		}
	}
	prepared, err := prepareTask(ctx, s.catalog, req.Environment, req.Task, req.Params, req.TimeoutSeconds)
	if err != nil {
		return nil, nil, preparedTask{}, err
	}
	return certnames, source, prepared, nil
}

// CheckTask is CheckRun for a task.
func (s *Starter) CheckTask(ctx context.Context, req TaskRequest) (TaskCheck, error) {
	_, _, prepared, err := s.prepareTask(ctx, req, true)
	if err != nil {
		return TaskCheck{}, err
	}
	return TaskCheck{
		Environment:  prepared.payload.Environment,
		StoredParams: prepared.storedParams,
		Sensitive:    prepared.sensitive,
	}, nil
}

// StartTask checks req, records the job and dispatches it. Refusals are
// as StartRun's, with *InvalidTaskError for the task itself.
func (s *Starter) StartTask(ctx context.Context, req TaskRequest) (Job, error) {
	certnames, source, prepared, err := s.prepareTask(ctx, req, false)
	if err != nil {
		return Job{}, err
	}
	var parent *int64
	var step *int
	if req.Parent != nil {
		parent, step = &req.Parent.JobID, &req.Parent.Step
	}
	job, err := s.store.CreateTaskJobChild(ctx, req.Task, prepared.payload.Environment, prepared.storedParams, certnames, req.Actor, JobOrigin{Source: source, Schedule: req.Schedule, Agent: req.Agent}, parent, step)
	if err != nil {
		return Job{}, err
	}
	s.dispatcher.DispatchTask(ctx, job, prepared.payload)
	return job, nil
}

// checkEnvironmentExists refuses name only on positive knowledge that
// the server lacks it.
func (s *Starter) checkEnvironmentExists(ctx context.Context, name string) error {
	if name == "" || s.environments == nil {
		return nil
	}
	known, err := s.environments.Environments(ctx)
	if err != nil {
		return nil
	}
	for _, env := range known {
		if env == name {
			return nil
		}
	}
	return &InvalidRunOptionsError{Reason: fmt.Sprintf("environment %q not found on the Puppet server", name)}
}

// IsRefusal reports whether err is the console refusing a request for
// what it asked, as opposed to failing to carry it out.
func IsRefusal(err error) bool {
	var scope *OutOfScopeError
	var target *InvalidTargetError
	var options *InvalidRunOptionsError
	var task *InvalidTaskError
	var plan *InvalidPlanError
	var patchOptions *InvalidPatchOptionsError
	var beingPatched *NodesBeingPatchedError
	var agentless *AgentlessTargetsError
	var notPermitted *TaskNotPermittedError
	return errors.As(err, &agentless) || errors.As(err, &patchOptions) || errors.As(err, &beingPatched) || errors.As(err, &scope) || errors.As(err, &target) || errors.As(err, &options) || errors.As(err, &task) || errors.As(err, &plan) || errors.As(err, &notPermitted)
}

// CheckTaskName refuses a task the caller may not run, or that does not
// exist in environment, without starting anything: what can be said about a
// task before its target and parameters are known. A plan uses it to refuse
// a request whose steps name a task that would be refused.
func (s *Starter) CheckTaskName(ctx context.Context, tasks taskscope.Scope, environment, name string) error {
	if err := checkTaskAllowed(tasks, name); err != nil {
		return err
	}
	if s.catalog == nil {
		return &InvalidTaskError{Err: taskcatalog.ErrNotConfigured}
	}
	if environment == "" {
		environment = DefaultTaskEnvironment
	}
	if _, err := s.catalog.Task(ctx, environment, name); err != nil {
		return &InvalidTaskError{Err: err}
	}
	return nil
}
