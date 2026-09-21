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
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// Config configures the Deployer - see internal/runtime.Config for where
// these values come from. All fields are required for deploys to work,
// but codemanager itself doesn't enforce that - the console logs and
// leaves deploy endpoints erroring per-request if misconfigured, the
// same "never fatal at startup" posture already used for OIDC.
type Config struct {
	G10KBinPath string  // path to a real g10k binary (see `make g10k-install`)
	Sources     Sources // control repos to deploy from, see add-multi-source-code-deploy
	CodeDirPath string  // e.g. "openvox-code" - contains environments/ (live) and .deploys/ (staging)
}

// Deployer runs g10k as a subprocess against a configured control repo
// source and stages its output for atomic activation - see Activate in
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
	return d != nil && d.cfg.G10KBinPath != "" && len(d.cfg.Sources) > 0
}

// Sources returns the configured control repo sources.
func (d *Deployer) Sources() Sources {
	if d == nil {
		return nil
	}
	return d.cfg.Sources
}

// Deployment describes one staged deploy: which source and branch it
// came from, the Puppet environment name it produces, and where its
// content is waiting to be activated.
type Deployment struct {
	Source      Source
	Branch      string
	Environment string
	StagingDir  string

	// SizeBytes is the deployed tree's content size, measured here
	// rather than when it is displayed so that reporting it never
	// walks a module tree. Nil when the walk failed - the deploy
	// itself still succeeded, and an unmeasured size must stay
	// distinguishable from a real zero.
	SizeBytes *int64
}

// cacheDir is the g10k cachedir every deploy attempt shares - see
// design.md: it must live on the same filesystem device as each
// attempt's staging basedir, since g10k hardlinks Forge module files
// from cache into the target and hardlinking fails across devices.
// Keeping it under the same CodeDirPath tree as the staging directories
// guarantees that regardless of where the OS's default cache/tmp lives.
// Shared across sources as well as attempts: two sources depending on
// the same Forge module at the same version should resolve it once.
func (d *Deployer) cacheDir() string {
	return filepath.Join(d.cfg.CodeDirPath, ".deploys", ".cache")
}

// Run deploys branch from the named source into a fresh versioned
// staging directory. It does not make the deploy live - see Activate.
//
// The generated g10k config contains only the source being deployed,
// never the full set. g10k's -branch flag matches a raw branch name
// across every source in its config, so a config listing them all would
// deploy every source's `production` in one run; its per-source
// -environment flag is not a fix, because that compares against a
// literal source_branch while the directory it writes is prefix+branch,
// which disagree for an unprefixed or custom-prefixed source. One
// source per invocation makes a deploy's blast radius exactly one
// environment structurally, rather than by careful flag choice.
func (d *Deployer) Run(ctx context.Context, sourceName, branch string) (Deployment, error) {
	source, ok := d.cfg.Sources.Find(sourceName)
	if !ok {
		return Deployment{}, fmt.Errorf("unknown code source %q (configured: %s)",
			sourceName, strings.Join(d.cfg.Sources.Names(), ", "))
	}

	environment := source.EnvironmentFor(branch)

	attemptDir := filepath.Join(d.cfg.CodeDirPath, ".deploys", environment,
		time.Now().UTC().Format("20060102T150405.000000000"))
	basedir := filepath.Join(attemptDir, "basedir")
	cachedir := d.cacheDir()

	if err := os.MkdirAll(basedir, 0o755); err != nil {
		return Deployment{}, fmt.Errorf("create staging basedir: %w", err)
	}
	if err := os.MkdirAll(cachedir, 0o755); err != nil {
		return Deployment{}, fmt.Errorf("create cachedir: %w", err)
	}

	configPath := filepath.Join(attemptDir, "g10k.yaml")
	if err := os.WriteFile(configPath, []byte(g10kConfig(source, cachedir, basedir)), 0o600); err != nil {
		return Deployment{}, fmt.Errorf("write g10k config: %w", err)
	}

	cmd := exec.CommandContext(ctx, d.cfg.G10KBinPath, "-config", configPath, "-branch", branch)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return Deployment{}, fmt.Errorf("g10k deploy failed: %w: %s", err, string(output))
	}

	// g10k writes prefix+branch, which is exactly how environment was
	// derived above - see Source.EffectivePrefix, which mirrors g10k's
	// own resolution so these two never disagree.
	deployedDir := filepath.Join(basedir, environment)
	if _, statErr := os.Stat(deployedDir); statErr != nil {
		return Deployment{}, fmt.Errorf("g10k reported success but deployed directory is missing: %w", statErr)
	}

	deployment := Deployment{
		Source:      source,
		Branch:      branch,
		Environment: environment,
		StagingDir:  deployedDir,
	}

	// A failed measurement is not a failed deploy: the code is staged
	// and correct, and refusing to activate it because a stat call
	// failed would turn a reporting nicety into an outage.
	if size, err := treeSize(deployedDir); err == nil {
		deployment.SizeBytes = &size
	}

	return deployment, nil
}

// g10kConfig renders the per-attempt g10k configuration for a single
// source. prefix is emitted quoted because g10k's own config struct
// types it as a string: an unquoted YAML `true` fails to decode there.
func g10kConfig(source Source, cachedir, basedir string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "---\ncachedir: %q\nsources:\n  %s:\n    remote: %q\n    basedir: %q\n",
		cachedir, source.Name, source.Remote, basedir)
	if source.Prefix != "" {
		fmt.Fprintf(&b, "    prefix: %q\n", string(source.Prefix))
	}
	if source.PrivateKey != "" {
		fmt.Fprintf(&b, "    private_key: %q\n", source.PrivateKey)
	}
	return b.String()
}

// treeSize returns the total size of the regular files under root.
//
// Content size, not disk usage: g10k hardlinks Forge module files from
// the shared cachedir into each environment, so two environments using
// the same module version share those bytes on disk. This sums file
// sizes without deduplicating by inode, which overstates real
// consumption where environments overlap - deliberately, because the
// question being answered is "how big is this repository's deployed
// code". An inode-deduplicated figure would make one repository's size
// change depending on which other repositories happen to be deployed
// beside it, a number that moves for reasons unrelated to the
// repository it describes. See design.md in add-code-repository-overview.
//
// Symlinks are counted as entries but never followed: an activated
// environment is itself a symlink into staging, and following one would
// walk the same tree twice or loop.
//
// Walk errors are reported rather than swallowed, but a partial sum is
// returned alongside them, so a caller that prefers an approximate size
// to none can use it.
func treeSize(root string) (int64, error) {
	var total int64
	err := filepath.WalkDir(root, func(_ string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.Type().IsRegular() {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		total += info.Size()
		return nil
	})
	return total, err
}
