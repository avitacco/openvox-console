package codemanager

import (
	"fmt"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"
)

// DefaultSourceName is the name given to the source built from the
// single-repo CONSOLE_CONTROL_REPO_URL configuration, and the source a
// trigger targets when it names none. Deploy records written before
// multi-source support existed were all this source, which is why the
// deploys.source column defaults to it.
const DefaultSourceName = "control"

// sourceNamePattern is what a source name may contain. A source's name
// can become part of a Puppet environment name (see Prefix), and Puppet
// rejects environment names outside [a-z0-9_] - so a source named
// "team-a" would deploy into a directory openvoxserver then refuses to
// load, with nothing in the deploy itself reporting a problem.
var sourceNamePattern = regexp.MustCompile(`^[a-z0-9_]+$`)

// Prefix is a source's environment-name prefix setting. It accepts the
// three forms g10k's own config does - the boolean true (prefix with
// the source's name), the boolean false or nothing at all (no prefix),
// or a literal string (prefix with that) - which is why it is neither a
// plain bool nor a plain string.
type Prefix string

// Prefix values with a meaning beyond "use this literal string".
const (
	prefixTrue  Prefix = "true"
	prefixFalse Prefix = "false"
)

// UnmarshalYAML accepts `prefix: true`, `prefix: false` and
// `prefix: some_string`. Without this, YAML's native booleans would
// fail to decode into a string-shaped field, and requiring operators to
// quote `"true"` would be a papercut with no upside.
func (p *Prefix) UnmarshalYAML(node *yaml.Node) error {
	var b bool
	if err := node.Decode(&b); err == nil {
		if b {
			*p = prefixTrue
		} else {
			*p = prefixFalse
		}
		return nil
	}

	var s string
	if err := node.Decode(&s); err != nil {
		return fmt.Errorf("prefix must be true, false, or a string")
	}
	*p = Prefix(s)
	return nil
}

// Source is one control repo the console can deploy from.
type Source struct {
	// Name identifies the source in triggers, deploy history and
	// webhook URLs, and is what `prefix: true` prefixes with.
	Name string `yaml:"-"`

	// Remote is the git URL g10k resolves against.
	Remote string `yaml:"remote"`

	// Prefix decides the environment names this source's branches
	// produce - see EffectivePrefix.
	Prefix Prefix `yaml:"prefix"`

	// PrivateKey is an optional path to an SSH key used for this
	// source's remote, passed through to g10k as the source's
	// private_key. Per-source rather than global so two repos can use
	// different deploy keys.
	PrivateKey string `yaml:"private_key"`

	// WebhookSecret is the HMAC secret this source's webhook endpoint
	// verifies against. Per-source so a secret leaked from one repo
	// cannot trigger another's deploy.
	WebhookSecret string `yaml:"webhook_secret"`

	// WebhookSecretFile reads WebhookSecret from a file instead, the
	// same convention CONSOLE_*_FILE follows for Docker/systemd
	// secrets. It wins over WebhookSecret when both are set.
	WebhookSecretFile string `yaml:"webhook_secret_file"`
}

// EffectivePrefix returns the string prepended to a branch name to get
// its Puppet environment name. It mirrors g10k's own prefix resolution
// (resolveSourcePrefix) exactly, so the console and g10k always agree
// on where a deploy lands without the console reimplementing it:
// "true" prefixes with the source name, "false" or unset does not
// prefix at all, and anything else is used literally. Each non-empty
// result carries its own trailing underscore, so the caller just
// concatenates.
func (s Source) EffectivePrefix() string {
	switch s.Prefix {
	case "", prefixFalse:
		return ""
	case prefixTrue:
		return s.Name + "_"
	default:
		return string(s.Prefix) + "_"
	}
}

// EnvironmentFor returns the Puppet environment name that deploying
// branch from this source produces - the directory g10k writes and the
// name the console activates it under.
func (s Source) EnvironmentFor(branch string) string {
	return s.EffectivePrefix() + branch
}

// Sources is the set of control repos configured for deployment, in the
// order they were declared.
type Sources []Source

// Find returns the source named name. A caller passing "" gets the
// default source, so a trigger that names no source targets the one a
// single-repo installation has.
func (s Sources) Find(name string) (Source, bool) {
	if len(s) == 0 {
		return Source{}, false
	}
	if name == "" {
		return s.Default()
	}
	for _, src := range s {
		if src.Name == name {
			return src, true
		}
	}
	return Source{}, false
}

// Default returns the source a trigger naming none targets: the one
// called "control" if present, otherwise the first declared. The
// fallback matters for a sources file that never declares a "control"
// at all - without it, an unsuffixed webhook or a bare manual trigger
// would have nothing to aim at.
func (s Sources) Default() (Source, bool) {
	if len(s) == 0 {
		return Source{}, false
	}
	for _, src := range s {
		if src.Name == DefaultSourceName {
			return src, true
		}
	}
	return s[0], true
}

// Names returns every configured source name, in declaration order.
func (s Sources) Names() []string {
	names := make([]string, 0, len(s))
	for _, src := range s {
		names = append(names, src.Name)
	}
	return names
}

// Validate reports why this set of sources cannot be deployed from.
//
// The prefix rule is the load-bearing one: two sources sharing an
// effective prefix collide the moment they share a branch name, and the
// collision is one source's deploy overwriting another's live code.
// Branch names are not knowable at startup but prefixes are, so the
// check happens here - refusing to start - rather than at deploy time,
// when the only thing left to do is refuse anyway and production code
// is already at risk. In practice this means at most one source may be
// unprefixed.
func (s Sources) Validate() error {
	seenName := make(map[string]bool, len(s))
	seenPrefix := make(map[string]string, len(s))

	for _, src := range s {
		if !sourceNamePattern.MatchString(src.Name) {
			return fmt.Errorf("code source name %q is invalid: names may contain only lowercase letters, digits and underscores, because a source name can become part of a Puppet environment name", src.Name)
		}
		if seenName[src.Name] {
			return fmt.Errorf("code source %q is declared more than once", src.Name)
		}
		seenName[src.Name] = true

		if strings.TrimSpace(src.Remote) == "" {
			return fmt.Errorf("code source %q has no remote", src.Name)
		}

		prefix := src.EffectivePrefix()
		if other, clash := seenPrefix[prefix]; clash {
			if prefix == "" {
				return fmt.Errorf("code sources %q and %q are both unprefixed, so a branch of the same name in each would deploy to the same environment: give at least one of them a prefix", other, src.Name)
			}
			return fmt.Errorf("code sources %q and %q resolve to the same environment prefix %q, so a branch of the same name in each would deploy to the same environment", other, src.Name, prefix)
		}
		seenPrefix[prefix] = src.Name
	}
	return nil
}
