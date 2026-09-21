package codemanager

import (
	"fmt"
	"os"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// sourcesFile is the on-disk shape of the sources file:
//
//	sources:
//	  control:
//	    remote: git@github.com:org/control-repo.git
//	  team_a:
//	    remote: git@github.com:org/team-a.git
//	    prefix: true
//	    private_key: /etc/openvox-console/keys/team_a
//	    webhook_secret_file: /run/secrets/team_a_webhook
type sourcesFile struct {
	Sources map[string]Source `yaml:"sources"`
}

// LoadSourcesFile reads and validates the sources file at path. Sources
// are returned in name order: a YAML mapping has no meaningful order of
// its own, and a stable one keeps the UI's source list and any error
// message naming "the first" source from moving around between restarts.
//
// A webhook_secret_file is read here rather than at verification time so
// an unreadable one is a startup failure naming the source, not a
// webhook that quietly rejects every push it receives.
func LoadSourcesFile(path string) (Sources, error) {
	contents, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read code sources file: %w", err)
	}

	var parsed sourcesFile
	decoder := yaml.NewDecoder(strings.NewReader(string(contents)))
	// Surfaces a misspelled key as an error instead of silently
	// ignoring it - a mistyped `remotes:` would otherwise read as a
	// source with no remote, and a mistyped `webhook_secret` as a
	// webhook nothing can authenticate against.
	decoder.KnownFields(true)
	if err := decoder.Decode(&parsed); err != nil {
		return nil, fmt.Errorf("parse code sources file %s: %w", path, err)
	}

	if len(parsed.Sources) == 0 {
		return nil, fmt.Errorf("code sources file %s declares no sources", path)
	}

	names := make([]string, 0, len(parsed.Sources))
	for name := range parsed.Sources {
		names = append(names, name)
	}
	sort.Strings(names)

	sources := make(Sources, 0, len(names))
	for _, name := range names {
		src := parsed.Sources[name]
		src.Name = name

		if src.WebhookSecretFile != "" {
			secret, err := os.ReadFile(src.WebhookSecretFile)
			if err != nil {
				return nil, fmt.Errorf("code source %q: read webhook_secret_file: %w", name, err)
			}
			// Trailing newlines are near-universal in secret files;
			// one reaching an HMAC comparison fails every signature in
			// a way that is genuinely hard to see.
			src.WebhookSecret = strings.TrimSpace(string(secret))
		}

		sources = append(sources, src)
	}

	if err := sources.Validate(); err != nil {
		return nil, fmt.Errorf("code sources file %s: %w", path, err)
	}
	return sources, nil
}
