package agentless

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"golang.org/x/crypto/ssh"
)

func pemEncode(b *pem.Block) []byte { return pem.EncodeToMemory(b) }

func formatAll(v any) string { return fmt.Sprintf("%+v", v) }

func jsonUnmarshal(b []byte, v any) error { return json.Unmarshal(b, v) }

func sha256Hex(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

func parseSigner(pemKey string) (ssh.Signer, error) { return ssh.ParsePrivateKey([]byte(pemKey)) }
