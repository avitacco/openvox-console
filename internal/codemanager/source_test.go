package codemanager

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEffectivePrefix(t *testing.T) {
	// Mirrors g10k's own resolveSourcePrefix: true -> source name,
	// false/unset -> nothing, anything else used literally. If these
	// ever disagree, the console activates a symlink under a name g10k
	// did not write to and every deploy fails with "deployed directory
	// is missing".
	for _, tc := range []struct {
		name   string
		source Source
		want   string
		env    string
	}{
		{"unset", Source{Name: "team_a"}, "", "production"},
		{"false", Source{Name: "team_a", Prefix: "false"}, "", "production"},
		{"true", Source{Name: "team_a", Prefix: "true"}, "team_a_", "team_a_production"},
		{"literal", Source{Name: "team_a", Prefix: "infra"}, "infra_", "infra_production"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.source.EffectivePrefix(); got != tc.want {
				t.Errorf("EffectivePrefix() = %q, want %q", got, tc.want)
			}
			if got := tc.source.EnvironmentFor("production"); got != tc.env {
				t.Errorf("EnvironmentFor(production) = %q, want %q", got, tc.env)
			}
		})
	}
}

func TestLoadSourcesFile_ParsesEverySupportedForm(t *testing.T) {
	dir := t.TempDir()

	secretPath := filepath.Join(dir, "team_a.secret")
	// The trailing newline is the point: secret files nearly always
	// carry one, and one reaching an HMAC comparison rejects every
	// legitimate push.
	if err := os.WriteFile(secretPath, []byte("s3cret-from-file\n"), 0o600); err != nil {
		t.Fatalf("write secret file: %v", err)
	}

	path := filepath.Join(dir, "sources.yaml")
	contents := `
sources:
  control:
    remote: git@github.com:org/control-repo.git
    prefix: false
    webhook_secret: inline-secret
  team_a:
    remote: git@github.com:org/team-a.git
    prefix: true
    private_key: /etc/openvox-console/keys/team_a
    webhook_secret_file: ` + secretPath + `
`
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatalf("write sources file: %v", err)
	}

	sources, err := LoadSourcesFile(path)
	if err != nil {
		t.Fatalf("LoadSourcesFile() error: %v", err)
	}
	if len(sources) != 2 {
		t.Fatalf("got %d sources, want 2", len(sources))
	}

	// Name order, not YAML map order, so the UI list and error messages
	// are stable across restarts.
	if got := sources.Names(); got[0] != "control" || got[1] != "team_a" {
		t.Errorf("Names() = %v, want [control team_a]", got)
	}

	control := sources[0]
	if control.Remote != "git@github.com:org/control-repo.git" {
		t.Errorf("control remote = %q", control.Remote)
	}
	if control.EffectivePrefix() != "" {
		t.Errorf("control prefix = %q, want empty", control.EffectivePrefix())
	}
	if control.WebhookSecret != "inline-secret" {
		t.Errorf("control webhook secret = %q", control.WebhookSecret)
	}

	teamA := sources[1]
	if teamA.EnvironmentFor("production") != "team_a_production" {
		t.Errorf("team_a environment = %q", teamA.EnvironmentFor("production"))
	}
	if teamA.PrivateKey != "/etc/openvox-console/keys/team_a" {
		t.Errorf("team_a private key = %q", teamA.PrivateKey)
	}
	if teamA.WebhookSecret != "s3cret-from-file" {
		t.Errorf("team_a webhook secret = %q, want the file's contents with the newline trimmed", teamA.WebhookSecret)
	}
}

func TestLoadSourcesFile_Rejections(t *testing.T) {
	for _, tc := range []struct {
		name     string
		contents string
		wantErr  string
	}{
		{
			name:     "no sources declared",
			contents: "sources: {}\n",
			wantErr:  "declares no sources",
		},
		{
			name:     "misspelled key",
			contents: "sources:\n  control:\n    remotes: git@example.com:org/repo.git\n",
			wantErr:  "remotes",
		},
		{
			name:     "missing remote",
			contents: "sources:\n  control:\n    prefix: true\n",
			wantErr:  "has no remote",
		},
		{
			name:     "invalid name",
			contents: "sources:\n  team-a:\n    remote: git@example.com:org/repo.git\n",
			wantErr:  "invalid",
		},
		{
			// The collision this whole prefix mechanism exists to
			// prevent: both unprefixed, so both deploy production to
			// environments/production and one silently overwrites the
			// other's live code.
			name: "two unprefixed sources",
			contents: `
sources:
  control:
    remote: git@example.com:org/control.git
  team_a:
    remote: git@example.com:org/team-a.git
`,
			wantErr: "both unprefixed",
		},
		{
			name: "two sources sharing a literal prefix",
			contents: `
sources:
  control:
    remote: git@example.com:org/control.git
    prefix: infra
  team_a:
    remote: git@example.com:org/team-a.git
    prefix: infra
`,
			wantErr: "same environment prefix",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "sources.yaml")
			if err := os.WriteFile(path, []byte(tc.contents), 0o600); err != nil {
				t.Fatalf("write sources file: %v", err)
			}
			_, err := LoadSourcesFile(path)
			if err == nil {
				t.Fatal("LoadSourcesFile() succeeded, want an error")
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Errorf("error = %q, want it to mention %q", err, tc.wantErr)
			}
		})
	}
}

func TestLoadSourcesFile_UnreadableSecretFileFailsAtStartup(t *testing.T) {
	// A webhook whose secret never loaded would reject every push with
	// a signature mismatch, which looks like a misconfigured git host
	// rather than a missing file on the console.
	path := filepath.Join(t.TempDir(), "sources.yaml")
	contents := "sources:\n  control:\n    remote: git@example.com:org/control.git\n    webhook_secret_file: /nonexistent/secret\n"
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatalf("write sources file: %v", err)
	}

	_, err := LoadSourcesFile(path)
	if err == nil {
		t.Fatal("LoadSourcesFile() succeeded, want an error")
	}
	if !strings.Contains(err.Error(), "control") || !strings.Contains(err.Error(), "webhook_secret_file") {
		t.Errorf("error = %q, want it to name both the source and the setting", err)
	}
}

func TestSourcesFindAndDefault(t *testing.T) {
	sources := Sources{
		{Name: "team_a", Remote: "git@example.com:org/a.git", Prefix: "true"},
		{Name: "control", Remote: "git@example.com:org/control.git"},
	}

	// "control" wins over declaration order: an unsuffixed webhook or a
	// bare manual trigger on an upgraded single-repo install must keep
	// hitting the repo it always did.
	def, ok := sources.Default()
	if !ok || def.Name != "control" {
		t.Errorf("Default() = %q, %v; want control", def.Name, ok)
	}

	if got, ok := sources.Find(""); !ok || got.Name != "control" {
		t.Errorf(`Find("") = %q, %v; want the default source`, got.Name, ok)
	}
	if got, ok := sources.Find("team_a"); !ok || got.Name != "team_a" {
		t.Errorf(`Find("team_a") = %q, %v`, got.Name, ok)
	}
	if _, ok := sources.Find("nope"); ok {
		t.Error(`Find("nope") succeeded, want not found`)
	}

	if _, ok := (Sources{}).Default(); ok {
		t.Error("Default() on an empty set succeeded, want not found")
	}
	if _, ok := (Sources{}).Find(""); ok {
		t.Error(`Find("") on an empty set succeeded, want not found`)
	}
}

func TestSourcesDefault_FallsBackToFirstDeclared(t *testing.T) {
	// A sources file need not declare a "control" at all; without this
	// fallback the unsuffixed webhook route would have nothing to aim at.
	sources := Sources{
		{Name: "alpha", Remote: "git@example.com:org/alpha.git"},
		{Name: "beta", Remote: "git@example.com:org/beta.git", Prefix: "true"},
	}
	def, ok := sources.Default()
	if !ok || def.Name != "alpha" {
		t.Errorf("Default() = %q, %v; want alpha", def.Name, ok)
	}
}
