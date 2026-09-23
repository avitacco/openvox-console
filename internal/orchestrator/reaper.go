package orchestrator

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// StaleReason is recorded against a job the reaper fails. It is
// deliberately distinct from any failure the node itself reported: the
// run may well have happened, but nothing was left alive to correlate
// its outcome.
const StaleReason = "could not be tracked to completion: the console instance that dispatched this job stopped before it finished"

// staleAfter is how long past a job's start it is considered
// untrackable.
//
// A dispatch's own wait is bounded by dispatchTimeout, so a legitimately
// in-flight job cannot outlive that. Doubling it leaves generous room
// for clock skew and a slow correlation step, so the reaper only ever
// catches jobs whose tracker is genuinely gone rather than ones still
// being waited on.
const staleAfter = 2 * dispatchTimeout

// Reaper fails jobs left non-terminal by an instance that stopped.
//
// In-flight dispatch tracking lives in the dispatching instance's
// memory: the response to a dispatch only ever arrives on the NATS
// connection that instance holds. If it stops, nothing is left to
// correlate that job, and without this the job would read "running"
// forever - indistinguishable, to an operator, from one still in
// progress.
type Reaper struct {
	pool   *pgxpool.Pool
	logger *slog.Logger

	// after is staleAfter, overridable in tests.
	after time.Duration
}

// NewReaper builds a Reaper over pool.
func NewReaper(pool *pgxpool.Pool, logger *slog.Logger) *Reaper {
	return &Reaper{pool: pool, logger: logger, after: staleAfter}
}

// ReapOnce fails every job still running past the staleness threshold,
// along with its non-terminal targets, and reports how many jobs it
// failed.
func (r *Reaper) ReapOnce(ctx context.Context) (int64, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return 0, fmt.Errorf("begin reap transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	// Targets first: once the job row is terminal it no longer matches
	// the job-selection predicate below.
	if _, err := tx.Exec(ctx,
		`UPDATE job_targets SET status = $1, finished_at = now(), error_detail = $2
		 WHERE status IN ($3, $4)
		   AND job_id IN (
		     SELECT id FROM jobs WHERE status = $5 AND started_at < now() - $6 * interval '1 millisecond'
		   )`,
		StatusFailed, StaleReason, StatusPending, StatusRunning, StatusRunning, r.after.Milliseconds()); err != nil {
		return 0, fmt.Errorf("fail stale job targets: %w", err)
	}

	tag, err := tx.Exec(ctx,
		`UPDATE jobs SET status = $1, finished_at = now()
		 WHERE status = $2 AND started_at < now() - $3 * interval '1 millisecond'`,
		StatusFailed, StatusRunning, r.after.Milliseconds())
	if err != nil {
		return 0, fmt.Errorf("fail stale jobs: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return 0, fmt.Errorf("commit reap transaction: %w", err)
	}
	return tag.RowsAffected(), nil
}

// Run reaps on every tick until ctx is cancelled.
//
// Intended to run under a lease so exactly one instance reaps, though
// the operation is idempotent and harmless if two ever overlap - the
// second simply finds nothing left to fail.
func (r *Reaper) Run(ctx context.Context, every time.Duration) {
	ticker := time.NewTicker(every)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			failed, err := r.ReapOnce(ctx)
			if err != nil {
				r.logger.Warn("failed to reap stale jobs", "error", err)
				continue
			}
			if failed > 0 {
				r.logger.Info("failed jobs left running by a stopped instance", "jobs", failed)
			}
		}
	}
}
