package persistence

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5"
)

type hierarchyGroup struct {
	name     string
	isRoot   bool
	parent   *string
	priority *int
	mode     string
}

func hierarchyGroups(t *testing.T, dsn string) map[string]hierarchyGroup {
	t.Helper()
	ctx := context.Background()
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer conn.Close(ctx)

	rows, err := conn.Query(ctx, `
		SELECT g.name, g.is_root, p.name, g.priority, g.rule_match
		  FROM node_groups g LEFT JOIN node_groups p ON p.id = g.parent_id`)
	if err != nil {
		t.Fatalf("query groups: %v", err)
	}
	defer rows.Close()

	groups := map[string]hierarchyGroup{}
	for rows.Next() {
		var g hierarchyGroup
		if err := rows.Scan(&g.name, &g.isRoot, &g.parent, &g.priority, &g.mode); err != nil {
			t.Fatalf("scan group: %v", err)
		}
		groups[g.name] = g
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("read groups: %v", err)
	}
	return groups
}

func execSQL(t *testing.T, dsn string, statements ...string) {
	t.Helper()
	ctx := context.Background()
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer conn.Close(ctx)
	for _, stmt := range statements {
		if _, err := conn.Exec(ctx, stmt); err != nil {
			t.Fatalf("exec %q: %v", stmt, err)
		}
	}
}

// assertOneRootEverythingElseBeneathIt is what every starting state must
// end as: exactly one root, called All Nodes, with no parent and no
// priority, and every other group a direct child of it matching "all".
func assertOneRootEverythingElseBeneathIt(t *testing.T, groups map[string]hierarchyGroup, wantOthers ...string) {
	t.Helper()

	roots := 0
	for _, g := range groups {
		if g.isRoot {
			roots++
		}
	}
	if roots != 1 {
		t.Fatalf("%d root groups, want exactly 1: %+v", roots, groups)
	}
	root, ok := groups["All Nodes"]
	if !ok || !root.isRoot || root.parent != nil || root.priority != nil {
		t.Fatalf(`"All Nodes" = %+v, want the root with no parent and no priority`, root)
	}

	if len(groups) != len(wantOthers)+1 {
		t.Errorf("%d groups, want %d plus the root: %+v", len(groups), len(wantOthers), groups)
	}
	for _, name := range wantOthers {
		g, ok := groups[name]
		if !ok {
			t.Errorf("group %q missing after migration: %+v", name, groups)
			continue
		}
		if g.isRoot || g.parent == nil || *g.parent != "All Nodes" {
			t.Errorf("group %q = %+v, want a direct child of the root", name, g)
		}
		if g.priority == nil {
			t.Errorf("group %q lost its priority", name)
		}
		if g.mode != "all" {
			t.Errorf("group %q rule_match = %q, want all", name, g.mode)
		}
	}
}

func TestMigrate_GroupHierarchy_NoGroups(t *testing.T) {
	dsn := scratchDatabase(t, testDSN(t))
	migrateTo(t, dsn, 29)
	assertOneRootEverythingElseBeneathIt(t, hierarchyGroups(t, dsn))
}

func TestMigrate_GroupHierarchy_ExistingGroupsBecomeChildrenOfRoot(t *testing.T) {
	dsn := scratchDatabase(t, testDSN(t))
	migrateTo(t, dsn, 28)
	execSQL(t, dsn,
		`INSERT INTO node_groups (name, priority) VALUES ('web', 10), ('db', 20)`,
	)
	migrateTo(t, dsn, 29)
	assertOneRootEverythingElseBeneathIt(t, hierarchyGroups(t, dsn), "web", "db")
}

// An existing "All Nodes" is an ordinary group and must stay one - its
// rule, pins and priority intact - rather than become the root, which
// would apply its classes to nodes it never matched.
func TestMigrate_GroupHierarchy_ExistingAllNodesIsRenamedNotAdopted(t *testing.T) {
	dsn := scratchDatabase(t, testDSN(t))
	migrateTo(t, dsn, 28)
	execSQL(t, dsn,
		`INSERT INTO node_groups (name, priority) VALUES ('All Nodes', 10), ('web', 20)`,
		`INSERT INTO node_group_rule_conditions (group_id, fact_path, operator, value)
		 SELECT id, 'kernel', '=', 'Linux' FROM node_groups WHERE name = 'All Nodes'`,
	)
	migrateTo(t, dsn, 29)

	groups := hierarchyGroups(t, dsn)
	assertOneRootEverythingElseBeneathIt(t, groups, "All Nodes (previous)", "web")
	if p := groups["All Nodes (previous)"].priority; p == nil || *p != 10 {
		t.Errorf("renamed group priority = %v, want its original 10", p)
	}

	ctx := context.Background()
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer conn.Close(ctx)
	var rootConditions, renamedConditions int
	if err := conn.QueryRow(ctx, `
		SELECT count(*) FILTER (WHERE g.is_root), count(*) FILTER (WHERE g.name = 'All Nodes (previous)')
		  FROM node_group_rule_conditions c JOIN node_groups g ON g.id = c.group_id`,
	).Scan(&rootConditions, &renamedConditions); err != nil {
		t.Fatalf("count conditions: %v", err)
	}
	if rootConditions != 0 || renamedConditions != 1 {
		t.Errorf("rule conditions: root has %d, renamed group has %d; want 0 and 1", rootConditions, renamedConditions)
	}
}

func TestMigrate_GroupHierarchy_PreviousNameAlreadyTaken(t *testing.T) {
	dsn := scratchDatabase(t, testDSN(t))
	migrateTo(t, dsn, 28)
	execSQL(t, dsn,
		`INSERT INTO node_groups (name, priority) VALUES ('All Nodes', 10), ('All Nodes (previous)', 20)`,
	)
	migrateTo(t, dsn, 29)

	groups := hierarchyGroups(t, dsn)
	if len(groups) != 3 {
		t.Fatalf("%d groups, want 3: %+v", len(groups), groups)
	}
	if !groups["All Nodes"].isRoot {
		t.Errorf(`"All Nodes" should be the new root: %+v`, groups)
	}
	if _, ok := groups["All Nodes (previous)"]; !ok {
		t.Errorf("the group already named \"All Nodes (previous)\" should keep its name: %+v", groups)
	}
}

func TestMigrate_GroupHierarchy_UpDownUp(t *testing.T) {
	dsn := scratchDatabase(t, testDSN(t))
	migrateTo(t, dsn, 28)
	execSQL(t, dsn, `INSERT INTO node_groups (name, priority) VALUES ('web', 10)`)

	assertColumns := func(step string, want bool) {
		t.Helper()
		for _, column := range []string{"parent_id", "is_root", "rule_match"} {
			if got := columnExists(t, dsn, "node_groups", column); got != want {
				t.Errorf("%s: node_groups.%s present = %v, want %v", step, column, got, want)
			}
		}
	}

	migrateTo(t, dsn, 29)
	assertColumns("up", true)

	// Nest a group, so the down migration has a real tree to flatten.
	execSQL(t, dsn, `
		INSERT INTO node_groups (name, priority, parent_id)
		SELECT 'web-eu', 20, id FROM node_groups WHERE name = 'web'`)

	migrateTo(t, dsn, 28)
	assertColumns("down", false)

	ctx := context.Background()
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	var remaining int
	if err := conn.QueryRow(ctx, `SELECT count(*) FROM node_groups`).Scan(&remaining); err != nil {
		t.Fatalf("count groups: %v", err)
	}
	conn.Close(ctx)
	if remaining != 2 {
		t.Errorf("%d groups after down, want the 2 non-root groups", remaining)
	}

	migrateTo(t, dsn, 29)
	assertColumns("up again", true)
	assertOneRootEverythingElseBeneathIt(t, hierarchyGroups(t, dsn), "web", "web-eu")
}
