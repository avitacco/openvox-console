package agentupgrade

import (
	"github.com/voxpupuli/enterprise-console/internal/agentdist"
)

// State is how a node's client compares with the build this console
// serves.
type State string

const (
	// StateCurrent: the same build, or a newer one.
	StateCurrent State = "current"
	// StateOutdated: an older build than this console serves.
	StateOutdated State = "outdated"
	// StateUnknown: the client cannot report its build, so it predates
	// build reporting. Out of date, and unable to upgrade itself.
	StateUnknown State = "unknown"
	// StateUnreported: the node has not connected since build tracking
	// was introduced. Nothing is known, so nothing is claimed.
	StateUnreported State = "unreported"
)

// Reasons an out-of-date client is not upgraded by the console.
const (
	// BlockedInstallScript: the client cannot upgrade itself; the
	// install script has to be re-run on the node.
	BlockedInstallScript = "install-script"
	// BlockedDeclined: the node is configured to decline upgrades.
	BlockedDeclined = "declined"
	// BlockedNoArtifact: this console has nothing to offer the node's
	// platform - typically Linux packages that were not built.
	BlockedNoArtifact = "no-artifact"
)

// Catalog is what Assess needs to know about the client builds this
// console serves. *agentdist.Catalog satisfies it.
type Catalog interface {
	Build(goos, goarch string) (agentdist.Build, bool)
	Artifact(goos, goarch, installMethod string) (agentdist.Artifact, bool)
}

// Assessment is the outcome of comparing one node's client with the
// console's.
type Assessment struct {
	State State
	// OutOfDate is true for StateOutdated and StateUnknown.
	OutOfDate bool
	// CanUpgrade is whether the console can tell this client to
	// upgrade. Blocked says why not, when it is out of date and cannot.
	CanUpgrade bool
	Blocked    string
	// Target and Artifact are set when CanUpgrade is.
	Target   agentdist.Build
	Artifact agentdist.Artifact
}

// Assess decides whether rec's client is out of date against catalog,
// and whether the console can upgrade it. rec is nil for a node with no
// record.
//
// A client is out of date when its build is unknown, or when it differs
// from the console's build for its platform and was built earlier. It
// is never out of date for being newer - a console cluster part-way
// through a deployment, or one that was rolled back, must not push
// clients backwards - and never when the console's own build carries no
// build time, since "earlier" then has no meaning.
func Assess(rec *Record, catalog Catalog) Assessment {
	if rec == nil {
		return Assessment{State: StateUnreported}
	}
	if !rec.Capable {
		return Assessment{State: StateUnknown, OutOfDate: true, Blocked: BlockedInstallScript}
	}

	build, ok := catalog.Build(rec.OS, rec.Arch)
	if !ok || build.BuildTime.IsZero() || rec.Digest == build.Digest {
		return Assessment{State: StateCurrent}
	}
	// A client with no build time of its own is a development build;
	// every stamped build is taken to supersede it.
	if rec.BuildTime != nil && !rec.BuildTime.Before(build.BuildTime) {
		return Assessment{State: StateCurrent}
	}

	a := Assessment{State: StateOutdated, OutOfDate: true}
	switch {
	case !rec.AcceptsUpgrades:
		a.Blocked = BlockedDeclined
	case rec.InstallMethod != agentdist.FormatDeb && rec.InstallMethod != agentdist.FormatRPM && rec.InstallMethod != agentdist.FormatBinary,
		rec.InstallMethod == agentdist.FormatBinary && rec.OS == "linux":
		// Not installed in a way the client can replace: a Linux binary
		// no package owns is a pre-packaging install.
		a.Blocked = BlockedInstallScript
	default:
		artifact, ok := catalog.Artifact(rec.OS, rec.Arch, rec.InstallMethod)
		if !ok {
			a.Blocked = BlockedNoArtifact
			break
		}
		a.CanUpgrade = true
		a.Target = build
		a.Artifact = artifact
	}
	return a
}
