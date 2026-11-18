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

	"github.com/voxpupuli/enterprise-console/internal/auditlog"
	"github.com/voxpupuli/enterprise-console/internal/nodetransport"
)

// The patch actions. The node agent has the counterpart; the JSON is
// the contract, as for every other action.
const (
	actionPatchApply  = "patch_apply"
	actionPatchStatus = "patch_status"
)

// Agent run states, as patch_status reports them.
const (
	agentPatchRunning     = "running"
	agentPatchDone        = "done"
	agentPatchFailed      = "failed"
	agentPatchInterrupted = "interrupted"
)

// patchAgentParams is patch_apply's parameters.
type patchAgentParams struct {
	Scope    string   `json:"scope"`
	Packages []string `json:"packages,omitempty"`
	DryRun   bool     `json:"dry_run,omitempty"`
	Reboot   bool     `json:"reboot,omitempty"`
}

// PatchPackage is one package's outcome on one node: what a patch run
// did with it. Outcome is upgraded, would_upgrade, not_upgraded or
// nothing_to_apply.
type PatchPackage struct {
	Name     string `json:"name"`
	From     string `json:"from,omitempty"`
	To       string `json:"to,omitempty"`
	Security bool   `json:"security,omitempty"`
	Outcome  string `json:"outcome"`
}

// PatchPending is one update still pending after a run.
type PatchPending struct {
	Name      string `json:"name"`
	Installed string `json:"installed,omitempty"`
	Available string `json:"available"`
	Security  bool   `json:"security"`
}

// PatchScan is the pending-update scan an agent reports.
type PatchScan struct {
	Manager        string         `json:"manager,omitempty"`
	Unsupported    bool           `json:"unsupported,omitempty"`
	RebootRequired bool           `json:"reboot_required"`
	Updates        []PatchPending `json:"updates"`
}

// patchAgentStatus is the agent's patch_apply acceptance and
// patch_status answer.
type patchAgentStatus struct {
	State          string         `json:"state"`
	Phase          string         `json:"phase,omitempty"`
	Manager        string         `json:"manager,omitempty"`
	Packages       []PatchPackage `json:"packages,omitempty"`
	ExitCode       int            `json:"exit_code"`
	Output         string         `json:"output,omitempty"`
	Error          string         `json:"error,omitempty"`
	RebootRequired bool           `json:"reboot_required"`
	Rebooting      bool           `json:"rebooting,omitempty"`
	Scan           *PatchScan     `json:"scan,omitempty"`
}

type patchAgentResponse struct {
	Result *struct {
		PatchRun *patchAgentStatus `json:"patch_run"`
	} `json:"result"`
	Error string `json:"error"`
}

// PatchResult is what a patch target stores as its result.
type PatchResult struct {
	Manager        string         `json:"manager,omitempty"`
	DryRun         bool           `json:"dryRun,omitempty"`
	Packages       []PatchPackage `json:"packages"`
	RebootRequired bool           `json:"rebootRequired"`
	Rebooted       bool           `json:"rebooted,omitempty"`
}

// PatchInventory is what patch runs need of the patch inventory: to
// record the scan a finished run took, and to scan a node again once it
// has rebooted.
type PatchInventory interface {
	SaveScan(ctx context.Context, certname string, scan PatchScan) error
	// Rescan scans the node and stores the answer. reached is false when
	// the node is not connected, which is not a failure to scan.
	Rescan(ctx context.Context, certname string) (reached bool, err error)
	RecordScanFailure(ctx context.Context, certname, message string) error
}

// Messages recorded against a patch target.
const (
	patchAgentTooOld = "this node's agent client is too old to patch; upgrade it and run again. Nothing was installed."
	patchNotStopped  = "not started: the run stopped after %d failed node(s)"
)

// PatchCoordinator carries a patch run through its batches. Each batch
// is patched at once and in full before the next begins; within a batch
// every node is asked to start, then asked for its status at a regular
// interval until it has finished, each answer being a heartbeat.
//
// A run is followed by the instance that started it. If that instance
// stops, nothing follows it and the reaper fails the job once its
// heartbeat is stale.
type PatchCoordinator struct {
	store     *Store
	transport Transport
	logger    *slog.Logger

	inventory     PatchInventory
	recorder      ActivityRecorder
	auditRecorder AuditRecorder

	// Timing, overridable in tests.
	pollEvery       time.Duration // between status requests
	startTimeout    time.Duration // for the request that starts a node
	pollTimeout     time.Duration // for one status request
	silence         time.Duration // a node unheard of this long has failed
	maxPerNode      time.Duration // a node still running after this has failed
	rebootSettle    time.Duration // before the first scan of a rebooted node
	rebootWait      time.Duration // how long a rebooted node has to return
	rebootPollEvery time.Duration

	mu     sync.Mutex
	runCtx context.Context
	wg     sync.WaitGroup
}

// NewPatchCoordinator builds a PatchCoordinator. logger may be nil.
func NewPatchCoordinator(store *Store, transport Transport, logger *slog.Logger) *PatchCoordinator {
	if logger == nil {
		logger = slog.Default()
	}
	return &PatchCoordinator{
		store: store, transport: transport, logger: logger,
		pollEvery: 15 * time.Second, startTimeout: 2 * time.Minute, pollTimeout: 30 * time.Second,
		silence: 10 * time.Minute, maxPerNode: 4 * time.Hour,
		rebootSettle: time.Minute, rebootWait: 10 * time.Minute, rebootPollEvery: 15 * time.Second,
		runCtx: context.Background(),
	}
}

// SetInventory lets a finished run refresh the patch inventory.
func (c *PatchCoordinator) SetInventory(i PatchInventory) { c.inventory = i }

// SetActivityRecorder records a run's start and end in the activity log.
func (c *PatchCoordinator) SetActivityRecorder(r ActivityRecorder) { c.recorder = r }

// SetAuditRecorder records a run's start and end as audit events.
func (c *PatchCoordinator) SetAuditRecorder(r AuditRecorder) { c.auditRecorder = r }

// Run holds ctx as the context every run is followed under, until it
// ends: a run outlives the request that started it.
func (c *PatchCoordinator) Run(ctx context.Context) {
	c.mu.Lock()
	c.runCtx = ctx
	c.mu.Unlock()
	<-ctx.Done()
}

// Wait waits for every run and every post-reboot scan started so far.
func (c *PatchCoordinator) Wait() { c.wg.Wait() }

func (c *PatchCoordinator) context() context.Context {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.runCtx
}

// Start records the run as triggered and follows it in the background.
func (c *PatchCoordinator) Start(job Job) {
	opts := PatchOptions{}
	if job.PatchOptions != nil {
		opts = *job.PatchOptions
	}
	if c.recorder != nil {
		c.recorder("patch.triggered", job.TriggeredBy, fmt.Sprintf("triggered a patch run (%s) against %s%s",
			opts.describe(), strings.Join(targetNames(job.Targets), ", "), job.originNote()))
	}
	c.audit("patch.triggered", job, map[string]any{
		"scope": opts.Scope, "packages": opts.Packages, "dryRun": opts.DryRun, "reboot": opts.Reboot,
		"batchSize": opts.BatchSize, "stopAfterFailures": opts.StopAfterFailures, "nodes": len(job.Targets),
		"source": job.TargetSource,
	})
	ctx := c.context()
	c.wg.Add(1)
	go func() {
		defer c.wg.Done()
		c.follow(ctx, job, opts)
	}()
}

func (c *PatchCoordinator) audit(action string, job Job, after any) {
	if c.auditRecorder == nil {
		return
	}
	c.auditRecorder(auditlog.Event{
		Action: action, Actor: job.TriggeredBy, ResourceType: "job", ResourceID: strconv.FormatInt(job.ID, 10), After: after,
	})
}

// follow runs the batches of one job to the end.
func (c *PatchCoordinator) follow(ctx context.Context, job Job, opts PatchOptions) {
	batches := map[int][]string{}
	var order []int
	for _, t := range job.Targets {
		b := 1
		if t.Batch != nil {
			b = *t.Batch
		}
		if _, ok := batches[b]; !ok {
			order = append(order, b)
		}
		batches[b] = append(batches[b], t.Certname)
	}

	failed := 0
	stopped := false
	for _, b := range order {
		if ctx.Err() != nil {
			return // shutting down: the reaper fails what is left
		}
		if opts.StopAfterFailures > 0 && failed >= opts.StopAfterFailures {
			stopped = true
			break
		}
		if cur, err := c.store.GetJob(ctx, job.ID); err != nil || cur.Status != StatusRunning {
			return // the reaper already failed it, or it is gone
		}
		var mu sync.Mutex
		var wg sync.WaitGroup
		for _, certname := range batches[b] {
			wg.Add(1)
			go func() {
				defer wg.Done()
				if !c.patchNode(ctx, job, opts, certname) {
					mu.Lock()
					failed++
					mu.Unlock()
				}
			}()
		}
		wg.Wait()
		if ctx.Err() != nil {
			return
		}
		if err := c.store.TouchJob(ctx, job.ID); err != nil {
			c.logger.Warn("failed to record patch heartbeat", "error", err, "jobID", job.ID)
		}
	}

	if stopped {
		if _, err := c.store.CloseNotStarted(ctx, job.ID, fmt.Sprintf(patchNotStopped, opts.StopAfterFailures)); err != nil {
			c.logger.Error("failed to close targets not started", "error", err, "jobID", job.ID)
		}
	}
	status := StatusSucceeded
	if failed > 0 || stopped {
		status = StatusFailed
	}
	if cur, err := c.store.GetJob(ctx, job.ID); err != nil || cur.Status != StatusRunning {
		return
	}
	if err := c.store.CompleteJob(ctx, job.ID, status); err != nil {
		c.logger.Error("failed to complete patch job", "error", err, "jobID", job.ID)
		return
	}
	summary := fmt.Sprintf("patch run %s: %d of %d node(s) failed", status, failed, len(job.Targets))
	if stopped {
		summary += ", stopped before the last batch"
	}
	if c.recorder != nil {
		c.recorder("patch."+status, job.TriggeredBy, summary)
	}
	c.audit("patch."+status, job, nil)
}

// patchNode patches one node and records its result. It returns whether
// the node was patched. A node ends failed with a reason, whatever goes
// wrong; only the console shutting down leaves it open.
func (c *PatchCoordinator) patchNode(ctx context.Context, job Job, opts PatchOptions, certname string) bool {
	fail := func(reason string, result any) bool {
		var raw json.RawMessage
		if result != nil {
			raw, _ = json.Marshal(result)
		}
		if err := c.store.RecordTaskResult(ctx, job.ID, certname, StatusFailed, nil, "", reason, raw); err != nil {
			c.logger.Error("failed to record patch failure", "error", err, "jobID", job.ID, "certname", certname)
		}
		return false
	}
	if err := c.store.StartPatchTarget(ctx, job.ID, certname); err != nil {
		c.logger.Error("failed to start patch target", "error", err, "jobID", job.ID, "certname", certname)
		return false
	}

	params, _ := json.Marshal(patchAgentParams{Scope: opts.Scope, Packages: opts.Packages, DryRun: opts.DryRun, Reboot: opts.Reboot})
	st, reason := c.ask(ctx, certname, actionPatchApply, params, c.startTimeout)
	if ctx.Err() != nil {
		return false
	}
	if reason != "" {
		return fail(reason, nil)
	}
	if st.State != agentPatchRunning {
		return fail(fmt.Sprintf("the node did not accept the patch run (state %q)", st.State), nil)
	}

	begun := time.Now()
	lastHeard := begun
	ticker := time.NewTicker(c.pollEvery)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return false
		case <-ticker.C:
		}
		if time.Since(begun) > c.maxPerNode {
			return fail(fmt.Sprintf("the node had not finished patching after %s", c.maxPerNode), nil)
		}
		st, reason := c.ask(ctx, certname, actionPatchStatus, nil, c.pollTimeout)
		if ctx.Err() != nil {
			return false
		}
		if reason != "" {
			if time.Since(lastHeard) > c.silence {
				return fail(fmt.Sprintf("the node stopped answering: no answer for %s (%s)", c.silence, reason), nil)
			}
			continue
		}
		lastHeard = time.Now()
		switch st.State {
		case agentPatchRunning:
			if err := c.store.TouchTarget(ctx, job.ID, certname); err != nil {
				c.logger.Warn("failed to record target heartbeat", "error", err, "jobID", job.ID)
			}
		case agentPatchDone, agentPatchFailed:
			return c.finishNode(ctx, job, opts, certname, st)
		case agentPatchInterrupted:
			return fail("the node agent restarted while patching; the packages may be partly installed. "+st.Error, nil)
		default:
			return fail(fmt.Sprintf("the node agent has no record of this run (state %q)", st.State), nil)
		}
	}
}

// ask sends one patch request to a node and reads the answer. A non-empty
// reason says why there is no usable answer, in words for the target.
func (c *PatchCoordinator) ask(ctx context.Context, certname, action string, params json.RawMessage, timeout time.Duration) (*patchAgentStatus, string) {
	payload, _ := json.Marshal(requestData{Module: moduleNamePuppet, Action: action, Params: params})
	raw, err := c.transport.Dispatch(ctx, certname, payload, timeout)
	switch {
	case errors.Is(err, nodetransport.ErrNodeNotConnected):
		return nil, "agent not connected"
	case err != nil:
		return nil, fmt.Sprintf("dispatch failed: %v", err)
	}
	var resp patchAgentResponse
	if err := json.Unmarshal(raw, &resp); err != nil {
		return nil, fmt.Sprintf("invalid response: %v", err)
	}
	if resp.Error != "" {
		if strings.Contains(resp.Error, "unsupported action") && strings.Contains(resp.Error, "patch_") {
			return nil, patchAgentTooOld
		}
		return nil, resp.Error
	}
	if resp.Result == nil || resp.Result.PatchRun == nil {
		return nil, "response carried no patch run status"
	}
	return resp.Result.PatchRun, ""
}

// finishNode records a node's final result, refreshes its inventory and,
// for a node that is rebooting, scans it again when it returns.
func (c *PatchCoordinator) finishNode(ctx context.Context, job Job, opts PatchOptions, certname string, st *patchAgentStatus) bool {
	result := PatchResult{Manager: st.Manager, DryRun: opts.DryRun, Packages: st.Packages, RebootRequired: st.RebootRequired, Rebooted: st.Rebooting}
	if result.Packages == nil {
		result.Packages = []PatchPackage{}
	}
	raw, _ := json.Marshal(result)
	status, detail := StatusSucceeded, ""
	if st.State == agentPatchFailed {
		status, detail = StatusFailed, st.Error
		if detail == "" {
			detail = "the patch run failed"
		}
	}
	exit := st.ExitCode
	if err := c.store.RecordTaskResult(ctx, job.ID, certname, status, &exit, st.Output, detail, raw); err != nil {
		c.logger.Error("failed to record patch result", "error", err, "jobID", job.ID, "certname", certname)
	}
	if c.inventory != nil && st.Scan != nil {
		if err := c.inventory.SaveScan(ctx, certname, *st.Scan); err != nil {
			c.logger.Warn("failed to refresh the patch inventory", "error", err, "certname", certname)
		}
	}
	if st.Rebooting && c.inventory != nil {
		c.wg.Add(1)
		go func() {
			defer c.wg.Done()
			c.rescanAfterReboot(ctx, certname)
		}()
	}
	return status == StatusSucceeded
}

// rescanAfterReboot scans a node that is rebooting once it is back,
// waiting up to rebootWait for it. A node that does not return has its
// snapshot say the scan failed.
func (c *PatchCoordinator) rescanAfterReboot(ctx context.Context, certname string) {
	wait := func(d time.Duration) bool {
		t := time.NewTimer(d)
		defer t.Stop()
		select {
		case <-ctx.Done():
			return false
		case <-t.C:
			return true
		}
	}
	if !wait(c.rebootSettle) {
		return
	}
	deadline := time.Now().Add(c.rebootWait)
	for {
		reached, err := c.inventory.Rescan(ctx, certname)
		if reached && err == nil {
			return
		}
		if time.Now().After(deadline) {
			msg := fmt.Sprintf("the node did not come back within %s of rebooting, so its updates were not scanned again", c.rebootWait)
			if err != nil {
				msg += ": " + err.Error()
			}
			if rerr := c.inventory.RecordScanFailure(ctx, certname, msg); rerr != nil {
				c.logger.Warn("failed to record the scan failure", "error", rerr, "certname", certname)
			}
			return
		}
		if !wait(c.rebootPollEvery) {
			return
		}
	}
}
