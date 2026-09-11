// Package orchestrator implements on-demand job orchestration on top of
// internal/nodetransport: translating run/task/plan requests into
// dispatch requests sent to a target's live node-agent connection,
// tracking each job's lifecycle in Postgres, and correlating a completed
// run with its openvoxdb report. See openspec/specs/orchestrator.
package orchestrator

import (
	"encoding/json"
	"time"
)

// JobKind is what a Job does.
type JobKind string

// Job kind values.
const (
	JobKindRun  JobKind = "run"
	JobKindTask JobKind = "task"
	JobKindPlan JobKind = "plan"
)

// Job and job-target status values. A target starts StatusPending
// (recorded but not yet dispatched), moves to StatusRunning once a
// request is sent, and ends at a terminal status. A job itself starts
// directly at StatusRunning (dispatch begins immediately on creation,
// unlike a target - see Store.CreateJob).
const (
	StatusPending   = "pending"
	StatusRunning   = "running"
	StatusSucceeded = "succeeded"
	StatusFailed    = "failed"
)

// Job is one orchestration request (a run, a task, or a plan) against
// one or more targets. A plan's steps are themselves Jobs (kind run or
// task) with ParentJobID set to the plan's ID and StepOrder giving
// their sequence (0-indexed) - see Store.CreatePlan.
type Job struct {
	ID          int64           `json:"id"`
	Kind        JobKind         `json:"kind"`
	TaskName    string          `json:"taskName,omitempty"`
	PlanName    string          `json:"planName,omitempty"`
	Params      json.RawMessage `json:"params,omitempty"`
	Status      string          `json:"status"`
	TriggeredBy string          `json:"triggeredBy"`
	StartedAt   time.Time       `json:"startedAt"`
	FinishedAt  *time.Time      `json:"finishedAt,omitempty"`
	Targets     []JobTarget     `json:"targets,omitempty"`
	ParentJobID *int64          `json:"parentJobId,omitempty"`
	StepOrder   *int            `json:"stepOrder,omitempty"`

	// TargetCount and TargetPreview are populated by ListJobs only (GetJob
	// leaves them zero/nil in favor of the full Targets slice). They give
	// list views a cheap way to show which node(s) a job ran against
	// without loading every target's full detail (output, exit code, etc.)
	// for every job on the page.
	TargetCount   int      `json:"targetCount,omitempty"`
	TargetPreview []string `json:"targetPreview,omitempty"`
}

// JobTarget is one node's result within a Job.
type JobTarget struct {
	ID          int64      `json:"id"`
	JobID       int64      `json:"jobId"`
	Certname    string     `json:"certname"`
	Status      string     `json:"status"`
	StartedAt   *time.Time `json:"startedAt,omitempty"`
	FinishedAt  *time.Time `json:"finishedAt,omitempty"`
	ExitCode    *int       `json:"exitCode,omitempty"`
	Output      string     `json:"output,omitempty"`
	ErrorDetail string     `json:"errorDetail,omitempty"`
	ReportHash  string     `json:"reportHash,omitempty"`
}
