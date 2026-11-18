// Package groupnodes exposes, for a given node group, the set of nodes
// currently in it - matching its rule/pins and every one of its
// ancestors - the "group -> nodes" direction
// internal/classifier itself doesn't provide (it only resolves a single
// node's matching groups). Kept separate from internal/classifier for the
// same reason internal/encapi is: composing classifier with openvoxdb
// without either package importing the other.
package groupnodes

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/voxpupuli/enterprise-console/internal/auditlog"
	"github.com/voxpupuli/enterprise-console/internal/classifier"
	"github.com/voxpupuli/enterprise-console/internal/nodescope"
	"github.com/voxpupuli/enterprise-console/internal/openvoxdb"
)

// groupGetter is the subset of classifier.Store this package needs.
// Only the whole set: whether a node is in a group depends on the
// group's ancestors, so one group fetched alone cannot answer it.
type groupGetter interface {
	ListAllGroups(ctx context.Context) ([]classifier.Group, error)
}

// groupTree loads every group and arranges them, returning the group
// identified by id alongside, or classifier.ErrNotFound.
func groupTree(ctx context.Context, groups groupGetter, id int64) (*classifier.Tree, classifier.Group, error) {
	all, err := groups.ListAllGroups(ctx)
	if err != nil {
		return nil, classifier.Group{}, err
	}
	tree := classifier.NewTree(all)
	group, ok := tree.Group(id)
	if !ok {
		return nil, classifier.Group{}, classifier.ErrNotFound
	}
	return tree, group, nil
}

// inventory is the subset of *openvoxdb.Client this package needs.
type inventory interface {
	Nodes(ctx context.Context) ([]openvoxdb.Node, error)
	Facts(ctx context.Context, certname string) ([]openvoxdb.Fact, error)
	FleetFacts(ctx context.Context) ([]openvoxdb.NodeAllFacts, error)
}

// Handlers serves the group-nodes HTTP API.
type Handlers struct {
	groups          groupGetter
	nodes           inventory
	recordAuditRead func(r *http.Request, event auditlog.Event)
	scope           nodescope.Func
}

// SetScope makes the handlers show each caller only the nodes in their
// node group scope - see internal/nodescope.
func (h *Handlers) SetScope(scope nodescope.Func) { h.scope = scope }

// NewHandlers builds Handlers backed by groups and nodes. recordAuditRead
// is bound to the classifier category - see design.md in the
// configurable-audit-logging change for why this is an injected function.
func NewHandlers(groups groupGetter, nodes inventory, recordAuditRead func(r *http.Request, event auditlog.Event)) *Handlers {
	return &Handlers{groups: groups, nodes: nodes, recordAuditRead: recordAuditRead, scope: nodescope.Unscoped}
}

// Register wires this package's route onto mux, requiring classifier:read
// - resolving a group's matching nodes is a read operation on the group,
// the same permission the group's own detail view requires.
func (h *Handlers) Register(mux *http.ServeMux, authorize func(permission string, next http.HandlerFunc) http.HandlerFunc) {
	mux.HandleFunc("GET /api/v1/groups/{id}/nodes", authorize("classifier:read", h.matchingNodes))
	mux.HandleFunc("GET /api/v1/groups/node-counts", authorize("classifier:read", h.nodeCounts))
}

// Resolver answers "which nodes are in this group" for packages
// outside this one, so they can scope a query to a group without
// importing the classifier themselves.
type Resolver struct {
	groups groupGetter
	nodes  inventory
}

// NewResolver builds a Resolver over the same dependencies Handlers
// uses.
func NewResolver(groups groupGetter, nodes inventory) *Resolver {
	return &Resolver{groups: groups, nodes: nodes}
}

// GroupCertnames returns the certnames currently in the group - matching
// it and all of its ancestors - or classifier.ErrNotFound if no such
// group exists. One fleet-wide fact query, not one per node.
func (r *Resolver) GroupCertnames(ctx context.Context, id int64) ([]string, error) {
	tree, _, err := groupTree(ctx, r.groups, id)
	if err != nil {
		return nil, err
	}
	fleet, err := r.nodes.FleetFacts(ctx)
	if err != nil {
		return nil, err
	}
	certnames := make([]string, 0)
	for _, node := range fleet {
		if tree.Contains(id, node.Certname, node.Facts) {
			certnames = append(certnames, node.Certname)
		}
	}
	return certnames, nil
}

// NodesInGroups answers which nodes are in any of the groups identified
// by ids - and so, since a child group only ever holds nodes its parent
// holds, in any of their descendants too. It is the membership question
// behind node group scope (internal/nodescope).
//
// fleetWide is true when ids include the root group, which every node
// is in; nothing else is then looked up and certnames is nil. Ids that
// name no group contribute nothing, so a grant on a since-deleted group
// fails closed.
//
// Besides every node openvoxdb has facts for, pinned certnames are
// considered: a node pinned into a granted group is in scope before it
// has ever reported, provided the group's ancestors hold it too.
func (r *Resolver) NodesInGroups(ctx context.Context, ids []int64) (fleetWide bool, certnames []string, err error) {
	groups, err := r.groups.ListAllGroups(ctx)
	if err != nil {
		return false, nil, err
	}
	tree := classifier.NewTree(groups)

	granted := make([]int64, 0, len(ids))
	for _, id := range ids {
		group, ok := tree.Group(id)
		if !ok {
			continue
		}
		if group.IsRoot {
			return true, nil, nil
		}
		granted = append(granted, id)
	}
	certnames = make([]string, 0)
	if len(granted) == 0 {
		return false, certnames, nil
	}

	fleet, err := r.nodes.FleetFacts(ctx)
	if err != nil {
		return false, nil, err
	}
	seen := make(map[string]bool, len(fleet))
	consider := func(certname string, facts map[string]any) {
		if seen[certname] {
			return
		}
		seen[certname] = true
		for _, id := range granted {
			if tree.Contains(id, certname, facts) {
				certnames = append(certnames, certname)
				return
			}
		}
	}
	for _, node := range fleet {
		consider(node.Certname, node.Facts)
	}
	for _, group := range groups {
		for _, pinned := range group.Pins {
			consider(pinned, nil)
		}
	}
	return false, certnames, nil
}

// NodeCount is how many nodes currently match one group.
type NodeCount struct {
	GroupID int64 `json:"groupId"`
	Nodes   int   `json:"nodes"`
}

// CountsResponse is the node-counts endpoint's JSON shape.
type CountsResponse struct {
	Counts []NodeCount `json:"counts"`
}

// nodeCounts reports the matching-node total for every group at once.
//
// Deliberately not "call matchingNodes per group": that resolves one
// group by fetching each node's facts individually, so a page of 25
// groups across 1000 nodes would be 25,000 openvoxdb queries. Here the
// fleet's facts are fetched once and every group is matched against
// that snapshot in memory, which is two queries regardless of how many
// groups or nodes exist.
func (h *Handlers) nodeCounts(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	scope, err := h.scope(r)
	if err != nil {
		writeError(w, http.StatusBadGateway, err)
		return
	}

	groups, err := h.groups.ListAllGroups(ctx)
	if err != nil {
		writeError(w, http.StatusBadGateway, err)
		return
	}
	fleet, err := h.nodes.FleetFacts(ctx)
	if err != nil {
		writeError(w, http.StatusBadGateway, err)
		return
	}

	// One walk of the tree per node finds every group it is in, and
	// skips any subtree whose top it fails - so a child is never
	// counted for a node its parent does not match.
	//
	// Only nodes in the caller's scope are counted, so a count never
	// reveals how many nodes a caller cannot see.
	tree := classifier.NewTree(groups)
	matched := make(map[int64]int, len(groups))
	for _, node := range fleet {
		if !scope.Allows(node.Certname) {
			continue
		}
		for _, g := range tree.Matching(node.Certname, node.Facts) {
			matched[g.ID]++
		}
	}
	counts := make([]NodeCount, 0, len(groups))
	for _, g := range groups {
		counts = append(counts, NodeCount{GroupID: g.ID, Nodes: matched[g.ID]})
	}

	h.recordAuditRead(r, auditlog.Event{Action: "group.nodecounts.viewed", ResourceType: "group"})
	writeJSON(w, http.StatusOK, CountsResponse{Counts: counts})
}

// Response is the group-nodes endpoint's JSON shape.
type Response struct {
	Certnames []string `json:"certnames"`

	// PinsOutsideGroup lists the group's pinned nodes that are not
	// currently in it. A pin overrides the group's own rule but not
	// its ancestors, so a node pinned here that falls outside the
	// parent is not in the group - which would otherwise be silent.
	// Includes a pinned certname the inventory does not know at all.
	PinsOutsideGroup []string `json:"pinsOutsideGroup"`
}

func (h *Handlers) matchingNodes(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	ctx := r.Context()

	scope, err := h.scope(r)
	if err != nil {
		writeError(w, http.StatusBadGateway, err)
		return
	}

	tree, group, err := groupTree(ctx, h.groups, id)
	if err != nil {
		if errors.Is(err, classifier.ErrNotFound) {
			writeError(w, http.StatusNotFound, err)
			return
		}
		writeError(w, http.StatusBadGateway, err)
		return
	}

	nodes, err := h.nodes.Nodes(ctx)
	if err != nil {
		writeError(w, http.StatusBadGateway, err)
		return
	}

	certnames := make([]string, 0)
	for _, n := range nodes {
		// The caller's scope applies on top of the group: they see the
		// nodes that are in both.
		if !scope.Allows(n.Certname) {
			continue
		}
		facts, err := h.nodes.Facts(ctx, n.Certname)
		if err != nil {
			writeError(w, http.StatusBadGateway, err)
			return
		}
		if tree.Contains(id, n.Certname, factsMap(facts)) {
			certnames = append(certnames, n.Certname)
		}
	}

	inGroup := make(map[string]bool, len(certnames))
	for _, c := range certnames {
		inGroup[c] = true
	}
	outside := make([]string, 0)
	for _, pinned := range group.Pins {
		// A pin names a node; one outside the caller's scope is not
		// theirs to be told about.
		if !inGroup[pinned] && scope.Allows(pinned) {
			outside = append(outside, pinned)
		}
	}

	h.recordAuditRead(r, auditlog.Event{Action: "group.nodes.viewed", ResourceType: "group", ResourceID: group.Name})
	writeJSON(w, http.StatusOK, Response{Certnames: certnames, PinsOutsideGroup: outside})
}

func factsMap(facts []openvoxdb.Fact) map[string]any {
	m := make(map[string]any, len(facts))
	for _, f := range facts {
		var v any
		if err := json.Unmarshal(f.Value, &v); err == nil {
			m[f.Name] = v
		}
	}
	return m
}

func pathID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, errors.New("invalid group id"))
		return 0, false
	}
	return id, true
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, err error) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
}

// NodeEnvironment is one node and the environment classification
// resolves for it, nil when no matching group names one.
type NodeEnvironment struct {
	Certname    string
	Environment *string
}

// AssignedEnvironments resolves every node's effective classification
// environment in one pass.
//
// Effective, not per-group: the classifier merges every matching group
// by hierarchy and priority, so a node matching three groups that name
// different environments yields the single environment it would
// actually be sent to. Counting per matching group instead would count
// such a node several times and attribute it to environments it will
// never run in.
//
// Two queries regardless of fleet size, matching nodeCounts' approach:
// the groups once, the fleet's facts once, and all matching in memory.
func (r *Resolver) AssignedEnvironments(ctx context.Context) ([]NodeEnvironment, error) {
	groups, err := r.groups.ListAllGroups(ctx)
	if err != nil {
		return nil, err
	}
	fleet, err := r.nodes.FleetFacts(ctx)
	if err != nil {
		return nil, err
	}

	tree := classifier.NewTree(groups)
	assigned := make([]NodeEnvironment, 0, len(fleet))
	for _, node := range fleet {
		classification := tree.Classify(node.Certname, node.Facts)
		assigned = append(assigned, NodeEnvironment{
			Certname:    node.Certname,
			Environment: classification.Environment,
		})
	}
	return assigned, nil
}
