package taskscope

import (
	"context"
	"strings"
	"testing"

	"github.com/voxpupuli/enterprise-console/internal/nodescope"
)

func TestValidatePatterns(t *testing.T) {
	ok := [][]string{
		nil, {}, {"*"}, {"patch::*"}, {"package::install"}, {"patch"},
		{"a", "b::c", "d::*"}, {"module_2::task_9"},
	}
	for _, p := range ok {
		if err := ValidatePatterns(p); err != nil {
			t.Errorf("%v refused: %v", p, err)
		}
	}
	bad := map[string][]string{
		"wildcard in the middle": {"pa*ch"},
		"bare module wildcard":   {"::*"},
		"empty":                  {""},
		"upper case":             {"Patch::apply"},
		"two wildcards":          {"patch::*::x"},
		"prefix wildcard":        {"patch*"},
		"three parts":            {"a::b::c"},
		"leading digit":          {"1patch::apply"},
		"space":                  {"patch::apply "},
		"too long":               {strings.Repeat("a", MaxPatternLength+1)},
	}
	for name, p := range bad {
		if err := ValidatePatterns(p); err == nil {
			t.Errorf("%s: %v accepted", name, p)
		}
	}
	many := make([]string, MaxPatterns+1)
	for i := range many {
		many[i] = "a::b"
	}
	if err := ValidatePatterns(many); err == nil {
		t.Error("more than the limit accepted")
	}
	if err := ValidatePatterns(many[:MaxPatterns]); err != nil {
		t.Errorf("exactly the limit refused: %v", err)
	}
	if err := ValidatePatterns([]string{strings.Repeat("a", MaxPatternLength)}); err != nil {
		t.Errorf("a pattern of exactly the limit refused: %v", err)
	}
}

func TestMatches(t *testing.T) {
	cases := []struct {
		pattern, task string
		want          bool
	}{
		{"patch::*", "patch::apply", true},
		{"patch::*", "patch::reboot", true},
		{"patch::*", "patch", true}, // the module's own task
		{"patch::*", "patching::apply", false},
		{"patch::*", "patch2::apply", false},
		{"patch::*", "Patch::apply", false},
		{"patch::*", "xpatch::apply", false},
		{"package::install", "package::install", true},
		{"package::install", "package::upgrade", false},
		{"package::install", "package", false},
		{"patch", "patch", true},
		{"patch", "patch::apply", false},
		{"*", "anything::at_all", true},
	}
	for _, c := range cases {
		if got := Matches(c.pattern, c.task); got != c.want {
			t.Errorf("Matches(%q, %q) = %v, want %v", c.pattern, c.task, got, c.want)
		}
	}
	if !Allows(nil, "x::y") || !Allows([]string{}, "x::y") {
		t.Error("an empty list does not allow every task")
	}
	if Allows([]string{"a::*", "b::c"}, "b::d") {
		t.Error("a list allowed a task none of its patterns match")
	}
	if got := Unique([]string{"a", "b", "a"}); len(got) != 2 || got[0] != "a" {
		t.Errorf("Unique = %v", got)
	}
}

// groupNodes is a fixed map of node group id to nodes.
type groupNodes map[int64][]string

func (g groupNodes) For(_ context.Context, ids []int64) (nodescope.Scope, error) {
	var all []string
	for _, id := range ids {
		all = append(all, g[id]...)
	}
	return nodescope.Of(all...), nil
}

func resolve(t *testing.T, grants []Grant) Scope {
	t.Helper()
	s, err := Resolve(context.Background(), grants, groupNodes{1: {"web1", "web2"}, 2: {"db1"}, 3: nil})
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func nodes(s nodescope.Scope) string { return strings.Join(s.Certnames(), ",") }

// The case the pairing exists for: one role allows patching on the web
// tier, another allows everything on the database tier. Together they
// must not allow anything but patching on the web tier.
func TestScope_PairsPatternsWithTheirOwnGroups(t *testing.T) {
	s := resolve(t, []Grant{
		{Tasks: []string{"patch::*"}, Groups: []int64{1}},
		{Tasks: []string{Everything}, Groups: []int64{2}},
	})
	if got := nodes(s.NodesFor("patch::apply")); got != "db1,web1,web2" {
		t.Errorf("patch::apply on %q, want web and database", got)
	}
	if got := nodes(s.NodesFor("package::install")); got != "db1" {
		t.Errorf("package::install on %q, want the database tier only", got)
	}
	if !s.Allows("package::install") {
		t.Error("a task one grant allows is not allowed")
	}
}

func TestScope_EdgeCases(t *testing.T) {
	// A role allowing every task, with an empty list, is {"*", groups}.
	s := resolve(t, []Grant{{Tasks: []string{Everything}, Groups: []int64{1}}})
	if got := nodes(s.NodesFor("any::task")); got != "web1,web2" {
		t.Errorf("got %q", got)
	}

	// A role with no node groups allows a task on no node, though the
	// task itself is allowed by name.
	s = resolve(t, []Grant{{Tasks: []string{Everything}, Groups: []int64{}}})
	if !s.Allows("any::task") {
		t.Error("a grant with no groups did not allow the task by name")
	}
	if got := s.NodesFor("any::task"); got.All() || len(got.Certnames()) != 0 {
		t.Errorf("a grant with no groups allows nodes %q", nodes(got))
	}

	// No grants at all (restricted, but nothing granted) allows nothing.
	s = resolve(t, []Grant{})
	if s.Allows("any::task") || !s.Restricted() {
		t.Error("an empty list of grants allowed a task")
	}

	// A nil list is unrestricted.
	s = resolve(t, nil)
	if !s.Allows("any::task") || s.Restricted() || !s.NodesFor("any::task").All() {
		t.Error("a nil list of grants is not unrestricted")
	}

	// A fleet-wide grant (a service token) covers every node.
	s = resolve(t, []Grant{{Tasks: []string{"deploy::*"}, Fleet: true}})
	if !s.NodesFor("deploy::app").All() {
		t.Error("a fleet grant did not cover every node")
	}
	if s.Allows("package::install") {
		t.Error("a deploy-only token allowed another task")
	}

	// The zero Scope allows nothing.
	var zero Scope
	if zero.Allows("x") || !zero.Restricted() {
		t.Error("the zero scope allowed a task")
	}

	// Two grants for the same task union their groups.
	s = resolve(t, []Grant{
		{Tasks: []string{"a::*"}, Groups: []int64{1}},
		{Tasks: []string{"a::b"}, Groups: []int64{2}},
	})
	if got := nodes(s.NodesFor("a::b")); got != "db1,web1,web2" {
		t.Errorf("got %q", got)
	}
}
