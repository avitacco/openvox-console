// Package activity records and exposes an audit trail of meaningful
// actions taken through the console. Every capability records through a
// Publisher, which writes each Event straight to one table - one shape
// and one writer path for every producer, rather than each service
// keeping its own audit entries.
//
// Events were once carried over the NATS bus to a single subscribing
// recorder (see design.md in the phase-4-activity-and-audit-log change).
// Core NATS is at-most-once: an event published while no recorder was
// running - any split topology without a worker instance - or while the
// recorder was behind or restarting was silently lost. Writing directly
// removes every one of those windows, and needs nothing but the database
// the action being recorded has just written to anyway.
package activity

import "time"

// Event is one recorded action.
type Event struct {
	Category   string    `json:"category"` // e.g. "classifier", "rbac"
	Action     string    `json:"action"`   // e.g. "group.created", "role.assigned"
	Actor      string    `json:"actor"`    // username, or "system" for OIDC-driven changes
	Summary    string    `json:"summary"`  // human-readable, e.g. "created group web-servers"
	OccurredAt time.Time `json:"occurredAt"`
}
