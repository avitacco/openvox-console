package orchestrator

import (
	"context"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/voxpupuli/enterprise-console/internal/auditlog"
	"github.com/voxpupuli/enterprise-console/internal/initialrun"
)

// TestInitialRunJob_IsAttributedToTheSystemInActivityAndAudit covers the
// attribution half of add-initial-run-on-first-connect: a run the
// console dispatches for a newly enrolled node is recorded as an
// ordinary job, but its actor must be unmistakably the system rather
// than a user - nobody asked for it, and the string must not be able to
// collide with a username.
func TestInitialRunJob_IsAttributedToTheSystemInActivityAndAudit(t *testing.T) {
	store := testStore(t)
	ctx := context.Background()

	job, err := store.CreateJob(ctx, JobKindRun, "", "", nil,
		[]string{"initial-run-attribution.example.com"}, initialrun.TriggeredBy)
	if err != nil {
		t.Fatalf("CreateJob() error: %v", err)
	}
	if job.TriggeredBy != initialrun.TriggeredBy {
		t.Fatalf("job.TriggeredBy = %q, want %q", job.TriggeredBy, initialrun.TriggeredBy)
	}

	var (
		mu            sync.Mutex
		activityActor string
		auditEvents   []auditlog.Event
	)

	d := NewDispatcher(store, unreachableTransport{}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	d.SetActivityRecorder(func(_, actor, _ string) {
		mu.Lock()
		defer mu.Unlock()
		activityActor = actor
	})
	d.SetAuditRecorder(func(e auditlog.Event) {
		mu.Lock()
		defer mu.Unlock()
		auditEvents = append(auditEvents, e)
	})

	d.DispatchRun(ctx, job)

	// Dispatch is asynchronous; wait for the recorders to have fired.
	deadline := time.Now().Add(5 * time.Second)
	for {
		mu.Lock()
		gotActivity := activityActor != ""
		gotAudit := len(auditEvents) > 0
		mu.Unlock()
		if gotActivity && gotAudit {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("activity and audit recorders were not both called within 5s")
		}
		time.Sleep(10 * time.Millisecond)
	}

	mu.Lock()
	defer mu.Unlock()

	if activityActor != initialrun.TriggeredBy {
		t.Errorf("activity actor = %q, want %q", activityActor, initialrun.TriggeredBy)
	}
	for _, e := range auditEvents {
		if e.Actor != initialrun.TriggeredBy {
			t.Errorf("audit event %q actor = %q, want %q", e.Action, e.Actor, initialrun.TriggeredBy)
		}
		if e.ResourceType != "job" {
			t.Errorf("audit event %q resourceType = %q, want %q", e.Action, e.ResourceType, "job")
		}
	}
}

// unreachableTransport stands in for a node that is not connected: the
// attribution assertions above hold regardless of whether the run
// actually reaches the node, which is the point - a failed initial run
// is still recorded as the system's.
type unreachableTransport struct{}

func (unreachableTransport) Dispatch(context.Context, string, []byte, time.Duration) ([]byte, error) {
	return nil, errNotConnectedForTest
}

var errNotConnectedForTest = errNotConnected{}

type errNotConnected struct{}

func (errNotConnected) Error() string { return "node is not connected" }
