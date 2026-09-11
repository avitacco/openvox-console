package rbac

import (
	"context"
	"fmt"
	"time"
)

// PersistRevocation records jti as revoked, durably, until expiresAt.
// Safe to call more than once for the same jti (e.g. a duplicate NATS
// delivery) - it just overwrites the same row.
func (s *Store) PersistRevocation(ctx context.Context, jti string, expiresAt time.Time) error {
	_, err := s.pool.Exec(ctx,
		`INSERT INTO revoked_tokens (jti, expires_at) VALUES ($1, $2)
		 ON CONFLICT (jti) DO UPDATE SET expires_at = EXCLUDED.expires_at`,
		jti, expiresAt,
	)
	if err != nil {
		return fmt.Errorf("persist revocation: %w", err)
	}
	return nil
}

// UnexpiredRevocations returns every revoked jti whose token hasn't
// naturally expired yet - loaded into a fresh instance's in-memory set at
// startup. Already-expired entries are skipped: the token itself would
// already fail expiry validation, so there's nothing to gain from keeping
// them in memory.
func (s *Store) UnexpiredRevocations(ctx context.Context) ([]revocationEvent, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT jti, expires_at FROM revoked_tokens WHERE expires_at > now()`,
	)
	if err != nil {
		return nil, fmt.Errorf("load unexpired revocations: %w", err)
	}
	defer rows.Close()

	events := make([]revocationEvent, 0)
	for rows.Next() {
		var e revocationEvent
		if err := rows.Scan(&e.JTI, &e.ExpiresAt); err != nil {
			return nil, fmt.Errorf("load unexpired revocations: %w", err)
		}
		events = append(events, e)
	}
	return events, rows.Err()
}
