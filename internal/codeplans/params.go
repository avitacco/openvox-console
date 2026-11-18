package codeplans

import (
	"encoding/json"
	"fmt"
	"strings"
)

// ParamError is a parameter a plan cannot be started with.
type ParamError struct {
	Name   string
	Reason string
}

func (e *ParamError) Error() string { return fmt.Sprintf("parameter %s %s", e.Name, e.Reason) }

// TargetSpec is a target parameter as given: node names, a node group, or a
// query. Exactly one is set.
type TargetSpec struct {
	Nodes []string
	Group *int64
	Query string
}

// ParseTargetSpec reads a target parameter's value: a node name, a list of
// them, or an object with a group id or a query.
func ParseTargetSpec(v any) (TargetSpec, error) {
	switch x := v.(type) {
	case string:
		if strings.TrimSpace(x) == "" {
			return TargetSpec{}, fmt.Errorf("name at least one node")
		}
		return TargetSpec{Nodes: []string{x}}, nil
	case []any:
		if len(x) == 0 {
			return TargetSpec{}, fmt.Errorf("name at least one node")
		}
		var out []string
		for _, e := range x {
			s, ok := e.(string)
			if !ok || strings.TrimSpace(s) == "" {
				return TargetSpec{}, fmt.Errorf("a list of targets holds node names")
			}
			out = append(out, s)
		}
		return TargetSpec{Nodes: out}, nil
	case map[string]any:
		group, hasGroup := x["group"]
		query, hasQuery := x["query"]
		if len(x) != 1 || hasGroup == hasQuery {
			return TargetSpec{}, fmt.Errorf(`give a node name, a list of names, {"group": id} or {"query": "..."}`)
		}
		if hasGroup {
			n, ok := group.(json.Number)
			id, err := n.Int64()
			if !ok || err != nil || id <= 0 {
				return TargetSpec{}, fmt.Errorf("a node group is its id")
			}
			return TargetSpec{Group: &id}, nil
		}
		q, ok := query.(string)
		if !ok || strings.TrimSpace(q) == "" {
			return TargetSpec{}, fmt.Errorf("a query is text")
		}
		return TargetSpec{Query: q}, nil
	}
	return TargetSpec{}, fmt.Errorf(`give a node name, a list of names, {"group": id} or {"query": "..."}`)
}

// Bind checks the parameters a plan is started with against its
// declarations and returns the variables its steps see. A parameter not
// given takes its default; one that is required and missing, one the plan
// does not declare, and one of the wrong type are refused, naming it. A
// Sensitive parameter is wrapped so that it cannot be written anywhere. A
// target parameter is left as given, to be resolved by the caller.
func (p *Plan) Bind(given map[string]any) (Env, error) {
	declared := map[string]bool{}
	for _, d := range p.Parameters {
		declared[d.Name] = true
	}
	for name := range given {
		if !declared[name] {
			return nil, &ParamError{Name: name, Reason: "is not a parameter of this plan"}
		}
	}
	env := Env{}
	for _, d := range p.Parameters {
		v, ok := given[d.Name]
		if !ok || v == nil {
			switch {
			case d.HasDefault:
				def, err := Eval(d.Default, env)
				if err != nil {
					return nil, &ParamError{Name: d.Name, Reason: "has a default that cannot be worked out: " + err.Error()}
				}
				v = def
			case d.Required:
				return nil, &ParamError{Name: d.Name, Reason: "is required"}
			default:
				v = nil
			}
		}
		if v != nil {
			if d.Target {
				if _, err := ParseTargetSpec(v); err != nil {
					return nil, &ParamError{Name: d.Name, Reason: "is not a valid target: " + err.Error()}
				}
			} else if why := d.Type.Check(v); why != "" {
				return nil, &ParamError{Name: d.Name, Reason: fmt.Sprintf("is not a %s: %s", d.TypeText, why)}
			}
		} else if !d.Type.AcceptsUndef() && !d.Target && !strings.HasPrefix(strings.TrimSpace(d.TypeText), "Optional[") {
			return nil, &ParamError{Name: d.Name, Reason: "is required"}
		}
		if d.Sensitive && v != nil {
			v = Sensitive{V: v}
		}
		env[d.Name] = v
	}
	return env, nil
}
