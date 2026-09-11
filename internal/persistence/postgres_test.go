package persistence

import (
	"context"
	"os"
	"testing"
	"time"
)

func testDSN(t *testing.T) string {
	t.Helper()
	dsn := os.Getenv("CONSOLE_TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("CONSOLE_TEST_POSTGRES_DSN not set; skipping integration test")
	}
	return dsn
}

func TestConnect_And_Check_Reachable(t *testing.T) {
	dsn := testDSN(t)

	db, err := Connect(context.Background(), dsn)
	if err != nil {
		t.Fatalf("Connect() error: %v", err)
	}
	defer db.Close()

	if err := db.Check(context.Background()); err != nil {
		t.Errorf("Check() error = %v, want nil (postgres reachable)", err)
	}
}

func TestCheck_Unreachable(t *testing.T) {
	// A DSN pointing at a port nothing is listening on. Connect() does not
	// dial eagerly (the pool is lazy), so it should succeed; Check() is
	// what surfaces unreachability, matching the health check contract.
	db, err := Connect(context.Background(), "postgres://postgres:postgres@127.0.0.1:1/nonexistent")
	if err != nil {
		t.Fatalf("Connect() error: %v", err)
	}
	defer db.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	if err := db.Check(ctx); err == nil {
		t.Error("Check() error = nil, want an error for an unreachable database")
	}
}

func TestMigrate_AppliesThenNoOpsOnRerun(t *testing.T) {
	dsn := testDSN(t)

	if err := Migrate(dsn); err != nil {
		t.Fatalf("first Migrate() error: %v", err)
	}

	// Re-running against an already-migrated database must be a no-op,
	// not an error.
	if err := Migrate(dsn); err != nil {
		t.Fatalf("second Migrate() error: %v", err)
	}
}
