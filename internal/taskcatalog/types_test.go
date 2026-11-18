package taskcatalog

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func mustDecode(t *testing.T, s string) any {
	t.Helper()
	v, err := decodeValue(json.RawMessage(s))
	if err != nil {
		t.Fatalf("decode %s: %v", s, err)
	}
	return v
}

func TestTypeCheck(t *testing.T) {
	for _, tc := range []struct {
		typ   string
		value string
		ok    bool
	}{
		{"", `{"any":"thing"}`, true},
		{"Any", `null`, true},
		{"String", `"x"`, true},
		{"String", `3`, false},
		{"String[1]", `""`, false},
		{"String[1]", `"a"`, true},
		{"String[1, 3]", `"abcd"`, false},
		{"Integer", `3`, true},
		{"Integer", `3.5`, false},
		{"Integer", `"3"`, false},
		{"Integer[1, 10]", `0`, false},
		{"Integer[1, 10]", `10`, true},
		{"Integer[3]", `2`, false},
		{"Integer[default, 5]", `-100`, true},
		{"Float", `1.5`, true},
		{"Float", `1`, false},
		{"Numeric", `1`, true},
		{"Numeric", `1.5`, true},
		{"Boolean", `true`, true},
		{"Boolean", `"true"`, false},
		{"Optional[String]", `null`, true},
		{"Optional[String]", `"x"`, true},
		{"Optional[String]", `1`, false},
		{"Optional[String[1]]", `""`, false},
		{"NotUndef", `null`, false},
		{"NotUndef[String]", `"x"`, true},
		{"Undef", `null`, true},
		{"Undef", `1`, false},
		{"Enum['plain', 'loud']", `"loud"`, true},
		{"Enum['plain', 'loud']", `"quiet"`, false},
		{`Enum["a", "b"]`, `"b"`, true},
		// puppetlabs-service and puppetlabs-package write bare words.
		{"Enum[start, stop, restart, enable, disable, status]", `"restart"`, true},
		{"Enum[install, status, uninstall, upgrade]", `"remove"`, false},
		{"Enum['Yes', 'No', true]", `"yes"`, true},
		{"Pattern[/^\\d+$/]", `"123"`, true},
		{"Pattern[/^\\d+$/]", `"12a"`, false},
		{"Pattern[/^a/, /^b/]", `"bee"`, true},
		{"Array", `[1, "a"]`, true},
		{"Array[String]", `["a", "b"]`, true},
		{"Array[String]", `["a", 1]`, false},
		{"Array[String, 1]", `[]`, false},
		{"Optional[Array[String]]", `null`, true},
		{"Hash", `{"a": 1}`, true},
		{"Hash", `[]`, false},
		{"Hash[String, Integer]", `{"a": 1}`, true},
		{"Hash[String, Integer]", `{"a": "1"}`, false},
		{"Hash[String, Data]", `{"a": {"b": [1, null]}}`, true},
		{"Optional[Variant[String, Array[String]]]", `["a"]`, true},
		{"Optional[Variant[String, Array[String]]]", `"a"`, true},
		{"Optional[Variant[String, Array[String]]]", `3`, false},
		{"Variant[Integer, Enum[all]]", `"all"`, true},
		{"Data", `{"a": [true, 1.5, "x", null]}`, true},
		{"Scalar", `[]`, false},
		{"Scalar", `"x"`, true},
		// Types this parser does not model accept anything.
		{"Stdlib::Port", `"not a port"`, true},
		{"Struct[{name => String, Optional[size] => Integer}]", `42`, true},
		{"Tuple[String, Integer]", `"x"`, true},
		{"Optional[Stdlib::Absolutepath]", `"relative"`, true},
		{"Array[Stdlib::Host]", `["x"]`, true},
		{"Array[Stdlib::Host]", `"x"`, false},
	} {
		typ, err := parseType(tc.typ)
		if err != nil {
			t.Errorf("parseType(%q): %v", tc.typ, err)
			continue
		}
		why := typ.check(mustDecode(t, tc.value))
		if (why == "") != tc.ok {
			t.Errorf("%s accepts %s = %v (%s), want %v", tc.typ, tc.value, why == "", why, tc.ok)
		}
	}
}

func TestParseTypeRejectsBrokenSyntax(t *testing.T) {
	for _, s := range []string{"String[", "Integer[1,", "Enum['a]", "Pattern[/(/]", "string", "Array[String]]"} {
		if _, err := parseType(s); err == nil {
			t.Errorf("parseType(%q) succeeded, want an error", s)
		}
	}
}

func TestAcceptsUndef(t *testing.T) {
	for typ, want := range map[string]bool{
		"":                         true,
		"Any":                      true,
		"Data":                     true,
		"Optional[String]":         true,
		"Variant[Undef, String]":   true,
		"Stdlib::Port":             true,
		"String":                   false,
		"Variant[String, Integer]": false,
		"NotUndef":                 false,
		"Optional[Stdlib::Port]":   true,
		"Array[Optional[String]]":  false,
	} {
		parsed, err := parseType(typ)
		if err != nil {
			t.Fatalf("parseType(%q): %v", typ, err)
		}
		if got := parsed.acceptsUndef(); got != want {
			t.Errorf("%q acceptsUndef = %v, want %v", typ, got, want)
		}
	}
}

func echoTask(t *testing.T) Task {
	return Task{Name: "task_fixture::echo", Metadata: recordedMetadata(t, "echo")}
}

func validationProblems(t *testing.T, err error) map[string]string {
	t.Helper()
	var ve *ValidationError
	if !errors.As(err, &ve) {
		t.Fatalf("err = %v, want a ValidationError", err)
	}
	out := map[string]string{}
	for _, p := range ve.Problems {
		out[p.Param] = p.Message
	}
	return out
}

func TestValidateAcceptsAndFillsDefaults(t *testing.T) {
	params, err := Validate(echoTask(t), json.RawMessage(`{"message":"hi","mode":"loud"}`))
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if params["count"] != json.Number("1") {
		t.Errorf("count = %#v, want the declared default 1", params["count"])
	}
	if _, ok := params["tags"]; ok {
		t.Error("tags was filled in, but it has no default")
	}
}

func TestValidateMissingRequired(t *testing.T) {
	problems := validationProblems(t, func() error {
		_, err := Validate(echoTask(t), json.RawMessage(`{"mode":"loud"}`))
		return err
	}())
	if problems["message"] != "required" || len(problems) != 1 {
		t.Errorf("problems = %v, want message required", problems)
	}
}

func TestValidateUndeclared(t *testing.T) {
	_, err := Validate(echoTask(t), json.RawMessage(`{"message":"hi","mode":"loud","colour":"red"}`))
	if problems := validationProblems(t, err); !strings.Contains(problems["colour"], "not a parameter") {
		t.Errorf("problems = %v", problems)
	}
}

func TestValidateWrongType(t *testing.T) {
	_, err := Validate(echoTask(t), json.RawMessage(`{"message":"hi","mode":"loud","count":"three"}`))
	problems := validationProblems(t, err)
	if !strings.Contains(problems["count"], "Optional[Integer[1, 10]]") {
		t.Errorf("problems = %v, want count to name its declared type", problems)
	}
}

func TestValidateOptionalKeepsDetail(t *testing.T) {
	_, err := Validate(echoTask(t), json.RawMessage(`{"message":"hi","mode":"loud","count":99}`))
	if got := validationProblems(t, err)["count"]; got != "expected Optional[Integer[1, 10]], got 99" {
		t.Errorf("count problem = %q", got)
	}
}

func TestValidateReportsEveryProblem(t *testing.T) {
	_, err := Validate(echoTask(t), json.RawMessage(`{"count":99,"extra":1}`))
	problems := validationProblems(t, err)
	for _, name := range []string{"count", "extra", "message", "mode"} {
		if _, ok := problems[name]; !ok {
			t.Errorf("no problem reported for %s: %v", name, problems)
		}
	}
}

func TestValidateOptionalOmitted(t *testing.T) {
	task := Task{Name: "t", Metadata: Metadata{Parameters: map[string]Parameter{"note": {Type: "Optional[String]"}}}}
	if _, err := Validate(task, nil); err != nil {
		t.Errorf("Validate: %v", err)
	}
}

func TestValidateUncheckedType(t *testing.T) {
	task := Task{Name: "t", Metadata: Metadata{Parameters: map[string]Parameter{"port": {Type: "Stdlib::Port"}}}}
	if _, err := Validate(task, json.RawMessage(`{"port":"anything"}`)); err != nil {
		t.Errorf("Validate: %v", err)
	}
}

func TestValidateNoParametersKeyAcceptsAnything(t *testing.T) {
	task := Task{Name: "task_fixture::plain", Metadata: recordedMetadata(t, "plain")}
	if _, err := Validate(task, json.RawMessage(`{"whatever":[1,2]}`)); err != nil {
		t.Errorf("Validate: %v", err)
	}
}

func TestValidateEmptyParametersRejectsAny(t *testing.T) {
	task := Task{Name: "task_fixture", Metadata: recordedMetadata(t, "init")}
	if _, err := Validate(task, json.RawMessage(`{"x":1}`)); err == nil {
		t.Error("Validate accepted a parameter for a task declaring none")
	}
}

func TestValidateRejectsMetaparameters(t *testing.T) {
	task := Task{Name: "task_fixture::plain", Metadata: recordedMetadata(t, "plain")}
	_, err := Validate(task, json.RawMessage(`{"_installdir":"/etc"}`))
	if problems := validationProblems(t, err); problems["_installdir"] == "" {
		t.Errorf("problems = %v", problems)
	}
}

func TestValidateRejectsNonObject(t *testing.T) {
	_, err := Validate(echoTask(t), json.RawMessage(`[1,2]`))
	validationProblems(t, err)
}

func TestInputFor(t *testing.T) {
	for typ, want := range map[string]ParamInput{
		"Boolean":                  {Kind: "boolean", Required: true},
		"Optional[Integer[1, 10]]": {Kind: "integer"},
		"Float":                    {Kind: "number", Required: true},
		"Enum[start, stop]":        {Kind: "enum", Options: []string{"start", "stop"}, Required: true},
		"String[1]":                {Kind: "string", Required: true},
		"Optional[Array[String]]":  {Kind: "json"},
		"Hash":                     {Kind: "json", Required: true},
		"Stdlib::Port":             {Kind: "json"},
	} {
		got := InputFor(Parameter{Type: typ})
		if got.Kind != want.Kind || got.Required != want.Required || strings.Join(got.Options, ",") != strings.Join(want.Options, ",") {
			t.Errorf("InputFor(%q) = %+v, want %+v", typ, got, want)
		}
	}
	if InputFor(Parameter{Type: "String", Default: json.RawMessage(`"x"`)}).Required {
		t.Error("a parameter with a default is marked required")
	}
}
