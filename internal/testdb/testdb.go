// Package testdb gives a test a connection pool to a real Postgres
// instance whose schema is already migrated.
//
// It exists because the alternative - every package's own testStore
// helper opening a pool and assuming the tables are already there -
// makes the suite silently depend on the order packages run in. Only
// internal/persistence's tests applied migrations, so with `go test -p 1
// ./...` every package sorting before it (activity, classifier,
// codemanager, orchestrator) passed locally against an
// already-migrated development database and failed against a fresh one
// with `relation "..." does not exist`.
//
// Migrations run once per test binary, on first use.
//
// It's a normal (non-_test.go) package specifically so its helpers can
// be imported by other packages' tests - never imported by any
// production code path. Note that internal/persistence's own tests must
// not use it: that would be an import cycle, and they already migrate.
package testdb

import (
	"context"
	"os"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/voxpupuli/enterprise-console/internal/persistence"
)

var (
	migrateOnce sync.Once
	migrateErr  error
)

// Pool returns a pool to the database named by CONSOLE_TEST_POSTGRES_DSN,
// skipping the test when that is unset (matching how these integration
// tests have always behaved) and failing it if migrations cannot be
// applied. The pool is closed when the test finishes.
func Pool(t *testing.T) *pgxpool.Pool {
	t.Helper()

	dsn := os.Getenv("CONSOLE_TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("CONSOLE_TEST_POSTGRES_DSN not set; skipping integration test")
	}

	migrateOnce.Do(func() { migrateErr = persistence.Migrate(dsn) })
	if migrateErr != nil {
		t.Fatalf("apply migrations to test database: %v", migrateErr)
	}

	pool, err := pgxpool.New(context.Background(), dsn)
	if err != nil {
		t.Fatalf("pgxpool.New() error: %v", err)
	}
	t.Cleanup(pool.Close)

	return pool
}
