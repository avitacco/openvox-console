package taskexec

import (
	"strings"
	"testing"
)

func TestCapped(t *testing.T) {
	b := NewCapped(5)
	b.Write([]byte("abc"))
	b.Write([]byte("defgh"))
	if got := b.String(); !strings.HasPrefix(got, "abcde\n[output truncated: 5 of 8 bytes shown]") {
		t.Errorf("String() = %q", got)
	}
	small := NewCapped(5)
	small.Write([]byte("abc"))
	if small.String() != "abc" {
		t.Errorf("String() = %q", small.String())
	}
}

func TestChoose(t *testing.T) {
	shell := Implementation{Name: "t.sh", Requirements: []string{"shell"}}
	agent := Implementation{Name: "t.rb", Requirements: []string{"puppet-agent"}}
	got, err := Choose([]Implementation{agent, shell}, []string{"shell"})
	if err != nil || got.Name != "t.sh" {
		t.Fatalf("got %+v, %v", got, err)
	}
	_, err = Choose([]Implementation{agent}, []string{"shell"})
	if err == nil || !strings.Contains(err.Error(), "provides shell") || !strings.Contains(err.Error(), "t.rb (requires puppet-agent)") {
		t.Errorf("err = %v", err)
	}
}

func TestInput(t *testing.T) {
	stdin, env, err := Input("both", map[string]any{"name": "x", "n": 3}, "mod::t", "/run/x")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(stdin), `"_installdir":"/run/x"`) || len(env) != 4 || env[0] != "PT__installdir=/run/x" {
		t.Errorf("stdin %s env %v", stdin, env)
	}
	stdin, env, _ = Input("environment", map[string]any{"a": "b"}, "t", "")
	if stdin != nil || len(env) != 2 {
		t.Errorf("environment only: stdin %s env %v", stdin, env)
	}
}

func TestParseOutput(t *testing.T) {
	if got := string(ParseOutput(` {"a":1} `)); got != `{"a":1}` {
		t.Errorf("object = %s", got)
	}
	if got := string(ParseOutput("plain")); got != `{"_output":"plain"}` {
		t.Errorf("text = %s", got)
	}
}
