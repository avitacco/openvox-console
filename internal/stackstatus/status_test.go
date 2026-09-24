package stackstatus_test

import (
	"strings"
	"testing"

	"github.com/voxpupuli/enterprise-console/internal/stackstatus"
)

// The status page shows a dependency's target so an operator can tell a
// misconfiguration from an outage. It must not also hand them the
// password - the first version of this page rendered the Postgres DSN
// verbatim to every holder of status:read.
func TestRedactTarget(t *testing.T) {
	tests := []struct {
		name      string
		in        string
		wantNot   string
		wantParts []string
	}{
		{
			name:      "postgres URL with password",
			in:        "postgres://console:s3cret@localhost:5432/console?sslmode=disable",
			wantNot:   "s3cret",
			wantParts: []string{"localhost:5432", "console", "sslmode=disable"},
		},
		{
			name:      "keyword form with password",
			in:        "host=db.example.com user=console password=s3cret dbname=console",
			wantNot:   "s3cret",
			wantParts: []string{"host=db.example.com", "dbname=console"},
		},
		{
			name:      "URL with no credentials is unchanged",
			in:        "https://openvoxdb:8081",
			wantParts: []string{"https://openvoxdb:8081"},
		},
		{
			name:      "plain value is unchanged",
			in:        "embedded",
			wantParts: []string{"embedded"},
		},
		{
			name: "empty stays empty",
			in:   "",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := stackstatus.RedactTarget(tc.in)

			if tc.wantNot != "" && strings.Contains(got, tc.wantNot) {
				t.Errorf("RedactTarget(%q) = %q, which still contains the secret %q", tc.in, got, tc.wantNot)
			}
			for _, part := range tc.wantParts {
				if !strings.Contains(got, part) {
					t.Errorf("RedactTarget(%q) = %q, which lost %q - the target must stay useful for diagnosis", tc.in, got, part)
				}
			}
			if tc.in == "" && got != "" {
				t.Errorf("RedactTarget(\"\") = %q, want empty", got)
			}
		})
	}
}
