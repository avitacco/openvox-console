package runtime

import (
	"bytes"
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeKeyFile(t *testing.T, contents string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "secrets-key")
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestLoadConfig_SecretsKeyUnsetByDefault(t *testing.T) {
	cfg, err := LoadConfig(envMap(validEnv(nil)))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.SecretsKey != nil {
		t.Errorf("SecretsKey = %x, want nil when unset", cfg.SecretsKey)
	}
}

func TestLoadConfig_SecretsKeyFromFile(t *testing.T) {
	key := bytes.Repeat([]byte{7}, 32)
	// Trailing newline, as `openssl rand -base64 32 > file` produces.
	path := writeKeyFile(t, base64.StdEncoding.EncodeToString(key)+"\n")

	cfg, err := LoadConfig(envMap(validEnv(map[string]string{"CONSOLE_SECRETS_KEY_FILE": path})))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !bytes.Equal(cfg.SecretsKey, key) {
		t.Errorf("SecretsKey = %x, want %x", cfg.SecretsKey, key)
	}
}

func TestLoadConfig_SecretsKeyWrongLengthIsRejected(t *testing.T) {
	path := writeKeyFile(t, base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{7}, 16)))

	_, err := LoadConfig(envMap(validEnv(map[string]string{"CONSOLE_SECRETS_KEY_FILE": path})))
	if err == nil {
		t.Fatal("error is nil, want a rejection for a 16-byte key")
	}
	if !strings.Contains(err.Error(), "CONSOLE_SECRETS_KEY") || !strings.Contains(err.Error(), "32 bytes") {
		t.Errorf("error = %q, want it to name the setting and the required length", err)
	}
}

func TestLoadConfig_SecretsKeyInvalidBase64IsRejected(t *testing.T) {
	_, err := LoadConfig(envMap(validEnv(map[string]string{"CONSOLE_SECRETS_KEY": "not base64 !!"})))
	if err == nil || !strings.Contains(err.Error(), "CONSOLE_SECRETS_KEY") {
		t.Errorf("error = %v, want a rejection naming CONSOLE_SECRETS_KEY", err)
	}
}

func TestLoadConfig_SecretsKeyUnreadableFileIsFatal(t *testing.T) {
	_, err := LoadConfig(envMap(validEnv(map[string]string{
		"CONSOLE_SECRETS_KEY_FILE": filepath.Join(t.TempDir(), "does-not-exist"),
	})))
	if err == nil || !strings.Contains(err.Error(), "CONSOLE_SECRETS_KEY_FILE") {
		t.Errorf("error = %v, want a failure naming CONSOLE_SECRETS_KEY_FILE", err)
	}
}
