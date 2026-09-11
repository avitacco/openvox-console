package rbac

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// PersistOIDCState records the PKCE verifier and nonce for an
// in-progress OIDC login, keyed by state, so the callback (which may
// land on a different console instance) can retrieve them. Also
// opportunistically sweeps expired rows from prior, abandoned login
// attempts.
func (s *Store) PersistOIDCState(ctx context.Context, state, verifier, nonce string, expiresAt time.Time) error {
	if _, err := s.pool.Exec(ctx, `DELETE FROM oidc_login_state WHERE expires_at < now()`); err != nil {
		return fmt.Errorf("sweep expired oidc login state: %w", err)
	}

	_, err := s.pool.Exec(ctx,
		`INSERT INTO oidc_login_state (state, pkce_verifier, nonce, expires_at) VALUES ($1, $2, $3, $4)`,
		state, verifier, nonce, expiresAt,
	)
	if err != nil {
		return fmt.Errorf("persist oidc login state: %w", err)
	}
	return nil
}

// ConsumeOIDCState looks up and deletes the login state for state (single
// use, like a refresh token), returning ErrNotFound if it doesn't exist
// or has already expired.
func (s *Store) ConsumeOIDCState(ctx context.Context, state string) (verifier, nonce string, err error) {
	err = s.pool.QueryRow(ctx,
		`DELETE FROM oidc_login_state WHERE state = $1 AND expires_at > now()
		 RETURNING pkce_verifier, nonce`,
		state,
	).Scan(&verifier, &nonce)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", "", ErrNotFound
	}
	if err != nil {
		return "", "", fmt.Errorf("consume oidc login state: %w", err)
	}
	return verifier, nonce, nil
}
