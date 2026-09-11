// Package codemanager deploys Puppet code (a Puppetfile plus manifests)
// from a control repo into the live environment directory openvoxserver
// reads from, by shelling out to a real g10k binary - see design.md in
// the phase-5-code-manager change for why g10k is invoked as a
// subprocess rather than vendored in-process (it's package main with no
// importable API, a language-level constraint, not a missed refactor).
package codemanager

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

// Config configures the Deployer - see internal/runtime.Config for where
// these values come from. All fields are required for deploys to work,
// but codemanager itself doesn't enforce that - the console logs and
// leaves deploy endpoints erroring per-request if misconfigured, the
// same "never fatal at startup" posture already used for OIDC.
type Config struct {
	G10KBinPath    string // path to a real g10k binary (see `make g10k-install`)
	ControlRepoURL string // git remote g10k resolves against
	CodeDirPath    string // e.g. "openvox-code" - contains environments/ (live) and .deploys/ (staging)
}

// Deployer runs g10k as a subprocess against the configured control repo
// and stages its output for atomic activation - see Activate in
// activation.go. Deployer never activates a deploy itself; Run only
// stages it, so a caller can record status/publish events around
// exactly the moment activation happens.
type Deployer struct {
	cfg Config
}

// NewDeployer builds a Deployer.
func NewDeployer(cfg Config) *Deployer {
	return &Deployer{cfg: cfg}
}

// Configured reports whether enough config is present to actually run a
// deploy - mirrors rbac.OIDCService.Configured's "never fatal at
// startup, just inactive" posture for optional-until-configured features.
func (d *Deployer) Configured() bool {
	return d != nil && d.cfg.G10KBinPath != "" && d.cfg.ControlRepoURL != ""
}

// cacheDir is the g10k cachedir every deploy attempt shares - see
// design.md: it must live on the same filesystem device as each
// attempt's staging basedir, since g10k hardlinks Forge module files
// from cache into the target and hardlinking fails across devices.
// Keeping it under the same CodeDirPath tree as the staging directories
// guarantees that regardless of where the OS's default cache/tmp lives.
func (d *Deployer) cacheDir() string {
	return filepath.Join(d.cfg.CodeDirPath, ".deploys", ".cache")
}

// Run deploys environment (a control repo branch name) into a fresh
// versioned staging directory and returns its path on success. It does
// not make the deploy live - see Activate.
func (d *Deployer) Run(ctx context.Context, environment string) (stagingDir string, err error) {
	attemptDir := filepath.Join(d.cfg.CodeDirPath, ".deploys", environment,
		time.Now().UTC().Format("20060102T150405.000000000"))
	basedir := filepath.Join(attemptDir, "basedir")
	cachedir := d.cacheDir()

	if err := os.MkdirAll(basedir, 0o755); err != nil {
		return "", fmt.Errorf("create staging basedir: %w", err)
	}
	if err := os.MkdirAll(cachedir, 0o755); err != nil {
		return "", fmt.Errorf("create cachedir: %w", err)
	}

	configPath := filepath.Join(attemptDir, "g10k.yaml")
	configContents := fmt.Sprintf(
		"---\ncachedir: %q\nsources:\n  control:\n    remote: %q\n    basedir: %q\n",
		cachedir, d.cfg.ControlRepoURL, basedir,
	)
	if err := os.WriteFile(configPath, []byte(configContents), 0o600); err != nil {
		return "", fmt.Errorf("write g10k config: %w", err)
	}

	cmd := exec.CommandContext(ctx, d.cfg.G10KBinPath, "-config", configPath, "-branch", environment)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("g10k deploy failed: %w: %s", err, string(output))
	}

	deployedDir := filepath.Join(basedir, environment)
	if _, statErr := os.Stat(deployedDir); statErr != nil {
		return "", fmt.Errorf("g10k reported success but deployed directory is missing: %w", statErr)
	}
	return deployedDir, nil
}
