// Package activity records and exposes an audit trail of meaningful
// actions taken through the console. Every other capability publishes an
// Event on the shared NATS subject; this package's Recorder is the single
// subscriber that persists them to one table - see architecture-summary.md
// ("a single subscriber persisting to one table, rather than each service
// independently writing its own audit entries") and design.md in the
// phase-4-activity-and-audit-log change.
package activity

import "time"

// Subject is the NATS subject every activity event is published on.
// There is exactly one, regardless of which capability or action
// produced the event - see design.md's "single subject" decision.
const Subject = "activity.events"

// Event is the envelope published on Subject and persisted by Recorder.
type Event struct {
	Category   string    `json:"category"` // e.g. "classifier", "rbac"
	Action     string    `json:"action"`   // e.g. "group.created", "role.assigned"
	Actor      string    `json:"actor"`    // username, or "system" for OIDC-driven changes
	Summary    string    `json:"summary"`  // human-readable, e.g. "created group web-servers"
	OccurredAt time.Time `json:"occurredAt"`
}
