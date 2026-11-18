package classifier

import (
	"context"
	"errors"
	"sync"
	"testing"
)

// mustCreate creates g and deletes it when the test ends. Cleanups run
// last-created first, so a child is always removed before its parent.
func mustCreate(t *testing.T, s *Store, g Group) Group {
	t.Helper()
	created, err := s.CreateGroup(context.Background(), g)
	if err != nil {
		t.Fatalf("CreateGroup(%q) error: %v", g.Name, err)
	}
	cleanupGroup(t, s, created.ID)
	return created
}

func storeRoot(t *testing.T, s *Store) Group {
	t.Helper()
	groups, err := s.ListAllGroups(context.Background())
	if err != nil {
		t.Fatalf("ListAllGroups() error: %v", err)
	}
	if len(groups) == 0 || !groups[0].IsRoot {
		t.Fatalf("ListAllGroups() should list the root first; got %+v", groups)
	}
	roots := 0
	for _, g := range groups {
		if g.IsRoot {
			roots++
		}
	}
	if roots != 1 {
		t.Fatalf("found %d root groups, want exactly 1", roots)
	}
	return groups[0]
}

func TestStore_RootExistsWithNoParent(t *testing.T) {
	root := storeRoot(t, testStore(t))
	if root.Name != "All Nodes" || root.ParentID != nil {
		t.Errorf("root = %+v, want All Nodes with no parent", root)
	}
}

func TestStore_GroupCreatedWithoutParentIsChildOfRoot(t *testing.T) {
	s := testStore(t)
	root := storeRoot(t, s)

	created := mustCreate(t, s, Group{Name: "hierarchy-test-default-parent", Priority: 110001})
	if created.ParentID == nil || *created.ParentID != root.ID {
		t.Errorf("created.ParentID = %v, want the root (%d)", created.ParentID, root.ID)
	}
	if created.RuleMatch != RuleMatchAll {
		t.Errorf("created.RuleMatch = %q, want all by default", created.RuleMatch)
	}

	got, err := s.GetGroup(context.Background(), created.ID)
	if err != nil {
		t.Fatalf("GetGroup() error: %v", err)
	}
	if got.ParentID == nil || *got.ParentID != root.ID || got.IsRoot || got.RuleMatch != RuleMatchAll {
		t.Errorf("stored group = %+v, want a non-root child of the root matching all", got)
	}
}

func TestStore_ParentAndRuleMatchRoundTrip(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	root := storeRoot(t, s)

	parent := mustCreate(t, s, Group{Name: "hierarchy-test-rt-parent", Priority: 110002})
	other := mustCreate(t, s, Group{Name: "hierarchy-test-rt-other", Priority: 110003})
	child := mustCreate(t, s, Group{Name: "hierarchy-test-rt-child", Priority: 110004, ParentID: &parent.ID, RuleMatch: RuleMatchAny})

	got, err := s.GetGroup(ctx, child.ID)
	if err != nil {
		t.Fatalf("GetGroup() error: %v", err)
	}
	if got.ParentID == nil || *got.ParentID != parent.ID || got.RuleMatch != RuleMatchAny {
		t.Errorf("after create = %+v, want parent %d and mode any", got, parent.ID)
	}

	// Move it, and change the mode.
	got.ParentID = &other.ID
	got.RuleMatch = RuleMatchAll
	if err := s.UpdateGroup(ctx, got); err != nil {
		t.Fatalf("UpdateGroup() error: %v", err)
	}
	moved, _ := s.GetGroup(ctx, child.ID)
	if moved.ParentID == nil || *moved.ParentID != other.ID || moved.RuleMatch != RuleMatchAll {
		t.Errorf("after move = %+v, want parent %d and mode all", moved, other.ID)
	}

	// Omitting the parent puts it back under the root.
	moved.ParentID = nil
	if err := s.UpdateGroup(ctx, moved); err != nil {
		t.Fatalf("UpdateGroup() error: %v", err)
	}
	back, _ := s.GetGroup(ctx, child.ID)
	if back.ParentID == nil || *back.ParentID != root.ID {
		t.Errorf("after clearing parent = %+v, want the root as parent", back)
	}
}

func TestStore_MissingParentRejected(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	missing := int64(-1)

	if _, err := s.CreateGroup(ctx, Group{Name: "hierarchy-test-missing-parent", Priority: 110005, ParentID: &missing}); !errors.Is(err, ErrParentNotFound) {
		t.Errorf("CreateGroup() error = %v, want ErrParentNotFound", err)
	}

	g := mustCreate(t, s, Group{Name: "hierarchy-test-missing-parent-update", Priority: 110006})
	g.ParentID = &missing
	if err := s.UpdateGroup(ctx, g); !errors.Is(err, ErrParentNotFound) {
		t.Errorf("UpdateGroup() error = %v, want ErrParentNotFound", err)
	}
}

func TestStore_InvalidRuleMatchRejected(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	if _, err := s.CreateGroup(ctx, Group{Name: "hierarchy-test-bad-mode", Priority: 110007, RuleMatch: "some"}); !errors.Is(err, ErrInvalidRuleMatch) {
		t.Errorf("CreateGroup() error = %v, want ErrInvalidRuleMatch", err)
	}

	g := mustCreate(t, s, Group{Name: "hierarchy-test-bad-mode-update", Priority: 110008})
	g.RuleMatch = "some"
	if err := s.UpdateGroup(ctx, g); !errors.Is(err, ErrInvalidRuleMatch) {
		t.Errorf("UpdateGroup() error = %v, want ErrInvalidRuleMatch", err)
	}
}

func TestStore_MoveUnderSelfOrDescendantRejected(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	top := mustCreate(t, s, Group{Name: "hierarchy-test-loop-top", Priority: 110009})
	middle := mustCreate(t, s, Group{Name: "hierarchy-test-loop-middle", Priority: 110010, ParentID: &top.ID})
	leaf := mustCreate(t, s, Group{Name: "hierarchy-test-loop-leaf", Priority: 110011, ParentID: &middle.ID})

	self := top
	self.ParentID = &top.ID
	if err := s.UpdateGroup(ctx, self); !errors.Is(err, ErrHierarchyLoop) {
		t.Errorf("moving a group under itself: error = %v, want ErrHierarchyLoop", err)
	}

	for _, descendant := range []Group{middle, leaf} {
		moved := top
		moved.ParentID = &descendant.ID
		if err := s.UpdateGroup(ctx, moved); !errors.Is(err, ErrHierarchyLoop) {
			t.Errorf("moving a group under descendant %q: error = %v, want ErrHierarchyLoop", descendant.Name, err)
		}
	}

	// Nothing moved.
	got, _ := s.GetGroup(ctx, top.ID)
	if got.ParentID == nil || *got.ParentID != *top.ParentID {
		t.Errorf("top's parent changed to %v after refused moves", got.ParentID)
	}
}

func TestStore_DeleteGroupWithChildrenRejected(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	parent := mustCreate(t, s, Group{Name: "hierarchy-test-delete-parent", Priority: 110012})
	child := mustCreate(t, s, Group{Name: "hierarchy-test-delete-child", Priority: 110013, ParentID: &parent.ID})

	if err := s.DeleteGroup(ctx, parent.ID); !errors.Is(err, ErrHasChildren) {
		t.Fatalf("DeleteGroup(parent) error = %v, want ErrHasChildren", err)
	}
	if _, err := s.GetGroup(ctx, parent.ID); err != nil {
		t.Errorf("parent should still exist: %v", err)
	}
	if _, err := s.GetGroup(ctx, child.ID); err != nil {
		t.Errorf("child should still exist: %v", err)
	}

	// Once the child is gone the parent can be deleted.
	if err := s.DeleteGroup(ctx, child.ID); err != nil {
		t.Fatalf("DeleteGroup(child) error: %v", err)
	}
	if err := s.DeleteGroup(ctx, parent.ID); err != nil {
		t.Errorf("DeleteGroup(parent) after its child was removed: %v", err)
	}
}

func TestStore_RootIsProtected(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	root := storeRoot(t, s)

	if err := s.DeleteGroup(ctx, root.ID); !errors.Is(err, ErrRootImmutable) {
		t.Errorf("DeleteGroup(root) error = %v, want ErrRootImmutable", err)
	}

	other := mustCreate(t, s, Group{Name: "hierarchy-test-root-other", Priority: 110014})
	changes := map[string]func(*Group){
		"rename": func(g *Group) { g.Name = "Everything" },
		"parent": func(g *Group) { g.ParentID = &other.ID },
		"rule":   func(g *Group) { g.Rule = []Condition{{FactPath: "kernel", Operator: "=", Value: "Linux"}} },
		"pins":   func(g *Group) { g.Pins = []string{"web01"} },
	}
	for name, change := range changes {
		g := root
		change(&g)
		if err := s.UpdateGroup(ctx, g); !errors.Is(err, ErrRootImmutable) {
			t.Errorf("%s: UpdateGroup(root) error = %v, want ErrRootImmutable", name, err)
		}
	}
	if got := storeRoot(t, s); got.Name != root.Name || got.ParentID != nil || len(got.Rule) != 0 || len(got.Pins) != 0 {
		t.Errorf("root = %+v, want it unchanged after refused updates", got)
	}
}

func TestStore_RootClassesParametersAndEnvironmentAreEditable(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	root := storeRoot(t, s)
	t.Cleanup(func() { _ = s.UpdateGroup(ctx, root) })

	env := "hierarchy-test-env"
	edited := root
	edited.Environment = &env
	edited.Classes = []Class{{Name: "hierarchy_test_base", Parameters: map[string]any{"enabled": true}}}
	edited.Parameters = map[string]any{"hierarchy_test": "yes"}
	if err := s.UpdateGroup(ctx, edited); err != nil {
		t.Fatalf("UpdateGroup(root) error: %v", err)
	}

	got := storeRoot(t, s)
	if got.Environment == nil || *got.Environment != env {
		t.Errorf("root Environment = %v, want %q", got.Environment, env)
	}
	if len(got.Classes) != 1 || got.Classes[0].Name != "hierarchy_test_base" {
		t.Errorf("root Classes = %+v", got.Classes)
	}
	if got.Parameters["hierarchy_test"] != "yes" {
		t.Errorf("root Parameters = %+v", got.Parameters)
	}
	if !got.IsRoot || got.Name != "All Nodes" {
		t.Errorf("root = %+v, want it still the root", got)
	}
}

// Two moves that are each fine alone but together form a loop - a under
// b, and b under a - must not both succeed, however they interleave.
func TestStore_ConcurrentMovesCannotFormALoop(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	a := mustCreate(t, s, Group{Name: "hierarchy-test-race-a", Priority: 110015})
	b := mustCreate(t, s, Group{Name: "hierarchy-test-race-b", Priority: 110016})

	for round := 0; round < 20; round++ {
		moveA, moveB := a, b
		moveA.ParentID = &b.ID
		moveB.ParentID = &a.ID

		var wg sync.WaitGroup
		errs := make([]error, 2)
		wg.Add(2)
		go func() { defer wg.Done(); errs[0] = s.UpdateGroup(ctx, moveA) }()
		go func() { defer wg.Done(); errs[1] = s.UpdateGroup(ctx, moveB) }()
		wg.Wait()

		succeeded := 0
		for _, err := range errs {
			switch {
			case err == nil:
				succeeded++
			case !errors.Is(err, ErrHierarchyLoop):
				t.Fatalf("round %d: unexpected error: %v", round, err)
			}
		}
		if succeeded != 1 {
			t.Fatalf("round %d: %d moves succeeded, want exactly 1", round, succeeded)
		}

		// Back to siblings for the next round.
		if err := s.UpdateGroup(ctx, a); err != nil {
			t.Fatalf("reset a: %v", err)
		}
		if err := s.UpdateGroup(ctx, b); err != nil {
			t.Fatalf("reset b: %v", err)
		}
	}
}

// A group a role is granted on carries someone's access to its nodes;
// deleting it would silently take that away.
func TestStore_DeleteGrantedGroupRejectedUntilTheGrantIsRemoved(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	group := mustCreate(t, s, Group{Name: "hierarchy-test-delete-granted", Priority: 110090})

	// The classifier does not import rbac; the grant is written the way
	// rbac's store writes it.
	var roleID int64
	if err := s.pool.QueryRow(ctx, `INSERT INTO roles (name) VALUES ('hierarchy-test-granted-role') RETURNING id`).Scan(&roleID); err != nil {
		t.Fatalf("create role: %v", err)
	}
	t.Cleanup(func() { _, _ = s.pool.Exec(context.Background(), `DELETE FROM roles WHERE id = $1`, roleID) })
	if _, err := s.pool.Exec(ctx, `INSERT INTO role_node_groups (role_id, group_id) VALUES ($1, $2)`, roleID, group.ID); err != nil {
		t.Fatalf("grant group: %v", err)
	}

	if err := s.DeleteGroup(ctx, group.ID); !errors.Is(err, ErrGroupGranted) {
		t.Fatalf("DeleteGroup(granted) error = %v, want ErrGroupGranted", err)
	}
	if _, err := s.GetGroup(ctx, group.ID); err != nil {
		t.Errorf("group should still exist: %v", err)
	}

	if _, err := s.pool.Exec(ctx, `DELETE FROM role_node_groups WHERE group_id = $1`, group.ID); err != nil {
		t.Fatalf("remove grant: %v", err)
	}
	if err := s.DeleteGroup(ctx, group.ID); err != nil {
		t.Errorf("DeleteGroup() after removing the grant: %v", err)
	}
}
