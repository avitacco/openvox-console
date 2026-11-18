package classifier

import (
	"reflect"
	"testing"
)

func classGroup(id int64, name string, parent int64, priority int, server string) Group {
	return Group{
		ID: id, Name: name, ParentID: int64Ptr(parent), Priority: priority,
		Classes: []Class{{Name: "ntp", Parameters: map[string]any{"server": server}}},
	}
}

func TestWarnings_ChildOverridesHigherPriorityAncestor(t *testing.T) {
	groups := []Group{rootGroup(), classGroup(2, "parent", 1, 20, "a"), classGroup(3, "child", 2, 10, "b")}

	got := Warnings(groups)
	want := map[int64][]Warning{
		3: {{AncestorID: 2, AncestorName: "parent", Kind: WarningClassParameter, Class: "ntp", Key: "server"}},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Warnings() = %+v, want %+v", got, want)
	}
}

func TestWarnings_NoneWhenChildHasHigherPriority(t *testing.T) {
	groups := []Group{rootGroup(), classGroup(2, "parent", 1, 10, "a"), classGroup(3, "child", 2, 20, "b")}
	if got := Warnings(groups); len(got) != 0 {
		t.Errorf("Warnings() = %+v, want none", got)
	}
}

func TestWarnings_NoneWithoutAConflictingValue(t *testing.T) {
	same := []Group{rootGroup(), classGroup(2, "parent", 1, 20, "a"), classGroup(3, "child", 2, 10, "a")}
	if got := Warnings(same); len(got) != 0 {
		t.Errorf("same value: Warnings() = %+v, want none", got)
	}

	disjoint := []Group{
		rootGroup(),
		classGroup(2, "parent", 1, 20, "a"),
		{ID: 3, Name: "child", ParentID: int64Ptr(2), Priority: 10, Parameters: map[string]any{"other": 1}},
	}
	if got := Warnings(disjoint); len(got) != 0 {
		t.Errorf("no shared value: Warnings() = %+v, want none", got)
	}
}

func TestWarnings_NeverAgainstTheRoot(t *testing.T) {
	root := rootGroup()
	root.Parameters = map[string]any{"k": "root"}
	root.Environment = strPtr("production")
	groups := []Group{
		root,
		{ID: 2, Name: "group", ParentID: int64Ptr(1), Priority: -100, Parameters: map[string]any{"k": "mine"}, Environment: strPtr("staging")},
	}
	if got := Warnings(groups); len(got) != 0 {
		t.Errorf("Warnings() = %+v, want none against the root", got)
	}
}

func TestWarnings_EveryKindAndDistantAncestors(t *testing.T) {
	groups := []Group{
		rootGroup(),
		{
			ID: 2, Name: "top", ParentID: int64Ptr(1), Priority: 90,
			Environment: strPtr("production"),
			Parameters:  map[string]any{"zone": "a", "tier": "gold"},
		},
		{ID: 3, Name: "middle", ParentID: int64Ptr(2), Priority: 5},
		{
			ID: 4, Name: "leaf", ParentID: int64Ptr(3), Priority: 10,
			Environment: strPtr("staging"),
			Parameters:  map[string]any{"zone": "b", "tier": "gold"},
		},
	}

	got := Warnings(groups)
	want := map[int64][]Warning{
		4: {
			{AncestorID: 2, AncestorName: "top", Kind: WarningEnvironment},
			{AncestorID: 2, AncestorName: "top", Kind: WarningParameter, Key: "zone"},
		},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Warnings() = %+v, want %+v", got, want)
	}
}

func TestOverridesOf_NamesTheDescendantsOverridingAnAncestor(t *testing.T) {
	groups := []Group{
		rootGroup(),
		classGroup(2, "parent", 1, 50, "a"),
		classGroup(3, "zeta", 2, 10, "b"),
		classGroup(4, "alpha", 2, 20, "c"),
		classGroup(5, "agrees", 2, 30, "a"),
	}

	got := OverridesOf(groups, 2)
	if len(got) != 2 || got[0].GroupName != "alpha" || got[1].GroupName != "zeta" {
		t.Fatalf("OverridesOf() = %+v, want alpha then zeta", got)
	}
	if got[0].GroupID != 4 || len(got[0].Warnings) != 1 {
		t.Errorf("OverridesOf()[0] = %+v", got[0])
	}
	if other := OverridesOf(groups, 3); len(other) != 0 {
		t.Errorf("OverridesOf(a leaf) = %+v, want none", other)
	}
}
