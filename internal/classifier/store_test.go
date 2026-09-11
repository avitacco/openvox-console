package classifier

import (
	"context"
	"os"
	"reflect"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

// testStore builds a Store against a real Postgres instance from
// CONSOLE_TEST_POSTGRES_DSN, skipping if it isn't set - mirrors
// internal/persistence's testDSN pattern. Each test cleans up its own
// rows so runs don't interfere with each other.
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

func cleanupGroup(t *testing.T, s *Store, id int64) {
	t.Helper()
	t.Cleanup(func() {
		_ = s.DeleteGroup(context.Background(), id)
	})
}

func TestStore_CreateAndGetGroup(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	env := "production"
	created, err := s.CreateGroup(ctx, Group{
		Name:        "classifier-store-test-create",
		Environment: &env,
		Priority:    100001,
		Classes: []Class{
			{Name: "ntp", Parameters: map[string]any{"server": "0.pool.ntp.org"}},
			{Name: "common"},
		},
		Parameters: map[string]any{"role": "web"},
		Rule:       []Condition{{FactPath: "os.family", Operator: "=", Value: "Debian"}},
		Pins:       []string{"web01"},
	})
	if err != nil {
		t.Fatalf("CreateGroup() error: %v", err)
	}
	cleanupGroup(t, s, created.ID)

	if created.ID == 0 {
		t.Fatal("CreateGroup() did not assign an ID")
	}

	got, err := s.GetGroup(ctx, created.ID)
	if err != nil {
		t.Fatalf("GetGroup() error: %v", err)
	}

	if got.Name != "classifier-store-test-create" {
		t.Errorf("Name = %q", got.Name)
	}
	if got.Environment == nil || *got.Environment != "production" {
		t.Errorf("Environment = %v", got.Environment)
	}
	if got.Priority != 100001 {
		t.Errorf("Priority = %d", got.Priority)
	}
	if len(got.Classes) != 2 {
		t.Fatalf("Classes = %+v, want 2", got.Classes)
	}
	var ntp *Class
	for i := range got.Classes {
		if got.Classes[i].Name == "ntp" {
			ntp = &got.Classes[i]
		}
	}
	if ntp == nil || ntp.Parameters["server"] != "0.pool.ntp.org" {
		t.Errorf("ntp class = %+v", ntp)
	}
	if !reflect.DeepEqual(got.Parameters, map[string]any{"role": "web"}) {
		t.Errorf("Parameters = %v", got.Parameters)
	}
	if len(got.Rule) != 1 || got.Rule[0].FactPath != "os.family" {
		t.Errorf("Rule = %+v", got.Rule)
	}
	if len(got.Pins) != 1 || got.Pins[0] != "web01" {
		t.Errorf("Pins = %+v", got.Pins)
	}
}

func TestStore_DuplicatePriorityRejected(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	first, err := s.CreateGroup(ctx, Group{Name: "classifier-store-test-dup-a", Priority: 100002})
	if err != nil {
		t.Fatalf("CreateGroup() error: %v", err)
	}
	cleanupGroup(t, s, first.ID)

	_, err = s.CreateGroup(ctx, Group{Name: "classifier-store-test-dup-b", Priority: 100002})
	if err != ErrDuplicatePriority {
		t.Errorf("CreateGroup() with duplicate priority error = %v, want ErrDuplicatePriority", err)
	}
}

func TestStore_UpdateGroupReflectsOnRead(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	created, err := s.CreateGroup(ctx, Group{
		Name:     "classifier-store-test-update",
		Priority: 100003,
		Classes:  []Class{{Name: "ntp"}},
	})
	if err != nil {
		t.Fatalf("CreateGroup() error: %v", err)
	}
	cleanupGroup(t, s, created.ID)

	created.Classes = []Class{{Name: "nginx"}, {Name: "postgres"}}
	if err := s.UpdateGroup(ctx, created); err != nil {
		t.Fatalf("UpdateGroup() error: %v", err)
	}

	got, err := s.GetGroup(ctx, created.ID)
	if err != nil {
		t.Fatalf("GetGroup() error: %v", err)
	}
	if len(got.Classes) != 2 {
		t.Fatalf("Classes = %+v, want 2 (nginx, postgres)", got.Classes)
	}
}

func TestStore_ListGroups(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	a, err := s.CreateGroup(ctx, Group{Name: "classifier-store-test-list-a", Priority: 100004})
	if err != nil {
		t.Fatalf("CreateGroup() error: %v", err)
	}
	cleanupGroup(t, s, a.ID)

	b, err := s.CreateGroup(ctx, Group{Name: "classifier-store-test-list-b", Priority: 100005})
	if err != nil {
		t.Fatalf("CreateGroup() error: %v", err)
	}
	cleanupGroup(t, s, b.ID)

	groups, total, err := s.ListGroups(ctx, 1, 1000)
	if err != nil {
		t.Fatalf("ListGroups() error: %v", err)
	}
	if total != len(groups) {
		t.Errorf("total = %d, want %d (page large enough to hold everything)", total, len(groups))
	}

	var foundA, foundB bool
	for _, g := range groups {
		if g.ID == a.ID {
			foundA = true
		}
		if g.ID == b.ID {
			foundB = true
		}
	}
	if !foundA || !foundB {
		t.Errorf("ListGroups() missing created groups; got %d groups", len(groups))
	}
}

func TestStore_ListGroups_Pagination(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	a, err := s.CreateGroup(ctx, Group{Name: "classifier-store-test-page-a", Priority: 100007})
	if err != nil {
		t.Fatalf("CreateGroup() error: %v", err)
	}
	cleanupGroup(t, s, a.ID)

	b, err := s.CreateGroup(ctx, Group{Name: "classifier-store-test-page-b", Priority: 100008})
	if err != nil {
		t.Fatalf("CreateGroup() error: %v", err)
	}
	cleanupGroup(t, s, b.ID)

	firstPage, total, err := s.ListGroups(ctx, 1, 1)
	if err != nil {
		t.Fatalf("ListGroups(page 1) error: %v", err)
	}
	if len(firstPage) != 1 {
		t.Fatalf("len(firstPage) = %d, want 1", len(firstPage))
	}
	if total < 2 {
		t.Errorf("total = %d, want at least 2", total)
	}

	secondPage, _, err := s.ListGroups(ctx, 2, 1)
	if err != nil {
		t.Fatalf("ListGroups(page 2) error: %v", err)
	}
	if len(secondPage) != 1 {
		t.Fatalf("len(secondPage) = %d, want 1", len(secondPage))
	}
	if firstPage[0].ID == secondPage[0].ID {
		t.Error("page 1 and page 2 returned the same group")
	}
}

func TestStore_ListAllGroups_ReturnsEverythingUnpaginated(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	a, err := s.CreateGroup(ctx, Group{Name: "classifier-store-test-listall-a", Priority: 100009})
	if err != nil {
		t.Fatalf("CreateGroup() error: %v", err)
	}
	cleanupGroup(t, s, a.ID)

	groups, err := s.ListAllGroups(ctx)
	if err != nil {
		t.Fatalf("ListAllGroups() error: %v", err)
	}
	var found bool
	for _, g := range groups {
		if g.ID == a.ID {
			found = true
		}
	}
	if !found {
		t.Errorf("ListAllGroups() missing created group; got %d groups", len(groups))
	}
}

func TestStore_DeleteGroup(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	created, err := s.CreateGroup(ctx, Group{Name: "classifier-store-test-delete", Priority: 100006})
	if err != nil {
		t.Fatalf("CreateGroup() error: %v", err)
	}

	if err := s.DeleteGroup(ctx, created.ID); err != nil {
		t.Fatalf("DeleteGroup() error: %v", err)
	}

	_, err = s.GetGroup(ctx, created.ID)
	if err != ErrNotFound {
		t.Errorf("GetGroup() after delete error = %v, want ErrNotFound", err)
	}
}

func TestStore_GetGroup_NotFound(t *testing.T) {
	s := testStore(t)
	_, err := s.GetGroup(context.Background(), -1)
	if err != ErrNotFound {
		t.Errorf("GetGroup(-1) error = %v, want ErrNotFound", err)
	}
}
