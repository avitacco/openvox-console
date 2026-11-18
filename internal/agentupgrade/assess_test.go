package agentupgrade

import (
	"testing"
	"time"

	"github.com/voxpupuli/enterprise-console/internal/agentdist"
)

var consoleBuilt = time.Date(2026, 11, 11, 9, 30, 0, 0, time.UTC)

// fakeCatalog serves one build for every platform, and artifacts for
// the install methods listed.
type fakeCatalog struct {
	build     agentdist.Build
	artifacts map[string]bool // "os/method"
}

func (f fakeCatalog) Build(goos, _ string) (agentdist.Build, bool) {
	if goos == "plan9" {
		return agentdist.Build{}, false
	}
	return f.build, true
}

func (f fakeCatalog) Artifact(goos, _, method string) (agentdist.Artifact, bool) {
	if !f.artifacts[goos+"/"+method] {
		return agentdist.Artifact{}, false
	}
	return agentdist.Artifact{Path: "/packages/" + method, Size: 10, SHA256: "artifact-" + method, Format: method}, true
}

func stampedCatalog() fakeCatalog {
	return fakeCatalog{
		build:     agentdist.Build{Digest: "console-build", BuildTime: consoleBuilt, Version: "v2"},
		artifacts: map[string]bool{"linux/deb": true, "linux/rpm": true, "darwin/binary": true, "windows/binary": true},
	}
}

func reported(digest string, built *time.Time, mutate ...func(*Record)) *Record {
	r := &Record{Certname: "n1", Capable: true, UpgradeState: UpgradeIdle, Status: Status{
		Digest: digest, BuildTime: built, OS: "linux", Arch: "amd64", InstallMethod: "deb", AcceptsUpgrades: true,
	}}
	for _, m := range mutate {
		m(r)
	}
	return r
}

func at(d time.Duration) *time.Time {
	t := consoleBuilt.Add(d)
	return &t
}

func TestAssess(t *testing.T) {
	unstamped := stampedCatalog()
	unstamped.build.BuildTime = time.Time{}
	noPackages := stampedCatalog()
	noPackages.artifacts = map[string]bool{"darwin/binary": true}

	for _, c := range []struct {
		name    string
		rec     *Record
		catalog fakeCatalog
		want    Assessment
	}{
		{"no record", nil, stampedCatalog(),
			Assessment{State: StateUnreported}},
		{"client predates build reporting", &Record{Certname: "n1", Capable: false}, stampedCatalog(),
			Assessment{State: StateUnknown, OutOfDate: true, Blocked: BlockedInstallScript}},
		{"same build", reported("console-build", at(0)), stampedCatalog(),
			Assessment{State: StateCurrent}},
		{"older build", reported("old", at(-time.Hour)), stampedCatalog(),
			Assessment{State: StateOutdated, OutOfDate: true, CanUpgrade: true}},
		{"newer build", reported("newer", at(time.Hour)), stampedCatalog(),
			Assessment{State: StateCurrent}},
		{"different build with the same build time", reported("other", at(0)), stampedCatalog(),
			Assessment{State: StateCurrent}},
		{"console build is unstamped", reported("old", at(-time.Hour)), unstamped,
			Assessment{State: StateCurrent}},
		{"console build is unstamped and the client cannot report", &Record{Capable: false}, unstamped,
			Assessment{State: StateUnknown, OutOfDate: true, Blocked: BlockedInstallScript}},
		{"client is unstamped", reported("dev", nil), stampedCatalog(),
			Assessment{State: StateOutdated, OutOfDate: true, CanUpgrade: true}},
		{"node declines upgrades", reported("old", at(-time.Hour), func(r *Record) { r.AcceptsUpgrades = false }), stampedCatalog(),
			Assessment{State: StateOutdated, OutOfDate: true, Blocked: BlockedDeclined}},
		{"script-managed Linux install", reported("old", at(-time.Hour), func(r *Record) { r.InstallMethod = "unknown" }), stampedCatalog(),
			Assessment{State: StateOutdated, OutOfDate: true, Blocked: BlockedInstallScript}},
		{"packages were not built", reported("old", at(-time.Hour)), noPackages,
			Assessment{State: StateOutdated, OutOfDate: true, Blocked: BlockedNoArtifact}},
		{"macOS binary", reported("old", at(-time.Hour), func(r *Record) { r.OS, r.InstallMethod = "darwin", "binary" }), stampedCatalog(),
			Assessment{State: StateOutdated, OutOfDate: true, CanUpgrade: true}},
		{"platform the console serves nothing for", reported("old", at(-time.Hour), func(r *Record) { r.OS = "plan9" }), stampedCatalog(),
			Assessment{State: StateCurrent}},
	} {
		t.Run(c.name, func(t *testing.T) {
			got := Assess(c.rec, c.catalog)
			if got.State != c.want.State || got.OutOfDate != c.want.OutOfDate || got.CanUpgrade != c.want.CanUpgrade || got.Blocked != c.want.Blocked {
				t.Errorf("Assess = {State:%s OutOfDate:%v CanUpgrade:%v Blocked:%q}, want {State:%s OutOfDate:%v CanUpgrade:%v Blocked:%q}",
					got.State, got.OutOfDate, got.CanUpgrade, got.Blocked,
					c.want.State, c.want.OutOfDate, c.want.CanUpgrade, c.want.Blocked)
			}
			if got.CanUpgrade && (got.Target.Digest != "console-build" || got.Artifact.SHA256 == "") {
				t.Errorf("an upgradable assessment lacks its target or artifact: %+v", got)
			}
		})
	}
}

func TestBackoff(t *testing.T) {
	want := []time.Duration{5 * time.Minute, 10 * time.Minute, 20 * time.Minute, 40 * time.Minute, 80 * time.Minute, 160 * time.Minute, 320 * time.Minute, 6 * time.Hour, 6 * time.Hour}
	for attempts, w := range want {
		if got := Backoff(attempts); got != w {
			t.Errorf("Backoff(%d) = %s, want %s", attempts, got, w)
		}
	}
	if got := Backoff(1000); got != 6*time.Hour {
		t.Errorf("Backoff(1000) = %s, want the cap", got)
	}
}
