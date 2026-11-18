package nodeagent

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"regexp"
)

// actionRunWithOptions is a Puppet run with options - see runOptions.
// It is its own action rather than parameters on actionRun: a client
// that predates it ignores parameters on actionRun, and would carry out
// a no-op request as an enforcing run. An action it does not know, it
// refuses - which is the behaviour a console sending this relies on.
const actionRunWithOptions = "run_with_options"

// runOptions mirrors internal/orchestrator's RunOptions (the wire
// contract, not the Go type, is what the two share).
type runOptions struct {
	Noop        bool   `json:"noop,omitempty"`
	Environment string `json:"environment,omitempty"`
	Debug       bool   `json:"debug,omitempty"`
}

// environmentName is Puppet's own rule for an environment's name. The
// console checks it too; it is checked again here because this is where
// the name becomes a command-line argument.
var environmentName = regexp.MustCompile(`^[A-Za-z0-9_]+$`)

// maxRunOutput bounds what a run with options sends back. A debug run
// of a large catalog prints megabytes; the first part is where a
// failure's cause is, and the whole response has to fit in one message.
const maxRunOutput = maxTaskStdout

// plainRunArgs is the Puppet run this client has always performed.
func plainRunArgs() []string {
	return []string{"agent", "--onetime", "--no-daemonize", "--detailed-exitcodes"}
}

// runWithOptionsArgs is plainRunArgs with o applied.
//
// --verbose is added whenever --debug is not: without one of them the
// agent prints nothing, and what a no-op run would change is the whole
// point of running it. A plain run stays silent, as it always was.
func runWithOptionsArgs(o runOptions) []string {
	args := plainRunArgs()
	if o.Noop {
		args = append(args, "--noop")
	}
	if o.Environment != "" {
		// One argument, never through a shell.
		args = append(args, "--environment", o.Environment)
	}
	if o.Debug {
		args = append(args, "--debug")
	} else {
		args = append(args, "--verbose")
	}
	// The output is read on a web page, where Puppet's default ANSI
	// colour codes are noise (checked against a real agent: they are
	// written even when stdout is not a terminal).
	return append(args, "--color=false")
}

// decodeRunOptions reads a run-with-options request's parameters. An
// option this client does not know fails the request: running without
// it would be doing something other than what was asked.
func decodeRunOptions(params json.RawMessage) (runOptions, error) {
	var o runOptions
	if len(params) == 0 || string(params) == "null" {
		return o, nil
	}
	dec := json.NewDecoder(bytes.NewReader(params))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&o); err != nil {
		return runOptions{}, fmt.Errorf("invalid run options: %v", err)
	}
	if o.Environment != "" && !environmentName.MatchString(o.Environment) {
		return runOptions{}, fmt.Errorf("invalid environment name %q: only letters, digits and underscores are allowed", o.Environment)
	}
	return o, nil
}

// handleRunWithOptions runs Puppet with the requested options and
// returns its output. A request it cannot honour exactly is refused
// before Puppet is started.
func handleRunWithOptions(ctx context.Context, runner CommandRunner, puppetBin string, params json.RawMessage) []byte {
	options, err := decodeRunOptions(params)
	if err != nil {
		return errorResponse(err.Error() + "; Puppet was not run")
	}
	stdout, stderr, exitCode, err := runner(ctx, puppetBin, runWithOptionsArgs(options)...)
	return runResponse(truncateOutput(stdout, maxRunOutput), truncateOutput(stderr, maxTaskStderr), exitCode, err)
}

// truncateOutput keeps the first limit bytes of s, saying so at the end
// when that is not all - the same notice a task's output carries.
func truncateOutput(s string, limit int) string {
	if len(s) <= limit {
		return s
	}
	return s[:limit] + fmt.Sprintf("\n[output truncated: %d of %d bytes shown]", limit, len(s))
}
