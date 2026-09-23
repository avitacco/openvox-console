package runtime_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/voxpupuli/enterprise-console/internal/runtime"
)

func TestParseModeAcceptsEverySupportedMode(t *testing.T) {
	for _, want := range runtime.Modes() {
		got, err := runtime.ParseMode(string(want))
		if err != nil {
			t.Errorf("ParseMode(%q) error: %v", want, err)
			continue
		}
		if got != want {
			t.Errorf("ParseMode(%q) = %q, want %q", want, got, want)
		}
	}
}

// The empty value is what an unset CONSOLE_RUN_MODE looks like, and must
// select "all" - a deployment predating run modes has to keep working
// without being reconfigured.
func TestParseModeDefaultsToAll(t *testing.T) {
	got, err := runtime.ParseMode("")
	if err != nil {
		t.Fatalf("ParseMode(\"\") error: %v", err)
	}
	if got != runtime.ModeAll {
		t.Errorf("ParseMode(\"\") = %q, want %q", got, runtime.ModeAll)
	}
}

func TestParseModeRejectsUnrecognizedValue(t *testing.T) {
	// "wrker" rather than "nonsense": a plausible typo is the case that
	// matters, since defaulting it silently would start an instance
	// serving everything while its operator expected background work
	// only.
	_, err := runtime.ParseMode("wrker")
	if err == nil {
		t.Fatal("ParseMode(\"wrker\") returned no error, want one")
	}

	var invalid *runtime.InvalidModeError
	if !errors.As(err, &invalid) {
		t.Fatalf("ParseMode error is %T, want *runtime.InvalidModeError", err)
	}
	if invalid.Value != "wrker" {
		t.Errorf("InvalidModeError.Value = %q, want %q", invalid.Value, "wrker")
	}

	// The message has to name the offending value and every valid mode:
	// an operator reading it in a crash loop needs to fix the value
	// without going to the source.
	msg := err.Error()
	if !strings.Contains(msg, "wrker") {
		t.Errorf("error message %q does not name the invalid value", msg)
	}
	for _, m := range runtime.Modes() {
		if !strings.Contains(msg, string(m)) {
			t.Errorf("error message %q does not name valid mode %q", msg, m)
		}
	}
}

// Case matters: run modes are matched exactly, so a value that differs
// only in case is a typo, not an alias.
func TestParseModeIsCaseSensitive(t *testing.T) {
	if _, err := runtime.ParseMode("ALL"); err == nil {
		t.Error("ParseMode(\"ALL\") returned no error, want one")
	}
}

func TestModesReturnsACopy(t *testing.T) {
	first := runtime.Modes()
	first[0] = "mutated"
	if runtime.Modes()[0] != runtime.ModeAll {
		t.Error("Modes() returned a slice aliasing package state; a caller mutated it")
	}
}
