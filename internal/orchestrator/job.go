// Package orchestrator implements on-demand job orchestration on top of
// internal/nodetransport: translating run/task/plan requests into
// dispatch requests sent to a target's live node-agent connection,
// tracking each job's lifecycle in Postgres, and correlating a completed
// run with its openvoxdb report. See openspec/specs/orchestrator.
package orchestrator

import (
	"encoding/json"
	"fmt"
	"time"
)

// JobKind is what a Job does.
type JobKind string

// Job kind values.
const (
	JobKindRun  JobKind = "run"
	JobKindTask JobKind = "task"
	JobKindPlan JobKind = "plan"
	// JobKindPatch applies pending updates in batches (see patch.go).
	JobKindPatch JobKind = "patch"
	// JobKindCodePlan is the run of a plan from environment code; its task
	// steps are child jobs (see codeplanstore.go).
	JobKindCodePlan JobKind = "codeplan"
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
	ID       int64   `json:"id"`
	Kind     JobKind `json:"kind"`
	TaskName string  `json:"taskName,omitempty"`
	PlanName string  `json:"planName,omitempty"`
	// Environment is the code environment a task job's task was taken
	// from. Empty for runs and plans.
	Environment string `json:"environment,omitempty"`
	// Params are as stored: a task's sensitive parameters are already
	// redacted here (see redactSensitive) - the real values never reach
	// Postgres.
	Params json.RawMessage `json:"params,omitempty"`
	// RunOptions are a run's options, when it has any: derived from
	// Params, which is where they are stored. Nil for a plain run.
	RunOptions *RunOptions `json:"runOptions,omitempty"`
	// PatchOptions are a patch run's options, derived from Params.
	PatchOptions *PatchOptions `json:"patchOptions,omitempty"`
	// HeartbeatAt is when a patch run or code plan last reported progress.
	HeartbeatAt *time.Time `json:"heartbeatAt,omitempty"`
	// Trace, Result and Error are a code plan's: what each step did, what
	// the plan returned, and why it failed. TraceHidden says the reader's
	// node scope left the messages and result out.
	Trace       []TraceEntry    `json:"planTrace,omitempty"`
	Result      json.RawMessage `json:"result,omitempty"`
	Error       string          `json:"error,omitempty"`
	TraceHidden bool            `json:"traceHidden,omitempty"`
	Status      string          `json:"status"`
	TriggeredBy string          `json:"triggeredBy"`
	StartedAt   time.Time       `json:"startedAt"`
	FinishedAt  *time.Time      `json:"finishedAt,omitempty"`
	Targets     []JobTarget     `json:"targets,omitempty"`
	ParentJobID *int64          `json:"parentJobId,omitempty"`
	StepOrder   *int            `json:"stepOrder,omitempty"`

	// Steps are a plan's steps in order, set when one plan is read.
	Steps []PlanStepView `json:"steps,omitempty"`
	// Plan is the plan a step belongs to, set when a step is read.
	Plan *PlanRef `json:"plan,omitempty"`

	// TargetCount and TargetPreview are populated by ListJobs only (GetJob
	// leaves them zero/nil in favor of the full Targets slice). They give
	// list views a cheap way to show which node(s) a job ran against
	// without loading every target's full detail (output, exit code, etc.)
	// for every job on the page.
	TargetCount   int      `json:"targetCount,omitempty"`
	TargetPreview []string `json:"targetPreview,omitempty"`

	// TargetSource is the query or node group the targets were resolved
	// from when the job started. Nil for a job given a list of nodes.
	TargetSource *TargetSource `json:"targetSource,omitempty"`
	// Schedule is the schedule that started the job, if one did.
	Schedule *ScheduleRef `json:"schedule,omitempty"`
	// Agent is the agent request that was approved to start the job.
	Agent *AgentRef `json:"agentRequest,omitempty"`

	// TargetsHidden is set on a job read by a caller whose node group
	// scope does not cover every node the job ran on: some results are
	// left out. The job's own Status is as stored, so it can reflect a
	// node the caller is not shown - which is what this flag explains.
	TargetsHidden bool `json:"targetsHidden,omitempty"`
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
	// Batch is the batch a patch run patched this node in, from 1.
	Batch *int `json:"batch,omitempty"`
	// HeartbeatAt is when this node last reported progress.
	HeartbeatAt *time.Time `json:"heartbeatAt,omitempty"`
	// ReportEnvironment is the environment the run's report records.
	// Set only when reading a job that requested an environment and
	// whose report has been found, so the two can be compared.
	ReportEnvironment string `json:"reportEnvironment,omitempty"`
	// Result is a task target's structured result: the JSON object the
	// task printed, or its text under "_output". Empty for runs.
	Result json.RawMessage `json:"result,omitempty"`
}

// originNote says, for an activity summary, what chose the job's
// targets and what started it, when it was not somebody naming nodes
// by hand. Empty otherwise.
func (j Job) originNote() string {
	note := ""
	if j.TargetSource != nil {
		switch j.TargetSource.Kind {
		case TargetKindQuery:
			note += " (matched by a query)"
		case TargetKindGroup:
			note += fmt.Sprintf(" (node group %q)", j.TargetSource.GroupName)
		}
	}
	if j.Schedule != nil {
		note += fmt.Sprintf(", started by schedule %q", j.Schedule.Name)
	}
	if j.Agent != nil {
		note += fmt.Sprintf(", requested by agent %q and approved by %s", j.Agent.Token, j.Agent.ApprovedBy)
	}
	return note
}
