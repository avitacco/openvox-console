package groupnodes

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/voxpupuli/enterprise-console/internal/auditlog"
	"github.com/voxpupuli/enterprise-console/internal/classifier"
	"github.com/voxpupuli/enterprise-console/internal/openvoxdb"
)

func noopRecordAuditRead(_ *http.Request, _ auditlog.Event) {}

type fakeGroups struct {
	groups map[int64]classifier.Group
}

func (f *fakeGroups) GetGroup(_ context.Context, id int64) (classifier.Group, error) {
	g, ok := f.groups[id]
	if !ok {
		return classifier.Group{}, classifier.ErrNotFound
	}
	return g, nil
}

type fakeInventory struct {
	nodes []openvoxdb.Node
	facts map[string][]openvoxdb.Fact
}

func (f *fakeInventory) Nodes(context.Context) ([]openvoxdb.Node, error) {
	return f.nodes, nil
}

func (f *fakeInventory) Facts(_ context.Context, certname string) ([]openvoxdb.Fact, error) {
	return f.facts[certname], nil
}

func newRequest(id string) (*httptest.ResponseRecorder, *http.Request) {
	req := httptest.NewRequest(http.MethodGet, "/api/v1/groups/"+id+"/nodes", nil)
	req.SetPathValue("id", id)
	return httptest.NewRecorder(), req
}

func TestMatchingNodes_ResolvesGroupToMatchingCertnames(t *testing.T) {
	groups := &fakeGroups{groups: map[int64]classifier.Group{
		1: {
			ID:       1,
			Name:     "debian",
			Priority: 1,
			Rule:     []classifier.Condition{{FactPath: "osfamily", Operator: "=", Value: "Debian"}},
		},
	}}
	inv := &fakeInventory{
		nodes: []openvoxdb.Node{{Certname: "web01"}, {Certname: "web02"}},
		facts: map[string][]openvoxdb.Fact{
			"web01": {{Certname: "web01", Name: "osfamily", Value: json.RawMessage(`"Debian"`)}},
			"web02": {{Certname: "web02", Name: "osfamily", Value: json.RawMessage(`"RedHat"`)}},
		},
	}
	h := NewHandlers(groups, inv, noopRecordAuditRead)

	rec, req := newRequest("1")
	h.matchingNodes(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var resp Response
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(resp.Certnames) != 1 || resp.Certnames[0] != "web01" {
		t.Errorf("Certnames = %v, want [web01]", resp.Certnames)
	}
}

func TestMatchingNodes_PinnedNodeMatchesRegardlessOfFacts(t *testing.T) {
	groups := &fakeGroups{groups: map[int64]classifier.Group{
		1: {ID: 1, Name: "pinned", Priority: 1, Pins: []string{"web02"}},
	}}
	inv := &fakeInventory{
		nodes: []openvoxdb.Node{{Certname: "web01"}, {Certname: "web02"}},
		facts: map[string][]openvoxdb.Fact{},
	}
	h := NewHandlers(groups, inv, noopRecordAuditRead)

	rec, req := newRequest("1")
	h.matchingNodes(rec, req)

	var resp Response
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(resp.Certnames) != 1 || resp.Certnames[0] != "web02" {
		t.Errorf("Certnames = %v, want [web02]", resp.Certnames)
	}
}

func TestMatchingNodes_UnknownGroupReturns404(t *testing.T) {
	h := NewHandlers(&fakeGroups{groups: map[int64]classifier.Group{}}, &fakeInventory{}, noopRecordAuditRead)

	rec, req := newRequest("99")
	h.matchingNodes(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

func TestMatchingNodes_RecordsReadAuditEvent(t *testing.T) {
	groups := &fakeGroups{groups: map[int64]classifier.Group{
		1: {ID: 1, Name: "debian", Priority: 1, Rule: []classifier.Condition{{FactPath: "osfamily", Operator: "=", Value: "Debian"}}},
	}}
	inv := &fakeInventory{
		nodes: []openvoxdb.Node{{Certname: "web01"}},
		facts: map[string][]openvoxdb.Fact{"web01": {{Certname: "web01", Name: "osfamily", Value: json.RawMessage(`"Debian"`)}}},
	}
	var recorded []auditlog.Event
	h := NewHandlers(groups, inv, func(_ *http.Request, e auditlog.Event) { recorded = append(recorded, e) })

	rec, req := newRequest("1")
	h.matchingNodes(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if len(recorded) != 1 || recorded[0].Action != "group.nodes.viewed" || recorded[0].ResourceID != "debian" {
		t.Errorf("recorded = %+v, want exactly one group.nodes.viewed for debian", recorded)
	}
}

func TestMatchingNodes_NoMatchesReturnsEmptyNotNull(t *testing.T) {
	groups := &fakeGroups{groups: map[int64]classifier.Group{
		1: {ID: 1, Name: "empty", Priority: 1, Rule: []classifier.Condition{{FactPath: "osfamily", Operator: "=", Value: "Solaris"}}},
	}}
	inv := &fakeInventory{
		nodes: []openvoxdb.Node{{Certname: "web01"}},
		facts: map[string][]openvoxdb.Fact{
			"web01": {{Certname: "web01", Name: "osfamily", Value: json.RawMessage(`"Debian"`)}},
		},
	}
	h := NewHandlers(groups, inv, noopRecordAuditRead)

	rec, req := newRequest("1")
	h.matchingNodes(rec, req)

	var resp Response
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Certnames == nil {
		t.Error("Certnames = nil, want empty slice")
	}
	if len(resp.Certnames) != 0 {
		t.Errorf("Certnames = %v, want empty", resp.Certnames)
	}
}
