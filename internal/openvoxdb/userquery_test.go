package openvoxdb

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHasSubquery(t *testing.T) {
	cases := []struct {
		pql  string
		want bool
	}{
		{`nodes[certname] { }`, false},
		{`nodes { certname ~ "web" }`, false},
		{`inventory[certname] { facts.os.family = "Debian" and trusted.extensions.pp_role = "web" }`, false},
		{`nodes[certname] { certname ~ "a{2,3}" }`, false},
		{`nodes[certname] { certname ~ 'a{2}' }`, false},
		{`nodes[certname] { certname = "say \"{\" twice {" }`, false},
		{`facts[certname, count()] { name = "role" group by certname }`, false},
		// Explicit subquery.
		{`nodes[certname] { certname in facts[certname] { name = "role" } }`, true},
		// Implicit subquery.
		{`nodes[certname] { facts { name = "role" and value = "db" } }`, true},
		// A string that closes before the second block hides nothing.
		{`nodes[certname] { certname = "x" and facts_environment in nodes[facts_environment] { certname = "theirs" } }`, true},
		{`nodes[certname] { certname = 'it\'s' and resources { type = "Class" } }`, true},
	}
	for _, c := range cases {
		if got := HasSubquery(c.pql); got != c.want {
			t.Errorf("HasSubquery(%s) = %v, want %v", c.pql, got, c.want)
		}
	}
}

func TestCheckUserQuery(t *testing.T) {
	ok := []string{
		`nodes[certname] { }`,
		`  inventory[certname] { facts.os.family = "Debian" }`,
	}
	for _, pql := range ok {
		if err := CheckUserQuery(pql); err != nil {
			t.Errorf("CheckUserQuery(%s) = %v, want nil", pql, err)
		}
	}
	bad := map[string]string{
		"":                   "empty",
		"   ":                "empty",
		`["from", "nodes"]`:  "PQL string",
		`nodes[certname] { `: "unbalanced",
		`nodes { certname = "` + strings.Repeat("a", MaxUserQueryLength) + `" }`: "longer than",
	}
	for pql, want := range bad {
		err := CheckUserQuery(pql)
		var invalid *InvalidQueryError
		if !errors.As(err, &invalid) {
			t.Errorf("CheckUserQuery(%.40s) = %v, want an InvalidQueryError", pql, err)
			continue
		}
		if !strings.Contains(invalid.Message, want) {
			t.Errorf("CheckUserQuery(%.40s) = %q, want it to mention %q", pql, invalid.Message, want)
		}
	}
}

// The body is what a real openvoxdb answers a query it cannot parse
// with - recorded from the dev stack (see the change's task 1.3).
const recordedParseError = `PQL parse error at line 1, column 30:

nodes[certname] { certname = }
                             ^

Expected one of:

[
false
true
#"[0-9]+"
-
'
"
`

func TestQueryDistinguishesRefusedFromUnreachable(t *testing.T) {
	status := http.StatusBadRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(status)
		_, _ = w.Write([]byte(recordedParseError))
	}))
	client := &Client{baseURL: srv.URL, http: srv.Client()}

	_, err := client.Query(context.Background(), `nodes[certname] { certname = }`)
	var invalid *InvalidQueryError
	if !errors.As(err, &invalid) {
		t.Fatalf("a 400 from openvoxdb = %v, want an InvalidQueryError", err)
	}
	if !strings.Contains(invalid.Message, "PQL parse error") {
		t.Errorf("message = %q, want openvoxdb's own", invalid.Message)
	}

	status = http.StatusInternalServerError
	_, err = client.Query(context.Background(), `nodes[certname] { }`)
	if err == nil || errors.As(err, &invalid) {
		t.Errorf("a 500 from openvoxdb = %v, want an error that is not an InvalidQueryError", err)
	}

	srv.Close()
	_, err = client.Query(context.Background(), `nodes[certname] { }`)
	if err == nil || errors.As(err, &invalid) {
		t.Errorf("a refused connection = %v, want an error that is not an InvalidQueryError", err)
	}
	if err != nil && !strings.Contains(err.Error(), "unreachable") {
		t.Errorf("a refused connection = %v, want it to say unreachable", err)
	}
}
