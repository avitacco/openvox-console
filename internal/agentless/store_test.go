package agentless

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"strings"
	"testing"

	"golang.org/x/crypto/ssh"

	"github.com/voxpupuli/enterprise-console/internal/sealer"
	"github.com/voxpupuli/enterprise-console/internal/testdb"
)

func testSealer(t *testing.T) *sealer.Sealer {
	t.Helper()
	key := make([]byte, sealer.KeySize)
	if _, err := rand.Read(key); err != nil {
		t.Fatal(err)
	}
	s, err := sealer.New(key)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func newStore(t *testing.T) *Store { return NewStore(testdb.ScratchPool(t), testSealer(t)) }

// testKey makes a private key in OpenSSH format, optionally protected.
func testKey(t *testing.T, passphrase string) (pemKey, fingerprint string) {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	var out []byte
	if passphrase == "" {
		b, err := ssh.MarshalPrivateKey(priv, "")
		if err != nil {
			t.Fatal(err)
		}
		out = pemEncode(b)
	} else {
		b, err := ssh.MarshalPrivateKeyWithPassphrase(priv, "", []byte(passphrase))
		if err != nil {
			t.Fatal(err)
		}
		out = pemEncode(b)
	}
	sp, _ := ssh.NewPublicKey(pub)
	return string(out), ssh.FingerprintSHA256(sp)
}

func str(s string) *string { return &s }

func TestCreateTarget_WithItsOwnPassword_NeverReadsBack(t *testing.T) {
	s, ctx := newStore(t), context.Background()
	got, err := s.CreateTarget(ctx, TargetInput{Name: "db-legacy", Address: "10.0.0.5", Login: Login{Username: "ops", Secrets: Secrets{Password: str("hunter2-secret")}}}, "alice")
	if err != nil {
		t.Fatal(err)
	}
	if got.Port != 22 || !got.HasPassword || got.HasKey || got.Username != "ops" || got.HostKey.Pinned != "" {
		t.Errorf("target = %+v", got)
	}
	for _, read := range []func() (any, error){
		func() (any, error) { return s.GetTarget(ctx, got.ID) },
		func() (any, error) { return s.GetTargetByName(ctx, "db-legacy") },
		func() (any, error) { return s.ListTargets(ctx) },
	} {
		v, err := read()
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(strings.ToLower(formatAll(v)), "hunter2") {
			t.Errorf("a read returned the password: %v", v)
		}
	}
	// The column holds an envelope, not the password.
	var raw []byte
	if err := s.pool.QueryRow(ctx, `SELECT sealed_password FROM agentless_targets WHERE id = $1`, got.ID).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "hunter2") {
		t.Error("the password is stored in the clear")
	}
	conn, err := s.Connection(ctx, "db-legacy")
	if err != nil || conn.Password != "hunter2-secret" || conn.Username != "ops" || conn.Address != "10.0.0.5" {
		t.Errorf("connection = %+v, %v", conn, err)
	}
}

func TestCreateTarget_Validation(t *testing.T) {
	s, ctx := newStore(t), context.Background()
	ok := Login{Username: "ops", Secrets: Secrets{Password: str("x")}}
	for name, in := range map[string]TargetInput{
		"bad name":      {Name: "-bad", Address: "h", Login: ok},
		"space in name": {Name: "a b", Address: "h", Login: ok},
		"bad address":   {Name: "a", Address: "h; reboot", Login: ok},
		"bad port":      {Name: "a", Address: "h", Port: 70000, Login: ok},
		"dash user":     {Name: "a", Address: "h", Login: Login{Username: "-oProxyCommand=x", Secrets: ok.Secrets}},
		"bad run-as":    {Name: "a", Address: "h", Login: Login{Username: "u", RunAs: "r oot", Secrets: ok.Secrets}},
		"no secret":     {Name: "a", Address: "h", Login: Login{Username: "u"}},
	} {
		if _, err := s.CreateTarget(ctx, in, "t"); err == nil {
			t.Errorf("%s was accepted", name)
		} else {
			var inv *InvalidError
			if !errors.As(err, &inv) {
				t.Errorf("%s: err = %v, want a refusal", name, err)
			}
		}
	}
	if _, err := s.CreateTarget(ctx, TargetInput{Name: "x", Address: "h", Login: ok}, "t"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateTarget(ctx, TargetInput{Name: "x", Address: "h2", Login: ok}, "t"); !errors.Is(err, ErrNameTaken) {
		t.Errorf("duplicate name = %v", err)
	}
}

func TestPrivateKeys_AreCheckedAndFingerprinted(t *testing.T) {
	s, ctx := newStore(t), context.Background()
	key, fp := testKey(t, "")
	got, err := s.CreateTarget(ctx, TargetInput{Name: "k1", Address: "h", Login: Login{Username: "ops", Secrets: Secrets{PrivateKey: &key}}}, "t")
	if err != nil || got.KeyFingerprint != fp || !got.HasKey {
		t.Fatalf("got %+v, %v", got, err)
	}
	// Garbage is refused, and the refusal does not repeat it.
	_, err = s.CreateTarget(ctx, TargetInput{Name: "k2", Address: "h", Login: Login{Username: "ops", Secrets: Secrets{PrivateKey: str("-----BEGIN SECRET-----garbage")}}}, "t")
	var inv *InvalidError
	if !errors.As(err, &inv) || inv.Field != "privateKey" || strings.Contains(err.Error(), "garbage") {
		t.Errorf("garbage key: %v", err)
	}
	// A protected key needs its passphrase.
	pkey, pfp := testKey(t, "open-sesame")
	_, err = s.CreateTarget(ctx, TargetInput{Name: "k3", Address: "h", Login: Login{Username: "ops", Secrets: Secrets{PrivateKey: &pkey}}}, "t")
	if !errors.As(err, &inv) || inv.Field != "passphrase" {
		t.Errorf("missing passphrase: %v", err)
	}
	_, err = s.CreateTarget(ctx, TargetInput{Name: "k4", Address: "h", Login: Login{Username: "ops", Secrets: Secrets{PrivateKey: &pkey, Passphrase: str("wrong")}}}, "t")
	if !errors.As(err, &inv) || strings.Contains(err.Error(), "wrong") {
		t.Errorf("wrong passphrase: %v", err)
	}
	got, err = s.CreateTarget(ctx, TargetInput{Name: "k5", Address: "h", Login: Login{Username: "ops", Secrets: Secrets{PrivateKey: &pkey, Passphrase: str("open-sesame")}}}, "t")
	if err != nil || got.KeyFingerprint != pfp {
		t.Fatalf("got %+v, %v", got, err)
	}
	conn, err := s.Connection(ctx, "k5")
	if err != nil || string(conn.Passphrase) != "open-sesame" || !strings.Contains(string(conn.PrivateKey), "PRIVATE KEY") {
		t.Errorf("connection = %v", err)
	}
}

func TestSealedSecret_CopiedToAnotherRowDoesNotOpen(t *testing.T) {
	s, ctx := newStore(t), context.Background()
	for _, n := range []string{"one", "two"} {
		if _, err := s.CreateTarget(ctx, TargetInput{Name: n, Address: "h", Login: Login{Username: "u", Secrets: Secrets{Password: str("pw-" + n)}}}, "t"); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.pool.Exec(ctx, `UPDATE agentless_targets SET sealed_password = (SELECT sealed_password FROM agentless_targets WHERE name = 'one') WHERE name = 'two'`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Connection(ctx, "two"); err == nil || strings.Contains(err.Error(), "pw-") {
		t.Errorf("a ciphertext moved to another row opened: %v", err)
	}
}

func TestWithoutASecretsKey(t *testing.T) {
	s := NewStore(testdb.ScratchPool(t), nil)
	_, err := s.CreateTarget(context.Background(), TargetInput{Name: "a", Address: "h", Login: Login{Username: "u", Secrets: Secrets{Password: str("x")}}}, "t")
	if !errors.Is(err, ErrNoSecretsKey) {
		t.Errorf("err = %v", err)
	}
}

func TestSharedCredential_UsedByTargets(t *testing.T) {
	s, ctx := newStore(t), context.Background()
	cred, err := s.CreateCredential(ctx, "ops-key", Login{Username: "ops", RunAs: "root", Secrets: Secrets{Password: str("first-pass")}}, "t")
	if err != nil {
		t.Fatal(err)
	}
	for _, n := range []string{"a", "b", "c"} {
		if _, err := s.CreateTarget(ctx, TargetInput{Name: n, Address: "h-" + n, CredentialID: &cred.ID}, "t"); err != nil {
			t.Fatal(err)
		}
	}
	conn, _ := s.Connection(ctx, "b")
	if conn.Username != "ops" || conn.RunAs != "root" || conn.Password != "first-pass" {
		t.Errorf("connection = %+v", conn)
	}
	// Replacing the secret reaches every target.
	if _, err := s.UpdateCredential(ctx, cred.ID, Login{RunAs: "root", Secrets: Secrets{Password: str("second-pass")}}, "t"); err != nil {
		t.Fatal(err)
	}
	for _, n := range []string{"a", "b", "c"} {
		if c, _ := s.Connection(ctx, n); c.Password != "second-pass" {
			t.Errorf("%s still has %q", n, c.Password)
		}
	}
	// A credential in use is not deleted.
	var inUse *InUseError
	if err := s.DeleteCredential(ctx, cred.ID); !errors.As(err, &inUse) || len(inUse.Targets) != 3 {
		t.Errorf("delete in use = %v", err)
	}
	got, _ := s.GetCredential(ctx, cred.ID)
	if len(got.Targets) != 3 || !got.HasPassword {
		t.Errorf("credential = %+v", got)
	}
	// A target's own login wins over the shared one when it is given one.
	own, err := s.UpdateTarget(ctx, mustID(t, s, "a"), TargetInput{Address: "h-a", Login: Login{Username: "special", Secrets: Secrets{Password: str("own-pass")}}}, "t")
	if err != nil || own.CredentialID != nil || own.Username != "special" {
		t.Fatalf("own = %+v, %v", own, err)
	}
	if c, _ := s.Connection(ctx, "a"); c.Password != "own-pass" || c.Username != "special" {
		t.Errorf("own login not used: %+v", c)
	}
	for _, n := range []string{"b", "c"} {
		id := mustID(t, s, n)
		if err := s.DeleteTarget(ctx, id); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.DeleteCredential(ctx, cred.ID); err != nil {
		t.Errorf("delete unused: %v", err)
	}
}

func TestUpdateTarget_SecretsAreWriteOnly(t *testing.T) {
	s, ctx := newStore(t), context.Background()
	tg, _ := s.CreateTarget(ctx, TargetInput{Name: "u1", Address: "h", Login: Login{Username: "ops", Secrets: Secrets{Password: str("keep-me")}}}, "t")
	// Changing the address without a secret keeps the secret.
	if _, err := s.UpdateTarget(ctx, tg.ID, TargetInput{Address: "h2", Port: 2222}, "t"); err != nil {
		t.Fatal(err)
	}
	c, _ := s.Connection(ctx, "u1")
	if c.Password != "keep-me" || c.Address != "h2" || c.Port != 2222 {
		t.Errorf("connection = %+v", c)
	}
	// A port or address left out is not reset, and the pin stays.
	if _, err := s.ProposeKey(ctx, tg.ID, wireKey(t)); err != nil {
		t.Fatal(err)
	}
	// (pin it through the same path an operator would)
	prop, _ := s.GetTarget(ctx, tg.ID)
	if _, err := s.AcceptKey(ctx, tg.ID, prop.HostKey.Proposed); err != nil {
		t.Fatal(err)
	}
	kept, err := s.UpdateTarget(ctx, tg.ID, TargetInput{Address: "h2", Login: Login{Secrets: Secrets{Password: str("new-pw")}}}, "t")
	if err != nil || kept.Port != 2222 || kept.HostKey.Pinned == "" {
		t.Errorf("a password change without a port: port %d, pin %q, %v", kept.Port, kept.HostKey.Pinned, err)
	}
	// An empty value removes the secret, and a target must keep one.
	var inv *InvalidError
	if _, err := s.UpdateTarget(ctx, tg.ID, TargetInput{Address: "h2", Login: Login{Secrets: Secrets{Password: str("")}}}, "t"); !errors.As(err, &inv) {
		t.Errorf("removing the only secret = %v", err)
	}
}

func TestHostKeyPin(t *testing.T) {
	s, ctx := newStore(t), context.Background()
	tg, _ := s.CreateTarget(ctx, TargetInput{Name: "hk", Address: "h", Login: Login{Username: "u", Secrets: Secrets{Password: str("x")}}}, "t")
	k1, k2 := wireKey(t), wireKey(t)

	fp1, err := s.ProposeKey(ctx, tg.ID, k1)
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := s.GetTarget(ctx, tg.ID); got.HostKey.Proposed != fp1 || got.HostKey.Pinned != "" {
		t.Errorf("after propose: %+v", got.HostKey)
	}
	// Accepting needs the fingerprint the operator was shown.
	if _, err := s.AcceptKey(ctx, tg.ID, "SHA256:other"); !errors.Is(err, ErrKeyNotPending) {
		t.Errorf("accept another fingerprint = %v", err)
	}
	got, err := s.AcceptKey(ctx, tg.ID, fp1)
	if err != nil || got.HostKey.Pinned != fp1 || got.HostKey.Proposed != "" {
		t.Fatalf("accept: %+v, %v", got.HostKey, err)
	}
	if c, _ := s.Connection(ctx, "hk"); string(c.PinnedKey) != string(k1) {
		t.Error("the pinned key is not the one accepted")
	}

	// A different key is flagged, and does not replace the pin by itself.
	fp2, _ := s.FlagChangedKey(ctx, tg.ID, k2)
	got, _ = s.GetTarget(ctx, tg.ID)
	if !got.HostKey.Changed || got.HostKey.Presented != fp2 || got.HostKey.Pinned != fp1 {
		t.Errorf("changed: %+v", got.HostKey)
	}
	var inv *InvalidError
	if _, err := s.AcceptKey(ctx, tg.ID, fp2); !errors.As(err, &inv) {
		t.Errorf("accept over a pin = %v, want an explicit replace", err)
	}
	got, err = s.ReplaceKey(ctx, tg.ID, fp2)
	if err != nil || got.HostKey.Pinned != fp2 || got.HostKey.Changed {
		t.Errorf("replace: %+v, %v", got.HostKey, err)
	}
	// Moving the target clears the pin: a pin vouches for a host.
	got, _ = s.UpdateTarget(ctx, tg.ID, TargetInput{Address: "elsewhere"}, "t")
	if got.HostKey.Pinned != "" {
		t.Errorf("pin survived a change of address: %+v", got.HostKey)
	}
}

func TestRecordCheck(t *testing.T) {
	s, ctx := newStore(t), context.Background()
	tg, _ := s.CreateTarget(ctx, TargetInput{Name: "ck", Address: "h", Login: Login{Username: "u", Secrets: Secrets{Password: str("x")}}}, "t")
	if err := s.RecordCheck(ctx, tg.ID, false, "authenticate", "authentication failed"); err != nil {
		t.Fatal(err)
	}
	got, _ := s.GetTarget(ctx, tg.ID)
	if got.Check.OK == nil || *got.Check.OK || got.Check.Stage != "authenticate" || got.Check.At == nil {
		t.Errorf("check = %+v", got.Check)
	}
}

func mustID(t *testing.T, s *Store, name string) int64 {
	t.Helper()
	tg, err := s.GetTargetByName(context.Background(), name)
	if err != nil {
		t.Fatal(err)
	}
	return tg.ID
}

func wireKey(t *testing.T) []byte {
	t.Helper()
	pub, _, _ := ed25519.GenerateKey(rand.Reader)
	k, err := ssh.NewPublicKey(pub)
	if err != nil {
		t.Fatal(err)
	}
	return k.Marshal()
}
