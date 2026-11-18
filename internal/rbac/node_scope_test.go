package rbac

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// testNodeGroup creates a node group directly under the root and
// returns its id. rbac does not import the classifier, so the row is
// written directly; the priority is derived from the clock to stay
// clear of the unique constraint across test runs.
func testNodeGroup(t *testing.T, s *Store, name string) int64 {
	t.Helper()
	ctx := context.Background()
	var id int64
	err := s.pool.QueryRow(ctx, `
		INSERT INTO node_groups (name, priority, parent_id)
		SELECT $1, $2, id FROM node_groups WHERE is_root
		RETURNING id`, name, 1_000_000+int(time.Now().UnixNano()%1_000_000_000)).Scan(&id)
	if err != nil {
		t.Fatalf("create node group %q: %v", name, err)
	}
	t.Cleanup(func() {
		_, _ = s.pool.Exec(context.Background(), `DELETE FROM role_node_groups WHERE group_id = $1`, id)
		_, _ = s.pool.Exec(context.Background(), `DELETE FROM node_groups WHERE id = $1`, id)
	})
	return id
}

func rootGroupID(t *testing.T, s *Store) int64 {
	t.Helper()
	var id int64
	if err := s.pool.QueryRow(context.Background(), `SELECT id FROM node_groups WHERE is_root`).Scan(&id); err != nil {
		t.Fatalf("find root group: %v", err)
	}
	return id
}

func scopedRole(t *testing.T, s *Store, name string, perms []string, groups []int64) Role {
	t.Helper()
	r, err := s.CreateScopedRole(context.Background(), name, perms, groups)
	if err != nil {
		t.Fatalf("CreateScopedRole(%q) error: %v", name, err)
	}
	t.Cleanup(func() { _ = s.DeleteRole(context.Background(), r.ID) })
	return r
}

func TestCreateRole_WithoutGroupsIsGrantedTheRoot(t *testing.T) {
	s := testStore(t)
	r, err := s.CreateRole(context.Background(), "scope-test-default", []string{"nodes:read"})
	if err != nil {
		t.Fatalf("CreateRole() error: %v", err)
	}
	t.Cleanup(func() { _ = s.DeleteRole(context.Background(), r.ID) })

	got, err := s.GetRole(context.Background(), r.ID)
	if err != nil {
		t.Fatalf("GetRole() error: %v", err)
	}
	if want := []int64{rootGroupID(t, s)}; !reflect.DeepEqual(got.NodeGroups, want) {
		t.Errorf("NodeGroups = %v, want the root group %v", got.NodeGroups, want)
	}
}

func TestCreateScopedRole_RoundTripsGroups(t *testing.T) {
	s := testStore(t)
	web := testNodeGroup(t, s, "scope-test-web")
	db := testNodeGroup(t, s, "scope-test-db")

	r := scopedRole(t, s, "scope-test-two-groups", []string{"nodes:read"}, []int64{web, db, web})
	got, err := s.GetRole(context.Background(), r.ID)
	if err != nil {
		t.Fatalf("GetRole() error: %v", err)
	}
	if len(got.NodeGroups) != 2 {
		t.Errorf("NodeGroups = %v, want the two groups once each", got.NodeGroups)
	}

	listed, err := s.ListRoles(context.Background())
	if err != nil {
		t.Fatalf("ListRoles() error: %v", err)
	}
	for _, role := range listed {
		if role.ID == r.ID && len(role.NodeGroups) != 2 {
			t.Errorf("listed NodeGroups = %v, want 2", role.NodeGroups)
		}
	}
}

func TestCreateScopedRole_EmptyListIsStoredAsNoGroups(t *testing.T) {
	s := testStore(t)
	r := scopedRole(t, s, "scope-test-none", []string{"nodes:read"}, []int64{})
	got, err := s.GetRole(context.Background(), r.ID)
	if err != nil {
		t.Fatalf("GetRole() error: %v", err)
	}
	if got.NodeGroups == nil || len(got.NodeGroups) != 0 {
		t.Errorf("NodeGroups = %#v, want an empty list", got.NodeGroups)
	}
}

func TestCreateScopedRole_UnknownGroupIsRejectedAndNothingIsCreated(t *testing.T) {
	s := testStore(t)
	_, err := s.CreateScopedRole(context.Background(), "scope-test-unknown", []string{"nodes:read"}, []int64{-42})
	if !errors.Is(err, ErrUnknownNodeGroup) {
		t.Fatalf("error = %v, want ErrUnknownNodeGroup", err)
	}
	if _, err := s.GetRoleByName(context.Background(), "scope-test-unknown"); !errors.Is(err, ErrNotFound) {
		t.Errorf("role exists after a rejected create (err = %v)", err)
	}
}

func TestUpdateRole_NodeGroups(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	web := testNodeGroup(t, s, "scope-test-upd-web")
	db := testNodeGroup(t, s, "scope-test-upd-db")
	r := scopedRole(t, s, "scope-test-update", []string{"nodes:read"}, []int64{web})

	// nil: groups left alone while permissions change.
	if err := s.UpdateRole(ctx, Role{ID: r.ID, Name: r.Name, Permissions: []string{"nodes:read", "code:read"}}); err != nil {
		t.Fatalf("UpdateRole(nil groups) error: %v", err)
	}
	got, _ := s.GetRole(ctx, r.ID)
	if !reflect.DeepEqual(got.NodeGroups, []int64{web}) || len(got.Permissions) != 2 {
		t.Errorf("after nil-groups update: groups %v perms %v, want [%d] and 2 permissions", got.NodeGroups, got.Permissions, web)
	}

	// non-nil: replaced.
	if err := s.UpdateRole(ctx, Role{ID: r.ID, Name: r.Name, Permissions: got.Permissions, NodeGroups: []int64{db}}); err != nil {
		t.Fatalf("UpdateRole(replace) error: %v", err)
	}
	got, _ = s.GetRole(ctx, r.ID)
	if !reflect.DeepEqual(got.NodeGroups, []int64{db}) {
		t.Errorf("after replace: groups %v, want [%d]", got.NodeGroups, db)
	}

	// unknown: rejected, role unchanged.
	err := s.UpdateRole(ctx, Role{ID: r.ID, Name: "scope-test-renamed", Permissions: []string{}, NodeGroups: []int64{-42}})
	if !errors.Is(err, ErrUnknownNodeGroup) {
		t.Fatalf("error = %v, want ErrUnknownNodeGroup", err)
	}
	got, _ = s.GetRole(ctx, r.ID)
	if got.Name != r.Name || !reflect.DeepEqual(got.NodeGroups, []int64{db}) || len(got.Permissions) != 2 {
		t.Errorf("role changed by a rejected update: %+v", got)
	}

	// empty: no groups.
	if err := s.UpdateRole(ctx, Role{ID: r.ID, Name: r.Name, Permissions: got.Permissions, NodeGroups: []int64{}}); err != nil {
		t.Fatalf("UpdateRole(empty) error: %v", err)
	}
	got, _ = s.GetRole(ctx, r.ID)
	if len(got.NodeGroups) != 0 {
		t.Errorf("after empty update: groups %v, want none", got.NodeGroups)
	}
}

func TestUserNodeScopes(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	root := rootGroupID(t, s)
	web := testNodeGroup(t, s, "scope-test-uns-web")
	db := testNodeGroup(t, s, "scope-test-uns-db")

	u, err := s.CreateUser(ctx, "scope-test-user", "password123")
	if err != nil {
		t.Fatalf("CreateUser() error: %v", err)
	}
	t.Cleanup(func() { _ = s.DeleteUser(context.Background(), u.ID) })

	roles := []Role{
		scopedRole(t, s, "scope-test-uns-a", []string{"nodes:read", "classifier:read"}, []int64{web}),
		scopedRole(t, s, "scope-test-uns-b", []string{"nodes:read"}, []int64{db}),
		// Carries a different permission fleet-wide: must not widen nodes:read.
		scopedRole(t, s, "scope-test-uns-c", []string{"vulnerabilities:read"}, []int64{root, web}),
		// A permission held only through a role with no groups.
		scopedRole(t, s, "scope-test-uns-d", []string{"code:read"}, []int64{}),
	}
	for _, r := range roles {
		if err := s.AssignRole(ctx, u.ID, r.ID); err != nil {
			t.Fatalf("AssignRole() error: %v", err)
		}
	}

	got, err := s.UserNodeScopes(ctx, u.ID)
	if err != nil {
		t.Fatalf("UserNodeScopes() error: %v", err)
	}
	want := map[string][]int64{
		"nodes:read":           sortedIDs(web, db),
		"classifier:read":      {web},
		"vulnerabilities:read": {root},
		"code:read":            {},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("UserNodeScopes() = %v, want %v", got, want)
	}

	// A user with no roles still gets a scope that says "nothing",
	// never a nil that a token would turn into fleet-wide.
	lonely, err := s.CreateUser(ctx, "scope-test-user-no-roles", "password123")
	if err != nil {
		t.Fatalf("CreateUser() error: %v", err)
	}
	t.Cleanup(func() { _ = s.DeleteUser(context.Background(), lonely.ID) })
	none, err := s.UserNodeScopes(ctx, lonely.ID)
	if err != nil || none == nil || len(none) != 0 {
		t.Errorf("UserNodeScopes(no roles) = %#v, %v; want an empty non-nil map", none, err)
	}
}

func sortedIDs(a, b int64) []int64 {
	if a < b {
		return []int64{a, b}
	}
	return []int64{b, a}
}

func TestIssueUserToken_AlwaysStatesAScope(t *testing.T) {
	verifier, issuer := testVerifierWithRevoked(t, "")

	for name, scopes := range map[string]map[string][]int64{
		"nil scopes":   nil,
		"empty scopes": {},
		"one scope":    {"nodes:read": {7}},
	} {
		t.Run(name, func(t *testing.T) {
			signed, _, err := issuer.IssueUserToken("alice", TokenTypeAccess, []string{"nodes:read"}, scopes, time.Minute)
			if err != nil {
				t.Fatalf("IssueUserToken() error: %v", err)
			}
			claims, err := verifier.Verify(signed)
			if err != nil {
				t.Fatalf("Verify() error: %v", err)
			}
			if claims.NodeScopes == nil {
				t.Fatal("a user token came back with no scope at all, which reads as fleet-wide")
			}
			groups, fleetWide := claims.NodeGroupsFor("nodes:read")
			if fleetWide {
				t.Error("user token reported fleet-wide")
			}
			if want := scopes["nodes:read"]; len(groups) != len(want) {
				t.Errorf("groups = %v, want %v", groups, want)
			}
		})
	}
}

func TestIssueToken_ServiceTokenIsFleetWide(t *testing.T) {
	verifier, issuer := testVerifierWithRevoked(t, "")
	signed, _, err := issuer.IssueToken("enc-bridge", TokenTypeService, []string{"nodes:read"}, time.Minute)
	if err != nil {
		t.Fatalf("IssueToken() error: %v", err)
	}
	claims, err := verifier.Verify(signed)
	if err != nil {
		t.Fatalf("Verify() error: %v", err)
	}
	if groups, fleetWide := claims.NodeGroupsFor("nodes:read"); !fleetWide || groups != nil {
		t.Errorf("NodeGroupsFor = %v, %v; want fleet-wide", groups, fleetWide)
	}
}

// A token minted before node scope existed has no nodeScopes claim at
// all, and keeps the fleet-wide access it was issued with.
func TestClaims_TokenWithoutTheClaimIsFleetWide(t *testing.T) {
	var claims Claims
	if err := json.Unmarshal([]byte(`{"sub":"alice","type":"access","permissions":["nodes:read"]}`), &claims); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if _, fleetWide := claims.NodeGroupsFor("nodes:read"); !fleetWide {
		t.Error("a token without the claim should be fleet-wide")
	}
}

func TestLogin_TokensCarryTheUsersNodeScopes(t *testing.T) {
	svc, store, _ := newTestAuthService(t)
	const userID, username = int64(1), "alice"
	store.scopes = map[int64]map[string][]int64{userID: {"nodes:read": {12}}}

	pair, err := svc.Login(context.Background(), username, "correct-password")
	if err != nil {
		t.Fatalf("Login() error: %v", err)
	}
	for kind, token := range map[string]string{"access": pair.AccessToken, "refresh": pair.RefreshToken} {
		claims, err := svc.verifier.Verify(token)
		if err != nil {
			t.Fatalf("Verify(%s) error: %v", kind, err)
		}
		if got := claims.NodeScopes["nodes:read"]; len(got) != 1 || got[0] != 12 {
			t.Errorf("%s token nodes:read scope = %v, want [12]", kind, got)
		}
	}

	// A refresh re-reads the scope, so a narrowed role takes effect.
	store.scopes[userID] = map[string][]int64{"nodes:read": {34}}
	refreshed, err := svc.Refresh(context.Background(), pair.RefreshToken)
	if err != nil {
		t.Fatalf("Refresh() error: %v", err)
	}
	claims, _ := svc.verifier.Verify(refreshed.AccessToken)
	if got := claims.NodeScopes["nodes:read"]; len(got) != 1 || got[0] != 34 {
		t.Errorf("refreshed nodes:read scope = %v, want [34]", got)
	}
}

func requestWithScopes(subject string, scopes map[string][]int64) *http.Request {
	claims := &Claims{
		RegisteredClaims: jwt.RegisteredClaims{Subject: subject, ID: "test-jti", ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour))},
		NodeScopes:       scopes,
	}
	ctx := context.WithValue(context.Background(), claimsContextKey, claims)
	return httptest.NewRequest(http.MethodGet, "/", nil).WithContext(ctx)
}

func TestHandlers_Roles_NodeGroups(t *testing.T) {
	h, store, _ := newTestHandlers(t)
	web := testNodeGroup(t, store, "scope-test-h-web")
	root := rootGroupID(t, store)

	create := func(body string) (*httptest.ResponseRecorder, Role) {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/roles", strings.NewReader(body)).WithContext(requestAs("admin").Context())
		rec := httptest.NewRecorder()
		h.createRole(rec, req)
		var role Role
		_ = json.Unmarshal(rec.Body.Bytes(), &role)
		if role.ID != 0 {
			t.Cleanup(func() { _ = store.DeleteRole(context.Background(), role.ID) })
		}
		return rec, role
	}

	rec, role := create(`{"name":"scope-test-h-scoped","permissions":["nodes:read"],"nodeGroups":[` + strconv.FormatInt(web, 10) + `]}`)
	if rec.Code != http.StatusCreated || !reflect.DeepEqual(role.NodeGroups, []int64{web}) {
		t.Fatalf("scoped create: status %d, groups %v; want 201 and [%d]: %s", rec.Code, role.NodeGroups, web, rec.Body.String())
	}

	rec, legacy := create(`{"name":"scope-test-h-legacy","permissions":["nodes:read"]}`)
	if rec.Code != http.StatusCreated || !reflect.DeepEqual(legacy.NodeGroups, []int64{root}) {
		t.Errorf("create without nodeGroups: status %d, groups %v; want 201 and the root [%d]", rec.Code, legacy.NodeGroups, root)
	}

	rec, empty := create(`{"name":"scope-test-h-empty","permissions":["nodes:read"],"nodeGroups":[]}`)
	if rec.Code != http.StatusCreated || empty.NodeGroups == nil || len(empty.NodeGroups) != 0 {
		t.Errorf("create with empty nodeGroups: status %d, groups %#v; want 201 and []", rec.Code, empty.NodeGroups)
	}

	rec, _ = create(`{"name":"scope-test-h-bad","permissions":["nodes:read"],"nodeGroups":[-42]}`)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("create with unknown group: status %d, want 400: %s", rec.Code, rec.Body.String())
	}

	update := func(body string) *httptest.ResponseRecorder {
		id := strconv.FormatInt(role.ID, 10)
		req := httptest.NewRequest(http.MethodPut, "/api/v1/roles/"+id, strings.NewReader(body)).WithContext(requestAs("admin").Context())
		req.SetPathValue("id", id)
		rec := httptest.NewRecorder()
		h.updateRole(rec, req)
		return rec
	}
	if rec := update(`{"name":"scope-test-h-scoped","permissions":["nodes:read"],"nodeGroups":[-42]}`); rec.Code != http.StatusBadRequest {
		t.Errorf("update with unknown group: status %d, want 400", rec.Code)
	}
	if got, _ := store.GetRole(context.Background(), role.ID); !reflect.DeepEqual(got.NodeGroups, []int64{web}) {
		t.Errorf("role groups after a rejected update = %v, want [%d]", got.NodeGroups, web)
	}
	rec = update(`{"name":"scope-test-h-scoped","permissions":["nodes:read"],"nodeGroups":[` + strconv.FormatInt(root, 10) + `]}`)
	var updated Role
	_ = json.Unmarshal(rec.Body.Bytes(), &updated)
	if rec.Code != http.StatusOK || !reflect.DeepEqual(updated.NodeGroups, []int64{root}) {
		t.Errorf("update to root: status %d, groups %v", rec.Code, updated.NodeGroups)
	}
}

func TestHandlers_GetMe_ReportsNodeScope(t *testing.T) {
	h, store, _ := newTestHandlers(t)
	web := testNodeGroup(t, store, "scope-test-me-web")
	root := rootGroupID(t, store)
	u, err := store.CreateUser(context.Background(), "scope-test-me-user", "password123")
	if err != nil {
		t.Fatalf("CreateUser() error: %v", err)
	}
	t.Cleanup(func() { _ = store.DeleteUser(context.Background(), u.ID) })

	me := func(scopes map[string][]int64) meResponse {
		rec := httptest.NewRecorder()
		h.getMe(rec, requestWithScopes(u.Username, scopes))
		if rec.Code != http.StatusOK {
			t.Fatalf("getMe status = %d: %s", rec.Code, rec.Body.String())
		}
		var resp meResponse
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatalf("decode: %v", err)
		}
		return resp
	}

	scoped := me(map[string][]int64{"nodes:read": {web, -42}})
	if scoped.NodeScope.All || len(scoped.NodeScope.Groups) != 1 || scoped.NodeScope.Groups[0].Name != "scope-test-me-web" {
		t.Errorf("scoped nodeScope = %+v, want only scope-test-me-web and not fleet-wide", scoped.NodeScope)
	}
	if scoped.Username != u.Username {
		t.Errorf("profile fields lost: %+v", scoped)
	}
	if fleet := me(map[string][]int64{"nodes:read": {root}}); !fleet.NodeScope.All {
		t.Errorf("root nodeScope = %+v, want fleet-wide", fleet.NodeScope)
	}
	if legacy := me(nil); !legacy.NodeScope.All {
		t.Errorf("token without a scope: nodeScope = %+v, want fleet-wide", legacy.NodeScope)
	}
	if none := me(map[string][]int64{}); none.NodeScope.All || len(none.NodeScope.Groups) != 0 {
		t.Errorf("no nodes:read scope: nodeScope = %+v, want no groups", none.NodeScope)
	}
}
