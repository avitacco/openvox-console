package rbac

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// CreateUser creates a user with the given username, hashing password
// with bcrypt before storing it.
func (s *Store) CreateUser(ctx context.Context, username, password string) (User, error) {
	hash, err := hashPassword(password)
	if err != nil {
		return User{}, err
	}

	var u User
	err = s.pool.QueryRow(ctx,
		`INSERT INTO users (username, password_hash) VALUES ($1, $2) RETURNING id, username, password_hash, created_at`,
		username, hash,
	).Scan(&u.ID, &u.Username, &u.PasswordHash, &u.CreatedAt)
	if err != nil {
		return User{}, translateError(err)
	}
	return u, nil
}

// derefOrEmpty returns *s, or "" if s is nil - a User's FirstName/
// LastName/Email are nullable columns, but callers merging a partial
// profile edit (see Handlers.updateUser and OIDCService.syncProfile) work
// with plain strings.
func derefOrEmpty(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func nullIfEmpty(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// UpdateProfile replaces the stored first name, last name, and email for
// the user identified by id - an empty string clears the corresponding
// column. Callers that want to change only some fields (see
// Handlers.updateUser, OIDCService.syncProfile) merge with the current
// value themselves before calling this; the store layer doesn't guess at
// partial-update semantics.
func (s *Store) UpdateProfile(ctx context.Context, id int64, firstName, lastName, email string) error {
	tag, err := s.pool.Exec(ctx,
		`UPDATE users SET first_name = $1, last_name = $2, email = $3, updated_at = now() WHERE id = $4`,
		nullIfEmpty(firstName), nullIfEmpty(lastName), nullIfEmpty(email), id,
	)
	if err != nil {
		return fmt.Errorf("update profile: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// GetUser returns the user identified by id, or ErrNotFound.
func (s *Store) GetUser(ctx context.Context, id int64) (User, error) {
	return s.getUser(ctx, `id = $1`, id)
}

// GetUserByUsername returns the user with the given username, or
// ErrNotFound.
func (s *Store) GetUserByUsername(ctx context.Context, username string) (User, error) {
	return s.getUser(ctx, `username = $1`, username)
}

func (s *Store) getUser(ctx context.Context, where string, arg any) (User, error) {
	var u User
	err := s.pool.QueryRow(ctx,
		`SELECT id, username, password_hash, oidc_subject, first_name, last_name, email, created_at FROM users WHERE `+where,
		arg,
	).Scan(&u.ID, &u.Username, &u.PasswordHash, &u.OIDCSubject, &u.FirstName, &u.LastName, &u.Email, &u.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return User{}, ErrNotFound
	}
	if err != nil {
		return User{}, fmt.Errorf("get user: %w", err)
	}
	return u, nil
}

// GetUserByOIDCSubject returns the user provisioned for the given OIDC
// subject, or ErrNotFound.
func (s *Store) GetUserByOIDCSubject(ctx context.Context, subject string) (User, error) {
	return s.getUser(ctx, `oidc_subject = $1`, subject)
}

// CreateOIDCUser creates a user provisioned via OIDC login, identified by
// its stable OIDC subject rather than a local password. It stores a
// random, never-derivable password hash (see design.md in the
// oidc-authentication change) so password login for this account always
// fails, the same way a wrong password would for any other account.
func (s *Store) CreateOIDCUser(ctx context.Context, subject, username string) (User, error) {
	randomPassword := make([]byte, 32)
	if _, err := rand.Read(randomPassword); err != nil {
		return User{}, fmt.Errorf("generate random password: %w", err)
	}
	hash, err := hashPassword(hex.EncodeToString(randomPassword))
	if err != nil {
		return User{}, err
	}

	var u User
	err = s.pool.QueryRow(ctx,
		`INSERT INTO users (username, password_hash, oidc_subject) VALUES ($1, $2, $3)
		 RETURNING id, username, password_hash, oidc_subject, first_name, last_name, email, created_at`,
		username, hash, subject,
	).Scan(&u.ID, &u.Username, &u.PasswordHash, &u.OIDCSubject, &u.FirstName, &u.LastName, &u.Email, &u.CreatedAt)
	if err != nil {
		return User{}, translateError(err)
	}
	return u, nil
}

// ListUsers returns every user.
func (s *Store) ListUsers(ctx context.Context) ([]User, error) {
	rows, err := s.pool.Query(ctx, `SELECT id, username, password_hash, oidc_subject, first_name, last_name, email, created_at FROM users ORDER BY id`)
	if err != nil {
		return nil, fmt.Errorf("list users: %w", err)
	}
	defer rows.Close()

	users := make([]User, 0)
	for rows.Next() {
		var u User
		if err := rows.Scan(&u.ID, &u.Username, &u.PasswordHash, &u.OIDCSubject, &u.FirstName, &u.LastName, &u.Email, &u.CreatedAt); err != nil {
			return nil, fmt.Errorf("list users: %w", err)
		}
		users = append(users, u)
	}
	return users, rows.Err()
}

// UpdateUsername changes the username for the user identified by id.
func (s *Store) UpdateUsername(ctx context.Context, id int64, username string) error {
	tag, err := s.pool.Exec(ctx,
		`UPDATE users SET username = $1, updated_at = now() WHERE id = $2`,
		username, id,
	)
	if err != nil {
		return translateError(err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// UpdatePassword changes the password for the user identified by id.
func (s *Store) UpdatePassword(ctx context.Context, id int64, password string) error {
	hash, err := hashPassword(password)
	if err != nil {
		return err
	}
	tag, err := s.pool.Exec(ctx,
		`UPDATE users SET password_hash = $1, updated_at = now() WHERE id = $2`,
		hash, id,
	)
	if err != nil {
		return fmt.Errorf("update password: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// DeleteUser removes the user identified by id. Their role assignments
// are removed too (ON DELETE CASCADE); already-issued tokens remain valid
// until they expire or are explicitly revoked - deleting a user prevents
// future logins, it doesn't retroactively invalidate a live session.
func (s *Store) DeleteUser(ctx context.Context, id int64) error {
	tag, err := s.pool.Exec(ctx, `DELETE FROM users WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("delete user: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}
