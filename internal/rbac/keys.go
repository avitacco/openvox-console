package rbac

import (
	"crypto/ecdsa"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/golang-jwt/jwt/v5"
)

// LoadSigningKey reads an ES256 (P-256) private key from a PEM file. The
// same key is used to sign and verify tokens - see design.md: this
// console both issues and verifies its own tokens, so there's no need to
// distribute a separate public key.
func LoadSigningKey(path string) (*ecdsa.PrivateKey, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read signing key: %w", err)
	}
	key, err := jwt.ParseECPrivateKeyFromPEM(data)
	if err != nil {
		return nil, fmt.Errorf("parse signing key: %w", err)
	}
	return key, nil
}

// LoadVerificationKeys reads every "<kid>.pem" file in dir - each a
// private key PEM in the same format LoadSigningKey reads, since that's
// what `make rbac-keys`/`rbac-rotate-key` already generates and an
// operator retiring a key can just leave the old key file in place
// without a separate public-key export step - and returns a kid -> public
// key map for Verifier. An empty (or unset) dir returns an empty map, not
// an error: verification-key rotation is entirely optional, see
// operations.md's key rotation runbook.
func LoadVerificationKeys(dir string) (map[string]*ecdsa.PublicKey, error) {
	keys := map[string]*ecdsa.PublicKey{}
	if dir == "" {
		return keys, nil
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("read verification keys dir: %w", err)
	}
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".pem" {
			continue
		}
		kid := strings.TrimSuffix(entry.Name(), ".pem")
		key, err := LoadSigningKey(filepath.Join(dir, entry.Name()))
		if err != nil {
			return nil, fmt.Errorf("verification key %q: %w", entry.Name(), err)
		}
		keys[kid] = &key.PublicKey
	}
	return keys, nil
}
