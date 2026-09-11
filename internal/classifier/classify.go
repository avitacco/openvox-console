package classifier

import "sort"

// Classification is the merged result of every group matching a node.
type Classification struct {
	// Classes maps class name to its parameters (empty map, not nil, for
	// a non-parameterized class).
	Classes     map[string]map[string]any
	Parameters  map[string]any
	Environment *string
}

// Classify determines every group in groups that matches certname/facts
// and merges their classes, parameters, and environment into one
// Classification. Conflicting values are resolved by each matching
// group's explicit Priority (higher wins); non-conflicting values from
// different groups are unioned. See design.md for why this uses explicit
// priority rather than group hierarchy.
func Classify(groups []Group, certname string, facts map[string]any) Classification {
	matched := make([]Group, 0, len(groups))
	for _, g := range groups {
		if Matches(g, certname, facts) {
			matched = append(matched, g)
		}
	}

	// Applying groups in ascending priority order and letting later
	// writes win via plain map assignment is exactly "highest priority
	// wins on conflict, everything else unions" - no separate conflict
	// detection needed. Priorities are unique (enforced at the store
	// layer), so there's never a tie to break.
	sort.Slice(matched, func(i, j int) bool { return matched[i].Priority < matched[j].Priority })

	result := Classification{
		Classes:    map[string]map[string]any{},
		Parameters: map[string]any{},
	}

	for _, g := range matched {
		for _, c := range g.Classes {
			params, ok := result.Classes[c.Name]
			if !ok {
				params = map[string]any{}
			}
			for k, v := range c.Parameters {
				params[k] = v
			}
			result.Classes[c.Name] = params
		}
		for k, v := range g.Parameters {
			result.Parameters[k] = v
		}
		if g.Environment != nil {
			result.Environment = g.Environment
		}
	}

	return result
}
