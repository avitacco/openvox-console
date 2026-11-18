package taskcatalog

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// Problem is one reason a parameter set was rejected.
type Problem struct {
	Param   string `json:"param"`
	Message string `json:"message"`
}

// ValidationError lists every problem found with a parameter set, sorted
// by parameter, so a form can mark each field at once rather than one
// round-trip per mistake.
type ValidationError struct {
	Task     string
	Problems []Problem
}

func (e *ValidationError) Error() string {
	parts := make([]string, len(e.Problems))
	for i, p := range e.Problems {
		parts[i] = p.Param + ": " + p.Message
	}
	return fmt.Sprintf("invalid parameters for %s: %s", e.Task, strings.Join(parts, "; "))
}

// decodeParams decodes a parameter object, keeping numbers exact so
// Integer and Float can be told apart.
func decodeParams(raw json.RawMessage) (map[string]any, error) {
	params := map[string]any{}
	if len(bytes.TrimSpace(raw)) == 0 || string(bytes.TrimSpace(raw)) == "null" {
		return params, nil
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	if err := dec.Decode(&params); err != nil {
		return nil, fmt.Errorf("parameters must be a JSON object: %w", err)
	}
	if params == nil {
		params = map[string]any{}
	}
	return params, nil
}

// Validate checks params against task's metadata and returns them with
// declared defaults filled in for omitted parameters, as Bolt does. The
// error is a *ValidationError when the parameters are wrong, and a plain
// error when the metadata's own types cannot be parsed.
//
// Names starting with "_" are Puppet's metaparameters, which this console
// sets itself (_task, _installdir) or does not support, so a caller may
// never supply one.
func Validate(task Task, raw json.RawMessage) (map[string]any, error) {
	params, err := decodeParams(raw)
	if err != nil {
		return nil, &ValidationError{Task: task.Name, Problems: []Problem{{Param: "(parameters)", Message: err.Error()}}}
	}
	var problems []Problem
	for name := range params {
		if strings.HasPrefix(name, "_") {
			problems = append(problems, Problem{Param: name, Message: "metaparameters cannot be set"})
		}
	}

	// No "parameters" key at all: Puppet accepts anything.
	if task.Metadata.Parameters != nil {
		for name := range params {
			if _, declared := task.Metadata.Parameters[name]; !declared && !strings.HasPrefix(name, "_") {
				problems = append(problems, Problem{Param: name, Message: "not a parameter of this task"})
			}
		}
		for name, decl := range task.Metadata.Parameters {
			typ, err := parseType(decl.Type)
			if err != nil {
				return nil, fmt.Errorf("task %s, parameter %s: %w", task.Name, name, err)
			}
			v, given := params[name]
			if !given {
				if len(decl.Default) > 0 {
					def, err := decodeValue(decl.Default)
					if err != nil {
						return nil, fmt.Errorf("task %s, parameter %s: bad default: %w", task.Name, name, err)
					}
					params[name] = def
				} else if !typ.acceptsUndef() {
					problems = append(problems, Problem{Param: name, Message: "required"})
				}
				continue
			}
			if why := typ.check(v); why != "" {
				problems = append(problems, Problem{Param: name, Message: why})
			}
		}
	}

	if len(problems) > 0 {
		sort.Slice(problems, func(i, j int) bool { return problems[i].Param < problems[j].Param })
		return nil, &ValidationError{Task: task.Name, Problems: problems}
	}
	return params, nil
}

func decodeValue(raw json.RawMessage) (any, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var v any
	err := dec.Decode(&v)
	return v, err
}

// ParamInput is how a form should collect one parameter, summarised from
// its type (design decision 11). The server stays authoritative: this
// only chooses a widget.
type ParamInput struct {
	// Kind is "boolean", "integer", "number", "enum", "string" or "json".
	Kind     string   `json:"kind"`
	Options  []string `json:"options,omitempty"` // for "enum"
	Required bool     `json:"required"`
}

// InputFor summarises param's type for a form. An unparseable type gets
// a JSON input, so the form still works and the server reports the
// problem.
func InputFor(param Parameter) ParamInput {
	typ, err := parseType(param.Type)
	if err != nil {
		return ParamInput{Kind: "json"}
	}
	in := ParamInput{Required: len(param.Default) == 0 && !typ.acceptsUndef()}
	// Look through Optional/NotUndef to the type that shapes the value.
	for (typ.name == "Optional" || typ.name == "NotUndef") && len(typ.args) > 0 && typ.args[0].typ != nil {
		typ = typ.args[0].typ
	}
	switch typ.name {
	case "Boolean":
		in.Kind = "boolean"
	case "Integer":
		in.Kind = "integer"
	case "Float", "Numeric":
		in.Kind = "number"
	case "Enum":
		in.Kind = "enum"
		for _, a := range typ.args {
			if a.str != nil {
				in.Options = append(in.Options, *a.str)
			}
		}
	case "String", "Pattern":
		in.Kind = "string"
	default:
		in.Kind = "json"
	}
	return in
}
