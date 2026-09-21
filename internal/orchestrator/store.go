package orchestrator

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ErrNotFound is returned when a job or job target doesn't exist.
var ErrNotFound = errors.New("job not found")

// Store persists jobs and their per-target results in Postgres.
type Store struct {
	pool *pgxpool.Pool
	// now supplies the timestamps written to started_at and
	// finished_at. It is a field rather than SQL's now() so that a
	// caller which needs to control when a job appears to have run can
	// do so - see NewStoreWithClock.
	now func() time.Time
}

// NewStore builds a Store backed by pool, timestamping jobs with the
// current time.
func NewStore(pool *pgxpool.Pool) *Store {
	return NewStoreWithClock(pool, time.Now)
}

// NewStoreWithClock builds a Store that takes its timestamps from now
// instead of the system clock.
//
// This exists for callers that must place job history at a chosen time
// rather than at the moment of the write: the demo seeder, which dates a
// whole fabricated fleet to one fixed instant, and tests that assert on
// ordering or duration without sleeping. Production uses NewStore.
func NewStoreWithClock(pool *pgxpool.Pool, now func() time.Time) *Store {
	if now == nil {
		now = time.Now
	}
	return &Store{pool: pool, now: now}
}

// CreateJob records a new job (status StatusRunning - dispatch begins
// immediately, see job.go) with one job_target row per target, each
// starting at StatusPending, and returns the created Job with its
// targets loaded.
func (s *Store) CreateJob(ctx context.Context, kind JobKind, taskName, planName string, params json.RawMessage, targets []string, triggeredBy string) (Job, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Job{}, fmt.Errorf("begin transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	id, err := createJobTx(ctx, tx, s.now(), kind, taskName, planName, params, targets, triggeredBy, nil, nil)
	if err != nil {
		return Job{}, err
	}

	if err := tx.Commit(ctx); err != nil {
		return Job{}, fmt.Errorf("commit: %w", err)
	}

	return s.GetJob(ctx, id)
}

// PlanStep is one step of a plan to create via CreatePlan - a run
// (Kind=JobKindRun) or a task (Kind=JobKindTask, TaskName/Params set).
type PlanStep struct {
	Kind     JobKind         `json:"kind"`
	TaskName string          `json:"taskName,omitempty"`
	Params   json.RawMessage `json:"params,omitempty"`
}

// CreatePlan records a new plan job (kind JobKindPlan) plus one child
// job per step (see specs/orchestrator's "Task and plan execution"
// requirement), each with the same targets as the plan and StepOrder
// set to its 0-indexed position. Only the plan job itself is returned;
// use GetPlanSteps to load the ordered child jobs.
func (s *Store) CreatePlan(ctx context.Context, planName string, steps []PlanStep, targets []string, triggeredBy string) (Job, error) {
	if len(steps) == 0 {
		return Job{}, errors.New("create plan: at least one step is required")
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Job{}, fmt.Errorf("begin transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	// One timestamp for the plan and every step it contains: they are
	// created together, and reading the clock per row would scatter a
	// plan's steps across a few microseconds for no reason.
	startedAt := s.now()

	// The plan row itself is never directly dispatched (only its step
	// jobs are - see Dispatcher.DispatchPlan), so it gets no job_targets
	// of its own; insertJobRow, not createJobTx.
	planID, err := insertJobRow(ctx, tx, startedAt, JobKindPlan, "", planName, nil, StatusRunning, triggeredBy, nil, nil)
	if err != nil {
		return Job{}, err
	}

	for i, step := range steps {
		order := i
		if _, err := createJobTx(ctx, tx, startedAt, step.Kind, step.TaskName, "", step.Params, targets, triggeredBy, &planID, &order); err != nil {
			return Job{}, fmt.Errorf("create plan step %d: %w", i, err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return Job{}, fmt.Errorf("commit: %w", err)
	}

	return s.GetJob(ctx, planID)
}

// createJobTx inserts one job row and its job_targets within tx,
// returning the new job's ID. Shared by CreateJob and CreatePlan (for
// step jobs specifically - the plan row itself uses insertJobRow, see
// CreatePlan).
func createJobTx(ctx context.Context, tx pgx.Tx, startedAt time.Time, kind JobKind, taskName, planName string, params json.RawMessage, targets []string, triggeredBy string, parentJobID *int64, stepOrder *int) (int64, error) {
	if len(targets) == 0 {
		return 0, errors.New("create job: at least one target is required")
	}

	id, err := insertJobRow(ctx, tx, startedAt, kind, taskName, planName, params, StatusRunning, triggeredBy, parentJobID, stepOrder)
	if err != nil {
		return 0, err
	}

	for _, certname := range targets {
		if _, err := tx.Exec(ctx,
			`INSERT INTO job_targets (job_id, certname, status) VALUES ($1, $2, $3)`,
			id, certname, StatusPending,
		); err != nil {
			return 0, fmt.Errorf("create job target %q: %w", certname, err)
		}
	}

	return id, nil
}

// insertJobRow inserts just the jobs row (no job_targets) within tx,
// returning the new job's ID.
func insertJobRow(ctx context.Context, tx pgx.Tx, startedAt time.Time, kind JobKind, taskName, planName string, params json.RawMessage, status, triggeredBy string, parentJobID *int64, stepOrder *int) (int64, error) {
	var id int64
	err := tx.QueryRow(ctx,
		`INSERT INTO jobs (kind, task_name, plan_name, params, status, triggered_by, started_at, parent_job_id, step_order)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9) RETURNING id`,
		string(kind), nullIfEmpty(taskName), nullIfEmpty(planName), nullIfEmptyJSON(params), status, triggeredBy, startedAt, parentJobID, stepOrder,
	).Scan(&id)
	if err != nil {
		return 0, fmt.Errorf("create job: %w", err)
	}
	return id, nil
}

// GetPlanSteps returns planJobID's child jobs (each with targets
// loaded), ordered by StepOrder.
func (s *Store) GetPlanSteps(ctx context.Context, planJobID int64) ([]Job, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT id FROM jobs WHERE parent_job_id = $1 ORDER BY step_order`,
		planJobID,
	)
	if err != nil {
		return nil, fmt.Errorf("list plan steps: %w", err)
	}
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return nil, fmt.Errorf("list plan steps: %w", err)
		}
		ids = append(ids, id)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list plan steps: %w", err)
	}

	steps := make([]Job, 0, len(ids))
	for _, id := range ids {
		step, err := s.GetJob(ctx, id)
		if err != nil {
			return nil, err
		}
		steps = append(steps, step)
	}
	return steps, nil
}

// RecordTargetResult records one target's terminal outcome within a
// job (status should be StatusSucceeded or StatusFailed).
func (s *Store) RecordTargetResult(ctx context.Context, jobID int64, certname, status string, exitCode *int, output, errorDetail string) error {
	at := s.now()
	tag, err := s.pool.Exec(ctx,
		`UPDATE job_targets
		 SET status = $1, started_at = COALESCE(started_at, $2), finished_at = $2,
		     exit_code = $3, output = $4, error_detail = $5
		 WHERE job_id = $6 AND certname = $7`,
		status, at, exitCode, nullIfEmpty(output), nullIfEmpty(errorDetail), jobID, certname,
	)
	if err != nil {
		return fmt.Errorf("record job target result: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// SetTargetReport links a target's result to the openvoxdb report that
// run produced (see specs/orchestrator's "Job result correlation with
// reports" requirement).
func (s *Store) SetTargetReport(ctx context.Context, jobID int64, certname, reportHash string) error {
	tag, err := s.pool.Exec(ctx,
		`UPDATE job_targets SET report_hash = $1 WHERE job_id = $2 AND certname = $3`,
		reportHash, jobID, certname,
	)
	if err != nil {
		return fmt.Errorf("set job target report: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// CompleteJob marks the job identified by id as finished with status
// (StatusSucceeded or StatusFailed).
func (s *Store) CompleteJob(ctx context.Context, jobID int64, status string) error {
	tag, err := s.pool.Exec(ctx,
		`UPDATE jobs SET status = $1, finished_at = $2 WHERE id = $3`,
		status, s.now(), jobID,
	)
	if err != nil {
		return fmt.Errorf("complete job: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// JobFilter narrows ListJobs' results. A zero-value field (empty string)
// is not filtered on, and SortAsc false (the default) keeps the
// original most-recent-first order.
type JobFilter struct {
	Kind        string
	Status      string
	TriggeredBy string
	// Target matches a job that ran against this certname - a job can
	// have many targets (see job_targets), so this isn't a plain column
	// on jobs itself; ListJobs handles it as an EXISTS subquery rather
	// than through the simple column-equality filters above.
	Target  string
	SortAsc bool
}

// ListJobs returns one page of jobs matching filter, along with the
// total number of matching jobs across all pages. The full Targets
// slice is not loaded - use GetJob for a single job's full detail
// (output, exit code, etc.) - but each job's TargetCount and
// TargetPreview are, so list views can still show which node(s) it ran
// against. Sorted by started_at (most recent first unless
// filter.SortAsc), with id as a tiebreaker for jobs sharing a
// started_at.
func (s *Store) ListJobs(ctx context.Context, page, pageSize int, filter JobFilter) ([]Job, int, error) {
	var conditions []string
	var args []any
	addFilter := func(column, value string) {
		if value == "" {
			return
		}
		args = append(args, value)
		conditions = append(conditions, fmt.Sprintf("%s = $%d", column, len(args)))
	}
	addFilter("kind", filter.Kind)
	addFilter("status", filter.Status)
	addFilter("triggered_by", filter.TriggeredBy)
	if filter.Target != "" {
		// Substring, case-insensitive - matches internal/inventory's own
		// certname "name" filter convention (search-as-you-type over a
		// certname, not a single exact hostname). % and _ are LIKE
		// metacharacters; escaped so a certname that happens to contain
		// either is searched for literally instead of as a wildcard.
		escaped := strings.NewReplacer(`\`, `\\`, "%", `\%`, "_", `\_`).Replace(filter.Target)
		args = append(args, "%"+escaped+"%")
		// job_targets_certname_idx doesn't accelerate this leading-
		// wildcard ILIKE (a plain btree can't), but still helps the
		// exact-match case, and this table isn't large enough for the
		// sequential scan to matter either way - see inventory's own
		// "revisit if this matters at scale" precedent.
		conditions = append(conditions, fmt.Sprintf("EXISTS (SELECT 1 FROM job_targets jt WHERE jt.job_id = jobs.id AND jt.certname ILIKE $%d)", len(args)))
	}

	where := ""
	if len(conditions) > 0 {
		where = "WHERE " + strings.Join(conditions, " AND ")
	}

	var total int
	countQuery := "SELECT count(*) FROM jobs " + where
	if err := s.pool.QueryRow(ctx, countQuery, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count jobs: %w", err)
	}

	// order is one of two hardcoded literals, never interpolated from
	// caller input, so this string-built ORDER BY doesn't reopen the SQL
	// injection risk the $N placeholders above guard against.
	order := "DESC"
	if filter.SortAsc {
		order = "ASC"
	}
	args = append(args, pageSize, (page-1)*pageSize)
	query := fmt.Sprintf(
		`SELECT id, kind, COALESCE(task_name, ''), COALESCE(plan_name, ''), status, triggered_by, started_at, finished_at, parent_job_id, step_order
		 FROM jobs %s ORDER BY started_at %s, id %s LIMIT $%d OFFSET $%d`,
		where, order, order, len(args)-1, len(args),
	)
	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("list jobs: %w", err)
	}

	jobs := make([]Job, 0)
	ids := make([]int64, 0)
	for rows.Next() {
		var j Job
		var kind string
		if err := rows.Scan(&j.ID, &kind, &j.TaskName, &j.PlanName, &j.Status, &j.TriggeredBy, &j.StartedAt, &j.FinishedAt, &j.ParentJobID, &j.StepOrder); err != nil {
			rows.Close()
			return nil, 0, fmt.Errorf("list jobs: %w", err)
		}
		j.Kind = JobKind(kind)
		jobs = append(jobs, j)
		ids = append(ids, j.ID)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, 0, fmt.Errorf("list jobs: %w", err)
	}
	rows.Close()

	previews, err := s.loadTargetPreviews(ctx, ids)
	if err != nil {
		return nil, 0, err
	}
	for i := range jobs {
		p := previews[jobs[i].ID]
		jobs[i].TargetCount = p.count
		jobs[i].TargetPreview = p.certnames
	}

	return jobs, total, nil
}

type targetPreview struct {
	count     int
	certnames []string
}

// targetPreviewLimit caps how many certnames loadTargetPreviews returns
// per job - list views only need enough to render "web01, web02, +3
// more", not every target on a job that fans out to hundreds of nodes.
const targetPreviewLimit = 3

// loadTargetPreviews returns each job's target count and up to
// targetPreviewLimit certnames (insertion order), in a single query
// covering every job in jobIDs.
func (s *Store) loadTargetPreviews(ctx context.Context, jobIDs []int64) (map[int64]targetPreview, error) {
	previews := make(map[int64]targetPreview, len(jobIDs))
	if len(jobIDs) == 0 {
		return previews, nil
	}

	rows, err := s.pool.Query(ctx, `
		SELECT job_id, certname, cnt FROM (
			SELECT job_id, certname,
			       count(*) OVER (PARTITION BY job_id) AS cnt,
			       row_number() OVER (PARTITION BY job_id ORDER BY id) AS rn
			FROM job_targets
			WHERE job_id = ANY($1)
		) ranked
		WHERE rn <= $2`,
		jobIDs, targetPreviewLimit,
	)
	if err != nil {
		return nil, fmt.Errorf("load target previews: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var jobID int64
		var certname string
		var cnt int
		if err := rows.Scan(&jobID, &certname, &cnt); err != nil {
			return nil, fmt.Errorf("load target previews: %w", err)
		}
		p := previews[jobID]
		p.count = cnt
		p.certnames = append(p.certnames, certname)
		previews[jobID] = p
	}
	return previews, rows.Err()
}

// GetJob returns the job identified by id with its targets loaded, or
// ErrNotFound.
func (s *Store) GetJob(ctx context.Context, id int64) (Job, error) {
	var j Job
	var kind string
	err := s.pool.QueryRow(ctx,
		`SELECT id, kind, COALESCE(task_name, ''), COALESCE(plan_name, ''), params, status, triggered_by, started_at, finished_at, parent_job_id, step_order
		 FROM jobs WHERE id = $1`,
		id,
	).Scan(&j.ID, &kind, &j.TaskName, &j.PlanName, &j.Params, &j.Status, &j.TriggeredBy, &j.StartedAt, &j.FinishedAt, &j.ParentJobID, &j.StepOrder)
	if errors.Is(err, pgx.ErrNoRows) {
		return Job{}, ErrNotFound
	}
	if err != nil {
		return Job{}, fmt.Errorf("get job: %w", err)
	}
	j.Kind = JobKind(kind)

	targets, err := s.getJobTargets(ctx, id)
	if err != nil {
		return Job{}, err
	}
	j.Targets = targets

	return j, nil
}

func (s *Store) getJobTargets(ctx context.Context, jobID int64) ([]JobTarget, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT id, job_id, certname, status, started_at, finished_at, exit_code,
		        COALESCE(output, ''), COALESCE(error_detail, ''), COALESCE(report_hash, '')
		 FROM job_targets WHERE job_id = $1 ORDER BY id`,
		jobID,
	)
	if err != nil {
		return nil, fmt.Errorf("load job targets: %w", err)
	}
	defer rows.Close()

	targets := make([]JobTarget, 0)
	for rows.Next() {
		var t JobTarget
		if err := rows.Scan(&t.ID, &t.JobID, &t.Certname, &t.Status, &t.StartedAt, &t.FinishedAt,
			&t.ExitCode, &t.Output, &t.ErrorDetail, &t.ReportHash); err != nil {
			return nil, fmt.Errorf("load job targets: %w", err)
		}
		targets = append(targets, t)
	}
	return targets, rows.Err()
}

func nullIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func nullIfEmptyJSON(data json.RawMessage) any {
	if len(data) == 0 {
		return nil
	}
	return data
}
