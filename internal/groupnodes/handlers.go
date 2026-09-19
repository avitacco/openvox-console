// Package groupnodes exposes, for a given node group, the set of nodes
// currently matching its rule/pins - the "group -> nodes" direction
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
	"github.com/voxpupuli/enterprise-console/internal/openvoxdb"
)

// groupGetter is the subset of classifier.Store this package needs.
type groupGetter interface {
	GetGroup(ctx context.Context, id int64) (classifier.Group, error)
	ListAllGroups(ctx context.Context) ([]classifier.Group, error)
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
}

// NewHandlers builds Handlers backed by groups and nodes. recordAuditRead
// is bound to the classifier category - see design.md in the
// configurable-audit-logging change for why this is an injected function.
func NewHandlers(groups groupGetter, nodes inventory, recordAuditRead func(r *http.Request, event auditlog.Event)) *Handlers {
	return &Handlers{groups: groups, nodes: nodes, recordAuditRead: recordAuditRead}
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

// GroupCertnames returns the certnames currently matching the group, or
// classifier.ErrNotFound if no such group exists. One fleet-wide fact
// query, not one per node.
func (r *Resolver) GroupCertnames(ctx context.Context, id int64) ([]string, error) {
	group, err := r.groups.GetGroup(ctx, id)
	if err != nil {
		return nil, err
	}
	fleet, err := r.nodes.FleetFacts(ctx)
	if err != nil {
		return nil, err
	}
	certnames := make([]string, 0)
	for _, node := range fleet {
		if classifier.Matches(group, node.Certname, node.Facts) {
			certnames = append(certnames, node.Certname)
		}
	}
	return certnames, nil
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

	counts := make([]NodeCount, 0, len(groups))
	for _, g := range groups {
		matched := 0
		for _, node := range fleet {
			if classifier.Matches(g, node.Certname, node.Facts) {
				matched++
			}
		}
		counts = append(counts, NodeCount{GroupID: g.ID, Nodes: matched})
	}

	h.recordAuditRead(r, auditlog.Event{Action: "group.nodecounts.viewed", ResourceType: "group"})
	writeJSON(w, http.StatusOK, CountsResponse{Counts: counts})
}

// Response is the group-nodes endpoint's JSON shape.
type Response struct {
	Certnames []string `json:"certnames"`
}

func (h *Handlers) matchingNodes(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	ctx := r.Context()

	group, err := h.groups.GetGroup(ctx, id)
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
		facts, err := h.nodes.Facts(ctx, n.Certname)
		if err != nil {
			writeError(w, http.StatusBadGateway, err)
			return
		}
		if classifier.Matches(group, n.Certname, factsMap(facts)) {
			certnames = append(certnames, n.Certname)
		}
	}

	h.recordAuditRead(r, auditlog.Event{Action: "group.nodes.viewed", ResourceType: "group", ResourceID: group.Name})
	writeJSON(w, http.StatusOK, Response{Certnames: certnames})
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
