package main

import (
	"strings"
	"time"
)

// untaggedBase is the version base used until the repository has a
// release tag. It is above 0.0.0 on purpose: every package served
// before versions were ordered was 0.0.0+<sha> (deb), so any 0.1.0
// package is an upgrade over all of them.
const untaggedBase = "0.1.0"

// rpmEpoch puts every package built from here on above everything
// served earlier. Before versions were ordered the rpm Version was the
// bare git sha, and a sha that happens to start with a digit (4a4a7ef)
// compares above any small version number under rpm's rules, so a
// version alone cannot be made to win. An epoch is compared first.
const rpmEpoch = "1"

// packageVersions turns the project VERSION (`git describe --tags
// --always --dirty`: "v0.3.2-14-g1a2b3c4-dirty", a bare "bbba663-dirty"
// with no tags, or "dev") and the client's build time into versions
// each package manager orders by build time:
//
//	deb: <base>+<YYYYMMDDHHMMSS>.<revision>
//	rpm: version <base>, release <YYYYMMDDHHMMSS>.<revision>
//
// base is the release tag, or untaggedBase without one. The timestamp
// leads what follows the base, so two builds of one base order by when
// they were built whatever their revisions are; the revision is there
// only to say which source a package came from.
func packageVersions(describe string, buildTime time.Time) (deb, rpmVersion, rpmRelease string) {
	base, revision := splitDescribe(describe)
	stamp := buildTime.UTC().Format("20060102150405")

	debSuffix, rpmSuffix := stamp, stamp
	if revision != "" {
		// Hyphens separate a Debian revision and are not allowed in an
		// rpm release at all.
		debSuffix += "." + strings.ReplaceAll(revision, "-", ".")
		rpmSuffix += "." + strings.ReplaceAll(revision, "-", "_")
	}
	return base + "+" + debSuffix, base, rpmSuffix
}

// splitDescribe separates a release tag from whatever follows it.
// Anything not starting with a digit has no tag in it.
func splitDescribe(describe string) (base, revision string) {
	v := strings.TrimPrefix(describe, "v")
	if v == "" || v[0] < '0' || v[0] > '9' || !strings.Contains(strings.SplitN(v, "-", 2)[0], ".") {
		return untaggedBase, sanitizeRevision(v)
	}
	base, revision, _ = strings.Cut(v, "-")
	return base, sanitizeRevision(revision)
}

// sanitizeRevision keeps only characters both package formats accept.
func sanitizeRevision(s string) string {
	return strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '.', r == '-':
			return r
		}
		return '.'
	}, s)
}
