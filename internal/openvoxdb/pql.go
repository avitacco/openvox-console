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
