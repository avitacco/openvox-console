package orchestrator

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"
)

// TestDispatch_BeforeRunLeavesJobRunning pins down why the initial-run
// trigger must not start before Dispatcher.Run: Run is the only consumer
// of the unbuffered outcomes channel, so a dispatch started before it
// has nobody to hand its result to and the job never leaves "running".
//
// Every other dispatch path is an HTTP handler and so cannot fire before
// the server is serving. The initial-run trigger is driven by a node
// connecting, which happens as soon as the transport binds - much
// earlier - which is exactly how this was hit in practice.
func TestDispatch_BeforeRunLeavesJobRunning(t *testing.T) {
	store := testStore(t)
	ctx := context.Background()

	job, err := store.CreateJob(ctx, JobKindRun, "", "", nil,
		[]string{"dispatch-before-run.example.com"}, "system:initial-run")
	if err != nil {
		t.Fatalf("CreateJob() error: %v", err)
	}

	d := NewDispatcher(store, unreachableTransport{}, slog.New(slog.NewTextHandler(io.Discard, nil)))

	// Deliberately NOT starting Run.
	d.DispatchRun(ctx, job)
	time.Sleep(500 * time.Millisecond)

	stuck, err := store.GetJob(ctx, job.ID)
	if err != nil {
		t.Fatalf("GetJob() error: %v", err)
	}
	if stuck.Status != StatusRunning {
		t.Fatalf("without Run, job status = %q, want %q - if this now reaches a terminal state, the outcomes channel gained a buffer or another consumer and this ordering constraint can be relaxed",
			stuck.Status, StatusRunning)
	}

	// With Run consuming outcomes, the same job reaches a terminal state.
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	go d.Run(runCtx)

	deadline := time.Now().Add(5 * time.Second)
	for {
		got, err := store.GetJob(ctx, job.ID)
		if err != nil {
			t.Fatalf("GetJob() error: %v", err)
		}
		if got.Status != StatusRunning {
			if got.Status != StatusFailed {
				t.Fatalf("job status = %q, want %q (the transport is unreachable)", got.Status, StatusFailed)
			}
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("job never left \"running\" even with Run consuming outcomes")
		}
		time.Sleep(20 * time.Millisecond)
	}
}
