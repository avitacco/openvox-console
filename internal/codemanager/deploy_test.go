package codemanager

import (
	"context"
	"os"
	"os/exec"
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

// singleSource builds the Sources a pre-multi-source installation has:
// one unprefixed control repo, exactly what CONSOLE_CONTROL_REPO_URL
// resolves to.
func singleSource(remote string) Sources {
	return Sources{{Name: DefaultSourceName, Remote: remote}}
}

// newControlRepo creates a real bare git repo with a production branch
// carrying a marker file, for tests that need a second control repo.
// Real git rather than a fixture directory because g10k clones it.
// No Puppetfile: these tests are about environment placement, and
// resolving Forge modules would put the network in the loop.
func newControlRepo(t *testing.T, name, marker string) string {
	t.Helper()

	bare := filepath.Join(t.TempDir(), name+".git")
	run := func(dir string, args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=test", "GIT_AUTHOR_EMAIL=test@localhost",
			"GIT_COMMITTER_NAME=test", "GIT_COMMITTER_EMAIL=test@localhost",
		)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %s: %v: %s", strings.Join(args, " "), err, out)
		}
	}

	if err := os.MkdirAll(bare, 0o755); err != nil {
		t.Fatalf("mkdir bare repo: %v", err)
	}
	run(bare, "init", "--bare", "-q")

	work := filepath.Join(t.TempDir(), name)
	run(filepath.Dir(work), "clone", "-q", bare, work)
	if err := os.MkdirAll(filepath.Join(work, "manifests"), 0o755); err != nil {
		t.Fatalf("mkdir manifests: %v", err)
	}
	if err := os.WriteFile(filepath.Join(work, "manifests", "site.pp"), []byte(marker+"\n"), 0o644); err != nil {
		t.Fatalf("write site.pp: %v", err)
	}
	run(work, "add", "-A")
	run(work, "-c", "commit.gpgsign=false", "commit", "-q", "-m", "fixture")
	run(work, "branch", "-M", "production")
	run(work, "push", "-q", "origin", "production")

	return "file://" + bare
}

func TestDeployer_Run_RealDeployResolvesPuppetfile(t *testing.T) {
	g10kBin, controlRepoURL := realFixtures(t)
	tmpDir := t.TempDir()

	d := NewDeployer(Config{
		G10KBinPath: g10kBin,
		Sources:     singleSource(controlRepoURL),
		CodeDirPath: tmpDir,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	deployment, err := d.Run(ctx, DefaultSourceName, "production")
	if err != nil {
		t.Fatalf("Run() error: %v", err)
	}

	// An unprefixed source's environment is the bare branch name - the
	// compatibility guarantee for installations upgrading from the
	// single-control-repo configuration.
	if deployment.Environment != "production" {
		t.Errorf("Environment = %q, want production", deployment.Environment)
	}

	sitePP := filepath.Join(deployment.StagingDir, "manifests", "site.pp")
	content, err := os.ReadFile(sitePP)
	if err != nil {
		t.Fatalf("read deployed site.pp: %v", err)
	}
	if !strings.Contains(string(content), "code_manager_test_marker") {
		t.Errorf("deployed site.pp missing expected marker class, got: %s", content)
	}

	moduleDir := filepath.Join(deployment.StagingDir, "modules", "stdlib")
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
		G10KBinPath: g10kBin,
		Sources:     singleSource("file://" + filepath.Join(tmpDir, "nonexistent-repo")),
		CodeDirPath: tmpDir,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	_, err := d.Run(ctx, DefaultSourceName, "production")
	if err == nil {
		t.Fatal("Run() error = nil, want an error for an unresolvable control repo")
	}
}

func TestDeployer_Run_UnknownSourceDoesNotRunG10K(t *testing.T) {
	// The g10k path is deliberately nonsense: if Run tried to execute
	// it the error would be about the binary, not the source, so this
	// also proves nothing ran.
	d := NewDeployer(Config{
		G10KBinPath: "/nonexistent/g10k",
		Sources:     singleSource("file:///nowhere"),
		CodeDirPath: t.TempDir(),
	})

	_, err := d.Run(context.Background(), "team_a", "production")
	if err == nil {
		t.Fatal("Run() error = nil, want an error for an unconfigured source")
	}
	if !strings.Contains(err.Error(), "unknown code source") || !strings.Contains(err.Error(), "team_a") {
		t.Errorf("error = %q, want it to name the unknown source", err)
	}
	// The message lists what is configured, so an operator fixing a
	// typo does not have to go read the config file.
	if !strings.Contains(err.Error(), DefaultSourceName) {
		t.Errorf("error = %q, want it to list the configured sources", err)
	}
}

func TestDeployer_Run_PrefixedSourceDeploysToPrefixedEnvironment(t *testing.T) {
	g10kBin, _ := realFixtures(t)
	tmpDir := t.TempDir()

	remote := newControlRepo(t, "team-a", "class team_a_marker {}")
	d := NewDeployer(Config{
		G10KBinPath: g10kBin,
		Sources:     Sources{{Name: "team_a", Remote: remote, Prefix: prefixTrue}},
		CodeDirPath: tmpDir,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	deployment, err := d.Run(ctx, "team_a", "production")
	if err != nil {
		t.Fatalf("Run() error: %v", err)
	}

	// The whole point of the prefix: this source's production branch
	// does not land on the unprefixed "production" environment.
	if deployment.Environment != "team_a_production" {
		t.Fatalf("Environment = %q, want team_a_production", deployment.Environment)
	}
	if filepath.Base(deployment.StagingDir) != "team_a_production" {
		t.Errorf("staging dir = %q, want it to end in team_a_production", deployment.StagingDir)
	}
	if _, err := os.Stat(filepath.Join(deployment.StagingDir, "manifests", "site.pp")); err != nil {
		t.Errorf("deployed content missing: %v", err)
	}
}

func TestDeployer_Run_DeployingOneSourceLeavesOthersUntouched(t *testing.T) {
	g10kBin, _ := realFixtures(t)
	tmpDir := t.TempDir()

	controlRemote := newControlRepo(t, "control", "class control_marker {}")
	teamARemote := newControlRepo(t, "team-a", "class team_a_marker {}")

	d := NewDeployer(Config{
		G10KBinPath: g10kBin,
		Sources: Sources{
			{Name: DefaultSourceName, Remote: controlRemote},
			{Name: "team_a", Remote: teamARemote, Prefix: prefixTrue},
		},
		CodeDirPath: tmpDir,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	// Both sources' production branches live at once.
	control, err := d.Run(ctx, DefaultSourceName, "production")
	if err != nil {
		t.Fatalf("deploy control: %v", err)
	}
	if err := d.Activate(control.Environment, control.StagingDir); err != nil {
		t.Fatalf("activate control: %v", err)
	}

	teamA, err := d.Run(ctx, "team_a", "production")
	if err != nil {
		t.Fatalf("deploy team_a: %v", err)
	}
	if err := d.Activate(teamA.Environment, teamA.StagingDir); err != nil {
		t.Fatalf("activate team_a: %v", err)
	}

	liveSitePP := func(environment string) string {
		t.Helper()
		content, err := os.ReadFile(filepath.Join(tmpDir, "environments", environment, "manifests", "site.pp"))
		if err != nil {
			t.Fatalf("read live %s: %v", environment, err)
		}
		return string(content)
	}

	if got := liveSitePP("production"); !strings.Contains(got, "control_marker") {
		t.Errorf("environments/production = %q, want the control repo's content", got)
	}
	if got := liveSitePP("team_a_production"); !strings.Contains(got, "team_a_marker") {
		t.Errorf("environments/team_a_production = %q, want team_a's content", got)
	}

	// Redeploying one source must not disturb the other's live code -
	// the failure this change exists to prevent.
	before := liveSitePP("team_a_production")

	redeployed, err := d.Run(ctx, DefaultSourceName, "production")
	if err != nil {
		t.Fatalf("redeploy control: %v", err)
	}
	if err := d.Activate(redeployed.Environment, redeployed.StagingDir); err != nil {
		t.Fatalf("reactivate control: %v", err)
	}

	if after := liveSitePP("team_a_production"); after != before {
		t.Errorf("team_a's live code changed when control was redeployed:\nbefore: %q\nafter:  %q", before, after)
	}
	if got := liveSitePP("production"); !strings.Contains(got, "control_marker") {
		t.Errorf("environments/production = %q after redeploy", got)
	}
}

func TestG10KConfig_ContainsOnlyTheDeployedSource(t *testing.T) {
	// A config listing every source would let one `-branch production`
	// run deploy every repo at once; keeping it to one makes that
	// impossible rather than merely unlikely.
	source := Source{
		Name:       "team_a",
		Remote:     "git@example.com:org/team-a.git",
		Prefix:     prefixTrue,
		PrivateKey: "/etc/openvox-console/keys/team_a",
	}
	got := g10kConfig(source, "/code/.deploys/.cache", "/code/.deploys/team_a_production/x/basedir")

	if strings.Count(got, "remote:") != 1 {
		t.Errorf("config declares %d remotes, want exactly 1:\n%s", strings.Count(got, "remote:"), got)
	}
	for _, want := range []string{
		"  team_a:",
		`remote: "git@example.com:org/team-a.git"`,
		// Quoted because g10k types its own prefix field as a string:
		// an unquoted YAML true fails to decode there.
		`prefix: "true"`,
		`private_key: "/etc/openvox-console/keys/team_a"`,
		`cachedir: "/code/.deploys/.cache"`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("config missing %q:\n%s", want, got)
		}
	}
}

func TestG10KConfig_OmitsUnsetOptionalSettings(t *testing.T) {
	got := g10kConfig(Source{Name: "control", Remote: "file:///srv/control.git"}, "/cache", "/basedir")
	if strings.Contains(got, "prefix:") {
		t.Errorf("config emits a prefix for an unprefixed source:\n%s", got)
	}
	if strings.Contains(got, "private_key:") {
		t.Errorf("config emits a private_key when none is set:\n%s", got)
	}
}

func TestDeployer_Configured(t *testing.T) {
	if (&Deployer{}).Configured() {
		t.Error("a zero Deployer reports itself configured")
	}
	noSources := NewDeployer(Config{G10KBinPath: "/usr/local/bin/g10k", CodeDirPath: "code"})
	if noSources.Configured() {
		t.Error("a Deployer with no sources reports itself configured")
	}
	noBinary := NewDeployer(Config{Sources: singleSource("file:///srv/control.git"), CodeDirPath: "code"})
	if noBinary.Configured() {
		t.Error("a Deployer with no g10k binary reports itself configured")
	}
	full := NewDeployer(Config{
		G10KBinPath: "/usr/local/bin/g10k",
		Sources:     singleSource("file:///srv/control.git"),
		CodeDirPath: "code",
	})
	if !full.Configured() {
		t.Error("a fully configured Deployer reports itself unconfigured")
	}
}

func TestTreeSize(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "modules", "stdlib"), 0o755); err != nil {
		t.Fatalf("setup: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "site.pp"), []byte("0123456789"), 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "modules", "stdlib", "init.pp"), []byte("01234"), 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "empty"), nil, 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}
	// A symlink to a big file must not be followed - an activated
	// environment is itself a symlink into staging.
	big := filepath.Join(t.TempDir(), "big")
	if err := os.WriteFile(big, make([]byte, 4096), 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}
	if err := os.Symlink(big, filepath.Join(root, "link")); err != nil {
		t.Fatalf("setup: %v", err)
	}

	got, err := treeSize(root)
	if err != nil {
		t.Fatalf("treeSize() error: %v", err)
	}
	if got != 15 {
		t.Errorf("treeSize() = %d, want 15 (10 + 5, with the symlink not followed and the empty file contributing nothing)", got)
	}
}

func TestTreeSize_MissingRoot(t *testing.T) {
	if _, err := treeSize(filepath.Join(t.TempDir(), "nope")); err == nil {
		t.Error("treeSize() on a missing directory returned no error")
	}
}

func TestDeployer_Run_MeasuresDeployedSize(t *testing.T) {
	g10kBin, _ := realFixtures(t)
	tmpDir := t.TempDir()

	remote := newControlRepo(t, "sized", "class sized_marker {}")
	d := NewDeployer(Config{
		G10KBinPath: g10kBin,
		Sources:     Sources{{Name: "sized", Remote: remote}},
		CodeDirPath: tmpDir,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	deployment, err := d.Run(ctx, "sized", "production")
	if err != nil {
		t.Fatalf("Run() error: %v", err)
	}
	if deployment.SizeBytes == nil {
		t.Fatal("SizeBytes is nil after a successful deploy")
	}

	// Must match what is actually on disk, not an approximation.
	want, err := treeSize(deployment.StagingDir)
	if err != nil {
		t.Fatalf("treeSize() error: %v", err)
	}
	if *deployment.SizeBytes != want {
		t.Errorf("SizeBytes = %d, want %d", *deployment.SizeBytes, want)
	}
	if *deployment.SizeBytes == 0 {
		t.Error("SizeBytes = 0 for a deploy that produced real content")
	}
}
