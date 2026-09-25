package initialrun

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/voxpupuli/enterprise-console/internal/testdb"
)

func quietLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// recorder captures which certnames were dispatched.
type recorder struct {
	mu         sync.Mutex
	dispatched []string
	err        error
	onDispatch func()
}

func (r *recorder) dispatch(_ context.Context, certname string) error {
	if r.onDispatch != nil {
		r.onDispatch()
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.err != nil {
		return r.err
	}
	r.dispatched = append(r.dispatched, certname)
	return nil
}

func (r *recorder) names() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]string, len(r.dispatched))
	copy(out, r.dispatched)
	return out
}

// newTrigger wires a Trigger against a real Postgres claim store, with
// the given inventory answer and dispatch recorder.
func newTrigger(t *testing.T, inventory InventoryLookup, rec *recorder) *Trigger {
	t.Helper()
	pool := testdb.Pool(t)
	trig := NewTrigger(NewStore(pool), inventory, rec.dispatch, quietLogger())
	trig.Start(context.Background())
	t.Cleanup(trig.Stop)
	return trig
}

func cleanupClaims(t *testing.T, names ...string) {
	t.Helper()
	pool := testdb.Pool(t)
	t.Cleanup(func() {
		for _, n := range names {
			_, _ = pool.Exec(context.Background(), `DELETE FROM initial_run_claims WHERE certname = $1`, n)
		}
	})
}

// waitFor polls until cond holds or the deadline passes.
func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestTrigger_DispatchesForNodeWithNoInventory(t *testing.T) {
	const certname = "trigger-new.example.com"
	cleanupClaims(t, certname)

	rec := &recorder{}
	trig := newTrigger(t, func(context.Context, string) (bool, error) { return false, nil }, rec)

	trig.Notify(certname)

	waitFor(t, "the run to be dispatched", func() bool { return len(rec.names()) == 1 })
	if got := rec.names()[0]; got != certname {
		t.Fatalf("dispatched %q, want %q", got, certname)
	}
}

func TestTrigger_DoesNotDispatchForInventoriedNode(t *testing.T) {
	const certname = "trigger-known.example.com"
	cleanupClaims(t, certname)

	rec := &recorder{}
	trig := newTrigger(t, func(context.Context, string) (bool, error) { return true, nil }, rec)

	trig.Notify(certname)

	// Nothing should ever be dispatched; give the worker time to prove it.
	time.Sleep(250 * time.Millisecond)
	if got := rec.names(); len(got) != 0 {
		t.Fatalf("dispatched %v for an already-inventoried node, want none", got)
	}

	// And no claim should have been written, so the node stays eligible
	// if its inventory is later purged.
	pool := testdb.Pool(t)
	var claims int
	if err := pool.QueryRow(context.Background(),
		`SELECT count(*) FROM initial_run_claims WHERE certname = $1`, certname).Scan(&claims); err != nil {
		t.Fatalf("count claims: %v", err)
	}
	if claims != 0 {
		t.Fatalf("wrote %d claim(s) for an already-inventoried node, want 0", claims)
	}
}

// Task 3.3: repeated notifications, including while the first run has
// not yet reported (inventory still empty), must dispatch exactly once.
func TestTrigger_RepeatedNotificationsDispatchOnce(t *testing.T) {
	const certname = "trigger-flapping.example.com"
	cleanupClaims(t, certname)

	rec := &recorder{}
	// Inventory stays empty throughout - the node has connected but its
	// first run has not produced a report yet.
	trig := newTrigger(t, func(context.Context, string) (bool, error) { return false, nil }, rec)

	for range 5 {
		trig.Notify(certname)
	}

	waitFor(t, "the first dispatch", func() bool { return len(rec.names()) >= 1 })
	time.Sleep(250 * time.Millisecond) // let any duplicates land

	if got := rec.names(); len(got) != 1 {
		t.Fatalf("dispatched %d times for a flapping node, want exactly 1: %v", len(got), got)
	}
}

// Task 3.2: an inventory error is "unknown", not "absent".
func TestTrigger_InventoryErrorClaimsNothingAndDispatchesNothing(t *testing.T) {
	const certname = "trigger-openvoxdb-down.example.com"
	cleanupClaims(t, certname)

	rec := &recorder{}
	var lookups atomic.Int64
	trig := newTrigger(t, func(context.Context, string) (bool, error) {
		lookups.Add(1)
		return false, errors.New("openvoxdb unreachable")
	}, rec)

	trig.Notify(certname)
	waitFor(t, "the inventory lookup", func() bool { return lookups.Load() >= 1 })
	time.Sleep(200 * time.Millisecond)

	if got := rec.names(); len(got) != 0 {
		t.Fatalf("dispatched %v despite an inventory error, want none", got)
	}

	pool := testdb.Pool(t)
	var claims int
	if err := pool.QueryRow(context.Background(),
		`SELECT count(*) FROM initial_run_claims WHERE certname = $1`, certname).Scan(&claims); err != nil {
		t.Fatalf("count claims: %v", err)
	}
	if claims != 0 {
		t.Fatalf("wrote %d claim(s) despite an inventory error, want 0 so the node stays eligible", claims)
	}
}

// A failed dispatch keeps its claim: no automatic retry.
func TestTrigger_FailedDispatchIsNotRetried(t *testing.T) {
	const certname = "trigger-dispatch-fails.example.com"
	cleanupClaims(t, certname)

	var attempts atomic.Int64
	rec := &recorder{
		err:        errors.New("node unreachable"),
		onDispatch: func() { attempts.Add(1) },
	}
	trig := newTrigger(t, func(context.Context, string) (bool, error) { return false, nil }, rec)

	trig.Notify(certname)
	waitFor(t, "the failing dispatch", func() bool { return attempts.Load() >= 1 })

	// Reconnecting must not try again - the claim already stands.
	trig.Notify(certname)
	time.Sleep(250 * time.Millisecond)

	if got := attempts.Load(); got != 1 {
		t.Fatalf("dispatch attempted %d times after a failure, want 1 (no automatic retry)", got)
	}
}

// Task 3.4: a batch enrollment dispatches every node's run, without
// running them all at once.
func TestTrigger_BatchDispatchesEveryNodeWithBoundedConcurrency(t *testing.T) {
	const batch = 40

	names := make([]string, batch)
	for i := range names {
		names[i] = fmt.Sprintf("trigger-batch-%02d.example.com", i)
	}
	cleanupClaims(t, names...)

	var (
		inFlight atomic.Int64
		peak     atomic.Int64
	)
	rec := &recorder{onDispatch: func() {
		cur := inFlight.Add(1)
		for {
			old := peak.Load()
			if cur <= old || peak.CompareAndSwap(old, cur) {
				break
			}
		}
		time.Sleep(5 * time.Millisecond) // hold the slot so overlap is observable
		inFlight.Add(-1)
	}}

	trig := newTrigger(t, func(context.Context, string) (bool, error) { return false, nil }, rec)

	for _, n := range names {
		trig.Notify(n)
	}

	waitFor(t, "every node in the batch to be dispatched", func() bool { return len(rec.names()) == batch })

	if got := trig.Dropped(); got != 0 {
		t.Errorf("dropped %d notifications for a batch of %d, want 0 - the queue should absorb a routine batch", got, batch)
	}
	if got := peak.Load(); got > int64(defaultWorkers) {
		t.Fatalf("peak concurrent dispatches = %d, want at most %d", got, defaultWorkers)
	}
	if peak.Load() < 2 {
		t.Logf("peak concurrency was %d - workers kept up serially, which is fine", peak.Load())
	}
}

// Notify must never block, even with every worker busy and the queue
// full: blocking would push back on the node transport.
func TestTrigger_NotifyNeverBlocksWhenSaturated(t *testing.T) {
	release := make(chan struct{})
	defer close(release)

	rec := &recorder{onDispatch: func() { <-release }}
	trig := newTrigger(t, func(context.Context, string) (bool, error) { return false, nil }, rec)

	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := range defaultQueueDepth + defaultWorkers + 50 {
			trig.Notify(fmt.Sprintf("trigger-saturate-%03d.example.com", i))
		}
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Notify blocked when the queue was saturated")
	}

	if trig.Dropped() == 0 {
		t.Error("expected some notifications to be dropped once saturated, got 0")
	}
}
