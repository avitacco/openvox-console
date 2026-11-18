package classcatalog

import (
	"sort"

	"github.com/voxpupuli/enterprise-console/internal/taskcatalog"
)

// Kinds of mismatch between a group's classes and its environment's code.
const (
	// WarningClassMissing: the environment's code defines no such class.
	WarningClassMissing = "class-missing"
	// WarningParameterUnknown: the class declares no such parameter.
	WarningParameterUnknown = "parameter-unknown"
	// WarningParameterType: the value does not fit the declared type.
	WarningParameterType = "parameter-type"
)

// Warning is one mismatch. It is advice: nothing is refused, altered or
// left out of a node's classification because of it.
type Warning struct {
	Kind      string `json:"kind"`
	Class     string `json:"class"`
	Parameter string `json:"parameter,omitempty"`
	// Detail says what was expected, for WarningParameterType.
	Detail string `json:"detail,omitempty"`
}

// Use is one class a group names, with the parameters it sets.
type Use struct {
	Name       string
	Parameters map[string]any
}

// Check compares uses with what env defines, in the order given, each
// class's parameters sorted by name.
//
// Nothing is reported from a stale answer: after a deploy it would name
// exactly the classes that were just added. And when any manifest failed
// to parse, a class that cannot be found may simply be in that manifest,
// so missing classes are not reported - the parse errors are shown
// instead.
func Check(env Environment, uses []Use) []Warning {
	warnings := []Warning{}
	if env.Stale {
		return warnings
	}
	for _, use := range uses {
		class, ok := env.Class(use.Name)
		if !ok {
			if len(env.Errors) == 0 {
				warnings = append(warnings, Warning{Kind: WarningClassMissing, Class: use.Name})
			}
			continue
		}
		declared := map[string]Parameter{}
		for _, p := range class.Parameters {
			declared[p.Name] = p
		}
		names := make([]string, 0, len(use.Parameters))
		for name := range use.Parameters {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			p, ok := declared[name]
			if !ok {
				warnings = append(warnings, Warning{Kind: WarningParameterUnknown, Class: use.Name, Parameter: name})
				continue
			}
			if p.Type == "" {
				continue
			}
			if why := taskcatalog.CheckValue(p.Type, use.Parameters[name]); why != "" {
				warnings = append(warnings, Warning{Kind: WarningParameterType, Class: use.Name, Parameter: name, Detail: why})
			}
		}
	}
	return warnings
}
