package classifier

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/voxpupuli/enterprise-console/internal/pagination"
)

func TestEffectiveEnvironment(t *testing.T) {
	groups := []Group{
		{ID: 1, Name: "All Nodes", IsRoot: true},
		{ID: 2, Name: "staging", ParentID: int64Ptr(1), Environment: strPtr("staging")},
		{ID: 3, Name: "web", ParentID: int64Ptr(2)},
		{ID: 4, Name: "web canary", ParentID: int64Ptr(3), Environment: strPtr("canary")},
		{ID: 5, Name: "db", ParentID: int64Ptr(1)},
		{ID: 6, Name: "blank", ParentID: int64Ptr(2), Environment: strPtr("")},
	}
	want := map[int64]string{1: "production", 2: "staging", 3: "staging", 4: "canary", 5: "production", 6: "staging"}
	for _, g := range groups {
		if got := EffectiveEnvironment(groups, g); got != want[g.ID] {
			t.Errorf("%s: environment = %q, want %q", g.Name, got, want[g.ID])
		}
	}
	// With the other groups unavailable only the group's own setting
	// can be seen.
	if got := EffectiveEnvironment(nil, groups[2]); got != "production" {
		t.Errorf("without the tree: %q, want production", got)
	}
}

// checkerFor flags class "gone" as missing in every environment, and
// records the environments it was asked about.
func checkerFor(asked *[]string, available bool) ClassChecker {
	return func(_ context.Context, environment string, classes []Class) ([]ClassWarning, bool) {
		*asked = append(*asked, environment)
		if !available {
			return nil, false
		}
		warnings := []ClassWarning{}
		for _, c := range classes {
			if c.Name == "gone" {
				warnings = append(warnings, ClassWarning{Kind: "class-missing", Class: c.Name})
			}
		}
		return warnings, true
	}
}

type classFields struct {
	Name             string         `json:"name"`
	Classes          []Class        `json:"classes"`
	ClassEnvironment string         `json:"classEnvironment"`
	ClassWarnings    []ClassWarning `json:"classWarnings"`
	ClassesChecked   bool           `json:"classesChecked"`
}

func classStore() *fakeStore {
	s := newFakeStore()
	s.groups[1] = Group{ID: 1, Name: "All Nodes", IsRoot: true}
	s.groups[2] = Group{ID: 2, Name: "staging", ParentID: int64Ptr(1), Priority: 1, Environment: strPtr("staging")}
	s.groups[3] = Group{ID: 3, Name: "web", ParentID: int64Ptr(2), Priority: 2,
		Classes: []Class{{Name: "nginx"}, {Name: "gone", Parameters: map[string]any{"a": 1}}}}
	s.groups[4] = Group{ID: 4, Name: "db", ParentID: int64Ptr(1), Priority: 3, Classes: []Class{{Name: "postgres"}}}
	return s
}

func getGroupFields(t *testing.T, h *Handlers, id string) (classFields, string) {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/groups/"+id, nil)
	req.SetPathValue("id", id)
	rec := httptest.NewRecorder()
	h.getGroup(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("get group %s: %d %s", id, rec.Code, rec.Body)
	}
	var got classFields
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	return got, rec.Body.String()
}

func TestGetGroup_ClassWarnings(t *testing.T) {
	var asked []string
	h := NewHandlers(classStore(), noopRecordActivity, noopRecordAudit, noopRecordAudit)
	h.SetClassChecker(checkerFor(&asked, true))

	got, _ := getGroupFields(t, h, "3")
	if got.ClassEnvironment != "staging" || !got.ClassesChecked {
		t.Errorf("environment %q checked %v; want the parent's staging, checked", got.ClassEnvironment, got.ClassesChecked)
	}
	if len(got.ClassWarnings) != 1 || got.ClassWarnings[0].Kind != "class-missing" || got.ClassWarnings[0].Class != "gone" {
		t.Errorf("warnings = %+v", got.ClassWarnings)
	}
	// The flagged class is still on the group, untouched.
	if len(got.Classes) != 2 || got.Classes[1].Name != "gone" || got.Classes[1].Parameters["a"] != float64(1) {
		t.Errorf("classes = %+v", got.Classes)
	}
	if len(asked) != 1 || asked[0] != "staging" {
		t.Errorf("checker asked about %v", asked)
	}

	got, _ = getGroupFields(t, h, "4")
	if got.ClassEnvironment != "production" || !got.ClassesChecked || len(got.ClassWarnings) != 0 {
		t.Errorf("db: %+v", got)
	}
}

func TestGetGroup_ClassesNotCheckedWhenDiscoveryIsUnavailable(t *testing.T) {
	var asked []string
	h := NewHandlers(classStore(), noopRecordActivity, noopRecordAudit, noopRecordAudit)
	h.SetClassChecker(checkerFor(&asked, false))
	got, body := getGroupFields(t, h, "3")
	if got.ClassesChecked || len(got.ClassWarnings) != 0 {
		t.Errorf("unavailable discovery flagged %+v (checked %v)", got.ClassWarnings, got.ClassesChecked)
	}
	// Present and empty, not absent: the page reads it unconditionally.
	if !strings.Contains(body, `"classWarnings":[]`) {
		t.Errorf("classWarnings should be an empty array: %s", body)
	}
}

func TestGetGroup_NoCheckerAndNoClasses(t *testing.T) {
	h := NewHandlers(classStore(), noopRecordActivity, noopRecordAudit, noopRecordAudit)
	got, body := getGroupFields(t, h, "3")
	if got.ClassesChecked || !strings.Contains(body, `"classWarnings":[]`) || got.ClassEnvironment != "staging" {
		t.Errorf("without a checker: %s", body)
	}

	// A group with no classes is not worth asking the server about.
	var asked []string
	h.SetClassChecker(checkerFor(&asked, true))
	getGroupFields(t, h, "2")
	if len(asked) != 0 {
		t.Errorf("a group with no classes was checked")
	}
}

func TestListGroups_ClassWarnings(t *testing.T) {
	for _, query := range []string{"?all=true", ""} {
		var asked []string
		h := NewHandlers(classStore(), noopRecordActivity, noopRecordAudit, noopRecordAudit)
		h.SetClassChecker(checkerFor(&asked, true))
		rec := httptest.NewRecorder()
		h.listGroups(rec, httptest.NewRequest(http.MethodGet, "/api/v1/groups"+query, nil))
		var page pagination.Page[classFields]
		if err := json.Unmarshal(rec.Body.Bytes(), &page); err != nil || rec.Code != 200 {
			t.Fatalf("list%s: %d %v", query, rec.Code, err)
		}
		flagged := map[string]int{}
		for _, g := range page.Items {
			flagged[g.Name] = len(g.ClassWarnings)
		}
		if flagged["web"] != 1 || flagged["db"] != 0 || flagged["staging"] != 0 || len(page.Items) != 4 {
			t.Errorf("list%s: flagged = %v", query, flagged)
		}
	}
}

// The list must not fail, or lose a group, because discovery does.
func TestListGroups_SurvivesUnavailableDiscovery(t *testing.T) {
	var asked []string
	h := NewHandlers(classStore(), noopRecordActivity, noopRecordAudit, noopRecordAudit)
	h.SetClassChecker(checkerFor(&asked, false))
	rec := httptest.NewRecorder()
	h.listGroups(rec, httptest.NewRequest(http.MethodGet, "/api/v1/groups?all=true", nil))
	var page pagination.Page[classFields]
	json.Unmarshal(rec.Body.Bytes(), &page)
	if rec.Code != 200 || len(page.Items) != 4 {
		t.Fatalf("list: %d with %d groups", rec.Code, len(page.Items))
	}
}

// Saving a group whose classes are flagged succeeds, and stores them
// as sent.
func TestUpdateGroup_SavesWithClassWarnings(t *testing.T) {
	var asked []string
	s := classStore()
	h := NewHandlers(s, noopRecordActivity, noopRecordAudit, noopRecordAudit)
	h.SetClassChecker(checkerFor(&asked, true))
	body := `{"name":"db","parentId":1,"priority":3,"classes":[{"name":"gone","parameters":{"x":"y"}}]}`
	req := httptest.NewRequest(http.MethodPut, "/api/v1/groups/4", strings.NewReader(body))
	req.SetPathValue("id", "4")
	rec := httptest.NewRecorder()
	h.updateGroup(rec, req)
	var got classFields
	json.Unmarshal(rec.Body.Bytes(), &got)
	if rec.Code != http.StatusOK || len(got.ClassWarnings) != 1 {
		t.Fatalf("update: %d %s", rec.Code, rec.Body)
	}
	if stored := s.groups[4].Classes; len(stored) != 1 || stored[0].Name != "gone" || stored[0].Parameters["x"] != "y" {
		t.Errorf("stored classes = %+v", stored)
	}
}
