package stackstatus_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/voxpupuli/enterprise-console/internal/stackstatus"
)

// routed builds a routed instance reporting the given view.
func routed(id, mode string, peers, leaves int) stackstatus.Instance {
	return stackstatus.Instance{
		ID: id, Mode: mode, Health: stackstatus.HealthOK,
		Cluster: stackstatus.ClusterView{
			Attachment:      stackstatus.AttachmentRouted,
			RoutedPeers:     peers,
			LeafConnections: leaves,
		},
	}
}

func leafInstance(id, mode string) stackstatus.Instance {
	return stackstatus.Instance{
		ID: id, Mode: mode, Health: stackstatus.HealthOK,
		Cluster: stackstatus.ClusterView{Attachment: stackstatus.AttachmentLeaf},
	}
}

func standalone(id, mode string) stackstatus.Instance {
	return stackstatus.Instance{
		ID: id, Mode: mode, Health: stackstatus.HealthOK,
		Cluster: stackstatus.ClusterView{Attachment: stackstatus.AttachmentStandalone},
	}
}

func TestExpectedInstancesForASingleStandaloneInstance(t *testing.T) {
	got, ok := stackstatus.ExpectedInstances([]stackstatus.Instance{standalone("a", "all")})
	if !ok || got != 1 {
		t.Errorf("ExpectedInstances() = %d, %v; want 1, true", got, ok)
	}
}

// Three fully-meshed peers: each sees the other two.
func TestExpectedInstancesForAnAllRoutedCluster(t *testing.T) {
	got, ok := stackstatus.ExpectedInstances([]stackstatus.Instance{
		routed("a", "web", 2, 0),
		routed("b", "web", 2, 0),
		routed("c", "worker", 2, 0),
	})
	if !ok || got != 3 {
		t.Errorf("ExpectedInstances() = %d, %v; want 3, true", got, ok)
	}
}

// Leaves attached to more than one peer: the peers' leaf counts describe
// disjoint sets, so they sum. This is the case a single instance's own
// view gets wrong.
func TestExpectedInstancesCountsLeavesAcrossPeers(t *testing.T) {
	got, ok := stackstatus.ExpectedInstances([]stackstatus.Instance{
		routed("a", "web", 1, 1),
		routed("b", "web", 1, 2),
		leafInstance("l1", "enc"),
		leafInstance("l2", "enc"),
		leafInstance("l3", "enc"),
	})
	// 2 routed + 3 leaves.
	if !ok || got != 5 {
		t.Errorf("ExpectedInstances() = %d, %v; want 5, true", got, ok)
	}
}

// A routed instance that went silent is still counted, because the ones
// that replied can see it.
func TestExpectedInstancesCountsASilentRoutedPeer(t *testing.T) {
	got, ok := stackstatus.ExpectedInstances([]stackstatus.Instance{
		routed("a", "web", 2, 0),
		routed("b", "web", 2, 0),
	})
	if !ok || got != 3 {
		t.Errorf("ExpectedInstances() = %d, %v; want 3 (the silent peer is still visible to the others), true", got, ok)
	}
}

// Only leaves replied: a leaf cannot see the cluster, so the expected
// set is unknown and the caller must treat the picture as incomplete.
func TestExpectedInstancesIsUnknownWhenOnlyLeavesReply(t *testing.T) {
	if got, ok := stackstatus.ExpectedInstances([]stackstatus.Instance{
		leafInstance("l1", "enc"),
		leafInstance("l2", "enc"),
	}); ok {
		t.Errorf("ExpectedInstances() = %d, true; want unknown when only leaves replied", got)
	}
}

type fakeAsker struct {
	replies []stackstatus.Instance
	err     error
}

func (f fakeAsker) AskAll(context.Context, string, []byte, time.Duration) ([][]byte, error) {
	if f.err != nil {
		return nil, f.err
	}
	out := make([][]byte, 0, len(f.replies))
	for _, inst := range f.replies {
		data, _ := json.Marshal(inst)
		out = append(out, data)
	}
	return out, nil
}

func TestAggregateGroupsByModeWithCounts(t *testing.T) {
	self := routed("a", "web", 2, 0)
	stack := stackstatus.Aggregate(context.Background(), fakeAsker{replies: []stackstatus.Instance{
		self, routed("b", "web", 2, 0), routed("c", "worker", 2, 0),
	}}, self)

	if len(stack.Modes) != 2 {
		t.Fatalf("Modes = %+v, want two groups", stack.Modes)
	}
	byMode := map[string]int{}
	for _, g := range stack.Modes {
		byMode[g.Mode] = g.Count
		if len(g.Instances) != g.Count {
			t.Errorf("group %q: Count = %d but %d instances", g.Mode, g.Count, len(g.Instances))
		}
	}
	if byMode["web"] != 2 || byMode["worker"] != 1 {
		t.Errorf("counts = %v, want web=2 worker=1", byMode)
	}
	if stack.Incomplete {
		t.Errorf("complete result marked incomplete: %s", stack.IncompleteReason)
	}
}

func TestAggregateMarksIncompleteWhenAnInstanceIsSilent(t *testing.T) {
	self := routed("a", "web", 2, 0)
	// Three peers exist (each sees 2), only two replied.
	stack := stackstatus.Aggregate(context.Background(), fakeAsker{replies: []stackstatus.Instance{
		self, routed("b", "web", 2, 0),
	}}, self)

	if !stack.Incomplete {
		t.Error("a missing instance did not mark the result incomplete")
	}
	if stack.Expected != 3 || stack.Replied != 2 {
		t.Errorf("Expected/Replied = %d/%d, want 3/2", stack.Expected, stack.Replied)
	}
	if stack.IncompleteReason == "" {
		t.Error("incomplete result carries no reason")
	}
}

// The case option 1 exists for: a leaf attached to another peer goes
// silent, and the serving instance cannot see it directly.
func TestAggregateDetectsASilentLeafAttachedElsewhere(t *testing.T) {
	// self sees one peer and no leaves of its own; the peer reports one
	// leaf attached to it. That leaf does not reply.
	self := routed("a", "web", 1, 0)
	stack := stackstatus.Aggregate(context.Background(), fakeAsker{replies: []stackstatus.Instance{
		self, routed("b", "web", 1, 1),
	}}, self)

	if stack.Expected != 3 {
		t.Errorf("Expected = %d, want 3 (two peers plus the leaf the peer reported)", stack.Expected)
	}
	if !stack.Incomplete {
		t.Error("a silent leaf attached to another peer was not detected; the serving instance cannot see it directly, which is exactly why views are combined")
	}
}

// ...and when that leaf does reply, the picture is complete.
func TestAggregateIsCompleteWhenTheLeafReplies(t *testing.T) {
	self := routed("a", "web", 1, 0)
	stack := stackstatus.Aggregate(context.Background(), fakeAsker{replies: []stackstatus.Instance{
		self, routed("b", "web", 1, 1), leafInstance("l1", "enc"),
	}}, self)

	if stack.Incomplete {
		t.Errorf("complete result marked incomplete: %s", stack.IncompleteReason)
	}
	if stack.Expected != 3 || stack.Replied != 3 {
		t.Errorf("Expected/Replied = %d/%d, want 3/3", stack.Expected, stack.Replied)
	}
}

func TestAggregateReportsSelfWhenTheBusIsUnreachable(t *testing.T) {
	self := routed("a", "web", 2, 0)
	stack := stackstatus.Aggregate(context.Background(),
		fakeAsker{err: errors.New("no connection")}, self)

	if !stack.Incomplete {
		t.Error("an unreachable bus did not mark the result incomplete")
	}
	if stack.Replied != 1 {
		t.Errorf("Replied = %d, want 1 (the serving instance itself)", stack.Replied)
	}
	if len(stack.Modes) != 1 || stack.Modes[0].Instances[0].ID != "a" {
		t.Errorf("the serving instance is not reported: %+v", stack.Modes)
	}
}

func TestAggregateIsCompleteForASingleStandaloneInstance(t *testing.T) {
	self := standalone("a", "all")
	stack := stackstatus.Aggregate(context.Background(),
		fakeAsker{replies: []stackstatus.Instance{self}}, self)

	if stack.Incomplete {
		t.Errorf("a lone instance was marked incomplete: %s", stack.IncompleteReason)
	}
	if stack.Replied != 1 || stack.Expected != 1 {
		t.Errorf("Expected/Replied = %d/%d, want 1/1", stack.Expected, stack.Replied)
	}
}

// Instances disagreeing about a dependency is surfaced, not smoothed
// over - it is the partial-connectivity signal the page exists to show.
func TestAggregateSurfacesDependencyDisagreement(t *testing.T) {
	self := routed("a", "web", 1, 0)
	self.Dependencies = []stackstatus.Dependency{{Name: "openvoxdb", Health: stackstatus.HealthOK}}
	peer := routed("b", "web", 1, 0)
	peer.Dependencies = []stackstatus.Dependency{{Name: "openvoxdb", Health: stackstatus.HealthUnreachable}}

	stack := stackstatus.Aggregate(context.Background(),
		fakeAsker{replies: []stackstatus.Instance{self, peer}}, self)

	if len(stack.Disagreements) != 1 || stack.Disagreements[0] != "openvoxdb" {
		t.Errorf("Disagreements = %v, want [openvoxdb]", stack.Disagreements)
	}
}

// The incomplete flag must always be present in the encoding: a consumer
// must not be able to miss it because the field was omitted.
func TestIncompleteIsAlwaysEncoded(t *testing.T) {
	data, err := json.Marshal(stackstatus.Stack{})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if _, ok := decoded["incomplete"]; !ok {
		t.Error("the incomplete flag is omitted when false; a consumer could miss it entirely")
	}
}
