package openvoxdb

import (
	"fmt"
	"strings"
)

// validatePQL is a syntax sanity check, not a PQL grammar validator (see
// the package doc comment on why this client stays thin): it rejects
// empty input and unbalanced brackets/quotes, which is enough to satisfy
// the "malformed query never reaches openvoxdb" requirement without
// building a full parser.
func validatePQL(pql string) error {
	if strings.TrimSpace(pql) == "" {
		return fmt.Errorf("malformed PQL query: empty query")
	}

	var stack []rune
	inString := false
	escaped := false

	for _, r := range pql {
		if inString {
			switch {
			case escaped:
				escaped = false
			case r == '\\':
				escaped = true
			case r == '"':
				inString = false
			}
			continue
		}

		switch r {
		case '"':
			inString = true
		case '{', '[':
			stack = append(stack, r)
		case '}', ']':
			if len(stack) == 0 {
				return fmt.Errorf("malformed PQL query: unbalanced brackets")
			}
			open := stack[len(stack)-1]
			if (r == '}' && open != '{') || (r == ']' && open != '[') {
				return fmt.Errorf("malformed PQL query: mismatched brackets")
			}
			stack = stack[:len(stack)-1]
		}
	}

	if inString {
		return fmt.Errorf("malformed PQL query: unterminated string")
	}
	if len(stack) != 0 {
		return fmt.Errorf("malformed PQL query: unbalanced brackets")
	}
	return nil
}

// pqlString renders s as a double-quoted PQL string literal, escaping
// characters that would otherwise let it break out of the literal.
func pqlString(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range s {
		if r == '"' || r == '\\' {
			b.WriteByte('\\')
		}
		b.WriteRune(r)
	}
	b.WriteByte('"')
	return b.String()
}

// InvalidQueryError is a query that was refused as a query: by this
// package's own checks, or by openvoxdb answering it with a 4xx. Message
// is openvoxdb's own where it gave one.
type InvalidQueryError struct {
	Message string
}

func (e *InvalidQueryError) Error() string { return e.Message }

// MaxUserQueryLength bounds a query a user typed.
const MaxUserQueryLength = 4000

// CheckUserQuery checks the shape of a PQL query a user supplied, before
// it is sent anywhere: not empty, not over MaxUserQueryLength, in PQL
// string form rather than AST (JSON array) form, and balanced. It
// returns an *InvalidQueryError saying which.
func CheckUserQuery(pql string) error {
	trimmed := strings.TrimSpace(pql)
	switch {
	case trimmed == "":
		return &InvalidQueryError{Message: "the query is empty"}
	case len(pql) > MaxUserQueryLength:
		return &InvalidQueryError{Message: fmt.Sprintf("the query is longer than %d characters", MaxUserQueryLength)}
	case strings.HasPrefix(trimmed, "["):
		return &InvalidQueryError{Message: "a PQL string is required, such as nodes[certname] { }; the JSON (AST) form is not accepted"}
	}
	if err := validatePQL(pql); err != nil {
		return &InvalidQueryError{Message: err.Error()}
	}
	return nil
}

// HasSubquery reports whether pql reads from more than one entity: it
// has more than one brace block outside string literals. Both of PQL's
// subquery forms - explicit ("certname in facts[certname] { ... }") and
// implicit ("nodes { facts { ... } }") - need a second block, and no
// single-entity query has one, so counting is enough without parsing
// the language. It errs toward true.
func HasSubquery(pql string) bool {
	blocks := 0
	inString := false
	var quote rune
	escaped := false
	for _, r := range pql {
		if inString {
			switch {
			case escaped:
				escaped = false
			case r == '\\':
				escaped = true
			case r == quote:
				inString = false
			}
			continue
		}
		switch r {
		case '"', '\'':
			inString, quote = true, r
		case '{':
			blocks++
		}
	}
	return blocks > 1
}
