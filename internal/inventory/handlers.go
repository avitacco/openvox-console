// Package inventory implements the node inventory view: listing nodes
// with their status and last check-in time, showing a single node's
// facts, and search/filtering - backed by openvoxdb-client.
package inventory

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/voxpupuli/enterprise-console/internal/auditlog"
	"github.com/voxpupuli/enterprise-console/internal/certstatus"
	"github.com/voxpupuli/enterprise-console/internal/openvoxdb"
	"github.com/voxpupuli/enterprise-console/internal/pagination"
)

// queryClient is the subset of openvoxdb.Client that this package needs,
// so handlers can be tested against a fake without a live openvoxdb.
type queryClient interface {
	Nodes(ctx context.Context) ([]openvoxdb.Node, error)
	Facts(ctx context.Context, certname string) ([]openvoxdb.Fact, error)
	FactCertnames(ctx context.Context, name, value string) ([]string, error)
	NodeByCertname(ctx context.Context, certname string) (*openvoxdb.Node, error)
	DeactivateNode(ctx context.Context, certname string) error
}

// certCleaner is the subset of *certstatus.Client (already used this
// way by internal/nodeconnectivity, whose CertStatusClient this
// structurally satisfies too) that node deletion needs. Deleting a node
// also cleans its CA certificate record - otherwise it keeps appearing
// on the node list, since the frontend's node list is a union of
// openvoxdb's inventory and the separate connectivity/CA registry, not
// openvoxdb's inventory alone (see design.md in add-node-deletion for
// the live finding that led to this). May be nil, matching
// nodeconnectivity's own pattern, when no CA client is configured
// (CONSOLE_CA_CLIENT_URL unset) - deletion then only deactivates the
// openvoxdb record, and the node may keep appearing until its cert is
// cleaned some other way.
type certCleaner interface {
	Clean(ctx context.Context, certname string) error
}

// Handlers serves the inventory HTTP API.
type Handlers struct {
	client          queryClient
	certClient      certCleaner
	recordAudit     func(r *http.Request, event auditlog.Event)
	recordAuditRead func(r *http.Request, event auditlog.Event)
}

// NewHandlers builds Handlers backed by client and certClient (may be
// nil - see certCleaner's doc comment). recordAudit and recordAuditRead
// are both bound to the nodes category (configurable-audit-logging),
// matching the recordAudit/recordAuditRead split
// internal/nodeconnectivity already uses for its own mix of read and
// write actions.
func NewHandlers(client queryClient, certClient certCleaner, recordAudit, recordAuditRead func(r *http.Request, event auditlog.Event)) *Handlers {
	return &Handlers{client: client, certClient: certClient, recordAudit: recordAudit, recordAuditRead: recordAuditRead}
}

// Register wires this package's routes onto mux. Every route requires
// nodes:read except node deletion, which requires nodes:manage - a
// separate permission from nodes:read since deleting a node is a
// destructive action a read-only role should not be able to take (see
// design.md in add-node-deletion for why this is a new permission
// rather than reusing nodes:certs:manage).
func (h *Handlers) Register(mux *http.ServeMux, authorize func(permission string, next http.HandlerFunc) http.HandlerFunc) {
	mux.HandleFunc("GET /api/v1/nodes", authorize("nodes:read", h.listNodes))
	mux.HandleFunc("GET /api/v1/nodes/summary", authorize("nodes:read", h.nodesSummary))
	mux.HandleFunc("GET /api/v1/nodes/{name}", authorize("nodes:read", h.getNode))
	mux.HandleFunc("DELETE /api/v1/nodes/{name}", authorize("nodes:manage", h.deleteNode))
}

type nodeSummary struct {
	Certname        string  `json:"certname"`
	Status          *string `json:"status"`
	ReportTimestamp *string `json:"reportTimestamp"`
}

func toNodeSummary(n openvoxdb.Node) nodeSummary {
	summary := nodeSummary{Certname: n.Certname, Status: n.LatestReportStatus}
	if n.ReportTimestamp != nil {
		ts := n.ReportTimestamp.Format(time.RFC3339)
		summary.ReportTimestamp = &ts
	}
	return summary
}

func (h *Handlers) listNodes(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	nodes, err := h.client.Nodes(ctx)
	if err != nil {
		writeError(w, err)
		return
	}

	factName := r.URL.Query().Get("fact")
	factValue := r.URL.Query().Get("value")
	if factName != "" && factValue != "" {
		certnames, err := h.client.FactCertnames(ctx, factName, factValue)
		if err != nil {
			writeError(w, err)
			return
		}
		allowed := make(map[string]bool, len(certnames))
		for _, c := range certnames {
			allowed[c] = true
		}
		nodes = filterNodes(nodes, func(n openvoxdb.Node) bool { return allowed[n.Certname] })
	}

	if name := r.URL.Query().Get("name"); name != "" {
		nodes = filterNodes(nodes, func(n openvoxdb.Node) bool {
			return strings.Contains(strings.ToLower(n.Certname), strings.ToLower(name))
		})
	}

	// openvoxdb has no way to push name/fact filtering into the query
	// itself (see queryClient), so filtering already happens above on
	// the full result set; pagination here only trims the HTTP response,
	// it doesn't reduce the openvoxdb query cost. Revisit if that
	// matters at scale (see design.md's PQL rationale).
	page, pageSize := pagination.Params(r)
	total := len(nodes)
	start := min((page-1)*pageSize, total)
	end := min(start+pageSize, total)

	summaries := make([]nodeSummary, 0, end-start)
	for _, n := range nodes[start:end] {
		summaries = append(summaries, toNodeSummary(n))
	}
	h.recordAuditRead(r, auditlog.Event{Action: "node.list.viewed", ResourceType: "node"})
	writeJSON(w, pagination.New(summaries, page, pageSize, total))
}

// summary is a fleet-wide count of nodes by their latest report status -
// backs the dashboard's status cards. ByStatus always has exactly four
// keys - "failed", "corrected", "intentional", "unchanged" - regardless
// of whether any node currently falls into a given one (each starts at
// 0, not omitted). A node whose latest report is "noop", or that has no
// report at all, isn't counted under any of the four - see design.md in
// add-dashboard-fleet-status-stats for why. Total counts every known
// node regardless, so it does NOT generally equal the sum of ByStatus's
// values when a node is excluded.
type summary struct {
	Total    int            `json:"total"`
	ByStatus map[string]int `json:"byStatus"`
}

// classifyForSummary maps a node's latest report status and corrective-
// change flag to one of the four fleet-status-summary categories, or ""
// if the node is excluded (a "noop" latest report, or none at all).
// "changed" splits on corrective-change: true means Puppet corrected
// unexpected drift ("corrected"); false or nil (including when
// corrective-change tracking isn't enabled for that node, so no
// corrective change can be confirmed) means "intentional" - see
// design.md for why nil deliberately isn't its own bucket.
func classifyForSummary(n openvoxdb.Node) string {
	if n.LatestReportStatus == nil {
		return ""
	}
	switch *n.LatestReportStatus {
	case "failed":
		return "failed"
	case "unchanged":
		return "unchanged"
	case "changed":
		if n.LatestReportCorrectiveChange != nil && *n.LatestReportCorrectiveChange {
			return "corrected"
		}
		return "intentional"
	default:
		return ""
	}
}

func (h *Handlers) nodesSummary(w http.ResponseWriter, r *http.Request) {
	nodes, err := h.client.Nodes(r.Context())
	if err != nil {
		writeError(w, err)
		return
	}

	s := summary{Total: len(nodes), ByStatus: map[string]int{
		"failed": 0, "corrected": 0, "intentional": 0, "unchanged": 0,
	}}
	for _, n := range nodes {
		if category := classifyForSummary(n); category != "" {
			s.ByStatus[category]++
		}
	}
	h.recordAuditRead(r, auditlog.Event{Action: "node.summary.viewed", ResourceType: "node"})
	writeJSON(w, s)
}

func filterNodes(nodes []openvoxdb.Node, keep func(openvoxdb.Node) bool) []openvoxdb.Node {
	filtered := make([]openvoxdb.Node, 0, len(nodes))
	for _, n := range nodes {
		if keep(n) {
			filtered = append(filtered, n)
		}
	}
	return filtered
}

type nodeDetail struct {
	Certname string                     `json:"certname"`
	Facts    map[string]json.RawMessage `json:"facts"`
}

func (h *Handlers) getNode(w http.ResponseWriter, r *http.Request) {
	certname := r.PathValue("name")

	facts, err := h.client.Facts(r.Context(), certname)
	if err != nil {
		writeError(w, err)
		return
	}

	detail := nodeDetail{Certname: certname, Facts: make(map[string]json.RawMessage, len(facts))}
	for _, f := range facts {
		detail.Facts[f.Name] = f.Value
	}
	h.recordAuditRead(r, auditlog.Event{Action: "node.viewed", ResourceType: "node", ResourceID: certname})
	writeJSON(w, detail)
}

// deleteNode deactivates certname in openvoxdb. openvoxdb's own
// "deactivate node" command does not error for an unknown certname -
// confirmed live (see design.md): it silently creates a new, already-
// deactivated stub node record instead of rejecting the request. So
// "unknown node" has to be enforced here first. The existence check
// uses NodeByCertname (openvoxdb's single-node lookup route), not
// Nodes' collection query - confirmed live that the collection query
// unconditionally excludes deactivated nodes with no override, so it
// can't tell "unknown" apart from "known but already deactivated" (see
// design.md), while the single-node route correctly returns both an
// active and an already-deactivated node, 404ing only for a truly
// unknown certname.
func (h *Handlers) deleteNode(w http.ResponseWriter, r *http.Request) {
	certname := r.PathValue("name")
	ctx := r.Context()

	if _, err := h.client.NodeByCertname(ctx, certname); err != nil {
		if errors.Is(err, openvoxdb.ErrNodeNotFound) {
			writeNotFound(w, "node not found: "+certname)
			return
		}
		writeError(w, err)
		return
	}

	if err := h.client.DeactivateNode(ctx, certname); err != nil {
		writeError(w, err)
		return
	}

	// Also cleans the CA certificate record, not just the openvoxdb
	// inventory record - otherwise the node keeps appearing on the node
	// list via its connectivity/CA registry entry, confirmed live (see
	// design.md). A *certstatus.NotFoundError (nothing to clean - the
	// node never had a CA record) is not a failure; any other error is,
	// since the whole point of combining these two actions is that
	// "deleted" means actually gone from the list.
	if h.certClient != nil {
		var notFound *certstatus.NotFoundError
		if err := h.certClient.Clean(ctx, certname); err != nil && !errors.As(err, &notFound) {
			writeError(w, err)
			return
		}
	}

	h.recordAudit(r, auditlog.Event{Action: "node.deleted", ResourceType: "node", ResourceID: certname})
	w.WriteHeader(http.StatusNoContent)
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, err error) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusBadGateway)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
}

func writeNotFound(w http.ResponseWriter, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusNotFound)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": message})
}
