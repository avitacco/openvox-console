package rbac

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/voxpupuli/enterprise-console/internal/auditlog"
)

// groupAPI is a Handlers on a real mux, called with real tokens so the
// permission gate is part of what is tested.
type groupAPI struct {
	t        *testing.T
	mux      *http.ServeMux
	h        *Handlers
	store    *Store
	captured *capturedAudit
	admin    string
}

func newGroupAPI(t *testing.T) *groupAPI {
	t.Helper()
	h, store, captured := newTestHandlers(t)
	mux := http.NewServeMux()
	h.Register(mux)
	tok, _, err := h.issuer.IssueToken("group-api-admin", "access", []string{"rbac:admin"}, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	return &groupAPI{t: t, mux: mux, h: h, store: store, captured: captured, admin: tok}
}

func (a *groupAPI) do(token, method, path, body string) *httptest.ResponseRecorder {
	a.t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	a.mux.ServeHTTP(rec, req)
	return rec
}

func (a *groupAPI) admin_(method, path, body string) *httptest.ResponseRecorder {
	a.t.Helper()
	return a.do(a.admin, method, path, body)
}

func (a *groupAPI) createGroup(name string) Group {
	a.t.Helper()
	rec := a.admin_(http.MethodPost, "/api/v1/user-groups", `{"name":`+strconv.Quote(name)+`}`)
	if rec.Code != http.StatusCreated {
		a.t.Fatalf("create group %q = %d: %s", name, rec.Code, rec.Body.String())
	}
	var g Group
	_ = json.Unmarshal(rec.Body.Bytes(), &g)
	a.t.Cleanup(func() { _, _ = a.store.DeleteGroup(context.Background(), g.ID, true) })
	return g
}

func idPath(id int64) string { return strconv.FormatInt(id, 10) }

func TestGroupAPI_OnlyAdministratorsManageGroups(t *testing.T) {
	a := newGroupAPI(t)
	reader, _, _ := a.h.issuer.IssueToken("reader", "access", []string{"nodes:read", "orchestrator:run"}, time.Hour)
	for _, c := range []struct{ method, path string }{
		{"GET", "/api/v1/user-groups"}, {"POST", "/api/v1/user-groups"}, {"GET", "/api/v1/user-groups/1"},
		{"PUT", "/api/v1/user-groups/1"}, {"DELETE", "/api/v1/user-groups/1"},
		{"POST", "/api/v1/user-groups/1/members/1"}, {"DELETE", "/api/v1/user-groups/1/members/1"},
		{"POST", "/api/v1/user-groups/1/roles/1"}, {"DELETE", "/api/v1/user-groups/1/roles/1"},
		{"GET", "/api/v1/users/1/groups"},
	} {
		if rec := a.do(reader, c.method, c.path, `{"name":"x"}`); rec.Code != http.StatusForbidden {
			t.Errorf("%s %s without rbac:admin = %d, want 403", c.method, c.path, rec.Code)
		}
		if rec := a.do("", c.method, c.path, `{"name":"x"}`); rec.Code != http.StatusUnauthorized {
			t.Errorf("%s %s without a token = %d, want 401", c.method, c.path, rec.Code)
		}
	}
}

func TestGroupAPI_CreateRenameAndNames(t *testing.T) {
	a := newGroupAPI(t)
	g := a.createGroup("ga-web-ops")
	if len(a.captured.write) != 1 || a.captured.write[0].Action != "group.created" || a.captured.write[0].ResourceID != "ga-web-ops" {
		t.Errorf("audit = %+v, want one group.created for ga-web-ops", a.captured.write)
	}

	rec := a.admin_("POST", "/api/v1/user-groups", `{"name":"GA-Web-Ops"}`)
	if rec.Code != http.StatusConflict {
		t.Errorf("a name differing in case = %d, want 409", rec.Code)
	}
	rec = a.admin_("POST", "/api/v1/user-groups", `{"name":"  "}`)
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), `"name"`) {
		t.Errorf("a blank name = %d %s, want 400 naming the field", rec.Code, rec.Body.String())
	}

	a.captured.write = nil
	rec = a.admin_("PUT", "/api/v1/user-groups/"+idPath(g.ID), `{"name":"ga-web-team"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("rename = %d: %s", rec.Code, rec.Body.String())
	}
	if len(a.captured.write) != 1 || a.captured.write[0].Action != "group.renamed" {
		t.Fatalf("audit = %+v, want group.renamed", a.captured.write)
	}
	b, _ := a.captured.write[0].Before.(groupAudit)
	af, _ := a.captured.write[0].After.(groupAudit)
	if b.Name != "ga-web-ops" || af.Name != "ga-web-team" {
		t.Errorf("rename audit before/after = %q/%q", b.Name, af.Name)
	}
	if rec := a.admin_("GET", "/api/v1/user-groups/-5", ""); rec.Code != http.StatusNotFound && rec.Code != http.StatusBadRequest {
		t.Errorf("a missing group = %d, want 404", rec.Code)
	}
}

func TestGroupAPI_MembersRolesAndAudit(t *testing.T) {
	a := newGroupAPI(t)
	g := a.createGroup("ga-members")
	u := testUser(t, a.store, "ga-member")
	r := taskRole(t, a.store, "ga-role", []string{"nodes:read"}, nil, nil)
	a.captured.write = nil
	base := "/api/v1/user-groups/" + idPath(g.ID)

	for _, call := range []struct{ method, path string }{
		{"POST", base + "/members/" + idPath(u.ID)},
		{"POST", base + "/members/" + idPath(u.ID)}, // twice
		{"POST", base + "/roles/" + idPath(r.ID)},
	} {
		if rec := a.admin_(call.method, call.path, ""); rec.Code != http.StatusNoContent {
			t.Fatalf("%s %s = %d: %s", call.method, call.path, rec.Code, rec.Body.String())
		}
	}

	rec := a.admin_("GET", base, "")
	var got Group
	_ = json.Unmarshal(rec.Body.Bytes(), &got)
	if got.MemberCount != 1 || len(got.Members) != 1 || len(got.Roles) != 1 {
		t.Errorf("group = %+v, want one member and one role", got)
	}

	rec = a.admin_("GET", "/api/v1/users/"+idPath(u.ID)+"/roles", "")
	var roles []Role
	_ = json.Unmarshal(rec.Body.Bytes(), &roles)
	if len(roles) != 1 || !reflect.DeepEqual(roles[0].Via, []string{"ga-members"}) {
		t.Errorf("user roles = %+v, want one role via ga-members", roles)
	}
	rec = a.admin_("GET", "/api/v1/users/"+idPath(u.ID)+"/groups", "")
	if !strings.Contains(rec.Body.String(), "ga-members") {
		t.Errorf("user groups = %s", rec.Body.String())
	}

	for _, call := range []struct{ method, path string }{
		{"DELETE", base + "/roles/" + idPath(r.ID)},
		{"DELETE", base + "/members/" + idPath(u.ID)},
	} {
		if rec := a.admin_(call.method, call.path, ""); rec.Code != http.StatusNoContent {
			t.Fatalf("%s %s = %d: %s", call.method, call.path, rec.Code, rec.Body.String())
		}
	}
	var actions []string
	for _, e := range a.captured.write {
		actions = append(actions, e.Action)
		if e.ResourceType != "user_group" || e.ResourceID != "ga-members" {
			t.Errorf("event %+v is not about the group", e)
		}
	}
	// The repeated add is audited too: it is a request that was made.
	want := []string{"group.member.added", "group.member.added", "group.role.granted", "group.role.revoked", "group.member.removed"}
	if !reflect.DeepEqual(actions, want) {
		t.Errorf("audit actions = %v, want %v", actions, want)
	}
	first, _ := a.captured.write[0].After.(groupAudit)
	if first.User != "ga-member" {
		t.Errorf("the add did not name the user: %+v", a.captured.write[0])
	}
	assertNoSecrets(t, a.captured.write)

	if rec := a.admin_("POST", base+"/members/999999999", ""); rec.Code != http.StatusNotFound {
		t.Errorf("adding a missing user = %d, want 404", rec.Code)
	}
	if rec := a.admin_("POST", base+"/roles/999999999", ""); rec.Code != http.StatusNotFound {
		t.Errorf("granting a missing role = %d, want 404", rec.Code)
	}
}

func assertNoSecrets(t *testing.T, events []auditlog.Event) {
	t.Helper()
	for _, e := range events {
		b, _ := json.Marshal([]any{e.Before, e.After, e.ResourceID})
		for _, bad := range []string{"password", "token", "hash"} {
			if strings.Contains(strings.ToLower(string(b)), bad) {
				t.Errorf("event %s carries %q: %s", e.Action, bad, b)
			}
		}
	}
}

func TestGroupAPI_DeletingAGroupWithMembersNeedsConfirmation(t *testing.T) {
	a := newGroupAPI(t)
	g := a.createGroup("ga-delete")
	base := "/api/v1/user-groups/" + idPath(g.ID)
	for _, n := range []string{"ga-del-1", "ga-del-2", "ga-del-3"} {
		u := testUser(t, a.store, n)
		if rec := a.admin_("POST", base+"/members/"+idPath(u.ID), ""); rec.Code != http.StatusNoContent {
			t.Fatal(rec.Body.String())
		}
	}
	a.captured.write = nil

	rec := a.admin_("DELETE", base, "")
	if rec.Code != http.StatusConflict {
		t.Fatalf("delete without confirm = %d, want 409", rec.Code)
	}
	var body struct {
		MemberCount int `json:"memberCount"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	if body.MemberCount != 3 {
		t.Errorf("memberCount = %d, want 3", body.MemberCount)
	}
	if len(a.captured.write) != 0 {
		t.Errorf("a refused delete was audited: %+v", a.captured.write)
	}
	if rec := a.admin_("GET", base, ""); rec.Code != http.StatusOK {
		t.Fatalf("the group is gone after a refused delete: %d", rec.Code)
	}

	rec = a.admin_("DELETE", base+"?confirm=true", "")
	if rec.Code != http.StatusNoContent {
		t.Fatalf("confirmed delete = %d: %s", rec.Code, rec.Body.String())
	}
	if len(a.captured.write) != 1 || a.captured.write[0].Action != "group.deleted" {
		t.Fatalf("audit = %+v, want group.deleted", a.captured.write)
	}
	before, _ := a.captured.write[0].Before.(groupAudit)
	if before.MemberCount == nil || *before.MemberCount != 3 {
		t.Errorf("the delete did not record 3 members: %+v", before)
	}
}
