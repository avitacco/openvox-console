package taskcatalog

// ParamType is a parsed Puppet type expression, for a caller that checks
// values against a declared type: a task's parameter, or a plan's. It is
// the same checker tasks are validated with, so the two agree.
type ParamType struct{ expr *typeExpr }

// ParseParamType parses a type expression. An empty one is Any, and a name
// it does not know accepts every value (see typeExpr).
func ParseParamType(s string) (*ParamType, error) {
	e, err := parseType(s)
	if err != nil {
		return nil, err
	}
	return &ParamType{expr: e}, nil
}

// Check returns why v is not of the type, or "" when it is. v is a decoded
// JSON value: nil, bool, float64, string, []any or map[string]any.
func (p *ParamType) Check(v any) string { return p.expr.check(v) }

// AcceptsUndef reports whether the type allows a value that is not given.
func (p *ParamType) AcceptsUndef() bool { return p.expr.acceptsUndef() }

// IsSensitive reports whether the type is Sensitive[...], or Optional of one.
func (p *ParamType) IsSensitive() bool {
	e := p.expr
	for e != nil && e.name == "Optional" && len(e.args) > 0 {
		e = e.args[0].typ
	}
	return e != nil && e.name == "Sensitive"
}

// String is the type as it was written.
func (p *ParamType) String() string { return p.expr.raw }
