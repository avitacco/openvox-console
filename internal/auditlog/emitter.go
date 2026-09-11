package auditlog

import "log/slog"

// Config is the per-Category Level, resolved once at startup (see
// design.md - "no per-request lookup", the same reasoning already used
// for RBAC permissions embedded in issued JWTs).
type Config map[Category]Level

// levelFor returns the configured level for category, defaulting to
// LevelOff for a category missing from the map (e.g. a zero-value
// Config in a test that doesn't care about that category).
func (c Config) levelFor(category Category) Level {
	return c[category]
}

// Emitter emits audit events as structured log records through logger -
// a synchronous, in-process write (see design.md's "Emission is a
// synchronous slog write, not a NATS publish" decision), not persisted
// anywhere by this package.
type Emitter struct {
	cfg    Config
	logger *slog.Logger
}

// NewEmitter builds an Emitter using cfg's per-category levels, writing
// through logger.
func NewEmitter(cfg Config, logger *slog.Logger) *Emitter {
	return &Emitter{cfg: cfg, logger: logger}
}

// Write emits event for category if that category's level is Writes or
// Full - i.e. a mutating action or authentication event.
func (e *Emitter) Write(category Category, event Event) {
	if e.cfg.levelFor(category) >= LevelWrites {
		e.emit(category, event)
	}
}

// Read emits event for category only if that category's level is Full -
// i.e. a read/view of that category's data.
func (e *Emitter) Read(category Category, event Event) {
	if e.cfg.levelFor(category) >= LevelFull {
		e.emit(category, event)
	}
}

func (e *Emitter) emit(category Category, event Event) {
	attrs := event.attrs(category)
	args := make([]any, len(attrs))
	for i, a := range attrs {
		args[i] = a
	}
	e.logger.Info("audit", args...)
}
