package nodeagent

import (
	"context"
	"strings"
	"testing"
)

const plainRun = "puppet agent --onetime --no-daemonize --detailed-exitcodes"

func runWithOptions(t *testing.T, params any, stdout string) (argv []string, resp wireResponse) {
	t.Helper()
	handler := NewHandler(recordingRunner(&argv, stdout, 0), "puppet", t.TempDir(), noFileExists)
	resp = decodeWireResponse(t, handler(context.Background(), requestPayload(t, "puppet", actionRunWithOptions, params)))
	return argv, resp
}

func TestRunWithOptions_Argv(t *testing.T) {
	cases := []struct {
		params map[string]any
		want   string
	}{
		{map[string]any{"noop": true}, plainRun + " --noop --verbose --color=false"},
		{map[string]any{"environment": "feature_x"}, plainRun + " --environment feature_x --verbose --color=false"},
		{map[string]any{"debug": true}, plainRun + " --debug --color=false"},
		{map[string]any{"noop": true, "environment": "Prod_2", "debug": true}, plainRun + " --noop --environment Prod_2 --debug --color=false"},
		{map[string]any{"noop": false, "debug": false, "environment": ""}, plainRun + " --verbose --color=false"},
		{nil, plainRun + " --verbose --color=false"},
	}
	for _, tc := range cases {
		argv, resp := runWithOptions(t, tc.params, "Notice: applied")
		if got := strings.Join(argv, " "); got != tc.want {
			t.Errorf("%v: argv = %q, want %q", tc.params, got, tc.want)
		}
		if resp.Error != "" || resp.Result == nil || resp.Result.Output.Stdout != "Notice: applied" {
			t.Errorf("%v: response = %+v", tc.params, resp)
		}
	}
}

// The environment is one argument, whatever it contains - and anything
// that is not an environment name never gets that far.
func TestRunWithOptions_RefusedWithoutRunningPuppet(t *testing.T) {
	cases := []map[string]any{
		{"environment": "feature-x"},
		{"environment": "x --noop"},
		{"environment": "--no-noop"},
		{"environment": "x;reboot"},
		{"environment": "../production"},
		{"trace": true}, // an option this client does not know
		{"noop": true, "evaltrace": true},
		{"noop": "yes"},
	}
	for _, params := range cases {
		argv, resp := runWithOptions(t, params, "")
		if argv != nil {
			t.Errorf("%v: Puppet was run: %v", params, argv)
		}
		if resp.Error == "" || resp.Result != nil || !strings.Contains(resp.Error, "Puppet was not run") {
			t.Errorf("%v: response = %+v", params, resp)
		}
	}
	// The unknown option is named, so the refusal can be acted on.
	if _, resp := runWithOptions(t, map[string]any{"trace": true}, ""); !strings.Contains(resp.Error, "trace") {
		t.Errorf("error does not name the option: %q", resp.Error)
	}
}

func TestRunWithOptions_TruncatesLongOutput(t *testing.T) {
	long := strings.Repeat("Debug: line\n", maxRunOutput/8)
	_, resp := runWithOptions(t, map[string]any{"debug": true}, long)
	out := resp.Result.Output.Stdout
	if !strings.HasPrefix(out, long[:maxRunOutput]) || !strings.Contains(out[maxRunOutput:], "[output truncated: ") {
		t.Errorf("output of %d bytes was not truncated with a notice (got %d bytes)", len(long), len(out))
	}
	// Short output is returned as it is.
	if _, resp := runWithOptions(t, map[string]any{"debug": true}, "short"); resp.Result.Output.Stdout != "short" {
		t.Errorf("short output = %q", resp.Result.Output.Stdout)
	}
}

// A plain run is what it always was: same arguments, parameters ignored.
func TestPlainRun_Unchanged(t *testing.T) {
	for _, params := range []any{nil, map[string]any{"noop": true}} {
		var argv []string
		handler := NewHandler(recordingRunner(&argv, "", 0), "puppet", t.TempDir(), noFileExists)
		handler(context.Background(), requestPayload(t, "puppet", actionRun, params))
		if got := strings.Join(argv, " "); got != plainRun {
			t.Errorf("plain run argv = %q", got)
		}
	}
}
