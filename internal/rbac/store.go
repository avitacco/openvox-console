package rbac

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/crypto/bcrypt"
)

// Domain-level errors, distinguished from raw database errors so callers
// (in particular HTTP handlers) can respond appropriately.
var (
	ErrNotFound      = errors.New("not found")
	ErrDuplicateName = errors.New("a record with this name already exists")
)

// Store persists users, roles, permissions, and tokens in Postgres.
type Store struct {
	pool *pgxpool.Pool
}

// NewStore builds a Store backed by pool.
func NewStore(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool}
}

func translateError(err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		return ErrDuplicateName
	}
	return fmt.Errorf("store error: %w", err)
}

// hashPassword wraps bcrypt with the package's chosen cost, so callers
// never pick their own (and potentially weaker) cost factor.
func hashPassword(password string) (string, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return "", fmt.Errorf("hash password: %w", err)
	}
	return string(hash), nil
}

// dummyPasswordHash is compared against on a login attempt for a username
// that doesn't exist, so the bcrypt cost (and therefore the response
// timing) is the same whether or not the account exists - see
// internal/rbac/auth.go.
var dummyPasswordHash = mustHash("not-a-real-password-used-only-for-timing")

func mustHash(password string) string {
	hash, err := hashPassword(password)
	if err != nil {
		panic(err)
	}
	return hash
}

// UsersEmpty reports whether the users table has no rows - used to decide
// whether to run the first-admin bootstrap at startup.
func (s *Store) UsersEmpty(ctx context.Context) (bool, error) {
	var count int
	if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM users`).Scan(&count); err != nil {
		return false, fmt.Errorf("count users: %w", err)
	}
	return count == 0, nil
}
