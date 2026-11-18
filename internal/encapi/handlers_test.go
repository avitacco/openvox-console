package encapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/voxpupuli/enterprise-console/internal/classifier"
	"github.com/voxpupuli/enterprise-console/internal/openvoxdb"
)

type fakeGroups struct {
	groups []classifier.Group
}

func (f *fakeGroups) ListAllGroups(context.Context) ([]classifier.Group, error) {
	return f.groups, nil
}

type fakeFacts struct {
	facts map[string][]openvoxdb.Fact
}

func (f *fakeFacts) Facts(_ context.Context, certname string) ([]openvoxdb.Fact, error) {
	return f.facts[certname], nil
}

func TestClassify_MatchingGroup(t *testing.T) {
	groups := &fakeGroups{groups: []classifier.Group{
		{
			Name:     "debian-ntp",
			Priority: 1,
			Rule:     []classifier.Condition{{FactPath: "osfamily", Operator: "=", Value: "Debian"}},
			Classes:  []classifier.Class{{Name: "ntp", Parameters: map[string]any{"server": "0.pool.ntp.org"}}},
		},
	}}
	facts := &fakeFacts{facts: map[string][]openvoxdb.Fact{
		"web01": {{Certname: "web01", Name: "osfamily", Value: json.RawMessage(`"Debian"`)}},
	}}
	h := NewHandlers(groups, facts)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/enc/web01", nil)
	req.SetPathValue("certname", "web01")
	rec := httptest.NewRecorder()
	h.classify(rec, req)

	var resp Response
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if _, ok := resp.Classes["ntp"]; !ok {
		t.Fatalf("Classes = %v, want ntp", resp.Classes)
	}
	if resp.Classes["ntp"]["server"] != "0.pool.ntp.org" {
		t.Errorf("ntp.server = %v", resp.Classes["ntp"]["server"])
	}
}

func TestClassify_NoMatchingGroupsReturnsEmptyNotError(t *testing.T) {
	groups := &fakeGroups{groups: []classifier.Group{
		{Name: "redhat-only", Priority: 1, Rule: []classifier.Condition{{FactPath: "osfamily", Operator: "=", Value: "RedHat"}}},
	}}
	facts := &fakeFacts{facts: map[string][]openvoxdb.Fact{
		"web01": {{Certname: "web01", Name: "osfamily", Value: json.RawMessage(`"Debian"`)}},
	}}
	h := NewHandlers(groups, facts)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/enc/web01", nil)
	req.SetPathValue("certname", "web01")
	rec := httptest.NewRecorder()
	h.classify(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var resp Response
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(resp.Classes) != 0 {
		t.Errorf("Classes = %v, want empty", resp.Classes)
	}
	if resp.Environment != nil {
		t.Errorf("Environment = %v, want nil", resp.Environment)
	}
}

func idPtr(v int64) *int64 { return &v }

func classifyFor(t *testing.T, groups []classifier.Group, certname string, facts map[string]any) (*httptest.ResponseRecorder, Response) {
	t.Helper()
	var rows []openvoxdb.Fact
	for name, value := range facts {
		raw, _ := json.Marshal(value)
		rows = append(rows, openvoxdb.Fact{Certname: certname, Name: name, Value: raw})
	}
	h := NewHandlers(&fakeGroups{groups: groups}, &fakeFacts{facts: map[string][]openvoxdb.Fact{certname: rows}})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/enc/"+certname, nil)
	req.SetPathValue("certname", certname)
	rec := httptest.NewRecorder()
	h.classify(rec, req)

	var resp Response
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	return rec, resp
}

// The node satisfies the child's rule but not its parent's, so the
// child's class must not reach it.
func TestClassify_ChildNarrowedOutByItsParent(t *testing.T) {
	groups := []classifier.Group{
		{ID: 1, Name: "All Nodes", IsRoot: true},
		{
			ID: 2, Name: "linux", ParentID: idPtr(1), Priority: 10,
			Rule:    []classifier.Condition{{FactPath: "kernel", Operator: "=", Value: "Linux"}},
			Classes: []classifier.Class{{Name: "profile::linux"}},
		},
		{
			ID: 3, Name: "web", ParentID: idPtr(2), Priority: 20,
			Rule:    []classifier.Condition{{FactPath: "role", Operator: "=", Value: "web"}},
			Classes: []classifier.Class{{Name: "profile::web"}},
		},
	}

	_, outside := classifyFor(t, groups, "win01", map[string]any{"kernel": "windows", "role": "web"})
	if len(outside.Classes) != 0 {
		t.Errorf("Classes = %v, want none for a node outside the parent", outside.Classes)
	}

	_, inside := classifyFor(t, groups, "web01", map[string]any{"kernel": "Linux", "role": "web"})
	if _, ok := inside.Classes["profile::web"]; !ok {
		t.Errorf("Classes = %v, want profile::web for a node inside the parent", inside.Classes)
	}
	if _, ok := inside.Classes["profile::linux"]; !ok {
		t.Errorf("Classes = %v, want the parent's profile::linux inherited", inside.Classes)
	}
}

// A child overriding a higher-priority parent is warned about on the
// groups pages. openvoxserver must see a normal classification: the
// child's value, and nothing but classes, parameters and environment.
func TestClassify_WarnedGroupGivesNormalResponseWithChildValue(t *testing.T) {
	groups := []classifier.Group{
		{ID: 1, Name: "All Nodes", IsRoot: true},
		{
			ID: 2, Name: "parent", ParentID: idPtr(1), Priority: 20, Pins: []string{"web01"},
			Classes: []classifier.Class{{Name: "ntp", Parameters: map[string]any{"server": "parent.example"}}},
		},
		{
			ID: 3, Name: "child", ParentID: idPtr(2), Priority: 10, Pins: []string{"web01"},
			Classes: []classifier.Class{{Name: "ntp", Parameters: map[string]any{"server": "child.example"}}},
		},
	}
	if len(classifier.Warnings(groups)) == 0 {
		t.Fatal("test setup: these groups should carry an override warning")
	}

	rec, resp := classifyFor(t, groups, "web01", map[string]any{})
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if got := resp.Classes["ntp"]["server"]; got != "child.example" {
		t.Errorf("ntp.server = %v, want the child's value", got)
	}

	var raw map[string]json.RawMessage
	if err := json.Unmarshal(rec.Body.Bytes(), &raw); err != nil {
		t.Fatalf("decode raw response: %v", err)
	}
	for key := range raw {
		switch key {
		case "classes", "parameters", "environment":
		default:
			t.Errorf("response carries unexpected key %q: %s", key, rec.Body)
		}
	}
}

func TestConfigData_MergedAcrossMatchingGroups(t *testing.T) {
	root := classifier.Group{ID: 1, Name: "All Nodes", IsRoot: true, ConfigData: map[string]any{"dns::timeout": 5.0, "root::only": "r"}}
	parent := classifier.Group{ID: 2, Name: "debian", ParentID: ptr(1), Priority: 10,
		Rule:       []classifier.Condition{{FactPath: "osfamily", Operator: "=", Value: "Debian"}},
		ConfigData: map[string]any{"dns::timeout": 2.0, "app::opts": map[string]any{"a": 1.0, "b": 2.0}}}
	other := classifier.Group{ID: 3, Name: "redhat", ParentID: ptr(1), Priority: 20,
		Rule:       []classifier.Condition{{FactPath: "osfamily", Operator: "=", Value: "RedHat"}},
		ConfigData: map[string]any{"never": "seen"}}
	h := NewHandlers(&fakeGroups{groups: []classifier.Group{root, parent, other}}, &fakeFacts{facts: map[string][]openvoxdb.Fact{
		"web01": {{Certname: "web01", Name: "osfamily", Value: json.RawMessage(`"Debian"`)}},
	}})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/enc/web01/data", nil)
	req.SetPathValue("certname", "web01")
	rec := httptest.NewRecorder()
	h.configData(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body)
	}
	var got map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	want := map[string]any{"dns::timeout": 2.0, "root::only": "r", "app::opts": map[string]any{"a": 1.0, "b": 2.0}}
	if len(got) != len(want) || got["dns::timeout"] != 2.0 || got["root::only"] != "r" {
		t.Errorf("data = %v, want %v (and nothing from the group the node does not match)", got, want)
	}
	if _, ok := got["never"]; ok {
		t.Error("data from a group the node does not match was served")
	}
}

func TestConfigData_NoDataIsAnEmptyObject(t *testing.T) {
	h := NewHandlers(&fakeGroups{groups: []classifier.Group{{ID: 1, Name: "All Nodes", IsRoot: true}}}, &fakeFacts{})
	req := httptest.NewRequest(http.MethodGet, "/api/v1/enc/web01/data", nil)
	req.SetPathValue("certname", "web01")
	rec := httptest.NewRecorder()
	h.configData(rec, req)
	if rec.Code != http.StatusOK || strings.TrimSpace(rec.Body.String()) != "{}" {
		t.Errorf("= %d %q, want 200 {}", rec.Code, rec.Body)
	}
}

func ptr(v int64) *int64 { return &v }
