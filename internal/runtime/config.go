// Package runtime provides the console binary's own startup behavior:
// configuration loading, structured logging, and the health check endpoint.
package runtime

import (
	"encoding/base64"
	"fmt"
	"os"
	"strings"

	"github.com/voxpupuli/enterprise-console/internal/auditlog"
	"github.com/voxpupuli/enterprise-console/internal/sealer"
)

// Config holds the console binary's startup configuration.
type Config struct {
	HTTPAddr    string
	PostgresDSN string

	OpenvoxdbURL      string
	OpenvoxdbCertFile string
	OpenvoxdbKeyFile  string
	OpenvoxdbCAFile   string

	RBACSigningKeyFile string

	// Optional: JWT signing key rotation (see internal/rbac/keys.go and
	// operations.md's rotation runbook). RBACSigningKeyID defaults to a
	// fixed value if unset, so an existing single-key deployment's
	// already-issued tokens (no kid in their header) keep verifying
	// unchanged. RBACVerificationKeysDir is unset by default (no extra
	// verification keys beyond the active signer's own).
	RBACSigningKeyID        string
	RBACVerificationKeysDir string

	// Optional: only used to bootstrap the very first admin user when the
	// users table is empty. Leaving these unset is fine - bootstrap is
	// simply skipped, matching a database that already has users.
	BootstrapAdminUsername string
	BootstrapAdminPassword string

	// Optional: OIDC login. Leaving OIDCIssuer unset disables OIDC
	// entirely - local username/password login and service tokens are
	// unaffected either way.
	OIDCIssuer        string
	OIDCClientID      string
	OIDCClientSecret  string
	OIDCRedirectURL   string
	OIDCScopes        string
	OIDCUsernameClaim string
	OIDCRoleClaim     string
	OIDCRoleMapping   string

	// Optional: code deployment. Leaving G10KBinPath unset, or both
	// ControlRepoURL and CodeSourcesPath unset, disables deploy
	// triggers - code-manager endpoints error per request rather than
	// the console failing to start, matching OIDC's "never fatal at
	// startup" posture.
	//
	// ControlRepoURL and CodeSourcesPath are the two ways to declare
	// what to deploy from, and are mutually exclusive: the first is a
	// single control repo, the second a file declaring any number of
	// them. Setting both is a startup error rather than a merge, so
	// that reading one of them always tells you the whole answer.
	G10KBinPath       string
	ControlRepoURL    string
	CodeSourcesPath   string
	CodeWebhookSecret string
	CodeDirPath       string

	// Optional: the NATS-based node transport agents connect to for
	// on-demand orchestration. Leaving NodeTransportAddr unset disables
	// it entirely - no node can ever be "connected", so triggered jobs
	// simply fail every target with "not connected" rather than the
	// console failing to start (same posture as G10K above).
	// NodeTransportAddr is what the embedded NATS server binds/listens
	// on (e.g. ":7422" - all interfaces); NodeTransportPublicAddr is the
	// "host:port" a *remote* node should dial, which can differ (e.g. a
	// bind address has no useful host part for a remote client) -
	// defaults to NodeTransportAddr, which is only correct when that
	// already names a resolvable host.
	NodeTransportAddr       string
	NodeTransportPublicAddr string
	NodeTransportCertFile   string
	NodeTransportKeyFile    string
	NodeTransportCAFile     string

	// Optional: the Puppet server address a *remote node* should enroll
	// against and run against, written into the generated install
	// script. Node-facing in the same sense as
	// NodeTransportPublicAddr above, and deliberately separate from
	// CAClientURL below: that one is this console's own client view of
	// the CA API and is routinely a loopback or container-internal
	// address, which would be actively wrong to write into a remote
	// node's puppet.conf. Leaving this unset makes the install script
	// leave a node's existing Puppet server configuration alone rather
	// than substituting a guessed address.
	PuppetServerPublicAddr string

	// Optional: certificate status reporting (internal/certstatus),
	// querying openvoxserver's own Puppet Server CA API. Leaving
	// CAClientURL unset disables it entirely - every node's certificate
	// status simply reports as "unknown" rather than the console failing
	// to start. CAClientCertFile/KeyFile must carry the pp_cli_auth
	// authorization extension (full CA admin access, not a narrower
	// read-only role - openvoxserver's own auth.conf has no such
	// narrower grant) - see operations.md's runbook for how this
	// credential is generated and why it's kept separate from every
	// other credential this project holds.
	CAClientURL      string
	CAClientCertFile string
	CAClientKeyFile  string
	CAClientCAFile   string

	// Optional: this console's own externally-reachable base URL,
	// embedded into the agent-distribution install script so a node
	// can fetch its node-agent-client binary back from this console.
	// Defaults to "http://localhost<HTTPAddr>" - fine for local dev,
	// but a real deployment behind any proxy/DNS name should set this
	// explicitly.
	ConsoleBaseURL string

	// Optional: per-category audit event emission level (see
	// internal/auditlog) - each defaults to "writes" (mutating actions
	// and authentication events; see configurable-audit-logging's
	// design.md) if unset, independent of the others.
	AuditNodes           auditlog.Level
	AuditClassifier      auditlog.Level
	AuditRBAC            auditlog.Level
	AuditAuth            auditlog.Level
	AuditCode            auditlog.Level
	AuditOrchestrator    auditlog.Level
	AuditVulnerabilities auditlog.Level

	// Optional: file path to emit audit events to instead of the
	// console's standard log output. Empty means stdout, alongside
	// ordinary operational logs (distinguished by category/action
	// fields, not a separate stream).
	AuditLogPath string

	// Optional: the key sealing secrets stored in Postgres - vulnerability
	// provider credentials, today (see internal/sealer). sealer.KeySize
	// bytes, supplied base64-encoded via CONSOLE_SECRETS_KEY or, better,
	// CONSOLE_SECRETS_KEY_FILE. Unset means no provider type that needs
	// credentials can be configured; nothing else is affected.
	SecretsKey []byte
}

// LoadConfig reads configuration from environment variables via getenv,
// applying defaults where sensible and failing fast when a required value
// is missing.
//
// Any setting can be supplied from a file instead of the environment by
// setting <NAME>_FILE to a readable path - the convention Docker secrets
// (/run/secrets/...) and Kubernetes projected volumes both produce,
// letting a password or token stay out of the process environment, out
// of `docker inspect`, and out of a compose file. <NAME>_FILE wins when
// both are set, since naming a file is the more explicit instruction. A
// file that is named but cannot be read is fatal rather than silently
// falling back to the environment: a deployment that meant to read a
// secret from disk should fail loudly, not start with the value empty
// (or, worse, with a stale one from the environment).
func LoadConfig(getenv func(string) string) (Config, error) {
	var fileErr error
	env := getenv
	getenv = func(name string) string {
		path := env(name + "_FILE")
		if path == "" {
			return env(name)
		}
		contents, err := os.ReadFile(path)
		if err != nil {
			if fileErr == nil {
				fileErr = fmt.Errorf("%s_FILE: %w", name, err)
			}
			return ""
		}
		// Trailing newlines are near-universal in secret files (editors
		// add them, `echo` adds one, Docker preserves whatever it was
		// given); a password carrying one fails authentication in a way
		// that is genuinely hard to see.
		return strings.TrimSpace(string(contents))
	}

	cfg := Config{
		HTTPAddr:    getenv("CONSOLE_HTTP_ADDR"),
		PostgresDSN: getenv("CONSOLE_POSTGRES_DSN"),

		OpenvoxdbURL:      getenv("CONSOLE_OPENVOXDB_URL"),
		OpenvoxdbCertFile: getenv("CONSOLE_OPENVOXDB_CERT_FILE"),
		OpenvoxdbKeyFile:  getenv("CONSOLE_OPENVOXDB_KEY_FILE"),
		OpenvoxdbCAFile:   getenv("CONSOLE_OPENVOXDB_CA_FILE"),

		RBACSigningKeyFile: getenv("CONSOLE_RBAC_SIGNING_KEY_FILE"),

		RBACSigningKeyID:        getenv("CONSOLE_RBAC_SIGNING_KEY_ID"),
		RBACVerificationKeysDir: getenv("CONSOLE_RBAC_VERIFICATION_KEYS_DIR"),

		BootstrapAdminUsername: getenv("CONSOLE_BOOTSTRAP_ADMIN_USERNAME"),
		BootstrapAdminPassword: getenv("CONSOLE_BOOTSTRAP_ADMIN_PASSWORD"),

		OIDCIssuer:        getenv("CONSOLE_OIDC_ISSUER"),
		OIDCClientID:      getenv("CONSOLE_OIDC_CLIENT_ID"),
		OIDCClientSecret:  getenv("CONSOLE_OIDC_CLIENT_SECRET"),
		OIDCRedirectURL:   getenv("CONSOLE_OIDC_REDIRECT_URL"),
		OIDCScopes:        getenv("CONSOLE_OIDC_SCOPES"),
		OIDCUsernameClaim: getenv("CONSOLE_OIDC_USERNAME_CLAIM"),
		OIDCRoleClaim:     getenv("CONSOLE_OIDC_ROLE_CLAIM"),
		OIDCRoleMapping:   getenv("CONSOLE_OIDC_ROLE_MAPPING"),

		G10KBinPath:       getenv("CONSOLE_G10K_BIN_PATH"),
		ControlRepoURL:    getenv("CONSOLE_CONTROL_REPO_URL"),
		CodeSourcesPath:   getenv("CONSOLE_CODE_SOURCES_PATH"),
		CodeWebhookSecret: getenv("CONSOLE_CODE_WEBHOOK_SECRET"),
		CodeDirPath:       getenv("CONSOLE_CODE_DIR_PATH"),

		NodeTransportAddr:       getenv("CONSOLE_NODE_TRANSPORT_ADDR"),
		NodeTransportPublicAddr: getenv("CONSOLE_NODE_TRANSPORT_PUBLIC_ADDR"),
		PuppetServerPublicAddr:  getenv("CONSOLE_PUPPET_SERVER_PUBLIC_ADDR"),
		NodeTransportCertFile:   getenv("CONSOLE_NODE_TRANSPORT_CERT_FILE"),
		NodeTransportKeyFile:    getenv("CONSOLE_NODE_TRANSPORT_KEY_FILE"),
		NodeTransportCAFile:     getenv("CONSOLE_NODE_TRANSPORT_CA_FILE"),

		CAClientURL:      getenv("CONSOLE_CA_CLIENT_URL"),
		CAClientCertFile: getenv("CONSOLE_CA_CLIENT_CERT_FILE"),
		CAClientKeyFile:  getenv("CONSOLE_CA_CLIENT_KEY_FILE"),
		CAClientCAFile:   getenv("CONSOLE_CA_CLIENT_CA_FILE"),

		ConsoleBaseURL: getenv("CONSOLE_BASE_URL"),

		AuditLogPath: getenv("CONSOLE_AUDIT_LOG_PATH"),
	}

	auditLevels := []struct {
		env  string
		dest *auditlog.Level
	}{
		{"CONSOLE_AUDIT_NODES", &cfg.AuditNodes},
		{"CONSOLE_AUDIT_CLASSIFIER", &cfg.AuditClassifier},
		{"CONSOLE_AUDIT_RBAC", &cfg.AuditRBAC},
		{"CONSOLE_AUDIT_AUTH", &cfg.AuditAuth},
		{"CONSOLE_AUDIT_CODE", &cfg.AuditCode},
		{"CONSOLE_AUDIT_ORCHESTRATOR", &cfg.AuditOrchestrator},
		{"CONSOLE_AUDIT_VULNERABILITIES", &cfg.AuditVulnerabilities},
	}
	for _, a := range auditLevels {
		raw := getenv(a.env)
		if raw == "" {
			*a.dest = auditlog.LevelWrites
			continue
		}
		level, err := auditlog.ParseLevel(raw)
		if err != nil {
			return Config{}, fmt.Errorf("%s: %w", a.env, err)
		}
		*a.dest = level
	}

	if cfg.HTTPAddr == "" {
		cfg.HTTPAddr = ":8080"
	}
	if cfg.OIDCScopes == "" {
		cfg.OIDCScopes = "openid profile email groups"
	}
	if cfg.OIDCUsernameClaim == "" {
		cfg.OIDCUsernameClaim = "email"
	}
	if cfg.OIDCRoleClaim == "" {
		cfg.OIDCRoleClaim = "groups"
	}
	if cfg.CodeDirPath == "" {
		cfg.CodeDirPath = "openvox-code"
	}
	if cfg.RBACSigningKeyID == "" {
		cfg.RBACSigningKeyID = "default"
	}
	if cfg.ConsoleBaseURL == "" {
		cfg.ConsoleBaseURL = "http://localhost" + cfg.HTTPAddr
	}
	if cfg.NodeTransportPublicAddr == "" {
		cfg.NodeTransportPublicAddr = cfg.NodeTransportAddr
	}

	// Read before the fileErr check so an unreadable
	// CONSOLE_SECRETS_KEY_FILE is reported as itself; decoded after it.
	secretsKey := getenv("CONSOLE_SECRETS_KEY")

	// Checked before the required-value loop below so an unreadable
	// secret file is reported as itself, rather than as whichever
	// setting it happened to leave empty.
	if fileErr != nil {
		return Config{}, fileErr
	}

	if secretsKey != "" {
		key, err := base64.StdEncoding.DecodeString(secretsKey)
		if err != nil {
			return Config{}, fmt.Errorf("CONSOLE_SECRETS_KEY: not valid base64: %w", err)
		}
		if len(key) != sealer.KeySize {
			return Config{}, fmt.Errorf("CONSOLE_SECRETS_KEY: must decode to %d bytes, got %d", sealer.KeySize, len(key))
		}
		cfg.SecretsKey = key
	}

	// Refused rather than merged: were both honoured, the same repo
	// could be declared twice with different prefixes, and reading the
	// sources file alone would not tell you what actually deploys.
	if cfg.ControlRepoURL != "" && cfg.CodeSourcesPath != "" {
		return Config{}, fmt.Errorf(
			"CONSOLE_CONTROL_REPO_URL and CONSOLE_CODE_SOURCES_PATH are mutually exclusive: " +
				"declare the single control repo in the sources file and unset CONSOLE_CONTROL_REPO_URL, or unset CONSOLE_CODE_SOURCES_PATH")
	}

	required := map[string]string{
		"CONSOLE_POSTGRES_DSN":          cfg.PostgresDSN,
		"CONSOLE_OPENVOXDB_URL":         cfg.OpenvoxdbURL,
		"CONSOLE_OPENVOXDB_CERT_FILE":   cfg.OpenvoxdbCertFile,
		"CONSOLE_OPENVOXDB_KEY_FILE":    cfg.OpenvoxdbKeyFile,
		"CONSOLE_OPENVOXDB_CA_FILE":     cfg.OpenvoxdbCAFile,
		"CONSOLE_RBAC_SIGNING_KEY_FILE": cfg.RBACSigningKeyFile,
	}
	for _, name := range []string{
		"CONSOLE_POSTGRES_DSN",
		"CONSOLE_OPENVOXDB_URL",
		"CONSOLE_OPENVOXDB_CERT_FILE",
		"CONSOLE_OPENVOXDB_KEY_FILE",
		"CONSOLE_OPENVOXDB_CA_FILE",
		"CONSOLE_RBAC_SIGNING_KEY_FILE",
	} {
		if required[name] == "" {
			return Config{}, fmt.Errorf("missing required configuration: %s", name)
		}
	}

	return cfg, nil
}
