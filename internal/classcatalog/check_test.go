package classcatalog

import (
	"context"
	"reflect"
	"testing"
)

func fixtureEnv(t *testing.T) Environment {
	t.Helper()
	c, _ := newCatalog(t, &fakeSource{resp: recorded(t)})
	env, err := c.Classes(context.Background(), "production")
	if err != nil {
		t.Fatal(err)
	}
	// The recorded environment has a manifest that does not parse; most
	// tests want one that is clean.
	env.Errors = nil
	return env
}

func TestCheck(t *testing.T) {
	env := fixtureEnv(t)
	cases := []struct {
		name string
		uses []Use
		want []Warning
	}{
		{"a class the code defines, with fitting values", []Use{{Name: "class_fixture", Parameters: map[string]any{
			"owner": "ops", "port": float64(8443), "manage_service": false, "ensure": "absent",
			"servers": []any{"a"}, "comment": nil, "secret": "s3cret",
		}}}, nil},
		{"a class with no parameters set", []Use{{Name: "class_fixture::worker"}}, nil},
		{"a class the code does not define", []Use{{Name: "gone"}},
			[]Warning{{Kind: WarningClassMissing, Class: "gone"}}},
		{"a parameter the class does not declare", []Use{{Name: "class_fixture", Parameters: map[string]any{"colour": "red"}}},
			[]Warning{{Kind: WarningParameterUnknown, Class: "class_fixture", Parameter: "colour"}}},
		{"text on an Integer", []Use{{Name: "class_fixture", Parameters: map[string]any{"port": "8080"}}},
			[]Warning{{Kind: WarningParameterType, Class: "class_fixture", Parameter: "port", Detail: "expected Integer[1, 65535]"}}},
		{"a type that is not evaluated", []Use{{Name: "class_fixture", Parameters: map[string]any{"config_dir": float64(7)}}}, nil},
		{"an untyped parameter", []Use{{Name: "class_fixture", Parameters: map[string]any{"untyped": []any{1}}}}, nil},
		{"several, in the order given", []Use{
			{Name: "gone"},
			{Name: "class_fixture", Parameters: map[string]any{"zzz": 1, "aaa": 1}},
		}, []Warning{
			{Kind: WarningClassMissing, Class: "gone"},
			{Kind: WarningParameterUnknown, Class: "class_fixture", Parameter: "aaa"},
			{Kind: WarningParameterUnknown, Class: "class_fixture", Parameter: "zzz"},
		}},
	}
	for _, tc := range cases {
		got := Check(env, tc.uses)
		if tc.want == nil {
			tc.want = []Warning{}
		}
		if !reflect.DeepEqual(got, tc.want) {
			t.Errorf("%s:\n got %+v\nwant %+v", tc.name, got, tc.want)
		}
	}
}

func TestCheck_NothingFromAStaleAnswer(t *testing.T) {
	env := fixtureEnv(t)
	env.Stale = true
	got := Check(env, []Use{{Name: "gone"}, {Name: "class_fixture", Parameters: map[string]any{"colour": "red"}}})
	if len(got) != 0 {
		t.Errorf("stale answer flagged %+v", got)
	}
}

// A class that cannot be found may be in the manifest that failed to
// parse, so it is not called missing. What can still be checked is.
func TestCheck_ParseErrorsSuppressMissingClasses(t *testing.T) {
	env := fixtureEnv(t)
	env.Errors = []FileError{{File: "site/x/manifests/init.pp", Error: "Syntax error"}}
	got := Check(env, []Use{{Name: "gone"}, {Name: "class_fixture", Parameters: map[string]any{"colour": "red"}}})
	want := []Warning{{Kind: WarningParameterUnknown, Class: "class_fixture", Parameter: "colour"}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %+v, want %+v", got, want)
	}
}
