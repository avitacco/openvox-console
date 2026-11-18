package codeplans

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/voxpupuli/enterprise-console/internal/taskcatalog"
)

// Step kinds.
const (
	KindTask    = "task"
	KindPlan    = "plan"
	KindMessage = "message"
)

// UnusableError says why a plan file cannot be run, naming the step when
// it is a step's fault. It is what listing shows for an unusable plan.
type UnusableError struct {
	// Step counts from 1; zero is the plan as a whole.
	Step   int
	Name   string
	Reason string
}

func (e *UnusableError) Error() string {
	switch {
	case e.Step == 0:
		return e.Reason
	case e.Name != "":
		return fmt.Sprintf("step %d (%s): %s", e.Step, e.Name, e.Reason)
	}
	return fmt.Sprintf("step %d: %s", e.Step, e.Reason)
}

// Param is one declared parameter.
type Param struct {
	Name        string
	TypeText    string
	Type        *taskcatalog.ParamType
	Description string
	// Target is a TargetSpec: node names, a node group or a query.
	Target    bool
	Sensitive bool
	// Required parameters have no default and do not accept undef.
	Required   bool
	HasDefault bool
	Default    Node
}

// Step is one step of a plan.
type Step struct {
	// Index counts from 1.
	Index       int
	Name        string
	Description string
	Kind        string
	Task        string
	Plan        string
	Targets     Node
	// Parameters are a task's or a plan's parameters, in the order written.
	Parameters []NamedNode
	Message    Node
}

// NamedNode is an expression with the key it is under.
type NamedNode struct {
	Key  string
	Node Node
}

// Plan is a checked plan.
type Plan struct {
	Name        string
	Description string
	Parameters  []Param
	Steps       []Step
	Return      Node
	HasReturn   bool
	// SHA256 is the plan file's digest, recorded on a run.
	SHA256 string
}

var (
	identifier = regexp.MustCompile(`^[a-z_][a-z0-9_]*$`)
	// refText is a whole reference: $name, $name.field, $name[0], $name['k'].
	refPrefix = regexp.MustCompile(`^\$([A-Za-z_][A-Za-z0-9_]*)`)
)

// unsupported steps and keys, with what to say about each.
var unsupportedKeys = map[string]string{
	"command":   "commands are not supported: run a task instead",
	"script":    "scripts are not supported: run a task instead",
	"upload":    "uploads are not supported",
	"download":  "downloads are not supported",
	"resources": "applying resources needs the Puppet language, which this console does not run",
	"eval":      "eval needs the Puppet language, which this console does not run",
}

var stepKeys = map[string]bool{"name": true, "description": true, "task": true, "plan": true, "message": true, "targets": true, "parameters": true}

// Parse checks a plan file and returns the plan, or an *UnusableError.
func Parse(file File) (*Plan, error) {
	if file.Oversize {
		return nil, &UnusableError{Reason: fmt.Sprintf("the plan file is larger than %d KiB", MaxPlanFileBytes>>10)}
	}
	var root yaml.Node
	if err := yaml.NewDecoder(bytes.NewReader(file.Data)).Decode(&root); err != nil {
		if errors.Is(err, io.EOF) {
			return nil, &UnusableError{Reason: "the plan file is empty"}
		}
		return nil, &UnusableError{Reason: "the plan file is not valid YAML: " + firstLine(err.Error())}
	}
	if root.Kind != yaml.DocumentNode || len(root.Content) != 1 || root.Content[0].Kind != yaml.MappingNode {
		return nil, &UnusableError{Reason: "a plan is a YAML mapping with steps"}
	}
	top := root.Content[0]
	sum := sha256.Sum256(file.Data)
	plan := &Plan{Name: file.Name, SHA256: hex.EncodeToString(sum[:])}

	var paramsNode, stepsNode, returnNode *yaml.Node
	seen := map[string]bool{}
	for i := 0; i+1 < len(top.Content); i += 2 {
		key, val := top.Content[i].Value, top.Content[i+1]
		if seen[key] {
			return nil, &UnusableError{Reason: fmt.Sprintf("%q is given twice", key)}
		}
		seen[key] = true
		switch key {
		case "description":
			if val.Kind != yaml.ScalarNode {
				return nil, &UnusableError{Reason: "the description must be text"}
			}
			plan.Description = val.Value
		case "parameters":
			paramsNode = val
		case "steps":
			stepsNode = val
		case "return":
			returnNode = val
		default:
			return nil, &UnusableError{Reason: fmt.Sprintf("%q is not supported at the top of a plan (supported: description, parameters, steps, return)", key)}
		}
	}

	scope := map[string]bool{}
	if paramsNode != nil {
		if err := parseParams(plan, paramsNode, scope); err != nil {
			return nil, err
		}
	}
	if stepsNode == nil || stepsNode.Kind != yaml.SequenceNode || len(stepsNode.Content) == 0 {
		return nil, &UnusableError{Reason: "a plan needs a list of steps"}
	}
	for i, sn := range stepsNode.Content {
		step, err := parseStep(i+1, sn, scope)
		if err != nil {
			return nil, err
		}
		plan.Steps = append(plan.Steps, step)
		if step.Name != "" {
			scope[step.Name] = true
		}
	}
	if returnNode != nil {
		var v any
		if err := returnNode.Decode(&v); err != nil {
			return nil, &UnusableError{Reason: "return: " + firstLine(err.Error())}
		}
		n, err := compile(v, scope)
		if err != nil {
			return nil, &UnusableError{Reason: "return: " + err.Error()}
		}
		plan.Return, plan.HasReturn = n, true
	}
	return plan, nil
}

func firstLine(s string) string {
	line, _, _ := strings.Cut(s, "\n")
	return line
}

func parseParams(plan *Plan, n *yaml.Node, scope map[string]bool) error {
	if n.Kind != yaml.MappingNode {
		return &UnusableError{Reason: "parameters must be a mapping of names to their declarations"}
	}
	for i := 0; i+1 < len(n.Content); i += 2 {
		name, decl := n.Content[i].Value, n.Content[i+1]
		bad := func(format string, a ...any) error {
			return &UnusableError{Reason: fmt.Sprintf("parameter %s: %s", name, fmt.Sprintf(format, a...))}
		}
		if !identifier.MatchString(name) {
			return bad("a parameter name is lower case letters, digits and underscores")
		}
		if scope[name] {
			return bad("declared twice")
		}
		p := Param{Name: name, TypeText: "Any"}
		if decl.Kind != yaml.MappingNode && !(decl.Kind == yaml.ScalarNode && decl.Tag == "!!null") {
			return bad("declare it with type, default and description")
		}
		for j := 0; decl.Kind == yaml.MappingNode && j+1 < len(decl.Content); j += 2 {
			key, val := decl.Content[j].Value, decl.Content[j+1]
			switch key {
			case "type":
				if val.Kind != yaml.ScalarNode {
					return bad("the type must be text")
				}
				p.TypeText = val.Value
			case "description":
				p.Description = val.Value
			case "default":
				var v any
				if err := val.Decode(&v); err != nil {
					return bad("default: %s", firstLine(err.Error()))
				}
				node, err := compile(v, scope)
				if err != nil {
					return bad("default: %s", err.Error())
				}
				p.Default, p.HasDefault = node, true
			default:
				return bad("%q is not supported (supported: type, default, description)", key)
			}
		}
		t, err := taskcatalog.ParseParamType(p.TypeText)
		if err != nil {
			return bad("the type %q could not be read", p.TypeText)
		}
		p.Type = t
		p.Sensitive = t.IsSensitive()
		inner := strings.TrimSpace(p.TypeText)
		for strings.HasPrefix(inner, "Optional[") && strings.HasSuffix(inner, "]") {
			inner = strings.TrimSpace(inner[len("Optional[") : len(inner)-1])
		}
		p.Target = inner == "TargetSpec"
		p.Required = !p.HasDefault && (!t.AcceptsUndef() || p.Target) && !strings.HasPrefix(strings.TrimSpace(p.TypeText), "Optional[")
		plan.Parameters = append(plan.Parameters, p)
		scope[name] = true
	}
	return nil
}

func parseStep(index int, n *yaml.Node, scope map[string]bool) (Step, error) {
	s := Step{Index: index}
	fail := func(format string, a ...any) (Step, error) {
		return Step{}, &UnusableError{Step: index, Name: s.Name, Reason: fmt.Sprintf(format, a...)}
	}
	if n.Kind != yaml.MappingNode {
		return fail("a step is a mapping")
	}
	values := map[string]*yaml.Node{}
	for i := 0; i+1 < len(n.Content); i += 2 {
		key := n.Content[i].Value
		if _, dup := values[key]; dup {
			return fail("%q is given twice", key)
		}
		values[key] = n.Content[i+1]
	}
	if nn, ok := values["name"]; ok {
		s.Name = nn.Value
	}
	for key := range values {
		if why, no := unsupportedKeys[key]; no {
			return fail("%q: %s", key, why)
		}
		if !stepKeys[key] {
			return fail("%q is not supported in a step (supported: name, description, task, plan, message, targets, parameters)", key)
		}
	}
	if s.Name != "" {
		if !identifier.MatchString(s.Name) {
			return fail("a step name is lower case letters, digits and underscores, as it becomes a variable")
		}
		if scope[s.Name] {
			return fail("the name is already a parameter or an earlier step")
		}
	}
	if d, ok := values["description"]; ok {
		s.Description = d.Value
	}
	kinds := 0
	for _, k := range []string{KindTask, KindPlan, KindMessage} {
		if _, ok := values[k]; ok {
			kinds++
			s.Kind = k
		}
	}
	if kinds != 1 {
		return fail("a step does one thing: give exactly one of task, plan or message")
	}
	switch s.Kind {
	case KindTask:
		s.Task = values["task"].Value
		if values["task"].Kind != yaml.ScalarNode || s.Task == "" {
			return fail("task: name the task, as module::task")
		}
		tn, ok := values["targets"]
		if !ok {
			return fail("a task step needs targets")
		}
		node, err := decodeCompile(tn, scope)
		if err != nil {
			return fail("targets: %s", err.Error())
		}
		s.Targets = node
	case KindPlan:
		s.Plan = values["plan"].Value
		if values["plan"].Kind != yaml.ScalarNode || !planName.MatchString(s.Plan) {
			return fail("plan: name the plan, as module::plan")
		}
		if _, ok := values["targets"]; ok {
			return fail("a plan step passes its targets as a parameter, not as targets")
		}
	case KindMessage:
		node, err := decodeCompile(values["message"], scope)
		if err != nil {
			return fail("message: %s", err.Error())
		}
		s.Message = node
		if _, ok := values["targets"]; ok {
			return fail("a message has no targets")
		}
		if _, ok := values["parameters"]; ok {
			return fail("a message has no parameters")
		}
	}
	if pn, ok := values["parameters"]; ok {
		if pn.Kind != yaml.MappingNode {
			return fail("parameters must be a mapping")
		}
		for i := 0; i+1 < len(pn.Content); i += 2 {
			key := pn.Content[i].Value
			node, err := decodeCompile(pn.Content[i+1], scope)
			if err != nil {
				return fail("parameter %s: %s", key, err.Error())
			}
			s.Parameters = append(s.Parameters, NamedNode{Key: key, Node: node})
		}
	}
	return s, nil
}

func decodeCompile(n *yaml.Node, scope map[string]bool) (Node, error) {
	var v any
	if err := n.Decode(&v); err != nil {
		return nil, errors.New(firstLine(err.Error()))
	}
	return compile(v, scope)
}
