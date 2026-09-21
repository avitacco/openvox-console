package codemanager

import "testing"

func strptr(s string) *string { return &s }

func TestAttributeEnvironments_OnlyWhatWasDeployed(t *testing.T) {
	owners := attributeEnvironments(map[string]SourceDeploySummary{
		DefaultSourceName: {Environments: []string{"production", "staging"}},
		"team_a":          {Environments: []string{"team_a_production"}},
	})

	for env, want := range map[string]string{
		"production":        DefaultSourceName,
		"staging":           DefaultSourceName,
		"team_a_production": "team_a",
	} {
		if got := owners[env]; got != want {
			t.Errorf("owners[%q] = %q, want %q", env, got, want)
		}
	}

	// The failure prefix matching would produce: an environment nobody
	// deployed must not land on the unprefixed source just because no
	// other prefix claims it.
	if got, owned := owners["some_abandoned_env"]; owned {
		t.Errorf("an undeployed environment was attributed to %q", got)
	}
}

func TestEnvironmentCounts_Tally(t *testing.T) {
	owners := attributeEnvironments(map[string]SourceDeploySummary{
		DefaultSourceName: {Environments: []string{"production"}},
		"team_a":          {Environments: []string{"team_a_production"}},
	})

	var counts EnvironmentCounts
	counts.tally(strptr("production"))
	counts.tally(strptr("production"))
	counts.tally(strptr("team_a_production"))
	counts.tally(strptr("legacy_env")) // deployed by nothing configured
	counts.tally(nil)                  // never reported
	counts.tally(strptr(""))           // reported with no environment

	if got := counts.forSource(DefaultSourceName, owners); got != 2 {
		t.Errorf("control count = %d, want 2", got)
	}
	if got := counts.forSource("team_a", owners); got != 1 {
		t.Errorf("team_a count = %d, want 1", got)
	}
	if got := counts.unattributed(owners); got != 1 {
		t.Errorf("unattributed = %d, want 1 (legacy_env)", got)
	}
	if counts.Unknown != 2 {
		t.Errorf("Unknown = %d, want 2 (a nil and an empty environment)", counts.Unknown)
	}

	// A source that deployed nothing owns nothing, rather than
	// inheriting the leftovers.
	if got := counts.forSource("never_deployed", owners); got != 0 {
		t.Errorf("never-deployed source count = %d, want 0", got)
	}
}

func TestEnvironmentCounts_ZeroValueIsUsable(t *testing.T) {
	var counts EnvironmentCounts
	owners := EnvironmentOwners{}
	if got := counts.forSource("anything", owners); got != 0 {
		t.Errorf("forSource on a zero EnvironmentCounts = %d, want 0", got)
	}
	if got := counts.unattributed(owners); got != 0 {
		t.Errorf("unattributed on a zero EnvironmentCounts = %d, want 0", got)
	}
}
