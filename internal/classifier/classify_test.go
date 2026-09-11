package classifier

import (
	"reflect"
	"testing"
)

func strPtr(s string) *string { return &s }

func TestClassify_NoMatchingGroups(t *testing.T) {
	groups := []Group{
		{Name: "web", Priority: 1, Rule: []Condition{{FactPath: "role", Operator: "=", Value: "db"}}},
	}
	result := Classify(groups, "web01", map[string]any{"role": "web"})

	if len(result.Classes) != 0 {
		t.Errorf("Classes = %v, want empty", result.Classes)
	}
	if len(result.Parameters) != 0 {
		t.Errorf("Parameters = %v, want empty", result.Parameters)
	}
	if result.Environment != nil {
		t.Errorf("Environment = %v, want nil", result.Environment)
	}
}

func TestClassify_HigherPriorityWinsConflict(t *testing.T) {
	groups := []Group{
		{
			Name: "base", Priority: 1, Pins: []string{"web01"},
			Classes: []Class{{Name: "ntp", Parameters: map[string]any{"server": "low-priority.example"}}},
		},
		{
			Name: "override", Priority: 2, Pins: []string{"web01"},
			Classes: []Class{{Name: "ntp", Parameters: map[string]any{"server": "high-priority.example"}}},
		},
	}
	result := Classify(groups, "web01", map[string]any{})

	got := result.Classes["ntp"]["server"]
	if got != "high-priority.example" {
		t.Errorf("ntp.server = %v, want the higher-priority group's value", got)
	}
}

func TestClassify_NonConflictingClassesAreUnioned(t *testing.T) {
	groups := []Group{
		{Name: "a", Priority: 1, Pins: []string{"web01"}, Classes: []Class{{Name: "ntp"}}},
		{Name: "b", Priority: 2, Pins: []string{"web01"}, Classes: []Class{{Name: "nginx"}}},
	}
	result := Classify(groups, "web01", map[string]any{})

	if _, ok := result.Classes["ntp"]; !ok {
		t.Error("expected ntp class from group a")
	}
	if _, ok := result.Classes["nginx"]; !ok {
		t.Error("expected nginx class from group b")
	}
}

func TestClassify_EnvironmentFromHighestPriorityMatchingGroup(t *testing.T) {
	groups := []Group{
		{Name: "a", Priority: 1, Pins: []string{"web01"}, Environment: strPtr("staging")},
		{Name: "b", Priority: 2, Pins: []string{"web01"}, Environment: strPtr("production")},
	}
	result := Classify(groups, "web01", map[string]any{})

	if result.Environment == nil || *result.Environment != "production" {
		t.Errorf("Environment = %v, want production", result.Environment)
	}
}

func TestClassify_EnvironmentOmittedWhenNoGroupSetsOne(t *testing.T) {
	groups := []Group{{Name: "a", Priority: 1, Pins: []string{"web01"}, Classes: []Class{{Name: "ntp"}}}}
	result := Classify(groups, "web01", map[string]any{})

	if result.Environment != nil {
		t.Errorf("Environment = %v, want nil", *result.Environment)
	}
}

func TestClassify_ParametersMergedAcrossGroups(t *testing.T) {
	groups := []Group{
		{Name: "a", Priority: 1, Pins: []string{"web01"}, Parameters: map[string]any{"role": "web"}},
		{Name: "b", Priority: 2, Pins: []string{"web01"}, Parameters: map[string]any{"datacenter": "us-east"}},
	}
	result := Classify(groups, "web01", map[string]any{})

	want := map[string]any{"role": "web", "datacenter": "us-east"}
	if !reflect.DeepEqual(result.Parameters, want) {
		t.Errorf("Parameters = %v, want %v", result.Parameters, want)
	}
}

func TestClassify_OnlyMatchingGroupsContribute(t *testing.T) {
	groups := []Group{
		{Name: "matches", Priority: 1, Pins: []string{"web01"}, Classes: []Class{{Name: "ntp"}}},
		{Name: "no-match", Priority: 2, Rule: []Condition{{FactPath: "role", Operator: "=", Value: "db"}}, Classes: []Class{{Name: "postgres"}}},
	}
	result := Classify(groups, "web01", map[string]any{"role": "web"})

	if _, ok := result.Classes["postgres"]; ok {
		t.Error("non-matching group's class should not appear in the result")
	}
	if _, ok := result.Classes["ntp"]; !ok {
		t.Error("matching group's class should appear in the result")
	}
}
