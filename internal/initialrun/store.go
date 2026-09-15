// Package initialrun dispatches one Puppet run to a node the first time
// it connects with no inventory record, so a freshly enrolled node
// reports its own facts without an operator triggering a run by hand.
// See design.md in add-initial-run-on-first-connect.
package initialrun

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Store records which certnames have already had an initial run
// dispatched for them.
type Store struct {
	pool *pgxpool.Pool
}

// NewStore builds a Store backed by pool.
func NewStore(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool}
}

// Claim attempts to take the initial-run claim for certname, reporting
// whether this caller won it. Only the winner may dispatch.
//
// The insert is the claim: ON CONFLICT DO NOTHING means the row either
// did not exist and now does (won), or already existed (lost), decided
// by Postgres in one statement. There is deliberately no separate "has
// this been claimed?" read - that would reintroduce the race this
// method exists to close, between two console instances handling the
// same connect event, or one instance handling a rapid reconnect.
//
// The claim is written before the run is dispatched, so a crash between
// the two costs one missed automatic run (the node's own scheduled run
// still covers it) rather than dispatching twice. See design.md for why
// that direction was chosen.
func (s *Store) Claim(ctx context.Context, certname string) (bool, error) {
	tag, err := s.pool.Exec(ctx,
		`INSERT INTO initial_run_claims (certname) VALUES ($1)
		 ON CONFLICT (certname) DO NOTHING`, certname)
	if err != nil {
		return false, fmt.Errorf("claim initial run for %q: %w", certname, err)
	}
	return tag.RowsAffected() == 1, nil
}
