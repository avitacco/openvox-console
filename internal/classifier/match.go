package classifier

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// Matches reports whether group g applies to a node named certname with
// the given facts (a decoded JSON object: fact name -> value, where a
// structured fact's value is itself a nested map).
//
// A node matches if it is explicitly pinned to g, or if g has at least
// one rule condition and every condition is satisfied (AND-only - see
// design.md). A group with neither pins nor rule conditions matches no
// node - an empty rule is not treated as "match everyone," since that
// would make an accidentally-incomplete group dangerous by default.
func Matches(g Group, certname string, facts map[string]any) bool {
	for _, pinned := range g.Pins {
		if pinned == certname {
			return true
		}
	}

	if len(g.Rule) == 0 {
		return false
	}
	for _, cond := range g.Rule {
		if !cond.matches(facts) {
			return false
		}
	}
	return true
}

func (c Condition) matches(facts map[string]any) bool {
	value, ok := lookupFactPath(facts, c.FactPath)

	switch c.Operator {
	case "=":
		return ok && factEquals(value, c.Value)
	case "!=":
		return !ok || !factEquals(value, c.Value)
	case "~":
		if !ok {
			return false
		}
		re, err := regexp.Compile(c.Value)
		if err != nil {
			return false
		}
		return re.MatchString(fmt.Sprint(value))
	case ">", "<", ">=", "<=":
		if !ok {
			return false
		}
		return factCompare(value, c.Value, c.Operator)
	default:
		return false
	}
}

// lookupFactPath navigates a dotted path (e.g. "os.family") through
// nested fact maps.
func lookupFactPath(facts map[string]any, path string) (any, bool) {
	var cur any = facts
	for _, part := range strings.Split(path, ".") {
		m, ok := cur.(map[string]any)
		if !ok {
			return nil, false
		}
		cur, ok = m[part]
		if !ok {
			return nil, false
		}
	}
	return cur, true
}

func factEquals(factValue any, ruleValue string) bool {
	return fmt.Sprint(factValue) == ruleValue
}

func factCompare(factValue any, ruleValue, op string) bool {
	a, aOK := toFloat(factValue)
	b, err := strconv.ParseFloat(ruleValue, 64)
	if !aOK || err != nil {
		return false
	}
	switch op {
	case ">":
		return a > b
	case "<":
		return a < b
	case ">=":
		return a >= b
	case "<=":
		return a <= b
	}
	return false
}

func toFloat(v any) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case int:
		return float64(n), true
	case string:
		f, err := strconv.ParseFloat(n, 64)
		return f, err == nil
	default:
		return 0, false
	}
}
