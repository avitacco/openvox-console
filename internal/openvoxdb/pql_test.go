package openvoxdb

import "testing"

func TestValidatePQL_Valid(t *testing.T) {
	for _, pql := range []string{
		`nodes {}`,
		`facts { certname = "web01" }`,
		`events { report = "abc123" }`,
	} {
		if err := validatePQL(pql); err != nil {
			t.Errorf("validatePQL(%q) = %v, want nil", pql, err)
		}
	}
}

func TestValidatePQL_Malformed(t *testing.T) {
	for _, pql := range []string{
		``,
		`   `,
		`nodes {`,
		`nodes }`,
		`facts { certname = "unterminated }`,
		`nodes [}`,
	} {
		if err := validatePQL(pql); err == nil {
			t.Errorf("validatePQL(%q) = nil, want an error", pql)
		}
	}
}

func TestPQLString_EscapesQuotesAndBackslashes(t *testing.T) {
	got := pqlString(`node"with\quote`)
	want := `"node\"with\\quote"`
	if got != want {
		t.Errorf("pqlString() = %q, want %q", got, want)
	}
}
