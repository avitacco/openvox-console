package orchestrator

import "testing"

func TestPuppetRunFailed(t *testing.T) {
	cases := []struct {
		exitCode int
		want     bool
		note     string
	}{
		{0, false, "no changes"},
		{2, false, "changes applied, no errors"},
		{1, true, "run itself errored - found live against a real agent, previously misclassified as success"},
		{4, true, "resource failures"},
		{6, true, "changes + failures"},
		{3, true, "changes + run error"},
		{5, true, "run error + resource failures"},
		{7, true, "changes + run error + resource failures"},
	}
	for _, c := range cases {
		if got := puppetRunFailed(c.exitCode); got != c.want {
			t.Errorf("puppetRunFailed(%d) = %v, want %v (%s)", c.exitCode, got, c.want, c.note)
		}
	}
}
