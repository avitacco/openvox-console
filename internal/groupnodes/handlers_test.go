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
	all    []classifier.Group
}

func (f *fakeGroups) GetGroup(_ context.Context, id int64) (classifier.Group, error) {
	g, ok := f.groups[id]
	if !ok {
		return classifier.Group{}, classifier.ErrNotFound
	}
	return g, nil
}

func (f *fakeGroups) ListAllGroups(context.Context) ([]classifier.Group, error) {
	return f.all, nil
}

type fakeInventory struct {
	nodes      []openvoxdb.Node
	facts      map[string][]openvoxdb.Fact
	fleetFacts []openvoxdb.NodeAllFacts
}

func (f *fakeInventory) Nodes(context.Context) ([]openvoxdb.Node, error) {
	return f.nodes, nil
}

func (f *fakeInventory) Facts(_ context.Context, certname string) ([]openvoxdb.Fact, error) {
	return f.facts[certname], nil
}

func (f *fakeInventory) FleetFacts(context.Context) ([]openvoxdb.NodeAllFacts, error) {
	return f.fleetFacts, nil
}

// TestNodeCounts_CountsPerGroup covers the counts endpoint: each group
// is matched against one fleet snapshot, pins count as members, and a
// group matching nothing still reports a zero rather than going absent
// (the page needs a number for every row).
func TestNodeCounts_CountsPerGroup(t *testing.T) {
	groups := &fakeGroups{all: []classifier.Group{
		{ID: 1, Name: "debian", Rule: []classifier.Condition{{FactPath: "osfamily", Operator: "=", Value: "Debian"}}},
		{ID: 2, Name: "redhat", Rule: []classifier.Condition{{FactPath: "osfamily", Operator: "=", Value: "RedHat"}}},
		{ID: 3, Name: "pinned-only", Pins: []string{"c.example.com"}},
	}}
	inv := &fakeInventory{fleetFacts: []openvoxdb.NodeAllFacts{
		{Certname: "a.example.com", Facts: map[string]any{"osfamily": "Debian"}},
		{Certname: "b.example.com", Facts: map[string]any{"osfamily": "Debian"}},
		{Certname: "c.example.com", Facts: map[string]any{"osfamily": "Windows"}},
	}}
	h := NewHandlers(groups, inv, noopRecordAuditRead)

	rec := httptest.NewRecorder()
	h.nodeCounts(rec, httptest.NewRequest(http.MethodGet, "/api/v1/groups/node-counts", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}

	var got CountsResponse
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	want := map[int64]int{1: 2, 2: 0, 3: 1}
	if len(got.Counts) != len(want) {
		t.Fatalf("got %d counts, want %d: %+v", len(got.Counts), len(want), got.Counts)
	}
	for _, c := range got.Counts {
		if w, ok := want[c.GroupID]; !ok || c.Nodes != w {
			t.Errorf("group %d counted %d nodes, want %d", c.GroupID, c.Nodes, want[c.GroupID])
		}
	}
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

func TestAssignedEnvironments_HighestPriorityGroupWins(t *testing.T) {
	// The requirement this exists for: a node matching several groups
	// that name different environments is counted once, against the
	// environment it would actually be sent to - not once per group.
	prod, staging, unused := "production", "team_a_staging", "never_wins"

	groups := []classifier.Group{
		{
			ID: 1, Name: "all-linux", Priority: 10, Environment: &unused,
			Rule: []classifier.Condition{{FactPath: "kernel", Operator: "=", Value: "Linux"}},
		},
		{
			ID: 2, Name: "webservers", Priority: 30, Environment: &prod,
			Rule: []classifier.Condition{{FactPath: "role", Operator: "=", Value: "web"}},
		},
		{
			ID: 3, Name: "team-a-owned", Priority: 20, Environment: &staging,
			Rule: []classifier.Condition{{FactPath: "team", Operator: "=", Value: "a"}},
		},
	}

	resolver := NewResolver(
		&fakeGroups{all: groups},
		&fakeInventory{fleetFacts: []openvoxdb.NodeAllFacts{
			// Matches all three; priority 30 (production) must win.
			{Certname: "web01", Facts: map[string]any{"kernel": "Linux", "role": "web", "team": "a"}},
			// Matches two; priority 20 (team_a_staging) wins.
			{Certname: "db01", Facts: map[string]any{"kernel": "Linux", "team": "a"}},
			// Matches only the lowest-priority group.
			{Certname: "misc01", Facts: map[string]any{"kernel": "Linux"}},
			// Matches nothing: no environment at all.
			{Certname: "win01", Facts: map[string]any{"kernel": "windows"}},
		}},
	)

	assigned, err := resolver.AssignedEnvironments(context.Background())
	if err != nil {
		t.Fatalf("AssignedEnvironments() error: %v", err)
	}
	if len(assigned) != 4 {
		t.Fatalf("got %d entries, want one per node (4)", len(assigned))
	}

	got := make(map[string]string, len(assigned))
	for _, node := range assigned {
		if node.Environment == nil {
			got[node.Certname] = "<none>"
			continue
		}
		got[node.Certname] = *node.Environment
	}

	for certname, want := range map[string]string{
		"web01":  prod,
		"db01":   staging,
		"misc01": unused,
		"win01":  "<none>",
	} {
		if got[certname] != want {
			t.Errorf("%s assigned %q, want %q", certname, got[certname], want)
		}
	}
}

func TestAssignedEnvironments_NoGroupsMeansNoEnvironments(t *testing.T) {
	resolver := NewResolver(
		&fakeGroups{},
		&fakeInventory{fleetFacts: []openvoxdb.NodeAllFacts{
			{Certname: "a", Facts: map[string]any{"kernel": "Linux"}},
		}},
	)

	assigned, err := resolver.AssignedEnvironments(context.Background())
	if err != nil {
		t.Fatalf("AssignedEnvironments() error: %v", err)
	}
	if len(assigned) != 1 || assigned[0].Environment != nil {
		t.Errorf("assigned = %+v, want one entry with no environment", assigned)
	}
}
