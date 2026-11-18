package classifier

import (
	"math"
	"sort"
)

// Tree is a set of groups arranged by parent, built once and then asked
// about any number of nodes. It is the only thing that knows how
// ancestry affects matching and precedence, so every caller - the ENC
// endpoint, node counts, a group's node list - gets the same answer.
//
// A group whose parent is missing from the set (or which names none) is
// treated as sitting directly under the root. That is what every group
// is after the hierarchy migration, and it keeps a partial set of
// groups meaningful rather than silently matching nothing. Groups
// caught in a parent loop, which the store refuses to create, are
// unreachable from the root and match no node.
type Tree struct {
	nodes map[int64]*treeNode
	// top holds the groups to start matching from: the root if there is
	// one, otherwise the groups with no parent in the set.
	top []*treeNode
}

type treeNode struct {
	group    Group
	parent   *treeNode
	children []*treeNode

	// depth is the distance from the root (0). A group with no parent
	// in the set counts as a child of the root (1).
	depth int
	// rank is the group's own priority, or its parent's rank if that
	// is higher; the root's is below every possible priority. Together
	// with depth and priority it gives the merge order - see mergeOrder.
	rank int
	// reachable is false for groups in a parent loop.
	reachable bool
}

// NewTree arranges groups by ParentID.
func NewTree(groups []Group) *Tree {
	t := &Tree{nodes: make(map[int64]*treeNode, len(groups))}

	var root *treeNode
	ordered := make([]*treeNode, 0, len(groups))
	for _, g := range groups {
		n := &treeNode{group: g}
		t.nodes[g.ID] = n
		ordered = append(ordered, n)
		if g.IsRoot && root == nil {
			root = n
		}
	}

	var orphans []*treeNode
	for _, n := range ordered {
		if n == root {
			continue
		}
		var parent *treeNode
		if n.group.ParentID != nil {
			parent = t.nodes[*n.group.ParentID]
		}
		switch {
		case parent != nil && parent != n:
			n.parent = parent
			parent.children = append(parent.children, n)
		case root != nil:
			n.parent = root
			root.children = append(root.children, n)
		default:
			orphans = append(orphans, n)
		}
	}

	if root != nil {
		root.rank = math.MinInt
		root.reachable = true
		t.top = []*treeNode{root}
		for _, c := range root.children {
			place(c, 1, math.MinInt)
		}
	} else {
		t.top = orphans
		for _, n := range orphans {
			place(n, 1, math.MinInt)
		}
	}
	return t
}

// place fills in depth and rank for n and everything beneath it.
func place(n *treeNode, depth, parentRank int) {
	n.depth = depth
	n.rank = n.group.Priority
	if parentRank > n.rank {
		n.rank = parentRank
	}
	n.reachable = true
	for _, c := range n.children {
		place(c, depth+1, n.rank)
	}
}

// Matching returns every group that applies to the node, in merge
// order: applying them in sequence and letting later values replace
// earlier ones gives the node's classification.
//
// A group applies only if its parent does, so a subtree is not looked
// at once the group above it has failed.
func (t *Tree) Matching(certname string, facts map[string]any) []Group {
	var matched []*treeNode
	var walk func(n *treeNode)
	walk = func(n *treeNode) {
		if !ownMatch(n.group, certname, facts) {
			return
		}
		matched = append(matched, n)
		for _, c := range n.children {
			walk(c)
		}
	}
	for _, n := range t.top {
		walk(n)
	}

	sort.SliceStable(matched, func(i, j int) bool { return mergeOrder(matched[i], matched[j]) })

	groups := make([]Group, len(matched))
	for i, n := range matched {
		groups[i] = n.group
	}
	return groups
}

// mergeOrder reports whether a is applied before b, i.e. loses to it.
//
// "A child always wins, priority otherwise" is not an ordering by
// itself: with a parent at priority 10, its child at 1 and an unrelated
// group at 5, the child beats the parent, the parent beats the
// unrelated group, and the unrelated group beats the child. Ranking a
// child no lower than its parent breaks that loop the same way every
// time: the child here ranks 10 and beats both.
//
// At equal rank the deeper group is applied later, which is what makes
// a descendant win over an ancestor it shares a rank with; at equal
// depth the unique priority decides, so the order is total.
func mergeOrder(a, b *treeNode) bool {
	if a.rank != b.rank {
		return a.rank < b.rank
	}
	if a.depth != b.depth {
		return a.depth < b.depth
	}
	return a.group.Priority < b.group.Priority
}

// Contains reports whether the group identified by id applies to the
// node: the group and every one of its ancestors must match it. False
// for an id not in the tree.
func (t *Tree) Contains(id int64, certname string, facts map[string]any) bool {
	n, ok := t.nodes[id]
	if !ok || !n.reachable {
		return false
	}
	for ; n != nil; n = n.parent {
		if !ownMatch(n.group, certname, facts) {
			return false
		}
	}
	return true
}

// Has reports whether a group with this id is in the tree.
func (t *Tree) Has(id int64) bool {
	_, ok := t.nodes[id]
	return ok
}

// Group returns the group with this id.
func (t *Tree) Group(id int64) (Group, bool) {
	n, ok := t.nodes[id]
	if !ok {
		return Group{}, false
	}
	return n.group, true
}

// Classify merges every group applying to the node into one
// Classification - see Classify.
func (t *Tree) Classify(certname string, facts map[string]any) Classification {
	result := Classification{
		Classes:    map[string]map[string]any{},
		Parameters: map[string]any{},
		ConfigData: map[string]any{},
	}

	for _, g := range t.Matching(certname, facts) {
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
		for k, v := range g.ConfigData {
			result.ConfigData[k] = v
		}
		if g.Environment != nil {
			result.Environment = g.Environment
		}
	}

	return result
}
