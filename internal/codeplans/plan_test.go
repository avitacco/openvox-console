package codeplans

import (
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
)

func parse(t *testing.T, src string) (*Plan, error) {
	t.Helper()
	return Parse(File{Name: "ops::demo", Data: []byte(src)})
}

const goodPlan = `
description: Restart a service and report
parameters:
  targets:
    type: TargetSpec
    description: Where to restart
  service:
    type: String[1]
    default: nginx
  attempts:
    type: Integer[1, 5]
    default: 1
  token:
    type: Sensitive[String]
  note:
    type: Optional[String]
steps:
  - name: restart
    description: Restart it
    task: service::restart
    targets: $targets
    parameters:
      name: $service
      password: $token
  - name: second
    task: service::status
    targets: $restart.ok_targets
    parameters:
      name: "svc ${service}"
  - message: "restarted ${restart.value} (price: $$5)"
  - name: sub
    plan: ops::other
    parameters:
      targets: $targets
      list: [$service, {k: "$restart.results[0].value"}]
return: $second
`

func TestParse_AGoodPlan(t *testing.T) {
	p, err := parse(t, goodPlan)
	if err != nil {
		t.Fatal(err)
	}
	if p.Description != "Restart a service and report" || len(p.Parameters) != 5 || len(p.Steps) != 4 || !p.HasReturn || len(p.SHA256) != 64 {
		t.Fatalf("plan = %+v", p)
	}
	byName := map[string]Param{}
	for _, d := range p.Parameters {
		byName[d.Name] = d
	}
	if !byName["targets"].Target || !byName["targets"].Required || byName["service"].Required || !byName["token"].Sensitive || byName["note"].Required {
		t.Errorf("parameters = %+v", byName)
	}
	if got := []string{p.Parameters[0].Name, p.Parameters[1].Name, p.Parameters[2].Name}; !reflect.DeepEqual(got, []string{"targets", "service", "attempts"}) {
		t.Errorf("declaration order lost: %v", got)
	}
	kinds := []string{p.Steps[0].Kind, p.Steps[1].Kind, p.Steps[2].Kind, p.Steps[3].Kind}
	if !reflect.DeepEqual(kinds, []string{KindTask, KindTask, KindMessage, KindPlan}) || p.Steps[0].Name != "restart" {
		t.Errorf("steps = %v", kinds)
	}
}

func TestParse_Refusals(t *testing.T) {
	cases := map[string]struct{ src, want string }{
		"a command step":             {"steps:\n  - name: a\n    command: whoami\n    targets: [a]\n", `step 1 (a): "command": commands are not supported`},
		"an eval step":               {"steps:\n  - eval: 1+1\n", `"eval": eval needs the Puppet language`},
		"resources":                  {"steps:\n  - resources: []\n", "applying resources needs the Puppet language"},
		"a script":                   {"steps:\n  - script: x.sh\n    targets: [a]\n", "scripts are not supported"},
		"an unknown step key":        {"steps:\n  - task: a::b\n    targets: [x]\n    retry: 3\n", `"retry" is not supported in a step`},
		"two actions":                {"steps:\n  - task: a::b\n    plan: c::d\n    targets: [x]\n", "exactly one of task, plan or message"},
		"no action":                  {"steps:\n  - name: x\n", "exactly one of task, plan or message"},
		"a task without targets":     {"steps:\n  - task: a::b\n", "needs targets"},
		"targets on a plan step":     {"steps:\n  - plan: a::b\n    targets: [x]\n", "passes its targets as a parameter"},
		"an unknown top key":         {"version: 1\nsteps:\n  - message: hi\n", `"version" is not supported at the top`},
		"no steps":                   {"parameters: {}\n", "needs a list of steps"},
		"an empty steps list":        {"steps: []\n", "needs a list of steps"},
		"not a mapping":              {"- a\n- b\n", "a YAML mapping"},
		"bad yaml":                   {"steps: [\n", "not valid YAML"},
		"an operator":                {"parameters:\n  n:\n    type: Integer\nsteps:\n  - message: x\n    name: a\n  - message: $n + 1\n", "plans have no operators or functions"},
		"a function call":            {"parameters:\n  n: {type: String}\nsteps:\n  - message: $n.upcase()\n", "only .name, [0] and ['name'] may follow"},
		"a bare dollar":              {"steps:\n  - message: costs $5\n", "must be written $$"},
		"an unknown variable":        {"steps:\n  - message: $nope\n", "$nope is not a parameter or an earlier step"},
		"a later step":               {"steps:\n  - message: $later\n  - name: later\n    message: hi\n", "$later is not a parameter or an earlier step"},
		"a bad step name":            {"steps:\n  - name: Bad-Name\n    message: hi\n", "lower case letters"},
		"a repeated name":            {"steps:\n  - name: a\n    message: hi\n  - name: a\n    message: hi\n", "already a parameter or an earlier step"},
		"a name that is a parameter": {"parameters:\n  a: {type: String}\nsteps:\n  - name: a\n    message: hi\n", "already a parameter or an earlier step"},
		"an unknown parameter key":   {"parameters:\n  a:\n    required: true\nsteps:\n  - message: hi\n", `"required" is not supported`},
		"a bad parameter name":       {"parameters:\n  Bad: {type: String}\nsteps:\n  - message: hi\n", "lower case letters"},
		"an unclosed ${":             {"steps:\n  - message: \"a ${b\"\n", "is not closed"},
		"a duplicate key":            {"steps:\n  - message: hi\n    message: again\n", "is given twice"},
		"a message with targets":     {"steps:\n  - message: hi\n    targets: [a]\n", "a message has no targets"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := parse(t, c.src)
			var u *UnusableError
			if !errors.As(err, &u) {
				t.Fatalf("err = %v, want an unusable plan", err)
			}
			if !strings.Contains(err.Error(), c.want) {
				t.Errorf("err = %q, want it to contain %q", err, c.want)
			}
		})
	}
}

func TestParse_StepErrorsNameTheStepByNumberAndName(t *testing.T) {
	_, err := parse(t, "steps:\n  - message: fine\n  - name: bad\n    command: x\n    targets: [a]\n")
	var u *UnusableError
	if !errors.As(err, &u) || u.Step != 2 || u.Name != "bad" {
		t.Errorf("err = %#v", err)
	}
}

func TestParse_OversizeAndEmpty(t *testing.T) {
	if _, err := Parse(File{Oversize: true}); err == nil || !strings.Contains(err.Error(), "larger than") {
		t.Errorf("oversize = %v", err)
	}
	if _, err := Parse(File{Data: nil}); err == nil || !strings.Contains(err.Error(), "empty") {
		t.Errorf("empty = %v", err)
	}
}

func TestBind(t *testing.T) {
	p, err := parse(t, goodPlan)
	if err != nil {
		t.Fatal(err)
	}
	env, err := p.Bind(map[string]any{"targets": []any{"a", "b"}, "token": "s3cret"})
	if err != nil {
		t.Fatal(err)
	}
	if env["service"] != "nginx" || env["attempts"] != json.Number("1") || env["note"] != nil {
		t.Errorf("defaults: %+v", env)
	}
	if s, ok := env["token"].(Sensitive); !ok || s.V != "s3cret" || s.String() != Redacted {
		t.Errorf("token = %#v, want it wrapped as Sensitive", env["token"])
	}
}

func TestBind_Refusals(t *testing.T) {
	p, _ := parse(t, goodPlan)
	ok := map[string]any{"targets": []any{"a"}, "token": "x"}
	with := func(k string, v any) map[string]any {
		m := map[string]any{}
		for a, b := range ok {
			m[a] = b
		}
		if v == nil {
			delete(m, k)
		} else {
			m[k] = v
		}
		return m
	}
	cases := map[string]struct {
		given map[string]any
		name  string
		want  string
	}{
		"a required parameter is missing": {with("targets", nil), "targets", "is required"},
		"another is missing":              {with("token", nil), "token", "is required"},
		"an unknown parameter":            {with("extra", 1), "extra", "is not a parameter of this plan"},
		"the wrong type":                  {with("attempts", "many"), "attempts", "Integer"},
		"out of range":                    {with("attempts", json.Number("9")), "attempts", "Integer"},
		"an empty string for String[1]":   {with("service", ""), "service", "String"},
		"a bad target":                    {with("targets", json.Number("3")), "targets", "not a valid target"},
		"an empty target list":            {with("targets", []any{}), "targets", "at least one node"},
		"a target list of numbers":        {with("targets", []any{json.Number("1")}), "targets", "node names"},
		"a group that is not an id":       {with("targets", map[string]any{"group": "x"}), "targets", "its id"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := p.Bind(c.given)
			var pe *ParamError
			if !errors.As(err, &pe) || pe.Name != c.name || !strings.Contains(err.Error(), c.want) {
				t.Errorf("err = %v, want parameter %s: %s", err, c.name, c.want)
			}
		})
	}
	// The three forms of a target all bind.
	for _, v := range []any{"web01", []any{"a"}, map[string]any{"group": json.Number("4")}, map[string]any{"query": "nodes[certname] {}"}} {
		if _, err := p.Bind(with("targets", v)); err != nil {
			t.Errorf("target %v: %v", v, err)
		}
	}
}

func TestParseTargetSpec(t *testing.T) {
	g, _ := ParseTargetSpec(map[string]any{"group": json.Number("7")})
	if g.Group == nil || *g.Group != 7 {
		t.Errorf("group = %+v", g)
	}
	q, _ := ParseTargetSpec(map[string]any{"query": "q"})
	n, _ := ParseTargetSpec("one")
	if q.Query != "q" || len(n.Nodes) != 1 {
		t.Errorf("spec = %+v %+v", q, n)
	}
	if _, err := ParseTargetSpec(map[string]any{"group": json.Number("1"), "query": "q"}); err == nil {
		t.Error("both group and query were accepted")
	}
}
