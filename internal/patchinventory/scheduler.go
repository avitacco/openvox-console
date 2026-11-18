package patchinventory

import (
	"context"
	"log/slog"
	"sort"
	"time"

	"github.com/voxpupuli/enterprise-console/internal/leases"
)

// SchedulerLease is the lease one instance holds while it runs a pass, so
// that however many instances run the worker only one scans at a time.
const SchedulerLease = "patch-scan"

// DefaultTick is how often the scheduler looks for nodes that are due.
const DefaultTick = time.Minute

// PurgeAfter is how long a node's snapshot is kept without any attempt
// before it is removed.
const PurgeAfter = 90 * 24 * time.Hour

const purgeEvery = 24 * time.Hour

// Scheduler scans every node whose last attempt is older than the
// interval. It writes no audit entries: a pass over hundreds of nodes
// would drown the ones a person made.
type Scheduler struct {
	store   *Store
	scanner *Scanner
	leases  *leases.Leases
	source  interface {
		nodeNames(context.Context) ([]string, error)
	}
	logger    *slog.Logger
	tick      time.Duration
	now       func() time.Time
	lastPurge time.Time
}

type nodeLister struct{ NodeSource }

func (n nodeLister) nodeNames(ctx context.Context) ([]string, error) {
	nodes, err := n.Nodes(ctx)
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(nodes))
	for _, node := range nodes {
		names = append(names, node.Certname)
	}
	return names, nil
}

// NewScheduler builds a Scheduler. leases may be nil in tests, in which
// case passes run unleased.
func NewScheduler(store *Store, scanner *Scanner, leases *leases.Leases, source NodeSource, logger *slog.Logger) *Scheduler {
	return &Scheduler{store: store, scanner: scanner, leases: leases, source: nodeLister{source},
		logger: logger, tick: DefaultTick, now: time.Now}
}

// Run does a pass now and then every tick until ctx is cancelled.
func (s *Scheduler) Run(ctx context.Context) {
	ticker := time.NewTicker(s.tick)
	defer ticker.Stop()
	for {
		s.pass(ctx)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (s *Scheduler) pass(ctx context.Context) {
	if s.leases == nil {
		_ = s.RunOnce(ctx)
		return
	}
	if _, err := s.leases.Hold(ctx, SchedulerLease, s.RunOnce); err != nil {
		s.logger.Warn("patch scan pass failed", "error", err)
	}
}

// Due returns the nodes to scan now: those with no attempt recorded or
// whose last attempt is at least the interval old. A node that is offline
// records no attempt, so it is tried again on the next pass.
func Due(names []string, attempts map[string]time.Time, interval time.Duration, now time.Time) []string {
	var due []string
	for _, n := range names {
		at, tried := attempts[n]
		if !tried || now.Sub(at) >= interval {
			due = append(due, n)
		}
	}
	sort.Strings(due)
	return due
}

// RunOnce is one pass.
func (s *Scheduler) RunOnce(ctx context.Context) error {
	now := s.now()
	if now.Sub(s.lastPurge) >= purgeEvery {
		if n, err := s.store.Purge(ctx, now.Add(-PurgeAfter)); err != nil {
			s.logger.Error("patch scan: purge old snapshots", "error", err)
		} else {
			s.lastPurge = now
			if n > 0 {
				s.logger.Info("purged patch snapshots of nodes not seen for 90 days", "count", n)
			}
		}
	}

	settings, err := s.store.Settings(ctx)
	if err != nil {
		return err
	}
	if !settings.Enabled {
		return nil
	}
	names, err := s.source.nodeNames(ctx)
	if err != nil {
		return err
	}
	attempts, err := s.store.LastAttempts(ctx)
	if err != nil {
		return err
	}
	due := Due(names, attempts, settings.Interval, now)
	if len(due) == 0 {
		return nil
	}
	results := s.scanner.ScanMany(ctx, due)
	var scanned, failed int
	for _, r := range results {
		switch r.Outcome {
		case OutcomeScanned:
			scanned++
		case OutcomeOffline:
		default:
			failed++
		}
	}
	if scanned+failed > 0 {
		s.logger.Info("patch scan pass finished", "scanned", scanned, "failed", failed, "due", len(due))
	}
	return nil
}
