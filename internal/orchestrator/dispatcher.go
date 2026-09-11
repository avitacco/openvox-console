package orchestrator

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/voxpupuli/enterprise-console/internal/auditlog"
	"github.com/voxpupuli/enterprise-console/internal/nodetransport"
)

// dispatchTimeout bounds how long Dispatcher waits for a node-agent's
// response to one request. Generous (a Puppet run can legitimately take
// a while) - a safety bound against a request that silently never gets a
// reply, not a realistic run-duration limit. An earlier dispatcher
// this replaced had no such bound at all (it waited indefinitely on a
// long-lived connection's inbox) - NATS's request-reply model needs an
// explicit one.
const dispatchTimeout = time.Hour

// ActivityRecorder records one activity log entry. Unlike the
// `func(r *http.Request, action, summary string)` closure classifier
// and codemanager use, this takes actor directly rather than deriving
// it from a request's claims - a job's triggering actor is already
// resolved and stored on the Job (Job.TriggeredBy) by the time the
// Dispatcher fires a completion event asynchronously, well outside any
// HTTP request's lifetime. See design.md/tasks.md 9.2.
type ActivityRecorder func(action, actor, summary string)

// AuditRecorder records one audit event (configurable-audit-logging,
// bound to the orchestrator category) - the same "actor/resource passed
// directly, not derived from a request" reasoning as ActivityRecorder
// applies here too.
type AuditRecorder func(auditlog.Event)

// Transport is the subset of *nodetransport.Server the dispatcher
// depends on, kept as an interface (mirroring the old
// connectivity lookup) so dispatcher tests don't need a real
// embedded NATS server.
type Transport interface {
	Dispatch(ctx context.Context, certname string, payload []byte, timeout time.Duration) ([]byte, error)
}

// Dispatcher sends dispatch requests to connected nodes (via Transport)
// and processes their responses - each dispatch is a single blocking
// Transport.Dispatch call in its own goroutine (see dispatchOne),
// feeding results back through outcomes to be handled one at a time by
// Run - the same serialization guarantee previously relied on for
// free from consuming a single shared inbox channel.
type Dispatcher struct {
	store         *Store
	transport     Transport
	outcomes      chan dispatchOutcome
	logger        *slog.Logger
	correlator    *ReportCorrelator // optional - see SetReportCorrelator
	recorder      ActivityRecorder  // optional - see SetActivityRecorder
	auditRecorder AuditRecorder     // optional - see SetAuditRecorder

	mu      sync.Mutex
	pending map[string]struct{} // in-flight dispatch tracking IDs, for PendingCount
	runCtx  context.Context     // set by Run - see dispatchOne's doc comment
}

// dispatchOutcome is one target's Transport.Dispatch result, fed onto
// Dispatcher.outcomes for serialized handling by Run.
type dispatchOutcome struct {
	jobID    int64
	certname string
	action   string
	started  time.Time
	data     []byte
	err      error
}

// NewDispatcher builds a Dispatcher. logger may be nil (slog.Default()
// is used).
func NewDispatcher(store *Store, transport Transport, logger *slog.Logger) *Dispatcher {
	if logger == nil {
		logger = slog.Default()
	}
	return &Dispatcher{
		store:     store,
		transport: transport,
		outcomes:  make(chan dispatchOutcome),
		logger:    logger,
		pending:   make(map[string]struct{}),
		// Overwritten by Run - a safe non-nil default in case a dispatch
		// is somehow triggered before Run starts (shouldn't happen in
		// practice; every caller starts Run immediately at startup).
		runCtx: context.Background(),
	}
}

// SetReportCorrelator attaches a ReportCorrelator, enabling automatic
// report correlation for successful run targets (see
// specs/orchestrator's "Job result correlation with reports"
// requirement). Optional - if never called, jobs still complete
// normally, just without a linked report.
func (d *Dispatcher) SetReportCorrelator(c *ReportCorrelator) {
	d.correlator = c
}

// SetActivityRecorder attaches an ActivityRecorder, enabling activity
// log entries for job triggers and terminal outcomes (see
// specs/orchestrator's "Job actions recorded in the activity log"
// requirement). Optional - if never called, jobs still run normally,
// just without activity log entries.
func (d *Dispatcher) SetActivityRecorder(r ActivityRecorder) {
	d.recorder = r
}

func (d *Dispatcher) recordActivity(action string, job Job, summary string) {
	if d.recorder == nil {
		return
	}
	d.recorder(action, job.TriggeredBy, summary)
}

// SetAuditRecorder attaches an AuditRecorder, enabling audit events for
// job triggers and terminal outcomes (configurable-audit-logging).
// Optional - if never called, jobs still run normally, just without
// audit events.
func (d *Dispatcher) SetAuditRecorder(r AuditRecorder) {
	d.auditRecorder = r
}

func (d *Dispatcher) recordAuditEvent(action string, job Job) {
	if d.auditRecorder == nil {
		return
	}
	d.auditRecorder(auditlog.Event{
		Action: action, Actor: job.TriggeredBy,
		ResourceType: "job", ResourceID: strconv.FormatInt(job.ID, 10),
	})
}

// PendingCount reports the number of dispatch requests this instance
// currently has dispatched and awaiting a response - used to back the
// console_orchestrator_pending_dispatches metrics gauge. Only this
// instance's own dispatches, per operations.md's statelessness boundary
// - a response only ever arrives on the connection the dispatching
// instance itself holds.
func (d *Dispatcher) PendingCount() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return len(d.pending)
}

func targetNames(targets []JobTarget) []string {
	names := make([]string, len(targets))
	for i, t := range targets {
		names[i] = t.Certname
	}
	return names
}

// Run consumes outcomes until ctx is cancelled, handling each one at a
// time and updating Store. ctx is also the long-lived context every
// background dispatch (see dispatchOne) runs under - not whatever
// request-scoped context happened to trigger it, since a dispatch
// legitimately outlives the HTTP request (or plan-step completion, ...)
// that started it.
func (d *Dispatcher) Run(ctx context.Context) {
	d.mu.Lock()
	d.runCtx = ctx
	d.mu.Unlock()

	for {
		select {
		case <-ctx.Done():
			return
		case outcome, ok := <-d.outcomes:
			if !ok {
				return
			}
			d.handleOutcome(ctx, outcome)
		}
	}
}

// DispatchRun sends a run request to every target of job (see
// specs/orchestrator's "On-demand Puppet run" requirement). A target
// with no live connection is immediately recorded as failed, without
// blocking the other targets.
func (d *Dispatcher) DispatchRun(ctx context.Context, job Job) {
	if job.ParentJobID == nil {
		d.recordActivity("run.triggered", job, fmt.Sprintf("triggered a run against %s", strings.Join(targetNames(job.Targets), ", ")))
		d.recordAuditEvent("run.triggered", job)
	}
	for _, target := range job.Targets {
		d.dispatchOne(ctx, job.ID, target.Certname, actionRun, nil)
	}
}

// DispatchTask sends a task request (see specs/orchestrator's "Task
// and plan execution" requirement) to every target of job.
func (d *Dispatcher) DispatchTask(ctx context.Context, job Job, taskName string, params json.RawMessage) {
	if job.ParentJobID == nil {
		d.recordActivity("task.triggered", job, fmt.Sprintf("triggered task %q against %s", taskName, strings.Join(targetNames(job.Targets), ", ")))
		d.recordAuditEvent("task.triggered", job)
	}
	data, err := json.Marshal(taskParams{Task: taskName, Params: params})
	if err != nil {
		for _, target := range job.Targets {
			d.finishTarget(ctx, job.ID, target.Certname, StatusFailed, nil, "", fmt.Sprintf("failed to encode task params: %v", err))
		}
		return
	}
	for _, target := range job.Targets {
		d.dispatchOne(ctx, job.ID, target.Certname, actionRunTask, data)
	}
}

// DispatchPlan starts a plan's execution by dispatching its first step
// (see specs/orchestrator's "A plan runs its steps in the declared
// order" scenario). Subsequent steps are dispatched automatically as
// each prior step completes - see maybeCompleteJob/advancePlan.
func (d *Dispatcher) DispatchPlan(ctx context.Context, planJob Job) {
	d.recordActivity("plan.triggered", planJob, fmt.Sprintf("triggered plan %q", planJob.PlanName))
	d.recordAuditEvent("plan.triggered", planJob)

	steps, err := d.store.GetPlanSteps(ctx, planJob.ID)
	if err != nil {
		d.logger.Error("failed to load plan steps", "error", err, "planJobID", planJob.ID)
		return
	}
	if len(steps) == 0 {
		d.logger.Error("plan has no steps to dispatch", "planJobID", planJob.ID)
		return
	}
	d.dispatchStep(ctx, steps[0])
}

func (d *Dispatcher) dispatchStep(ctx context.Context, step Job) {
	switch step.Kind {
	case JobKindTask:
		d.DispatchTask(ctx, step, step.TaskName, step.Params)
	default:
		d.DispatchRun(ctx, step)
	}
}

// dispatchOne dispatches one request to certname's node-agent in its own
// goroutine (so multiple targets run concurrently - see the Dispatcher
// doc comment), feeding the result back through d.outcomes for Run to
// handle serially. The actual dispatch runs under d.runCtx (Run's own
// long-lived context, set once at startup), not ctx - ctx here is
// whatever triggered this specific call (an HTTP request, a plan step
// continuing after a prior one, ...) and may already be done by the
// time a node responds, but the dispatch itself must not be cancelled
// just because its trigger's own context ended.
func (d *Dispatcher) dispatchOne(ctx context.Context, jobID int64, certname, action string, params json.RawMessage) {
	reqData, err := json.Marshal(requestData{
		Module: moduleNamePuppet,
		Action: action,
		Params: params,
	})
	if err != nil {
		d.finishTarget(ctx, jobID, certname, StatusFailed, nil, "", fmt.Sprintf("failed to encode request: %v", err))
		return
	}

	trackingID := uuid.NewString()
	started := time.Now().UTC()
	d.mu.Lock()
	d.pending[trackingID] = struct{}{}
	runCtx := d.runCtx
	d.mu.Unlock()

	go func() {
		defer func() {
			d.mu.Lock()
			delete(d.pending, trackingID)
			d.mu.Unlock()
		}()

		data, err := d.transport.Dispatch(runCtx, certname, reqData, dispatchTimeout)
		select {
		case d.outcomes <- dispatchOutcome{jobID: jobID, certname: certname, action: action, started: started, data: data, err: err}:
		case <-runCtx.Done():
		}
	}()
}

func (d *Dispatcher) handleOutcome(ctx context.Context, o dispatchOutcome) {
	if o.err != nil {
		reason := fmt.Sprintf("dispatch failed: %v", o.err)
		if errors.Is(o.err, nodetransport.ErrNodeNotConnected) {
			reason = "agent not connected"
		}
		d.finishTarget(ctx, o.jobID, o.certname, StatusFailed, nil, "", reason)
		return
	}

	var resp wireResponse
	if err := json.Unmarshal(o.data, &resp); err != nil {
		d.finishTarget(ctx, o.jobID, o.certname, StatusFailed, nil, "", fmt.Sprintf("invalid response: %v", err))
		return
	}
	if resp.Error != "" {
		// Covers both a genuine execution error and a "busy" rejection
		// (see specs/node-agent's "Single in-flight request per client"
		// requirement) - both are terminal failures for this target in
		// this phase; no automatic retry.
		d.finishTarget(ctx, o.jobID, o.certname, StatusFailed, nil, "", resp.Error)
		return
	}
	if resp.Result == nil {
		d.finishTarget(ctx, o.jobID, o.certname, StatusFailed, nil, "", "response carried neither a result nor an error")
		return
	}

	status := StatusSucceeded
	if puppetRunFailed(resp.Result.Output.ExitCode) {
		status = StatusFailed
	}
	exitCode := resp.Result.Output.ExitCode
	d.finishTarget(ctx, o.jobID, o.certname, status, &exitCode, resp.Result.Output.Stdout, resp.Result.Output.Stderr)

	if status == StatusSucceeded && o.action == actionRun && d.correlator != nil {
		// ctx here is Dispatcher.Run's own long-lived context, so
		// correlation naturally stops if the dispatcher does -
		// intentional, not a context misuse.
		go d.correlator.Correlate(ctx, o.jobID, o.certname, o.started)
	}
}

func (d *Dispatcher) finishTarget(ctx context.Context, jobID int64, certname, status string, exitCode *int, output, errorDetail string) {
	if err := d.store.RecordTargetResult(ctx, jobID, certname, status, exitCode, output, errorDetail); err != nil {
		d.logger.Error("failed to record job target result", "error", err, "jobID", jobID, "certname", certname)
	}
	d.maybeCompleteJob(ctx, jobID)
}

// maybeCompleteJob completes jobID once every target has reached a
// terminal status, rolling up to StatusFailed if any target failed,
// StatusSucceeded only if every target did.
func (d *Dispatcher) maybeCompleteJob(ctx context.Context, jobID int64) {
	job, err := d.store.GetJob(ctx, jobID)
	if err != nil {
		d.logger.Error("failed to load job for completion check", "error", err, "jobID", jobID)
		return
	}

	anyFailed := false
	for _, target := range job.Targets {
		if target.Status != StatusSucceeded && target.Status != StatusFailed {
			return // at least one target is still pending/running
		}
		if target.Status == StatusFailed {
			anyFailed = true
		}
	}

	finalStatus := StatusSucceeded
	if anyFailed {
		finalStatus = StatusFailed
	}
	if err := d.store.CompleteJob(ctx, jobID, finalStatus); err != nil {
		d.logger.Error("failed to complete job", "error", err, "jobID", jobID)
		return
	}

	if job.ParentJobID == nil {
		action := string(job.Kind) + "." + finalStatus
		d.recordActivity(action, job, fmt.Sprintf("%s %s: %s", job.Kind, finalStatus, strings.Join(targetNames(job.Targets), ", ")))
		d.recordAuditEvent(action, job)
	} else if job.StepOrder != nil {
		d.advancePlan(ctx, *job.ParentJobID, *job.StepOrder, finalStatus)
	}
}

// advancePlan runs after one of a plan's step jobs completes: on
// failure, the plan fails immediately without dispatching further
// steps (fail-fast - no retry/continue policy is specified); on
// success, it dispatches the next step, or completes the plan itself
// if completedStepOrder was the last one (see specs/orchestrator's "A
// plan runs its steps in the declared order" scenario).
func (d *Dispatcher) advancePlan(ctx context.Context, planJobID int64, completedStepOrder int, completedStepStatus string) {
	if completedStepStatus == StatusFailed {
		d.completePlan(ctx, planJobID, StatusFailed, "a step failed")
		return
	}

	steps, err := d.store.GetPlanSteps(ctx, planJobID)
	if err != nil {
		d.logger.Error("failed to load plan steps to advance", "error", err, "planJobID", planJobID)
		return
	}

	for _, step := range steps {
		if step.StepOrder != nil && *step.StepOrder == completedStepOrder+1 {
			d.dispatchStep(ctx, step)
			return
		}
	}

	// No next step found - completedStepOrder was the last one.
	d.completePlan(ctx, planJobID, StatusSucceeded, "all steps completed")
}

func (d *Dispatcher) completePlan(ctx context.Context, planJobID int64, status, reason string) {
	if err := d.store.CompleteJob(ctx, planJobID, status); err != nil {
		d.logger.Error("failed to complete plan", "error", err, "planJobID", planJobID, "status", status)
		return
	}
	plan, err := d.store.GetJob(ctx, planJobID)
	if err != nil {
		d.logger.Error("failed to load plan for activity recording", "error", err, "planJobID", planJobID)
		return
	}
	action := "plan." + status
	d.recordActivity(action, plan, fmt.Sprintf("plan %q %s (%s)", plan.PlanName, status, reason))
	d.recordAuditEvent(action, plan)
}
