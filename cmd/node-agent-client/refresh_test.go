package main

import (
	"bytes"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRefreshPackageInventoryFact_LogsFailureWithoutStopping(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root ignores file permissions")
	}
	factsDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(factsDir, "package_inventory.sh"), []byte("old script\n"), 0o555); err != nil {
		t.Fatalf("seed fact file: %v", err)
	}
	var logs bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logs, nil))

	// Returning at all is the "startup proceeds" half - the helper has no
	// error result for run() to bail out on.
	refreshPackageInventoryFact(logger, factsDir, func(p string) bool { return p == "/usr/bin/rpm" })

	if !strings.Contains(logs.String(), "could not refresh package-inventory fact") {
		t.Errorf("logs = %q, want a refresh failure warning", logs.String())
	}
}

func TestRefreshPackageInventoryFact_LogsRewrite(t *testing.T) {
	factsDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(factsDir, "package_inventory.sh"), []byte("old script\n"), 0o755); err != nil {
		t.Fatalf("seed fact file: %v", err)
	}
	var logs bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logs, nil))

	refreshPackageInventoryFact(logger, factsDir, func(p string) bool { return p == "/usr/bin/rpm" })

	if !strings.Contains(logs.String(), "refreshed package-inventory fact") {
		t.Errorf("logs = %q, want a rewrite message", logs.String())
	}
}
