package stackstatus

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"time"

	"github.com/nats-io/nats.go"
)

// Subject is where a status request is published and every instance
// answers.
const Subject = "console.status.request"

// Window bounds how long the aggregator waits for replies.
//
// Fixed in code rather than configurable: it bounds a human-facing page
// load, and a knob would invite tuning it upward to paper over an
// instance that is genuinely unwell - turning a visible problem into a
// slow page.
const Window = time.Second

// asker is the subset of *messaging.Bus the aggregator needs.
type asker interface {
	AskAll(ctx context.Context, subject string, data []byte, window time.Duration) ([][]byte, error)
}

// responderBus is the subset of *messaging.Bus the responder needs.
type responderBus interface {
	Subscribe(subject string, handler nats.MsgHandler) (*nats.Subscription, error)
}

// ModeGroup is the instances running one mode.
type ModeGroup struct {
	Mode      string     `json:"mode"`
	Count     int        `json:"count"`
	Instances []Instance `json:"instances"`
}

// Stack is the aggregated answer.
type Stack struct {
	Modes []ModeGroup `json:"modes"`

	// Dependencies are as the serving instance sees them.
	Dependencies []Dependency `json:"dependencies"`

	// Disagreements names any dependency that instances do not agree
	// about - the partial-connectivity case a central check would hide.
	Disagreements []string `json:"disagreements,omitempty"`

	// Expected is how many instances the combined cluster views say
	// should have answered.
	Expected int `json:"expected"`

	// Replied is how many actually did.
	Replied int `json:"replied"`

	// Incomplete reports that this picture may be missing an instance.
	// Never omitted from the encoding: a consumer must not be able to
	// miss it by the field being absent.
	Incomplete bool `json:"incomplete"`

	// IncompleteReason says why, for an operator who needs to know
	// whether to trust what they are looking at.
	IncompleteReason string `json:"incompleteReason,omitempty"`
}

// Respond subscribes to status requests and answers each with this
// instance's own report.
//
// A plain Subscribe, not a queue group: every instance must answer, which
// is the opposite of the queue-group rule that governs subscribers with
// side effects. Answering a question is not a side effect.
func Respond(bus responderBus, reporter *Reporter) (*nats.Subscription, error) {
	return bus.Subscribe(Subject, func(msg *nats.Msg) {
		report := reporter.Report(context.Background())
		data, err := json.Marshal(report)
		if err != nil {
			return
		}
		_ = msg.Respond(data)
	})
}

// Aggregate asks every instance for its status and assembles the answer.
//
// The serving instance's own report is passed in rather than gathered,
// so the result is never empty even when the bus is unreachable: an
// operator looking at a broken cluster still learns about the instance
// they reached.
func Aggregate(ctx context.Context, bus asker, self Instance) Stack {
	replies, err := bus.AskAll(ctx, Subject, nil, Window)
	if err != nil {
		return assemble([]Instance{self}, self, fmt.Sprintf("the event bus could not be reached: %v", err))
	}

	instances := make([]Instance, 0, len(replies)+1)
	seen := map[string]bool{}
	for _, raw := range replies {
		var inst Instance
		if err := json.Unmarshal(raw, &inst); err != nil {
			// A malformed reply is a reply we cannot account for, so
			// it is not silently dropped - it is simply not added, and
			// the expected/replied comparison below notices.
			continue
		}
		if seen[inst.ID] {
			continue
		}
		seen[inst.ID] = true
		instances = append(instances, inst)
	}

	// The serving instance answers its own request over the in-process
	// bus, so it is normally already here; add it if it is not.
	if !seen[self.ID] {
		instances = append(instances, self)
	}

	return assemble(instances, self, "")
}

// assemble groups instances by mode, works out the expected count, and
// decides whether the picture is complete.
func assemble(instances []Instance, self Instance, busErr string) Stack {
	stack := Stack{
		Dependencies: self.Dependencies,
		Replied:      len(instances),
	}

	byMode := map[string][]Instance{}
	for _, inst := range instances {
		byMode[inst.Mode] = append(byMode[inst.Mode], inst)
	}
	modes := make([]string, 0, len(byMode))
	for mode := range byMode {
		modes = append(modes, mode)
	}
	sort.Strings(modes)

	for _, mode := range modes {
		group := byMode[mode]
		sort.Slice(group, func(i, j int) bool { return group[i].ID < group[j].ID })
		stack.Modes = append(stack.Modes, ModeGroup{Mode: mode, Count: len(group), Instances: group})
	}

	stack.Disagreements = disagreements(instances)

	if busErr != "" {
		stack.Incomplete = true
		stack.IncompleteReason = busErr
		stack.Expected = 0
		return stack
	}

	expected, ok := ExpectedInstances(instances)
	stack.Expected = expected
	switch {
	case !ok:
		stack.Incomplete = true
		stack.IncompleteReason = "no instance reported a usable cluster view, so the expected number of instances is unknown"
	case len(instances) < expected:
		stack.Incomplete = true
		stack.IncompleteReason = fmt.Sprintf("%d of %d instances did not reply within %s", expected-len(instances), expected, Window)
	}
	return stack
}

// ExpectedInstances works out how many instances should have answered,
// by combining the cluster views of the ones that did.
//
// The serving instance's own view is not enough. NATS makes a server's
// routed peers visible to it, but a leaf connection is visible only to
// the peer it attached to - and enc instances attach as leaves by
// design. A status request served by one peer would otherwise have no
// idea a leaf attached to another peer exists, and would report a
// complete picture with that instance missing.
//
// Two facts make the combination sound:
//
//   - Routed instances are fully meshed, so every routed instance sees
//     every other one. The largest reported peer count plus one is the
//     number of routed instances - and it still counts a routed instance
//     that went silent, because the ones that replied can still see it.
//   - A leaf attaches to exactly one peer, so the leaf counts reported
//     by different peers describe disjoint sets and are summed.
//
// The result is that an instance is accounted for whenever *any*
// replying instance can see it. The one apparent blind spot - a leaf
// whose peer also went silent - is already covered, because that silent
// peer is itself missing from the expected count.
//
// ok is false when no reply carried a usable view, in which case the
// caller must treat the picture as incomplete rather than assuming what
// it holds is everything.
func ExpectedInstances(instances []Instance) (expected int, ok bool) {
	maxRoutedPeers := -1
	leaves := 0
	sawRouted := false
	sawAny := false

	for _, inst := range instances {
		switch inst.Cluster.Attachment {
		case AttachmentStandalone:
			sawAny = true
			// A standalone instance is the whole cluster it knows of.
			if maxRoutedPeers < 0 {
				maxRoutedPeers = 0
			}
		case AttachmentRouted:
			sawAny = true
			sawRouted = true
			if inst.Cluster.RoutedPeers > maxRoutedPeers {
				maxRoutedPeers = inst.Cluster.RoutedPeers
			}
			leaves += inst.Cluster.LeafConnections
		case AttachmentLeaf:
			sawAny = true
			// A leaf sees only the peer it attached to, so it
			// contributes nothing to the count. It is counted by that
			// peer's LeafConnections.
		}
	}

	if !sawAny || maxRoutedPeers < 0 {
		// Only leaves replied: they cannot see the cluster, so the
		// expected set is unknown.
		return 0, false
	}
	if !sawRouted {
		// A standalone instance: itself, and nothing else.
		return 1, true
	}
	return 1 + maxRoutedPeers + leaves, true
}

// disagreements names dependencies that instances report differently -
// the partial-connectivity case that a single central check would hide.
func disagreements(instances []Instance) []string {
	seen := map[string]map[Health]bool{}
	for _, inst := range instances {
		for _, dep := range inst.Dependencies {
			if seen[dep.Name] == nil {
				seen[dep.Name] = map[Health]bool{}
			}
			seen[dep.Name][dep.Health] = true
		}
	}

	var names []string
	for name, healths := range seen {
		if len(healths) > 1 {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	return names
}
