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
	"fmt"
	"net/url"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
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

// ScratchPool returns a pool to a new, empty-but-migrated database that
// exists only for this test, for a test whose subject is something the
// shared database cannot show, such as "no user is left" (the shared one
// always has the bootstrap administrator). The database is dropped when
// the test finishes. It skips like Pool when no DSN is configured.
func ScratchPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dsn := os.Getenv("CONSOLE_TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("CONSOLE_TEST_POSTGRES_DSN not set; skipping integration test")
	}
	ctx := context.Background()
	admin, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer admin.Close(ctx)
	name := fmt.Sprintf("testdb_scratch_%d", time.Now().UnixNano())
	if _, err := admin.Exec(ctx, "CREATE DATABASE "+name); err != nil {
		t.Fatalf("create scratch database: %v", err)
	}
	t.Cleanup(func() {
		c, err := pgx.Connect(ctx, dsn)
		if err != nil {
			t.Errorf("connect to drop scratch database: %v", err)
			return
		}
		defer c.Close(ctx)
		if _, err := c.Exec(ctx, "DROP DATABASE IF EXISTS "+name+" WITH (FORCE)"); err != nil {
			t.Errorf("drop scratch database: %v", err)
		}
	})
	u, err := url.Parse(dsn)
	if err != nil {
		t.Fatalf("parse DSN: %v", err)
	}
	u.Path = "/" + name
	if err := persistence.Migrate(u.String()); err != nil {
		t.Fatalf("migrate scratch database: %v", err)
	}
	pool, err := pgxpool.New(ctx, u.String())
	if err != nil {
		t.Fatalf("pgxpool.New() error: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}
