// Package auditlog emits a structured, per-category-configurable stream
// of audit events - authentication, administrative changes, and
// (where enabled) read access. It never persists anything itself: an
// event is a single synchronous structured log write, and what happens
// to that stream afterward (storage, retention, export) is the
// operator's responsibility, not this package's. See design.md in the
// configurable-audit-logging change for why this is a synchronous log
// write rather than a fire-and-forget publish like internal/activity.
package auditlog

import "fmt"

// Category is one of the console's existing capability boundaries, each
// independently configurable - see design.md's "category granularity"
// decision (matches package boundaries, not individual actions).
type Category string

const (
	CategoryNodes        Category = "nodes"
	CategoryClassifier   Category = "classifier"
	CategoryRBAC         Category = "rbac"
	CategoryAuth         Category = "auth"
	CategoryCode         Category = "code"
	CategoryOrchestrator Category = "orchestrator"
)

// Level is how much a Category emits. Levels are ordered: Full satisfies
// a Writes check, and Writes satisfies an Off check (trivially, since
// nothing needs Off).
type Level int

const (
	// LevelOff emits nothing for the category.
	LevelOff Level = iota
	// LevelWrites emits mutating actions and authentication events.
	LevelWrites
	// LevelFull emits everything LevelWrites does, plus read/view access.
	LevelFull
)

// ParseLevel parses "off", "writes", or "full" (case-insensitive). An
// unrecognized value is a config error - see internal/runtime.Config's
// audit-level fields, which reject an invalid CONSOLE_AUDIT_* value
// rather than silently falling back to a default.
func ParseLevel(s string) (Level, error) {
	switch s {
	case "off":
		return LevelOff, nil
	case "writes":
		return LevelWrites, nil
	case "full":
		return LevelFull, nil
	default:
		return 0, &InvalidLevelError{Value: s}
	}
}

// InvalidLevelError reports an unrecognized audit level string.
type InvalidLevelError struct {
	Value string
}

func (e *InvalidLevelError) Error() string {
	return fmt.Sprintf("invalid audit level %q: must be \"off\", \"writes\", or \"full\"", e.Value)
}
