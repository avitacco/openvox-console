package runtime

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/voxpupuli/enterprise-console/internal/auditlog"
)

func envMap(m map[string]string) func(string) string {
	return func(key string) string { return m[key] }
}

func validEnv(overrides map[string]string) map[string]string {
	env := map[string]string{
		"CONSOLE_POSTGRES_DSN":          "postgres://localhost/console",
		"CONSOLE_OPENVOXDB_URL":         "https://localhost:8081",
		"CONSOLE_OPENVOXDB_CERT_FILE":   "certs/console-cert.pem",
		"CONSOLE_OPENVOXDB_KEY_FILE":    "certs/console-key.pem",
		"CONSOLE_OPENVOXDB_CA_FILE":     "certs/ca.pem",
		"CONSOLE_RBAC_SIGNING_KEY_FILE": "certs/rbac-signing-key.pem",
	}
	for k, v := range overrides {
		env[k] = v
	}
	return env
}

// modeEnv returns the overrides a given mode needs on top of validEnv:
// verification keys for a mode that issues no tokens, and a transport
// listener for orchestrator.
func modeEnv(mode Mode) map[string]string {
	env := map[string]string{"CONSOLE_RUN_MODE": string(mode)}
	if !mode.IssuesTokens() {
		env["CONSOLE_RBAC_VERIFICATION_KEYS_DIR"] = "certs/rbac-keys"
	}
	if mode == ModeOrchestrator {
		env["CONSOLE_NODE_TRANSPORT_ADDR"] = ":7422"
	}
	return env
}

func TestLoadConfig_Valid(t *testing.T) {
	cfg, err := LoadConfig(envMap(validEnv(nil)))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.PostgresDSN != "postgres://localhost/console" {
		t.Errorf("PostgresDSN = %q", cfg.PostgresDSN)
	}
	if cfg.OpenvoxdbURL != "https://localhost:8081" {
		t.Errorf("OpenvoxdbURL = %q", cfg.OpenvoxdbURL)
	}
	if cfg.HTTPAddr != ":8080" {
		t.Errorf("expected default HTTPAddr, got %q", cfg.HTTPAddr)
	}
}

func TestLoadConfig_RBACKeyRotationUnsetByDefault(t *testing.T) {
	cfg, err := LoadConfig(envMap(validEnv(nil)))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.RBACSigningKeyID != "default" {
		t.Errorf("RBACSigningKeyID = %q, want the fixed default so existing no-kid tokens keep verifying", cfg.RBACSigningKeyID)
	}
	if cfg.RBACVerificationKeysDir != "" {
		t.Errorf("RBACVerificationKeysDir = %q, want empty", cfg.RBACVerificationKeysDir)
	}
}

func TestLoadConfig_RBACKeyRotationConfigured(t *testing.T) {
	cfg, err := LoadConfig(envMap(validEnv(map[string]string{
		"CONSOLE_RBAC_SIGNING_KEY_ID":        "2026-08",
		"CONSOLE_RBAC_VERIFICATION_KEYS_DIR": "certs/rbac-verification-keys",
	})))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.RBACSigningKeyID != "2026-08" {
		t.Errorf("RBACSigningKeyID = %q, want 2026-08", cfg.RBACSigningKeyID)
	}
	if cfg.RBACVerificationKeysDir != "certs/rbac-verification-keys" {
		t.Errorf("RBACVerificationKeysDir = %q, want certs/rbac-verification-keys", cfg.RBACVerificationKeysDir)
	}
}

func TestLoadConfig_NodeTransportUnsetByDefault(t *testing.T) {
	cfg, err := LoadConfig(envMap(validEnv(nil)))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.NodeTransportAddr != "" {
		t.Errorf("NodeTransportAddr = %q, want empty (node transport inactive by default)", cfg.NodeTransportAddr)
	}
	if cfg.NodeTransportPublicAddr != "" {
		t.Errorf("NodeTransportPublicAddr = %q, want empty when NodeTransportAddr is also empty", cfg.NodeTransportPublicAddr)
	}
}

func TestLoadConfig_NodeTransportConfigured(t *testing.T) {
	cfg, err := LoadConfig(envMap(validEnv(map[string]string{
		"CONSOLE_NODE_TRANSPORT_ADDR":      ":7422",
		"CONSOLE_NODE_TRANSPORT_CERT_FILE": "certs/console-cert.pem",
		"CONSOLE_NODE_TRANSPORT_KEY_FILE":  "certs/console-key.pem",
		"CONSOLE_NODE_TRANSPORT_CA_FILE":   "certs/ca.pem",
	})))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.NodeTransportAddr != ":7422" {
		t.Errorf("NodeTransportAddr = %q, want :7422", cfg.NodeTransportAddr)
	}
	if cfg.NodeTransportPublicAddr != ":7422" {
		t.Errorf("NodeTransportPublicAddr = %q, want it to default to NodeTransportAddr", cfg.NodeTransportPublicAddr)
	}
	if cfg.NodeTransportCertFile != "certs/console-cert.pem" {
		t.Errorf("NodeTransportCertFile = %q", cfg.NodeTransportCertFile)
	}
}

func TestLoadConfig_PuppetServerPublicAddr(t *testing.T) {
	cfg, err := LoadConfig(envMap(validEnv(map[string]string{
		"CONSOLE_PUPPET_SERVER_PUBLIC_ADDR": "openvoxserver.example.com",
	})))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.PuppetServerPublicAddr != "openvoxserver.example.com" {
		t.Errorf("PuppetServerPublicAddr = %q, want the configured address", cfg.PuppetServerPublicAddr)
	}
}

// Unset must stay empty rather than falling back to CAClientURL's host:
// that value is the console's own view of the CA (routinely loopback or
// a container name) and writing it into a remote node's puppet.conf
// would point that node at an address it cannot reach - see design.md
// in add-install-script-auto-enrollment.
func TestLoadConfig_PuppetServerPublicAddrUnsetDoesNotFallBackToCAClientURL(t *testing.T) {
	cfg, err := LoadConfig(envMap(validEnv(map[string]string{
		"CONSOLE_CA_CLIENT_URL": "https://localhost:8140",
	})))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.PuppetServerPublicAddr != "" {
		t.Errorf("PuppetServerPublicAddr = %q, want empty when unset", cfg.PuppetServerPublicAddr)
	}
}

func TestLoadConfig_NodeTransportPublicAddrOverride(t *testing.T) {
	cfg, err := LoadConfig(envMap(validEnv(map[string]string{
		"CONSOLE_NODE_TRANSPORT_ADDR":        ":7422",
		"CONSOLE_NODE_TRANSPORT_PUBLIC_ADDR": "console.example.com:7422",
	})))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.NodeTransportPublicAddr != "console.example.com:7422" {
		t.Errorf("NodeTransportPublicAddr = %q, want the explicit override", cfg.NodeTransportPublicAddr)
	}
}

func TestLoadConfig_CAClientUnsetByDefault(t *testing.T) {
	cfg, err := LoadConfig(envMap(validEnv(nil)))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.CAClientURL != "" {
		t.Errorf("CAClientURL = %q, want empty (cert status reporting inactive by default)", cfg.CAClientURL)
	}
	if cfg.CAClientCertFile != "" || cfg.CAClientKeyFile != "" || cfg.CAClientCAFile != "" {
		t.Errorf("CAClient cert/key/CA = %q/%q/%q, want all empty", cfg.CAClientCertFile, cfg.CAClientKeyFile, cfg.CAClientCAFile)
	}
}

func TestLoadConfig_CAClientConfigured(t *testing.T) {
	cfg, err := LoadConfig(envMap(validEnv(map[string]string{
		"CONSOLE_CA_CLIENT_URL":       "https://openvoxserver:8140",
		"CONSOLE_CA_CLIENT_CERT_FILE": "certs/ca-client-cert.pem",
		"CONSOLE_CA_CLIENT_KEY_FILE":  "certs/ca-client-key.pem",
		"CONSOLE_CA_CLIENT_CA_FILE":   "certs/ca.pem",
	})))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.CAClientURL != "https://openvoxserver:8140" {
		t.Errorf("CAClientURL = %q", cfg.CAClientURL)
	}
	if cfg.CAClientCertFile != "certs/ca-client-cert.pem" {
		t.Errorf("CAClientCertFile = %q", cfg.CAClientCertFile)
	}
	if cfg.CAClientKeyFile != "certs/ca-client-key.pem" {
		t.Errorf("CAClientKeyFile = %q", cfg.CAClientKeyFile)
	}
	if cfg.CAClientCAFile != "certs/ca.pem" {
		t.Errorf("CAClientCAFile = %q", cfg.CAClientCAFile)
	}
}

func TestLoadConfig_MissingRequired(t *testing.T) {
	_, err := LoadConfig(envMap(nil))
	if err == nil {
		t.Fatal("expected an error for missing required configuration, got nil")
	}
}

func TestLoadConfig_MissingIndividualRequiredValues(t *testing.T) {
	for _, key := range []string{
		"CONSOLE_OPENVOXDB_URL",
		"CONSOLE_OPENVOXDB_CERT_FILE",
		"CONSOLE_OPENVOXDB_KEY_FILE",
		"CONSOLE_OPENVOXDB_CA_FILE",
		"CONSOLE_RBAC_SIGNING_KEY_FILE",
	} {
		env := validEnv(nil)
		delete(env, key)
		if _, err := LoadConfig(envMap(env)); err == nil {
			t.Errorf("expected an error when %s is missing, got nil", key)
		}
	}
}

func TestLoadConfig_OIDCUnsetByDefault(t *testing.T) {
	cfg, err := LoadConfig(envMap(validEnv(nil)))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.OIDCIssuer != "" {
		t.Errorf("OIDCIssuer = %q, want empty (OIDC inactive by default)", cfg.OIDCIssuer)
	}
	if cfg.OIDCScopes != "openid profile email groups" {
		t.Errorf("OIDCScopes = %q, want the default scope list", cfg.OIDCScopes)
	}
	if cfg.OIDCUsernameClaim != "email" {
		t.Errorf("OIDCUsernameClaim = %q, want default %q", cfg.OIDCUsernameClaim, "email")
	}
	if cfg.OIDCRoleClaim != "groups" {
		t.Errorf("OIDCRoleClaim = %q, want default %q", cfg.OIDCRoleClaim, "groups")
	}
}

func TestLoadConfig_OIDCConfigured(t *testing.T) {
	cfg, err := LoadConfig(envMap(validEnv(map[string]string{
		"CONSOLE_OIDC_ISSUER":         "https://idp.example.com",
		"CONSOLE_OIDC_CLIENT_ID":      "console",
		"CONSOLE_OIDC_CLIENT_SECRET":  "secret",
		"CONSOLE_OIDC_REDIRECT_URL":   "http://localhost:8080/api/v1/auth/oidc/callback",
		"CONSOLE_OIDC_ROLE_MAPPING":   `{"platform-admins":"admin"}`,
		"CONSOLE_OIDC_USERNAME_CLAIM": "preferred_username",
	})))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.OIDCIssuer != "https://idp.example.com" {
		t.Errorf("OIDCIssuer = %q", cfg.OIDCIssuer)
	}
	if cfg.OIDCUsernameClaim != "preferred_username" {
		t.Errorf("OIDCUsernameClaim = %q, want the overridden value", cfg.OIDCUsernameClaim)
	}
	if cfg.OIDCRoleMapping != `{"platform-admins":"admin"}` {
		t.Errorf("OIDCRoleMapping = %q", cfg.OIDCRoleMapping)
	}
}

func TestLoadConfig_CodeManagerUnsetByDefault(t *testing.T) {
	cfg, err := LoadConfig(envMap(validEnv(nil)))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.G10KBinPath != "" {
		t.Errorf("G10KBinPath = %q, want empty (deploys inactive by default)", cfg.G10KBinPath)
	}
	if cfg.ControlRepoURL != "" {
		t.Errorf("ControlRepoURL = %q, want empty", cfg.ControlRepoURL)
	}
	if cfg.CodeDirPath != "openvox-code" {
		t.Errorf("CodeDirPath = %q, want default %q", cfg.CodeDirPath, "openvox-code")
	}
}

func TestLoadConfig_CodeManagerConfigured(t *testing.T) {
	cfg, err := LoadConfig(envMap(validEnv(map[string]string{
		"CONSOLE_G10K_BIN_PATH":       "bin/g10k",
		"CONSOLE_CONTROL_REPO_URL":    "file:///tmp/control-repo.git",
		"CONSOLE_CODE_WEBHOOK_SECRET": "shh",
		"CONSOLE_CODE_DIR_PATH":       "custom-code-dir",
	})))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.G10KBinPath != "bin/g10k" {
		t.Errorf("G10KBinPath = %q", cfg.G10KBinPath)
	}
	if cfg.ControlRepoURL != "file:///tmp/control-repo.git" {
		t.Errorf("ControlRepoURL = %q", cfg.ControlRepoURL)
	}
	if cfg.CodeWebhookSecret != "shh" {
		t.Errorf("CodeWebhookSecret = %q", cfg.CodeWebhookSecret)
	}
	if cfg.CodeDirPath != "custom-code-dir" {
		t.Errorf("CodeDirPath = %q, want the overridden value", cfg.CodeDirPath)
	}
}

func TestLoadConfig_CustomHTTPAddr(t *testing.T) {
	cfg, err := LoadConfig(envMap(validEnv(map[string]string{
		"CONSOLE_HTTP_ADDR": ":9090",
	})))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.HTTPAddr != ":9090" {
		t.Errorf("HTTPAddr = %q, want :9090", cfg.HTTPAddr)
	}
}

func TestLoadConfig_AuditLevelsDefaultToWrites(t *testing.T) {
	cfg, err := LoadConfig(envMap(validEnv(nil)))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for name, level := range map[string]auditlog.Level{
		"AuditNodes":           cfg.AuditNodes,
		"AuditClassifier":      cfg.AuditClassifier,
		"AuditRBAC":            cfg.AuditRBAC,
		"AuditAuth":            cfg.AuditAuth,
		"AuditCode":            cfg.AuditCode,
		"AuditOrchestrator":    cfg.AuditOrchestrator,
		"AuditVulnerabilities": cfg.AuditVulnerabilities,
	} {
		if level != auditlog.LevelWrites {
			t.Errorf("%s = %v, want LevelWrites by default", name, level)
		}
	}
}

func TestLoadConfig_AuditLevelExplicitValue(t *testing.T) {
	cfg, err := LoadConfig(envMap(validEnv(map[string]string{
		"CONSOLE_AUDIT_NODES":           "full",
		"CONSOLE_AUDIT_RBAC":            "off",
		"CONSOLE_AUDIT_VULNERABILITIES": "full",
	})))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.AuditNodes != auditlog.LevelFull {
		t.Errorf("AuditNodes = %v, want LevelFull", cfg.AuditNodes)
	}
	if cfg.AuditRBAC != auditlog.LevelOff {
		t.Errorf("AuditRBAC = %v, want LevelOff", cfg.AuditRBAC)
	}
	if cfg.AuditVulnerabilities != auditlog.LevelFull {
		t.Errorf("AuditVulnerabilities = %v, want LevelFull", cfg.AuditVulnerabilities)
	}
	// Unrelated categories are unaffected.
	if cfg.AuditClassifier != auditlog.LevelWrites {
		t.Errorf("AuditClassifier = %v, want default LevelWrites", cfg.AuditClassifier)
	}
}

func TestLoadConfig_AuditLevelInvalidValueIsRejected(t *testing.T) {
	for _, env := range []string{
		"CONSOLE_AUDIT_NODES", "CONSOLE_AUDIT_CLASSIFIER", "CONSOLE_AUDIT_RBAC",
		"CONSOLE_AUDIT_AUTH", "CONSOLE_AUDIT_CODE", "CONSOLE_AUDIT_ORCHESTRATOR",
		"CONSOLE_AUDIT_VULNERABILITIES",
	} {
		_, err := LoadConfig(envMap(validEnv(map[string]string{env: "bogus"})))
		if err == nil {
			t.Errorf("%s=bogus: expected an error, got nil", env)
		}
	}
}

func TestLoadConfig_AuditLogPath(t *testing.T) {
	cfg, err := LoadConfig(envMap(validEnv(nil)))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.AuditLogPath != "" {
		t.Errorf("AuditLogPath = %q, want empty (stdout) by default", cfg.AuditLogPath)
	}

	cfg, err = LoadConfig(envMap(validEnv(map[string]string{
		"CONSOLE_AUDIT_LOG_PATH": "/var/log/openvox-console/audit.log",
	})))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.AuditLogPath != "/var/log/openvox-console/audit.log" {
		t.Errorf("AuditLogPath = %q, want the overridden value", cfg.AuditLogPath)
	}
}

// Docker secrets and Kubernetes projected volumes both present a secret
// as a file, not an environment variable - <NAME>_FILE is how a
// deployment keeps a password out of the process environment and out of
// `docker inspect`.
func TestLoadConfig_ReadsValueFromFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "dsn")
	// Trailing newline included deliberately: secret files almost always
	// have one, and a DSN carrying it fails to connect in a way that is
	// hard to spot.
	if err := os.WriteFile(path, []byte("postgres://console:s3cret@db/console\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	env := validEnv(nil)
	delete(env, "CONSOLE_POSTGRES_DSN")
	env["CONSOLE_POSTGRES_DSN_FILE"] = path

	cfg, err := LoadConfig(envMap(env))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.PostgresDSN != "postgres://console:s3cret@db/console" {
		t.Errorf("PostgresDSN = %q, want the file's contents with surrounding whitespace trimmed", cfg.PostgresDSN)
	}
}

func TestLoadConfig_FileWinsOverEnvironment(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "dsn")
	if err := os.WriteFile(path, []byte("postgres://from-file/console"), 0o600); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadConfig(envMap(validEnv(map[string]string{
		"CONSOLE_POSTGRES_DSN":      "postgres://from-env/console",
		"CONSOLE_POSTGRES_DSN_FILE": path,
	})))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.PostgresDSN != "postgres://from-file/console" {
		t.Errorf("PostgresDSN = %q, want the file to win when both are set", cfg.PostgresDSN)
	}
}

// A named-but-unreadable secret file must fail loudly. Falling back to
// the environment would start the console with a stale or empty value
// when the operator's clear intent was to read it from disk.
func TestLoadConfig_UnreadableFileIsFatal(t *testing.T) {
	_, err := LoadConfig(envMap(validEnv(map[string]string{
		"CONSOLE_POSTGRES_DSN_FILE": filepath.Join(t.TempDir(), "does-not-exist"),
	})))
	if err == nil {
		t.Fatal("error is nil, want a failure naming the unreadable file")
	}
	if !strings.Contains(err.Error(), "CONSOLE_POSTGRES_DSN_FILE") {
		t.Errorf("error = %q, want it to name the setting whose file could not be read", err)
	}
}

// Every setting goes through the same lookup, so this is not limited to
// the DSN - an optional secret like the webhook signing secret reads
// from a file too.
func TestLoadConfig_FileWorksForOptionalSecrets(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "webhook")
	if err := os.WriteFile(path, []byte("s3cret\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadConfig(envMap(validEnv(map[string]string{
		"CONSOLE_CODE_WEBHOOK_SECRET_FILE": path,
	})))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.CodeWebhookSecret != "s3cret" {
		t.Errorf("CodeWebhookSecret = %q, want it read from the file", cfg.CodeWebhookSecret)
	}
}

func TestLoadConfig_CodeSourcesPathAndControlRepoURLAreMutuallyExclusive(t *testing.T) {
	// Merging the two would let the same repo be declared twice with
	// different prefixes, and would mean the sources file no longer
	// tells you what actually deploys.
	_, err := LoadConfig(envMap(validEnv(map[string]string{
		"CONSOLE_CONTROL_REPO_URL":  "git@example.com:org/control.git",
		"CONSOLE_CODE_SOURCES_PATH": "/etc/openvox-console/code-sources.yaml",
	})))
	if err == nil {
		t.Fatal("LoadConfig() succeeded, want an error")
	}
	for _, want := range []string{"CONSOLE_CONTROL_REPO_URL", "CONSOLE_CODE_SOURCES_PATH"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error = %q, want it to name %s", err, want)
		}
	}
}

func TestLoadConfig_EitherCodeSourceFormAloneIsAccepted(t *testing.T) {
	single, err := LoadConfig(envMap(validEnv(map[string]string{
		"CONSOLE_CONTROL_REPO_URL": "git@example.com:org/control.git",
	})))
	if err != nil {
		t.Fatalf("single control repo: unexpected error: %v", err)
	}
	if single.ControlRepoURL != "git@example.com:org/control.git" || single.CodeSourcesPath != "" {
		t.Errorf("ControlRepoURL = %q, CodeSourcesPath = %q", single.ControlRepoURL, single.CodeSourcesPath)
	}

	multi, err := LoadConfig(envMap(validEnv(map[string]string{
		"CONSOLE_CODE_SOURCES_PATH": "/etc/openvox-console/code-sources.yaml",
	})))
	if err != nil {
		t.Fatalf("sources file: unexpected error: %v", err)
	}
	if multi.CodeSourcesPath != "/etc/openvox-console/code-sources.yaml" || multi.ControlRepoURL != "" {
		t.Errorf("ControlRepoURL = %q, CodeSourcesPath = %q", multi.ControlRepoURL, multi.CodeSourcesPath)
	}
}

func TestLoadConfig_RunModeDefaultsToAll(t *testing.T) {
	cfg, err := LoadConfig(envMap(validEnv(nil)))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Mode != ModeAll {
		t.Errorf("Mode = %q, want %q for an unset CONSOLE_RUN_MODE", cfg.Mode, ModeAll)
	}
}

func TestLoadConfig_RunModeConfigured(t *testing.T) {
	for _, want := range Modes() {
		// Each mode is given what that mode requires - the point of
		// mode-scoped requirements is that this set differs per mode.
		cfg, err := LoadConfig(envMap(validEnv(modeEnv(want))))
		if err != nil {
			t.Errorf("CONSOLE_RUN_MODE=%q: unexpected error: %v", want, err)
			continue
		}
		if cfg.Mode != want {
			t.Errorf("CONSOLE_RUN_MODE=%q: Mode = %q", want, cfg.Mode)
		}
	}
}

// An unrecognized mode must be refused, and the message has to carry
// enough for an operator to fix it without reading the source: the
// offending value and the full valid set.
func TestLoadConfig_RunModeInvalidValueIsRejected(t *testing.T) {
	_, err := LoadConfig(envMap(validEnv(map[string]string{
		"CONSOLE_RUN_MODE": "wrker",
	})))
	if err == nil {
		t.Fatal("expected an error for an invalid CONSOLE_RUN_MODE, got none")
	}

	msg := err.Error()
	if !strings.Contains(msg, "CONSOLE_RUN_MODE") {
		t.Errorf("error %q does not name the setting", msg)
	}
	if !strings.Contains(msg, "wrker") {
		t.Errorf("error %q does not name the invalid value", msg)
	}
	for _, m := range Modes() {
		if !strings.Contains(msg, string(m)) {
			t.Errorf("error %q does not name valid mode %q", msg, m)
		}
	}
}

// The mode is parsed before any other validation, so a deployment that
// gets both wrong is told about the mode rather than about a setting
// whose necessity depends on the mode it mistyped.
func TestLoadConfig_RunModeReportedBeforeMissingRequiredValues(t *testing.T) {
	_, err := LoadConfig(envMap(map[string]string{
		"CONSOLE_RUN_MODE": "wrker",
	}))
	if err == nil {
		t.Fatal("expected an error, got none")
	}
	if !strings.Contains(err.Error(), "CONSOLE_RUN_MODE") {
		t.Errorf("error %q reports something other than the invalid run mode", err)
	}
}

// A mode that does not issue tokens must start without a signing key -
// an enc instance typically runs next to a compiler, outside the
// console's trust boundary, and requiring the private key there would
// put token minting on every such host for no reason.
func TestLoadConfig_NonIssuingModeNeedsNoSigningKey(t *testing.T) {
	for _, mode := range []Mode{ModeENC, ModeWorker} {
		env := validEnv(map[string]string{
			"CONSOLE_RUN_MODE":                   string(mode),
			"CONSOLE_RBAC_VERIFICATION_KEYS_DIR": "certs/rbac-keys",
		})
		delete(env, "CONSOLE_RBAC_SIGNING_KEY_FILE")

		cfg, err := LoadConfig(envMap(env))
		if err != nil {
			t.Errorf("mode %q: unexpected error: %v", mode, err)
			continue
		}
		if cfg.RBACSigningKeyFile != "" {
			t.Errorf("mode %q: RBACSigningKeyFile = %q, want empty", mode, cfg.RBACSigningKeyFile)
		}
	}
}

// ...but it must be given verification keys, or it would start and then
// reject every token presented to it.
func TestLoadConfig_NonIssuingModeRequiresVerificationKeys(t *testing.T) {
	env := validEnv(map[string]string{"CONSOLE_RUN_MODE": string(ModeENC)})
	delete(env, "CONSOLE_RBAC_SIGNING_KEY_FILE")

	_, err := LoadConfig(envMap(env))
	if err == nil {
		t.Fatal("expected an error for enc mode with no verification keys directory, got none")
	}
	if !strings.Contains(err.Error(), "CONSOLE_RBAC_VERIFICATION_KEYS_DIR") {
		t.Errorf("error %q does not name the missing verification keys directory", err)
	}
}

// An issuing mode still requires the signing key.
func TestLoadConfig_IssuingModeRequiresSigningKey(t *testing.T) {
	for _, mode := range []Mode{ModeAll, ModeWeb} {
		env := validEnv(map[string]string{"CONSOLE_RUN_MODE": string(mode)})
		delete(env, "CONSOLE_RBAC_SIGNING_KEY_FILE")

		_, err := LoadConfig(envMap(env))
		if err == nil {
			t.Errorf("mode %q: expected an error with no signing key, got none", mode)
			continue
		}
		if !strings.Contains(err.Error(), "CONSOLE_RBAC_SIGNING_KEY_FILE") {
			t.Errorf("mode %q: error %q does not name the missing signing key", mode, err)
		}
	}
}

// orchestrator mode without a listener would accept no node connection at
// all, leaving every dispatch failing "not connected" on an instance that
// otherwise looks healthy.
func TestLoadConfig_OrchestratorRequiresTransportAddr(t *testing.T) {
	env := modeEnv(ModeOrchestrator)
	delete(env, "CONSOLE_NODE_TRANSPORT_ADDR")

	_, err := LoadConfig(envMap(validEnv(env)))
	if err == nil {
		t.Fatal("expected an error for orchestrator mode with no transport address, got none")
	}
	if !strings.Contains(err.Error(), "CONSOLE_NODE_TRANSPORT_ADDR") {
		t.Errorf("error %q does not name the missing transport address", err)
	}
	if !strings.Contains(err.Error(), string(ModeOrchestrator)) {
		t.Errorf("error %q does not say which mode required it", err)
	}
}

// Other modes leave the transport optional, exactly as before run modes.
func TestLoadConfig_NonOrchestratorModesDoNotRequireTransportAddr(t *testing.T) {
	for _, mode := range []Mode{ModeAll, ModeWeb} {
		if _, err := LoadConfig(envMap(validEnv(modeEnv(mode)))); err != nil {
			t.Errorf("mode %q: unexpected error: %v", mode, err)
		}
	}
}

func TestModeIssuesTokens(t *testing.T) {
	issuing := map[Mode]bool{ModeAll: true, ModeWeb: true, ModeENC: false, ModeOrchestrator: false, ModeWorker: false}
	for mode, want := range issuing {
		if got := mode.IssuesTokens(); got != want {
			t.Errorf("%q.IssuesTokens() = %v, want %v", mode, got, want)
		}
	}
}

// The default is a single, unclustered instance: no listener, no peers.
func TestLoadConfig_ClusterUnsetByDefault(t *testing.T) {
	cfg, err := LoadConfig(envMap(validEnv(nil)))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Clustered() {
		t.Error("an instance with no cluster configuration reports itself clustered")
	}
	if got := cfg.EffectiveClusterMode(); got != ClusterModeNone {
		t.Errorf("EffectiveClusterMode() = %q, want none", got)
	}
	if len(cfg.PeerList()) != 0 {
		t.Errorf("PeerList() = %v, want empty", cfg.PeerList())
	}
}

func TestLoadConfig_ClusterRoutedPeers(t *testing.T) {
	cfg, err := LoadConfig(envMap(validEnv(map[string]string{
		"CONSOLE_CLUSTER_ADDR":   "127.0.0.1:6222",
		"CONSOLE_CLUSTER_PEERS":  "10.0.0.2:6222, 10.0.0.3:6222,",
		"CONSOLE_CLUSTER_SECRET": "s3cret",
	})))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !cfg.Clustered() {
		t.Error("a configured instance does not report itself clustered")
	}
	// Unset CONSOLE_CLUSTER_MODE defaults to routing, which is what the
	// core modes need.
	if got := cfg.EffectiveClusterMode(); got != ClusterModeRoute {
		t.Errorf("EffectiveClusterMode() = %q, want %q", got, ClusterModeRoute)
	}
	want := []string{"10.0.0.2:6222", "10.0.0.3:6222"}
	got := cfg.PeerList()
	if len(got) != len(want) {
		t.Fatalf("PeerList() = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("PeerList()[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestLoadConfig_ClusterLeafAttachment(t *testing.T) {
	env := modeEnv(ModeENC)
	env["CONSOLE_CLUSTER_PEERS"] = "10.0.0.2:6222"
	env["CONSOLE_CLUSTER_MODE"] = "leaf"
	env["CONSOLE_CLUSTER_LEAF_SECRET"] = "leaf-s3cret"

	cfg, err := LoadConfig(envMap(validEnv(env)))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := cfg.EffectiveClusterMode(); got != ClusterModeLeaf {
		t.Errorf("EffectiveClusterMode() = %q, want %q", got, ClusterModeLeaf)
	}
	// A leaf dials out and needs no listener of its own.
	if cfg.ClusterAddr != "" {
		t.Errorf("ClusterAddr = %q, want empty for a leaf", cfg.ClusterAddr)
	}
}

// A peer listener with no credentials would put anything that can reach
// the port onto the internal event bus.
func TestLoadConfig_ClusterListenerRequiresSecret(t *testing.T) {
	_, err := LoadConfig(envMap(validEnv(map[string]string{
		"CONSOLE_CLUSTER_ADDR": "127.0.0.1:6222",
	})))
	if err == nil {
		t.Fatal("expected an error for a peer listener with no secret, got none")
	}
	if !strings.Contains(err.Error(), "CONSOLE_CLUSTER_SECRET") {
		t.Errorf("error %q does not name the missing secret", err)
	}
}

func TestLoadConfig_ClusterPeersRequireSecret(t *testing.T) {
	_, err := LoadConfig(envMap(validEnv(map[string]string{
		"CONSOLE_CLUSTER_PEERS": "10.0.0.2:6222",
	})))
	if err == nil {
		t.Fatal("expected an error for peers with no secret, got none")
	}
	if !strings.Contains(err.Error(), "CONSOLE_CLUSTER_SECRET") {
		t.Errorf("error %q does not name the missing secret", err)
	}
}

// A leaf authenticates with the leaf credential, never the route one -
// an edge host is the least trusted place a console runs.
func TestLoadConfig_ClusterLeafRequiresLeafSecret(t *testing.T) {
	env := modeEnv(ModeENC)
	env["CONSOLE_CLUSTER_PEERS"] = "10.0.0.2:6223"
	env["CONSOLE_CLUSTER_MODE"] = "leaf"
	env["CONSOLE_CLUSTER_SECRET"] = "s3cret"

	_, err := LoadConfig(envMap(validEnv(env)))
	if err == nil || !strings.Contains(err.Error(), "CONSOLE_CLUSTER_LEAF_SECRET") {
		t.Fatalf("error = %v, want one naming CONSOLE_CLUSTER_LEAF_SECRET", err)
	}
}

func TestLoadConfig_ClusterLeafListenerRequiresLeafSecret(t *testing.T) {
	_, err := LoadConfig(envMap(validEnv(map[string]string{
		"CONSOLE_CLUSTER_ADDR":      "127.0.0.1:6222",
		"CONSOLE_CLUSTER_LEAF_ADDR": "127.0.0.1:6223",
		"CONSOLE_CLUSTER_SECRET":    "s3cret",
	})))
	if err == nil || !strings.Contains(err.Error(), "CONSOLE_CLUSTER_LEAF_SECRET") {
		t.Fatalf("error = %v, want one naming CONSOLE_CLUSTER_LEAF_SECRET", err)
	}
}

func TestLoadConfig_ClusterLeafSecretMustDiffer(t *testing.T) {
	_, err := LoadConfig(envMap(validEnv(map[string]string{
		"CONSOLE_CLUSTER_ADDR":        "127.0.0.1:6222",
		"CONSOLE_CLUSTER_LEAF_ADDR":   "127.0.0.1:6223",
		"CONSOLE_CLUSTER_SECRET":      "same",
		"CONSOLE_CLUSTER_LEAF_SECRET": "same",
	})))
	if err == nil || !strings.Contains(err.Error(), "must differ") {
		t.Fatalf("error = %v, want the shared secret refused", err)
	}
}

// Peer TLS defaults to the console's own openvoxdb client credential, so
// clustering needs no new certificate material.
func TestLoadConfig_ClusterTLSDefaultsToOpenvoxdbCredential(t *testing.T) {
	cfg, err := LoadConfig(envMap(validEnv(map[string]string{
		"CONSOLE_CLUSTER_ADDR":   "127.0.0.1:6222",
		"CONSOLE_CLUSTER_SECRET": "s3cret",
	})))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.ClusterTLSCertFile != cfg.OpenvoxdbCertFile || cfg.ClusterTLSKeyFile != cfg.OpenvoxdbKeyFile || cfg.ClusterTLSCAFile != cfg.OpenvoxdbCAFile {
		t.Errorf("cluster TLS = %q/%q/%q, want the openvoxdb credential", cfg.ClusterTLSCertFile, cfg.ClusterTLSKeyFile, cfg.ClusterTLSCAFile)
	}
}

// The separate transport cluster is gone; a leftover setting must not
// silently do nothing.
func TestLoadConfig_RemovedTransportClusterSettingsAreRefused(t *testing.T) {
	_, err := LoadConfig(envMap(validEnv(map[string]string{
		"CONSOLE_NODE_TRANSPORT_CLUSTER_ADDR": ":6232",
	})))
	if err == nil || !strings.Contains(err.Error(), "CONSOLE_NODE_TRANSPORT_CLUSTER_ADDR") {
		t.Fatalf("error = %v, want the removed setting named", err)
	}
}

func TestLoadConfig_ClusterModeInvalidValueIsRejected(t *testing.T) {
	_, err := LoadConfig(envMap(validEnv(map[string]string{
		"CONSOLE_CLUSTER_MODE": "mesh",
	})))
	if err == nil {
		t.Fatal("expected an error for an invalid cluster mode, got none")
	}
	msg := err.Error()
	if !strings.Contains(msg, "mesh") {
		t.Errorf("error %q does not name the invalid value", msg)
	}
	for _, m := range ClusterModes() {
		if !strings.Contains(msg, string(m)) {
			t.Errorf("error %q does not name valid cluster mode %q", msg, m)
		}
	}
}
