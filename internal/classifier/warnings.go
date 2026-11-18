package classifier

import (
	"reflect"
	"sort"
)

// Kinds of value a group can override an ancestor on.
const (
	WarningClassParameter = "class_parameter"
	WarningParameter      = "parameter"
	WarningEnvironment    = "environment"
)

// Warning says that a group sets a value which conflicts with one set by
// an ancestor of higher priority. The group's value is the one used -
// a descendant always wins - although priority alone would have chosen
// the ancestor's, which is worth telling whoever set the priorities.
//
// It describes the groups' configuration, not any node: it is reported
// even if no node currently matches both groups.
type Warning struct {
	AncestorID   int64  `json:"ancestorId"`
	AncestorName string `json:"ancestorName"`
	// Kind is one of the Warning* constants.
	Kind string `json:"kind"`
	// Class is the class name, for WarningClassParameter.
	Class string `json:"class,omitempty"`
	// Key is the parameter name; empty for WarningEnvironment.
	Key string `json:"key,omitempty"`
}

// Override is a descendant that overrides a given ancestor, with the
// values concerned.
type Override struct {
	GroupID   int64     `json:"groupId"`
	GroupName string    `json:"groupName"`
	Warnings  []Warning `json:"warnings"`
}

// Warnings returns, for each group that has any, the values on which it
// overrides a higher-priority ancestor, nearest ancestor first. The
// root is never an ancestor that warns: it has no priority to override.
func Warnings(groups []Group) map[int64][]Warning {
	t := NewTree(groups)
	out := map[int64][]Warning{}
	for id, n := range t.nodes {
		if n.group.IsRoot || !n.reachable {
			continue
		}
		var ws []Warning
		for a := n.parent; a != nil; a = a.parent {
			if a.group.IsRoot || a.group.Priority <= n.group.Priority {
				continue
			}
			ws = append(ws, conflicts(n.group, a.group)...)
		}
		if len(ws) > 0 {
			out[id] = ws
		}
	}
	return out
}

// OverridesOf returns the descendants of the group identified by id that
// override it, ordered by name.
func OverridesOf(groups []Group, id int64) []Override {
	var out []Override
	byID := map[int64]Group{}
	for _, g := range groups {
		byID[g.ID] = g
	}
	for groupID, ws := range Warnings(groups) {
		var mine []Warning
		for _, w := range ws {
			if w.AncestorID == id {
				mine = append(mine, w)
			}
		}
		if len(mine) > 0 {
			out = append(out, Override{GroupID: groupID, GroupName: byID[groupID].Name, Warnings: mine})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].GroupName < out[j].GroupName })
	return out
}

// conflicts lists the values group sets differently from ancestor, in a
// stable order: environment, top-scope parameters, then class
// parameters, each sorted by name.
func conflicts(group, ancestor Group) []Warning {
	var ws []Warning
	base := Warning{AncestorID: ancestor.ID, AncestorName: ancestor.Name}

	if group.Environment != nil && ancestor.Environment != nil && *group.Environment != *ancestor.Environment {
		w := base
		w.Kind = WarningEnvironment
		ws = append(ws, w)
	}

	for _, key := range differingKeys(group.Parameters, ancestor.Parameters) {
		w := base
		w.Kind = WarningParameter
		w.Key = key
		ws = append(ws, w)
	}

	ancestorClasses := map[string]map[string]any{}
	for _, c := range ancestor.Classes {
		ancestorClasses[c.Name] = c.Parameters
	}
	classes := append([]Class(nil), group.Classes...)
	sort.Slice(classes, func(i, j int) bool { return classes[i].Name < classes[j].Name })
	for _, c := range classes {
		theirs, ok := ancestorClasses[c.Name]
		if !ok {
			continue
		}
		for _, key := range differingKeys(c.Parameters, theirs) {
			w := base
			w.Kind = WarningClassParameter
			w.Class = c.Name
			w.Key = key
			ws = append(ws, w)
		}
	}
	return ws
}

// differingKeys returns, sorted, the keys present in both maps with
// different values.
func differingKeys(mine, theirs map[string]any) []string {
	var keys []string
	for k, v := range mine {
		if other, ok := theirs[k]; ok && !reflect.DeepEqual(v, other) {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	return keys
}
