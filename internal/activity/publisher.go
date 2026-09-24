package activity

import (
	"context"
	"log/slog"
	"time"
)

// recordTimeout bounds how long recording one event may hold up the
// caller.
const recordTimeout = 5 * time.Second

// eventStore is the subset of *Store Publisher needs, so it can be
// tested without a live Postgres.
type eventStore interface {
	RecordEvent(ctx context.Context, e Event) error
}

// Publisher records activity events for a fixed category (one per
// producing capability - see design.md's injected-closure decision, which
// is built on top of this type in internal/app).
type Publisher struct {
	store    eventStore
	logger   *slog.Logger
	category string
}

// NewPublisher builds a Publisher that records every event as category.
func NewPublisher(store eventStore, logger *slog.Logger, category string) *Publisher {
	return &Publisher{store: store, logger: logger, category: category}
}

// Publish records an activity event. A failure is logged, never
// returned - see design.md: the action being recorded has already
// succeeded by the time this is called, so failing to record it should
// never fail the caller's request.
//
// Exactly one row per call, on whichever instance the action happened:
// there is no subscriber to run, and so none to be missing or to run
// twice.
func (p *Publisher) Publish(action, actor, summary string) {
	e := Event{
		Category:   p.category,
		Action:     action,
		Actor:      actor,
		Summary:    summary,
		OccurredAt: time.Now().UTC(),
	}
	ctx, cancel := context.WithTimeout(context.Background(), recordTimeout)
	defer cancel()
	if err := p.store.RecordEvent(ctx, e); err != nil {
		p.logger.Error("failed to record activity event", "error", err, "category", e.Category, "action", e.Action)
	}
}
