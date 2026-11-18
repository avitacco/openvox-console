// Package agentless lets the console run tasks on hosts that have no node
// agent, over SSH (add-agentless-ssh-targets): the registry of targets and
// their sealed logins, the SSH executor, and the HTTP API.
package agentless

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/crypto/ssh"

	"github.com/voxpupuli/enterprise-console/internal/sealer"
)

// Refusals a caller can act on.
var (
	ErrNotFound      = errors.New("not found")
	ErrNameTaken     = errors.New("that name is already in use")
	ErrNoSecretsKey  = errors.New("storing credentials needs the console's secrets key (CONSOLE_SECRETS_KEY_FILE), which is not configured")
	ErrKeyNotPending = errors.New("the key presented by this target is not the one you were shown")
)

// InvalidError is a request refused for a value, naming the field.
type InvalidError struct{ Field, Reason string }

func (e *InvalidError) Error() string { return e.Reason }

// InUseError is a shared credential still referenced by targets.
type InUseError struct{ Targets []string }

func (e *InUseError) Error() string {
	return "this credential is still used by: " + strings.Join(e.Targets, ", ")
}

// Field values that are never reflected back and never logged.
var (
	namePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,251}$`)
	userPattern = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9_.@-]{0,63}$`)
	hostPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9.:_-]{0,252}$`)
)

// Secrets are the secret parts of a login. On create at least one of
// PrivateKey and Password is needed; on update a nil field is unchanged,
// and a non-nil empty one removes the secret.
type Secrets struct {
	PrivateKey *string
	Passphrase *string
	Password   *string
}

// Login is who a target logs in as. Secrets are write-only.
type Login struct {
	Username string
	RunAs    string
	Secrets
}

// Credential is a shared login as it is read: no secret.
type Credential struct {
	ID             int64     `json:"id"`
	Name           string    `json:"name"`
	Username       string    `json:"username"`
	RunAs          string    `json:"runAs,omitempty"`
	HasKey         bool      `json:"hasKey"`
	HasPassphrase  bool      `json:"hasPassphrase"`
	HasPassword    bool      `json:"hasPassword"`
	KeyFingerprint string    `json:"keyFingerprint,omitempty"`
	Targets        []string  `json:"targets,omitempty"`
	UpdatedBy      string    `json:"updatedBy"`
	UpdatedAt      time.Time `json:"updatedAt"`
}

// HostKey is the pin state of a target.
type HostKey struct {
	// Pinned is the trusted fingerprint; empty until one is accepted.
	Pinned string `json:"pinned,omitempty"`
	// Proposed is a fingerprint seen by a check, waiting to be accepted.
	Proposed string `json:"proposed,omitempty"`
	// Changed is set when a pinned target presented another key;
	// Presented is its fingerprint.
	Changed   bool   `json:"changed,omitempty"`
	Presented string `json:"presented,omitempty"`
}

// Check is the result of the last connectivity check.
type Check struct {
	At    *time.Time `json:"at,omitempty"`
	OK    *bool      `json:"ok,omitempty"`
	Stage string     `json:"stage,omitempty"`
	Error string     `json:"error,omitempty"`
}

// Target is a target as it is read: no secret.
type Target struct {
	ID             int64     `json:"id"`
	Name           string    `json:"name"`
	Address        string    `json:"address"`
	Port           int       `json:"port"`
	CredentialID   *int64    `json:"credentialId,omitempty"`
	CredentialName string    `json:"credentialName,omitempty"`
	Username       string    `json:"username"`
	RunAs          string    `json:"runAs,omitempty"`
	HasKey         bool      `json:"hasKey"`
	HasPassword    bool      `json:"hasPassword"`
	KeyFingerprint string    `json:"keyFingerprint,omitempty"`
	HostKey        HostKey   `json:"hostKey"`
	Check          Check     `json:"check"`
	UpdatedBy      string    `json:"updatedBy"`
	UpdatedAt      time.Time `json:"updatedAt"`
}

// TargetInput is a request to create or change a target.
type TargetInput struct {
	Name    string
	Address string
	Port    int
	// CredentialID names a shared credential; nil uses the target's own
	// Login. Set together they are refused.
	CredentialID *int64
	Login        Login
}

// Connection is everything the executor needs to reach a target, secrets
// opened. It is held only for the length of one connection and has no
// String method that would print it.
type Connection struct {
	Name       string
	Address    string
	Port       int
	Username   string
	RunAs      string
	PrivateKey []byte
	Passphrase []byte
	Password   string
	// PinnedKey is the trusted public key (wire format), nil when none.
	PinnedKey []byte
	ID        int64
}

// Store is the Postgres store. sealer may be nil, in which case nothing
// with a secret can be stored or opened.
type Store struct {
	pool   *pgxpool.Pool
	sealer *sealer.Sealer
	now    func() time.Time
}

// NewStore builds a Store.
func NewStore(pool *pgxpool.Pool, s *sealer.Sealer) *Store {
	return &Store{pool: pool, sealer: s, now: time.Now}
}

func binding(table string, id int64, field string) []byte {
	return []byte(fmt.Sprintf("agentless:%s:%d:%s", table, id, field))
}

// parseLogin checks a login's values and, for a private key, that it can
// be read, returning the key's public fingerprint. Neither the key nor
// the passphrase is ever part of an error.
func parseLogin(l Login, creating bool) (fingerprint string, err error) {
	if !userPattern.MatchString(l.Username) && (creating || l.Username != "") {
		return "", &InvalidError{Field: "username", Reason: "the user name may use letters, digits, and _ . @ -, up to 64 characters, and may not start with a dash"}
	}
	if l.RunAs != "" && !userPattern.MatchString(l.RunAs) {
		return "", &InvalidError{Field: "runAs", Reason: "the run-as user may use letters, digits, and _ . @ -, up to 64 characters, and may not start with a dash"}
	}
	if l.PrivateKey != nil && *l.PrivateKey != "" {
		var pass []byte
		if l.Passphrase != nil {
			pass = []byte(*l.Passphrase)
		}
		fp, err := keyFingerprint([]byte(*l.PrivateKey), pass)
		if err != nil {
			return "", err
		}
		fingerprint = fp
	}
	return fingerprint, nil
}

// keyFingerprint reads a private key and returns its public fingerprint.
func keyFingerprint(key, passphrase []byte) (string, error) {
	var signer ssh.Signer
	var err error
	if len(passphrase) > 0 {
		signer, err = ssh.ParsePrivateKeyWithPassphrase(key, passphrase)
	} else {
		signer, err = ssh.ParsePrivateKey(key)
	}
	var missing *ssh.PassphraseMissingError
	switch {
	case errors.As(err, &missing):
		return "", &InvalidError{Field: "passphrase", Reason: "the private key is protected by a passphrase; give it"}
	case err != nil:
		return "", &InvalidError{Field: "privateKey", Reason: "the private key could not be read (is it a PEM or OpenSSH private key, and is the passphrase right?)"}
	}
	return ssh.FingerprintSHA256(signer.PublicKey()), nil
}

func validateTarget(in TargetInput, creating bool) error {
	if creating && !namePattern.MatchString(in.Name) {
		return &InvalidError{Field: "name", Reason: "the name may use letters, digits, and . _ -, and must start with a letter or digit"}
	}
	if !hostPattern.MatchString(in.Address) {
		return &InvalidError{Field: "address", Reason: "the address must be a host name or an IP address"}
	}
	if in.Port != 0 && (in.Port < 1 || in.Port > 65535) {
		return &InvalidError{Field: "port", Reason: "the port must be between 1 and 65535"}
	}
	return nil
}

func (s *Store) seal(table string, id int64, field string, value *string) ([]byte, error) {
	if value == nil || *value == "" {
		return nil, nil
	}
	if s.sealer == nil {
		return nil, ErrNoSecretsKey
	}
	return s.sealer.Seal([]byte(*value), binding(table, id, field))
}

func uniqueViolation(err error) bool {
	var pg *pgconn.PgError
	return errors.As(err, &pg) && pg.Code == "23505"
}

const credentialColumns = `id, name, username, run_as, sealed_private_key IS NOT NULL, sealed_passphrase IS NOT NULL,
	sealed_password IS NOT NULL, key_fingerprint, updated_by, updated_at`

func scanCredential(row pgx.Row) (Credential, error) {
	var c Credential
	err := row.Scan(&c.ID, &c.Name, &c.Username, &c.RunAs, &c.HasKey, &c.HasPassphrase, &c.HasPassword, &c.KeyFingerprint, &c.UpdatedBy, &c.UpdatedAt)
	return c, err
}

// CreateCredential stores a shared credential.
func (s *Store) CreateCredential(ctx context.Context, name string, l Login, actor string) (Credential, error) {
	if !namePattern.MatchString(name) {
		return Credential{}, &InvalidError{Field: "name", Reason: "the name may use letters, digits, and . _ -, and must start with a letter or digit"}
	}
	fp, err := parseLogin(l, true)
	if err != nil {
		return Credential{}, err
	}
	hasKey, hasPass := l.PrivateKey != nil && *l.PrivateKey != "", l.Password != nil && *l.Password != ""
	if !hasKey && !hasPass {
		return Credential{}, &InvalidError{Field: "privateKey", Reason: "give a private key or a password"}
	}
	if s.sealer == nil {
		return Credential{}, ErrNoSecretsKey
	}
	var out Credential
	err = pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		var id int64
		// The secret columns need a value to satisfy the check, and the
		// real one is sealed against the id the row is given: sealed
		// below, in this transaction, before anything can read the row.
		if err := tx.QueryRow(ctx,
			`INSERT INTO agentless_credentials (name, username, run_as, sealed_password, key_fingerprint, created_by, updated_by)
			 VALUES ($1, $2, $3, '\x00', $4, $5, $5) RETURNING id`, name, l.Username, l.RunAs, fp, actor).Scan(&id); err != nil {
			return err
		}
		key, err := s.seal("credentials", id, "private_key", l.PrivateKey)
		if err != nil {
			return err
		}
		pass, err := s.seal("credentials", id, "passphrase", l.Passphrase)
		if err != nil {
			return err
		}
		pw, err := s.seal("credentials", id, "password", l.Password)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE agentless_credentials SET sealed_private_key = $2, sealed_passphrase = $3, sealed_password = $4 WHERE id = $1`, id, key, pass, pw); err != nil {
			return err
		}
		out, err = scanCredential(tx.QueryRow(ctx, `SELECT `+credentialColumns+` FROM agentless_credentials WHERE id = $1`, id))
		return err
	})
	if uniqueViolation(err) {
		return Credential{}, ErrNameTaken
	}
	if err != nil {
		return Credential{}, fmt.Errorf("create credential: %w", err)
	}
	return out, nil
}

// UpdateCredential changes a shared credential. Name changes are not
// supported: targets refer to it by id, but audit entries and operators
// refer to it by name.
func (s *Store) UpdateCredential(ctx context.Context, id int64, l Login, actor string) (Credential, error) {
	var out Credential
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		cur, err := s.openSecretsTx(ctx, tx, "agentless_credentials", "credentials", id)
		if err != nil {
			return err
		}
		merged := mergeSecrets(cur, l.Secrets)
		if merged.PrivateKey == nil && merged.Password == nil {
			return &InvalidError{Field: "privateKey", Reason: "a credential needs a private key or a password"}
		}
		username := cur.username
		if l.Username != "" {
			username = l.Username
		}
		if _, err := parseLogin(Login{Username: username, RunAs: l.RunAs}, true); err != nil {
			return err
		}
		fp := ""
		if merged.PrivateKey != nil {
			if fp, err = parseLogin(Login{Username: username, Secrets: merged}, true); err != nil {
				return err
			}
		}
		if err := s.writeSecretsTx(ctx, tx, "agentless_credentials", "credentials", id, merged, fp); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE agentless_credentials SET username = $2, run_as = $3, updated_by = $4, updated_at = now() WHERE id = $1`, id, username, l.RunAs, actor); err != nil {
			return err
		}
		out, err = scanCredential(tx.QueryRow(ctx, `SELECT `+credentialColumns+` FROM agentless_credentials WHERE id = $1`, id))
		return err
	})
	if err != nil {
		return Credential{}, wrap("update credential", err)
	}
	return out, nil
}

type openSecrets struct {
	username string
	Secrets
}

func mergeSecrets(cur openSecrets, in Secrets) Secrets {
	pick := func(old, new *string) *string {
		if new == nil {
			return old
		}
		if *new == "" {
			return nil
		}
		return new
	}
	return Secrets{PrivateKey: pick(cur.PrivateKey, in.PrivateKey), Passphrase: pick(cur.Passphrase, in.Passphrase), Password: pick(cur.Password, in.Password)}
}

// openSecretsTx reads and opens a row's current secrets, so an update can
// keep those it was not given and re-check a key against its passphrase.
func (s *Store) openSecretsTx(ctx context.Context, tx pgx.Tx, table, kind string, id int64) (openSecrets, error) {
	var cur openSecrets
	var key, pass, pw []byte
	err := tx.QueryRow(ctx, fmt.Sprintf(`SELECT username, sealed_private_key, sealed_passphrase, sealed_password FROM %s WHERE id = $1 FOR UPDATE`, table), id).
		Scan(&cur.username, &key, &pass, &pw)
	if errors.Is(err, pgx.ErrNoRows) {
		return cur, ErrNotFound
	}
	if err != nil {
		return cur, err
	}
	if (key != nil || pass != nil || pw != nil) && s.sealer == nil {
		return cur, ErrNoSecretsKey
	}
	open := func(b []byte, field string) (*string, error) {
		if b == nil {
			return nil, nil
		}
		plain, err := s.sealer.Open(b, binding(kind, id, field))
		if err != nil {
			return nil, errors.New("a stored secret could not be opened with the configured secrets key")
		}
		v := string(plain)
		return &v, nil
	}
	if cur.PrivateKey, err = open(key, "private_key"); err != nil {
		return cur, err
	}
	if cur.Passphrase, err = open(pass, "passphrase"); err != nil {
		return cur, err
	}
	if cur.Password, err = open(pw, "password"); err != nil {
		return cur, err
	}
	return cur, nil
}

func (s *Store) writeSecretsTx(ctx context.Context, tx pgx.Tx, table, kind string, id int64, sec Secrets, fingerprint string) error {
	key, err := s.seal(kind, id, "private_key", sec.PrivateKey)
	if err != nil {
		return err
	}
	pass, err := s.seal(kind, id, "passphrase", sec.Passphrase)
	if err != nil {
		return err
	}
	pw, err := s.seal(kind, id, "password", sec.Password)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, fmt.Sprintf(`UPDATE %s SET sealed_private_key = $2, sealed_passphrase = $3, sealed_password = $4, key_fingerprint = $5 WHERE id = $1`, table), id, key, pass, pw, fingerprint)
	return err
}

func wrap(what string, err error) error {
	var invalid *InvalidError
	switch {
	case errors.Is(err, ErrNotFound), errors.Is(err, ErrNameTaken), errors.Is(err, ErrNoSecretsKey), errors.As(err, &invalid):
		return err
	}
	var inUse *InUseError
	if errors.As(err, &inUse) {
		return err
	}
	return fmt.Errorf("%s: %w", what, err)
}

// DeleteCredential removes a shared credential nothing uses.
func (s *Store) DeleteCredential(ctx context.Context, id int64) error {
	var using []string
	rows, err := s.pool.Query(ctx, `SELECT name FROM agentless_targets WHERE credential_id = $1 ORDER BY name`, id)
	if err != nil {
		return fmt.Errorf("delete credential: %w", err)
	}
	using, err = pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return fmt.Errorf("delete credential: %w", err)
	}
	if len(using) > 0 {
		return &InUseError{Targets: using}
	}
	tag, err := s.pool.Exec(ctx, `DELETE FROM agentless_credentials WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("delete credential: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// GetCredential reads a shared credential, with the targets that use it.
func (s *Store) GetCredential(ctx context.Context, id int64) (Credential, error) {
	c, err := scanCredential(s.pool.QueryRow(ctx, `SELECT `+credentialColumns+` FROM agentless_credentials WHERE id = $1`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return Credential{}, ErrNotFound
	}
	if err != nil {
		return Credential{}, fmt.Errorf("get credential: %w", err)
	}
	rows, err := s.pool.Query(ctx, `SELECT name FROM agentless_targets WHERE credential_id = $1 ORDER BY name`, id)
	if err != nil {
		return Credential{}, fmt.Errorf("get credential: %w", err)
	}
	if c.Targets, err = pgx.CollectRows(rows, pgx.RowTo[string]); err != nil {
		return Credential{}, fmt.Errorf("get credential: %w", err)
	}
	return c, nil
}

// ListCredentials lists shared credentials by name.
func (s *Store) ListCredentials(ctx context.Context) ([]Credential, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+credentialColumns+` FROM agentless_credentials ORDER BY name`)
	if err != nil {
		return nil, fmt.Errorf("list credentials: %w", err)
	}
	out, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (Credential, error) { return scanCredential(r) })
	if err != nil {
		return nil, fmt.Errorf("list credentials: %w", err)
	}
	return out, nil
}

const targetColumns = `t.id, t.name, t.address, t.port, t.credential_id, COALESCE(c.name, ''),
	COALESCE(c.username, t.username), COALESCE(c.run_as, t.run_as),
	COALESCE(c.sealed_private_key, t.sealed_private_key) IS NOT NULL, COALESCE(c.sealed_password, t.sealed_password) IS NOT NULL,
	CASE WHEN c.id IS NOT NULL THEN c.key_fingerprint ELSE t.key_fingerprint END,
	t.pinned_fingerprint, t.proposed_fingerprint, t.key_changed, t.presented_fingerprint,
	t.last_check_at, t.last_check_ok, t.last_check_stage, t.last_check_error, t.updated_by, t.updated_at`

const targetFrom = ` FROM agentless_targets t LEFT JOIN agentless_credentials c ON c.id = t.credential_id`

func scanTarget(row pgx.Row) (Target, error) {
	var t Target
	err := row.Scan(&t.ID, &t.Name, &t.Address, &t.Port, &t.CredentialID, &t.CredentialName, &t.Username, &t.RunAs, &t.HasKey, &t.HasPassword, &t.KeyFingerprint,
		&t.HostKey.Pinned, &t.HostKey.Proposed, &t.HostKey.Changed, &t.HostKey.Presented,
		&t.Check.At, &t.Check.OK, &t.Check.Stage, &t.Check.Error, &t.UpdatedBy, &t.UpdatedAt)
	return t, err
}

// CreateTarget registers a target.
func (s *Store) CreateTarget(ctx context.Context, in TargetInput, actor string) (Target, error) {
	if err := validateTarget(in, true); err != nil {
		return Target{}, err
	}
	port := in.Port
	if port == 0 {
		port = 22
	}
	var fp string
	if in.CredentialID != nil {
		if in.Login.Username != "" || in.Login.PrivateKey != nil || in.Login.Password != nil {
			return Target{}, &InvalidError{Field: "credentialId", Reason: "give a shared credential or a login of the target's own, not both"}
		}
	} else {
		var err error
		if fp, err = parseLogin(in.Login, true); err != nil {
			return Target{}, err
		}
		if (in.Login.PrivateKey == nil || *in.Login.PrivateKey == "") && (in.Login.Password == nil || *in.Login.Password == "") {
			return Target{}, &InvalidError{Field: "privateKey", Reason: "give a private key or a password, or choose a shared credential"}
		}
		if s.sealer == nil {
			return Target{}, ErrNoSecretsKey
		}
	}
	var out Target
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		var id int64
		var err error
		if in.CredentialID != nil {
			err = tx.QueryRow(ctx, `INSERT INTO agentless_targets (name, address, port, credential_id, created_by, updated_by) VALUES ($1,$2,$3,$4,$5,$5) RETURNING id`,
				in.Name, in.Address, port, *in.CredentialID, actor).Scan(&id)
		} else {
			// See CreateCredential for why a placeholder is written first.
			err = tx.QueryRow(ctx, `INSERT INTO agentless_targets (name, address, port, username, run_as, sealed_password, key_fingerprint, created_by, updated_by)
				VALUES ($1,$2,$3,$4,$5,'\x00',$6,$7,$7) RETURNING id`, in.Name, in.Address, port, in.Login.Username, in.Login.RunAs, fp, actor).Scan(&id)
			if err == nil {
				err = s.writeSecretsTx(ctx, tx, "agentless_targets", "targets", id, in.Login.Secrets, fp)
			}
		}
		if err != nil {
			return err
		}
		out, err = scanTarget(tx.QueryRow(ctx, `SELECT `+targetColumns+targetFrom+` WHERE t.id = $1`, id))
		return err
	})
	var pg *pgconn.PgError
	switch {
	case uniqueViolation(err):
		return Target{}, ErrNameTaken
	case errors.As(err, &pg) && pg.Code == "23503":
		return Target{}, &InvalidError{Field: "credentialId", Reason: "that shared credential does not exist"}
	case err != nil:
		return Target{}, wrap("create target", err)
	}
	return out, nil
}

// UpdateTarget changes a target's address, port and login. Changing the
// address clears the host key pin: a pin vouches for a host, not a name.
func (s *Store) UpdateTarget(ctx context.Context, id int64, in TargetInput, actor string) (Target, error) {
	if err := validateTarget(in, false); err != nil {
		return Target{}, err
	}
	var out Target
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		var oldAddr string
		var oldPort int
		var oldCred *int64
		if err := tx.QueryRow(ctx, `SELECT address, port, credential_id FROM agentless_targets WHERE id = $1 FOR UPDATE`, id).Scan(&oldAddr, &oldPort, &oldCred); errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		} else if err != nil {
			return err
		}
		// A port left out is the port it has; an address always is given.
		port := in.Port
		if port == 0 {
			port = oldPort
		}
		if in.CredentialID != nil {
			if in.Login.Username != "" || in.Login.PrivateKey != nil || in.Login.Password != nil {
				return &InvalidError{Field: "credentialId", Reason: "give a shared credential or a login of the target's own, not both"}
			}
			if _, err := tx.Exec(ctx, `UPDATE agentless_targets SET credential_id = $2, username = '', run_as = '', sealed_private_key = NULL, sealed_passphrase = NULL, sealed_password = NULL, key_fingerprint = '' WHERE id = $1`, id, *in.CredentialID); err != nil {
				return err
			}
		} else if oldCred != nil {
			// Switching from a shared credential to a login of its own.
			fp, err := parseLogin(in.Login, true)
			if err != nil {
				return err
			}
			if (in.Login.PrivateKey == nil || *in.Login.PrivateKey == "") && (in.Login.Password == nil || *in.Login.Password == "") {
				return &InvalidError{Field: "privateKey", Reason: "give a private key or a password for the target's own login"}
			}
			if _, err := tx.Exec(ctx, `UPDATE agentless_targets SET credential_id = NULL, username = $2, run_as = $3, sealed_password = '\x00' WHERE id = $1`, id, in.Login.Username, in.Login.RunAs); err != nil {
				return err
			}
			if err := s.writeSecretsTx(ctx, tx, "agentless_targets", "targets", id, in.Login.Secrets, fp); err != nil {
				return err
			}
		} else {
			cur, err := s.openSecretsTx(ctx, tx, "agentless_targets", "targets", id)
			if err != nil {
				return err
			}
			merged := mergeSecrets(cur, in.Login.Secrets)
			if merged.PrivateKey == nil && merged.Password == nil {
				return &InvalidError{Field: "privateKey", Reason: "a target needs a private key or a password"}
			}
			username := cur.username
			if in.Login.Username != "" {
				username = in.Login.Username
			}
			fp, err := parseLogin(Login{Username: username, RunAs: in.Login.RunAs, Secrets: merged}, true)
			if err != nil {
				return err
			}
			if err := s.writeSecretsTx(ctx, tx, "agentless_targets", "targets", id, merged, fp); err != nil {
				return err
			}
			if _, err := tx.Exec(ctx, `UPDATE agentless_targets SET username = $2, run_as = $3 WHERE id = $1`, id, username, in.Login.RunAs); err != nil {
				return err
			}
		}
		if _, err := tx.Exec(ctx, `UPDATE agentless_targets SET address = $2, port = $3, updated_by = $4, updated_at = now() WHERE id = $1`, id, in.Address, port, actor); err != nil {
			return err
		}
		if oldAddr != in.Address || oldPort != port {
			if _, err := tx.Exec(ctx, `UPDATE agentless_targets SET pinned_key = NULL, pinned_fingerprint = '', proposed_key = NULL, proposed_fingerprint = '', key_changed = FALSE, presented_fingerprint = '' WHERE id = $1`, id); err != nil {
				return err
			}
		}
		var err error
		out, err = scanTarget(tx.QueryRow(ctx, `SELECT `+targetColumns+targetFrom+` WHERE t.id = $1`, id))
		return err
	})
	var pg *pgconn.PgError
	if errors.As(err, &pg) && pg.Code == "23503" {
		return Target{}, &InvalidError{Field: "credentialId", Reason: "that shared credential does not exist"}
	}
	if err != nil {
		return Target{}, wrap("update target", err)
	}
	return out, nil
}

// DeleteTarget removes a target.
func (s *Store) DeleteTarget(ctx context.Context, id int64) error {
	tag, err := s.pool.Exec(ctx, `DELETE FROM agentless_targets WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("delete target: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// GetTarget reads a target by id.
func (s *Store) GetTarget(ctx context.Context, id int64) (Target, error) {
	t, err := scanTarget(s.pool.QueryRow(ctx, `SELECT `+targetColumns+targetFrom+` WHERE t.id = $1`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return Target{}, ErrNotFound
	}
	if err != nil {
		return Target{}, fmt.Errorf("get target: %w", err)
	}
	return t, nil
}

// GetTargetByName reads a target by its name.
func (s *Store) GetTargetByName(ctx context.Context, name string) (Target, error) {
	t, err := scanTarget(s.pool.QueryRow(ctx, `SELECT `+targetColumns+targetFrom+` WHERE t.name = $1`, name))
	if errors.Is(err, pgx.ErrNoRows) {
		return Target{}, ErrNotFound
	}
	if err != nil {
		return Target{}, fmt.Errorf("get target: %w", err)
	}
	return t, nil
}

// ListTargets lists targets by name.
func (s *Store) ListTargets(ctx context.Context) ([]Target, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+targetColumns+targetFrom+` ORDER BY t.name`)
	if err != nil {
		return nil, fmt.Errorf("list targets: %w", err)
	}
	out, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (Target, error) { return scanTarget(r) })
	if err != nil {
		return nil, fmt.Errorf("list targets: %w", err)
	}
	return out, nil
}

// Names lists every target's name, for routing and for refusing a name
// already in use.
func (s *Store) Names(ctx context.Context) ([]string, error) {
	rows, err := s.pool.Query(ctx, `SELECT name FROM agentless_targets ORDER BY name`)
	if err != nil {
		return nil, fmt.Errorf("list target names: %w", err)
	}
	names, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return nil, fmt.Errorf("list target names: %w", err)
	}
	return names, nil
}

// Connection resolves a target's login with its secrets opened. A target
// with a credential uses it; the target's own login is otherwise used.
func (s *Store) Connection(ctx context.Context, name string) (Connection, error) {
	var (
		conn         Connection
		credID       *int64
		pinned       []byte
		cUser, tUser *string
		cRun, tRun   string
		address      string
		port         int
	)
	err := s.pool.QueryRow(ctx,
		`SELECT t.id, t.address, t.port, t.credential_id, t.username, t.run_as, c.username, COALESCE(c.run_as, ''), t.pinned_key
		 FROM agentless_targets t LEFT JOIN agentless_credentials c ON c.id = t.credential_id WHERE t.name = $1`, name).
		Scan(&conn.ID, &address, &port, &credID, &tUser, &tRun, &cUser, &cRun, &pinned)
	if errors.Is(err, pgx.ErrNoRows) {
		return Connection{}, ErrNotFound
	}
	if err != nil {
		return Connection{}, fmt.Errorf("read target: %w", err)
	}
	conn.Name, conn.Address, conn.Port, conn.PinnedKey = name, address, port, pinned

	table, kind, owner := "agentless_targets", "targets", conn.ID
	conn.Username, conn.RunAs = deref(tUser), tRun
	if credID != nil {
		table, kind, owner = "agentless_credentials", "credentials", *credID
		conn.Username, conn.RunAs = deref(cUser), cRun
	}
	var key, pass, pw []byte
	if err := s.pool.QueryRow(ctx, fmt.Sprintf(`SELECT sealed_private_key, sealed_passphrase, sealed_password FROM %s WHERE id = $1`, table), owner).Scan(&key, &pass, &pw); err != nil {
		return Connection{}, fmt.Errorf("read login: %w", err)
	}
	if s.sealer == nil {
		return Connection{}, ErrNoSecretsKey
	}
	open := func(b []byte, field string) ([]byte, error) {
		if b == nil {
			return nil, nil
		}
		plain, err := s.sealer.Open(b, binding(kind, owner, field))
		if err != nil {
			return nil, errors.New("a stored secret could not be opened with the configured secrets key")
		}
		return plain, nil
	}
	if conn.PrivateKey, err = open(key, "private_key"); err != nil {
		return Connection{}, err
	}
	if conn.Passphrase, err = open(pass, "passphrase"); err != nil {
		return Connection{}, err
	}
	p, err := open(pw, "password")
	if err != nil {
		return Connection{}, err
	}
	conn.Password = string(p)
	return conn, nil
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// PublicKeyFingerprint is the SHA-256 fingerprint of a public key in wire
// format, as ssh-keygen prints it.
func PublicKeyFingerprint(wire []byte) (string, error) {
	pk, err := ssh.ParsePublicKey(wire)
	if err != nil {
		return "", err
	}
	return ssh.FingerprintSHA256(pk), nil
}

// ProposeKey records the host key a check saw on a target with no pin, for
// an operator to accept.
func (s *Store) ProposeKey(ctx context.Context, id int64, wire []byte) (string, error) {
	fp, err := PublicKeyFingerprint(wire)
	if err != nil {
		return "", err
	}
	if _, err := s.pool.Exec(ctx, `UPDATE agentless_targets SET proposed_key = $2, proposed_fingerprint = $3 WHERE id = $1`, id, wire, fp); err != nil {
		return "", fmt.Errorf("record proposed host key: %w", err)
	}
	return fp, nil
}

// FlagChangedKey records that a pinned target presented another key.
func (s *Store) FlagChangedKey(ctx context.Context, id int64, wire []byte) (string, error) {
	fp, err := PublicKeyFingerprint(wire)
	if err != nil {
		return "", err
	}
	if _, err := s.pool.Exec(ctx, `UPDATE agentless_targets SET key_changed = TRUE, presented_fingerprint = $3, proposed_key = $2, proposed_fingerprint = $3 WHERE id = $1`, id, wire, fp); err != nil {
		return "", fmt.Errorf("record changed host key: %w", err)
	}
	return fp, nil
}

// AcceptKey pins the proposed key, provided it is the one the operator was
// shown (fingerprint). It does not replace an existing pin: see ReplaceKey.
func (s *Store) AcceptKey(ctx context.Context, id int64, fingerprint string) (Target, error) {
	return s.pin(ctx, id, fingerprint, false)
}

// ReplaceKey pins the proposed key over an existing pin - the explicit
// action a changed key needs.
func (s *Store) ReplaceKey(ctx context.Context, id int64, fingerprint string) (Target, error) {
	return s.pin(ctx, id, fingerprint, true)
}

func (s *Store) pin(ctx context.Context, id int64, fingerprint string, replace bool) (Target, error) {
	var out Target
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		var proposed []byte
		var proposedFP, pinnedFP string
		if err := tx.QueryRow(ctx, `SELECT proposed_key, proposed_fingerprint, pinned_fingerprint FROM agentless_targets WHERE id = $1 FOR UPDATE`, id).Scan(&proposed, &proposedFP, &pinnedFP); errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		} else if err != nil {
			return err
		}
		if proposed == nil || proposedFP != fingerprint {
			return ErrKeyNotPending
		}
		if pinnedFP != "" && !replace {
			return &InvalidError{Field: "fingerprint", Reason: "this target already has a pinned host key; replace it explicitly"}
		}
		if _, err := tx.Exec(ctx, `UPDATE agentless_targets SET pinned_key = proposed_key, pinned_fingerprint = proposed_fingerprint,
			proposed_key = NULL, proposed_fingerprint = '', key_changed = FALSE, presented_fingerprint = '' WHERE id = $1`, id); err != nil {
			return err
		}
		var err error
		out, err = scanTarget(tx.QueryRow(ctx, `SELECT `+targetColumns+targetFrom+` WHERE t.id = $1`, id))
		return err
	})
	if err != nil {
		return Target{}, wrap("pin host key", err)
	}
	return out, nil
}

// RecordCheck stores the result of a connectivity check.
func (s *Store) RecordCheck(ctx context.Context, id int64, ok bool, stage, message string) error {
	if _, err := s.pool.Exec(ctx, `UPDATE agentless_targets SET last_check_at = now(), last_check_ok = $2, last_check_stage = $3, last_check_error = $4 WHERE id = $1`, id, ok, stage, message); err != nil {
		return fmt.Errorf("record check: %w", err)
	}
	return nil
}
