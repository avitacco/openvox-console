package orchestrator

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/voxpupuli/enterprise-console/internal/nodeagent"
	"github.com/voxpupuli/enterprise-console/internal/nodetransport"
	"github.com/voxpupuli/enterprise-console/internal/testca"
)

// startRealAgent starts a real nodetransport.Server and a real
// nodeagent.Client connected to it, executing requests via runner - for
// integration tests that exercise the whole real stack (transport +
// node-agent) rather than fakes on either side.
func startRealAgent(t *testing.T, certname string, runner nodeagent.CommandRunner) (*nodetransport.Server, func()) {
	t.Helper()

	ca := testca.NewCA(t)
	serverCert, serverKey := ca.Issue(t, "test-node-transport", true)
	transport, err := nodetransport.New(nodetransport.Config{
		ListenAddr: "127.0.0.1:0",
		CertFile:   serverCert,
		KeyFile:    serverKey,
		CAFile:     ca.PEMFile(t),
	})
	if err != nil {
		t.Fatalf("nodetransport.New() error: %v", err)
	}
	certPath, keyPath := ca.Issue(t, certname, false)

	handler := nodeagent.NewHandler(runner, "puppet", t.TempDir(), func(string) bool { return false })
	client := nodeagent.New(nodeagent.Config{
		TransportAddr: transport.Addr(),
		CertFile:      certPath,
		KeyFile:       keyPath,
		CAFile:        ca.PEMFile(t),
	}, handler, nil)

	ctx, cancel := context.WithCancel(context.Background())
	go client.Run(ctx)

	waitForConnected(t, transport, certname)

	return transport, func() {
		cancel()
		transport.Close()
	}
}

func waitForConnected(t *testing.T, transport *nodetransport.Server, certname string) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if transport.Registry().Lookup(certname) {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("certname %q never connected to the test node transport", certname)
}

func waitForJobStatus(t *testing.T, store *Store, jobID int64, want string) Job {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	var last Job
	for time.Now().Before(deadline) {
		job, err := store.GetJob(context.Background(), jobID)
		if err != nil {
			t.Fatalf("GetJob() error: %v", err)
		}
		last = job
		if job.Status == want {
			return job
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("job %d status = %q after timeout, want %q (targets: %+v)", jobID, last.Status, want, last.Targets)
	return Job{}
}

func fakeRunner(stdout string, exitCode int) nodeagent.CommandRunner {
	return func(ctx context.Context, name string, args ...string) (string, string, int, error) {
		return stdout, "", exitCode, nil
	}
}

func TestDispatcher_RunSucceedsAgainstRealAgent(t *testing.T) {
	store := testStore(t)
	broker, stopAgent := startRealAgent(t, "web01.example.com", fakeRunner("changes applied", 2))
	defer stopAgent()

	dispatcher := NewDispatcher(store, broker, nil)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go dispatcher.Run(ctx)

	job, err := store.CreateJob(ctx, JobKindRun, "", "", nil, []string{"web01.example.com"}, "dispatcher-test-actor")
	if err != nil {
		t.Fatalf("CreateJob() error: %v", err)
	}
	dispatcher.DispatchRun(ctx, job)

	final := waitForJobStatus(t, store, job.ID, StatusSucceeded)
	if len(final.Targets) != 1 {
		t.Fatalf("len(Targets) = %d, want 1", len(final.Targets))
	}
	target := final.Targets[0]
	if target.Status != StatusSucceeded {
		t.Errorf("target Status = %q, want %q", target.Status, StatusSucceeded)
	}
	if target.Output != "changes applied" {
		t.Errorf("target Output = %q", target.Output)
	}
	if target.ExitCode == nil || *target.ExitCode != 2 {
		t.Errorf("target ExitCode = %v, want 2", target.ExitCode)
	}
}

func TestDispatcher_RunFailureAgainstRealAgent(t *testing.T) {
	store := testStore(t)
	// Exit code 4 is Puppet's --detailed-exitcodes convention for
	// "failures occurred" - see puppetRunFailed.
	broker, stopAgent := startRealAgent(t, "web01.example.com", fakeRunner("catalog failed", 4))
	defer stopAgent()

	dispatcher := NewDispatcher(store, broker, nil)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go dispatcher.Run(ctx)

	job, err := store.CreateJob(ctx, JobKindRun, "", "", nil, []string{"web01.example.com"}, "actor")
	if err != nil {
		t.Fatalf("CreateJob() error: %v", err)
	}
	dispatcher.DispatchRun(ctx, job)

	final := waitForJobStatus(t, store, job.ID, StatusFailed)
	if final.Targets[0].Status != StatusFailed {
		t.Errorf("target Status = %q, want %q", final.Targets[0].Status, StatusFailed)
	}
}

func TestDispatcher_TargetWithNoLiveConnectionFailsWithoutBlockingOthers(t *testing.T) {
	store := testStore(t)
	broker, stopAgent := startRealAgent(t, "connected.example.com", fakeRunner("ok", 0))
	defer stopAgent()

	dispatcher := NewDispatcher(store, broker, nil)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go dispatcher.Run(ctx)

	job, err := store.CreateJob(ctx, JobKindRun, "", "", nil,
		[]string{"connected.example.com", "disconnected.example.com"}, "actor")
	if err != nil {
		t.Fatalf("CreateJob() error: %v", err)
	}
	dispatcher.DispatchRun(ctx, job)

	final := waitForJobStatus(t, store, job.ID, StatusFailed) // one target failed -> job rolls up to failed
	if len(final.Targets) != 2 {
		t.Fatalf("len(Targets) = %d, want 2", len(final.Targets))
	}

	byName := map[string]JobTarget{}
	for _, target := range final.Targets {
		byName[target.Certname] = target
	}

	connected := byName["connected.example.com"]
	if connected.Status != StatusSucceeded {
		t.Errorf("connected.example.com Status = %q, want %q (should not be blocked by the other target)", connected.Status, StatusSucceeded)
	}

	disconnected := byName["disconnected.example.com"]
	if disconnected.Status != StatusFailed {
		t.Errorf("disconnected.example.com Status = %q, want %q", disconnected.Status, StatusFailed)
	}
	if disconnected.ErrorDetail == "" {
		t.Error("disconnected.example.com ErrorDetail is empty, want a reason")
	}
}

func TestDispatcher_PlanStepsRunInOrder(t *testing.T) {
	store := testStore(t)

	var mu sync.Mutex
	var invokedOrder []string
	releaseStep1 := make(chan struct{})

	runner := func(ctx context.Context, name string, args ...string) (string, string, int, error) {
		mu.Lock()
		// args[0] is "agent" for a run, or "run" for a task (puppet task run <name>) -
		// use it plus a marker to identify which step this call is.
		label := "run"
		if len(args) > 0 && args[0] == "task" {
			label = "task:" + args[2]
		}
		invokedOrder = append(invokedOrder, label)
		mu.Unlock()

		if label == "run" {
			<-releaseStep1 // block step 1 until the test releases it
		}
		return "ok", "", 0, nil
	}
	broker, stopAgent := startRealAgent(t, "plan-target.example.com", runner)
	defer stopAgent()

	dispatcher := NewDispatcher(store, broker, nil)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go dispatcher.Run(ctx)

	plan, err := store.CreatePlan(ctx, "ordered-plan", []PlanStep{
		{Kind: JobKindRun},
		{Kind: JobKindTask, TaskName: "verify"},
	}, []string{"plan-target.example.com"}, "actor")
	if err != nil {
		t.Fatalf("CreatePlan() error: %v", err)
	}
	dispatcher.DispatchPlan(ctx, plan)

	// Step 1 (the run) should have started, but step 2 (the task) must
	// not have - it blocks on releaseStep1.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		mu.Lock()
		started := len(invokedOrder) > 0
		mu.Unlock()
		if started {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	time.Sleep(200 * time.Millisecond) // give a wrongly-early step 2 a chance to start
	mu.Lock()
	if len(invokedOrder) != 1 || invokedOrder[0] != "run" {
		mu.Unlock()
		t.Fatalf("invokedOrder = %v after step 1 alone should have started, want [\"run\"]", invokedOrder)
	}
	mu.Unlock()

	close(releaseStep1)

	waitForJobStatus(t, store, plan.ID, StatusSucceeded)

	mu.Lock()
	defer mu.Unlock()
	want := []string{"run", "task:verify"}
	if len(invokedOrder) != len(want) {
		t.Fatalf("invokedOrder = %v, want %v", invokedOrder, want)
	}
	for i := range want {
		if invokedOrder[i] != want[i] {
			t.Errorf("invokedOrder[%d] = %q, want %q", i, invokedOrder[i], want[i])
		}
	}
}

func TestDispatcher_PlanFailsFastOnStepFailure(t *testing.T) {
	store := testStore(t)

	var mu sync.Mutex
	var invokedCount int
	runner := func(ctx context.Context, name string, args ...string) (string, string, int, error) {
		mu.Lock()
		invokedCount++
		mu.Unlock()
		return "failed", "", 4, nil // Puppet detailed-exitcodes failure
	}
	broker, stopAgent := startRealAgent(t, "plan-fail-target.example.com", runner)
	defer stopAgent()

	dispatcher := NewDispatcher(store, broker, nil)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go dispatcher.Run(ctx)

	plan, err := store.CreatePlan(ctx, "fail-fast-plan", []PlanStep{
		{Kind: JobKindRun},
		{Kind: JobKindTask, TaskName: "verify"},
	}, []string{"plan-fail-target.example.com"}, "actor")
	if err != nil {
		t.Fatalf("CreatePlan() error: %v", err)
	}
	dispatcher.DispatchPlan(ctx, plan)

	waitForJobStatus(t, store, plan.ID, StatusFailed)

	// Give a wrongly-dispatched step 2 a chance to have started.
	time.Sleep(200 * time.Millisecond)
	mu.Lock()
	defer mu.Unlock()
	if invokedCount != 1 {
		t.Errorf("invokedCount = %d, want 1 (step 2 must not run after step 1 failed)", invokedCount)
	}
}

func TestDispatcher_JobStaysRunningUntilAllTargetsFinish(t *testing.T) {
	store := testStore(t)

	release := make(chan struct{})
	slowRunner := func(ctx context.Context, name string, args ...string) (string, string, int, error) {
		<-release
		return "done", "", 0, nil
	}
	broker, stopAgent := startRealAgent(t, "slow.example.com", slowRunner)
	defer stopAgent()

	dispatcher := NewDispatcher(store, broker, nil)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go dispatcher.Run(ctx)

	job, err := store.CreateJob(ctx, JobKindRun, "", "", nil, []string{"slow.example.com"}, "actor")
	if err != nil {
		t.Fatalf("CreateJob() error: %v", err)
	}
	dispatcher.DispatchRun(ctx, job)

	// Give the request time to reach the agent and start executing,
	// then confirm the job is still "running" while the handler blocks.
	time.Sleep(200 * time.Millisecond)
	stillRunning, err := store.GetJob(ctx, job.ID)
	if err != nil {
		t.Fatalf("GetJob() error: %v", err)
	}
	if stillRunning.Status != StatusRunning {
		t.Fatalf("job Status = %q before the handler finished, want %q", stillRunning.Status, StatusRunning)
	}

	close(release)
	waitForJobStatus(t, store, job.ID, StatusSucceeded)
}

func TestDispatcher_PendingCountTracksInFlightDispatch(t *testing.T) {
	store := testStore(t)
	release := make(chan struct{})
	slowRunner := func(ctx context.Context, name string, args ...string) (string, string, int, error) {
		<-release
		return "done", "", 0, nil
	}
	broker, stopAgent := startRealAgent(t, "pending-count.example.com", slowRunner)
	defer stopAgent()

	dispatcher := NewDispatcher(store, broker, nil)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go dispatcher.Run(ctx)

	if got := dispatcher.PendingCount(); got != 0 {
		t.Fatalf("PendingCount() = %d, want 0 before any dispatch", got)
	}

	job, err := store.CreateJob(ctx, JobKindRun, "", "", nil, []string{"pending-count.example.com"}, "actor")
	if err != nil {
		t.Fatalf("CreateJob() error: %v", err)
	}
	dispatcher.DispatchRun(ctx, job)

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && dispatcher.PendingCount() == 0 {
		time.Sleep(10 * time.Millisecond)
	}
	if got := dispatcher.PendingCount(); got != 1 {
		t.Fatalf("PendingCount() = %d while the handler is still blocked, want 1", got)
	}

	close(release)
	waitForJobStatus(t, store, job.ID, StatusSucceeded)

	deadline = time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && dispatcher.PendingCount() != 0 {
		time.Sleep(10 * time.Millisecond)
	}
	if got := dispatcher.PendingCount(); got != 0 {
		t.Errorf("PendingCount() = %d after the job completed, want 0", got)
	}
}
