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

	id, err := createJobTx(ctx, tx, s.now(), kind, taskName, planName, "", params, targets, triggeredBy, nil, nil)
	if err != nil {
		return Job{}, err
	}

	if err := tx.Commit(ctx); err != nil {
		return Job{}, fmt.Errorf("commit: %w", err)
	}

	return s.GetJob(ctx, id)
}

// CreateRunJob records a Puppet run with options. A plain run stores
// nothing for them, exactly as CreateJob(JobKindRun, ...) does.
func (s *Store) CreateRunJob(ctx context.Context, options RunOptions, targets []string, triggeredBy string) (Job, error) {
	return s.CreateRunJobFrom(ctx, options, targets, triggeredBy, JobOrigin{})
}

// JobOrigin is where a job came from, beyond who triggered it: the
// query or node group its targets were resolved from, and the schedule
// that started it. The zero JobOrigin is a job somebody started on a
// list of nodes.
type JobOrigin struct {
	Source   *TargetSource
	Schedule *ScheduleRef
}

// setOrigin records origin on the job just inserted in tx.
func setOrigin(ctx context.Context, tx pgx.Tx, jobID int64, origin JobOrigin) error {
	if origin.Source == nil && origin.Schedule == nil {
		return nil
	}
	var source []byte
	if origin.Source != nil {
		encoded, err := json.Marshal(origin.Source)
		if err != nil {
			return fmt.Errorf("encode target source: %w", err)
		}
		source = encoded
	}
	var scheduleID *int64
	var scheduleName *string
	if origin.Schedule != nil {
		scheduleName = &origin.Schedule.Name
		if origin.Schedule.ID != 0 {
			scheduleID = &origin.Schedule.ID
		}
	}
	if _, err := tx.Exec(ctx,
		`UPDATE jobs SET target_source = $2, schedule_id = $3, schedule_name = $4 WHERE id = $1`,
		jobID, source, scheduleID, scheduleName); err != nil {
		return fmt.Errorf("record job origin: %w", err)
	}
	return nil
}

// CreateRunJobFrom is CreateRunJob, also recording where the job came
// from.
func (s *Store) CreateRunJobFrom(ctx context.Context, options RunOptions, targets []string, triggeredBy string, origin JobOrigin) (Job, error) {
	var params json.RawMessage
	if !options.IsZero() {
		encoded, err := json.Marshal(options)
		if err != nil {
			return Job{}, fmt.Errorf("encode run options: %w", err)
		}
		params = encoded
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Job{}, fmt.Errorf("begin transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	id, err := createJobTx(ctx, tx, s.now(), JobKindRun, "", "", options.Environment, params, targets, triggeredBy, nil, nil)
	if err != nil {
		return Job{}, err
	}
	if err := setOrigin(ctx, tx, id, origin); err != nil {
		return Job{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Job{}, fmt.Errorf("commit: %w", err)
	}
	return s.GetJob(ctx, id)
}

// CreateTaskJob records a task job like CreateJob does, also recording
// the code environment its task was resolved from. params must already
// have sensitive values redacted.
func (s *Store) CreateTaskJob(ctx context.Context, taskName, environment string, params json.RawMessage, targets []string, triggeredBy string) (Job, error) {
	return s.CreateTaskJobFrom(ctx, taskName, environment, params, targets, triggeredBy, JobOrigin{})
}

// CreateTaskJobFrom is CreateTaskJob, also recording where the job came
// from.
func (s *Store) CreateTaskJobFrom(ctx context.Context, taskName, environment string, params json.RawMessage, targets []string, triggeredBy string, origin JobOrigin) (Job, error) {
	return s.CreateTaskJobChild(ctx, taskName, environment, params, targets, triggeredBy, origin, nil, nil)
}

// CreateTaskJobChild is CreateTaskJobFrom for a task job that is a step of
// another job - a code plan: parent is that job's id, and step is the
// step's position among those the plan has run.
func (s *Store) CreateTaskJobChild(ctx context.Context, taskName, environment string, params json.RawMessage, targets []string, triggeredBy string, origin JobOrigin, parent *int64, step *int) (Job, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Job{}, fmt.Errorf("begin transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	id, err := createJobTx(ctx, tx, s.now(), JobKindTask, taskName, "", environment, params, targets, triggeredBy, parent, step)
	if err != nil {
		return Job{}, err
	}
	if err := setOrigin(ctx, tx, id, origin); err != nil {
		return Job{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Job{}, fmt.Errorf("commit: %w", err)
	}
	return s.GetJob(ctx, id)
}

// PlanStep is one step of a plan to create via CreatePlan - a run
// (Kind=JobKindRun) or a task (Kind=JobKindTask, TaskName/Params set).
//
// The same shape is a step of a plan request, of a saved plan and of a
// scheduled plan. As a row passed to CreatePlan, a run step's Params
// are its options and its Environment the one they ask for, which is
// how a run job stores them.
type PlanStep struct {
	Kind     JobKind `json:"kind"`
	TaskName string  `json:"taskName,omitempty"`
	// Environment is the code environment a task step's task is taken
	// from; empty is the default environment.
	Environment string          `json:"environment,omitempty"`
	Params      json.RawMessage `json:"params,omitempty"`
	// TimeoutSeconds is a task step's timeout; zero is the default.
	TimeoutSeconds int `json:"timeoutSeconds,omitempty"`
	// Options are a run step's RunOptions.
	Options json.RawMessage `json:"options,omitempty"`
}

// CreatePlan records a new plan job (kind JobKindPlan) plus one child
// job per step (see specs/orchestrator's "Task and plan execution"
// requirement), each with the same targets as the plan and StepOrder
// set to its 0-indexed position. Only the plan job itself is returned;
// use GetPlanSteps to load the ordered child jobs.
func (s *Store) CreatePlan(ctx context.Context, planName string, steps []PlanStep, targets []string, triggeredBy string) (Job, error) {
	return s.CreatePlanFrom(ctx, planName, steps, targets, triggeredBy, JobOrigin{})
}

// CreatePlanFrom is CreatePlan, also recording on the plan where it
// came from.
func (s *Store) CreatePlanFrom(ctx context.Context, planName string, steps []PlanStep, targets []string, triggeredBy string, origin JobOrigin) (Job, error) {
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
	planID, err := insertJobRow(ctx, tx, startedAt, JobKindPlan, "", planName, "", nil, StatusRunning, triggeredBy, nil, nil)
	if err != nil {
		return Job{}, err
	}

	for i, step := range steps {
		order := i
		if _, err := createJobTx(ctx, tx, startedAt, step.Kind, step.TaskName, "", step.Environment, step.Params, targets, triggeredBy, &planID, &order); err != nil {
			return Job{}, fmt.Errorf("create plan step %d: %w", i, err)
		}
	}
	if err := setOrigin(ctx, tx, planID, origin); err != nil {
		return Job{}, err
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
func createJobTx(ctx context.Context, tx pgx.Tx, startedAt time.Time, kind JobKind, taskName, planName, environment string, params json.RawMessage, targets []string, triggeredBy string, parentJobID *int64, stepOrder *int) (int64, error) {
	if len(targets) == 0 {
		return 0, errors.New("create job: at least one target is required")
	}

	id, err := insertJobRow(ctx, tx, startedAt, kind, taskName, planName, environment, params, StatusRunning, triggeredBy, parentJobID, stepOrder)
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
func insertJobRow(ctx context.Context, tx pgx.Tx, startedAt time.Time, kind JobKind, taskName, planName, environment string, params json.RawMessage, status, triggeredBy string, parentJobID *int64, stepOrder *int) (int64, error) {
	var id int64
	err := tx.QueryRow(ctx,
		`INSERT INTO jobs (kind, task_name, plan_name, environment, params, status, triggered_by, started_at, parent_job_id, step_order)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10) RETURNING id`,
		string(kind), nullIfEmpty(taskName), nullIfEmpty(planName), nullIfEmpty(environment), nullIfEmptyJSON(params), status, triggeredBy, startedAt, parentJobID, stepOrder,
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

// FailPlan ends a plan whose step failedStepOrder failed: every later
// step is closed as not run, on every one of its nodes, and the plan
// itself is failed - together, so a plan is never read as over with a
// step still open.
func (s *Store) FailPlan(ctx context.Context, planJobID int64, failedStepOrder int) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	at := s.now()
	if _, err := tx.Exec(ctx,
		`UPDATE job_targets SET status = $1, finished_at = $2, error_detail = $3
		 WHERE status IN ($4, $5)
		   AND job_id IN (SELECT id FROM jobs WHERE parent_job_id = $6 AND step_order > $7)`,
		StatusFailed, at, notRunReason(failedStepOrder+1), StatusPending, StatusRunning, planJobID, failedStepOrder); err != nil {
		return fmt.Errorf("close plan step targets: %w", err)
	}
	if _, err := tx.Exec(ctx,
		`UPDATE jobs SET status = $1, finished_at = $2
		 WHERE parent_job_id = $3 AND step_order > $4 AND status = $5`,
		StatusFailed, at, planJobID, failedStepOrder, StatusRunning); err != nil {
		return fmt.Errorf("close plan steps: %w", err)
	}
	tag, err := tx.Exec(ctx, `UPDATE jobs SET status = $1, finished_at = $2 WHERE id = $3`, StatusFailed, at, planJobID)
	if err != nil {
		return fmt.Errorf("fail plan: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit: %w", err)
	}
	return nil
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

// RecordTaskResult records a task target's terminal outcome like
// RecordTargetResult, also storing its structured result.
func (s *Store) RecordTaskResult(ctx context.Context, jobID int64, certname, status string, exitCode *int, output, errorDetail string, result json.RawMessage) error {
	at := s.now()
	tag, err := s.pool.Exec(ctx,
		`UPDATE job_targets
		 SET status = $1, started_at = COALESCE(started_at, $2), finished_at = $2,
		     exit_code = $3, output = $4, error_detail = $5, result = $6
		 WHERE job_id = $7 AND certname = $8`,
		status, at, exitCode, nullIfEmpty(output), nullIfEmpty(errorDetail), nullIfEmptyJSON(result), jobID, certname,
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
	// Parent lists the steps of this plan. Without it the list holds
	// no steps at all: a step is read through its plan.
	Parent *int64
	// Certnames limits the list to jobs that ran on at least one of
	// these nodes, and each job's target count and preview to them: a
	// caller's node group scope (internal/nodescope). Nil means no
	// restriction; an empty, non-nil list matches no job. A plan has no
	// targets of its own, so it matches through its steps.
	Certnames []string
}

// inScopeCondition is the SQL for "this jobs row ran on one of the
// certnames in $n" - directly, or for a plan through any of its steps.
func inScopeCondition(param int) string {
	return fmt.Sprintf(`(EXISTS (SELECT 1 FROM job_targets st WHERE st.job_id = jobs.id AND st.certname = ANY($%[1]d))
		OR EXISTS (SELECT 1 FROM jobs step JOIN job_targets st ON st.job_id = step.id
		           WHERE step.parent_job_id = jobs.id AND st.certname = ANY($%[1]d)))`, param)
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

	if filter.Certnames != nil {
		args = append(args, filter.Certnames)
		conditions = append(conditions, inScopeCondition(len(args)))
	}
	if filter.Parent != nil {
		args = append(args, *filter.Parent)
		conditions = append(conditions, fmt.Sprintf("parent_job_id = $%d", len(args)))
	} else {
		conditions = append(conditions, "parent_job_id IS NULL")
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
		`SELECT id, kind, COALESCE(task_name, ''), COALESCE(plan_name, ''), status, triggered_by, started_at, finished_at, parent_job_id, step_order,
		        CASE WHEN kind = 'run' THEN params END, target_source, schedule_id, schedule_name
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
		// A run's params are its options, which the list shows; a
		// task's are not loaded here.
		var runParams json.RawMessage
		var origin jobOriginColumns
		if err := rows.Scan(&j.ID, &kind, &j.TaskName, &j.PlanName, &j.Status, &j.TriggeredBy, &j.StartedAt, &j.FinishedAt, &j.ParentJobID, &j.StepOrder, &runParams,
			&origin.source, &origin.scheduleID, &origin.scheduleName); err != nil {
			rows.Close()
			return nil, 0, fmt.Errorf("list jobs: %w", err)
		}
		origin.apply(&j)
		j.Kind = JobKind(kind)
		j.Params = runParams
		j.setRunOptions()
		j.setPatchOptions()
		j.Params = nil
		jobs = append(jobs, j)
		ids = append(ids, j.ID)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, 0, fmt.Errorf("list jobs: %w", err)
	}
	rows.Close()

	previews, err := s.loadTargetPreviews(ctx, ids, filter.Certnames)
	if err != nil {
		return nil, 0, err
	}
	for i := range jobs {
		p := previews[jobs[i].ID]
		jobs[i].TargetCount = p.count
		jobs[i].TargetPreview = p.certnames
	}
	if err := s.hideQueriesOutsideScope(ctx, jobs, filter.Certnames); err != nil {
		return nil, 0, err
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
// covering every job in jobIDs. A non-nil certnames counts and previews
// only those nodes, so a scoped caller's list never names or counts a
// node outside their scope.
func (s *Store) loadTargetPreviews(ctx context.Context, jobIDs []int64, certnames []string) (map[int64]targetPreview, error) {
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
			WHERE job_id = ANY($1) AND ($3::text[] IS NULL OR certname = ANY($3))
		) ranked
		WHERE rn <= $2`,
		jobIDs, targetPreviewLimit, certnames,
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
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("load target previews: %w", err)
	}
	rows.Close()

	// A plan has no targets of its own. Its nodes are its steps', and
	// every step has the same ones, so the first step's stand for it.
	planRows, err := s.pool.Query(ctx, `
		SELECT plan_id, certname, cnt FROM (
			SELECT step.parent_job_id AS plan_id, jt.certname,
			       count(*) OVER (PARTITION BY step.parent_job_id) AS cnt,
			       row_number() OVER (PARTITION BY step.parent_job_id ORDER BY jt.id) AS rn
			FROM job_targets jt JOIN jobs step ON step.id = jt.job_id
			WHERE step.parent_job_id = ANY($1) AND step.step_order = 0
			  AND ($3::text[] IS NULL OR jt.certname = ANY($3))
		) ranked
		WHERE rn <= $2`,
		jobIDs, targetPreviewLimit, certnames,
	)
	if err != nil {
		return nil, fmt.Errorf("load plan target previews: %w", err)
	}
	defer planRows.Close()
	for planRows.Next() {
		var planID int64
		var certname string
		var cnt int
		if err := planRows.Scan(&planID, &certname, &cnt); err != nil {
			return nil, fmt.Errorf("load plan target previews: %w", err)
		}
		p := previews[planID]
		p.count = cnt
		p.certnames = append(p.certnames, certname)
		previews[planID] = p
	}
	return previews, planRows.Err()
}

// jobOriginColumns are a jobs row's target_source, schedule_id and
// schedule_name, as scanned.
type jobOriginColumns struct {
	source       []byte
	scheduleID   *int64
	scheduleName *string
}

func (c jobOriginColumns) apply(j *Job) {
	if len(c.source) > 0 {
		var source TargetSource
		if err := json.Unmarshal(c.source, &source); err == nil {
			j.TargetSource = &source
		}
	}
	if c.scheduleName != nil {
		j.Schedule = &ScheduleRef{Name: *c.scheduleName}
		if c.scheduleID != nil {
			j.Schedule.ID = *c.scheduleID
		}
	}
}

// hideQueriesOutsideScope blanks the query text of each job in jobs
// that ran on a node not among certnames (nil: no restriction). A query
// may name nodes its reader cannot see, so it is shown only to a reader
// who is shown everything it matched.
func (s *Store) hideQueriesOutsideScope(ctx context.Context, jobs []Job, certnames []string) error {
	if certnames == nil {
		return nil
	}
	var ids []int64
	for _, j := range jobs {
		if j.TargetSource != nil && j.TargetSource.Query != "" {
			ids = append(ids, j.ID)
		}
	}
	if len(ids) == 0 {
		return nil
	}
	rows, err := s.pool.Query(ctx,
		`SELECT DISTINCT job_id FROM job_targets WHERE job_id = ANY($1) AND NOT (certname = ANY($2))`,
		ids, certnames)
	if err != nil {
		return fmt.Errorf("check job query scope: %w", err)
	}
	defer rows.Close()
	hidden := map[int64]bool{}
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return fmt.Errorf("check job query scope: %w", err)
		}
		hidden[id] = true
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("check job query scope: %w", err)
	}
	for i := range jobs {
		if hidden[jobs[i].ID] {
			jobs[i].TargetSource.Query = ""
		}
	}
	return nil
}

// GetJob returns the job identified by id with its targets loaded, or
// ErrNotFound.
func (s *Store) GetJob(ctx context.Context, id int64) (Job, error) {
	var j Job
	var kind string
	var traceRaw []byte
	var origin jobOriginColumns
	err := s.pool.QueryRow(ctx,
		`SELECT id, kind, COALESCE(task_name, ''), COALESCE(plan_name, ''), COALESCE(environment, ''), params, status, triggered_by, started_at, finished_at, parent_job_id, step_order,
		        target_source, schedule_id, schedule_name, heartbeat_at, plan_trace, result, error
		 FROM jobs WHERE id = $1`,
		id,
	).Scan(&j.ID, &kind, &j.TaskName, &j.PlanName, &j.Environment, &j.Params, &j.Status, &j.TriggeredBy, &j.StartedAt, &j.FinishedAt, &j.ParentJobID, &j.StepOrder,
		&origin.source, &origin.scheduleID, &origin.scheduleName, &j.HeartbeatAt, &traceRaw, &j.Result, &j.Error)
	if errors.Is(err, pgx.ErrNoRows) {
		return Job{}, ErrNotFound
	}
	if err != nil {
		return Job{}, fmt.Errorf("get job: %w", err)
	}
	j.Kind = JobKind(kind)
	j.setRunOptions()
	j.setPatchOptions()
	if len(traceRaw) > 0 {
		if err := json.Unmarshal(traceRaw, &j.Trace); err != nil {
			return Job{}, fmt.Errorf("decode plan trace: %w", err)
		}
	}
	origin.apply(&j)

	targets, err := s.getJobTargets(ctx, id)
	if err != nil {
		return Job{}, err
	}
	j.Targets = targets

	if j.ParentJobID != nil {
		var ref PlanRef
		err := s.pool.QueryRow(ctx,
			`SELECT p.id, COALESCE(p.plan_name, ''), (SELECT count(*) FROM jobs st WHERE st.parent_job_id = p.id)
			 FROM jobs p WHERE p.id = $1`, *j.ParentJobID).Scan(&ref.ID, &ref.Name, &ref.Steps)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return Job{}, fmt.Errorf("get job's plan: %w", err)
		}
		if err == nil {
			j.Plan = &ref
		}
	}

	return j, nil
}

// GetJobWithSteps is GetJob, with a plan's steps loaded.
func (s *Store) GetJobWithSteps(ctx context.Context, id int64) (Job, error) {
	j, err := s.GetJob(ctx, id)
	if err != nil || j.Kind != JobKindPlan {
		return j, err
	}
	steps, err := s.GetPlanSteps(ctx, id)
	if err != nil {
		return Job{}, err
	}
	j.Steps = stepViews(j, steps)
	return j, nil
}

// GetJobInScope is GetJob for a caller limited to certnames (their node
// group scope; nil means no restriction, and is then exactly GetJob).
// The job is returned with only the targets among certnames, and
// TargetsHidden set when any were left out. A job none of whose nodes
// are among certnames is ErrNotFound - the same answer as a job that
// does not exist. A plan has no targets of its own and is found when
// any of its steps ran on one of certnames.
func (s *Store) GetJobInScope(ctx context.Context, id int64, certnames []string) (Job, error) {
	j, err := s.GetJobWithSteps(ctx, id)
	if err != nil || certnames == nil {
		return j, err
	}

	var visible bool
	if err := s.pool.QueryRow(ctx, "SELECT "+inScopeCondition(2)+" FROM jobs WHERE id = $1", id, certnames).Scan(&visible); err != nil {
		return Job{}, fmt.Errorf("check job scope: %w", err)
	}
	if !visible {
		return Job{}, ErrNotFound
	}

	allowed := make(map[string]bool, len(certnames))
	for _, c := range certnames {
		allowed[c] = true
	}
	kept := make([]JobTarget, 0, len(j.Targets))
	for _, t := range j.Targets {
		if allowed[t.Certname] {
			kept = append(kept, t)
		}
	}
	j.TargetsHidden = len(kept) != len(j.Targets)
	if j.TargetsHidden && j.TargetSource != nil {
		// The query may name nodes this reader cannot see.
		j.TargetSource.Query = ""
	}
	if j.Kind == JobKindCodePlan {
		// A code plan's nodes are those of its step jobs. When some are
		// hidden, what the plan printed, returned or was given may name
		// them, so none of that is shown.
		var hidden bool
		if err := s.pool.QueryRow(ctx,
			`SELECT EXISTS (SELECT 1 FROM jobs step JOIN job_targets st ON st.job_id = step.id
			                WHERE step.parent_job_id = $1 AND NOT (st.certname = ANY($2)))`,
			id, certnames).Scan(&hidden); err != nil {
			return Job{}, fmt.Errorf("check code plan scope: %w", err)
		}
		j.TargetsHidden = hidden
		if hidden {
			for i := range j.Trace {
				j.Trace[i].Message, j.Trace[i].Error = "", ""
			}
			j.Result, j.Error, j.Params, j.TraceHidden = nil, "", nil, true
			if j.TargetSource != nil {
				j.TargetSource.Query = ""
			}
		}
	}
	if j.Kind == JobKindPlan {
		// A plan's nodes are its steps' nodes. Whether any of those are
		// hidden is what the caller needs to know about the plan.
		var hidden bool
		if err := s.pool.QueryRow(ctx,
			`SELECT EXISTS (SELECT 1 FROM jobs step JOIN job_targets st ON st.job_id = step.id
			                WHERE step.parent_job_id = $1 AND NOT (st.certname = ANY($2)))`,
			id, certnames).Scan(&hidden); err != nil {
			return Job{}, fmt.Errorf("check plan scope: %w", err)
		}
		j.TargetsHidden = hidden

		// Each step shows only the nodes in scope, and a step with
		// none is not shown.
		steps := make([]PlanStepView, 0, len(j.Steps))
		for _, step := range j.Steps {
			shown := make([]JobTarget, 0, len(step.Targets))
			for _, t := range step.Targets {
				if allowed[t.Certname] {
					shown = append(shown, t)
				}
			}
			if len(shown) == 0 {
				continue
			}
			step.Targets = shown
			steps = append(steps, step)
		}
		j.Steps = steps
	}
	j.Targets = kept
	return j, nil
}

func (s *Store) getJobTargets(ctx context.Context, jobID int64) ([]JobTarget, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT id, job_id, certname, status, started_at, finished_at, exit_code,
		        COALESCE(output, ''), COALESCE(error_detail, ''), COALESCE(report_hash, ''), result, batch, heartbeat_at
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
			&t.ExitCode, &t.Output, &t.ErrorDetail, &t.ReportHash, &t.Result, &t.Batch, &t.HeartbeatAt); err != nil {
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
