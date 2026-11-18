package orchestrator

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// TraceEntry is one step of a code plan's run. Steps of a plan the plan
// called are in the same trace, one level deeper, so a reader sees the whole
// run in the order it happened.
type TraceEntry struct {
	// Seq numbers the entries of a run from 1, in the order they began.
	Seq int `json:"seq"`
	// Path is the step's place: "2" for the second step of the plan, "2.1"
	// for the first step of the plan it called.
	Path  string `json:"path"`
	Depth int    `json:"depth"`
	// Plan is the plan whose step this is.
	Plan        string `json:"plan"`
	Name        string `json:"name,omitempty"`
	Description string `json:"description,omitempty"`
	// Kind is task, plan or message.
	Kind       string `json:"kind"`
	Task       string `json:"task,omitempty"`
	PlanCalled string `json:"planCalled,omitempty"`
	// State is running, succeeded, failed or not_run.
	State      string     `json:"state"`
	StartedAt  *time.Time `json:"startedAt,omitempty"`
	FinishedAt *time.Time `json:"finishedAt,omitempty"`
	// Message is what a message step printed, or what a failure was.
	Message string `json:"message,omitempty"`
	// JobID is the task step's job.
	JobID *int64 `json:"jobId,omitempty"`
	Error string `json:"error,omitempty"`
}

// CreateCodePlanJob records the run of a plan from code. params are the
// parameters given, already redacted; the job has no targets of its own and
// starts with a heartbeat.
func (s *Store) CreateCodePlanJob(ctx context.Context, name, environment string, params json.RawMessage, triggeredBy string, origin JobOrigin) (Job, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Job{}, fmt.Errorf("begin transaction: %w", err)
	}
	defer tx.Rollback(ctx)
	at := s.now()
	id, err := insertJobRow(ctx, tx, at, JobKindCodePlan, "", name, environment, params, StatusRunning, triggeredBy, nil, nil)
	if err != nil {
		return Job{}, err
	}
	if _, err := tx.Exec(ctx, `UPDATE jobs SET heartbeat_at = $2, plan_trace = '[]' WHERE id = $1`, id, at); err != nil {
		return Job{}, fmt.Errorf("start code plan: %w", err)
	}
	if err := setOrigin(ctx, tx, id, origin); err != nil {
		return Job{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Job{}, fmt.Errorf("commit: %w", err)
	}
	return s.GetJob(ctx, id)
}

// SaveTrace stores a code plan's trace and records that it is alive.
func (s *Store) SaveTrace(ctx context.Context, jobID int64, trace []TraceEntry) error {
	raw, err := json.Marshal(trace)
	if err != nil {
		return fmt.Errorf("encode plan trace: %w", err)
	}
	tag, err := s.pool.Exec(ctx, `UPDATE jobs SET plan_trace = $2, heartbeat_at = $3 WHERE id = $1 AND status = $4`, jobID, raw, s.now(), StatusRunning)
	if err != nil {
		return fmt.Errorf("save plan trace: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// FinishCodePlan ends a code plan with its final trace, the value it
// returned and, when it failed, why. It does nothing for a job already
// ended - the reaper may have failed it - and reports whether it ended it.
func (s *Store) FinishCodePlan(ctx context.Context, jobID int64, status string, trace []TraceEntry, result json.RawMessage, reason string) (bool, error) {
	raw, err := json.Marshal(trace)
	if err != nil {
		return false, fmt.Errorf("encode plan trace: %w", err)
	}
	tag, err := s.pool.Exec(ctx,
		`UPDATE jobs SET status = $2, finished_at = $3, plan_trace = $4, result = $5, error = $6 WHERE id = $1 AND status = $7`,
		jobID, status, s.now(), raw, nullIfEmptyJSON(result), reason, StatusRunning)
	if err != nil {
		return false, fmt.Errorf("finish code plan: %w", err)
	}
	return tag.RowsAffected() > 0, nil
}

// JobKindOf returns a job's kind, or ErrNotFound.
func (s *Store) JobKindOf(ctx context.Context, id int64) (JobKind, error) {
	var kind string
	if err := s.pool.QueryRow(ctx, `SELECT kind FROM jobs WHERE id = $1`, id).Scan(&kind); errors.Is(err, pgx.ErrNoRows) {
		return "", ErrNotFound
	} else if err != nil {
		return "", fmt.Errorf("read job kind: %w", err)
	}
	return JobKind(kind), nil
}
