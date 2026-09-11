package classifier

import "testing"

func TestMatches_Operators(t *testing.T) {
	facts := map[string]any{
		"kernel": "Linux",
		"os": map[string]any{
			"family": "Debian",
		},
		"processors": map[string]any{
			"count": float64(8),
		},
	}

	tests := []struct {
		name string
		cond Condition
		want bool
	}{
		{"equal match", Condition{FactPath: "kernel", Operator: "=", Value: "Linux"}, true},
		{"equal no match", Condition{FactPath: "kernel", Operator: "=", Value: "Windows"}, false},
		{"not-equal match", Condition{FactPath: "kernel", Operator: "!=", Value: "Windows"}, true},
		{"not-equal no match", Condition{FactPath: "kernel", Operator: "!=", Value: "Linux"}, false},
		{"regex match", Condition{FactPath: "kernel", Operator: "~", Value: "^Lin"}, true},
		{"regex no match", Condition{FactPath: "kernel", Operator: "~", Value: "^Win"}, false},
		{"greater-than match", Condition{FactPath: "processors.count", Operator: ">", Value: "4"}, true},
		{"greater-than no match", Condition{FactPath: "processors.count", Operator: ">", Value: "16"}, false},
		{"less-than match", Condition{FactPath: "processors.count", Operator: "<", Value: "16"}, true},
		{"greater-or-equal match (equal)", Condition{FactPath: "processors.count", Operator: ">=", Value: "8"}, true},
		{"less-or-equal match (equal)", Condition{FactPath: "processors.count", Operator: "<=", Value: "8"}, true},
		{"nested fact path", Condition{FactPath: "os.family", Operator: "=", Value: "Debian"}, true},
		{"missing fact path", Condition{FactPath: "os.nonexistent", Operator: "=", Value: "x"}, false},
		{"unknown operator", Condition{FactPath: "kernel", Operator: "??", Value: "Linux"}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.cond.matches(facts); got != tt.want {
				t.Errorf("%+v.matches(facts) = %v, want %v", tt.cond, got, tt.want)
			}
		})
	}
}

func TestMatches_EmptyRuleAndNoPinsMatchesNothing(t *testing.T) {
	g := Group{Name: "empty"}
	if Matches(g, "any-node", map[string]any{}) {
		t.Error("a group with no rule and no pins should not match any node")
	}
}

func TestMatches_PinnedNodeMatchesWithoutSatisfyingRule(t *testing.T) {
	g := Group{
		Name: "pinned",
		Rule: []Condition{{FactPath: "kernel", Operator: "=", Value: "Windows"}},
		Pins: []string{"web01"},
	}
	facts := map[string]any{"kernel": "Linux"} // does NOT satisfy the rule

	if !Matches(g, "web01", facts) {
		t.Error("expected pinned node to match despite failing the rule")
	}
	if Matches(g, "web02", facts) {
		t.Error("expected non-pinned, non-rule-matching node not to match")
	}
}

func TestMatches_AllConditionsMustMatch(t *testing.T) {
	g := Group{
		Name: "and-rule",
		Rule: []Condition{
			{FactPath: "kernel", Operator: "=", Value: "Linux"},
			{FactPath: "os.family", Operator: "=", Value: "RedHat"}, // won't match Debian facts
		},
	}
	facts := map[string]any{
		"kernel": "Linux",
		"os":     map[string]any{"family": "Debian"},
	}
	if Matches(g, "node1", facts) {
		t.Error("expected group not to match when only some AND conditions are satisfied")
	}
}
