package agentless

import (
	"context"
	"log/slog"
	"sort"
	"sync"
	"time"
)

// NameSource lists the names of every agentless target.
type NameSource interface {
	Names(ctx context.Context) ([]string, error)
}

// Directory answers whether a name is an agentless target, for the
// orchestrator to route a dispatch by. The names are read from the store
// and kept for a few seconds, so routing does not cost a query per node;
// a change made on this instance clears the cache at once, and another
// instance's is seen within the TTL.
type Directory struct {
	source NameSource
	ttl    time.Duration
	logger *slog.Logger
	now    func() time.Time

	mu    sync.Mutex
	names map[string]bool
	at    time.Time
}

// DirectoryTTL is how long a read of the names is kept.
const DirectoryTTL = 5 * time.Second

// NewDirectory builds a Directory.
func NewDirectory(source NameSource, logger *slog.Logger) *Directory {
	if logger == nil {
		logger = slog.Default()
	}
	return &Directory{source: source, ttl: DirectoryTTL, logger: logger, now: time.Now}
}

// Invalidate drops what is cached, after a target was added or removed.
func (d *Directory) Invalidate() {
	d.mu.Lock()
	d.names, d.at = nil, time.Time{}
	d.mu.Unlock()
}

func (d *Directory) load(ctx context.Context) map[string]bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.names != nil && d.now().Sub(d.at) < d.ttl {
		return d.names
	}
	names, err := d.source.Names(ctx)
	if err != nil {
		// Keep what was known: a name that was agentless a moment ago
		// still is, and failing open to "has an agent" would send an
		// agentless target's task to nobody.
		d.logger.Warn("could not read the agentless targets", "error", err)
		return d.names
	}
	d.names = make(map[string]bool, len(names))
	for _, n := range names {
		d.names[n] = true
	}
	d.at = d.now()
	return d.names
}

// Has reports whether name is an agentless target.
func (d *Directory) Has(ctx context.Context, name string) bool { return d.load(ctx)[name] }

// Among returns those of certnames that are agentless targets, sorted.
func (d *Directory) Among(ctx context.Context, certnames []string) []string {
	known := d.load(ctx)
	var out []string
	for _, c := range certnames {
		if known[c] {
			out = append(out, c)
		}
	}
	sort.Strings(out)
	return out
}
