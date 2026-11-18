package codeplans

import (
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// Node is a compiled expression: what a step's targets, parameters and
// message, a parameter's default and a plan's return are made of. There is
// no code in it: a value, a reference to a variable, text with references
// in it, or a list or mapping of these.
type Node interface{ isNode() }

type literal struct{ V any }

// ref is $root, $root.key, $root[0] or $root['key'].
type ref struct {
	Root string
	Path []seg
	Text string
}

type seg struct {
	Key     string
	Index   int
	IsIndex bool
}

// interpolated is text with references: "restarting ${target.name}".
type interpolated struct{ Parts []Node }

type list []Node

type mapping []NamedNode

func (literal) isNode()      {}
func (ref) isNode()          {}
func (interpolated) isNode() {}
func (list) isNode()         {}
func (mapping) isNode()      {}

var (
	pathRoot = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*`)
	pathIdx  = regexp.MustCompile(`^\[(\d+)\]`)
	pathKey  = regexp.MustCompile(`^\[(?:'([^']*)'|"([^"]*)")\]`)
	pathDot  = regexp.MustCompile(`^\.([A-Za-z_][A-Za-z0-9_]*)`)
)

// parsePath reads root.key[0]['k'] and nothing after it.
func parsePath(s string) (ref, error) {
	root := pathRoot.FindString(s)
	if root == "" {
		return ref{}, fmt.Errorf("%q is not a reference: write a variable name after the $", s)
	}
	r := ref{Root: root, Text: s}
	rest := s[len(root):]
	for rest != "" {
		switch {
		case pathDot.MatchString(rest):
			m := pathDot.FindStringSubmatch(rest)
			r.Path = append(r.Path, seg{Key: m[1]})
			rest = rest[len(m[0]):]
		case pathIdx.MatchString(rest):
			m := pathIdx.FindStringSubmatch(rest)
			n, _ := strconv.Atoi(m[1])
			r.Path = append(r.Path, seg{Index: n, IsIndex: true})
			rest = rest[len(m[0]):]
		case pathKey.MatchString(rest):
			m := pathKey.FindStringSubmatch(rest)
			key := m[1]
			if key == "" {
				key = m[2]
			}
			r.Path = append(r.Path, seg{Key: key})
			rest = rest[len(m[0]):]
		default:
			return ref{}, fmt.Errorf("%q: only .name, [0] and ['name'] may follow a variable; plans have no operators or functions", "$"+s)
		}
	}
	return r, nil
}

// compile turns a decoded YAML value into a Node, checking every
// reference against the variables in scope.
func compile(v any, scope map[string]bool) (Node, error) {
	switch x := v.(type) {
	case nil, bool:
		return literal{V: x}, nil
	// Numbers are json.Number throughout, as task parameters are, so that
	// the type checker can tell 3 from 3.0.
	case int:
		return literal{V: json.Number(strconv.Itoa(x))}, nil
	case int64:
		return literal{V: json.Number(strconv.FormatInt(x, 10))}, nil
	case uint64:
		return literal{V: json.Number(strconv.FormatUint(x, 10))}, nil
	case json.Number:
		return literal{V: x}, nil
	case float64:
		return literal{V: json.Number(strconv.FormatFloat(x, 'f', -1, 64))}, nil
	case string:
		return compileString(x, scope)
	case []any:
		out := make(list, 0, len(x))
		for _, e := range x {
			n, err := compile(e, scope)
			if err != nil {
				return nil, err
			}
			out = append(out, n)
		}
		return out, nil
	case map[string]any:
		keys := make([]string, 0, len(x))
		for k := range x {
			keys = append(keys, k)
		}
		sortStrings(keys)
		out := make(mapping, 0, len(x))
		for _, k := range keys {
			n, err := compile(x[k], scope)
			if err != nil {
				return nil, err
			}
			out = append(out, NamedNode{Key: k, Node: n})
		}
		return out, nil
	}
	return nil, fmt.Errorf("a %T value is not supported (use text, numbers, booleans, lists and mappings)", v)
}

func checkScope(r ref, scope map[string]bool) error {
	if !scope[r.Root] {
		return fmt.Errorf("$%s is not a parameter or an earlier step", r.Root)
	}
	return nil
}

func compileString(s string, scope map[string]bool) (Node, error) {
	// A string that is exactly one reference keeps the referenced value's
	// type: a list stays a list.
	if strings.HasPrefix(s, "$") && !strings.HasPrefix(s, "$$") && !strings.HasPrefix(s, "${") {
		r, err := parsePath(s[1:])
		if err != nil {
			return nil, err
		}
		if err := checkScope(r, scope); err != nil {
			return nil, err
		}
		return r, nil
	}
	if !strings.Contains(s, "$") {
		return literal{V: s}, nil
	}
	var parts []Node
	var text strings.Builder
	flush := func() {
		if text.Len() > 0 {
			parts = append(parts, literal{V: text.String()})
			text.Reset()
		}
	}
	for i := 0; i < len(s); i++ {
		if s[i] != '$' {
			text.WriteByte(s[i])
			continue
		}
		switch {
		case i+1 < len(s) && s[i+1] == '$':
			text.WriteByte('$')
			i++
		case i+1 < len(s) && s[i+1] == '{':
			end := strings.IndexByte(s[i:], '}')
			if end < 0 {
				return nil, errors.New("a ${ is not closed")
			}
			r, err := parsePath(s[i+2 : i+end])
			if err != nil {
				return nil, err
			}
			if err := checkScope(r, scope); err != nil {
				return nil, err
			}
			flush()
			parts = append(parts, r)
			i += end
		default:
			return nil, fmt.Errorf("a $ in text must be written $$ for a dollar sign, or ${name} for a variable (in %q)", s)
		}
	}
	flush()
	if len(parts) == 1 {
		if l, ok := parts[0].(literal); ok {
			return l, nil
		}
	}
	return interpolated{Parts: parts}, nil
}

// Sensitive wraps a value that must not be written anywhere: it formats as
// a placeholder, and a task parameter is the only place it is unwrapped.
type Sensitive struct{ V any }

// Redacted is what a Sensitive value is shown as.
const Redacted = "[redacted]"

func (Sensitive) String() string { return Redacted }

// MarshalJSON writes the placeholder, so a Sensitive that reaches a record
// by mistake is still not the value.
func (Sensitive) MarshalJSON() ([]byte, error) { return json.Marshal(Redacted) }

// Env holds the variables a plan's expressions see: its parameters and the
// results of the steps run so far.
type Env map[string]any

// Eval computes a Node. Referencing a variable that is not set, or a key
// that is not there, is an error: a plan does not guess.
func Eval(n Node, env Env) (any, error) {
	switch x := n.(type) {
	case literal:
		return x.V, nil
	case ref:
		return evalRef(x, env)
	case interpolated:
		var b strings.Builder
		for _, p := range x.Parts {
			v, err := Eval(p, env)
			if err != nil {
				return nil, err
			}
			b.WriteString(text(v))
		}
		return b.String(), nil
	case list:
		out := make([]any, 0, len(x))
		for _, e := range x {
			v, err := Eval(e, env)
			if err != nil {
				return nil, err
			}
			out = append(out, v)
		}
		return out, nil
	case mapping:
		out := make(map[string]any, len(x))
		for _, e := range x {
			v, err := Eval(e.Node, env)
			if err != nil {
				return nil, err
			}
			out[e.Key] = v
		}
		return out, nil
	}
	return nil, errors.New("unknown expression")
}

func evalRef(r ref, env Env) (any, error) {
	v, ok := env[r.Root]
	if !ok {
		return nil, fmt.Errorf("$%s has no value yet", r.Root)
	}
	for _, s := range r.Path {
		if _, secret := v.(Sensitive); secret {
			return nil, fmt.Errorf("$%s is Sensitive: its parts cannot be used", r.Root)
		}
		switch c := v.(type) {
		case map[string]any:
			if s.IsIndex {
				return nil, fmt.Errorf("$%s: [%d] is for a list, and this is a mapping", r.Text, s.Index)
			}
			next, ok := c[s.Key]
			if !ok {
				return nil, fmt.Errorf("$%s: there is no %q", r.Text, s.Key)
			}
			v = next
		case []any:
			if !s.IsIndex {
				return nil, fmt.Errorf("$%s: .%s is for a mapping, and this is a list", r.Text, s.Key)
			}
			if s.Index >= len(c) {
				return nil, fmt.Errorf("$%s: the list has %d items", r.Text, len(c))
			}
			v = c[s.Index]
		default:
			return nil, fmt.Errorf("$%s: cannot look inside a value that is not a mapping or a list", r.Text)
		}
	}
	return v, nil
}

// text is a value as it appears inside text: a Sensitive value is its
// placeholder, a string is itself, anything else is its JSON.
func text(v any) string {
	switch x := v.(type) {
	case string:
		return x
	case Sensitive:
		return Redacted
	case nil:
		return ""
	case json.Number:
		return x.String()
	}
	b, err := json.Marshal(v)
	if err != nil {
		return fmt.Sprint(v)
	}
	return string(b)
}

// Unwrap returns v with every Sensitive in it replaced by its value, for
// building a task's parameters.
func Unwrap(v any) any {
	switch x := v.(type) {
	case Sensitive:
		return Unwrap(x.V)
	case map[string]any:
		out := make(map[string]any, len(x))
		for k, e := range x {
			out[k] = Unwrap(e)
		}
		return out
	case []any:
		out := make([]any, len(x))
		for i, e := range x {
			out[i] = Unwrap(e)
		}
		return out
	}
	return v
}

// Redact returns v with every Sensitive in it replaced by its placeholder,
// for anything that is stored or shown.
func Redact(v any) any {
	switch x := v.(type) {
	case Sensitive:
		return Redacted
	case map[string]any:
		out := make(map[string]any, len(x))
		for k, e := range x {
			out[k] = Redact(e)
		}
		return out
	case []any:
		out := make([]any, len(x))
		for i, e := range x {
			out[i] = Redact(e)
		}
		return out
	}
	return v
}

func sortStrings(s []string) { sort.Strings(s) }
