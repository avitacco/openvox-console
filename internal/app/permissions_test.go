package app

import (
	"slices"
	"testing"
)

// The bootstrap admin role is created with exactly allPermissions, so a
// permission missing here is one no fresh install's administrator holds -
// and on an existing deployment, one nobody can grant through the UI
// either, since the roles page lists the same set.
func TestAllPermissions_IncludesVulnerabilityPermissions(t *testing.T) {
	for _, want := range []string{"vulnerabilities:read", "vulnerabilities:manage"} {
		if !slices.Contains(allPermissions, want) {
			t.Errorf("allPermissions is missing %q", want)
		}
	}
}
