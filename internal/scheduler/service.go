package scheduler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/voxpupuli/enterprise-console/internal/auditlog"
	"github.com/voxpupuli/enterprise-console/internal/nodescope"
	"github.com/voxpupuli/enterprise-console/internal/orchestrator"
	"github.com/voxpupuli/enterprise-console/internal/sealer"
	"github.com/voxpupuli/enterprise-console/internal/taskscope"
)

// ErrNoAuthority is what an Authority returns for an owner who no
// longer exists or no longer holds orchestrator:run.
var ErrNoAuthority = errors.New("the schedule's owner no longer holds orchestrator:run")

// Authorization is what an owner may do at the moment a schedule fires:
// the nodes they may act on and the tasks they may run.
type Authorization struct {
	Scope nodescope.Scope
	Tasks taskscope.Scope
}

// Authority answers, at the moment a schedule fires, what its owner may
// do as their roles stand now - their node scope for orchestrator:run
// and the tasks they may run - or ErrNoAuthority.
type Authority func(ctx context.Context, owner Owner) (Authorization, error)

// Starter is the part of *orchestrator.Starter the scheduler uses.
type Starter interface {
	CheckRun(ctx context.Context, req orchestrator.RunRequest) error
	CheckTask(ctx context.Context, req orchestrator.TaskRequest) (orchestrator.TaskCheck, error)
	StartRun(ctx context.Context, req orchestrator.RunRequest) (orchestrator.Job, error)
	StartTask(ctx context.Context, req orchestrator.TaskRequest) (orchestrator.Job, error)
	CheckPlan(ctx context.Context, req orchestrator.PlanRequest) ([]orchestrator.TaskCheck, error)
	StartPlan(ctx context.Context, req orchestrator.PlanRequest) (orchestrator.Job, error)
}

// Service is schedules' rules: what may be saved, by whom, and what
// happens when one comes due.
type Service struct {
	store     *Store
	starter   Starter
	authority Authority
	sealer    *sealer.Sealer // nil without a secrets key
	logger    *slog.Logger

	// now is the clock due times are judged by: the database's, except
	// in tests.
	now func(ctx context.Context) (time.Time, error)

	recordActivity func(action, actor, summary string)
	recordAudit    func(auditlog.Event)
}

// NewService builds a Service. sealer may be nil, in which case a
// schedule carrying a sensitive value is refused.
func NewService(store *Store, starter Starter, authority Authority, sealer *sealer.Sealer, logger *slog.Logger) *Service {
	if logger == nil {
		logger = slog.Default()
	}
	return &Service{
		store: store, starter: starter, authority: authority, sealer: sealer, logger: logger,
		now:            store.Now,
		recordActivity: func(string, string, string) {},
		recordAudit:    func(auditlog.Event) {},
	}
}

// SetRecorders attaches the activity and audit logs.
func (s *Service) SetRecorders(activity func(action, actor, summary string), audit func(auditlog.Event)) {
	if activity != nil {
		s.recordActivity = activity
	}
	if audit != nil {
		s.recordAudit = audit
	}
}

func (s *Service) record(action, actor string, sched Schedule, summary string) {
	s.recordActivity(action, actor, summary)
	s.recordAudit(auditlog.Event{Action: action, Actor: actor, ResourceType: "schedule", ResourceID: strconv.FormatInt(sched.ID, 10)})
}

// Caller is who is making a request, and what they may reach.
type Caller struct {
	Owner Owner
	// Scope is their node scope for the permission that gated the
	// request.
	Scope nodescope.Scope
	// Tasks is which tasks they may run. The zero value allows none, as
	// on an orchestrator.StartRequest.
	Tasks taskscope.Scope
}

// Input is a schedule as a caller writes it.
type Input struct {
	Name string               `json:"name"`
	Kind orchestrator.JobKind `json:"kind"`

	Options json.RawMessage `json:"options,omitempty"`

	Task           string          `json:"task,omitempty"`
	Environment    string          `json:"environment,omitempty"`
	Params         json.RawMessage `json:"params,omitempty"`
	TimeoutSeconds int             `json:"timeoutSeconds,omitempty"`

	// PlanName and Steps are a plan's. PlanName defaults to the
	// schedule's own name.
	PlanName string                  `json:"planName,omitempty"`
	Steps    []orchestrator.PlanStep `json:"steps,omitempty"`

	Targets []string `json:"targets,omitempty"`
	Query   string   `json:"query,omitempty"`
	Group   *int64   `json:"group,omitempty"`

	StartAt time.Time `json:"startAt"`
	Repeat  *Repeat   `json:"repeat,omitempty"`
}

// visible reports whether caller may see or change sched: its owner,
// or a caller whose scope covers every node.
func visible(sched Schedule, caller Caller) bool {
	return caller.Scope.All() || sched.OwnedBy(caller.Owner)
}

// List returns the schedules caller may see.
func (s *Service) List(ctx context.Context, caller Caller) ([]Schedule, error) {
	if caller.Scope.All() {
		return s.store.List(ctx, nil)
	}
	return s.store.List(ctx, &caller.Owner)
}

// Get returns schedule id if caller may see it, and ErrNotFound
// otherwise - the same answer as for a schedule that does not exist.
func (s *Service) Get(ctx context.Context, id int64, caller Caller) (Schedule, error) {
	sched, err := s.store.Get(ctx, id)
	if err != nil {
		return Schedule{}, err
	}
	if !visible(sched, caller) {
		return Schedule{}, ErrNotFound
	}
	return sched, nil
}

// Create saves a new schedule owned by caller.
func (s *Service) Create(ctx context.Context, in Input, caller Caller) (Schedule, error) {
	sched, seal, err := s.check(ctx, in, caller, nil)
	if err != nil {
		return Schedule{}, err
	}
	saved, err := s.store.Create(ctx, sched, seal)
	if err != nil {
		return Schedule{}, err
	}
	s.record("schedule.created", caller.Owner.Name, saved, fmt.Sprintf("created schedule %q (%s)", saved.Name, describe(saved)))
	return saved, nil
}

// Replace saves new contents for schedule id. The caller becomes its
// owner: a schedule runs with the authority of whoever last saved what
// it does.
func (s *Service) Replace(ctx context.Context, id int64, in Input, caller Caller) (Schedule, error) {
	existing, err := s.Get(ctx, id, caller)
	if err != nil {
		return Schedule{}, err
	}
	sched, seal, err := s.check(ctx, in, caller, &existing)
	if err != nil {
		return Schedule{}, err
	}
	saved, err := s.store.Replace(ctx, id, sched, seal)
	if err != nil {
		return Schedule{}, err
	}
	summary := fmt.Sprintf("changed schedule %q (%s)", saved.Name, describe(saved))
	if !existing.OwnedBy(caller.Owner) {
		summary += fmt.Sprintf(", taking it over from %s", existing.Owner)
	}
	s.record("schedule.updated", caller.Owner.Name, saved, summary)
	return saved, nil
}

// Delete removes schedule id. The jobs it started are kept.
func (s *Service) Delete(ctx context.Context, id int64, caller Caller) error {
	sched, err := s.Get(ctx, id, caller)
	if err != nil {
		return err
	}
	if err := s.store.Delete(ctx, id); err != nil {
		return err
	}
	s.record("schedule.deleted", caller.Owner.Name, sched, fmt.Sprintf("deleted schedule %q", sched.Name))
	return nil
}

// Pause stops schedule id coming due until it is resumed. Its owner is
// unchanged.
func (s *Service) Pause(ctx context.Context, id int64, caller Caller) (Schedule, error) {
	sched, err := s.Get(ctx, id, caller)
	if err != nil {
		return Schedule{}, err
	}
	if sched.Enabled {
		if err := s.store.SetEnabled(ctx, id, false, sched.NextRunAt); err != nil {
			return Schedule{}, err
		}
		s.record("schedule.paused", caller.Owner.Name, sched, fmt.Sprintf("paused schedule %q", sched.Name))
	}
	return s.store.Get(ctx, id)
}

// Resume lets schedule id come due again, from its next time still
// ahead: nothing is started for the times that passed while it was
// paused.
func (s *Service) Resume(ctx context.Context, id int64, caller Caller) (Schedule, error) {
	sched, err := s.Get(ctx, id, caller)
	if err != nil {
		return Schedule{}, err
	}
	if sched.Enabled {
		return sched, nil
	}
	now, err := s.now(ctx)
	if err != nil {
		return Schedule{}, err
	}
	next := nextStart(sched.StartAt, sched.Repeat, now)
	if next == nil {
		return Schedule{}, &InvalidScheduleError{Field: "startAt", Reason: "this schedule's start time has passed; give it a new start time to run it"}
	}
	if err := s.store.SetEnabled(ctx, id, true, next); err != nil {
		return Schedule{}, err
	}
	s.record("schedule.resumed", caller.Owner.Name, sched, fmt.Sprintf("resumed schedule %q", sched.Name))
	return s.store.Get(ctx, id)
}

// check validates in as a schedule saved by caller now, and returns it
// ready to store. It is a dry run of firing: the action and the target
// are checked as the on-demand request the caller could make at this
// moment, except that a target matching no nodes is accepted - the
// fleet may not match yet. existing is the schedule being replaced.
func (s *Service) check(ctx context.Context, in Input, caller Caller, existing *Schedule) (Schedule, SealFunc, error) {
	now, err := s.now(ctx)
	if err != nil {
		return Schedule{}, nil, err
	}
	if err := checkTiming(in.Name, in.StartAt, in.Repeat, now); err != nil {
		return Schedule{}, nil, err
	}

	sched := Schedule{
		Name: strings.TrimSpace(in.Name), Kind: in.Kind,
		Targets: in.Targets, Query: in.Query, Group: in.Group,
		StartAt: in.StartAt.UTC(), Repeat: in.Repeat,
		Owner: caller.Owner.Name, OwnerKind: caller.Owner.Kind,
	}
	sched.NextRunAt = nextStart(sched.StartAt, sched.Repeat, now)
	start := orchestrator.StartRequest{Target: sched.Target(), Scope: caller.Scope, Tasks: caller.Tasks, Actor: caller.Owner.Name}

	switch in.Kind {
	case orchestrator.JobKindRun:
		sched.Options = in.Options
		if err := s.starter.CheckRun(ctx, orchestrator.RunRequest{StartRequest: start, Options: in.Options}); err != nil {
			return Schedule{}, nil, err
		}
		return sched, nil, nil

	case orchestrator.JobKindTask:
		params, err := s.paramsToCheck(in, caller, existing)
		if err != nil {
			return Schedule{}, nil, err
		}
		checked, err := s.starter.CheckTask(ctx, orchestrator.TaskRequest{
			StartRequest: start, Task: in.Task, Environment: in.Environment, Params: params, TimeoutSeconds: in.TimeoutSeconds,
		})
		if err != nil {
			return Schedule{}, nil, err
		}
		sched.Task, sched.Environment, sched.TimeoutSeconds = in.Task, checked.Environment, in.TimeoutSeconds
		sched.Params = checked.StoredParams
		if len(checked.Sensitive) == 0 {
			return sched, nil, nil
		}
		if s.sealer == nil {
			return Schedule{}, nil, &InvalidScheduleError{Field: "params", Reason: "this task has a sensitive parameter, and a schedule can keep one only when the console has a secrets key (CONSOLE_SECRETS_KEY); ask an administrator to configure it"}
		}
		sensitive, err := json.Marshal(checked.Sensitive)
		if err != nil {
			return Schedule{}, nil, fmt.Errorf("encode sensitive parameters: %w", err)
		}
		return sched, func(id int64) ([]byte, error) { return s.sealer.Seal(sensitive, sealBinding(id)) }, nil

	case orchestrator.JobKindPlan:
		planName := strings.TrimSpace(in.PlanName)
		if planName == "" {
			planName = sched.Name
		}
		steps := s.stepsToCheck(in, caller, existing)
		checks, err := s.starter.CheckPlan(ctx, orchestrator.PlanRequest{StartRequest: start, Name: planName, Steps: steps})
		if err != nil {
			return Schedule{}, nil, err
		}
		// The schedule's own copy: each step as it was checked, with a
		// task's sensitive values redacted here and sealed below.
		sched.PlanName = planName
		sched.Steps = make([]orchestrator.PlanStep, len(steps))
		sensitive := map[string]map[string]any{}
		for i, step := range steps {
			if step.Kind != orchestrator.JobKindTask {
				sched.Steps[i] = orchestrator.PlanStep{Kind: step.Kind, Options: step.Options}
				continue
			}
			sched.Steps[i] = orchestrator.PlanStep{
				Kind: step.Kind, TaskName: step.TaskName, Environment: checks[i].Environment,
				Params: checks[i].StoredParams, TimeoutSeconds: step.TimeoutSeconds,
			}
			if len(checks[i].Sensitive) > 0 {
				sensitive[strconv.Itoa(i)] = checks[i].Sensitive
			}
		}
		if len(sensitive) == 0 {
			return sched, nil, nil
		}
		if s.sealer == nil {
			return Schedule{}, nil, &InvalidScheduleError{Field: "steps", Reason: "a step of this plan has a sensitive parameter, and a schedule can keep one only when the console has a secrets key (CONSOLE_SECRETS_KEY); ask an administrator to configure it"}
		}
		encoded, err := json.Marshal(sensitive)
		if err != nil {
			return Schedule{}, nil, fmt.Errorf("encode sensitive parameters: %w", err)
		}
		return sched, func(id int64) ([]byte, error) { return s.sealer.Seal(encoded, sealBinding(id)) }, nil
	}
	return Schedule{}, nil, &InvalidScheduleError{Field: "kind", Reason: `a schedule's kind must be "run", "task" or "plan"`}
}

// stepsToCheck is in's steps with, for each task step, the sensitive
// values the request left out restored from existing - under the rule
// paramsToCheck follows for a task, applied step by step: only for the
// schedule's own owner, and only for a step whose task and environment
// are what they were at that position.
func (s *Service) stepsToCheck(in Input, caller Caller, existing *Schedule) []orchestrator.PlanStep {
	var kept map[string]any
	if existing != nil && len(existing.sealed) > 0 && existing.OwnedBy(caller.Owner) && existing.Kind == orchestrator.JobKindPlan {
		kept, _ = s.openSealed(*existing)
	}
	steps := make([]orchestrator.PlanStep, len(in.Steps))
	for i, step := range in.Steps {
		steps[i] = step
		if step.Kind != orchestrator.JobKindTask {
			continue
		}
		given := map[string]any{}
		if len(step.Params) > 0 && string(step.Params) != "null" {
			if err := json.Unmarshal(step.Params, &given); err != nil {
				// Not an object: the task validator says so itself.
				continue
			}
		}
		for name, v := range given {
			if v == orchestrator.RedactedValue {
				delete(given, name)
			}
		}
		if before, ok := kept[strconv.Itoa(i)].(map[string]any); ok && i < len(existing.Steps) &&
			existing.Steps[i].Kind == orchestrator.JobKindTask && existing.Steps[i].TaskName == step.TaskName &&
			sameEnvironment(existing.Steps[i].Environment, step.Environment) {
			for name, v := range before {
				if _, ok := given[name]; !ok {
					given[name] = v
				}
			}
		}
		if encoded, err := json.Marshal(given); err == nil {
			steps[i].Params = encoded
		}
	}
	return steps
}

// sealBinding ties a sealed value to its schedule, so one copied into
// another schedule's row fails to open there.
func sealBinding(id int64) []byte { return []byte("scheduled_job:" + strconv.FormatInt(id, 10)) }

// paramsToCheck is in's task parameters with the sensitive values the
// request left out restored from existing, when that is allowed.
//
// A sensitive value is never sent back to a browser, so a form saving a
// change has nothing to send for it. The stored value is kept only for
// the schedule's own owner and only while the task is the same. Anyone
// else must type it: otherwise taking over a schedule would hand over a
// secret its new owner never knew, to send to whichever node they
// chose.
func (s *Service) paramsToCheck(in Input, caller Caller, existing *Schedule) (json.RawMessage, error) {
	given := map[string]any{}
	if len(in.Params) > 0 && string(in.Params) != "null" {
		if err := json.Unmarshal(in.Params, &given); err != nil {
			// Not an object: the task validator says so in its own words.
			return in.Params, nil
		}
	}
	// The placeholder a read returns is not a value anyone means.
	for name, v := range given {
		if v == orchestrator.RedactedValue {
			delete(given, name)
		}
	}
	if existing != nil && len(existing.sealed) > 0 && existing.OwnedBy(caller.Owner) &&
		existing.Kind == orchestrator.JobKindTask && existing.Task == in.Task && sameEnvironment(existing.Environment, in.Environment) {
		kept, err := s.openSealed(*existing)
		if err == nil {
			for name, v := range kept {
				if _, ok := given[name]; !ok {
					given[name] = v
				}
			}
		}
	}
	return json.Marshal(given)
}

func sameEnvironment(stored, given string) bool {
	if given == "" {
		given = orchestrator.DefaultTaskEnvironment
	}
	return stored == given
}

func (s *Service) openSealed(sched Schedule) (map[string]any, error) {
	if len(sched.sealed) == 0 {
		return nil, nil
	}
	if s.sealer == nil {
		return nil, errors.New("the console has no secrets key (CONSOLE_SECRETS_KEY) to read this schedule's sensitive parameters with")
	}
	plain, err := s.sealer.Open(sched.sealed, sealBinding(sched.ID))
	if err != nil {
		return nil, errors.New("this schedule's sensitive parameters could not be read; the secrets key may have changed. Save the schedule again with their values")
	}
	var values map[string]any
	if err := json.Unmarshal(plain, &values); err != nil {
		return nil, fmt.Errorf("decode sensitive parameters: %w", err)
	}
	return values, nil
}

// describe says what a schedule does and when, for the activity log.
func describe(sched Schedule) string {
	what := "a Puppet run"
	switch sched.Kind {
	case orchestrator.JobKindTask:
		what = fmt.Sprintf("task %q", sched.Task)
	case orchestrator.JobKindPlan:
		what = fmt.Sprintf("plan %q (%d steps)", sched.PlanName, len(sched.Steps))
	}
	var on string
	switch {
	case sched.Query != "":
		on = "the nodes a query matches"
	case sched.Group != nil:
		on = fmt.Sprintf("node group %d", *sched.Group)
	default:
		on = fmt.Sprintf("%d named node(s)", len(sched.Targets))
	}
	when := "once at " + sched.StartAt.UTC().Format("2006-01-02 15:04 UTC")
	if sched.Repeat != nil {
		when = fmt.Sprintf("every %d %s(s) from %s", sched.Repeat.Every, sched.Repeat.Unit, sched.StartAt.UTC().Format("2006-01-02 15:04 UTC"))
	}
	return fmt.Sprintf("%s on %s, %s", what, on, when)
}

// --- firing ---

// TickInterval is how often an instance looks for due schedules. A
// variable only so that a test of the whole application need not wait
// for it.
var TickInterval = 20 * time.Second

// Run fires due schedules until ctx is cancelled. Several instances may
// run it at once: each due time is claimed by exactly one of them.
func (s *Service) Run(ctx context.Context) {
	ticker := time.NewTicker(TickInterval)
	defer ticker.Stop()
	for {
		if err := s.FireDue(ctx); err != nil && ctx.Err() == nil {
			s.logger.Warn("failed to fire due schedules", "error", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// FireDue claims and fires every schedule that is due.
func (s *Service) FireDue(ctx context.Context) error {
	now, err := s.now(ctx)
	if err != nil {
		return err
	}
	due, err := s.store.Due(ctx, now)
	if err != nil {
		return err
	}
	for _, sched := range due {
		if err := s.fire(ctx, sched, now); err != nil {
			s.logger.Warn("failed to fire schedule", "schedule", sched.ID, "name", sched.Name, "error", err)
		}
	}
	return nil
}

// fire claims sched's due time and, if this instance won it, carries it
// out. The claim is committed before anything is started - see the
// package comment.
func (s *Service) fire(ctx context.Context, sched Schedule, now time.Time) error {
	if sched.NextRunAt == nil {
		return nil
	}
	dueAt := *sched.NextRunAt
	firingID, claimed, err := s.store.Claim(ctx, sched.ID, dueAt, nextStart(sched.StartAt, sched.Repeat, now))
	if err != nil || !claimed {
		return err
	}

	outcome, detail, jobID := s.carryOut(ctx, sched, dueAt, now)
	if outcome != OutcomeStarted {
		action := "schedule.skipped"
		if outcome == OutcomeMissed {
			action = "schedule.missed"
		}
		s.record(action, sched.Owner, sched, fmt.Sprintf("schedule %q was %s: %s", sched.Name, outcome, detail))
	}
	return s.store.FinishFiring(ctx, firingID, outcome, detail, jobID)
}

// carryOut is a claimed firing: the request its owner could make at
// this moment, or the reason nothing was started.
func (s *Service) carryOut(ctx context.Context, sched Schedule, dueAt, now time.Time) (outcome, detail string, jobID *int64) {
	if late := now.Sub(dueAt); late > LateLimit {
		return OutcomeMissed, fmt.Sprintf("found %s after it was due, with no console instance running the scheduler in between; not started", late.Round(time.Minute)), nil
	}
	running, err := s.store.JobRunning(ctx, sched.ID)
	if err != nil {
		return OutcomeSkipped, err.Error(), nil
	}
	if running {
		return OutcomeSkipped, "the job this schedule started last time is still running", nil
	}

	owner := Owner{Name: sched.Owner, Kind: sched.OwnerKind}
	auth, err := s.authority(ctx, owner)
	if err != nil {
		return OutcomeSkipped, err.Error(), nil
	}
	start := orchestrator.StartRequest{
		Target: sched.Target(), Scope: auth.Scope, Tasks: auth.Tasks, Actor: sched.Owner,
		Schedule: &orchestrator.ScheduleRef{ID: sched.ID, Name: sched.Name},
	}

	var job orchestrator.Job
	switch sched.Kind {
	case orchestrator.JobKindRun:
		job, err = s.starter.StartRun(ctx, orchestrator.RunRequest{StartRequest: start, Options: sched.Options})
	case orchestrator.JobKindTask:
		var params json.RawMessage
		if params, err = s.firingParams(sched); err != nil {
			return OutcomeSkipped, err.Error(), nil
		}
		job, err = s.starter.StartTask(ctx, orchestrator.TaskRequest{
			StartRequest: start, Task: sched.Task, Environment: sched.Environment, Params: params, TimeoutSeconds: sched.TimeoutSeconds,
		})
	case orchestrator.JobKindPlan:
		var steps []orchestrator.PlanStep
		if steps, err = s.firingSteps(sched); err != nil {
			return OutcomeSkipped, err.Error(), nil
		}
		job, err = s.starter.StartPlan(ctx, orchestrator.PlanRequest{StartRequest: start, Name: sched.PlanName, Steps: steps})
	default:
		err = fmt.Errorf("unknown schedule kind %q", sched.Kind)
	}
	if err != nil {
		return OutcomeSkipped, err.Error(), nil
	}
	return OutcomeStarted, "", &job.ID
}

// firingSteps is a scheduled plan's steps with each task step's sealed
// values put back.
func (s *Service) firingSteps(sched Schedule) ([]orchestrator.PlanStep, error) {
	steps := append([]orchestrator.PlanStep(nil), sched.Steps...)
	if len(sched.sealed) == 0 {
		return steps, nil
	}
	sealed, err := s.openSealed(sched)
	if err != nil {
		return nil, err
	}
	for i := range steps {
		sensitive, ok := sealed[strconv.Itoa(i)].(map[string]any)
		if !ok {
			continue
		}
		params := map[string]any{}
		if len(steps[i].Params) > 0 {
			if err := json.Unmarshal(steps[i].Params, &params); err != nil {
				return nil, fmt.Errorf("decode step %d's stored parameters: %w", i+1, err)
			}
		}
		for name, v := range sensitive {
			params[name] = v
		}
		if steps[i].Params, err = json.Marshal(params); err != nil {
			return nil, fmt.Errorf("encode step %d's parameters: %w", i+1, err)
		}
	}
	return steps, nil
}

// firingParams is a scheduled task's parameters with its sealed values
// put back.
func (s *Service) firingParams(sched Schedule) (json.RawMessage, error) {
	if len(sched.sealed) == 0 {
		return sched.Params, nil
	}
	sensitive, err := s.openSealed(sched)
	if err != nil {
		return nil, err
	}
	params := map[string]any{}
	if len(sched.Params) > 0 {
		if err := json.Unmarshal(sched.Params, &params); err != nil {
			return nil, fmt.Errorf("decode stored parameters: %w", err)
		}
	}
	for name, v := range sensitive {
		params[name] = v
	}
	return json.Marshal(params)
}
