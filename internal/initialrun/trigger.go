package initialrun

import (
	"context"
	"log/slog"
	"sync"
)

// InventoryLookup reports whether the console already holds an inventory
// record for certname.
//
// It returns an error rather than a bool when it cannot tell - an
// openvoxdb outage must not be read as "no inventory", which would
// dispatch a run to every node that connects while openvoxdb is down.
type InventoryLookup func(ctx context.Context, certname string) (bool, error)

// RunDispatcher starts a Puppet run against certname, recording it as a
// job. Supplied by the caller so this package does not depend on the
// orchestrator directly.
type RunDispatcher func(ctx context.Context, certname string) error

// TriggeredBy is the job's recorded actor. The "system:" prefix cannot
// be produced by a username, so an automatic run is never mistaken for
// one a person asked for.
const TriggeredBy = "system:initial-run"

// defaultWorkers bounds how many initial runs are set up at once. Each
// worker holds an openvoxdb lookup, a Postgres claim and a dispatch, so
// an unbounded fan-out on a batch enrolment would compete with the rest
// of the console for the same connection pool. Four keeps a batch moving
// without letting it dominate.
const defaultWorkers = 4

// defaultQueueDepth is sized so that a routine batch enrolment queues
// rather than drops - dropping is meant to signal genuine overload, not
// "someone provisioned a rack".
const defaultQueueDepth = 256

// Trigger dispatches one Puppet run to a node the first time it connects
// without an inventory record. See design.md in
// add-initial-run-on-first-connect.
//
// Notify is safe to call from the node transport's connect notification:
// it never blocks, so a busy Trigger cannot slow the transport down.
type Trigger struct {
	store     *Store
	inventory InventoryLookup
	dispatch  RunDispatcher
	logger    *slog.Logger

	queue chan string
	wg    sync.WaitGroup

	// dropped counts notifications shed because the queue was full,
	// for tests and for the log line that reports them.
	mu      sync.Mutex
	dropped int
}

// NewTrigger builds a Trigger. Call Start to begin draining.
func NewTrigger(store *Store, inventory InventoryLookup, dispatch RunDispatcher, logger *slog.Logger) *Trigger {
	return &Trigger{
		store:     store,
		inventory: inventory,
		dispatch:  dispatch,
		logger:    logger,
		queue:     make(chan string, defaultQueueDepth),
	}
}

// Start launches the worker pool. It returns immediately; call Stop to
// drain and shut down.
func (t *Trigger) Start(ctx context.Context) {
	for range defaultWorkers {
		t.wg.Add(1)
		go func() {
			defer t.wg.Done()
			for certname := range t.queue {
				t.handle(ctx, certname)
			}
		}()
	}
}

// Stop closes the queue and waits for in-flight work to finish.
func (t *Trigger) Stop() {
	close(t.queue)
	t.wg.Wait()
}

// Notify records that certname connected. It never blocks: when the
// queue is full the notification is dropped and logged, which costs that
// node its automatic run - the same outcome as connecting while the
// console is down, and its own scheduled run still covers it. Blocking
// here would push back on the node transport instead, which is worse.
func (t *Trigger) Notify(certname string) {
	select {
	case t.queue <- certname:
	default:
		t.mu.Lock()
		t.dropped++
		total := t.dropped
		t.mu.Unlock()
		t.logger.Warn("initial-run queue full; skipping automatic first run for this node",
			"certname", certname, "droppedTotal", total)
	}
}

// Dropped reports how many notifications have been shed for a full
// queue.
func (t *Trigger) Dropped() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.dropped
}

// handle runs the check-claim-dispatch sequence for one certname.
//
// Order matters: inventory first (cheapest way to rule out the common
// case of an already-known node reconnecting), then the claim, then the
// dispatch. The claim is deliberately taken before dispatching - see
// Store.Claim.
func (t *Trigger) handle(ctx context.Context, certname string) {
	known, err := t.inventory(ctx, certname)
	if err != nil {
		// Unknown, so do nothing and leave the node eligible: no claim
		// is written, and its next connection tries again.
		t.logger.Warn("could not determine whether a connected node has inventory; skipping its automatic first run",
			"certname", certname, "error", err)
		return
	}
	if known {
		return
	}

	won, err := t.store.Claim(ctx, certname)
	if err != nil {
		t.logger.Warn("could not claim a node's initial run; skipping it",
			"certname", certname, "error", err)
		return
	}
	if !won {
		// Already handled - by this console earlier, or by another
		// instance observing the same connection.
		return
	}

	if err := t.dispatch(ctx, certname); err != nil {
		// The claim stands. A failed initial run is a visible failed
		// job and is never retried automatically: see design.md for
		// why a retry turns one systemic failure into a stream of them.
		t.logger.Error("initial run dispatch failed for a newly enrolled node",
			"certname", certname, "error", err)
		return
	}

	t.logger.Info("dispatched initial Puppet run for a newly enrolled node", "certname", certname)
}
