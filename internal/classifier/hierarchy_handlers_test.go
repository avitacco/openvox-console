package classifier

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/voxpupuli/enterprise-console/internal/pagination"
)

// failingStore returns err from every write, to exercise how each store
// error is reported without needing Postgres to produce it.
type failingStore struct {
	*fakeStore
	err error
}

func (f *failingStore) CreateGroup(context.Context, Group) (Group, error) { return Group{}, f.err }
func (f *failingStore) UpdateGroup(context.Context, Group) error          { return f.err }
func (f *failingStore) DeleteGroup(context.Context, int64) error          { return f.err }

func TestHierarchyErrors_StatusAndMessage(t *testing.T) {
	tests := []struct {
		err  error
		want int
	}{
		{ErrHasChildren, http.StatusConflict},
		{ErrGroupGranted, http.StatusConflict},
		{ErrHierarchyLoop, http.StatusConflict},
		{ErrParentNotFound, http.StatusBadRequest},
		{ErrInvalidRuleMatch, http.StatusBadRequest},
		{ErrRootImmutable, http.StatusBadRequest},
	}
	for _, tt := range tests {
		t.Run(tt.err.Error(), func(t *testing.T) {
			fake := newFakeStore()
			fake.groups[1] = Group{ID: 1, Name: "a", Priority: 1}
			h := NewHandlers(&failingStore{fakeStore: fake, err: tt.err}, noopRecordActivity, noopRecordAudit, noopRecordAudit)

			body, _ := json.Marshal(Group{Name: "a", Priority: 1})
			requests := map[string]func(*httptest.ResponseRecorder){
				"create": func(rec *httptest.ResponseRecorder) {
					h.createGroup(rec, httptest.NewRequest(http.MethodPost, "/api/v1/groups", bytes.NewReader(body)))
				},
				"update": func(rec *httptest.ResponseRecorder) {
					req := httptest.NewRequest(http.MethodPut, "/api/v1/groups/1", bytes.NewReader(body))
					req.SetPathValue("id", "1")
					h.updateGroup(rec, req)
				},
				"delete": func(rec *httptest.ResponseRecorder) {
					req := httptest.NewRequest(http.MethodDelete, "/api/v1/groups/1", nil)
					req.SetPathValue("id", "1")
					h.deleteGroup(rec, req)
				},
			}
			for name, do := range requests {
				rec := httptest.NewRecorder()
				do(rec)
				if rec.Code != tt.want {
					t.Errorf("%s: status = %d, want %d", name, rec.Code, tt.want)
				}
				// The store's message says what to do, and must reach the user.
				var body map[string]string
				if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil || body["error"] != tt.err.Error() {
					t.Errorf("%s: body = %s, want error %q", name, rec.Body, tt.err)
				}
			}
			if _, ok := fake.groups[1]; !ok {
				t.Error("the group should be unchanged after a refused request")
			}
		})
	}
}

// overridingGroups is a root, a parent at priority 20, and a child at
// priority 10 that sets the same class parameter differently.
func overridingGroups() *fakeStore {
	s := newFakeStore()
	s.groups[1] = Group{ID: 1, Name: "All Nodes", IsRoot: true}
	s.groups[2] = Group{ID: 2, Name: "parent", ParentID: int64Ptr(1), Priority: 20,
		Classes: []Class{{Name: "ntp", Parameters: map[string]any{"server": "a"}}}}
	s.groups[3] = Group{ID: 3, Name: "child", ParentID: int64Ptr(2), Priority: 10,
		Classes: []Class{{Name: "ntp", Parameters: map[string]any{"server": "b"}}}}
	s.nextID = 4
	return s
}

func captureLog(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, nil)))
	t.Cleanup(func() { slog.SetDefault(previous) })
	return &buf
}

func TestUpdateGroup_SaveThatOverridesAncestorSucceedsWithWarning(t *testing.T) {
	s := overridingGroups()
	logs := captureLog(t)
	var activity []recordedActivity
	h := NewHandlers(s, capturingRecordActivity(&activity), noopRecordAudit, noopRecordAudit)

	body, _ := json.Marshal(s.groups[3])
	req := httptest.NewRequest(http.MethodPut, "/api/v1/groups/3", bytes.NewReader(body))
	req.SetPathValue("id", "3")
	rec := httptest.NewRecorder()
	h.updateGroup(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 - a warning must not refuse the save; body: %s", rec.Code, rec.Body)
	}
	var resp groupResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	want := Warning{AncestorID: 2, AncestorName: "parent", Kind: WarningClassParameter, Class: "ntp", Key: "server"}
	if len(resp.Warnings) != 1 || resp.Warnings[0] != want {
		t.Errorf("Warnings = %+v, want [%+v]", resp.Warnings, want)
	}

	if len(activity) != 1 || !strings.Contains(activity[0].summary, "overrides higher-priority ancestor parent") {
		t.Errorf("activity = %+v, want a summary mentioning the override", activity)
	}
	if !strings.Contains(logs.String(), "level=WARN") || !strings.Contains(logs.String(), "child") {
		t.Errorf("server log = %q, want a warning naming the group", logs.String())
	}
}

func TestUpdateGroup_EditingAncestorNamesDescendantsNowOverridingIt(t *testing.T) {
	s := overridingGroups()
	var activity []recordedActivity
	h := NewHandlers(s, capturingRecordActivity(&activity), noopRecordAudit, noopRecordAudit)

	body, _ := json.Marshal(s.groups[2])
	req := httptest.NewRequest(http.MethodPut, "/api/v1/groups/2", bytes.NewReader(body))
	req.SetPathValue("id", "2")
	rec := httptest.NewRecorder()
	h.updateGroup(rec, req)

	var resp groupResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Warnings) != 0 {
		t.Errorf("Warnings = %+v, want none on the ancestor itself", resp.Warnings)
	}
	if len(resp.OverriddenBy) != 1 || resp.OverriddenBy[0].GroupName != "child" {
		t.Fatalf("OverriddenBy = %+v, want the child", resp.OverriddenBy)
	}
	if len(activity) != 1 || !strings.Contains(activity[0].summary, "overridden by lower-priority descendant child") {
		t.Errorf("activity = %+v, want a summary naming the descendant", activity)
	}
}

func TestUpdateGroup_CleanSaveRecordsNoOverride(t *testing.T) {
	s := overridingGroups()
	child := s.groups[3]
	child.Priority = 30 // now higher than its parent: no warning
	s.groups[3] = child
	logs := captureLog(t)
	var activity []recordedActivity
	h := NewHandlers(s, capturingRecordActivity(&activity), noopRecordAudit, noopRecordAudit)

	body, _ := json.Marshal(child)
	req := httptest.NewRequest(http.MethodPut, "/api/v1/groups/3", bytes.NewReader(body))
	req.SetPathValue("id", "3")
	rec := httptest.NewRecorder()
	h.updateGroup(rec, req)

	if len(activity) != 1 || activity[0].summary != "updated group child" {
		t.Errorf("activity = %+v, want a plain summary", activity)
	}
	if logs.Len() != 0 {
		t.Errorf("server log = %q, want nothing logged", logs.String())
	}
	if !strings.Contains(rec.Body.String(), `"warnings":[]`) {
		t.Errorf("body = %s, want an empty warnings list rather than none", rec.Body)
	}
}

func TestGetAndListGroups_ReportTheSameWarning(t *testing.T) {
	h := NewHandlers(overridingGroups(), noopRecordActivity, noopRecordAudit, noopRecordAudit)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/groups/3", nil)
	req.SetPathValue("id", "3")
	rec := httptest.NewRecorder()
	h.getGroup(rec, req)
	var got groupResponse
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(got.Warnings) != 1 {
		t.Errorf("viewing: Warnings = %+v, want one", got.Warnings)
	}

	for _, url := range []string{"/api/v1/groups", "/api/v1/groups?all=true"} {
		rec := httptest.NewRecorder()
		h.listGroups(rec, httptest.NewRequest(http.MethodGet, url, nil))
		var page pagination.Page[groupResponse]
		if err := json.NewDecoder(rec.Body).Decode(&page); err != nil {
			t.Fatalf("%s: decode: %v", url, err)
		}
		warned := 0
		for _, g := range page.Items {
			if len(g.Warnings) > 0 {
				warned++
				if g.Name != "child" {
					t.Errorf("%s: %q carries a warning, want only the child", url, g.Name)
				}
			}
		}
		if warned != 1 {
			t.Errorf("%s: %d groups carry a warning, want 1", url, warned)
		}
	}
}

func TestListGroups_AllReturnsEveryGroupInOneResponse(t *testing.T) {
	s := newFakeStore()
	for i := int64(1); i <= 60; i++ {
		s.groups[i] = Group{ID: i, Name: "g", Priority: int(i)}
	}
	h := NewHandlers(s, noopRecordActivity, noopRecordAudit, noopRecordAudit)

	rec := httptest.NewRecorder()
	h.listGroups(rec, httptest.NewRequest(http.MethodGet, "/api/v1/groups?all=true", nil))

	var page pagination.Page[groupResponse]
	if err := json.NewDecoder(rec.Body).Decode(&page); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(page.Items) != 60 || page.Total != 60 {
		t.Errorf("got %d items, total %d; want all 60", len(page.Items), page.Total)
	}
}

func TestGroupResponse_RootHasNoPriorityAndOthersKeepZero(t *testing.T) {
	root, _ := json.Marshal(newGroupResponse(Group{ID: 1, Name: "All Nodes", IsRoot: true}, nil))
	if strings.Contains(string(root), `"priority"`) {
		t.Errorf("root = %s, want no priority", root)
	}
	if !strings.Contains(string(root), `"isRoot":true`) {
		t.Errorf("root = %s, want isRoot true", root)
	}

	zero, _ := json.Marshal(newGroupResponse(Group{ID: 2, Name: "zero", ParentID: int64Ptr(1), RuleMatch: RuleMatchAny}, nil))
	for _, want := range []string{`"priority":0`, `"parentId":1`, `"ruleMatch":"any"`, `"isRoot":false`} {
		if !strings.Contains(string(zero), want) {
			t.Errorf("group = %s, want it to contain %s", zero, want)
		}
	}
}
