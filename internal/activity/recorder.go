package activity

import (
	"context"
	"encoding/json"
	"log/slog"

	"github.com/nats-io/nats.go"
)

// subscriber is the subset of *messaging.Bus Recorder needs, so it can be
// tested without a live NATS server.
type subscriber interface {
	QueueSubscribe(subject, queue string, handler nats.MsgHandler) (*nats.Subscription, error)
}

// recorderQueue is the queue group every instance's Recorder joins.
//
// This is what makes the recorder correct in a cluster. It persists each
// event to one table, so a plain fan-out subscription would write one
// identical row per running instance - a bug invisible on a single
// instance and immediate on two. A queue group delivers each event to
// exactly one member, wherever it happens to be running. See
// internal/messaging's package documentation for the general rule.
const recorderQueue = "activity-recorder"

// recorderStore is the subset of *Store Recorder needs.
type recorderStore interface {
	RecordEvent(ctx context.Context, e Event) error
}

// Recorder is the single subscriber that persists every published
// activity event - see design.md's "single subject" decision and
// architecture-summary.md's "a single subscriber persisting to one
// table" design.
type Recorder struct {
	store  recorderStore
	logger *slog.Logger
}

// NewRecorder builds a Recorder persisting events to store.
func NewRecorder(store recorderStore, logger *slog.Logger) *Recorder {
	return &Recorder{store: store, logger: logger}
}

// Start subscribes to Subject and persists every received event. Call it
// once at startup, before the HTTP server begins accepting requests - the
// same ordering rbac.Revoker.Start already relies on - so no event
// published by a live request can be missed.
//
// The subscription joins recorderQueue, so across a cluster exactly one
// instance persists each event.
func (r *Recorder) Start(bus subscriber) error {
	_, err := bus.QueueSubscribe(Subject, recorderQueue, func(msg *nats.Msg) {
		var e Event
		if err := json.Unmarshal(msg.Data, &e); err != nil {
			r.logger.Warn("dropping malformed activity event", "error", err)
			return
		}
		if err := r.store.RecordEvent(context.Background(), e); err != nil {
			r.logger.Error("failed to persist activity event", "error", err, "category", e.Category, "action", e.Action)
		}
	})
	return err
}
