package sealer

import (
	"bytes"
	"errors"
	"testing"
)

func testKey(fill byte) []byte {
	return bytes.Repeat([]byte{fill}, KeySize)
}

func mustNew(t *testing.T, key []byte) *Sealer {
	t.Helper()
	s, err := New(key)
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}
	return s
}

func TestNew_RejectsWrongKeySize(t *testing.T) {
	for _, n := range []int{0, 16, 31, 33} {
		if _, err := New(make([]byte, n)); err == nil {
			t.Errorf("New() with a %d-byte key succeeded, want an error", n)
		}
	}
}

func TestSealOpen_RoundTrip(t *testing.T) {
	s := mustNew(t, testKey(1))
	plaintext := []byte(`{"access_key":"abc","secret_key":"def"}`)

	sealed, err := s.Seal(plaintext, []byte("provider-1"))
	if err != nil {
		t.Fatalf("Seal() error: %v", err)
	}
	if bytes.Contains(sealed, plaintext) {
		t.Fatal("sealed value contains the plaintext")
	}
	if sealed[0] != envelopeV1 {
		t.Errorf("envelope version = %d, want %d", sealed[0], envelopeV1)
	}

	got, err := s.Open(sealed, []byte("provider-1"))
	if err != nil {
		t.Fatalf("Open() error: %v", err)
	}
	if !bytes.Equal(got, plaintext) {
		t.Errorf("Open() = %q, want %q", got, plaintext)
	}
}

func TestSeal_UsesAFreshNonceEachTime(t *testing.T) {
	s := mustNew(t, testKey(1))
	a, _ := s.Seal([]byte("same"), []byte("id"))
	b, _ := s.Seal([]byte("same"), []byte("id"))
	if bytes.Equal(a, b) {
		t.Error("sealing the same value twice produced identical output")
	}
}

func TestOpen_Failures(t *testing.T) {
	s := mustNew(t, testKey(1))
	sealed, err := s.Seal([]byte("secret"), []byte("provider-1"))
	if err != nil {
		t.Fatalf("Seal() error: %v", err)
	}

	tampered := bytes.Clone(sealed)
	tampered[len(tampered)-1] ^= 0xff
	wrongVersion := bytes.Clone(sealed)
	wrongVersion[0] = 2

	cases := []struct {
		name   string
		sealer *Sealer
		sealed []byte
		ad     string
	}{
		{"tampered ciphertext", s, tampered, "provider-1"},
		{"wrong associated data", s, sealed, "provider-2"},
		{"wrong key", mustNew(t, testKey(2)), sealed, "provider-1"},
		{"unknown envelope version", s, wrongVersion, "provider-1"},
		{"truncated", s, sealed[:10], "provider-1"},
		{"empty", s, nil, "provider-1"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := tc.sealer.Open(tc.sealed, []byte(tc.ad))
			if !errors.Is(err, ErrOpen) {
				t.Errorf("Open() error = %v, want ErrOpen", err)
			}
			if got != nil {
				t.Errorf("Open() returned %q, want nil", got)
			}
		})
	}
}
