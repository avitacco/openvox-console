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

// SilentReason is recorded against the targets of a job that reports progress the reaper
// fails because nothing has been heard of the run. Unlike StaleReason it
// does not blame an age: a patch run legitimately lasts hours, so it is
// judged only by whether it is still reporting.
const SilentReason = "progress stopped being reported: the console instance following this patch run stopped before it finished"

// silentAfter is how long a patch job may go without reporting progress
// before the reaper fails it. The coordinator polls every few seconds,
// so ten minutes is far past a slow poll.
const silentAfter = 10 * time.Minute

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
	// silent is silentAfter, overridable in tests.
	silent time.Duration
}

// NewReaper builds a Reaper over pool.
func NewReaper(pool *pgxpool.Pool, logger *slog.Logger) *Reaper {
	return &Reaper{pool: pool, logger: logger, after: staleAfter, silent: silentAfter}
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

	// A job that reports progress (a patch run, a code plan: it has a
	// heartbeat) is judged by its silence; every other by its age. Targets first: once the job
	// row is terminal it no longer matches the job-selection predicates.
	const byAge = `status = 'running' AND heartbeat_at IS NULL AND started_at < now() - $1 * interval '1 millisecond'`
	const bySilence = `status = 'running' AND heartbeat_at IS NOT NULL AND heartbeat_at < now() - $1 * interval '1 millisecond'`
	var failed int64
	for _, rule := range []struct {
		where  string
		window time.Duration
		reason string
	}{{byAge, r.after, StaleReason}, {bySilence, r.silent, SilentReason}} {
		if _, err := tx.Exec(ctx,
			`UPDATE job_targets SET status = $2, finished_at = now(), error_detail = $3
			 WHERE status IN ($4, $5) AND job_id IN (SELECT id FROM jobs WHERE `+rule.where+`)`,
			rule.window.Milliseconds(), StatusFailed, rule.reason, StatusPending, StatusRunning); err != nil {
			return 0, fmt.Errorf("fail stale job targets: %w", err)
		}
		tag, err := tx.Exec(ctx,
			`UPDATE jobs SET status = 'failed', finished_at = now() WHERE `+rule.where, rule.window.Milliseconds())
		if err != nil {
			return 0, fmt.Errorf("fail stale jobs: %w", err)
		}
		failed += tag.RowsAffected()
	}

	if err := tx.Commit(ctx); err != nil {
		return 0, fmt.Errorf("commit reap transaction: %w", err)
	}
	return failed, nil
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
