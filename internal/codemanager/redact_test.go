package codemanager

import (
	"strings"
	"testing"
)

func TestRedactRemote(t *testing.T) {
	for _, tc := range []struct {
		name   string
		remote string
		want   string
	}{
		{
			name:   "https with token",
			remote: "https://x-access-token:ghp_SUPERSECRET@github.com/org/repo.git",
			want:   "https://REDACTED@github.com/org/repo.git",
		},
		{
			name:   "https with username and password",
			remote: "https://alice:hunter2@git.internal/org/repo.git",
			want:   "https://REDACTED@git.internal/org/repo.git",
		},
		{
			name:   "https with a username only",
			remote: "https://alice@git.internal/org/repo.git",
			want:   "https://REDACTED@git.internal/org/repo.git",
		},
		{
			name:   "https without credentials is untouched",
			remote: "https://github.com/org/repo.git",
			want:   "https://github.com/org/repo.git",
		},
		{
			// The SCP-like form has no password field, so its user@host
			// is an account name rather than a secret - and rewriting
			// it would leave an operator unable to recognise their own
			// remote.
			name:   "scp-style ssh remote is untouched",
			remote: "git@github.com:org/repo.git",
			want:   "git@github.com:org/repo.git",
		},
		{
			name:   "ssh:// url with a user",
			remote: "ssh://git@github.com/org/repo.git",
			want:   "ssh://REDACTED@github.com/org/repo.git",
		},
		{
			name:   "file url",
			remote: "file:///srv/control.git",
			want:   "file:///srv/control.git",
		},
		{
			name:   "unparseable string is passed through",
			remote: "://not a url at all",
			want:   "://not a url at all",
		},
		{
			name:   "empty",
			remote: "",
			want:   "",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := redactRemote(tc.remote)
			if got != tc.want {
				t.Errorf("redactRemote(%q) = %q, want %q", tc.remote, got, tc.want)
			}
		})
	}
}

func TestRedactRemote_NeverLeaksTheSecret(t *testing.T) {
	// The property that actually matters, asserted independently of the
	// exact output format above: whatever shape the remote takes, the
	// secret must not survive, and the repository must stay identifiable.
	const secret = "ghp_SUPERSECRET"
	for _, remote := range []string{
		"https://x-access-token:" + secret + "@github.com/org/repo.git",
		"https://" + secret + "@github.com/org/repo.git",
		"ssh://user:" + secret + "@git.internal:2222/org/repo.git",
	} {
		got := redactRemote(remote)
		if strings.Contains(got, secret) {
			t.Errorf("redactRemote(%q) leaked the secret: %q", remote, got)
		}
		if !strings.Contains(got, "org/repo.git") {
			t.Errorf("redactRemote(%q) = %q, which no longer identifies the repository", remote, got)
		}
	}
}
