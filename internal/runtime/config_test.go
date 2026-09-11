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
		"AuditNodes":        cfg.AuditNodes,
		"AuditClassifier":   cfg.AuditClassifier,
		"AuditRBAC":         cfg.AuditRBAC,
		"AuditAuth":         cfg.AuditAuth,
		"AuditCode":         cfg.AuditCode,
		"AuditOrchestrator": cfg.AuditOrchestrator,
	} {
		if level != auditlog.LevelWrites {
			t.Errorf("%s = %v, want LevelWrites by default", name, level)
		}
	}
}

func TestLoadConfig_AuditLevelExplicitValue(t *testing.T) {
	cfg, err := LoadConfig(envMap(validEnv(map[string]string{
		"CONSOLE_AUDIT_NODES": "full",
		"CONSOLE_AUDIT_RBAC":  "off",
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
	// Unrelated categories are unaffected.
	if cfg.AuditClassifier != auditlog.LevelWrites {
		t.Errorf("AuditClassifier = %v, want default LevelWrites", cfg.AuditClassifier)
	}
}

func TestLoadConfig_AuditLevelInvalidValueIsRejected(t *testing.T) {
	for _, env := range []string{
		"CONSOLE_AUDIT_NODES", "CONSOLE_AUDIT_CLASSIFIER", "CONSOLE_AUDIT_RBAC",
		"CONSOLE_AUDIT_AUTH", "CONSOLE_AUDIT_CODE", "CONSOLE_AUDIT_ORCHESTRATOR",
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
