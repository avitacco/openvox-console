package orchestrator

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/voxpupuli/enterprise-console/internal/taskcatalog"
)

// TaskCatalog is the subset of *taskcatalog.Client the orchestrator
// needs: fetching one task's metadata and files from an environment. A
// nil *taskcatalog.Client satisfies it and reports
// taskcatalog.ErrNotConfigured.
type TaskCatalog interface {
	Task(ctx context.Context, environment, name string) (taskcatalog.Task, error)
}

// Task timeouts (see specs/orchestrator's "Task timeout" requirement).
// The maximum leaves the node five minutes to report its own timeout
// before the console stops waiting for the dispatch at dispatchTimeout,
// so a timed-out task is always reported as one rather than as a lost
// dispatch.
const (
	DefaultTaskTimeout = 30 * time.Minute
	MaxTaskTimeout     = dispatchTimeout - 5*time.Minute
)

// DefaultTaskEnvironment is where a task is taken from when a request
// names no environment, as in Puppet.
const DefaultTaskEnvironment = "production"

// RedactedValue replaces a sensitive parameter's value wherever a job's
// parameters are stored or shown.
const RedactedValue = "Sensitive [value redacted]"

// InvalidTaskError reports a task request the console refused before
// creating any job: an unknown task, invalid parameters, a bad timeout,
// or no task catalogue configured. Err is the underlying cause, and
// decides the HTTP status (see taskcatalog.HTTPStatus).
type InvalidTaskError struct {
	Err error
}

func (e *InvalidTaskError) Error() string { return e.Err.Error() }
func (e *InvalidTaskError) Unwrap() error { return e.Err }

// preparedTask is a task resolved and validated for dispatch: the payload
// the node gets (real parameter values) and the parameters as stored
// (sensitive values redacted).
type preparedTask struct {
	payload      taskcatalog.Resolved
	storedParams json.RawMessage
	// sensitive holds the real values of the sensitive parameters, for
	// a caller that must keep them to start the task later.
	sensitive map[string]any
}

// prepareTask resolves name in environment, validates params against its
// metadata, and builds the dispatch payload. timeoutSeconds of zero means
// DefaultTaskTimeout.
func prepareTask(ctx context.Context, catalog TaskCatalog, environment, name string, params json.RawMessage, timeoutSeconds int) (preparedTask, error) {
	if catalog == nil {
		return preparedTask{}, &InvalidTaskError{Err: taskcatalog.ErrNotConfigured}
	}
	if environment == "" {
		environment = DefaultTaskEnvironment
	}
	if err := checkTaskTimeout(name, timeoutSeconds); err != nil {
		return preparedTask{}, err
	}
	timeout := DefaultTaskTimeout
	if timeoutSeconds != 0 {
		timeout = time.Duration(timeoutSeconds) * time.Second
	}

	task, err := catalog.Task(ctx, environment, name)
	if err != nil {
		return preparedTask{}, &InvalidTaskError{Err: err}
	}
	values, err := taskcatalog.Validate(task, params)
	if err != nil {
		return preparedTask{}, &InvalidTaskError{Err: err}
	}
	payload, err := taskcatalog.Resolve(task)
	if err != nil {
		return preparedTask{}, &InvalidTaskError{Err: err}
	}
	payload.Params = values
	payload.TimeoutSeconds = int(timeout / time.Second)

	stored, err := json.Marshal(redactSensitive(task.Metadata, values))
	if err != nil {
		return preparedTask{}, fmt.Errorf("encode parameters: %w", err)
	}
	sensitive := map[string]any{}
	for name, v := range values {
		if task.Metadata.Parameters[name].Sensitive {
			sensitive[name] = v
		}
	}
	return preparedTask{payload: payload, storedParams: stored, sensitive: sensitive}, nil
}

// checkTaskTimeout refuses a timeout outside what a task may be given.
// Zero is the default.
func checkTaskTimeout(task string, timeoutSeconds int) error {
	if timeoutSeconds == 0 {
		return nil
	}
	if timeout := time.Duration(timeoutSeconds) * time.Second; timeout <= 0 || timeout > MaxTaskTimeout {
		return &InvalidTaskError{Err: &taskcatalog.ValidationError{Task: task, Problems: []taskcatalog.Problem{{
			Param:   "timeoutSeconds",
			Message: fmt.Sprintf("must be between 1 and %d", int(MaxTaskTimeout/time.Second)),
		}}}}
	}
	return nil
}

// redactSensitive returns a copy of values with every parameter the
// metadata marks sensitive replaced by RedactedValue.
func redactSensitive(md taskcatalog.Metadata, values map[string]any) map[string]any {
	out := make(map[string]any, len(values))
	for name, v := range values {
		if md.Parameters[name].Sensitive {
			v = RedactedValue
		}
		out[name] = v
	}
	return out
}
