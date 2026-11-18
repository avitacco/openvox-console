package rbac

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestNothingReadsUserRolesDirectly fails when production code asks
// "what roles does this user hold" of user_roles, the table of direct
// assignments, instead of the user_effective_roles view. Reading the
// table would silently ignore a user's groups, so that a role granted
// through a group would show on the Users page but not in a token (or
// the reverse). Only the code that writes direct assignments, and the
// one read that reports which roles are direct, may name the table.
func TestNothingReadsUserRolesDirectly(t *testing.T) {
	allowed := []string{
		"INSERT INTO user_roles", // AssignRole, ReconcileOIDCRoles
		"DELETE FROM user_roles WHERE user_id = $1 AND role_id = $2", // UnassignRole
		"DELETE FROM user_roles",             // ReconcileOIDCRoles (its own rows)
		"FROM user_roles WHERE user_id = $1", // ListUserRoles' direct half
	}
	root := filepath.Join("..", "..", "internal")
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return err
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for i, line := range strings.Split(string(b), "\n") {
			if !strings.Contains(line, "user_roles") {
				continue
			}
			if strings.HasPrefix(strings.TrimSpace(line), "//") || strings.HasPrefix(strings.TrimSpace(line), "--") {
				continue
			}
			ok := false
			for _, a := range allowed {
				if strings.Contains(line, a) {
					ok = true
				}
			}
			if !ok && strings.Contains(line, "user_roles") && !strings.Contains(line, "user_effective_roles") {
				t.Errorf("%s:%d reads user_roles directly; use user_effective_roles: %s", path, i+1, strings.TrimSpace(line))
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
