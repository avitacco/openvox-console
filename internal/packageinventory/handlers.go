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
	"slices"
	"sort"
	"strconv"

	"github.com/voxpupuli/enterprise-console/internal/auditlog"
	"github.com/voxpupuli/enterprise-console/internal/nodetransport"
	"github.com/voxpupuli/enterprise-console/internal/openvoxdb"
	"github.com/voxpupuli/enterprise-console/internal/pagination"
)

// queryClient is the subset of openvoxdb.Client this package needs, so
// handlers can be tested against a fake without a live openvoxdb.
type queryClient interface {
	NodePackages(ctx context.Context, certname string) ([]openvoxdb.Package, error)
	NodeFact(ctx context.Context, certname, name string) (*openvoxdb.Fact, error)
	SearchPackages(ctx context.Context, name string, version *string) ([]openvoxdb.Package, error)
	PackageNameCounts(ctx context.Context, filter openvoxdb.PackageFilter) ([]openvoxdb.PackageNameCount, error)
	PackageVersions(ctx context.Context, filter openvoxdb.PackageFilter) ([]openvoxdb.PackageVersion, error)
	PackageProviders(ctx context.Context) ([]openvoxdb.ProviderCount, error)
	PackageCountsByNode(ctx context.Context) ([]openvoxdb.NodePackageCount, error)
	Nodes(ctx context.Context) ([]openvoxdb.Node, error)
}

// consolePackageInventoryFact is the companion fact the Linux
// package-inventory fact scripts emit alongside _puppet_inventory_1 (see
// design.md in fix-package-inventory-fact-fidelity).
const consolePackageInventoryFact = "console_package_inventory"

// ErrGroupForbidden is what a group resolver returns when the caller
// may read packages but not the classifier.
var ErrGroupForbidden = errors.New("missing required permission: classifier:read")

// Handlers serves the package-inventory HTTP API.
type Handlers struct {
	client          queryClient
	transport       transport
	recordAudit     func(r *http.Request, event auditlog.Event)
	recordAuditRead func(r *http.Request, event auditlog.Event)
	// resolveGroup maps a group id to its matching certnames, or nil
	// when group filtering isn't wired. Injected as a function rather
	// than an interface because it also enforces the caller's
	// classifier:read - this package is gated on nodes:read, and
	// filtering by group would otherwise leak group membership to
	// someone without it. The policy lives in main.go, where rbac and
	// the classifier are already in scope.
	resolveGroup func(r *http.Request, groupID int64) ([]string, error)
}

// SetGroupResolver enables the catalogue's group filter. Left unset,
// the filter reports itself unavailable rather than silently ignoring
// a group the caller asked for.
func (h *Handlers) SetGroupResolver(fn func(r *http.Request, groupID int64) ([]string, error)) {
	h.resolveGroup = fn
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
	mux.HandleFunc("GET /api/v1/packages/catalog", authorize("nodes:read", h.packagesCatalog))
	mux.HandleFunc("GET /api/v1/nodes/{name}/packages/reporting", authorize("nodes:read", h.reportingStatus))
	mux.HandleFunc("PUT /api/v1/nodes/{name}/packages/reporting", authorize("orchestrator:run", h.setReporting))
}

// packageSummary's source fields are only set for apt packages on a node
// that has reported source-package data - see aptSources.
type packageSummary struct {
	PackageName   string `json:"packageName"`
	Provider      string `json:"provider"`
	Version       string `json:"version"`
	SourcePackage string `json:"sourcePackage,omitempty"`
	SourceVersion string `json:"sourceVersion,omitempty"`
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

	sources, err := h.aptSources(r.Context(), certname, packages)
	if err != nil {
		writeError(w, err)
		return
	}

	summaries := make([]packageSummary, 0, len(packages))
	for _, p := range packages {
		s := toPackageSummary(p)
		if sources != nil && p.Provider == "apt" {
			// A binary absent from sources is its own source package.
			s.SourcePackage, s.SourceVersion = p.PackageName, p.Version
			if src, ok := sources[p.PackageName]; ok {
				s.SourcePackage, s.SourceVersion = src[0], src[1]
			}
		}
		summaries = append(summaries, s)
	}
	h.recordAuditRead(r, auditlog.Event{Action: "node.packages.viewed", ResourceType: "node", ResourceID: certname})
	writeJSON(w, summaries)
}

// aptSources returns certname's binary package -> [source name, source
// version] map from its companion fact, or nil when packages has no apt
// entries (no lookup made) or the node hasn't reported usable source
// data - an older fact script, or a malformed fact. Its apt packages then
// carry no source fields rather than a guessed source. Only a failed
// openvoxdb lookup is an error; the fact itself is node-produced data.
func (h *Handlers) aptSources(ctx context.Context, certname string, packages []openvoxdb.Package) (map[string][2]string, error) {
	hasApt := false
	for _, p := range packages {
		if p.Provider == "apt" {
			hasApt = true
			break
		}
	}
	if !hasApt {
		return nil, nil
	}

	fact, err := h.client.NodeFact(ctx, certname, consolePackageInventoryFact)
	if err != nil || fact == nil {
		return nil, err
	}
	var value struct {
		Format  int                 `json:"format"`
		Sources map[string][]string `json:"sources"`
	}
	if err := json.Unmarshal(fact.Value, &value); err != nil || value.Format < 2 || value.Sources == nil {
		return nil, nil
	}
	sources := make(map[string][2]string, len(value.Sources))
	for binary, src := range value.Sources {
		if len(src) == 2 {
			sources[binary] = [2]string{src[0], src[1]}
		}
	}
	return sources, nil
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

// catalogPackage is one distinct package the fleet reports, collapsed
// across every node that has it.
type catalogPackage struct {
	PackageName string   `json:"packageName"`
	Providers   []string `json:"providers"`
	NodeCount   int      `json:"nodeCount"`
	Versions    []string `json:"versions"`
}

// catalogCoverage says how much of the fleet the catalogue is drawn
// from - without it, a fleet where one node reports looks the same as
// one with very few packages.
type catalogCoverage struct {
	NodesReporting int `json:"nodesReporting"`
	NodesTotal     int `json:"nodesTotal"`
}

// packageCatalog is the browsable, filterable package list.
type packageCatalog struct {
	Items     []catalogPackage `json:"items"`
	Page      int              `json:"page"`
	PageSize  int              `json:"pageSize"`
	Total     int              `json:"total"`
	Providers []string         `json:"providers"`
	Coverage  catalogCoverage  `json:"coverage"`
}

// emptyCatalog is the well-formed empty response for a filter that
// cannot match anything, so the page renders "no matches" rather than
// an error.
func emptyCatalog(r *http.Request) packageCatalog {
	page, pageSize := pagination.Params(r)
	return packageCatalog{
		Items:     []catalogPackage{},
		Page:      page,
		PageSize:  pageSize,
		Providers: []string{},
	}
}

// packagesCatalog lists the fleet's distinct packages, filtered and
// paginated, so the page has something to show without a search term.
//
// Filtering happens in openvoxdb, not here: `package_name ~ ...` and
// `provider = ...` both compose with `group by`, so a narrow filter
// transfers only its matches. Only paging and sorting are done here,
// because openvoxdb rejects `limit`/`order by` alongside `group by`.
func (h *Handlers) packagesCatalog(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	filter := openvoxdb.PackageFilter{
		NameContains:    r.URL.Query().Get("name"),
		VersionContains: r.URL.Query().Get("version"),
		Provider:        r.URL.Query().Get("provider"),
	}

	if raw := r.URL.Query().Get("group"); raw != "" {
		groupID, convErr := strconv.ParseInt(raw, 10, 64)
		if convErr != nil {
			writeStatus(w, http.StatusBadRequest, "group must be a group id")
			return
		}
		if h.resolveGroup == nil {
			writeStatus(w, http.StatusBadRequest, "group filtering is not available")
			return
		}
		certnames, resolveErr := h.resolveGroup(r, groupID)
		if resolveErr != nil {
			if errors.Is(resolveErr, ErrGroupForbidden) {
				writeStatus(w, http.StatusForbidden, resolveErr.Error())
				return
			}
			writeError(w, resolveErr)
			return
		}
		// A group matching no node has no packages. Querying with an
		// empty `in []` would either error or, worse, drop the
		// condition and return the whole fleet.
		if len(certnames) == 0 {
			h.recordAuditRead(r, auditlog.Event{Action: "package.catalog.viewed", ResourceType: "package"})
			writeJSON(w, emptyCatalog(r))
			return
		}
		filter.Certnames = certnames
	}

	counts, err := h.client.PackageNameCounts(ctx, filter)
	if err != nil {
		writeError(w, err)
		return
	}
	versions, err := h.client.PackageVersions(ctx, filter)
	if err != nil {
		writeError(w, err)
		return
	}
	providers, err := h.client.PackageProviders(ctx)
	if err != nil {
		writeError(w, err)
		return
	}
	nodeCounts, err := h.client.PackageCountsByNode(ctx)
	if err != nil {
		writeError(w, err)
		return
	}
	nodes, err := h.client.Nodes(ctx)
	if err != nil {
		writeError(w, err)
		return
	}

	// One package can be reported by more than one provider (and, on
	// multi-arch apt nodes, more than once per node), so the rows are
	// collapsed by name with the counts summed.
	byName := make(map[string]*catalogPackage)
	for _, c := range counts {
		p := byName[c.PackageName]
		if p == nil {
			p = &catalogPackage{PackageName: c.PackageName, Providers: []string{}, Versions: []string{}}
			byName[c.PackageName] = p
		}
		p.NodeCount += c.Count
		if c.Provider != "" && !slices.Contains(p.Providers, c.Provider) {
			p.Providers = append(p.Providers, c.Provider)
		}
	}
	for _, v := range versions {
		if p := byName[v.PackageName]; p != nil {
			p.Versions = append(p.Versions, v.Version)
		}
	}

	items := make([]catalogPackage, 0, len(byName))
	for _, p := range byName {
		sort.Strings(p.Providers)
		sort.Strings(p.Versions)
		items = append(items, *p)
	}
	// By name: map iteration is random, so an unsorted list would
	// reshuffle between identical requests and paginate incoherently.
	sort.Slice(items, func(i, j int) bool { return items[i].PackageName < items[j].PackageName })

	page, pageSize := pagination.Params(r)
	start := min(pagination.Offset(page, pageSize), len(items))
	end := min(start+pageSize, len(items))

	providerNames := make([]string, 0, len(providers))
	for _, p := range providers {
		providerNames = append(providerNames, p.Provider)
	}
	sort.Strings(providerNames)

	h.recordAuditRead(r, auditlog.Event{Action: "package.catalog.viewed", ResourceType: "package"})
	writeJSON(w, packageCatalog{
		Items:     items[start:end],
		Page:      page,
		PageSize:  pageSize,
		Total:     len(items),
		Providers: providerNames,
		Coverage:  catalogCoverage{NodesReporting: len(nodeCounts), NodesTotal: len(nodes)},
	})
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
