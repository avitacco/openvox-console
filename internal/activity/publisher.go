package activity

import (
	"encoding/json"
	"log/slog"
	"time"
)

// publisherBus is the subset of *messaging.Bus Publisher needs, so it can
// be tested without a live NATS server.
type publisherBus interface {
	Publish(subject string, data []byte) error
}

// Publisher publishes activity events for a fixed category (one per
// producing capability - see design.md's injected-closure decision, which
// is built on top of this type in cmd/console/main.go).
type Publisher struct {
	bus      publisherBus
	logger   *slog.Logger
	category string
}

// NewPublisher builds a Publisher that publishes every event as category.
func NewPublisher(bus publisherBus, logger *slog.Logger, category string) *Publisher {
	return &Publisher{bus: bus, logger: logger, category: category}
}

// Publish sends an activity event. A failure to publish is logged, never
// returned - see design.md: the action being recorded has already
// succeeded by the time this is called, so a publish-time hiccup should
// never fail the caller's request.
func (p *Publisher) Publish(action, actor, summary string) {
	e := Event{
		Category:   p.category,
		Action:     action,
		Actor:      actor,
		Summary:    summary,
		OccurredAt: time.Now().UTC(),
	}
	data, err := json.Marshal(e)
	if err != nil {
		p.logger.Error("failed to encode activity event", "error", err, "category", e.Category, "action", e.Action)
		return
	}
	if err := p.bus.Publish(Subject, data); err != nil {
		p.logger.Error("failed to publish activity event", "error", err, "category", e.Category, "action", e.Action)
	}
}
