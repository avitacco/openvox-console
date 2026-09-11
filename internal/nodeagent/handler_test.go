package nodeagent

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
)

// noFileExists is a fileExists stub for tests that never exercise the
// package-inventory set action's package-manager detection.
func noFileExists(string) bool { return false }

func fakeRunner(stdout, stderr string, exitCode int, err error) CommandRunner {
	return func(ctx context.Context, name string, args ...string) (string, string, int, error) {
		return stdout, stderr, exitCode, err
	}
}

// recordingRunner returns a CommandRunner that records the args it was
// called with, for assertions on what was actually invoked.
func recordingRunner(dst *[]string, stdout string, exitCode int) CommandRunner {
	return func(ctx context.Context, name string, args ...string) (string, string, int, error) {
		*dst = append([]string{name}, args...)
		return stdout, "", exitCode, nil
	}
}

func requestPayload(t *testing.T, module, action string, params any) []byte {
	t.Helper()
	var paramsRaw json.RawMessage
	if params != nil {
		raw, err := json.Marshal(params)
		if err != nil {
			t.Fatalf("marshal params: %v", err)
		}
		paramsRaw = raw
	}
	data, err := json.Marshal(requestData{Module: module, Action: action, Params: paramsRaw})
	if err != nil {
		t.Fatalf("marshal request data: %v", err)
	}
	return data
}

func decodeWireResponse(t *testing.T, resp []byte) wireResponse {
	t.Helper()
	var wr wireResponse
	if err := json.Unmarshal(resp, &wr); err != nil {
		t.Fatalf("decode wire response: %v (raw: %s)", err, resp)
	}
	return wr
}

func TestHandler_RunSuccess(t *testing.T) {
	handler := NewHandler(fakeRunner("changes applied", "", 2, nil), "puppet", t.TempDir(), noFileExists)
	req := requestPayload(t, "puppet", actionRun, nil)

	wr := decodeWireResponse(t, handler(context.Background(), req))

	if wr.Error != "" {
		t.Fatalf("Error = %q, want empty (success)", wr.Error)
	}
	if wr.Result == nil {
		t.Fatal("Result is nil, want a result")
	}
	if wr.Result.Output.Stdout != "changes applied" {
		t.Errorf("Stdout = %q", wr.Result.Output.Stdout)
	}
	if wr.Result.Output.ExitCode != 2 {
		t.Errorf("ExitCode = %d, want 2", wr.Result.Output.ExitCode)
	}
}

func TestHandler_RunInvokesPuppetAgentOnetime(t *testing.T) {
	var invoked []string
	handler := NewHandler(recordingRunner(&invoked, "", 0), "/opt/puppetlabs/bin/puppet", t.TempDir(), noFileExists)
	req := requestPayload(t, "puppet", actionRun, nil)

	handler(context.Background(), req)

	want := []string{"/opt/puppetlabs/bin/puppet", "agent", "--onetime", "--no-daemonize", "--detailed-exitcodes"}
	if len(invoked) != len(want) {
		t.Fatalf("invoked = %v, want %v", invoked, want)
	}
	for i := range want {
		if invoked[i] != want[i] {
			t.Errorf("invoked[%d] = %q, want %q", i, invoked[i], want[i])
		}
	}
}

func TestHandler_RunFailureToExecuteReturnsError(t *testing.T) {
	handler := NewHandler(fakeRunner("", "", 0, errors.New("exec: puppet not found")), "puppet", t.TempDir(), noFileExists)
	req := requestPayload(t, "puppet", actionRun, nil)

	wr := decodeWireResponse(t, handler(context.Background(), req))
	if wr.Error == "" {
		t.Fatal("Error is empty, want a failure reason")
	}
	if wr.Result != nil {
		t.Error("Result is non-nil, want nil on error")
	}
}

func TestHandler_RunTaskPassesParameters(t *testing.T) {
	var invoked []string
	handler := NewHandler(recordingRunner(&invoked, "task ran", 0), "puppet", t.TempDir(), noFileExists)
	req := requestPayload(t, "puppet", actionRunTask, taskParams{
		Task:   "package",
		Params: json.RawMessage(`{"name":"nginx","action":"install"}`),
	})

	resp := handler(context.Background(), req)

	want := []string{"puppet", "task", "run", "package", "--params", `{"name":"nginx","action":"install"}`}
	if len(invoked) != len(want) {
		t.Fatalf("invoked = %v, want %v", invoked, want)
	}
	for i := range want {
		if invoked[i] != want[i] {
			t.Errorf("invoked[%d] = %q, want %q", i, invoked[i], want[i])
		}
	}

	wr := decodeWireResponse(t, resp)
	if wr.Result == nil {
		t.Fatal("Result is nil, want a result")
	}
	if wr.Result.Output.Stdout != "task ran" {
		t.Errorf("Stdout = %q", wr.Result.Output.Stdout)
	}
}

func TestHandler_RunTaskMissingTaskNameReturnsError(t *testing.T) {
	handler := NewHandler(fakeRunner("", "", 0, nil), "puppet", t.TempDir(), noFileExists)
	req := requestPayload(t, "puppet", actionRunTask, taskParams{})

	wr := decodeWireResponse(t, handler(context.Background(), req))
	if wr.Error == "" {
		t.Fatal("Error is empty, want a failure reason")
	}
}

func TestHandler_UnsupportedActionReturnsError(t *testing.T) {
	handler := NewHandler(fakeRunner("", "", 0, nil), "puppet", t.TempDir(), noFileExists)
	req := requestPayload(t, "puppet", "some_unknown_action", nil)

	wr := decodeWireResponse(t, handler(context.Background(), req))
	if wr.Error == "" {
		t.Fatal("Error is empty, want a failure reason")
	}
}

func TestRunCommand_CapturesExitCodeNotError(t *testing.T) {
	// `false` always exits 1 and is present on every Linux system this
	// project targets (see design.md's GOOS=linux decision).
	_, _, exitCode, err := RunCommand(context.Background(), "false")
	if err != nil {
		t.Fatalf("RunCommand(false) error = %v, want nil (nonzero exit is not a Go error)", err)
	}
	if exitCode != 1 {
		t.Errorf("exitCode = %d, want 1", exitCode)
	}
}

func TestRunCommand_MissingBinaryIsError(t *testing.T) {
	_, _, _, err := RunCommand(context.Background(), "this-binary-does-not-exist-anywhere")
	if err == nil {
		t.Fatal("RunCommand() with a missing binary: expected an error, got nil")
	}
}
