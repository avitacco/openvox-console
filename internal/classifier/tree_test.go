package classifier

import (
	"reflect"
	"testing"
)

func int64Ptr(v int64) *int64 { return &v }

// rootGroup is the built-in root as the store returns it.
func rootGroup() Group { return Group{ID: 1, Name: "All Nodes", IsRoot: true} }

func factRule(path, value string) []Condition {
	return []Condition{{FactPath: path, Operator: "=", Value: value}}
}

func names(groups []Group) []string {
	out := make([]string, len(groups))
	for i, g := range groups {
		out[i] = g.Name
	}
	return out
}

func TestOwnMatch_RuleMatchModes(t *testing.T) {
	facts := map[string]any{"dc": "ams", "role": "web"}
	both := []Condition{
		{FactPath: "dc", Operator: "=", Value: "ams"},
		{FactPath: "role", Operator: "=", Value: "db"},
	}
	neither := []Condition{
		{FactPath: "dc", Operator: "=", Value: "lon"},
		{FactPath: "role", Operator: "=", Value: "db"},
	}

	tests := []struct {
		name  string
		group Group
		want  bool
	}{
		{"all: one condition fails", Group{Rule: both, RuleMatch: RuleMatchAll}, false},
		{"unset mode behaves as all", Group{Rule: both}, false},
		{"any: one condition holds", Group{Rule: both, RuleMatch: RuleMatchAny}, true},
		{"any: no condition holds", Group{Rule: neither, RuleMatch: RuleMatchAny}, false},
		{"all: empty rule matches nothing", Group{RuleMatch: RuleMatchAll}, false},
		{"any: empty rule matches nothing", Group{RuleMatch: RuleMatchAny}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ownMatch(tt.group, "node1", facts); got != tt.want {
				t.Errorf("ownMatch() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestTree_RootMatchesEveryNode(t *testing.T) {
	tree := NewTree([]Group{rootGroup()})

	// Including a node with no known facts at all.
	for _, facts := range []map[string]any{nil, {}, {"kernel": "Linux"}} {
		got := tree.Matching("node1", facts)
		if !reflect.DeepEqual(names(got), []string{"All Nodes"}) {
			t.Errorf("Matching(%v) = %v, want just the root", facts, names(got))
		}
	}
	if !tree.Contains(1, "node1", nil) {
		t.Error("Contains(root) = false, want true for any node")
	}
}

func TestTree_ChildMatchesOnlyWithinParent(t *testing.T) {
	groups := []Group{
		rootGroup(),
		{ID: 2, Name: "linux", ParentID: int64Ptr(1), Priority: 10, Rule: factRule("kernel", "Linux")},
		{ID: 3, Name: "web", ParentID: int64Ptr(2), Priority: 20, Rule: factRule("role", "web")},
	}
	tree := NewTree(groups)

	inside := map[string]any{"kernel": "Linux", "role": "web"}
	if got := names(tree.Matching("n1", inside)); !reflect.DeepEqual(got, []string{"All Nodes", "linux", "web"}) {
		t.Errorf("node inside the parent: Matching() = %v", got)
	}
	if !tree.Contains(3, "n1", inside) {
		t.Error("Contains(web) = false for a node matching web and its parent")
	}

	// Satisfies the child's rule but not the parent's.
	outside := map[string]any{"kernel": "windows", "role": "web"}
	if got := names(tree.Matching("n2", outside)); !reflect.DeepEqual(got, []string{"All Nodes"}) {
		t.Errorf("node outside the parent: Matching() = %v, want only the root", got)
	}
	if tree.Contains(3, "n2", outside) {
		t.Error("Contains(web) = true for a node its parent does not match")
	}
}

func TestTree_PinOverridesOwnRuleButNotTheParent(t *testing.T) {
	groups := []Group{
		rootGroup(),
		{ID: 2, Name: "linux", ParentID: int64Ptr(1), Priority: 10, Rule: factRule("kernel", "Linux")},
		{ID: 3, Name: "web", ParentID: int64Ptr(2), Priority: 20, Rule: factRule("role", "web"), Pins: []string{"pinned"}},
	}
	tree := NewTree(groups)

	// Pinned, fails the group's own rule, matches the parent.
	if !tree.Contains(3, "pinned", map[string]any{"kernel": "Linux", "role": "db"}) {
		t.Error("a pinned node inside the parent should match despite failing the group's rule")
	}
	// Pinned, but outside the parent.
	if tree.Contains(3, "pinned", map[string]any{"kernel": "windows"}) {
		t.Error("a pinned node outside the parent should not match")
	}
}

func TestTree_GroupWithNoRuleAndNoPinsMatchesNothing(t *testing.T) {
	groups := []Group{
		rootGroup(),
		{ID: 2, Name: "empty", ParentID: int64Ptr(1), Priority: 10},
	}
	if NewTree(groups).Contains(2, "n1", map[string]any{"kernel": "Linux"}) {
		t.Error("a non-root group with no rule and no pins should match no node")
	}
}

// A subtree under a group the node fails is never evaluated. The child's
// rule here uses a regex that would match, so if it were consulted the
// child would be reported.
func TestTree_SubtreeUnderNonMatchingGroupIsNotVisited(t *testing.T) {
	groups := []Group{
		rootGroup(),
		{ID: 2, Name: "linux", ParentID: int64Ptr(1), Priority: 10, Rule: factRule("kernel", "Linux")},
		{ID: 3, Name: "anything", ParentID: int64Ptr(2), Priority: 20, Rule: []Condition{{FactPath: "kernel", Operator: "~", Value: "."}}},
		{ID: 4, Name: "deeper", ParentID: int64Ptr(3), Priority: 30, Pins: []string{"n1"}},
	}
	got := names(NewTree(groups).Matching("n1", map[string]any{"kernel": "windows"}))
	if !reflect.DeepEqual(got, []string{"All Nodes"}) {
		t.Errorf("Matching() = %v, want only the root", got)
	}
}

func TestTree_ContainsUnknownGroup(t *testing.T) {
	if NewTree([]Group{rootGroup()}).Contains(99, "n1", nil) {
		t.Error("Contains() of a group not in the tree should be false")
	}
}

// The store refuses to create a loop, but a tree built from one must not
// hang or match.
func TestTree_ParentLoopMatchesNothing(t *testing.T) {
	groups := []Group{
		rootGroup(),
		{ID: 2, Name: "a", ParentID: int64Ptr(3), Priority: 10, Pins: []string{"n1"}},
		{ID: 3, Name: "b", ParentID: int64Ptr(2), Priority: 20, Pins: []string{"n1"}},
	}
	tree := NewTree(groups)
	if got := names(tree.Matching("n1", nil)); !reflect.DeepEqual(got, []string{"All Nodes"}) {
		t.Errorf("Matching() = %v, want only the root", got)
	}
	if tree.Contains(2, "n1", nil) {
		t.Error("Contains() of a group in a parent loop should be false")
	}
}

// param builds a group pinned to "n1" that sets top-scope parameter "k".
func param(id int64, name string, parent int64, priority int, value string) Group {
	return Group{
		ID: id, Name: name, ParentID: int64Ptr(parent), Priority: priority,
		Pins: []string{"n1"}, Parameters: map[string]any{"k": value},
	}
}

func TestClassify_ChildWinsOverLowerPriorityParent(t *testing.T) {
	groups := []Group{rootGroup(), param(2, "parent", 1, 10, "parent"), param(3, "child", 2, 20, "child")}
	if got := Classify(groups, "n1", nil).Parameters["k"]; got != "child" {
		t.Errorf("k = %v, want the child's value", got)
	}
}

func TestClassify_ChildWinsOverHigherPriorityParent(t *testing.T) {
	groups := []Group{rootGroup(), param(2, "parent", 1, 20, "parent"), param(3, "child", 2, 10, "child")}
	if got := Classify(groups, "n1", nil).Parameters["k"]; got != "child" {
		t.Errorf("k = %v, want the child's value although the parent has the higher priority", got)
	}
}

func TestClassify_DescendantWinsOverDistantAncestor(t *testing.T) {
	groups := []Group{
		rootGroup(),
		{ID: 2, Name: "top", ParentID: int64Ptr(1), Priority: 90, Pins: []string{"n1"}, Environment: strPtr("production")},
		{ID: 3, Name: "middle", ParentID: int64Ptr(2), Priority: 50, Pins: []string{"n1"}},
		{ID: 4, Name: "leaf", ParentID: int64Ptr(3), Priority: 10, Pins: []string{"n1"}, Environment: strPtr("staging")},
	}
	env := Classify(groups, "n1", nil).Environment
	if env == nil || *env != "staging" {
		t.Errorf("Environment = %v, want the descendant's (staging)", env)
	}
}

// Parent 10, its child 1, an unrelated group 5: the child ranks no lower
// than its parent, so it beats the unrelated group too.
func TestClassify_ChildCarriesParentRankAgainstUnrelatedGroup(t *testing.T) {
	groups := []Group{
		rootGroup(),
		{ID: 2, Name: "parent", ParentID: int64Ptr(1), Priority: 10, Pins: []string{"n1"}},
		param(3, "child", 2, 1, "child"),
		param(4, "unrelated", 1, 5, "unrelated"),
	}
	if got := Classify(groups, "n1", nil).Parameters["k"]; got != "child" {
		t.Errorf("k = %v, want the child's value", got)
	}
}

func TestClassify_TwoLiftedSiblingsDecidedByOwnPriority(t *testing.T) {
	groups := []Group{
		rootGroup(),
		{ID: 2, Name: "parent", ParentID: int64Ptr(1), Priority: 10, Pins: []string{"n1"}},
		param(3, "one", 2, 1, "one"),
		param(4, "two", 2, 2, "two"),
	}
	if got := Classify(groups, "n1", nil).Parameters["k"]; got != "two" {
		t.Errorf("k = %v, want the value from the sibling with priority 2", got)
	}
}

func TestClassify_UnrelatedGroupsStillDecidedByPriority(t *testing.T) {
	groups := []Group{rootGroup(), param(2, "low", 1, 10, "low"), param(3, "high", 1, 20, "high")}
	if got := Classify(groups, "n1", nil).Parameters["k"]; got != "high" {
		t.Errorf("k = %v, want the higher-priority group's value", got)
	}
}

func TestClassify_ChildInheritsWhatItDoesNotSet(t *testing.T) {
	groups := []Group{
		rootGroup(),
		{ID: 2, Name: "parent", ParentID: int64Ptr(1), Priority: 10, Pins: []string{"n1"}, Classes: []Class{{Name: "ntp", Parameters: map[string]any{"server": "a"}}}},
		{ID: 3, Name: "child", ParentID: int64Ptr(2), Priority: 20, Pins: []string{"n1"}, Classes: []Class{{Name: "nginx"}}},
	}
	classes := Classify(groups, "n1", nil).Classes
	if _, ok := classes["ntp"]; !ok {
		t.Errorf("Classes = %v, want the parent's ntp class included", classes)
	}
	if _, ok := classes["nginx"]; !ok {
		t.Errorf("Classes = %v, want the child's nginx class included", classes)
	}
}

func TestClassify_RootValuesReachEveryNodeAndLoseToAnyGroup(t *testing.T) {
	root := rootGroup()
	root.Classes = []Class{{Name: "base"}}
	root.Parameters = map[string]any{"k": "root"}
	// Priority is deliberately negative: the root must rank below even that.
	groups := []Group{root, param(2, "group", 1, -5, "group")}

	other := Classify(groups, "someone-else", nil)
	if _, ok := other.Classes["base"]; !ok {
		t.Error("a class on the root should reach a node no other group matches")
	}
	if other.Parameters["k"] != "root" {
		t.Errorf("k = %v, want the root's value where nothing overrides it", other.Parameters["k"])
	}
	if got := Classify(groups, "n1", nil).Parameters["k"]; got != "group" {
		t.Errorf("k = %v, want the group's value over the root's", got)
	}
}

func TestClassify_OnlyAnEmptyRootMatchesGivesEmptyClassification(t *testing.T) {
	groups := []Group{rootGroup(), {ID: 2, Name: "web", ParentID: int64Ptr(1), Priority: 1, Rule: factRule("role", "db")}}
	result := Classify(groups, "web01", map[string]any{"role": "web"})
	if len(result.Classes) != 0 || len(result.Parameters) != 0 || result.Environment != nil {
		t.Errorf("Classify() = %+v, want an empty classification", result)
	}
}

// Both x and z are lifted to the parent's rank of 10 and neither is
// above the other. z is deeper, so it wins although x has the higher
// priority of the two.
func TestClassify_EqualRankAtDifferentDepthsDeeperWins(t *testing.T) {
	groups := []Group{
		rootGroup(),
		{ID: 2, Name: "parent", ParentID: int64Ptr(1), Priority: 10, Pins: []string{"n1"}},
		param(3, "x", 2, 5, "x"),
		{ID: 4, Name: "y", ParentID: int64Ptr(2), Priority: 2, Pins: []string{"n1"}},
		param(5, "z", 4, 3, "z"),
	}
	if got := Classify(groups, "n1", nil).Parameters["k"]; got != "z" {
		t.Errorf("k = %v, want the deeper group's value", got)
	}
}
