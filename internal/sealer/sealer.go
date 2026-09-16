// Package sealer encrypts small secrets for storage at rest in Postgres -
// provider credentials, today (see design.md in
// add-vulnerability-tracking). It is deliberately narrow: one AES-256-GCM
// key, a versioned envelope, and associated data binding each ciphertext
// to the row it belongs to, so a ciphertext copied into another row fails
// to open rather than silently decrypting there.
package sealer

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"errors"
	"fmt"
)

// KeySize is the required key length: AES-256.
const KeySize = 32

// envelopeV1 is the only envelope format so far: version || nonce ||
// ciphertext. The version byte is what leaves room for key rotation (a
// later version can carry a key id) without guessing at old data.
const envelopeV1 byte = 1

// ErrOpen is returned for any ciphertext that cannot be opened - wrong
// key, wrong associated data, truncation, or tampering - without saying
// which, so a failure never leaks more than "not valid here".
var ErrOpen = errors.New("sealer: cannot open sealed value")

// Sealer seals and opens values with one key.
type Sealer struct {
	aead cipher.AEAD
}

// New builds a Sealer from a KeySize-byte key.
func New(key []byte) (*Sealer, error) {
	if len(key) != KeySize {
		return nil, fmt.Errorf("sealer: key must be %d bytes, got %d", KeySize, len(key))
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("sealer: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("sealer: %w", err)
	}
	return &Sealer{aead: aead}, nil
}

// Seal encrypts plaintext bound to associatedData (for example the owning
// row's id), returning the versioned envelope to store.
func (s *Sealer) Seal(plaintext, associatedData []byte) ([]byte, error) {
	nonce := make([]byte, s.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, fmt.Errorf("sealer: generate nonce: %w", err)
	}
	out := make([]byte, 0, 1+len(nonce)+len(plaintext)+s.aead.Overhead())
	out = append(out, envelopeV1)
	out = append(out, nonce...)
	return s.aead.Seal(out, nonce, plaintext, associatedData), nil
}

// Open decrypts an envelope produced by Seal with the same key and
// associatedData.
func (s *Sealer) Open(sealed, associatedData []byte) ([]byte, error) {
	nonceSize := s.aead.NonceSize()
	if len(sealed) < 1+nonceSize+s.aead.Overhead() || sealed[0] != envelopeV1 {
		return nil, ErrOpen
	}
	nonce := sealed[1 : 1+nonceSize]
	plaintext, err := s.aead.Open(nil, nonce, sealed[1+nonceSize:], associatedData)
	if err != nil {
		return nil, ErrOpen
	}
	return plaintext, nil
}
