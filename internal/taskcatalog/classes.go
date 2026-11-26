package taskcatalog

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
)

// ForbiddenError reports that openvoxserver refused the request: its
// auth.conf has no rule letting the console's certificate make it. This
// is told apart from UnreachableError because the fix is different - a
// rule to add, not a server to bring back.
type ForbiddenError struct {
	Path string
	// Method is the refused request's method; "" means a GET.
	Method string
}

func (e *ForbiddenError) Error() string {
	if e.Method != "" && e.Method != http.MethodGet {
		return fmt.Sprintf("openvoxserver does not allow the console to %s %s", e.Method, e.Path)
	}
	return fmt.Sprintf("openvoxserver does not allow the console to read %s", e.Path)
}

// ClassParam is one parameter of a class as openvoxserver reports it.
type ClassParam struct {
	Name string `json:"name"`
	// Type is the declared Puppet type, or "" when none is declared.
	Type string `json:"type"`
	// DefaultSource is the default as written in the manifest, or ""
	// when the parameter has none - which makes it required.
	DefaultSource string `json:"default_source"`
	// DefaultLiteral is the default's value when it is a literal, and
	// absent when it is an expression (or there is no default).
	DefaultLiteral json.RawMessage `json:"default_literal"`
	// HasDefault tells a parameter with no default from one whose
	// default's source is the empty string.
	HasDefault bool `json:"-"`
}

// UnmarshalJSON records whether a default was given at all.
func (p *ClassParam) UnmarshalJSON(data []byte) error {
	type plain ClassParam
	var probe map[string]json.RawMessage
	if err := json.Unmarshal(data, &probe); err != nil {
		return err
	}
	if err := json.Unmarshal(data, (*plain)(p)); err != nil {
		return err
	}
	_, hasSource := probe["default_source"]
	_, hasLiteral := probe["default_literal"]
	p.HasDefault = hasSource || hasLiteral
	return nil
}

// ClassInfo is one class and its parameters, in declaration order.
type ClassInfo struct {
	Name   string       `json:"name"`
	Params []ClassParam `json:"params"`
}

// ClassFile is one manifest of an environment: the classes it defines,
// or the error that stopped it being parsed.
type ClassFile struct {
	Path    string      `json:"path"`
	Classes []ClassInfo `json:"classes"`
	Error   string      `json:"error"`
}

// EnvironmentClasses is openvoxserver's answer for one environment.
type EnvironmentClasses struct {
	Name  string      `json:"name"`
	Files []ClassFile `json:"files"`
	// ETag is the server's version tag for this answer, or "" when it
	// gave none (it only does with its class cache enabled).
	ETag string `json:"-"`
}

// EnvironmentClasses reads the classes environment's code defines. When
// etag is not empty it is sent as If-None-Match, and a server that
// answers "not modified" yields notModified with a nil result - the
// caller's earlier answer still stands.
func (c *Client) EnvironmentClasses(ctx context.Context, environment, etag string) (result *EnvironmentClasses, notModified bool, err error) {
	if c == nil {
		return nil, false, ErrNotConfigured
	}
	const path = "/puppet/v3/environment_classes"
	u := c.baseURL + path + "?" + url.Values{"environment": {environment}}.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, false, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	if etag != "" {
		req.Header.Set("If-None-Match", etag)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, false, &UnreachableError{Err: err}
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponse))
	if err != nil {
		return nil, false, &UnreachableError{Err: fmt.Errorf("read response: %w", err)}
	}
	switch resp.StatusCode {
	case http.StatusOK:
	case http.StatusNotModified:
		return nil, true, nil
	case http.StatusForbidden:
		return nil, false, &ForbiddenError{Path: path}
	case http.StatusNotFound:
		return nil, false, &NotFoundError{Kind: "environment", Name: environment}
	default:
		return nil, false, &UnreachableError{Err: fmt.Errorf("GET %s: %s", path, resp.Status)}
	}
	var out EnvironmentClasses
	dec := json.NewDecoder(bytes.NewReader(body))
	if err := dec.Decode(&out); err != nil {
		return nil, false, &UnreachableError{Err: fmt.Errorf("decode %s: %w", path, err)}
	}
	out.ETag = resp.Header.Get("Etag")
	return &out, false, nil
}

// CheckValue reports why value does not fit the Puppet type written as
// typ, or "" when it fits or the type is one this package does not
// evaluate (see typeExpr). value is any JSON-decoded value.
func CheckValue(typ string, value any) string {
	t, err := parseType(typ)
	if err != nil {
		return ""
	}
	// check expects numbers as json.Number, which is how it tells an
	// Integer from a Float; re-decode whatever the caller holds.
	raw, err := json.Marshal(value)
	if err != nil {
		return ""
	}
	v, err := decodeValue(raw)
	if err != nil {
		return ""
	}
	return t.check(v)
}
