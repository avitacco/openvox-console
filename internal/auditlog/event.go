package auditlog

import "log/slog"

// Event is a single audit event. Every emitted event carries the same
// fixed key set (see design.md's "Fixed event schema" decision) so a
// downstream log pipeline can rely on one predictable shape rather than
// reverse-engineering per-action formats.
type Event struct {
	// Action is a stable identifier, e.g. "user.login.success",
	// "node.viewed" - not a free-text summary.
	Action string
	// Actor is the identity that performed the action (a username, or
	// "system" for console-initiated actions - matches internal/activity's
	// existing convention).
	Actor string
	// ResourceType and ResourceID identify what the action acted on, e.g.
	// ("node", "web01.example.com") or ("role", "admin").
	ResourceType string
	ResourceID   string
	// Before and After carry structured values for a change, only set
	// where the underlying store makes them available (e.g. a role's
	// permission set before/after an update). Left nil when not
	// applicable - never populated with a placeholder.
	Before any
	After  any
}

// attrs renders e as slog attributes under category, in the fixed key
// set: action, actor, category, resourceType, resourceId, and before/
// after when set. Called by Emitter, not by consumers directly.
func (e Event) attrs(category Category) []slog.Attr {
	attrs := []slog.Attr{
		slog.String("action", e.Action),
		slog.String("actor", e.Actor),
		slog.String("category", string(category)),
		slog.String("resourceType", e.ResourceType),
		slog.String("resourceId", e.ResourceID),
	}
	if e.Before != nil {
		attrs = append(attrs, slog.Any("before", e.Before))
	}
	if e.After != nil {
		attrs = append(attrs, slog.Any("after", e.After))
	}
	return attrs
}
