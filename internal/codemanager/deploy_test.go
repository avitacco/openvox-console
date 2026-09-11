package codemanager

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// realFixtures locates the real g10k binary and control repo fixture
// this package's tests deploy against - see `make g10k-install` and
// `make control-repo-fixture`. Skips if either isn't set up, mirroring
// this project's CONSOLE_TEST_POSTGRES_DSN-style opt-in integration test
// pattern - these are real dependencies, not mocked.
func realFixtures(t *testing.T) (g10kBin, controlRepoURL string) {
	t.Helper()

	root, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd() error: %v", err)
	}
	// internal/codemanager -> repo root
	root = filepath.Join(root, "..", "..")

	g10kBin = filepath.Join(root, "bin", "g10k")
	if _, err := os.Stat(g10kBin); err != nil {
		t.Skip("bin/g10k not found; run `make g10k-install` to enable this integration test")
	}

	repoPath := filepath.Join(root, ".dev", "control-repo.git")
	if _, err := os.Stat(repoPath); err != nil {
		t.Skip(".dev/control-repo.git not found; run `make control-repo-fixture` to enable this integration test")
	}

	return g10kBin, "file://" + repoPath
}

func TestDeployer_Run_RealDeployResolvesPuppetfile(t *testing.T) {
	g10kBin, controlRepoURL := realFixtures(t)
	tmpDir := t.TempDir()

	d := NewDeployer(Config{
		G10KBinPath:    g10kBin,
		ControlRepoURL: controlRepoURL,
		CodeDirPath:    tmpDir,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	stagingDir, err := d.Run(ctx, "production")
	if err != nil {
		t.Fatalf("Run() error: %v", err)
	}

	sitePP := filepath.Join(stagingDir, "manifests", "site.pp")
	content, err := os.ReadFile(sitePP)
	if err != nil {
		t.Fatalf("read deployed site.pp: %v", err)
	}
	if !strings.Contains(string(content), "code_manager_test_marker") {
		t.Errorf("deployed site.pp missing expected marker class, got: %s", content)
	}

	moduleDir := filepath.Join(stagingDir, "modules", "stdlib")
	if _, err := os.Stat(moduleDir); err != nil {
		t.Errorf("expected stdlib module to be resolved into %s: %v", moduleDir, err)
	}
}

func TestDeployer_Run_BrokenPuppetfileFailsCleanly(t *testing.T) {
	g10kBin, _ := realFixtures(t)
	tmpDir := t.TempDir()

	// A control repo URL that resolves to nothing real - the deploy must
	// fail with a real error, not hang or silently succeed, and must not
	// leave a directory that looks like a successful deploy.
	d := NewDeployer(Config{
		G10KBinPath:    g10kBin,
		ControlRepoURL: "file://" + filepath.Join(tmpDir, "nonexistent-repo"),
		CodeDirPath:    tmpDir,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	_, err := d.Run(ctx, "production")
	if err == nil {
		t.Fatal("Run() error = nil, want an error for an unresolvable control repo")
	}
}
