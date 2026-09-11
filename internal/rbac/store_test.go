package rbac

import (
	"testing"

	"github.com/voxpupuli/enterprise-console/internal/testdb"
)

// testStore builds a Store against a real Postgres instance from
// CONSOLE_TEST_POSTGRES_DSN, skipping if it isn't set - mirrors
// internal/classifier's testStore pattern.
func testStore(t *testing.T) *Store {
	t.Helper()

	pool := testdb.Pool(t)

	return NewStore(pool)
}
