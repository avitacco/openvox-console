package nodeagent

import "fmt"

// Config holds node-agent's startup configuration. CertFile, KeyFile,
// and CAFile point at the node's existing Puppet-issued
// certificate/key/CA - see design.md's "the node's existing Puppet
// client certificate authenticates the NATS connection directly"
// decision: this package never requests or manages a certificate
// itself, and does not parse puppet.conf to find them - the installer
// (agent-distribution) is responsible for resolving the node's actual
// certname/SSL paths and passing them in explicitly, mirroring
// internal/runtime.Config's OpenvoxdbCertFile/KeyFile/CAFile pattern.
type Config struct {
	TransportAddr string // "host:port" of the console's node transport
	CertFile      string
	KeyFile       string
	CAFile        string

	// PuppetBinPath is the puppet binary to invoke for run/task
	// requests. Optional - defaults to "puppet" (resolved via PATH).
	PuppetBinPath string

	// FactsDDir is Facter's external-facts directory, where the
	// package-inventory toggle writes/removes its fact script (see
	// design.md in add-package-inventory-toggle). Optional - defaults
	// to "/opt/puppetlabs/facter/facts.d", the real AIO path on Linux
	// and macOS. Explicit rather than OS-detected at runtime, matching
	// this package's existing pattern (see this type's own doc comment)
	// of the installer resolving real node-specific paths, not this
	// package guessing them.
	FactsDDir string
}

// LoadConfig reads configuration from environment variables via getenv,
// failing fast when a required value is missing - mirrors
// internal/runtime.LoadConfig's pattern.
func LoadConfig(getenv func(string) string) (Config, error) {
	cfg := Config{
		TransportAddr: getenv("NODE_AGENT_TRANSPORT_ADDR"),
		CertFile:      getenv("NODE_AGENT_CERT_FILE"),
		KeyFile:       getenv("NODE_AGENT_KEY_FILE"),
		CAFile:        getenv("NODE_AGENT_CA_FILE"),
		PuppetBinPath: getenv("NODE_AGENT_PUPPET_BIN_PATH"),
		FactsDDir:     getenv("NODE_AGENT_FACTS_D_DIR"),
	}
	if cfg.PuppetBinPath == "" {
		cfg.PuppetBinPath = "puppet"
	}
	if cfg.FactsDDir == "" {
		cfg.FactsDDir = "/opt/puppetlabs/facter/facts.d"
	}

	required := map[string]string{
		"NODE_AGENT_TRANSPORT_ADDR": cfg.TransportAddr,
		"NODE_AGENT_CERT_FILE":      cfg.CertFile,
		"NODE_AGENT_KEY_FILE":       cfg.KeyFile,
		"NODE_AGENT_CA_FILE":        cfg.CAFile,
	}
	for _, name := range []string{
		"NODE_AGENT_TRANSPORT_ADDR",
		"NODE_AGENT_CERT_FILE",
		"NODE_AGENT_KEY_FILE",
		"NODE_AGENT_CA_FILE",
	} {
		if required[name] == "" {
			return Config{}, fmt.Errorf("missing required configuration: %s", name)
		}
	}

	return cfg, nil
}
