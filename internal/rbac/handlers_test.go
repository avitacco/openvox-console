package rbac

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"github.com/voxpupuli/enterprise-console/internal/auditlog"
	"github.com/voxpupuli/enterprise-console/internal/messaging"
)

// capturedAudit collects the audit events a test's Handlers recorded, by
// which closure fired (auth, rbac-write, rbac-read) - see
// design.md in the configurable-audit-logging change for why Handlers
// has three separate injected closures.
type capturedAudit struct {
	auth  []auditlog.Event
	write []auditlog.Event
	read  []auditlog.Event
}

// newTestHandlers builds a real, Postgres-backed Handlers (skipping if
// CONSOLE_TEST_POSTGRES_DSN isn't set, like testStore) with every
// recordActivity/recordAudit* closure capturing instead of publishing,
// so tests can assert on exactly what would have been emitted.
func newTestHandlers(t *testing.T) (*Handlers, *Store, *capturedAudit) {
	t.Helper()
	store := testStore(t)

	key := testKey(t)
	bus, err := messaging.Start()
	if err != nil {
		t.Fatalf("messaging.Start() error: %v", err)
	}
	t.Cleanup(bus.Close)

	revoker := NewRevoker(bus, newFakeRevokedTokenStore())
	if err := revoker.Start(context.Background()); err != nil {
		t.Fatalf("revoker.Start() error: %v", err)
	}
	issuer := NewIssuer(key, testKid)
	verifier := singleKeyVerifier(testKid, key, revoker)
	authService := NewAuthService(store, issuer, verifier, revoker, noopRecordAudit)
	oidcService := NewOIDCService(context.Background(), discardLogger(), store, issuer, OIDCConfig{}, noopRecordSystemActivity, noopRecordAudit)

	captured := &capturedAudit{}
	h := NewHandlers(
		authService, store, verifier, revoker, issuer, oidcService,
		func(r *http.Request, action, summary string) {},
		func(r *http.Request, e auditlog.Event) { captured.auth = append(captured.auth, e) },
		func(r *http.Request, e auditlog.Event) { captured.write = append(captured.write, e) },
		func(r *http.Request, e auditlog.Event) { captured.read = append(captured.read, e) },
	)
	return h, store, captured
}

// requestAs builds a request carrying claims for subject, as if
// Verifier.Authenticate had already run - the shape every handler under
// test expects on r.Context().
func requestAs(subject string) *http.Request {
	claims := &Claims{RegisteredClaims: jwt.RegisteredClaims{
		Subject: subject, ID: "test-jti", ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
	}}
	ctx := context.WithValue(context.Background(), claimsContextKey, claims)
	return httptest.NewRequest(http.MethodGet, "/", nil).WithContext(ctx)
}

func TestHandlers_Logout_RecordsAuthAuditEvent(t *testing.T) {
	h, _, captured := newTestHandlers(t)

	rec := httptest.NewRecorder()
	h.logout(rec, requestAs("alice"))

	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", rec.Code)
	}
	if len(captured.auth) != 1 {
		t.Fatalf("auth events = %v, want exactly 1", captured.auth)
	}
	if captured.auth[0].Action != "user.logout" || captured.auth[0].ResourceID != "alice" {
		t.Errorf("event = %+v, want action=user.logout resourceId=alice", captured.auth[0])
	}
}

func TestHandlers_CreateUser_RecordsWriteAuditEvent(t *testing.T) {
	h, store, captured := newTestHandlers(t)

	body := strings.NewReader(`{"username":"audit-test-create","password":"pw"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/users", body).WithContext(requestAs("admin").Context())
	rec := httptest.NewRecorder()
	h.createUser(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201: %s", rec.Code, rec.Body.String())
	}
	t.Cleanup(func() {
		u, err := store.GetUserByUsername(context.Background(), "audit-test-create")
		if err == nil {
			_ = store.DeleteUser(context.Background(), u.ID)
		}
	})

	if len(captured.write) != 1 {
		t.Fatalf("write events = %v, want exactly 1", captured.write)
	}
	if captured.write[0].Action != "user.created" || captured.write[0].ResourceID != "audit-test-create" {
		t.Errorf("event = %+v, want action=user.created resourceId=audit-test-create", captured.write[0])
	}
}

func TestHandlers_UpdateUser_RecordsWriteAuditEventWithNoPassword(t *testing.T) {
	h, store, captured := newTestHandlers(t)

	u, err := store.CreateUser(context.Background(), "audit-test-update", "old-pw")
	if err != nil {
		t.Fatalf("CreateUser() error: %v", err)
	}
	t.Cleanup(func() { _ = store.DeleteUser(context.Background(), u.ID) })

	body := strings.NewReader(`{"password":"a-new-password-value"}`)
	req := httptest.NewRequest(http.MethodPut, "/api/v1/users/"+strconv.FormatInt(u.ID, 10), body).WithContext(requestAs("admin").Context())
	req.SetPathValue("id", strconv.FormatInt(u.ID, 10))
	rec := httptest.NewRecorder()
	h.updateUser(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204: %s", rec.Code, rec.Body.String())
	}

	if len(captured.write) != 1 {
		t.Fatalf("write events = %v, want exactly 1", captured.write)
	}
	got := captured.write[0]
	if got.Action != "admin.user.updated" {
		t.Errorf("Action = %q, want admin.user.updated", got.Action)
	}
	if strings.Contains(got.ResourceID, "a-new-password-value") {
		t.Errorf("event resourceId leaked the password: %+v", got)
	}
	if s, ok := got.Before.(string); ok && strings.Contains(s, "a-new-password-value") {
		t.Errorf("event Before leaked the password: %+v", got)
	}
	if s, ok := got.After.(string); ok && strings.Contains(s, "a-new-password-value") {
		t.Errorf("event After leaked the password: %+v", got)
	}
}

func TestHandlers_UpdateMe_RecordsWriteAuditEventWithNoPassword(t *testing.T) {
	h, store, captured := newTestHandlers(t)

	_, err := store.CreateUser(context.Background(), "audit-test-me", "old-pw")
	if err != nil {
		t.Fatalf("CreateUser() error: %v", err)
	}
	t.Cleanup(func() {
		u, err := store.GetUserByUsername(context.Background(), "audit-test-me")
		if err == nil {
			_ = store.DeleteUser(context.Background(), u.ID)
		}
	})

	body := strings.NewReader(`{"currentPassword":"old-pw","newPassword":"a-fresh-password"}`)
	req := httptest.NewRequest(http.MethodPut, "/api/v1/me", body).WithContext(requestAs("audit-test-me").Context())
	rec := httptest.NewRecorder()
	h.updateMe(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204: %s", rec.Code, rec.Body.String())
	}

	if len(captured.write) != 1 {
		t.Fatalf("write events = %v, want exactly 1", captured.write)
	}
	got := captured.write[0]
	if got.Action != "user.profile.updated" || got.ResourceID != "audit-test-me" {
		t.Errorf("event = %+v, want action=user.profile.updated resourceId=audit-test-me", got)
	}
	if strings.Contains(got.ResourceID, "fresh-password") {
		t.Errorf("event leaked the password: %+v", got)
	}
}

func TestHandlers_DeleteUser_RecordsWriteAuditEvent(t *testing.T) {
	h, store, captured := newTestHandlers(t)

	u, err := store.CreateUser(context.Background(), "audit-test-delete", "pw")
	if err != nil {
		t.Fatalf("CreateUser() error: %v", err)
	}

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/users/"+strconv.FormatInt(u.ID, 10), nil).WithContext(requestAs("admin").Context())
	req.SetPathValue("id", strconv.FormatInt(u.ID, 10))
	rec := httptest.NewRecorder()
	h.deleteUser(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204: %s", rec.Code, rec.Body.String())
	}

	if len(captured.write) != 1 || captured.write[0].Action != "user.deleted" {
		t.Errorf("write events = %v, want exactly one user.deleted", captured.write)
	}
}

func TestHandlers_AssignAndUnassignRole_RecordWriteAuditEvents(t *testing.T) {
	h, store, captured := newTestHandlers(t)

	u, err := store.CreateUser(context.Background(), "audit-test-assign", "pw")
	if err != nil {
		t.Fatalf("CreateUser() error: %v", err)
	}
	t.Cleanup(func() { _ = store.DeleteUser(context.Background(), u.ID) })
	role, err := store.CreateRole(context.Background(), "audit-test-role-assign", []string{"nodes:read"})
	if err != nil {
		t.Fatalf("CreateRole() error: %v", err)
	}
	t.Cleanup(func() { _ = store.DeleteRole(context.Background(), role.ID) })

	uID, rID := strconv.FormatInt(u.ID, 10), strconv.FormatInt(role.ID, 10)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/users/"+uID+"/roles/"+rID, nil).WithContext(requestAs("admin").Context())
	req.SetPathValue("id", uID)
	req.SetPathValue("roleId", rID)
	rec := httptest.NewRecorder()
	h.assignRole(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("assignRole status = %d, want 204: %s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodDelete, "/api/v1/users/"+uID+"/roles/"+rID, nil).WithContext(requestAs("admin").Context())
	req.SetPathValue("id", uID)
	req.SetPathValue("roleId", rID)
	rec = httptest.NewRecorder()
	h.unassignRole(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("unassignRole status = %d, want 204: %s", rec.Code, rec.Body.String())
	}

	if len(captured.write) != 2 {
		t.Fatalf("write events = %v, want exactly 2", captured.write)
	}
	wantID := "user:" + uID + "/role:" + rID
	if captured.write[0].Action != "role.assigned" || captured.write[0].ResourceID != wantID {
		t.Errorf("event 0 = %+v, want action=role.assigned resourceId=%s", captured.write[0], wantID)
	}
	if captured.write[1].Action != "role.unassigned" || captured.write[1].ResourceID != wantID {
		t.Errorf("event 1 = %+v, want action=role.unassigned resourceId=%s", captured.write[1], wantID)
	}
}

func TestHandlers_UpdateRole_RecordsPermissionDiff(t *testing.T) {
	h, store, captured := newTestHandlers(t)

	role, err := store.CreateRole(context.Background(), "audit-test-role-diff", []string{"nodes:read"})
	if err != nil {
		t.Fatalf("CreateRole() error: %v", err)
	}
	t.Cleanup(func() { _ = store.DeleteRole(context.Background(), role.ID) })

	body := strings.NewReader(`{"name":"audit-test-role-diff","permissions":["nodes:read","rbac:admin"]}`)
	req := httptest.NewRequest(http.MethodPut, "/api/v1/roles/"+strconv.FormatInt(role.ID, 10), body).WithContext(requestAs("admin").Context())
	req.SetPathValue("id", strconv.FormatInt(role.ID, 10))
	rec := httptest.NewRecorder()
	h.updateRole(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}

	if len(captured.write) != 1 {
		t.Fatalf("write events = %v, want exactly 1", captured.write)
	}
	got := captured.write[0]
	before, ok := got.Before.([]string)
	if !ok || len(before) != 1 || before[0] != "nodes:read" {
		t.Errorf("Before = %#v, want [nodes:read]", got.Before)
	}
	after, ok := got.After.([]string)
	if !ok || len(after) != 2 {
		t.Errorf("After = %#v, want [nodes:read rbac:admin]", got.After)
	}
}

func TestHandlers_ServiceTokenCreateAndRevoke_RecordWriteAuditEvents(t *testing.T) {
	h, store, captured := newTestHandlers(t)

	body := strings.NewReader(`{"name":"audit-test-svc-token","permissions":["enc:read"]}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/service-tokens", body).WithContext(requestAs("admin").Context())
	rec := httptest.NewRecorder()
	h.createServiceToken(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("createServiceToken status = %d, want 201: %s", rec.Code, rec.Body.String())
	}

	st, err := store.ListServiceTokens(context.Background())
	if err != nil {
		t.Fatalf("ListServiceTokens() error: %v", err)
	}
	var id int64
	for _, s := range st {
		if s.Name == "audit-test-svc-token" {
			id = s.ID
		}
	}
	if id == 0 {
		t.Fatal("created service token not found")
	}

	req = httptest.NewRequest(http.MethodDelete, "/api/v1/service-tokens/"+strconv.FormatInt(id, 10), nil).WithContext(requestAs("admin").Context())
	req.SetPathValue("id", strconv.FormatInt(id, 10))
	rec = httptest.NewRecorder()
	h.revokeServiceToken(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("revokeServiceToken status = %d, want 204: %s", rec.Code, rec.Body.String())
	}

	if len(captured.write) != 2 {
		t.Fatalf("write events = %v, want exactly 2", captured.write)
	}
	if captured.write[0].Action != "service_token.created" || captured.write[1].Action != "service_token.revoked" {
		t.Errorf("events = %+v, want [service_token.created, service_token.revoked]", captured.write)
	}
}

func TestHandlers_ReadHandlers_RecordReadAuditEvents(t *testing.T) {
	h, store, captured := newTestHandlers(t)

	u, err := store.CreateUser(context.Background(), "audit-test-read", "pw")
	if err != nil {
		t.Fatalf("CreateUser() error: %v", err)
	}
	t.Cleanup(func() { _ = store.DeleteUser(context.Background(), u.ID) })
	role, err := store.CreateRole(context.Background(), "audit-test-role-read", []string{"nodes:read"})
	if err != nil {
		t.Fatalf("CreateRole() error: %v", err)
	}
	t.Cleanup(func() { _ = store.DeleteRole(context.Background(), role.ID) })

	h.listUsers(httptest.NewRecorder(), requestAs("admin"))
	uReq := requestAs("admin")
	uReq.SetPathValue("id", strconv.FormatInt(u.ID, 10))
	h.getUser(httptest.NewRecorder(), uReq)
	h.listRoles(httptest.NewRecorder(), requestAs("admin"))
	rReq := requestAs("admin")
	rReq.SetPathValue("id", strconv.FormatInt(role.ID, 10))
	h.getRole(httptest.NewRecorder(), rReq)
	h.listServiceTokens(httptest.NewRecorder(), requestAs("admin"))

	if len(captured.read) != 5 {
		t.Fatalf("read events = %v, want exactly 5", captured.read)
	}
	wantActions := []string{"user.list.viewed", "user.viewed", "role.list.viewed", "role.viewed", "service_token.list.viewed"}
	for i, want := range wantActions {
		if captured.read[i].Action != want {
			t.Errorf("event %d Action = %q, want %q", i, captured.read[i].Action, want)
		}
	}
	if len(captured.write) != 0 || len(captured.auth) != 0 {
		t.Errorf("read-only handlers must not record write/auth events: write=%v auth=%v", captured.write, captured.auth)
	}
}
