package nodeagent

import (
	"fmt"
	"path/filepath"
	"runtime"
	"strings"
)

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

	// PuppetServer ("host:port") is where task files are downloaded
	// from. Optional - unset, the node's own Puppet configuration is
	// asked (see PuppetServerFromConfig), which is right for every node
	// except one finding its server through SRV records.
	PuppetServer string

	// TaskCacheDir holds downloaded task files, keyed by checksum (see
	// taskcache.go). Optional - defaults per OS (see defaultTaskCacheDir).
	TaskCacheDir string

	// AcceptUpgrades is whether this client installs a newer build when
	// the console asks (see upgrade.go). On unless
	// NODE_AGENT_ACCEPT_UPGRADES is set to a false value, which is how a
	// node opts out of automatic upgrades.
	AcceptUpgrades bool

	// UpgradeDir is where an upgrade artifact is downloaded and checked
	// before it is installed. Optional - defaults per OS, beside the
	// task cache (see defaultUpgradeDir).
	UpgradeDir string
}

// LoadConfig reads configuration from environment variables via getenv,
// failing fast when a required value is missing - mirrors
// internal/runtime.LoadConfig's pattern.
func LoadConfig(getenv func(string) string) (Config, error) {
	return loadConfig(getenv, runtime.GOOS)
}

// defaultTaskCacheDir is the task cache's default location on goos: next
// to the agent's own data, where only an administrator can write.
func defaultTaskCacheDir(getenv func(string) string, goos string) string {
	if goos == "windows" {
		programData := getenv("ProgramData")
		if programData == "" {
			programData = `C:\ProgramData`
		}
		// Joined with a literal separator: this may run (in tests) on a
		// platform whose filepath.Join would use the other one.
		return programData + `\PuppetLabs\node-agent\task-cache`
	}
	return filepath.Join("/opt/puppetlabs/node-agent", "task-cache")
}

// defaultUpgradeDir is where upgrade artifacts are staged on goos. It is
// created private to administrators each time it is used.
func defaultUpgradeDir(getenv func(string) string, goos string) string {
	if goos == "windows" {
		programData := getenv("ProgramData")
		if programData == "" {
			programData = `C:\ProgramData`
		}
		return programData + `\PuppetLabs\node-agent\upgrade`
	}
	return filepath.Join("/opt/puppetlabs/node-agent", "upgrade")
}

// parseAccept reads an on/off setting that is on unless clearly turned
// off, so a typo cannot silently opt a node out.
func parseAccept(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "false", "0", "no", "off":
		return false
	}
	return true
}

func loadConfig(getenv func(string) string, goos string) (Config, error) {
	cfg := Config{
		TransportAddr: getenv("NODE_AGENT_TRANSPORT_ADDR"),
		CertFile:      getenv("NODE_AGENT_CERT_FILE"),
		KeyFile:       getenv("NODE_AGENT_KEY_FILE"),
		CAFile:        getenv("NODE_AGENT_CA_FILE"),
		PuppetBinPath: getenv("NODE_AGENT_PUPPET_BIN_PATH"),
		FactsDDir:     getenv("NODE_AGENT_FACTS_D_DIR"),
		PuppetServer:  getenv("NODE_AGENT_PUPPET_SERVER"),
		TaskCacheDir:  getenv("NODE_AGENT_TASK_CACHE_DIR"),

		AcceptUpgrades: parseAccept(getenv("NODE_AGENT_ACCEPT_UPGRADES")),
		UpgradeDir:     getenv("NODE_AGENT_UPGRADE_DIR"),
	}
	if cfg.UpgradeDir == "" {
		cfg.UpgradeDir = defaultUpgradeDir(getenv, goos)
	}
	if cfg.TaskCacheDir == "" {
		cfg.TaskCacheDir = defaultTaskCacheDir(getenv, goos)
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
