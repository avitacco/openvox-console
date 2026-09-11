// Package classifier manages node groups - what classes, parameters, and
// environment apply to which nodes - and resolves, for a given node, the
// merged classification from every group that applies to it.
//
// Groups are flat, not hierarchical: precedence between conflicting
// matching groups is decided entirely by each group's explicit, unique
// Priority. See design.md in the change that introduced this package for
// why (a deliberate simplification of Puppet Enterprise's group
// inheritance model).
package classifier

// Condition is a single fact-based match predicate. A group's Rule is the
// AND of all its Conditions - see design.md for why there's no OR/nesting.
type Condition struct {
	FactPath string `json:"factPath"` // dotted path into a node's facts, e.g. "os.family"
	Operator string `json:"operator"` // one of: = != ~ > < >= <=
	Value    string `json:"value"`
}

// Class is a class to apply, with its parameters (may be empty for a
// non-parameterized class).
type Class struct {
	Name       string         `json:"name"`
	Parameters map[string]any `json:"parameters"`
}

// Group is a node group: a set of classes/parameters/environment to apply
// to every node the group matches, via its Rule and/or explicit Pins.
type Group struct {
	ID          int64          `json:"id,omitempty"`
	Name        string         `json:"name"`
	Environment *string        `json:"environment,omitempty"`
	Priority    int            `json:"priority"`
	Classes     []Class        `json:"classes"`
	Parameters  map[string]any `json:"parameters"`
	Rule        []Condition    `json:"rule"`
	Pins        []string       `json:"pins"`
}
