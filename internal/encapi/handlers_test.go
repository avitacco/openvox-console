package encapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
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
