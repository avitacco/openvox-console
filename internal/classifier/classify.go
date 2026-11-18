package classifier

// Classification is the merged result of every group matching a node.
type Classification struct {
	// Classes maps class name to its parameters (empty map, not nil, for
	// a non-parameterized class).
	Classes    map[string]map[string]any
	Parameters map[string]any
	// ConfigData is the merged configuration data: each key from the
	// group that wins it, whole (maps are not merged key by key).
	ConfigData  map[string]any
	Environment *string
}

// Classify determines every group in groups that applies to
// certname/facts and merges their classes, parameters, and environment
// into one Classification. Non-conflicting values from different groups
// are unioned. Conflicting values are resolved by the hierarchy first -
// a descendant always wins over its ancestors - and by each group's
// explicit Priority otherwise; see mergeOrder for the exact rule.
//
// A caller classifying many nodes against the same groups should build
// one Tree with NewTree and call its Classify, rather than rebuild the
// tree per node here.
func Classify(groups []Group, certname string, facts map[string]any) Classification {
	return NewTree(groups).Classify(certname, facts)
}
