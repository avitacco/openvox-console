package orchestrator

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/voxpupuli/enterprise-console/internal/auditlog"
	"github.com/voxpupuli/enterprise-console/internal/nodetransport"
)

// patchNodeScript says how one fake node answers: the apply request, then
// each status request in turn (the last answer repeats).
type patchNodeScript struct {
	applyErr    string
	unreachable bool
	statuses    []patchAgentStatus
	statusErr   bool // every status request fails
	next        int
	started     bool
}

type fakePatchFleet struct {
	mu      sync.Mutex
	nodes   map[string]*patchNodeScript
	active  int // nodes between apply and their final status
	maxSeen int
	order   []string // nodes in the order they were started
	applied []patchAgentParams
}

func (f *fakePatchFleet) Dispatch(_ context.Context, certname string, payload []byte, _ time.Duration) ([]byte, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	n, ok := f.nodes[certname]
	if !ok || n.unreachable {
		return nil, nodetransport.ErrNodeNotConnected
	}
	var req requestData
	_ = json.Unmarshal(payload, &req)
	reply := func(st *patchAgentStatus, errText string) ([]byte, error) {
		var resp patchAgentResponse
		resp.Error = errText
		if st != nil {
			resp.Result = &struct {
				PatchRun *patchAgentStatus `json:"patch_run"`
			}{PatchRun: st}
		}
		return json.Marshal(resp)
	}
	switch req.Action {
	case actionPatchApply:
		if n.applyErr != "" {
			return reply(nil, n.applyErr)
		}
		var p patchAgentParams
		_ = json.Unmarshal(req.Params, &p)
		f.applied = append(f.applied, p)
		n.started = true
		f.order = append(f.order, certname)
		f.active++
		f.maxSeen = max(f.maxSeen, f.active)
		return reply(&patchAgentStatus{State: agentPatchRunning}, "")
	case actionPatchStatus:
		if n.statusErr {
			return nil, errors.New("timeout")
		}
		i := min(n.next, len(n.statuses)-1)
		n.next++
		st := n.statuses[i]
		if st.State != agentPatchRunning && i == n.next-1 && n.started {
			n.started = false
			f.active--
		}
		return reply(&st, "")
	}
	return nil, errors.New("unexpected action " + req.Action)
}

func (f *fakePatchFleet) started(certname string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, c := range f.order {
		if c == certname {
			return true
		}
	}
	return false
}

type fakePatchInventory struct {
	mu       sync.Mutex
	saved    map[string]PatchScan
	rescans  []string
	reached  bool
	failures map[string]string
}

func (i *fakePatchInventory) SaveScan(_ context.Context, c string, s PatchScan) error {
	i.mu.Lock()
	defer i.mu.Unlock()
	if i.saved == nil {
		i.saved = map[string]PatchScan{}
	}
	i.saved[c] = s
	return nil
}
func (i *fakePatchInventory) Rescan(_ context.Context, c string) (bool, error) {
	i.mu.Lock()
	defer i.mu.Unlock()
	i.rescans = append(i.rescans, c)
	return i.reached, nil
}
func (i *fakePatchInventory) RecordScanFailure(_ context.Context, c, m string) error {
	i.mu.Lock()
	defer i.mu.Unlock()
	if i.failures == nil {
		i.failures = map[string]string{}
	}
	i.failures[c] = m
	return nil
}

var (
	doneOK   = patchAgentStatus{State: agentPatchDone, Packages: []PatchPackage{{Name: "curl", From: "1", To: "2", Outcome: "upgraded"}}, Scan: &PatchScan{Manager: "apt", Updates: []PatchPending{}}}
	runningS = patchAgentStatus{State: agentPatchRunning, Phase: "installing"}
	failedS  = patchAgentStatus{State: agentPatchFailed, ExitCode: 100, Error: "the package manager exited with status 100", Output: "E: broken"}
)

func okNode() *patchNodeScript {
	return &patchNodeScript{statuses: []patchAgentStatus{runningS, doneOK}}
}
func badNode() *patchNodeScript { return &patchNodeScript{statuses: []patchAgentStatus{failedS}} }

func newCoordinator(t *testing.T, fleet *fakePatchFleet) (*PatchCoordinator, *Store) {
	t.Helper()
	s := testStore(t)
	c := NewPatchCoordinator(s, fleet, nil)
	c.pollEvery, c.startTimeout, c.pollTimeout = 5*time.Millisecond, time.Second, time.Second
	c.silence, c.maxPerNode = time.Hour, time.Hour
	c.rebootSettle, c.rebootWait, c.rebootPollEvery = time.Millisecond, 50*time.Millisecond, 5*time.Millisecond
	return c, s
}

func runPatch(t *testing.T, c *PatchCoordinator, s *Store, opts PatchOptions, nodes ...string) Job {
	t.Helper()
	job, err := s.CreatePatchJob(context.Background(), opts, nodes, "tester", JobOrigin{})
	if err != nil {
		t.Fatal(err)
	}
	c.Start(job)
	c.Wait()
	got, err := s.GetJob(context.Background(), job.ID)
	if err != nil {
		t.Fatal(err)
	}
	return got
}

func target(j Job, certname string) JobTarget {
	for _, t := range j.Targets {
		if t.Certname == certname {
			return t
		}
	}
	return JobTarget{}
}

func TestPatchRun_BatchesRunInTurn(t *testing.T) {
	fleet := &fakePatchFleet{nodes: map[string]*patchNodeScript{}}
	names := []string{"pc1-a", "pc1-b", "pc1-c", "pc1-d", "pc1-e"}
	for _, n := range names {
		fleet.nodes[n] = okNode()
	}
	c, s := newCoordinator(t, fleet)
	job := runPatch(t, c, s, PatchOptions{Scope: PatchScopeAll, BatchSize: 2, StopAfterFailures: 3}, names...)
	if job.Status != StatusSucceeded || job.FinishedAt == nil {
		t.Fatalf("job = %s", job.Status)
	}
	if fleet.maxSeen > 2 {
		t.Errorf("%d nodes were being patched at once, want at most 2", fleet.maxSeen)
	}
	if len(fleet.order) != 5 {
		t.Errorf("start order = %v", fleet.order)
	}
	for _, n := range names {
		tg := target(job, n)
		if tg.Status != StatusSucceeded || tg.ExitCode == nil {
			t.Errorf("%s = %s", n, tg.Status)
		}
		var res PatchResult
		if err := json.Unmarshal(tg.Result, &res); err != nil || len(res.Packages) != 1 || res.Packages[0].To != "2" {
			t.Errorf("%s result = %s", n, tg.Result)
		}
	}
	if fleet.applied[0].Scope != "all" {
		t.Errorf("agent was asked for %+v", fleet.applied[0])
	}
}

func TestPatchRun_StopsAtTheThreshold(t *testing.T) {
	fleet := &fakePatchFleet{nodes: map[string]*patchNodeScript{}}
	names := []string{"pc2-a", "pc2-b", "pc2-c", "pc2-d", "pc2-e", "pc2-f", "pc2-g", "pc2-h", "pc2-i", "pc2-j"}
	for i, n := range names {
		fleet.nodes[n] = okNode()
		if i < 2 {
			fleet.nodes[n] = badNode()
		}
	}
	c, s := newCoordinator(t, fleet)
	job := runPatch(t, c, s, PatchOptions{Scope: PatchScopeAll, BatchSize: 2, StopAfterFailures: 2}, names...)
	if job.Status != StatusFailed {
		t.Fatalf("job = %s", job.Status)
	}
	for _, n := range names[2:] {
		tg := target(job, n)
		if tg.Status != StatusFailed || !strings.HasPrefix(tg.ErrorDetail, "not started") || fleet.started(n) {
			t.Errorf("%s = %s %q started=%v", n, tg.Status, tg.ErrorDetail, fleet.started(n))
		}
	}
	if got := target(job, "pc2-a"); got.ErrorDetail == "" || got.ExitCode == nil || *got.ExitCode != 100 || got.Output != "E: broken" {
		t.Errorf("failed node = %+v", got)
	}
}

func TestPatchRun_ZeroThresholdNeverStops(t *testing.T) {
	fleet := &fakePatchFleet{nodes: map[string]*patchNodeScript{"pc3-a": badNode(), "pc3-b": badNode(), "pc3-c": okNode()}}
	c, s := newCoordinator(t, fleet)
	job := runPatch(t, c, s, PatchOptions{Scope: PatchScopeAll, BatchSize: 1, StopAfterFailures: 0}, "pc3-a", "pc3-b", "pc3-c")
	if target(job, "pc3-c").Status != StatusSucceeded || job.Status != StatusFailed {
		t.Errorf("job %s, last node %s", job.Status, target(job, "pc3-c").Status)
	}
}

func TestPatchRun_AMidRunStatusKeepsTheHeartbeat(t *testing.T) {
	long := &patchNodeScript{}
	for i := 0; i < 8; i++ {
		long.statuses = append(long.statuses, runningS)
	}
	long.statuses = append(long.statuses, doneOK)
	fleet := &fakePatchFleet{nodes: map[string]*patchNodeScript{"pc4-a": long}}
	c, s := newCoordinator(t, fleet)
	job, _ := s.CreatePatchJob(context.Background(), PatchOptions{Scope: PatchScopeAll, BatchSize: 1}, []string{"pc4-a"}, "tester", JobOrigin{})
	first := *job.HeartbeatAt
	c.Start(job)
	time.Sleep(25 * time.Millisecond)
	mid, _ := s.GetJob(context.Background(), job.ID)
	if mid.Status != StatusRunning || mid.HeartbeatAt == nil || !mid.HeartbeatAt.After(first) || target(mid, "pc4-a").HeartbeatAt == nil {
		t.Errorf("mid-run: status %s heartbeat %v (started %v)", mid.Status, mid.HeartbeatAt, first)
	}
	c.Wait()
}

func TestPatchRun_ANodeThatStopsAnsweringFails(t *testing.T) {
	fleet := &fakePatchFleet{nodes: map[string]*patchNodeScript{"pc5-a": {statuses: []patchAgentStatus{runningS}, statusErr: true}}}
	c, s := newCoordinator(t, fleet)
	c.silence = 30 * time.Millisecond
	job := runPatch(t, c, s, PatchOptions{Scope: PatchScopeAll, BatchSize: 1}, "pc5-a")
	if tg := target(job, "pc5-a"); tg.Status != StatusFailed || !strings.Contains(tg.ErrorDetail, "stopped answering") {
		t.Errorf("target = %s %q", tg.Status, tg.ErrorDetail)
	}
}

func TestPatchRun_ANodeStillRunningAtTheLimitFails(t *testing.T) {
	fleet := &fakePatchFleet{nodes: map[string]*patchNodeScript{"pc6-a": {statuses: []patchAgentStatus{runningS}}}}
	c, s := newCoordinator(t, fleet)
	c.maxPerNode = 30 * time.Millisecond
	job := runPatch(t, c, s, PatchOptions{Scope: PatchScopeAll, BatchSize: 1}, "pc6-a")
	if tg := target(job, "pc6-a"); tg.Status != StatusFailed || !strings.Contains(tg.ErrorDetail, "had not finished") {
		t.Errorf("target = %s %q", tg.Status, tg.ErrorDetail)
	}
}

func TestPatchRun_RefusedNodes(t *testing.T) {
	fleet := &fakePatchFleet{nodes: map[string]*patchNodeScript{
		"pc7-old":  {applyErr: `unsupported action "patch_apply"`},
		"pc7-nopm": {applyErr: "this node has no package manager the console can patch (apt, dnf or yum)"},
		"pc7-busy": {applyErr: "node-agent is busy executing another request"},
		"pc7-off":  {unreachable: true},
	}}
	c, s := newCoordinator(t, fleet)
	job := runPatch(t, c, s, PatchOptions{Scope: PatchScopeAll, BatchSize: 10}, "pc7-old", "pc7-nopm", "pc7-busy", "pc7-off")
	want := map[string]string{
		"pc7-old": patchAgentTooOld, "pc7-nopm": "no package manager", "pc7-busy": "busy", "pc7-off": "agent not connected",
	}
	for n, sub := range want {
		if tg := target(job, n); tg.Status != StatusFailed || !strings.Contains(tg.ErrorDetail, sub) {
			t.Errorf("%s = %s %q, want %q", n, tg.Status, tg.ErrorDetail, sub)
		}
	}
	if len(fleet.applied) != 0 {
		t.Errorf("a refused node was patched: %v", fleet.applied)
	}
}

func TestPatchRun_RefreshesTheInventory(t *testing.T) {
	fleet := &fakePatchFleet{nodes: map[string]*patchNodeScript{"pc8-a": okNode()}}
	c, s := newCoordinator(t, fleet)
	inv := &fakePatchInventory{}
	c.SetInventory(inv)
	runPatch(t, c, s, PatchOptions{Scope: PatchScopeAll, BatchSize: 1}, "pc8-a")
	if got, ok := inv.saved["pc8-a"]; !ok || got.Manager != "apt" {
		t.Errorf("saved = %+v", inv.saved)
	}
	if len(inv.rescans) != 0 {
		t.Errorf("a node that did not reboot was scanned again: %v", inv.rescans)
	}
}

func TestPatchRun_AfterARebootTheNodeIsScannedAgain(t *testing.T) {
	rebooting := doneOK
	rebooting.Rebooting, rebooting.RebootRequired = true, true
	fleet := &fakePatchFleet{nodes: map[string]*patchNodeScript{"pc9-a": {statuses: []patchAgentStatus{rebooting}}}}
	c, s := newCoordinator(t, fleet)
	inv := &fakePatchInventory{reached: true}
	c.SetInventory(inv)
	job := runPatch(t, c, s, PatchOptions{Scope: PatchScopeAll, BatchSize: 1, Reboot: true}, "pc9-a")
	var res PatchResult
	_ = json.Unmarshal(target(job, "pc9-a").Result, &res)
	if target(job, "pc9-a").Status != StatusSucceeded || !res.Rebooted {
		t.Errorf("target = %s %+v", target(job, "pc9-a").Status, res)
	}
	if len(inv.rescans) != 1 || len(inv.failures) != 0 {
		t.Errorf("rescans %v failures %v", inv.rescans, inv.failures)
	}
}

func TestPatchRun_ANodeThatDoesNotReturnHasItsScanFail(t *testing.T) {
	rebooting := doneOK
	rebooting.Rebooting = true
	fleet := &fakePatchFleet{nodes: map[string]*patchNodeScript{"pc10-a": {statuses: []patchAgentStatus{rebooting}}}}
	c, s := newCoordinator(t, fleet)
	inv := &fakePatchInventory{reached: false}
	c.SetInventory(inv)
	job := runPatch(t, c, s, PatchOptions{Scope: PatchScopeAll, BatchSize: 1, Reboot: true}, "pc10-a")
	if job.Status != StatusSucceeded {
		t.Errorf("a node that was slow to return failed the run: %s", job.Status)
	}
	if !strings.Contains(inv.failures["pc10-a"], "did not come back") {
		t.Errorf("failures = %v", inv.failures)
	}
}

func TestPatchRun_IsRecordedAndAudited(t *testing.T) {
	fleet := &fakePatchFleet{nodes: map[string]*patchNodeScript{"pc11-a": okNode()}}
	c, s := newCoordinator(t, fleet)
	var mu sync.Mutex
	var actions []string
	c.SetAuditRecorder(func(e auditlog.Event) { mu.Lock(); actions = append(actions, e.Action); mu.Unlock() })
	var activity []string
	c.SetActivityRecorder(func(action, actor, summary string) { mu.Lock(); activity = append(activity, action); mu.Unlock() })
	runPatch(t, c, s, PatchOptions{Scope: PatchScopeSecurity, BatchSize: 1}, "pc11-a")
	mu.Lock()
	defer mu.Unlock()
	if strings.Join(actions, ",") != "patch.triggered,patch.succeeded" || strings.Join(activity, ",") != "patch.triggered,patch.succeeded" {
		t.Errorf("audit %v activity %v", actions, activity)
	}
}
