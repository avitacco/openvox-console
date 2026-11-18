package taskcatalog

import (
	"encoding/json"
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
)

// typeExpr is a parsed Puppet type expression. Only the types that occur
// in practice in task metadata are understood (design decision 11); any
// other name parses as unchecked, which accepts every value, so an
// unfamiliar type is left for the task itself to check rather than
// causing a request Puppet would have accepted to be rejected.
type typeExpr struct {
	name string // canonical name, or "" for unchecked
	args []typeArg
	raw  string // the expression as written, for messages
}

// typeArg is one bracketed argument: a nested type, a string, a number,
// a regular expression, a boolean, or the keyword `default`.
type typeArg struct {
	typ       *typeExpr
	str       *string
	num       *float64
	re        *regexp.Regexp
	boolean   *bool
	isDefault bool
}

var knownTypes = map[string]bool{
	"Any": true, "Data": true, "Scalar": true, "ScalarData": true, "Undef": true,
	"NotUndef": true, "Optional": true, "Variant": true,
	"String": true, "Pattern": true, "Enum": true,
	"Integer": true, "Float": true, "Numeric": true, "Boolean": true,
	"Array": true, "Hash": true,
	// A Sensitive value arrives as its plain inner value - from a task
	// request or from a node group's class parameters - so it is
	// checked against the wrapped type.
	"Sensitive": true,
}

// parseType parses a type expression. An empty expression is Any. A
// syntax error is reported, since it means the metadata itself is broken.
func parseType(s string) (*typeExpr, error) {
	if strings.TrimSpace(s) == "" {
		return &typeExpr{name: "Any", raw: "Any"}, nil
	}
	p := &typeParser{src: s}
	t, err := p.parseType()
	if err != nil {
		return nil, fmt.Errorf("parse type %q: %w", s, err)
	}
	p.skipSpace()
	if p.pos != len(p.src) {
		return nil, fmt.Errorf("parse type %q: unexpected %q", s, p.src[p.pos:])
	}
	return t, nil
}

type typeParser struct {
	src string
	pos int
}

func (p *typeParser) skipSpace() {
	for p.pos < len(p.src) && strings.ContainsRune(" \t\r\n", rune(p.src[p.pos])) {
		p.pos++
	}
}

func (p *typeParser) peek() byte {
	p.skipSpace()
	if p.pos >= len(p.src) {
		return 0
	}
	return p.src[p.pos]
}

func (p *typeParser) parseType() (*typeExpr, error) {
	p.skipSpace()
	start := p.pos
	for p.pos < len(p.src) {
		c := p.src[p.pos]
		if c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '_' || c == ':' {
			p.pos++
			continue
		}
		break
	}
	name := p.src[start:p.pos]
	if name == "" || name[0] < 'A' || name[0] > 'Z' {
		return nil, fmt.Errorf("expected a type name at %d", start)
	}
	t := &typeExpr{name: name}
	if !knownTypes[name] {
		// Unchecked: its arguments (Struct[{a => String}], Tuple[...],
		// an alias's own) are skipped rather than parsed, so no type
		// this parser does not model can make a task unusable.
		t.name = ""
		if p.peek() == '[' {
			if err := p.skipBalanced(); err != nil {
				return nil, err
			}
		}
		t.raw = strings.TrimSpace(p.src[start:p.pos])
		return t, nil
	}
	if p.peek() == '[' {
		p.pos++
		for {
			if p.peek() == ']' {
				p.pos++
				break
			}
			arg, err := p.parseArg()
			if err != nil {
				return nil, err
			}
			t.args = append(t.args, arg)
			switch p.peek() {
			case ',':
				p.pos++
				continue
			case ']':
				p.pos++
			default:
				return nil, fmt.Errorf("expected , or ] at %d", p.pos)
			}
			break
		}
	}
	t.raw = strings.TrimSpace(p.src[start:p.pos])
	return t, nil
}

// skipBalanced moves past a bracketed block, honouring nesting, quotes
// and regular expressions.
func (p *typeParser) skipBalanced() error {
	depth := 0
	for p.pos < len(p.src) {
		c := p.src[p.pos]
		switch c {
		case '[', '{', '(':
			depth++
		case ']', '}', ')':
			depth--
		case '\'', '"':
			if _, err := p.parseString(c); err != nil {
				return err
			}
			continue
		}
		p.pos++
		if depth == 0 {
			return nil
		}
	}
	return fmt.Errorf("unbalanced brackets")
}

func (p *typeParser) parseArg() (typeArg, error) {
	c := p.peek()
	switch {
	case c == '\'' || c == '"':
		s, err := p.parseString(c)
		return typeArg{str: &s}, err
	case c == '/':
		return p.parseRegexp()
	case c == '-' || c >= '0' && c <= '9':
		start := p.pos
		p.pos++
		for p.pos < len(p.src) && strings.ContainsRune("0123456789.eE+-_", rune(p.src[p.pos])) {
			p.pos++
		}
		n, err := strconv.ParseFloat(strings.ReplaceAll(p.src[start:p.pos], "_", ""), 64)
		if err != nil {
			return typeArg{}, fmt.Errorf("bad number %q", p.src[start:p.pos])
		}
		return typeArg{num: &n}, nil
	case c >= 'a' && c <= 'z':
		start := p.pos
		for p.pos < len(p.src) && (p.src[p.pos] >= 'a' && p.src[p.pos] <= 'z') {
			p.pos++
		}
		for p.pos < len(p.src) && (p.src[p.pos] >= 'a' && p.src[p.pos] <= 'z' || p.src[p.pos] >= '0' && p.src[p.pos] <= '9' || p.src[p.pos] == '_') {
			p.pos++
		}
		switch word := p.src[start:p.pos]; word {
		case "default":
			return typeArg{isDefault: true}, nil
		case "true", "false":
			b := word == "true"
			return typeArg{boolean: &b}, nil
		default:
			// A bare word is a string, as in Enum[start, stop].
			return typeArg{str: &word}, nil
		}
	default:
		t, err := p.parseType()
		return typeArg{typ: t}, err
	}
}

func (p *typeParser) parseString(quote byte) (string, error) {
	p.pos++ // opening quote
	var b strings.Builder
	for p.pos < len(p.src) {
		c := p.src[p.pos]
		switch {
		case c == '\\' && p.pos+1 < len(p.src):
			b.WriteByte(p.src[p.pos+1])
			p.pos += 2
		case c == quote:
			p.pos++
			return b.String(), nil
		default:
			b.WriteByte(c)
			p.pos++
		}
	}
	return "", fmt.Errorf("unterminated string")
}

func (p *typeParser) parseRegexp() (typeArg, error) {
	p.pos++ // opening slash
	var b strings.Builder
	for p.pos < len(p.src) {
		c := p.src[p.pos]
		switch {
		case c == '\\' && p.pos+1 < len(p.src) && p.src[p.pos+1] == '/':
			b.WriteByte('/')
			p.pos += 2
		case c == '/':
			p.pos++
			re, err := regexp.Compile(b.String())
			if err != nil {
				return typeArg{}, fmt.Errorf("bad pattern /%s/: %w", b.String(), err)
			}
			return typeArg{re: re}, nil
		default:
			b.WriteByte(c)
			p.pos++
		}
	}
	return typeArg{}, fmt.Errorf("unterminated pattern")
}

// acceptsUndef reports whether a parameter of this type may be omitted
// when it has no default.
func (t *typeExpr) acceptsUndef() bool {
	switch t.name {
	case "", "Any", "Data", "Undef", "Optional":
		return true
	case "Variant":
		for _, a := range t.args {
			if a.typ != nil && a.typ.acceptsUndef() {
				return true
			}
		}
	case "Sensitive":
		return len(t.args) == 0 || t.args[0].typ == nil || t.args[0].typ.acceptsUndef()
	}
	return false
}

// bounds reads the optional min/max numeric arguments starting at index
// from, with `default` meaning unbounded.
func (t *typeExpr) bounds(from int) (lo, hi float64) {
	lo, hi = math.Inf(-1), math.Inf(1)
	if len(t.args) > from && t.args[from].num != nil {
		lo = *t.args[from].num
	}
	if len(t.args) > from+1 && t.args[from+1].num != nil {
		hi = *t.args[from+1].num
	}
	return lo, hi
}

// sizeBounds is bounds for a size: the lower bound defaults to 0.
func (t *typeExpr) sizeBounds(from int) (lo, hi float64) {
	lo, hi = t.bounds(from)
	if math.IsInf(lo, -1) {
		lo = 0
	}
	return lo, hi
}

// check reports why v (decoded with json.Decoder.UseNumber) does not
// match t, or "" when it does.
func (t *typeExpr) check(v any) string {
	mismatch := "expected " + t.raw
	switch t.name {
	case "", "Any":
		return ""
	case "Undef":
		if v != nil {
			return mismatch
		}
	case "Optional":
		if v == nil || len(t.args) == 0 || t.args[0].typ == nil {
			return ""
		}
		// Keep the inner type's detail ("got 99") but name the type as
		// declared, Optional and all.
		inner := t.args[0].typ
		why := inner.check(v)
		if rest, ok := strings.CutPrefix(why, "expected "+inner.raw); ok {
			return mismatch + rest
		}
		return why
	case "Sensitive":
		if len(t.args) == 0 || t.args[0].typ == nil {
			return ""
		}
		inner := t.args[0].typ
		why := inner.check(v)
		if rest, ok := strings.CutPrefix(why, "expected "+inner.raw); ok {
			return mismatch + rest
		}
		return why
	case "NotUndef":
		if v == nil {
			return mismatch
		}
		if len(t.args) > 0 && t.args[0].typ != nil && t.args[0].typ.check(v) != "" {
			return mismatch
		}
	case "Variant":
		for _, a := range t.args {
			if a.typ != nil && a.typ.check(v) == "" {
				return ""
			}
		}
		return mismatch
	case "Data":
		if !isData(v) {
			return mismatch
		}
	case "Scalar", "ScalarData":
		switch v.(type) {
		case string, json.Number, bool:
		default:
			return mismatch
		}
	case "Boolean":
		if _, ok := v.(bool); !ok {
			return mismatch
		}
	case "String":
		s, ok := v.(string)
		if !ok {
			return mismatch
		}
		lo, hi := t.sizeBounds(0)
		if n := float64(len([]rune(s))); n < lo || n > hi {
			return fmt.Sprintf("expected %s, got a string of length %d", t.raw, len([]rune(s)))
		}
	case "Enum":
		s, ok := v.(string)
		if !ok {
			return mismatch
		}
		fold := len(t.args) > 0 && t.args[len(t.args)-1].boolean != nil && *t.args[len(t.args)-1].boolean
		for _, a := range t.args {
			if a.str != nil && (*a.str == s || fold && strings.EqualFold(*a.str, s)) {
				return ""
			}
		}
		return mismatch
	case "Pattern":
		s, ok := v.(string)
		if !ok {
			return mismatch
		}
		for _, a := range t.args {
			if a.re != nil && a.re.MatchString(s) || a.str != nil && strings.Contains(s, *a.str) {
				return ""
			}
		}
		return mismatch
	case "Integer", "Float", "Numeric":
		n, ok := v.(json.Number)
		if !ok {
			return mismatch
		}
		isInt := !strings.ContainsAny(string(n), ".eE")
		if t.name == "Integer" && !isInt || t.name == "Float" && isInt {
			return mismatch
		}
		f, err := n.Float64()
		if err != nil {
			return mismatch
		}
		if lo, hi := t.bounds(0); f < lo || f > hi {
			return fmt.Sprintf("expected %s, got %s", t.raw, n)
		}
	case "Array":
		arr, ok := v.([]any)
		if !ok {
			return mismatch
		}
		elem, sizeFrom := (*typeExpr)(nil), 0
		if len(t.args) > 0 && t.args[0].typ != nil {
			elem, sizeFrom = t.args[0].typ, 1
		}
		if lo, hi := t.sizeBounds(sizeFrom); float64(len(arr)) < lo || float64(len(arr)) > hi {
			return fmt.Sprintf("expected %s, got %d elements", t.raw, len(arr))
		}
		if elem != nil {
			for i, e := range arr {
				if why := elem.check(e); why != "" {
					return fmt.Sprintf("element %d: %s", i, why)
				}
			}
		}
	case "Hash":
		h, ok := v.(map[string]any)
		if !ok {
			return mismatch
		}
		var keyT, valT *typeExpr
		sizeFrom := 0
		if len(t.args) >= 2 && t.args[0].typ != nil && t.args[1].typ != nil {
			keyT, valT, sizeFrom = t.args[0].typ, t.args[1].typ, 2
		}
		if lo, hi := t.sizeBounds(sizeFrom); float64(len(h)) < lo || float64(len(h)) > hi {
			return fmt.Sprintf("expected %s, got %d entries", t.raw, len(h))
		}
		for k, e := range h {
			if keyT != nil {
				if why := keyT.check(k); why != "" {
					return fmt.Sprintf("key %q: %s", k, why)
				}
			}
			if valT != nil {
				if why := valT.check(e); why != "" {
					return fmt.Sprintf("value of %q: %s", k, why)
				}
			}
		}
	}
	return ""
}

func isData(v any) bool {
	switch v := v.(type) {
	case nil, string, json.Number, bool:
		return true
	case []any:
		for _, e := range v {
			if !isData(e) {
				return false
			}
		}
		return true
	case map[string]any:
		for _, e := range v {
			if !isData(e) {
				return false
			}
		}
		return true
	}
	return false
}
