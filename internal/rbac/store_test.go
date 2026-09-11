package rbac

import (
	"context"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

// testStore builds a Store against a real Postgres instance from
// CONSOLE_TEST_POSTGRES_DSN, skipping if it isn't set - mirrors
// internal/classifier's testStore pattern.
func testStore(t *testing.T) *Store {
	t.Helper()

	dsn := os.Getenv("CONSOLE_TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("CONSOLE_TEST_POSTGRES_DSN not set; skipping integration test")
	}

	pool, err := pgxpool.New(context.Background(), dsn)
	if err != nil {
		t.Fatalf("pgxpool.New() error: %v", err)
	}
	t.Cleanup(pool.Close)

	return NewStore(pool)
}
