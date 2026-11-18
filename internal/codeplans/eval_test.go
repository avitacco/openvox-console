package codeplans

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

// evalIn compiles and evaluates one value written as YAML would give it.
func evalIn(t *testing.T, v any, env Env) (any, error) {
	t.Helper()
	scope := map[string]bool{}
	for k := range env {
		scope[k] = true
	}
	n, err := compile(v, scope)
	if err != nil {
		return nil, err
	}
	return Eval(n, env)
}

func TestEval_ReferencesKeepTheirType(t *testing.T) {
	env := Env{
		"count": json.Number("3"),
		"names": []any{"a", "b"},
		"step":  StepResult([]TargetResult{{Target: "n1", OK: true, Value: map[string]any{"pid": json.Number("42"), "tags": []any{"x", "y"}}}}),
	}
	cases := map[string]any{
		"$count":                  json.Number("3"),
		"$names":                  []any{"a", "b"},
		"$names[1]":               "b",
		"$step.ok":                true,
		"$step.value.pid":         json.Number("42"),
		"$step.value.tags[0]":     "x",
		"$step['value']['pid']":   json.Number("42"),
		"$step.results[0].target": "n1",
		"$step.ok_targets":        []any{"n1"},
		"$step.error_targets":     []any{},
	}
	for src, want := range cases {
		got, err := evalIn(t, src, env)
		if err != nil || !reflect.DeepEqual(got, want) {
			t.Errorf("%s = %#v, %v; want %#v", src, got, err, want)
		}
	}
}

func TestEval_Interpolation(t *testing.T) {
	env := Env{"name": "web", "n": json.Number("2"), "list": []any{"a", "b"}, "m": map[string]any{"k": "v"}}
	cases := map[string]string{
		"hello ${name}":     "hello web",
		"${n} of ${name}":   "2 of web",
		"all: ${list}":      `all: ["a","b"]`,
		"map: ${m}":         `map: {"k":"v"}`,
		"$$5 and $${not}":   "$5 and ${not}",
		"cost $$":           "cost $",
		"${name}":           "web",
		"no variables here": "no variables here",
	}
	for src, want := range cases {
		got, err := evalIn(t, src, env)
		if err != nil || got != want {
			t.Errorf("%q = %#v, %v; want %q", src, got, err, want)
		}
	}
}

func TestEval_ListsAndMappingsAreWalked(t *testing.T) {
	got, err := evalIn(t, map[string]any{"a": []any{"$x", "t-${x}"}, "b": map[string]any{"c": "$x"}, "d": json.Number("1")}, Env{"x": "v"})
	want := map[string]any{"a": []any{"v", "t-v"}, "b": map[string]any{"c": "v"}, "d": json.Number("1")}
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Errorf("got %#v, %v", got, err)
	}
}

func TestEval_Errors(t *testing.T) {
	env := Env{"m": map[string]any{"k": "v"}, "l": []any{"a"}, "s": "text", "step": StepResult([]TargetResult{{Target: "a"}, {Target: "b"}})}
	cases := map[string]string{
		"$m.missing":  `there is no "missing"`,
		"$l[3]":       "the list has 1 items",
		"$m[0]":       "is for a list",
		"$l.name":     "is for a mapping",
		"$s.x":        "cannot look inside",
		"$step.value": `there is no "value"`, // a step on two targets has no single value
	}
	for src, want := range cases {
		if _, err := evalIn(t, src, env); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%s: err = %v, want %q", src, err, want)
		}
	}
	// A variable that has not been set yet.
	n, _ := compile("$later", map[string]bool{"later": true})
	if _, err := Eval(n, Env{}); err == nil || !strings.Contains(err.Error(), "has no value yet") {
		t.Errorf("an unset variable = %v", err)
	}
}

func TestSensitive_NeverPrintsAndIsUnwrappedOnlyOnPurpose(t *testing.T) {
	secret := Sensitive{V: "hunter2"}
	env := Env{"token": secret, "m": map[string]any{"password": secret}}

	got, err := evalIn(t, "using ${token} now", env)
	if err != nil || got != "using [redacted] now" || strings.Contains(got.(string), "hunter2") {
		t.Errorf("interpolated = %q, %v", got, err)
	}
	// Referring to it whole keeps it wrapped, so it can reach a task.
	v, _ := evalIn(t, "$token", env)
	if _, ok := v.(Sensitive); !ok {
		t.Errorf("$token = %#v, want it still wrapped", v)
	}
	// Looking inside one is refused, not quietly allowed.
	if _, err := evalIn(t, "$token.x", env); err == nil || !strings.Contains(err.Error(), "Sensitive") {
		t.Errorf("a part of a Sensitive = %v", err)
	}
	// Even a mistake cannot write it out.
	b, _ := json.Marshal(map[string]any{"v": secret})
	if strings.Contains(string(b), "hunter2") || !strings.Contains(string(b), Redacted) {
		t.Errorf("JSON = %s", b)
	}
	if s := Unwrap(env["m"]).(map[string]any)["password"]; s != "hunter2" {
		t.Errorf("Unwrap = %v", s)
	}
	if s := Redact(env["m"]).(map[string]any)["password"]; s != Redacted {
		t.Errorf("Redact = %v", s)
	}
}

func TestStepResult_Shapes(t *testing.T) {
	one := StepResult([]TargetResult{{Target: "a", OK: true, Value: "v"}})
	if one["ok"] != true || one["value"] != "v" || len(one["error_targets"].([]any)) != 0 {
		t.Errorf("one target = %+v", one)
	}
	two := StepResult([]TargetResult{{Target: "a", OK: true}, {Target: "b", Error: "boom"}})
	if two["ok"] != false || !reflect.DeepEqual(two["ok_targets"], []any{"a"}) || !reflect.DeepEqual(two["error_targets"], []any{"b"}) {
		t.Errorf("two targets = %+v", two)
	}
	if _, has := two["value"]; has {
		t.Error("a step on two targets has no single value")
	}
	if r := two["results"].([]any)[1].(map[string]any); r["error"] != "boom" || r["ok"] != false {
		t.Errorf("result = %+v", r)
	}
}
