package classifier

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/voxpupuli/enterprise-console/internal/auditlog"
	"github.com/voxpupuli/enterprise-console/internal/pagination"
)

type fakeStore struct {
	groups map[int64]Group
	nextID int64
}

func newFakeStore() *fakeStore { return &fakeStore{groups: map[int64]Group{}, nextID: 1} }

func (f *fakeStore) CreateGroup(_ context.Context, g Group) (Group, error) {
	for _, existing := range f.groups {
		if existing.Priority == g.Priority {
			return Group{}, ErrDuplicatePriority
		}
	}
	g.ID = f.nextID
	f.nextID++
	f.groups[g.ID] = g
	return g, nil
}

func (f *fakeStore) GetGroup(_ context.Context, id int64) (Group, error) {
	g, ok := f.groups[id]
	if !ok {
		return Group{}, ErrNotFound
	}
	return g, nil
}

func (f *fakeStore) ListGroups(_ context.Context, page, pageSize int) ([]Group, int, error) {
	groups := make([]Group, 0, len(f.groups))
	for _, g := range f.groups {
		groups = append(groups, g)
	}
	total := len(groups)
	start := (page - 1) * pageSize
	if start > total {
		start = total
	}
	end := start + pageSize
	if end > total {
		end = total
	}
	return groups[start:end], total, nil
}

func (f *fakeStore) UpdateGroup(_ context.Context, g Group) error {
	if _, ok := f.groups[g.ID]; !ok {
		return ErrNotFound
	}
	f.groups[g.ID] = g
	return nil
}

func (f *fakeStore) DeleteGroup(_ context.Context, id int64) error {
	if _, ok := f.groups[id]; !ok {
		return ErrNotFound
	}
	delete(f.groups, id)
	return nil
}

func noopRecordActivity(_ *http.Request, _, _ string) {}

// recordedActivity captures recordActivity calls, so tests can assert an
// activity event was actually published for a given action.
type recordedActivity struct {
	action, summary string
}

func capturingRecordActivity(dst *[]recordedActivity) func(*http.Request, string, string) {
	return func(_ *http.Request, action, summary string) {
		*dst = append(*dst, recordedActivity{action: action, summary: summary})
	}
}

func noopRecordAudit(_ *http.Request, _ auditlog.Event) {}

func capturingRecordAudit(dst *[]auditlog.Event) func(*http.Request, auditlog.Event) {
	return func(_ *http.Request, e auditlog.Event) {
		*dst = append(*dst, e)
	}
}

func TestCreateGroup_Success(t *testing.T) {
	h := NewHandlers(newFakeStore(), noopRecordActivity, noopRecordAudit, noopRecordAudit)
	body, _ := json.Marshal(Group{Name: "web", Priority: 1})

	req := httptest.NewRequest(http.MethodPost, "/api/v1/groups", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	h.createGroup(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201; body: %s", rec.Code, rec.Body)
	}
	var created Group
	if err := json.NewDecoder(rec.Body).Decode(&created); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if created.ID == 0 {
		t.Error("expected an assigned ID")
	}
}

func TestCreateGroup_RecordsActivity(t *testing.T) {
	var recorded []recordedActivity
	h := NewHandlers(newFakeStore(), capturingRecordActivity(&recorded), noopRecordAudit, noopRecordAudit)
	body, _ := json.Marshal(Group{Name: "web-servers", Priority: 1})

	req := httptest.NewRequest(http.MethodPost, "/api/v1/groups", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	h.createGroup(rec, req)

	if len(recorded) != 1 {
		t.Fatalf("recorded %d activity events, want 1", len(recorded))
	}
	if recorded[0].action != "group.created" {
		t.Errorf("action = %q, want group.created", recorded[0].action)
	}
	if recorded[0].summary != "created group web-servers" {
		t.Errorf("summary = %q", recorded[0].summary)
	}
}

func TestDeleteGroup_RecordsActivityWithName(t *testing.T) {
	s := newFakeStore()
	s.groups[1] = Group{ID: 1, Name: "to-delete", Priority: 1}
	var recorded []recordedActivity
	h := NewHandlers(s, capturingRecordActivity(&recorded), noopRecordAudit, noopRecordAudit)

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/groups/1", nil)
	req.SetPathValue("id", "1")
	rec := httptest.NewRecorder()
	h.deleteGroup(rec, req)

	if len(recorded) != 1 {
		t.Fatalf("recorded %d activity events, want 1", len(recorded))
	}
	if recorded[0].summary != "deleted group to-delete" {
		t.Errorf("summary = %q", recorded[0].summary)
	}
}

func TestCreateGroup_DuplicatePriorityReturns409(t *testing.T) {
	s := newFakeStore()
	h := NewHandlers(s, noopRecordActivity, noopRecordAudit, noopRecordAudit)
	s.groups[1] = Group{ID: 1, Name: "existing", Priority: 5}

	body, _ := json.Marshal(Group{Name: "new", Priority: 5})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/groups", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	h.createGroup(rec, req)

	if rec.Code != http.StatusConflict {
		t.Errorf("status = %d, want 409", rec.Code)
	}
}

func TestGetGroup_NotFoundReturns404(t *testing.T) {
	h := NewHandlers(newFakeStore(), noopRecordActivity, noopRecordAudit, noopRecordAudit)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/groups/999", nil)
	req.SetPathValue("id", "999")
	rec := httptest.NewRecorder()
	h.getGroup(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", rec.Code)
	}
}

func TestListGroups_ReturnsAll(t *testing.T) {
	s := newFakeStore()
	s.groups[1] = Group{ID: 1, Name: "a", Priority: 1}
	s.groups[2] = Group{ID: 2, Name: "b", Priority: 2}
	h := NewHandlers(s, noopRecordActivity, noopRecordAudit, noopRecordAudit)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/groups", nil)
	rec := httptest.NewRecorder()
	h.listGroups(rec, req)

	var page pagination.Page[Group]
	if err := json.NewDecoder(rec.Body).Decode(&page); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(page.Items) != 2 {
		t.Errorf("got %d groups, want 2", len(page.Items))
	}
	if page.Total != 2 {
		t.Errorf("Total = %d, want 2", page.Total)
	}
}

func TestUpdateGroup_PersistsChanges(t *testing.T) {
	s := newFakeStore()
	s.groups[1] = Group{ID: 1, Name: "a", Priority: 1}
	h := NewHandlers(s, noopRecordActivity, noopRecordAudit, noopRecordAudit)

	body, _ := json.Marshal(Group{Name: "a-renamed", Priority: 1})
	req := httptest.NewRequest(http.MethodPut, "/api/v1/groups/1", bytes.NewReader(body))
	req.SetPathValue("id", "1")
	rec := httptest.NewRecorder()
	h.updateGroup(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", rec.Code, rec.Body)
	}
	if s.groups[1].Name != "a-renamed" {
		t.Errorf("Name = %q, want a-renamed", s.groups[1].Name)
	}
}

func TestCreateGroup_RecordsAuditEvent(t *testing.T) {
	var recorded []auditlog.Event
	h := NewHandlers(newFakeStore(), noopRecordActivity, capturingRecordAudit(&recorded), noopRecordAudit)
	body, _ := json.Marshal(Group{Name: "audit-web", Priority: 1})

	req := httptest.NewRequest(http.MethodPost, "/api/v1/groups", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	h.createGroup(rec, req)

	if len(recorded) != 1 {
		t.Fatalf("recorded %d audit events, want 1", len(recorded))
	}
	if recorded[0].Action != "group.created" || recorded[0].ResourceID != "audit-web" {
		t.Errorf("event = %+v, want action=group.created resourceId=audit-web", recorded[0])
	}
}

func TestUpdateGroup_RecordsAuditEventWithBeforeAfter(t *testing.T) {
	s := newFakeStore()
	s.groups[1] = Group{ID: 1, Name: "a", Priority: 1}
	var recorded []auditlog.Event
	h := NewHandlers(s, noopRecordActivity, capturingRecordAudit(&recorded), noopRecordAudit)

	body, _ := json.Marshal(Group{Name: "a-renamed", Priority: 1})
	req := httptest.NewRequest(http.MethodPut, "/api/v1/groups/1", bytes.NewReader(body))
	req.SetPathValue("id", "1")
	rec := httptest.NewRecorder()
	h.updateGroup(rec, req)

	if len(recorded) != 1 {
		t.Fatalf("recorded %d audit events, want 1", len(recorded))
	}
	before, ok := recorded[0].Before.(Group)
	if !ok || before.Name != "a" {
		t.Errorf("Before = %#v, want the pre-update group named a", recorded[0].Before)
	}
	after, ok := recorded[0].After.(Group)
	if !ok || after.Name != "a-renamed" {
		t.Errorf("After = %#v, want the post-update group named a-renamed", recorded[0].After)
	}
}

func TestDeleteGroup_RecordsAuditEvent(t *testing.T) {
	s := newFakeStore()
	s.groups[1] = Group{ID: 1, Name: "to-delete", Priority: 1}
	var recorded []auditlog.Event
	h := NewHandlers(s, noopRecordActivity, capturingRecordAudit(&recorded), noopRecordAudit)

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/groups/1", nil)
	req.SetPathValue("id", "1")
	rec := httptest.NewRecorder()
	h.deleteGroup(rec, req)

	if len(recorded) != 1 || recorded[0].Action != "group.deleted" || recorded[0].ResourceID != "to-delete" {
		t.Errorf("recorded = %+v, want exactly one group.deleted for to-delete", recorded)
	}
}

func TestListAndGetGroup_RecordReadAuditEvents(t *testing.T) {
	s := newFakeStore()
	s.groups[1] = Group{ID: 1, Name: "a", Priority: 1}
	var recorded []auditlog.Event
	h := NewHandlers(s, noopRecordActivity, noopRecordAudit, capturingRecordAudit(&recorded))

	h.listGroups(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/api/v1/groups", nil))

	req := httptest.NewRequest(http.MethodGet, "/api/v1/groups/1", nil)
	req.SetPathValue("id", "1")
	h.getGroup(httptest.NewRecorder(), req)

	if len(recorded) != 2 {
		t.Fatalf("recorded %d read audit events, want 2", len(recorded))
	}
	if recorded[0].Action != "group.list.viewed" {
		t.Errorf("event 0 Action = %q, want group.list.viewed", recorded[0].Action)
	}
	if recorded[1].Action != "group.viewed" || recorded[1].ResourceID != "a" {
		t.Errorf("event 1 = %+v, want action=group.viewed resourceId=a", recorded[1])
	}
}

func TestDeleteGroup_RemovesIt(t *testing.T) {
	s := newFakeStore()
	s.groups[1] = Group{ID: 1, Name: "a", Priority: 1}
	h := NewHandlers(s, noopRecordActivity, noopRecordAudit, noopRecordAudit)

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/groups/1", nil)
	req.SetPathValue("id", "1")
	rec := httptest.NewRecorder()
	h.deleteGroup(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Errorf("status = %d, want 204", rec.Code)
	}
	if _, ok := s.groups[1]; ok {
		t.Error("expected group to be removed from store")
	}
}
