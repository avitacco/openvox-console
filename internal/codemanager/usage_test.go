package codemanager

import (
	"context"
	"errors"
	"testing"
)

func envList(pairs ...any) NodeEnvironmentsFunc {
	nodes := make([]NodeEnvironment, 0, len(pairs)/2)
	for i := 0; i < len(pairs); i += 2 {
		entry := NodeEnvironment{Certname: pairs[i].(string)}
		if env, ok := pairs[i+1].(string); ok {
			entry.Environment = &env
		}
		nodes = append(nodes, entry)
	}
	return func(context.Context) ([]NodeEnvironment, error) { return nodes, nil }
}

func TestUsageResolver_TalliesBothReadings(t *testing.T) {
	u := NewUsageResolver(
		envList("a", "production", "b", "production", "c", "team_a_production", "d", nil),
		envList("a", "production", "b", "team_a_production", "c", "team_a_production", "d", nil),
	)

	usage, err := u.Usage(context.Background())
	if err != nil {
		t.Fatalf("Usage() error: %v", err)
	}

	if got := usage.Assigned.ByEnvironment["production"]; got != 2 {
		t.Errorf("assigned production = %d, want 2", got)
	}
	if got := usage.Reporting.ByEnvironment["production"]; got != 1 {
		t.Errorf("reporting production = %d, want 1", got)
	}
	if got := usage.Reporting.ByEnvironment["team_a_production"]; got != 2 {
		t.Errorf("reporting team_a_production = %d, want 2", got)
	}
	// A node with no environment under either reading is counted as
	// unknown, not against any repository.
	if usage.Assigned.Unknown != 1 || usage.Reporting.Unknown != 1 {
		t.Errorf("Unknown = assigned %d / reporting %d, want 1 each", usage.Assigned.Unknown, usage.Reporting.Unknown)
	}
}

func TestUsageResolver_DivergenceIsPreserved(t *testing.T) {
	// The case the whole two-count design exists for: a repository has
	// deployed and nodes are classified into it, but nothing has
	// actually run there yet. The two counts must not be reconciled.
	u := NewUsageResolver(
		envList("a", "team_a_production", "b", "team_a_production"),
		envList("a", "production", "b", "production"),
	)

	usage, err := u.Usage(context.Background())
	if err != nil {
		t.Fatalf("Usage() error: %v", err)
	}

	owners := attributeEnvironments(map[string]SourceDeploySummary{
		DefaultSourceName: {Environments: []string{"production"}},
		"team_a":          {Environments: []string{"team_a_production"}},
	})

	if got := usage.Assigned.forSource("team_a", owners); got != 2 {
		t.Errorf("team_a assigned = %d, want 2", got)
	}
	if got := usage.Reporting.forSource("team_a", owners); got != 0 {
		t.Errorf("team_a reporting = %d, want 0 - nothing has run there yet", got)
	}
	if got := usage.Reporting.forSource(DefaultSourceName, owners); got != 2 {
		t.Errorf("control reporting = %d, want 2", got)
	}
}

func TestUsageResolver_UnwiredIsUnavailableNotEmpty(t *testing.T) {
	for _, tc := range []struct {
		name string
		u    *UsageResolver
	}{
		{"nil resolver", nil},
		{"no readings", NewUsageResolver(nil, nil)},
		{"only assigned", NewUsageResolver(envList("a", "production"), nil)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := tc.u.Usage(context.Background()); err == nil {
				t.Error("Usage() returned no error; an unwired resolver must report unavailable, not zero counts")
			}
		})
	}
}

func TestUsageResolver_ReadingFailurePropagates(t *testing.T) {
	// So the caller can report "unavailable" rather than rendering
	// every repository as unused.
	boom := errors.New("openvoxdb unreachable")
	failing := func(context.Context) ([]NodeEnvironment, error) { return nil, boom }

	u := NewUsageResolver(envList("a", "production"), failing)
	if _, err := u.Usage(context.Background()); !errors.Is(err, boom) {
		t.Errorf("Usage() error = %v, want the underlying failure", err)
	}

	u = NewUsageResolver(failing, envList("a", "production"))
	if _, err := u.Usage(context.Background()); !errors.Is(err, boom) {
		t.Errorf("Usage() error = %v, want the underlying failure", err)
	}
}
