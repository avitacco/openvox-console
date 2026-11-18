package orchestrator

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
)

// RunOptions are the ways a Puppet run can differ from a plain one. The
// zero value is a plain run.
type RunOptions struct {
	// Noop reports what the run would change and changes nothing.
	Noop bool `json:"noop,omitempty"`
	// Environment asks the node to run against this environment. It is
	// a request: when the node's classification sets an environment,
	// Puppet uses that one instead.
	Environment string `json:"environment,omitempty"`
	// Debug turns on debug-level output.
	Debug bool `json:"debug,omitempty"`
}

// IsZero reports whether o asks for a plain run.
func (o RunOptions) IsZero() bool { return o == RunOptions{} }

// describe names the options for the activity log, or "" for a plain
// run: "no-op, environment feature_x, debug".
func (o RunOptions) describe() string {
	var parts []string
	if o.Noop {
		parts = append(parts, "no-op")
	}
	if o.Environment != "" {
		parts = append(parts, "environment "+o.Environment)
	}
	if o.Debug {
		parts = append(parts, "debug")
	}
	return strings.Join(parts, ", ")
}

// environmentName is Puppet's own rule for an environment's name.
var environmentName = regexp.MustCompile(`^[A-Za-z0-9_]+$`)

// InvalidRunOptionsError is a run request the console refuses for its
// options, before any job exists.
type InvalidRunOptionsError struct{ Reason string }

func (e *InvalidRunOptionsError) Error() string { return e.Reason }

// decodeRunOptions reads a request's options. An option this console
// does not know is refused rather than ignored: a run that silently
// lacked an option its caller asked for is the failure run options must
// not have.
func decodeRunOptions(raw json.RawMessage) (RunOptions, error) {
	var o RunOptions
	if len(raw) == 0 || string(raw) == "null" {
		return o, nil
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&o); err != nil {
		return RunOptions{}, &InvalidRunOptionsError{Reason: "invalid run options: " + err.Error()}
	}
	if o.Environment != "" && !environmentName.MatchString(o.Environment) {
		return RunOptions{}, &InvalidRunOptionsError{Reason: fmt.Sprintf("invalid environment name %q: only letters, digits and underscores are allowed", o.Environment)}
	}
	return o, nil
}

// EnvironmentLister names the environments the Puppet server has.
// *taskcatalog.Client is one.
type EnvironmentLister interface {
	Environments(ctx context.Context) ([]string, error)
}

// SetEnvironments lets a run that names an environment be refused when
// the server is known not to have it. Without one, or when the server
// cannot be asked, the name is taken on trust: a server that cannot be
// reached will fail the run in its own words anyway.
func (h *Handlers) SetEnvironments(l EnvironmentLister) { h.starter.environments = l }

// ReportEnvironments finds the environment each report records, keyed
// by report hash. *openvoxdb.Client's ReportEnvironments is one.
type ReportEnvironments func(ctx context.Context, hashes []string) (map[string]string, error)

// SetReportEnvironments lets a job that asked for an environment show,
// per node, the environment its run actually used.
func (h *Handlers) SetReportEnvironments(f ReportEnvironments) { h.reportEnvironments = f }

// fillReportEnvironments sets ReportEnvironment on the targets of a run
// that requested an environment and has reports to compare it with. It
// is decoration: a failure to read the reports leaves the job as it is.
func (h *Handlers) fillReportEnvironments(ctx context.Context, job *Job) {
	if h.reportEnvironments == nil || job.RunOptions == nil || job.RunOptions.Environment == "" {
		return
	}
	var hashes []string
	for _, t := range job.Targets {
		if t.ReportHash != "" {
			hashes = append(hashes, t.ReportHash)
		}
	}
	if len(hashes) == 0 {
		return
	}
	envs, err := h.reportEnvironments(ctx, hashes)
	if err != nil {
		return
	}
	for i := range job.Targets {
		job.Targets[i].ReportEnvironment = envs[job.Targets[i].ReportHash]
	}
}

// setRunOptions derives RunOptions from a run job's stored params. A
// run with no stored options - every run made before options existed,
// and every plain run since - has none.
func (j *Job) setRunOptions() {
	if j.Kind != JobKindRun || len(j.Params) == 0 {
		return
	}
	var o RunOptions
	if err := json.Unmarshal(j.Params, &o); err != nil || o.IsZero() {
		return
	}
	j.RunOptions = &o
}

// clientTooOld is the reason recorded for a target whose node agent
// client does not know the run-with-options request. Such a client has
// not run Puppet: it refuses the request rather than carrying it out as
// a plain run.
const clientTooOld = "this node's agent client is too old for run options (no-op, environment, debug); upgrade the client and run again. Puppet was not run."

// unsupportedRunWithOptions reports whether reason is a client's
// refusal of the run-with-options action it does not know.
func unsupportedRunWithOptions(reason string) bool {
	return strings.Contains(reason, "unsupported action") && strings.Contains(reason, actionRunWithOptions)
}

var errNoOptions = errors.New("run has no options")

// runOptionsParams is the request parameters for a run with options.
func runOptionsParams(job Job) (json.RawMessage, error) {
	if job.RunOptions == nil || job.RunOptions.IsZero() {
		return nil, errNoOptions
	}
	return json.Marshal(job.RunOptions)
}
