// Package nodeagent implements the console's own on-node orchestration
// client, connecting to the console's node transport
// (internal/nodetransport) over NATS (see openspec/specs/node-agent).
// This file holds the run/task execution logic; the transport and
// reconnection behavior underneath it live in client.go.
package nodeagent

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

// Module/action identifiers this client understands - mirrors
// internal/orchestrator's own copy (see design.md - this project
// controls both ends, so the two packages don't share the Go type, only
// the wire contract).
const (
	actionRun     = "run"
	actionRunTask = "run_task"
	// actionPackageInventoryStatus and actionPackageInventorySet are new
	// as of add-package-inventory-toggle.
	actionPackageInventoryStatus = "package_inventory_status"
	actionPackageInventorySet    = "package_inventory_set"
)

// packageInventoryFactFilename is the external fact's filename within
// the facts.d directory NewHandler is given - fixed, not configurable,
// since it has to match what openvoxdb's facts terminus and Facter
// itself both expect (see design.md in add-package-inventory-reporting).
const packageInventoryFactFilename = "package_inventory.sh"

// CommandRunner runs an external command and captures its result.
// Injected so tests don't need a real Puppet installation - see
// RunCommand for the real implementation.
type CommandRunner func(ctx context.Context, name string, args ...string) (stdout, stderr string, exitCode int, err error)

// RunCommand is the production CommandRunner: runs name via os/exec. A
// nonzero exit code is a normal outcome (e.g. Puppet's
// --detailed-exitcodes convention), not a Go error - err is only
// non-nil when the command could not be run at all (binary not found,
// killed, etc.).
func RunCommand(ctx context.Context, name string, args ...string) (stdout, stderr string, exitCode int, err error) {
	cmd := exec.CommandContext(ctx, name, args...)
	var stdoutBuf, stderrBuf bytes.Buffer
	cmd.Stdout = &stdoutBuf
	cmd.Stderr = &stderrBuf

	runErr := cmd.Run()
	var exitErr *exec.ExitError
	if runErr != nil && errors.As(runErr, &exitErr) {
		return stdoutBuf.String(), stderrBuf.String(), exitErr.ExitCode(), nil
	}
	if runErr != nil {
		return stdoutBuf.String(), stderrBuf.String(), 0, runErr
	}
	return stdoutBuf.String(), stderrBuf.String(), 0, nil
}

// requestData is a dispatch request's payload - mirrors
// internal/orchestrator's own copy.
type requestData struct {
	Module string          `json:"module"`
	Action string          `json:"action"`
	Params json.RawMessage `json:"params,omitempty"`
}

// taskParams is requestData.Params' shape for an actionRunTask request.
type taskParams struct {
	Task   string          `json:"task"`
	Params json.RawMessage `json:"params,omitempty"`
}

// packageInventorySetParams is requestData.Params' shape for an
// actionPackageInventorySet request.
type packageInventorySetParams struct {
	Enabled bool `json:"enabled"`
}

// wireResponse is what this client replies with - Result on success,
// Error on an execution failure or a busy rejection, never both. Mirrors
// internal/orchestrator's own copy.
type wireResponse struct {
	Result *responseData `json:"result,omitempty"`
	Error  string        `json:"error,omitempty"`
}

// responseData mirrors internal/orchestrator's own copy - see there for
// why PackageInventory is a separate optional field rather than
// overloading Output.
type responseData struct {
	Output           outputData                `json:"output"`
	PackageInventory *packageInventoryResponse `json:"package_inventory,omitempty"`
}

// packageInventoryResponse mirrors internal/orchestrator's own copy -
// Ran distinguishes "no Puppet run happened" from "a run happened and
// produced empty output with exit 0" (a real, common outcome), which
// Output's zero value alone can't.
type packageInventoryResponse struct {
	Enabled bool `json:"enabled"`
	Ran     bool `json:"ran"`
}

type outputData struct {
	Stdout   string `json:"stdout"`
	Stderr   string `json:"stderr"`
	ExitCode int    `json:"exitcode"`
}

// Handler executes one dispatch request's raw payload and returns the
// (already JSON-encoded) wireResponse payload to reply with. Client
// guarantees at most one call to Handler is in flight at a time (see
// client.go's dispatch).
type Handler func(ctx context.Context, payload []byte) []byte

// NewHandler builds the Handler that executes run, task, and
// package-inventory requests via runner, invoking puppetBin (see
// Config.PuppetBinPath). factsDir is Facter's external-facts directory
// (the real AIO path in production, an injected temp directory in
// tests - see design.md in add-package-inventory-toggle) and fileExists
// is used to detect the node's package-manager family by checking for
// well-known binary paths (not a shell-out, and not a $PATH lookup,
// since a service's environment may not have one) - injected so tests
// can exercise both families and the neither-present case without
// depending on what's actually installed on the machine running them.
func NewHandler(runner CommandRunner, puppetBin, factsDir string, fileExists func(string) bool) Handler {
	return func(ctx context.Context, payload []byte) []byte {
		var data requestData
		if err := json.Unmarshal(payload, &data); err != nil {
			return errorResponse(fmt.Sprintf("invalid request data: %v", err))
		}

		switch data.Action {
		case actionRun:
			return runResponse(runner(ctx, puppetBin, "agent", "--onetime", "--no-daemonize", "--detailed-exitcodes"))
		case actionRunTask:
			return handleRunTask(ctx, runner, puppetBin, data.Params)
		case actionPackageInventoryStatus:
			return handlePackageInventoryStatus(factsDir)
		case actionPackageInventorySet:
			return handlePackageInventorySet(ctx, runner, puppetBin, factsDir, fileExists, data.Params)
		default:
			return errorResponse(fmt.Sprintf("unsupported action %q", data.Action))
		}
	}
}

func handleRunTask(ctx context.Context, runner CommandRunner, puppetBin string, params json.RawMessage) []byte {
	var tp taskParams
	if err := json.Unmarshal(params, &tp); err != nil {
		return errorResponse(fmt.Sprintf("invalid task params: %v", err))
	}
	if tp.Task == "" {
		return errorResponse("task request missing required \"task\" field")
	}
	args := []string{"task", "run", tp.Task}
	if len(tp.Params) > 0 {
		args = append(args, "--params", string(tp.Params))
	}
	return runResponse(runner(ctx, puppetBin, args...))
}

func runResponse(stdout, stderr string, exitCode int, err error) []byte {
	if err != nil {
		return errorResponse(fmt.Sprintf("failed to execute: %v", err))
	}
	return encodeResult(&responseData{Output: outputData{Stdout: stdout, Stderr: stderr, ExitCode: exitCode}})
}

func packageInventoryFactPath(factsDir string) string {
	return filepath.Join(factsDir, packageInventoryFactFilename)
}

func packageInventoryEnabled(factsDir string) (bool, error) {
	_, err := os.Stat(packageInventoryFactPath(factsDir))
	if err == nil {
		return true, nil
	}
	if os.IsNotExist(err) {
		return false, nil
	}
	return false, err
}

func handlePackageInventoryStatus(factsDir string) []byte {
	enabled, err := packageInventoryEnabled(factsDir)
	if err != nil {
		return errorResponse(fmt.Sprintf("failed to check package-inventory fact: %v", err))
	}
	return encodeResult(&responseData{PackageInventory: &packageInventoryResponse{Enabled: enabled}})
}

func handlePackageInventorySet(ctx context.Context, runner CommandRunner, puppetBin, factsDir string, fileExists func(string) bool, params json.RawMessage) []byte {
	var sp packageInventorySetParams
	if err := json.Unmarshal(params, &sp); err != nil {
		return errorResponse(fmt.Sprintf("invalid package-inventory set params: %v", err))
	}

	currentlyEnabled, err := packageInventoryEnabled(factsDir)
	if err != nil {
		return errorResponse(fmt.Sprintf("failed to check package-inventory fact: %v", err))
	}

	// Already in the requested state: report it, but write nothing and
	// trigger no Puppet run (see design.md - avoids a no-op run
	// cluttering the node's run history every time the console re-syncs
	// UI state).
	if sp.Enabled == currentlyEnabled {
		return encodeResult(&responseData{PackageInventory: &packageInventoryResponse{Enabled: currentlyEnabled}})
	}

	factPath := packageInventoryFactPath(factsDir)
	if sp.Enabled {
		family := detectPackageManagerFamily(fileExists)
		script, ok := packageInventoryFactScript(family)
		if !ok {
			return errorResponse("cannot enable package-inventory reporting: neither dpkg-query nor rpm found on this node")
		}
		if err := os.MkdirAll(factsDir, 0o755); err != nil {
			return errorResponse(fmt.Sprintf("failed to create facts.d directory: %v", err))
		}
		if err := os.WriteFile(factPath, script, 0o755); err != nil { //nolint:gosec // the fact script must be executable
			return errorResponse(fmt.Sprintf("failed to write package-inventory fact: %v", err))
		}
	} else {
		if err := os.Remove(factPath); err != nil && !os.IsNotExist(err) {
			return errorResponse(fmt.Sprintf("failed to remove package-inventory fact: %v", err))
		}
	}

	stdout, stderr, exitCode, err := runner(ctx, puppetBin, "agent", "--onetime", "--no-daemonize", "--detailed-exitcodes")
	if err != nil {
		return errorResponse(fmt.Sprintf("failed to execute: %v", err))
	}
	return encodeResult(&responseData{
		Output:           outputData{Stdout: stdout, Stderr: stderr, ExitCode: exitCode},
		PackageInventory: &packageInventoryResponse{Enabled: sp.Enabled, Ran: true},
	})
}

func encodeResult(result *responseData) []byte {
	data, err := json.Marshal(wireResponse{Result: result})
	if err != nil {
		return errorResponse(fmt.Sprintf("failed to encode response: %v", err))
	}
	return data
}

func errorResponse(reason string) []byte {
	data, _ := json.Marshal(wireResponse{Error: reason})
	return data
}
