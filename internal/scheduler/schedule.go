// Package scheduler saves Puppet runs and tasks to start later, once or
// repeatedly, and starts them when they come due
// (add-pql-targeting-and-scheduled-jobs; see openspec/specs/scheduled-jobs).
//
// It adds "when" and "as whom" to the orchestrator and nothing else. A
// firing is checked and started through orchestrator.Starter, exactly
// as a request made over HTTP at that moment would be: its targets are
// resolved then, with the owner's permissions and node scope as they
// are then.
//
// Firing is at most once. An instance claims a due time by moving the
// schedule's next start forward with a compare-and-set update, and only
// then starts the job; if it stops in between, the firing is lost
// rather than repeated. Running Puppet twice, or a task such as a
// reboot twice, is worse than not running it.
package scheduler

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/voxpupuli/enterprise-console/internal/orchestrator"
)

// Owner kinds. A username and a service token's name share one string
// space, so the kind is part of who an owner is.
const (
	OwnerUser         = "user"
	OwnerServiceToken = "service_token"
)

// Owner is who a schedule runs as.
type Owner struct {
	Name string `json:"name"`
	Kind string `json:"kind"`
}

// Repeat units. Each is a fixed length of time: a day is 24 hours, so a
// daily schedule keeps its UTC time and moves by an hour in local time
// where the clocks change.
const (
	UnitMinute = "minute"
	UnitHour   = "hour"
	UnitDay    = "day"
	UnitWeek   = "week"
)

var unitLength = map[string]time.Duration{
	UnitMinute: time.Minute,
	UnitHour:   time.Hour,
	UnitDay:    24 * time.Hour,
	UnitWeek:   7 * 24 * time.Hour,
}

// MinInterval is the shortest repetition a schedule may have.
const MinInterval = 5 * time.Minute

// LateLimit is how long after it came due a firing may still be
// started. Past it the moment has gone - a run meant for a maintenance
// window must not start the next morning because the console was down
// overnight - and the firing is recorded as missed.
const LateLimit = 15 * time.Minute

// interruptedAfter is how long a firing may sit claimed with no outcome
// before it is shown as interrupted.
const interruptedAfter = 10 * time.Minute

// MaxNameLength bounds a schedule's name.
const MaxNameLength = 100

// Repeat is "every N units". A schedule without one starts once.
type Repeat struct {
	Every int    `json:"every"`
	Unit  string `json:"unit"`
}

// Interval is the repetition as a length of time.
func (r Repeat) Interval() time.Duration {
	return time.Duration(r.Every) * unitLength[r.Unit]
}

// Schedule states, derived from Enabled and NextRunAt.
const (
	StateActive   = "active"
	StatePaused   = "paused"
	StateFinished = "finished"
)

// Firing outcomes. OutcomeFiring is a claim whose outcome has not been
// written; one that stays so is shown as OutcomeInterrupted.
const (
	OutcomeFiring      = "firing"
	OutcomeStarted     = "started"
	OutcomeSkipped     = "skipped"
	OutcomeMissed      = "missed"
	OutcomeInterrupted = "interrupted"
)

// Firing is one time a schedule came due, and what happened.
type Firing struct {
	ID      int64     `json:"id"`
	DueAt   time.Time `json:"dueAt"`
	FiredAt time.Time `json:"firedAt"`
	Outcome string    `json:"outcome"`
	Detail  string    `json:"detail,omitempty"`
	JobID   *int64    `json:"jobId,omitempty"`
}

// Schedule is a run, a task or a plan saved to start later.
type Schedule struct {
	ID   int64                `json:"id"`
	Name string               `json:"name"`
	Kind orchestrator.JobKind `json:"kind"`

	// Options are a run's options, as given.
	Options json.RawMessage `json:"options,omitempty"`

	Task           string `json:"task,omitempty"`
	Environment    string `json:"environment,omitempty"`
	TimeoutSeconds int    `json:"timeoutSeconds,omitempty"`
	// Params are a task's parameters with sensitive values redacted.
	// The values themselves are sealed, and never leave the store
	// except to be sent to nodes.
	Params json.RawMessage `json:"params,omitempty"`
	sealed []byte

	// PlanName and Steps are a plan's: the schedule's own copy of the
	// steps, taken when it was saved. It does not follow a saved plan,
	// which others can edit, because it runs with its owner's access.
	// Task steps' parameters are redacted as Params are; their
	// sensitive values are sealed together, by step.
	PlanName string                  `json:"planName,omitempty"`
	Steps    []orchestrator.PlanStep `json:"steps,omitempty"`

	// The target, spelled as a run or task request spells it.
	Targets []string `json:"targets,omitempty"`
	Query   string   `json:"query,omitempty"`
	Group   *int64   `json:"group,omitempty"`

	StartAt   time.Time  `json:"startAt"`
	Repeat    *Repeat    `json:"repeat,omitempty"`
	NextRunAt *time.Time `json:"nextRunAt,omitempty"`
	Enabled   bool       `json:"enabled"`
	State     string     `json:"state"`

	Owner     string    `json:"owner"`
	OwnerKind string    `json:"ownerKind"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`

	// LastFiring is set when schedules are listed; Firings, newest
	// first, when one is read.
	LastFiring *Firing  `json:"lastFiring,omitempty"`
	Firings    []Firing `json:"firings,omitempty"`
}

// Target is the schedule's target as the orchestrator takes it.
func (s Schedule) Target() orchestrator.Target {
	return orchestrator.Target{Nodes: s.Targets, Query: s.Query, Group: s.Group}
}

// OwnedBy reports whether owner owns the schedule.
func (s Schedule) OwnedBy(owner Owner) bool {
	return s.Owner == owner.Name && s.OwnerKind == owner.Kind
}

func (s *Schedule) setState() {
	switch {
	case !s.Enabled:
		s.State = StatePaused
	case s.NextRunAt == nil:
		s.State = StateFinished
	default:
		s.State = StateActive
	}
}

// InvalidScheduleError is a schedule refused for its own fields - its
// name or when it starts - as opposed to its action or target, which
// the orchestrator refuses in its own words.
type InvalidScheduleError struct {
	Field  string
	Reason string
}

func (e *InvalidScheduleError) Error() string { return e.Reason }

// ErrNotFound is a schedule that does not exist, or that the caller may
// not see: the two are deliberately one answer.
var ErrNotFound = errors.New("schedule not found")

// checkTiming validates name, start and repetition for a schedule saved
// at now.
func checkTiming(name string, startAt time.Time, repeat *Repeat, now time.Time) error {
	switch {
	case strings.TrimSpace(name) == "":
		return &InvalidScheduleError{Field: "name", Reason: "a name is required"}
	case len(name) > MaxNameLength:
		return &InvalidScheduleError{Field: "name", Reason: fmt.Sprintf("the name is longer than %d characters", MaxNameLength)}
	case startAt.IsZero():
		return &InvalidScheduleError{Field: "startAt", Reason: "a start time is required"}
	}
	if repeat == nil {
		if !startAt.After(now) {
			return &InvalidScheduleError{Field: "startAt", Reason: "the start time has passed; a schedule that does not repeat must start in the future"}
		}
		return nil
	}
	if _, ok := unitLength[repeat.Unit]; !ok {
		return &InvalidScheduleError{Field: "repeat", Reason: "repeat unit must be minute, hour, day or week"}
	}
	if repeat.Every < 1 {
		return &InvalidScheduleError{Field: "repeat", Reason: "repeat every must be at least 1"}
	}
	if repeat.Every > 10000 {
		return &InvalidScheduleError{Field: "repeat", Reason: "repeat every is too large"}
	}
	if repeat.Interval() < MinInterval {
		return &InvalidScheduleError{Field: "repeat", Reason: "a schedule cannot repeat more often than every 5 minutes"}
	}
	return nil
}

// nextStart is when a schedule starting at startAt next comes due after
// now: startAt itself while that is still ahead, otherwise the first
// whole interval after it that is. Nil when nothing further is due - a
// schedule that does not repeat and whose start has passed.
//
// However much time has passed, the answer is one moment on the
// schedule's own grid: a console that was down over several repeats
// resumes at the next one rather than running each it missed.
func nextStart(startAt time.Time, repeat *Repeat, now time.Time) *time.Time {
	if startAt.After(now) {
		return &startAt
	}
	if repeat == nil {
		return nil
	}
	interval := repeat.Interval()
	passed := now.Sub(startAt) / interval
	next := startAt.Add((passed + 1) * interval)
	return &next
}
