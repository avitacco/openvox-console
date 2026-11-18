package patchinventory

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/voxpupuli/enterprise-console/internal/auditlog"
	"github.com/voxpupuli/enterprise-console/internal/nodescope"
	"github.com/voxpupuli/enterprise-console/internal/openvoxdb"
	"github.com/voxpupuli/enterprise-console/internal/pagination"
)

// ErrGroupForbidden is returned by a group resolver for a caller who may
// not look inside node groups.
var ErrGroupForbidden = errors.New("listing a node group's nodes requires classifier:read")

// NodeSource is the openvoxdb data the fleet view lists nodes from.
type NodeSource interface {
	Nodes(ctx context.Context) ([]openvoxdb.Node, error)
	FleetNodeFacts(ctx context.Context) ([]openvoxdb.NodeFacts, error)
}

// Handlers serves the patch inventory API.
type Handlers struct {
	store        *Store
	scanner      *Scanner
	source       NodeSource
	scope        nodescope.Func
	resolveGroup func(r *http.Request, groupID int64) ([]string, error)
	audit        func(r *http.Request, event auditlog.Event)
	now          func() time.Time
}

// NewHandlers builds Handlers. audit records a user's action in the
// nodes category.
func NewHandlers(store *Store, scanner *Scanner, source NodeSource, audit func(r *http.Request, event auditlog.Event)) *Handlers {
	return &Handlers{store: store, scanner: scanner, source: source, scope: nodescope.Unscoped, audit: audit, now: time.Now}
}

// SetScope limits each route to the caller's node group scope for the
// permission that gates it.
func (h *Handlers) SetScope(scope nodescope.Func) { h.scope = scope }

// SetGroupResolver enables the group filter and group scans.
func (h *Handlers) SetGroupResolver(fn func(r *http.Request, groupID int64) ([]string, error)) {
	h.resolveGroup = fn
}

// Register wires the routes. See internal/rbac.Verifier.Authorize.
func (h *Handlers) Register(mux *http.ServeMux, authorize func(permission string, next http.HandlerFunc) http.HandlerFunc) {
	mux.HandleFunc("GET /api/v1/patches", authorize("nodes:read", h.fleet))
	mux.HandleFunc("GET /api/v1/patches/packages", authorize("nodes:read", h.packages))
	mux.HandleFunc("GET /api/v1/patches/settings", authorize("nodes:read", h.getSettings))
	mux.HandleFunc("PUT /api/v1/patches/settings", authorize("nodes:manage", h.putSettings))
	mux.HandleFunc("POST /api/v1/patches/scan", authorize("orchestrator:run", h.scanGroup))
	mux.HandleFunc("GET /api/v1/nodes/{name}/patches", authorize("nodes:read", h.node))
	mux.HandleFunc("POST /api/v1/nodes/{name}/patches/scan", authorize("orchestrator:run", h.scanNode))
}

// staleAfter is how old a successful scan may be before it is marked
// stale: twice the interval, judged against a day when scanning is off.
func staleAfter(st Settings) time.Duration {
	if !st.Enabled {
		return DefaultInterval
	}
	return 2 * st.Interval
}

// NodeRow is one node in the fleet view.
type NodeRow struct {
	Certname       string     `json:"certname"`
	OS             string     `json:"os,omitempty"`
	Scanned        bool       `json:"scanned"`
	ScannedAt      *time.Time `json:"scannedAt,omitempty"`
	AttemptedAt    *time.Time `json:"attemptedAt,omitempty"`
	Manager        string     `json:"manager,omitempty"`
	Updates        int        `json:"updates"`
	Security       int        `json:"security"`
	RebootRequired bool       `json:"rebootRequired"`
	Stale          bool       `json:"stale"`
	Unsupported    bool       `json:"unsupported"`
	Error          string     `json:"error,omitempty"`
}

// Totals summarises the filtered fleet.
type Totals struct {
	Nodes          int `json:"nodes"`
	Scanned        int `json:"scanned"`
	NeverScanned   int `json:"neverScanned"`
	Unsupported    int `json:"unsupported"`
	WithUpdates    int `json:"withUpdates"`
	WithSecurity   int `json:"withSecurity"`
	RebootRequired int `json:"rebootRequired"`
	Stale          int `json:"stale"`
}

type fleetResponse struct {
	Settings Settings                 `json:"settings"`
	Totals   Totals                   `json:"totals"`
	OSNames  []string                 `json:"osNames"`
	Nodes    pagination.Page[NodeRow] `json:"nodes"`
}

func osLabel(f *openvoxdb.OSFact) string {
	if f == nil {
		return ""
	}
	label := f.Name
	if f.Release.Full != "" {
		label += " " + f.Release.Full
	}
	return strings.TrimSpace(label)
}

// groupNarrow applies a request's group filter on top of scope.
func (h *Handlers) groupNarrow(w http.ResponseWriter, r *http.Request, scope nodescope.Scope) (nodescope.Scope, bool) {
	raw := r.URL.Query().Get("group")
	if raw == "" {
		return scope, true
	}
	id, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		writeStatus(w, http.StatusBadRequest, "group must be a group id")
		return scope, false
	}
	if h.resolveGroup == nil {
		writeStatus(w, http.StatusBadRequest, "group filtering is not available")
		return scope, false
	}
	certnames, err := h.resolveGroup(r, id)
	if err != nil {
		if errors.Is(err, ErrGroupForbidden) {
			writeStatus(w, http.StatusForbidden, err.Error())
		} else {
			writeStatus(w, http.StatusBadGateway, err.Error())
		}
		return scope, false
	}
	// The group narrows within the caller's scope; it never reaches outside it.
	return scope.Intersect(certnames), true
}

func (h *Handlers) fleet(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	scope, err := h.scope(r)
	if err != nil {
		writeStatus(w, http.StatusBadGateway, err.Error())
		return
	}
	scope, ok := h.groupNarrow(w, r, scope)
	if !ok {
		return
	}
	settings, err := h.store.Settings(ctx)
	if err != nil {
		writeStatus(w, http.StatusBadGateway, err.Error())
		return
	}
	nodes, err := h.source.Nodes(ctx)
	if err != nil {
		writeStatus(w, http.StatusBadGateway, err.Error())
		return
	}
	facts, err := h.source.FleetNodeFacts(ctx)
	if err != nil {
		writeStatus(w, http.StatusBadGateway, err.Error())
		return
	}
	osByNode := make(map[string]string, len(facts))
	for _, f := range facts {
		osByNode[f.Certname] = osLabel(f.OS)
	}
	snaps, err := h.store.Snapshots(ctx, nil)
	if err != nil {
		writeStatus(w, http.StatusBadGateway, err.Error())
		return
	}

	// A package filter keeps only nodes with that package pending.
	var havePackage map[string]bool
	if pkg := r.URL.Query().Get("package"); pkg != "" {
		matches, err := h.store.Search(ctx, pkg, scope.Certnames())
		if err != nil {
			writeStatus(w, http.StatusBadGateway, err.Error())
			return
		}
		havePackage = map[string]bool{}
		for _, m := range matches {
			havePackage[m.Certname] = true
		}
	}
	osFilter := strings.ToLower(r.URL.Query().Get("os"))

	now := h.now()
	limit := staleAfter(settings)
	osSet := map[string]bool{}
	rows := []NodeRow{}
	var totals Totals
	for _, n := range nodes {
		if !scope.Allows(n.Certname) {
			continue
		}
		label := osByNode[n.Certname]
		if label != "" {
			osSet[label] = true
		}
		if osFilter != "" && strings.ToLower(label) != osFilter {
			continue
		}
		if havePackage != nil && !havePackage[n.Certname] {
			continue
		}
		row := NodeRow{Certname: n.Certname, OS: label}
		if sn, ok := snaps[n.Certname]; ok {
			row.Manager, row.Updates, row.Security = sn.Manager, sn.UpdateCount, sn.SecurityCount
			row.RebootRequired, row.Unsupported, row.Error = sn.RebootRequired, sn.Unsupported, sn.Error
			at := sn.AttemptedAt
			row.AttemptedAt = &at
			if sn.ScannedAt != nil {
				row.Scanned = true
				row.ScannedAt = sn.ScannedAt
				row.Stale = now.Sub(*sn.ScannedAt) > limit
			}
		}
		totals.Nodes++
		switch {
		case row.Unsupported:
			totals.Unsupported++
		case !row.Scanned:
			totals.NeverScanned++
		default:
			totals.Scanned++
			if row.Updates > 0 {
				totals.WithUpdates++
			}
			if row.Security > 0 {
				totals.WithSecurity++
			}
			if row.RebootRequired {
				totals.RebootRequired++
			}
			if row.Stale {
				totals.Stale++
			}
		}
		rows = append(rows, row)
	}
	sort.Slice(rows, func(i, j int) bool {
		a, b := rows[i], rows[j]
		if a.Security != b.Security {
			return a.Security > b.Security
		}
		if a.Updates != b.Updates {
			return a.Updates > b.Updates
		}
		return a.Certname < b.Certname
	})
	osNames := make([]string, 0, len(osSet))
	for o := range osSet {
		osNames = append(osNames, o)
	}
	sort.Strings(osNames)

	page, pageSize := pagination.Params(r)
	start := min(pagination.Offset(page, pageSize), len(rows))
	end := min(start+pageSize, len(rows))
	writeJSON(w, fleetResponse{
		Settings: settings, Totals: totals, OSNames: osNames,
		Nodes: pagination.New(rows[start:end], page, pageSize, len(rows)),
	})
}

func (h *Handlers) packages(w http.ResponseWriter, r *http.Request) {
	name := r.URL.Query().Get("name")
	if name == "" {
		writeStatus(w, http.StatusBadRequest, "name is required")
		return
	}
	scope, err := h.scope(r)
	if err != nil {
		writeStatus(w, http.StatusBadGateway, err.Error())
		return
	}
	scope, ok := h.groupNarrow(w, r, scope)
	if !ok {
		return
	}
	matches, err := h.store.Search(r.Context(), name, scope.Certnames())
	if err != nil {
		writeStatus(w, http.StatusBadGateway, err.Error())
		return
	}
	page, pageSize := pagination.Params(r)
	start := min(pagination.Offset(page, pageSize), len(matches))
	end := min(start+pageSize, len(matches))
	writeJSON(w, pagination.New(matches[start:end], page, pageSize, len(matches)))
}

type nodeResponse struct {
	Certname string    `json:"certname"`
	Scanned  bool      `json:"scanned"`
	Stale    bool      `json:"stale"`
	Snapshot *Snapshot `json:"snapshot"`
	Updates  []Update  `json:"updates"`
}

func (h *Handlers) node(w http.ResponseWriter, r *http.Request) {
	certname := r.PathValue("name")
	scope, err := h.scope(r)
	if err != nil {
		writeStatus(w, http.StatusBadGateway, err.Error())
		return
	}
	// Outside the caller's scope reads as a node that does not exist.
	if !scope.Allows(certname) {
		writeStatus(w, http.StatusNotFound, "node not found")
		return
	}
	snap, updates, err := h.store.Get(r.Context(), certname)
	if err != nil {
		writeStatus(w, http.StatusBadGateway, err.Error())
		return
	}
	resp := nodeResponse{Certname: certname, Snapshot: snap, Updates: []Update{}}
	if snap != nil {
		resp.Updates = updates
		if snap.ScannedAt != nil {
			resp.Scanned = true
			settings, err := h.store.Settings(r.Context())
			if err != nil {
				writeStatus(w, http.StatusBadGateway, err.Error())
				return
			}
			resp.Stale = h.now().Sub(*snap.ScannedAt) > staleAfter(settings)
		}
	}
	writeJSON(w, resp)
}

type scanResponse struct {
	Results []NodeResult `json:"results"`
	Scanned int          `json:"scanned"`
	Failed  int          `json:"failed"`
	Offline int          `json:"offline"`
	// Skipped counts nodes of a group left out because the caller may not
	// act on them.
	Skipped int `json:"skipped"`
}

func summarise(results []NodeResult, skipped int) scanResponse {
	resp := scanResponse{Results: results, Skipped: skipped}
	for _, r := range results {
		switch r.Outcome {
		case OutcomeScanned:
			resp.Scanned++
		case OutcomeOffline:
			resp.Offline++
		default:
			resp.Failed++
		}
	}
	return resp
}

func (h *Handlers) scanNode(w http.ResponseWriter, r *http.Request) {
	certname := r.PathValue("name")
	// Before the node is sent anything.
	if !nodescope.Permit(w, r, h.scope, certname) {
		return
	}
	res, err := h.scanner.ScanNode(r.Context(), certname)
	if err != nil {
		writeStatus(w, http.StatusBadGateway, err.Error())
		return
	}
	resp := summarise([]NodeResult{res}, 0)
	h.audit(r, auditlog.Event{Action: "patch.scan.requested", ResourceType: "node", ResourceID: certname,
		After: map[string]int{"scanned": resp.Scanned, "failed": resp.Failed, "offline": resp.Offline}})
	writeJSON(w, resp)
}

func (h *Handlers) scanGroup(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Group int64 `json:"group"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Group == 0 {
		writeStatus(w, http.StatusBadRequest, "a group id is required")
		return
	}
	scope, err := h.scope(r)
	if err != nil {
		writeStatus(w, http.StatusBadGateway, "could not determine which nodes you may act on: "+err.Error())
		return
	}
	if h.resolveGroup == nil {
		writeStatus(w, http.StatusBadRequest, "group scanning is not available")
		return
	}
	members, err := h.resolveGroup(r, body.Group)
	if err != nil {
		if errors.Is(err, ErrGroupForbidden) {
			writeStatus(w, http.StatusForbidden, err.Error())
		} else {
			writeStatus(w, http.StatusBadGateway, err.Error())
		}
		return
	}
	inScope := scope.Intersect(members).Certnames()
	if inScope == nil { // a fleet-wide caller: every member
		inScope = append([]string(nil), members...)
	}
	sort.Strings(inScope)
	results := h.scanner.ScanMany(r.Context(), inScope)
	resp := summarise(results, len(members)-len(inScope))
	h.audit(r, auditlog.Event{Action: "patch.scan.requested", ResourceType: "nodeGroup", ResourceID: strconv.FormatInt(body.Group, 10),
		After: map[string]int{"scanned": resp.Scanned, "failed": resp.Failed, "offline": resp.Offline, "skipped": resp.Skipped}})
	writeJSON(w, resp)
}

func (h *Handlers) getSettings(w http.ResponseWriter, r *http.Request) {
	st, err := h.store.Settings(r.Context())
	if err != nil {
		writeStatus(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, st)
}

func (h *Handlers) putSettings(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Enabled         bool `json:"enabled"`
		IntervalMinutes int  `json:"intervalMinutes"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeStatus(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}
	before, err := h.store.Settings(r.Context())
	if err != nil {
		writeStatus(w, http.StatusBadGateway, err.Error())
		return
	}
	after, err := h.store.SetSettings(r.Context(), body.Enabled, body.IntervalMinutes)
	if err != nil {
		if errors.Is(err, ErrInterval) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]any{"error": err.Error(), "field": "intervalMinutes"})
			return
		}
		writeStatus(w, http.StatusBadGateway, err.Error())
		return
	}
	h.audit(r, auditlog.Event{Action: "patch.scanSettings.updated", ResourceType: "patchScanSettings",
		Before: before, After: after})
	writeJSON(w, after)
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
