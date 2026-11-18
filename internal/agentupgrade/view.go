package agentupgrade

import (
	"context"
	"time"
)

// NodeView is one node's client as the API reports it: the stored
// record, judged against the build this console serves.
type NodeView struct {
	Version       string     `json:"version,omitempty"`
	BuildTime     *time.Time `json:"buildTime,omitempty"`
	Digest        string     `json:"digest,omitempty"`
	OS            string     `json:"os,omitempty"`
	Arch          string     `json:"arch,omitempty"`
	InstallMethod string     `json:"installMethod,omitempty"`
	ReportedAt    *time.Time `json:"reportedAt,omitempty"`

	// State is current, outdated, unknown or unreported.
	State State `json:"state"`
	// OutOfDate is true for outdated and unknown.
	OutOfDate bool `json:"outOfDate"`
	// CanUpgrade is whether the console can upgrade this client now:
	// it is out of date, able to upgrade itself, and not already doing
	// so. Whether the node is connected is the caller's to check.
	CanUpgrade bool `json:"canUpgrade"`
	// Blocked says why an out-of-date client is not upgraded by the
	// console: "install-script", "declined" or "no-artifact".
	Blocked string `json:"blocked,omitempty"`
	// TargetVersion is the version the console would upgrade it to.
	TargetVersion string `json:"targetVersion,omitempty"`

	// Upgrade is the most recent attempt, present while one is in
	// progress or after one failed. Absent once the node is current.
	Upgrade *UpgradeView `json:"upgrade,omitempty"`
}

// UpgradeView is the state of the console's attempts to upgrade a node.
type UpgradeView struct {
	// State is requested, failed or exhausted.
	State         string     `json:"state"`
	Error         string     `json:"error,omitempty"`
	Attempts      int        `json:"attempts"`
	MaxAttempts   int        `json:"maxAttempts"`
	NextAttemptAt *time.Time `json:"nextAttemptAt,omitempty"`
}

// View judges rec against catalog. rec is nil for a node with no
// record, which yields the unreported view.
func View(rec *Record, catalog Catalog) NodeView {
	a := Assess(rec, catalog)
	v := NodeView{State: a.State, OutOfDate: a.OutOfDate, Blocked: a.Blocked}
	if rec == nil {
		return v
	}
	reportedAt := rec.ReportedAt
	v.ReportedAt = &reportedAt
	if rec.Capable {
		v.Version, v.BuildTime, v.Digest = rec.Version, rec.BuildTime, rec.Digest
		v.OS, v.Arch, v.InstallMethod = rec.OS, rec.Arch, rec.InstallMethod
	}
	if !a.OutOfDate {
		return v
	}
	if a.CanUpgrade {
		v.TargetVersion = a.Target.Version
		v.CanUpgrade = rec.UpgradeState != UpgradeRequested
	}
	// An attempt recorded against a build this console no longer serves
	// says nothing about the upgrade it would make now.
	if rec.UpgradeState != UpgradeIdle && (!a.CanUpgrade || rec.TargetDigest == a.Target.Digest) {
		v.Upgrade = &UpgradeView{
			State: rec.UpgradeState, Error: rec.LastError, Attempts: rec.Attempts,
			MaxAttempts: MaxAttempts,
		}
		if rec.UpgradeState == UpgradeFailed {
			v.Upgrade.NextAttemptAt = rec.NextAttemptAt
		}
	}
	return v
}

// Views returns every recorded node's view, by certname. Nodes with no
// record are absent; View(nil, ...) is their view.
func (c *Coordinator) Views(ctx context.Context) (map[string]NodeView, error) {
	records, err := c.store.List(ctx)
	if err != nil {
		return nil, err
	}
	out := make(map[string]NodeView, len(records))
	for i := range records {
		out[records[i].Certname] = View(&records[i], c.catalog)
	}
	return out, nil
}
