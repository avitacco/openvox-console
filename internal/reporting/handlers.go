// Package reporting implements the report history and resource-level
// event drill-down views - backed by openvoxdb-client.
package reporting

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/voxpupuli/enterprise-console/internal/auditlog"
	"github.com/voxpupuli/enterprise-console/internal/openvoxdb"
)

// queryClient is the subset of openvoxdb.Client this package needs, so
// handlers can be tested against a fake without a live openvoxdb.
type queryClient interface {
	Reports(ctx context.Context, certname string) ([]openvoxdb.Report, error)
	Events(ctx context.Context, reportHash string) ([]openvoxdb.Event, error)
}

// Handlers serves the reporting HTTP API.
type Handlers struct {
	client          queryClient
	recordAuditRead func(r *http.Request, event auditlog.Event)
}

// NewHandlers builds Handlers backed by client. recordAuditRead is bound
// to the nodes category (configurable-audit-logging) - both routes here
// require nodes:read, the same permission scope as internal/inventory.
func NewHandlers(client queryClient, recordAuditRead func(r *http.Request, event auditlog.Event)) *Handlers {
	return &Handlers{client: client, recordAuditRead: recordAuditRead}
}

// Register wires this package's routes onto mux, requiring the
// nodes:read permission on each - see internal/rbac.Verifier.Authorize
// for what authorize does.
func (h *Handlers) Register(mux *http.ServeMux, authorize func(permission string, next http.HandlerFunc) http.HandlerFunc) {
	mux.HandleFunc("GET /api/v1/nodes/{name}/reports", authorize("nodes:read", h.listReports))
	mux.HandleFunc("GET /api/v1/reports/{id}/events", authorize("nodes:read", h.listEvents))
}

type reportSummary struct {
	Hash      string `json:"hash"`
	Status    string `json:"status"`
	StartTime string `json:"startTime"`
	EndTime   string `json:"endTime"`
}

func toReportSummary(r openvoxdb.Report) reportSummary {
	return reportSummary{
		Hash:      r.Hash,
		Status:    r.Status,
		StartTime: r.StartTime.Format(time.RFC3339),
		EndTime:   r.EndTime.Format(time.RFC3339),
	}
}

func (h *Handlers) listReports(w http.ResponseWriter, r *http.Request) {
	certname := r.PathValue("name")

	reports, err := h.client.Reports(r.Context(), certname)
	if err != nil {
		writeError(w, err)
		return
	}

	if status := r.URL.Query().Get("status"); status != "" {
		filtered := make([]openvoxdb.Report, 0, len(reports))
		for _, rep := range reports {
			if rep.Status == status {
				filtered = append(filtered, rep)
			}
		}
		reports = filtered
	}

	summaries := make([]reportSummary, 0, len(reports))
	for _, rep := range reports {
		summaries = append(summaries, toReportSummary(rep))
	}
	h.recordAuditRead(r, auditlog.Event{Action: "report.list.viewed", ResourceType: "node", ResourceID: certname})
	writeJSON(w, summaries)
}

type eventDetail struct {
	ResourceType  string          `json:"resourceType"`
	ResourceTitle string          `json:"resourceTitle"`
	Property      *string         `json:"property"`
	Status        string          `json:"status"`
	OldValue      json.RawMessage `json:"oldValue"`
	NewValue      json.RawMessage `json:"newValue"`
	Message       *string         `json:"message"`
	Timestamp     string          `json:"timestamp"`
}

func toEventDetail(e openvoxdb.Event) eventDetail {
	return eventDetail{
		ResourceType:  e.ResourceType,
		ResourceTitle: e.ResourceTitle,
		Property:      e.Property,
		Status:        e.Status,
		OldValue:      e.OldValue,
		NewValue:      e.NewValue,
		Message:       e.Message,
		Timestamp:     e.Timestamp.Format(time.RFC3339),
	}
}

func (h *Handlers) listEvents(w http.ResponseWriter, r *http.Request) {
	reportHash := r.PathValue("id")

	events, err := h.client.Events(r.Context(), reportHash)
	if err != nil {
		writeError(w, err)
		return
	}

	if status := r.URL.Query().Get("status"); status != "" {
		filtered := make([]openvoxdb.Event, 0, len(events))
		for _, e := range events {
			if e.Status == status {
				filtered = append(filtered, e)
			}
		}
		events = filtered
	}

	details := make([]eventDetail, 0, len(events))
	for _, e := range events {
		details = append(details, toEventDetail(e))
	}
	h.recordAuditRead(r, auditlog.Event{Action: "report.events.viewed", ResourceType: "report", ResourceID: reportHash})
	writeJSON(w, details)
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
