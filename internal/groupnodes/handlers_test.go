package groupnodes

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"testing"

	"github.com/voxpupuli/enterprise-console/internal/auditlog"
	"github.com/voxpupuli/enterprise-console/internal/classifier"
	"github.com/voxpupuli/enterprise-console/internal/nodescope"
	"github.com/voxpupuli/enterprise-console/internal/openvoxdb"
)

func noopRecordAuditRead(_ *http.Request, _ auditlog.Event) {}

// fakeGroups serves either all, or - for tests that describe their
// groups by id - the values of groups.
type fakeGroups struct {
	groups map[int64]classifier.Group
	all    []classifier.Group
}

func (f *fakeGroups) ListAllGroups(context.Context) ([]classifier.Group, error) {
	if f.all != nil {
		return f.all, nil
	}
	all := make([]classifier.Group, 0, len(f.groups))
	for _, g := range f.groups {
		all = append(all, g)
	}
	sort.Slice(all, func(i, j int) bool { return all[i].ID < all[j].ID })
	return all, nil
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

func idPtr(v int64) *int64 { return &v }

// hierarchyGroups is a root, a "linux" group beneath it, and a "web"
// group beneath that which pins two nodes: one inside linux, one not.
func hierarchyGroups() []classifier.Group {
	return []classifier.Group{
		{ID: 1, Name: "All Nodes", IsRoot: true},
		{
			ID: 2, Name: "linux", ParentID: idPtr(1), Priority: 10,
			Rule: []classifier.Condition{{FactPath: "kernel", Operator: "=", Value: "Linux"}},
		},
		{
			ID: 3, Name: "web", ParentID: idPtr(2), Priority: 20,
			Rule: []classifier.Condition{{FactPath: "role", Operator: "=", Value: "web"}},
			Pins: []string{"pinned-linux", "pinned-windows"},
		},
	}
}

func hierarchyFleet() []openvoxdb.NodeAllFacts {
	return []openvoxdb.NodeAllFacts{
		{Certname: "linux-web", Facts: map[string]any{"kernel": "Linux", "role": "web"}},
		{Certname: "linux-db", Facts: map[string]any{"kernel": "Linux", "role": "db"}},
		{Certname: "windows-web", Facts: map[string]any{"kernel": "windows", "role": "web"}},
		{Certname: "pinned-linux", Facts: map[string]any{"kernel": "Linux", "role": "db"}},
		{Certname: "pinned-windows", Facts: map[string]any{"kernel": "windows", "role": "db"}},
	}
}

func hierarchyInventory() *fakeInventory {
	inv := &fakeInventory{fleetFacts: hierarchyFleet(), facts: map[string][]openvoxdb.Fact{}}
	for _, n := range inv.fleetFacts {
		inv.nodes = append(inv.nodes, openvoxdb.Node{Certname: n.Certname})
		for name, value := range n.Facts {
			raw, _ := json.Marshal(value)
			inv.facts[n.Certname] = append(inv.facts[n.Certname], openvoxdb.Fact{Certname: n.Certname, Name: name, Value: raw})
		}
	}
	return inv
}

// A child's rule is satisfied by a node its parent does not match
// (windows-web); that node is not counted for the child, so the child's
// count can never exceed its parent's. The root counts the whole fleet.
func TestNodeCounts_FollowTheHierarchy(t *testing.T) {
	h := NewHandlers(&fakeGroups{all: hierarchyGroups()}, hierarchyInventory(), noopRecordAuditRead)

	rec := httptest.NewRecorder()
	h.nodeCounts(rec, httptest.NewRequest(http.MethodGet, "/api/v1/groups/node-counts", nil))

	var resp CountsResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	got := map[int64]int{}
	for _, c := range resp.Counts {
		got[c.GroupID] = c.Nodes
	}
	want := map[int64]int{
		1: 5, // every node in the fleet
		2: 3, // linux-web, linux-db, pinned-linux
		3: 2, // linux-web, pinned-linux - not windows-web, not pinned-windows
	}
	for id, n := range want {
		if got[id] != n {
			t.Errorf("group %d: count = %d, want %d", id, got[id], n)
		}
	}
	if got[3] > got[2] {
		t.Errorf("child count %d exceeds parent count %d", got[3], got[2])
	}
}

// What a job targeting the group resolves to: a node outside the parent
// is excluded even though it satisfies the group's own rule or is pinned.
func TestGroupCertnames_ExcludeNodesOutsideTheParent(t *testing.T) {
	r := NewResolver(&fakeGroups{all: hierarchyGroups()}, hierarchyInventory())

	got, err := r.GroupCertnames(context.Background(), 3)
	if err != nil {
		t.Fatalf("GroupCertnames() error: %v", err)
	}
	sort.Strings(got)
	want := []string{"linux-web", "pinned-linux"}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Errorf("GroupCertnames() = %v, want %v", got, want)
	}

	if _, err := r.GroupCertnames(context.Background(), 99); !errors.Is(err, classifier.ErrNotFound) {
		t.Errorf("GroupCertnames(unknown) error = %v, want ErrNotFound", err)
	}
}

func TestMatchingNodes_ReportsPinsOutsideTheParent(t *testing.T) {
	h := NewHandlers(&fakeGroups{all: hierarchyGroups()}, hierarchyInventory(), noopRecordAuditRead)

	rec, req := newRequest("3")
	h.matchingNodes(rec, req)

	var resp Response
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	sort.Strings(resp.Certnames)
	if len(resp.Certnames) != 2 || resp.Certnames[0] != "linux-web" || resp.Certnames[1] != "pinned-linux" {
		t.Errorf("Certnames = %v, want [linux-web pinned-linux]", resp.Certnames)
	}
	if len(resp.PinsOutsideGroup) != 1 || resp.PinsOutsideGroup[0] != "pinned-windows" {
		t.Errorf("PinsOutsideGroup = %v, want [pinned-windows]", resp.PinsOutsideGroup)
	}
}

func TestMatchingNodes_PinsAllInsideTheParentReportsNone(t *testing.T) {
	groups := hierarchyGroups()
	groups[2].Pins = []string{"pinned-linux"}
	h := NewHandlers(&fakeGroups{all: groups}, hierarchyInventory(), noopRecordAuditRead)

	rec, req := newRequest("3")
	h.matchingNodes(rec, req)

	// Empty, not null: the page tells "none outside" from "unknown" by it.
	if !strings.Contains(rec.Body.String(), `"pinsOutsideGroup":[]`) {
		t.Errorf("body = %s, want an empty pinsOutsideGroup list", rec.Body)
	}
}

type failingInventory struct{ fakeInventory }

func (f *failingInventory) Nodes(context.Context) ([]openvoxdb.Node, error) {
	return nil, errors.New("openvoxdb unreachable")
}

// With node data unavailable the endpoint fails rather than reporting
// every pin as outside the group; the page then shows the group with
// its pins unmarked and says their status is unavailable.
func TestMatchingNodes_NodeDataUnavailableIsAnErrorNotEmptyPins(t *testing.T) {
	h := NewHandlers(&fakeGroups{all: hierarchyGroups()}, &failingInventory{}, noopRecordAuditRead)

	rec, req := newRequest("3")
	h.matchingNodes(rec, req)

	if rec.Code != http.StatusBadGateway {
		t.Errorf("status = %d, want 502", rec.Code)
	}
	if strings.Contains(rec.Body.String(), "pinsOutsideGroup") {
		t.Errorf("body = %s, want no pin status when node data is unavailable", rec.Body)
	}
}

func scopedTo(certnames ...string) nodescope.Func {
	return func(*http.Request) (nodescope.Scope, error) { return nodescope.Of(certnames...), nil }
}

// A scope is the granted group and everything beneath it, a sibling is
// excluded, and the root is fleet-wide without looking anything up.
func TestNodesInGroups(t *testing.T) {
	groups := append(hierarchyGroups(), classifier.Group{
		ID: 4, Name: "windows", ParentID: idPtr(1), Priority: 30,
		Rule: []classifier.Condition{{FactPath: "kernel", Operator: "=", Value: "windows"}},
		Pins: []string{"never-reported"},
	})
	r := NewResolver(&fakeGroups{all: groups}, hierarchyInventory())

	resolve := func(ids ...int64) (bool, []string) {
		t.Helper()
		fleetWide, certnames, err := r.NodesInGroups(context.Background(), ids)
		if err != nil {
			t.Fatalf("NodesInGroups(%v) error: %v", ids, err)
		}
		sort.Strings(certnames)
		return fleetWide, certnames
	}
	same := func(got, want []string) bool {
		if len(got) != len(want) {
			return false
		}
		for i := range got {
			if got[i] != want[i] {
				return false
			}
		}
		return true
	}

	// linux: its own nodes and those of its child "web" (a subset).
	if fleetWide, got := resolve(2); fleetWide || !same(got, []string{"linux-db", "linux-web", "pinned-linux"}) {
		t.Errorf("linux = %v (fleetWide %v), want the three Linux nodes", got, fleetWide)
	}
	// web only: the sibling-by-rule windows-web and the pin outside the parent stay out.
	if _, got := resolve(3); !same(got, []string{"linux-web", "pinned-linux"}) {
		t.Errorf("web = %v, want [linux-web pinned-linux]", got)
	}
	// windows: includes a node pinned there that has never reported facts.
	if _, got := resolve(4); !same(got, []string{"never-reported", "pinned-windows", "windows-web"}) {
		t.Errorf("windows = %v, want its two nodes and the pinned never-reported one", got)
	}
	// union of two grants, each node once.
	if _, got := resolve(3, 4, 3); !same(got, []string{"linux-web", "never-reported", "pinned-linux", "pinned-windows", "windows-web"}) {
		t.Errorf("web+windows = %v", got)
	}
	// the root is fleet-wide.
	if fleetWide, got := resolve(3, 1); !fleetWide || got != nil {
		t.Errorf("with the root: fleetWide %v, certnames %v; want fleet-wide and nil", fleetWide, got)
	}
	// a deleted group grants nothing - an empty, non-nil list.
	fleetWide, got, err := r.NodesInGroups(context.Background(), []int64{99})
	if err != nil || fleetWide || got == nil || len(got) != 0 {
		t.Errorf("unknown group = %#v (fleetWide %v, err %v), want an empty list", got, fleetWide, err)
	}
}

func TestNodeCounts_OnlyCountNodesInScope(t *testing.T) {
	h := NewHandlers(&fakeGroups{all: hierarchyGroups()}, hierarchyInventory(), noopRecordAuditRead)
	h.SetScope(scopedTo("linux-web", "windows-web"))

	rec := httptest.NewRecorder()
	h.nodeCounts(rec, httptest.NewRequest(http.MethodGet, "/api/v1/groups/node-counts", nil))
	var resp CountsResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	got := map[int64]int{}
	for _, c := range resp.Counts {
		got[c.GroupID] = c.Nodes
	}
	// root: both in-scope nodes; linux and web: linux-web only.
	if want := map[int64]int{1: 2, 2: 1, 3: 1}; got[1] != want[1] || got[2] != want[2] || got[3] != want[3] {
		t.Errorf("counts = %v, want %v", got, want)
	}
}

func TestMatchingNodes_OnlyListNodesInScope(t *testing.T) {
	h := NewHandlers(&fakeGroups{all: hierarchyGroups()}, hierarchyInventory(), noopRecordAuditRead)
	h.SetScope(scopedTo("linux-web"))

	rec, req := newRequest("3")
	h.matchingNodes(rec, req)
	var resp Response
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Certnames) != 1 || resp.Certnames[0] != "linux-web" {
		t.Errorf("certnames = %v, want only linux-web (pinned-linux is in the group but not in scope)", resp.Certnames)
	}
	// pinned-windows is a pin outside the group; the caller cannot see
	// that node, so they are not told about it either.
	if len(resp.PinsOutsideGroup) != 0 {
		t.Errorf("pinsOutsideGroup = %v, want none for nodes outside scope", resp.PinsOutsideGroup)
	}
}

func TestHandlers_ScopeErrorFailsTheRequest(t *testing.T) {
	h := NewHandlers(&fakeGroups{all: hierarchyGroups()}, hierarchyInventory(), noopRecordAuditRead)
	h.SetScope(func(*http.Request) (nodescope.Scope, error) { return nodescope.Scope{}, errors.New("openvoxdb down") })

	rec := httptest.NewRecorder()
	h.nodeCounts(rec, httptest.NewRequest(http.MethodGet, "/api/v1/groups/node-counts", nil))
	if rec.Code != http.StatusBadGateway {
		t.Errorf("nodeCounts status = %d, want 502", rec.Code)
	}
	rec, req := newRequest("3")
	h.matchingNodes(rec, req)
	if rec.Code != http.StatusBadGateway {
		t.Errorf("matchingNodes status = %d, want 502", rec.Code)
	}
}

// A node that has never reported has no facts, so no rule matches it.
// It is in a team's scope only through a pin - which is how a scoped
// team signs the certificate of its own new node - and a pin counts
// only where every ancestor holds the node too.
func TestNodesInGroups_NodeWithNoFacts(t *testing.T) {
	groups := []classifier.Group{
		{ID: 1, Name: "All Nodes", IsRoot: true},
		{
			ID: 2, Name: "web", ParentID: idPtr(1), Priority: 10,
			Rule: []classifier.Condition{{FactPath: "role", Operator: "=", Value: "web"}},
			Pins: []string{"new-web"},
		},
		{
			ID: 3, Name: "web-eu", ParentID: idPtr(2), Priority: 20,
			Rule: []classifier.Condition{{FactPath: "region", Operator: "=", Value: "eu"}},
			Pins: []string{"new-web", "new-web-eu-only"},
		},
	}
	// Neither new node is in the inventory: no facts at all.
	r := NewResolver(&fakeGroups{all: groups}, &fakeInventory{})

	in := func(group int64) map[string]bool {
		_, certnames, err := r.NodesInGroups(context.Background(), []int64{group})
		if err != nil {
			t.Fatalf("NodesInGroups(%d) error: %v", group, err)
		}
		set := map[string]bool{}
		for _, c := range certnames {
			set[c] = true
		}
		return set
	}

	web := in(2)
	if !web["new-web"] {
		t.Error("a node pinned into a group directly under the root should be in that group's scope before it has facts")
	}
	if web["new-web-eu-only"] || web["unpinned-new-node"] {
		t.Errorf("web scope = %v; a node not pinned there must not be in it", web)
	}

	// Pinned in the child and in its parent: held all the way up.
	// Pinned in the child only: the parent's rule cannot match a node
	// with no facts, so the pin does not count.
	eu := in(3)
	if !eu["new-web"] || eu["new-web-eu-only"] {
		t.Errorf("web-eu scope = %v, want new-web only (pinned up the chain)", eu)
	}
}
