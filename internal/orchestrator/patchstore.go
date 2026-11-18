package orchestrator

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"

	"github.com/jackc/pgx/v5"
)

// CreatePatchJob records a patch run: the job, with its options as
// params, and one target per node, pending, numbered by the batch it
// will be patched in. Nodes are batched in certname order, so a run is
// repeatable. Its heartbeat starts now.
func (s *Store) CreatePatchJob(ctx context.Context, options PatchOptions, targets []string, triggeredBy string, origin JobOrigin) (Job, error) {
	if len(targets) == 0 {
		return Job{}, errors.New("create patch job: at least one target is required")
	}
	if options.BatchSize < 1 {
		return Job{}, errors.New("create patch job: batch size must be at least 1")
	}
	params, err := json.Marshal(options)
	if err != nil {
		return Job{}, fmt.Errorf("encode patch options: %w", err)
	}
	sorted := append([]string(nil), targets...)
	sort.Strings(sorted)

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Job{}, fmt.Errorf("begin transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	// Starting runs is serialized, so two requests naming the same node
	// cannot both find it free.
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock($1)`, patchStartLock); err != nil {
		return Job{}, fmt.Errorf("lock patch start: %w", err)
	}
	busy, err := nodesBeingPatched(ctx, tx, sorted)
	if err != nil {
		return Job{}, err
	}
	if len(busy) > 0 {
		return Job{}, &NodesBeingPatchedError{Nodes: busy}
	}

	at := s.now()
	id, err := insertJobRow(ctx, tx, at, JobKindPatch, "", "", "", params, StatusRunning, triggeredBy, nil, nil)
	if err != nil {
		return Job{}, err
	}
	if _, err := tx.Exec(ctx, `UPDATE jobs SET heartbeat_at = $2 WHERE id = $1`, id, at); err != nil {
		return Job{}, fmt.Errorf("start patch heartbeat: %w", err)
	}
	for i, certname := range sorted {
		if _, err := tx.Exec(ctx,
			`INSERT INTO job_targets (job_id, certname, status, batch) VALUES ($1, $2, $3, $4)`,
			id, certname, StatusPending, i/options.BatchSize+1); err != nil {
			return Job{}, fmt.Errorf("create patch target %q: %w", certname, err)
		}
	}
	if err := setOrigin(ctx, tx, id, origin); err != nil {
		return Job{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Job{}, fmt.Errorf("commit: %w", err)
	}
	return s.GetJob(ctx, id)
}

// StartPatchTarget marks a target running and records its first
// heartbeat, as its batch begins.
func (s *Store) StartPatchTarget(ctx context.Context, jobID int64, certname string) error {
	at := s.now()
	tag, err := s.pool.Exec(ctx,
		`UPDATE job_targets SET status = $1, started_at = COALESCE(started_at, $2), heartbeat_at = $2
		 WHERE job_id = $3 AND certname = $4 AND status = $5`,
		StatusRunning, at, jobID, certname, StatusPending)
	if err != nil {
		return fmt.Errorf("start patch target: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return s.TouchJob(ctx, jobID)
}

// TouchJob records that a patch run is alive.
func (s *Store) TouchJob(ctx context.Context, jobID int64) error {
	if _, err := s.pool.Exec(ctx, `UPDATE jobs SET heartbeat_at = $2 WHERE id = $1 AND status = $3`, jobID, s.now(), StatusRunning); err != nil {
		return fmt.Errorf("record job heartbeat: %w", err)
	}
	return nil
}

// TouchTarget records that a node answered: its own heartbeat, and the
// job's.
func (s *Store) TouchTarget(ctx context.Context, jobID int64, certname string) error {
	at := s.now()
	if _, err := s.pool.Exec(ctx,
		`UPDATE job_targets SET heartbeat_at = $3 WHERE job_id = $1 AND certname = $2 AND status = $4`,
		jobID, certname, at, StatusRunning); err != nil {
		return fmt.Errorf("record target heartbeat: %w", err)
	}
	return s.TouchJob(ctx, jobID)
}

// CloseNotStarted fails every target of the job still pending, with
// reason: the run stopped before it reached them. It returns how many.
func (s *Store) CloseNotStarted(ctx context.Context, jobID int64, reason string) (int64, error) {
	tag, err := s.pool.Exec(ctx,
		`UPDATE job_targets SET status = $1, finished_at = $2, error_detail = $3
		 WHERE job_id = $4 AND status = $5`,
		StatusFailed, s.now(), reason, jobID, StatusPending)
	if err != nil {
		return 0, fmt.Errorf("close targets not started: %w", err)
	}
	return tag.RowsAffected(), nil
}

// patchStartLock is the advisory lock that serializes starting patch runs.
const patchStartLock int64 = 0x70617463 // "patc"

type querier interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
}

// NodesBeingPatched returns those of certnames that are a pending or
// running target of a running patch job.
func (s *Store) NodesBeingPatched(ctx context.Context, certnames []string) ([]string, error) {
	return nodesBeingPatched(ctx, s.pool, certnames)
}

func nodesBeingPatched(ctx context.Context, q querier, certnames []string) ([]string, error) {
	rows, err := q.Query(ctx,
		`SELECT DISTINCT t.certname FROM job_targets t JOIN jobs j ON j.id = t.job_id
		 WHERE j.kind = 'patch' AND j.status = $1 AND t.status IN ($2, $3) AND t.certname = ANY($4)
		 ORDER BY t.certname`,
		StatusRunning, StatusPending, StatusRunning, certnames)
	if err != nil {
		return nil, fmt.Errorf("find nodes being patched: %w", err)
	}
	names, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return nil, fmt.Errorf("find nodes being patched: %w", err)
	}
	return names, nil
}
