// Package classifier manages node groups - what classes, parameters, and
// environment apply to which nodes - and resolves, for a given node, the
// merged classification from every group that applies to it.
//
// Groups form a single tree under a built-in root, "All Nodes". A group
// matches a node only if its parent does, so a child's nodes are always
// a subset of its parent's. When matching groups set conflicting values,
// a descendant always wins over its ancestors, and explicit Priority
// decides between groups that are not ancestor and descendant - see
// Tree and design.md in the add-group-hierarchy change, which reversed
// the original flat, priority-only model.
package classifier

// Rule match modes: whether a group's Rule holds when all of its
// Conditions hold, or when any one of them does.
const (
	RuleMatchAll = "all"
	RuleMatchAny = "any"
)

// Condition is a single fact-based match predicate. A group's Rule is a
// flat list of Conditions combined by the group's RuleMatch mode; there
// is no nesting.
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
// to every node the group matches, via its Rule and/or explicit Pins,
// within the nodes its parent matches.
type Group struct {
	ID   int64  `json:"id,omitempty"`
	Name string `json:"name"`

	// ParentID is the group this one sits beneath. Nil only for the
	// root. A new group saved without one becomes a child of the root.
	ParentID *int64 `json:"parentId,omitempty"`

	// IsRoot marks the built-in "All Nodes" group: no parent, matches
	// every node, takes no Rule or Pins, and cannot be renamed, moved
	// or deleted. Set by the store, never by a request.
	IsRoot bool `json:"isRoot"`

	Environment *string `json:"environment,omitempty"`

	// Priority is unique among groups. The root has none (stored as
	// NULL, read as zero, and left out of API responses): it ranks
	// below every other group whatever this holds.
	Priority int `json:"priority"`

	Classes    []Class        `json:"classes"`
	Parameters map[string]any `json:"parameters"`

	// ConfigData is data openvoxserver reads with lookup(), through a
	// Hiera backend, ahead of the control repo's own (see
	// openvoxserver-init). It is nil in a request that does not send it,
	// which an update takes to mean "leave what is stored", and never nil
	// in anything read from the store. An empty map clears it.
	ConfigData map[string]any `json:"configData"`

	Rule []Condition `json:"rule"`
	// RuleMatch is RuleMatchAll or RuleMatchAny. Empty means all, which
	// is what every group did before the mode existed.
	RuleMatch string `json:"ruleMatch"`

	Pins []string `json:"pins"`
}
