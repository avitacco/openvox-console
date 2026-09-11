// Package packageinventory implements node-level and fleet-wide
// installed-package reporting, backed by openvoxdb-client's
// package_inventory query entity - kept separate from internal/inventory
// since it's its own query surface and its own spec, even though it's
// also keyed by certname (see design.md in add-package-inventory).
package packageinventory

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/voxpupuli/enterprise-console/internal/auditlog"
	"github.com/voxpupuli/enterprise-console/internal/nodetransport"
	"github.com/voxpupuli/enterprise-console/internal/openvoxdb"
)

// queryClient is the subset of openvoxdb.Client this package needs, so
// handlers can be tested against a fake without a live openvoxdb.
type queryClient interface {
	NodePackages(ctx context.Context, certname string) ([]openvoxdb.Package, error)
	SearchPackages(ctx context.Context, name string, version *string) ([]openvoxdb.Package, error)
}

// Handlers serves the package-inventory HTTP API.
type Handlers struct {
	client          queryClient
	transport       transport
	recordAudit     func(r *http.Request, event auditlog.Event)
	recordAuditRead func(r *http.Request, event auditlog.Event)
}

// NewHandlers builds Handlers backed by client and transport (may be
// nil - node-affecting requests then fail as "node not connected", the
// same posture internal/orchestrator already has when node transport
// isn't configured). recordAudit and recordAuditRead are both bound to
// the nodes category (configurable-audit-logging) - the existing
// package-listing routes and the new reporting-status read require
// nodes:read; the new reporting-set write requires orchestrator:run
// (see design.md in add-package-inventory-toggle for why that
// permission, not a new one).
func NewHandlers(client queryClient, transport transport, recordAudit, recordAuditRead func(r *http.Request, event auditlog.Event)) *Handlers {
	return &Handlers{client: client, transport: transport, recordAudit: recordAudit, recordAuditRead: recordAuditRead}
}

// Register wires this package's routes onto mux - see
// internal/rbac.Verifier.Authorize for what authorize does.
func (h *Handlers) Register(mux *http.ServeMux, authorize func(permission string, next http.HandlerFunc) http.HandlerFunc) {
	mux.HandleFunc("GET /api/v1/nodes/{name}/packages", authorize("nodes:read", h.listNodePackages))
	mux.HandleFunc("GET /api/v1/packages", authorize("nodes:read", h.searchPackages))
	mux.HandleFunc("GET /api/v1/nodes/{name}/packages/reporting", authorize("nodes:read", h.reportingStatus))
	mux.HandleFunc("PUT /api/v1/nodes/{name}/packages/reporting", authorize("orchestrator:run", h.setReporting))
}

type packageSummary struct {
	PackageName string `json:"packageName"`
	Provider    string `json:"provider"`
	Version     string `json:"version"`
}

func toPackageSummary(p openvoxdb.Package) packageSummary {
	return packageSummary{PackageName: p.PackageName, Provider: p.Provider, Version: p.Version}
}

func (h *Handlers) listNodePackages(w http.ResponseWriter, r *http.Request) {
	certname := r.PathValue("name")

	packages, err := h.client.NodePackages(r.Context(), certname)
	if err != nil {
		writeError(w, err)
		return
	}

	summaries := make([]packageSummary, 0, len(packages))
	for _, p := range packages {
		summaries = append(summaries, toPackageSummary(p))
	}
	h.recordAuditRead(r, auditlog.Event{Action: "node.packages.viewed", ResourceType: "node", ResourceID: certname})
	writeJSON(w, summaries)
}

type packageSearchResult struct {
	Certname string `json:"certname"`
	Version  string `json:"version"`
	Provider string `json:"provider"`
}

func toPackageSearchResult(p openvoxdb.Package) packageSearchResult {
	return packageSearchResult{Certname: p.Certname, Version: p.Version, Provider: p.Provider}
}

func (h *Handlers) searchPackages(w http.ResponseWriter, r *http.Request) {
	name := r.URL.Query().Get("name")
	if name == "" {
		writeStatus(w, http.StatusBadRequest, "name is required")
		return
	}

	var version *string
	if v := r.URL.Query().Get("version"); v != "" {
		version = &v
	}

	packages, err := h.client.SearchPackages(r.Context(), name, version)
	if err != nil {
		writeError(w, err)
		return
	}

	results := make([]packageSearchResult, 0, len(packages))
	for _, p := range packages {
		results = append(results, toPackageSearchResult(p))
	}
	h.recordAuditRead(r, auditlog.Event{Action: "package.search.viewed", ResourceType: "package", ResourceID: name})
	writeJSON(w, results)
}

// reportingStatus is the API response shape for both the status and set
// endpoints - Output is only present when a Puppet run actually
// occurred (Ran distinguishes that from "ran but produced empty
// output", see dispatch.go's dispatchResult doc comment).
type reportingStatus struct {
	Enabled bool          `json:"enabled"`
	Output  *reportingRun `json:"output,omitempty"`
}

type reportingRun struct {
	Stdout   string `json:"stdout"`
	Stderr   string `json:"stderr"`
	ExitCode int    `json:"exitCode"`
}

func toReportingStatus(result *dispatchResult) reportingStatus {
	rs := reportingStatus{Enabled: result.PackageInventory.Enabled}
	if result.PackageInventory.Ran {
		rs.Output = &reportingRun{Stdout: result.Output.Stdout, Stderr: result.Output.Stderr, ExitCode: result.Output.ExitCode}
	}
	return rs
}

func (h *Handlers) reportingStatus(w http.ResponseWriter, r *http.Request) {
	certname := r.PathValue("name")

	if h.transport == nil {
		writeNodeOffline(w, certname)
		return
	}
	result, err := dispatchPackageInventory(r.Context(), h.transport, certname, actionPackageInventoryStatus, nil)
	if err != nil {
		if errors.Is(err, nodetransport.ErrNodeNotConnected) {
			writeNodeOffline(w, certname)
			return
		}
		writeError(w, err)
		return
	}

	h.recordAuditRead(r, auditlog.Event{Action: "node.packageInventory.viewed", ResourceType: "node", ResourceID: certname})
	writeJSON(w, toReportingStatus(result))
}

func (h *Handlers) setReporting(w http.ResponseWriter, r *http.Request) {
	certname := r.PathValue("name")

	var body struct {
		Enabled bool `json:"enabled"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeStatus(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}

	if h.transport == nil {
		writeNodeOffline(w, certname)
		return
	}
	result, err := dispatchPackageInventory(r.Context(), h.transport, certname, actionPackageInventorySet, packageInventorySetParams{Enabled: body.Enabled})
	if err != nil {
		if errors.Is(err, nodetransport.ErrNodeNotConnected) {
			writeNodeOffline(w, certname)
			return
		}
		writeError(w, err)
		return
	}

	h.recordAudit(r, auditlog.Event{Action: "node.packageInventory.set", ResourceType: "node", ResourceID: certname})
	writeJSON(w, toReportingStatus(result))
}

func writeNodeOffline(w http.ResponseWriter, certname string) {
	writeStatus(w, http.StatusConflict, "node is offline (not connected to the node transport): "+certname)
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

func writeStatus(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": message})
}

func writeError(w http.ResponseWriter, err error) {
	writeStatus(w, http.StatusBadGateway, err.Error())
}
