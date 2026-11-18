package rbac

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
)

// AccountPolicy is the installation's rules for local accounts: what a
// password must be, and when repeated failed logins lock an account
// (see openspec/specs/account-policy).
//
// It is one row in Postgres rather than configuration, so every
// instance of a cluster applies the same policy without a restart and a
// change leaves an audit entry.
type AccountPolicy struct {
	// MinLength is the fewest characters a password may have.
	MinLength        int  `json:"minLength"`
	RequireLowercase bool `json:"requireLowercase"`
	RequireUppercase bool `json:"requireUppercase"`
	RequireDigit     bool `json:"requireDigit"`
	// RequireSymbol asks for a character that is not a letter, a digit
	// or a space.
	RequireSymbol bool `json:"requireSymbol"`
	// LockoutThreshold is how many consecutive failed password logins
	// lock an account. Zero turns lockout off.
	LockoutThreshold int `json:"lockoutThreshold"`
	// LockoutMinutes is how long a lock lasts.
	LockoutMinutes int `json:"lockoutMinutes"`

	UpdatedBy string     `json:"updatedBy,omitempty"`
	UpdatedAt *time.Time `json:"updatedAt,omitempty"`
}

// The ranges a policy's values may take.
const (
	MinPasswordLengthFloor   = 8
	MinPasswordLengthCeiling = MaxPasswordBytes
	MinLockoutThreshold      = 3
	MaxLockoutThreshold      = 100
	MinLockoutMinutes        = 1
	MaxLockoutMinutes        = 1440
)

// MaxPasswordBytes is bcrypt's limit: it ignores, or in newer versions
// of the library refuses, everything past 72 bytes. A longer password
// is refused here rather than silently cut short.
const MaxPasswordBytes = 72

// DefaultAccountPolicy is the policy of an installation that has never
// changed it.
func DefaultAccountPolicy() AccountPolicy {
	return AccountPolicy{MinLength: 12, LockoutThreshold: 10, LockoutMinutes: 15}
}

// LockoutPeriod is how long a lock lasts.
func (p AccountPolicy) LockoutPeriod() time.Duration {
	return time.Duration(p.LockoutMinutes) * time.Minute
}

// InvalidPolicyError is a policy refused for one of its values.
type InvalidPolicyError struct {
	Field  string
	Reason string
}

func (e *InvalidPolicyError) Error() string { return e.Reason }

// Validate refuses a value outside its range, naming the field.
func (p AccountPolicy) Validate() error {
	switch {
	case p.MinLength < MinPasswordLengthFloor || p.MinLength > MinPasswordLengthCeiling:
		return &InvalidPolicyError{Field: "minLength", Reason: fmt.Sprintf("the minimum password length must be between %d and %d", MinPasswordLengthFloor, MinPasswordLengthCeiling)}
	case p.LockoutThreshold != 0 && (p.LockoutThreshold < MinLockoutThreshold || p.LockoutThreshold > MaxLockoutThreshold):
		return &InvalidPolicyError{Field: "lockoutThreshold", Reason: fmt.Sprintf("the number of failed logins before an account is locked must be 0 (never lock) or between %d and %d", MinLockoutThreshold, MaxLockoutThreshold)}
	case p.LockoutMinutes < MinLockoutMinutes || p.LockoutMinutes > MaxLockoutMinutes:
		return &InvalidPolicyError{Field: "lockoutMinutes", Reason: fmt.Sprintf("the time an account stays locked must be between %d and %d minutes", MinLockoutMinutes, MaxLockoutMinutes)}
	}
	return nil
}

// Password rule codes, as a refusal lists them. They are identifiers
// for a client to translate; PasswordRuleError.Error spells them out.
const (
	RuleMinLength = "min_length"
	RuleLowercase = "lowercase"
	RuleUppercase = "uppercase"
	RuleDigit     = "digit"
	RuleSymbol    = "symbol"
	RuleMaxLength = "max_length"
	RuleUsername  = "username"
)

// CheckPassword returns the rules password fails for an account named
// username under policy, in a fixed order; none means it is acceptable.
// Length is counted in characters for the minimum, as a person counts
// it, and in bytes for the maximum, as bcrypt does.
func CheckPassword(policy AccountPolicy, username, password string) []string {
	var failed []string
	if utf8.RuneCountInString(password) < policy.MinLength {
		failed = append(failed, RuleMinLength)
	}
	var lower, upper, digit, symbol bool
	for _, r := range password {
		switch {
		case unicode.IsLower(r):
			lower = true
		case unicode.IsUpper(r):
			upper = true
		case unicode.IsDigit(r):
			digit = true
		case !unicode.IsLetter(r) && !unicode.IsSpace(r):
			symbol = true
		}
	}
	if policy.RequireLowercase && !lower {
		failed = append(failed, RuleLowercase)
	}
	if policy.RequireUppercase && !upper {
		failed = append(failed, RuleUppercase)
	}
	if policy.RequireDigit && !digit {
		failed = append(failed, RuleDigit)
	}
	if policy.RequireSymbol && !symbol {
		failed = append(failed, RuleSymbol)
	}
	if len(password) > MaxPasswordBytes {
		failed = append(failed, RuleMaxLength)
	}
	if username != "" && strings.EqualFold(password, username) {
		failed = append(failed, RuleUsername)
	}
	return failed
}

// PasswordRuleError is a password refused by the account policy. It
// names the rules, never the password.
type PasswordRuleError struct {
	Rules  []string
	Policy AccountPolicy
}

func (e *PasswordRuleError) Error() string {
	parts := make([]string, len(e.Rules))
	for i, rule := range e.Rules {
		switch rule {
		case RuleMinLength:
			parts[i] = fmt.Sprintf("it needs at least %d characters", e.Policy.MinLength)
		case RuleLowercase:
			parts[i] = "it needs a lowercase letter"
		case RuleUppercase:
			parts[i] = "it needs an uppercase letter"
		case RuleDigit:
			parts[i] = "it needs a digit"
		case RuleSymbol:
			parts[i] = "it needs a symbol"
		case RuleMaxLength:
			parts[i] = fmt.Sprintf("it may be at most %d bytes long", MaxPasswordBytes)
		case RuleUsername:
			parts[i] = "it may not be the username"
		default:
			parts[i] = rule
		}
	}
	return "the password does not meet the account policy: " + strings.Join(parts, "; ")
}

// checkPassword is CheckPassword as an error.
func checkPassword(policy AccountPolicy, username, password string) error {
	if failed := CheckPassword(policy, username, password); len(failed) > 0 {
		return &PasswordRuleError{Rules: failed, Policy: policy}
	}
	return nil
}

// AccountPolicy returns the stored policy, or the defaults when none
// has ever been saved.
func (s *Store) AccountPolicy(ctx context.Context) (AccountPolicy, error) {
	var p AccountPolicy
	var at time.Time
	err := s.pool.QueryRow(ctx,
		`SELECT min_length, require_lowercase, require_uppercase, require_digit, require_symbol,
		        lockout_threshold, lockout_minutes, updated_by, updated_at
		 FROM account_policy WHERE id = 1`,
	).Scan(&p.MinLength, &p.RequireLowercase, &p.RequireUppercase, &p.RequireDigit, &p.RequireSymbol,
		&p.LockoutThreshold, &p.LockoutMinutes, &p.UpdatedBy, &at)
	if errors.Is(err, pgx.ErrNoRows) {
		return DefaultAccountPolicy(), nil
	}
	if err != nil {
		return AccountPolicy{}, fmt.Errorf("read account policy: %w", err)
	}
	p.UpdatedAt = &at
	return p, nil
}

// SetAccountPolicy stores p as the installation's policy. It takes
// effect at once on every instance: the policy is read from the
// database each time it is applied.
func (s *Store) SetAccountPolicy(ctx context.Context, p AccountPolicy, by string) (AccountPolicy, error) {
	if err := p.Validate(); err != nil {
		return AccountPolicy{}, err
	}
	if _, err := s.pool.Exec(ctx,
		`INSERT INTO account_policy (id, min_length, require_lowercase, require_uppercase, require_digit, require_symbol,
		                             lockout_threshold, lockout_minutes, updated_by, updated_at)
		 VALUES (1, $1, $2, $3, $4, $5, $6, $7, $8, now())
		 ON CONFLICT (id) DO UPDATE SET min_length = EXCLUDED.min_length, require_lowercase = EXCLUDED.require_lowercase,
		   require_uppercase = EXCLUDED.require_uppercase, require_digit = EXCLUDED.require_digit,
		   require_symbol = EXCLUDED.require_symbol, lockout_threshold = EXCLUDED.lockout_threshold,
		   lockout_minutes = EXCLUDED.lockout_minutes, updated_by = EXCLUDED.updated_by, updated_at = now()`,
		p.MinLength, p.RequireLowercase, p.RequireUppercase, p.RequireDigit, p.RequireSymbol,
		p.LockoutThreshold, p.LockoutMinutes, by); err != nil {
		return AccountPolicy{}, fmt.Errorf("save account policy: %w", err)
	}
	return s.AccountPolicy(ctx)
}

// RecordFailedLogin counts one failed password login for user id under
// a policy that locks at threshold failures for period. It reports
// whether this failure locked the account, and until when.
//
// It is one statement, so failures arriving at several instances at
// once are each counted, and it is judged by the database's clock. A
// failure while the account is locked changes nothing: the lock does
// not grow. The first failure after a lock has passed starts the count
// again at one.
func (s *Store) RecordFailedLogin(ctx context.Context, id int64, threshold int, period time.Duration) (locked bool, until *time.Time, err error) {
	if threshold <= 0 {
		return false, nil, nil
	}
	err = s.pool.QueryRow(ctx,
		`UPDATE users u
		    SET failed_logins = n.count,
		        locked_until  = CASE WHEN n.count >= $2 THEN now() + $3 * interval '1 second' ELSE NULL END
		   FROM (SELECT id, CASE WHEN locked_until IS NOT NULL THEN 1 ELSE failed_logins + 1 END AS count
		           FROM users WHERE id = $1 AND (locked_until IS NULL OR locked_until <= now()) FOR UPDATE) n
		  WHERE u.id = n.id
		 RETURNING u.locked_until`,
		id, threshold, int64(period/time.Second),
	).Scan(&until)
	if errors.Is(err, pgx.ErrNoRows) {
		// Already locked (or gone): nothing to count.
		return false, nil, nil
	}
	if err != nil {
		return false, nil, fmt.Errorf("record failed login: %w", err)
	}
	return until != nil, until, nil
}

// ResetFailedLogins clears user id's failed-login count and any lock,
// and reports whether the account was locked.
func (s *Store) ResetFailedLogins(ctx context.Context, id int64) (wasLocked bool, err error) {
	err = s.pool.QueryRow(ctx,
		`UPDATE users u SET failed_logins = 0, locked_until = NULL
		   FROM (SELECT id, (locked_until IS NOT NULL AND locked_until > now()) AS locked FROM users WHERE id = $1 FOR UPDATE) o
		  WHERE u.id = o.id
		 RETURNING o.locked`, id,
	).Scan(&wasLocked)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, ErrNotFound
	}
	if err != nil {
		return false, fmt.Errorf("reset failed logins: %w", err)
	}
	return wasLocked, nil
}
