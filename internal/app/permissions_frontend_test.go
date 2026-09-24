package app

import (
	"os"
	"regexp"
	"slices"
	"strings"
	"testing"
)

// The roles and service-tokens pages render a checkbox per permission
// from a hand-maintained list in frontend/src/app.js, and the roles page
// then PUTs the checkboxes it rendered as the role's *complete*
// permission set.
//
// That makes drift between the two lists a data-loss bug rather than a
// cosmetic one: a permission missing from the frontend list is invisible
// in the UI, and is silently stripped from any role that holds it the
// moment somebody toggles any other checkbox on that role. It had
// already happened once - `nodes:manage` was missing from the frontend
// while two roles held it - which is what this test exists to prevent
// recurring.
func TestAllPermissionsMatchesTheFrontendList(t *testing.T) {
	source, err := os.ReadFile("../../frontend/src/app.js")
	if err != nil {
		t.Fatalf("read the frontend permission list: %v", err)
	}

	frontend := parseFrontendPermissions(t, string(source))

	for _, p := range allPermissions {
		if !slices.Contains(frontend, p) {
			t.Errorf("permission %q is missing from frontend/src/app.js ALL_PERMISSIONS; "+
				"it cannot be granted in the UI, and editing any role that holds it will silently strip it", p)
		}
	}
	for _, p := range frontend {
		if !slices.Contains(allPermissions, p) {
			t.Errorf("frontend/src/app.js ALL_PERMISSIONS offers %q, which the backend does not define", p)
		}
	}
}

// parseFrontendPermissions pulls the string literals out of the
// ALL_PERMISSIONS array. Deliberately a small parse of one known
// declaration rather than anything general: the alternative is
// duplicating the list a third time in this test.
func parseFrontendPermissions(t *testing.T, source string) []string {
	t.Helper()

	const marker = "export const ALL_PERMISSIONS = ["
	start := strings.Index(source, marker)
	if start < 0 {
		t.Fatal("could not find ALL_PERMISSIONS in frontend/src/app.js")
	}
	rest := source[start+len(marker):]
	end := strings.Index(rest, "]")
	if end < 0 {
		t.Fatal("ALL_PERMISSIONS in frontend/src/app.js is not terminated")
	}

	var out []string
	for _, m := range regexp.MustCompile(`'([^']+)'`).FindAllStringSubmatch(rest[:end], -1) {
		out = append(out, m[1])
	}
	if len(out) == 0 {
		t.Fatal("ALL_PERMISSIONS in frontend/src/app.js parsed as empty")
	}
	return out
}
