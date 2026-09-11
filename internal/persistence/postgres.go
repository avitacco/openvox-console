// Package persistence manages the console's connection to Postgres and the
// schema migration tooling for the single shared schema every capability
// (classifier, RBAC, activity, code manager, orchestrator) adds tables to.
package persistence

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

// DB wraps a Postgres connection pool.
type DB struct {
	Pool *pgxpool.Pool
}

// Connect creates a connection pool for dsn. It does not fail if Postgres
// is unreachable at call time: the pool connects lazily, and reachability
// is reported ongoing via Check (see the health check endpoint).
func Connect(ctx context.Context, dsn string) (*DB, error) {
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		return nil, fmt.Errorf("create postgres connection pool: %w", err)
	}
	return &DB{Pool: pool}, nil
}

// Name identifies this checker in the health check endpoint.
func (db *DB) Name() string { return "postgres" }

// Check reports whether Postgres is currently reachable.
func (db *DB) Check(ctx context.Context) error {
	return db.Pool.Ping(ctx)
}

// Close releases all pooled connections.
func (db *DB) Close() {
	db.Pool.Close()
}
