package scheduler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/voxpupuli/enterprise-console/internal/orchestrator"
)

// maxFirings is how many firings are kept for each schedule.
const maxFirings = 100

// Store persists schedules and their firings in Postgres.
type Store struct {
	pool *pgxpool.Pool
}

// NewStore builds a Store backed by pool.
func NewStore(pool *pgxpool.Pool) *Store { return &Store{pool: pool} }

// Now is the database's clock. What is due, and how late, is judged by
// it rather than by any instance's own, so instances whose clocks
// disagree still agree about a schedule.
func (s *Store) Now(ctx context.Context) (time.Time, error) {
	var now time.Time
	if err := s.pool.QueryRow(ctx, `SELECT now()`).Scan(&now); err != nil {
		return time.Time{}, fmt.Errorf("read database clock: %w", err)
	}
	return now, nil
}

const scheduleColumns = `id, name, kind, run_options, COALESCE(task_name, ''), COALESCE(environment, ''), COALESCE(timeout_seconds, 0),
	params, sealed_params, target, start_at, repeat_every, repeat_unit, next_run_at, enabled, owner, owner_kind, created_at, updated_at,
	COALESCE(plan_name, ''), steps`

func scanSchedule(row pgx.Row) (Schedule, error) {
	var s Schedule
	var kind string
	var target []byte
	var every *int
	var unit *string
	var steps []byte
	if err := row.Scan(&s.ID, &s.Name, &kind, &s.Options, &s.Task, &s.Environment, &s.TimeoutSeconds,
		&s.Params, &s.sealed, &target, &s.StartAt, &every, &unit, &s.NextRunAt, &s.Enabled, &s.Owner, &s.OwnerKind, &s.CreatedAt, &s.UpdatedAt,
		&s.PlanName, &steps); err != nil {
		return Schedule{}, err
	}
	if len(steps) > 0 {
		if err := json.Unmarshal(steps, &s.Steps); err != nil {
			return Schedule{}, fmt.Errorf("decode schedule %d steps: %w", s.ID, err)
		}
	}
	s.Kind = orchestrator.JobKind(kind)
	var t orchestrator.Target
	if err := json.Unmarshal(target, &t); err != nil {
		return Schedule{}, fmt.Errorf("decode schedule %d target: %w", s.ID, err)
	}
	s.Targets, s.Query, s.Group = t.Nodes, t.Query, t.Group
	if every != nil && unit != nil {
		s.Repeat = &Repeat{Every: *every, Unit: *unit}
	}
	s.setState()
	return s, nil
}

// SealFunc seals a schedule's sensitive values once its id is known,
// since the id is what the sealed value is bound to. It returns nil
// when there is nothing to seal.
type SealFunc func(id int64) ([]byte, error)

func writeArgs(s Schedule) ([]any, error) {
	target, err := json.Marshal(s.Target())
	if err != nil {
		return nil, fmt.Errorf("encode target: %w", err)
	}
	var every *int
	var unit *string
	if s.Repeat != nil {
		every, unit = &s.Repeat.Every, &s.Repeat.Unit
	}
	var steps []byte
	if len(s.Steps) > 0 {
		if steps, err = json.Marshal(s.Steps); err != nil {
			return nil, fmt.Errorf("encode steps: %w", err)
		}
	}
	return []any{
		s.Name, string(s.Kind), nullJSON(s.Options), nullString(s.Task), nullString(s.Environment), nullInt(s.TimeoutSeconds),
		nullJSON(s.Params), target, s.StartAt, every, unit, s.NextRunAt, s.Owner, s.OwnerKind,
		nullString(s.PlanName), nullJSON(steps),
	}, nil
}

// Create stores a new, enabled schedule.
func (st *Store) Create(ctx context.Context, s Schedule, seal SealFunc) (Schedule, error) {
	args, err := writeArgs(s)
	if err != nil {
		return Schedule{}, err
	}
	tx, err := st.pool.Begin(ctx)
	if err != nil {
		return Schedule{}, fmt.Errorf("begin transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	var id int64
	if err := tx.QueryRow(ctx, `
		INSERT INTO scheduled_jobs (name, kind, run_options, task_name, environment, timeout_seconds, params, target,
		                            start_at, repeat_every, repeat_unit, next_run_at, owner, owner_kind, plan_name, steps)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16) RETURNING id`, args...).Scan(&id); err != nil {
		return Schedule{}, fmt.Errorf("create schedule: %w", err)
	}
	if err := sealInto(ctx, tx, id, seal); err != nil {
		return Schedule{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Schedule{}, fmt.Errorf("commit: %w", err)
	}
	return st.Get(ctx, id)
}

// Replace overwrites schedule id's contents and owner with s, leaving
// whether it is paused as it was. ErrNotFound if it does not exist.
func (st *Store) Replace(ctx context.Context, id int64, s Schedule, seal SealFunc) (Schedule, error) {
	args, err := writeArgs(s)
	if err != nil {
		return Schedule{}, err
	}
	tx, err := st.pool.Begin(ctx)
	if err != nil {
		return Schedule{}, fmt.Errorf("begin transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	tag, err := tx.Exec(ctx, `
		UPDATE scheduled_jobs SET name = $1, kind = $2, run_options = $3, task_name = $4, environment = $5, timeout_seconds = $6,
		       params = $7, target = $8, start_at = $9, repeat_every = $10, repeat_unit = $11, next_run_at = $12,
		       owner = $13, owner_kind = $14, plan_name = $15, steps = $16, sealed_params = NULL, updated_at = now()
		WHERE id = $17`, append(args, id)...)
	if err != nil {
		return Schedule{}, fmt.Errorf("replace schedule: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return Schedule{}, ErrNotFound
	}
	if err := sealInto(ctx, tx, id, seal); err != nil {
		return Schedule{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Schedule{}, fmt.Errorf("commit: %w", err)
	}
	return st.Get(ctx, id)
}

func sealInto(ctx context.Context, tx pgx.Tx, id int64, seal SealFunc) error {
	if seal == nil {
		return nil
	}
	sealed, err := seal(id)
	if err != nil {
		return err
	}
	if sealed == nil {
		return nil
	}
	if _, err := tx.Exec(ctx, `UPDATE scheduled_jobs SET sealed_params = $2 WHERE id = $1`, id, sealed); err != nil {
		return fmt.Errorf("store sealed parameters: %w", err)
	}
	return nil
}

// Get returns schedule id with its firings, newest first, or
// ErrNotFound.
func (st *Store) Get(ctx context.Context, id int64) (Schedule, error) {
	s, err := scanSchedule(st.pool.QueryRow(ctx, `SELECT `+scheduleColumns+` FROM scheduled_jobs WHERE id = $1`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return Schedule{}, ErrNotFound
	}
	if err != nil {
		return Schedule{}, fmt.Errorf("get schedule: %w", err)
	}
	rows, err := st.pool.Query(ctx, `
		SELECT id, due_at, fired_at, outcome, COALESCE(detail, ''), job_id, now()
		FROM scheduled_job_firings WHERE schedule_id = $1 ORDER BY id DESC LIMIT $2`, id, maxFirings)
	if err != nil {
		return Schedule{}, fmt.Errorf("list firings: %w", err)
	}
	defer rows.Close()
	s.Firings = []Firing{}
	for rows.Next() {
		f, err := scanFiring(rows)
		if err != nil {
			return Schedule{}, fmt.Errorf("list firings: %w", err)
		}
		s.Firings = append(s.Firings, f)
	}
	if err := rows.Err(); err != nil {
		return Schedule{}, fmt.Errorf("list firings: %w", err)
	}
	if len(s.Firings) > 0 {
		s.LastFiring = &s.Firings[0]
	}
	return s, nil
}

// scanFiring reads a firing followed by the database's now(), which
// decides whether a claim with no outcome has been left too long.
func scanFiring(row pgx.Row) (Firing, error) {
	var f Firing
	var now time.Time
	if err := row.Scan(&f.ID, &f.DueAt, &f.FiredAt, &f.Outcome, &f.Detail, &f.JobID, &now); err != nil {
		return Firing{}, err
	}
	if f.Outcome == OutcomeFiring && now.Sub(f.FiredAt) > interruptedAfter {
		f.Outcome = OutcomeInterrupted
		f.Detail = "the console instance starting this stopped before it could; it was not retried"
	}
	return f, nil
}

// List returns every schedule, or only owner's when owner is not nil,
// each with its last firing, ordered by name.
func (st *Store) List(ctx context.Context, owner *Owner) ([]Schedule, error) {
	query := `SELECT ` + scheduleColumns + ` FROM scheduled_jobs`
	var args []any
	if owner != nil {
		query += ` WHERE owner = $1 AND owner_kind = $2`
		args = []any{owner.Name, owner.Kind}
	}
	rows, err := st.pool.Query(ctx, query+` ORDER BY lower(name), id`, args...)
	if err != nil {
		return nil, fmt.Errorf("list schedules: %w", err)
	}
	schedules := []Schedule{}
	index := map[int64]int{}
	ids := []int64{}
	for rows.Next() {
		s, err := scanSchedule(rows)
		if err != nil {
			rows.Close()
			return nil, fmt.Errorf("list schedules: %w", err)
		}
		index[s.ID] = len(schedules)
		ids = append(ids, s.ID)
		schedules = append(schedules, s)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list schedules: %w", err)
	}

	last, err := st.pool.Query(ctx, `
		SELECT DISTINCT ON (schedule_id) schedule_id, id, due_at, fired_at, outcome, COALESCE(detail, ''), job_id, now()
		FROM scheduled_job_firings WHERE schedule_id = ANY($1) ORDER BY schedule_id, id DESC`, ids)
	if err != nil {
		return nil, fmt.Errorf("list last firings: %w", err)
	}
	defer last.Close()
	for last.Next() {
		var scheduleID int64
		var f Firing
		var now time.Time
		if err := last.Scan(&scheduleID, &f.ID, &f.DueAt, &f.FiredAt, &f.Outcome, &f.Detail, &f.JobID, &now); err != nil {
			return nil, fmt.Errorf("list last firings: %w", err)
		}
		if f.Outcome == OutcomeFiring && now.Sub(f.FiredAt) > interruptedAfter {
			f.Outcome = OutcomeInterrupted
		}
		schedules[index[scheduleID]].LastFiring = &f
	}
	return schedules, last.Err()
}

// Delete removes schedule id and its firings. The jobs it started stay,
// still naming it. ErrNotFound if it does not exist.
func (st *Store) Delete(ctx context.Context, id int64) error {
	tag, err := st.pool.Exec(ctx, `DELETE FROM scheduled_jobs WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("delete schedule: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// SetEnabled pauses or resumes schedule id, setting its next start.
func (st *Store) SetEnabled(ctx context.Context, id int64, enabled bool, next *time.Time) error {
	tag, err := st.pool.Exec(ctx, `UPDATE scheduled_jobs SET enabled = $2, next_run_at = $3, updated_at = now() WHERE id = $1`, id, enabled, next)
	if err != nil {
		return fmt.Errorf("update schedule: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// Due lists the enabled schedules whose next start is at or before now.
func (st *Store) Due(ctx context.Context, now time.Time) ([]Schedule, error) {
	rows, err := st.pool.Query(ctx, `SELECT `+scheduleColumns+` FROM scheduled_jobs
		WHERE enabled AND next_run_at IS NOT NULL AND next_run_at <= $1 ORDER BY next_run_at LIMIT 100`, now)
	if err != nil {
		return nil, fmt.Errorf("list due schedules: %w", err)
	}
	defer rows.Close()
	var due []Schedule
	for rows.Next() {
		s, err := scanSchedule(rows)
		if err != nil {
			return nil, fmt.Errorf("list due schedules: %w", err)
		}
		due = append(due, s)
	}
	return due, rows.Err()
}

// Claim takes schedule id's firing for the due time dueAt, moving its
// next start to next. It reports false when another instance took it
// first, or the schedule was paused, changed or deleted meanwhile: the
// update only applies while next_run_at is still dueAt. On success the
// firing is recorded as claimed, with no outcome yet.
func (st *Store) Claim(ctx context.Context, id int64, dueAt time.Time, next *time.Time) (firingID int64, claimed bool, err error) {
	tx, err := st.pool.Begin(ctx)
	if err != nil {
		return 0, false, fmt.Errorf("begin transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	tag, err := tx.Exec(ctx, `UPDATE scheduled_jobs SET next_run_at = $3 WHERE id = $1 AND next_run_at = $2 AND enabled`, id, dueAt, next)
	if err != nil {
		return 0, false, fmt.Errorf("claim schedule: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return 0, false, nil
	}
	if err := tx.QueryRow(ctx, `INSERT INTO scheduled_job_firings (schedule_id, due_at, outcome) VALUES ($1, $2, $3) RETURNING id`,
		id, dueAt, OutcomeFiring).Scan(&firingID); err != nil {
		return 0, false, fmt.Errorf("record firing: %w", err)
	}
	if _, err := tx.Exec(ctx, `DELETE FROM scheduled_job_firings WHERE schedule_id = $1 AND id NOT IN
		(SELECT id FROM scheduled_job_firings WHERE schedule_id = $1 ORDER BY id DESC LIMIT $2)`, id, maxFirings); err != nil {
		return 0, false, fmt.Errorf("trim firings: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, false, fmt.Errorf("commit: %w", err)
	}
	return firingID, true, nil
}

// FinishFiring writes a claimed firing's outcome.
func (st *Store) FinishFiring(ctx context.Context, firingID int64, outcome, detail string, jobID *int64) error {
	if _, err := st.pool.Exec(ctx, `UPDATE scheduled_job_firings SET outcome = $2, detail = $3, job_id = $4 WHERE id = $1`,
		firingID, outcome, nullString(detail), jobID); err != nil {
		return fmt.Errorf("record firing outcome: %w", err)
	}
	return nil
}

// JobRunning reports whether a job schedule id started is still
// running.
func (st *Store) JobRunning(ctx context.Context, id int64) (bool, error) {
	var running bool
	if err := st.pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM jobs WHERE schedule_id = $1 AND status = 'running')`, id).Scan(&running); err != nil {
		return false, fmt.Errorf("check running job: %w", err)
	}
	return running, nil
}

func nullString(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func nullInt(n int) any {
	if n == 0 {
		return nil
	}
	return n
}

func nullJSON(data json.RawMessage) any {
	if len(data) == 0 || string(data) == "null" {
		return nil
	}
	return []byte(data)
}
