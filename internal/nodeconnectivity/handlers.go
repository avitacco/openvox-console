// Package nodeconnectivity exposes which nodes currently hold a live
// node transport connection (internal/nodetransport.Registry) and their
// Puppet certificate status (internal/certstatus) via a small read-only
// HTTP API - kept separate from internal/inventory (openvoxdb-sourced
// facts/report status) since these can legitimately disagree (a node
// can have rich openvoxdb history with no live connection, or vice
// versa for a brand-new node) - see design.md in the add-nodes-page and
// add-node-connectivity-timestamp-and-cert-status changes.
package nodeconnectivity

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/voxpupuli/enterprise-console/internal/auditlog"
	"github.com/voxpupuli/enterprise-console/internal/certstatus"
)

// Registry is the subset of *nodetransport.Registry this package needs,
// so handlers can be tested without a real embedded NATS server.
type Registry interface {
	KnownCertnames() []string
	Lookup(certname string) bool
	LastConnected(certname string) *time.Time
	LastDisconnected(certname string) *time.Time
}

// CertStatusClient is the subset of *certstatus.Client this package
// needs, so handlers can be tested without a real openvoxserver.
type CertStatusClient interface {
	Statuses(ctx context.Context) (map[string]string, error)
	Sign(ctx context.Context, certname string) error
	Revoke(ctx context.Context, certname string) error
	Clean(ctx context.Context, certname string) error
}

// InfrastructureCertnames is the subset of *infracert.Set this package
// needs, so handlers can be tested without real certificate/network
// derivation. *infracert.Set satisfies this directly.
type InfrastructureCertnames interface {
	Lookup(certname string) (reason string, ok bool)
}

// Handlers serves the node-connectivity HTTP API.
type Handlers struct {
	registry        Registry
	certStatus      CertStatusClient
	infraCerts      InfrastructureCertnames
	recordAudit     func(r *http.Request, event auditlog.Event)
	recordAuditRead func(r *http.Request, event auditlog.Event)
	logger          *slog.Logger
}

// NewHandlers builds Handlers backed by registry, certStatus, and
// infraCerts. All three may be nil (the node transport is disabled when
// CONSOLE_NODE_TRANSPORT_ADDR is unset; certificate status reporting is
// disabled when CONSOLE_CA_CLIENT_URL is unset; infrastructure-certname
// detection is best-effort and may have determined nothing) - a nil
// registry means every node simply reports as not connected with no
// history, a nil certStatus means every node reports "unknown"
// certificate status, and every sign/revoke/clean action is rejected as
// not configured, and a nil infraCerts means every certname reports
// isInfrastructure: false, rather than the endpoint erroring. logger
// may be nil (slog.Default() is used) - only used to log a certStatus
// request failure, which is reported to the caller as "unknown" for
// every node rather than failing the whole response (a transient
// openvoxserver CA outage shouldn't take down connectivity reporting
// too).
func NewHandlers(registry Registry, certStatus CertStatusClient, infraCerts InfrastructureCertnames, recordAudit, recordAuditRead func(r *http.Request, event auditlog.Event), logger *slog.Logger) *Handlers {
	if logger == nil {
		logger = slog.Default()
	}
	return &Handlers{registry: registry, certStatus: certStatus, infraCerts: infraCerts, recordAudit: recordAudit, recordAuditRead: recordAuditRead, logger: logger}
}

// Register wires this package's routes onto mux: viewing connectivity/
// certificate status requires nodes:read (matching
// internal/inventory's existing gate on node-related read endpoints);
// acting on a certificate requires the separate, more privileged
// nodes:certs:manage permission, so nodes:read alone never grants the
// ability to sign, revoke, or clean a node's certificate.
func (h *Handlers) Register(mux *http.ServeMux, authorize func(permission string, next http.HandlerFunc) http.HandlerFunc) {
	mux.HandleFunc("GET /api/v1/node-connectivity", authorize("nodes:read", h.listConnected))
	mux.HandleFunc("POST /api/v1/nodes/{certname}/cert/sign", authorize("nodes:certs:manage", h.signCert))
	mux.HandleFunc("POST /api/v1/nodes/{certname}/cert/revoke", authorize("nodes:certs:manage", h.revokeCert))
	mux.HandleFunc("DELETE /api/v1/nodes/{certname}/cert", authorize("nodes:certs:manage", h.cleanCert))
}

const certStatusUnknown = "unknown"

// nodeStatus is one node's connectivity and certificate status - a
// certname neither system has ever heard of simply doesn't appear in
// the response, rather than being listed with every field empty/unknown.
type nodeStatus struct {
	Certname             string     `json:"certname"`
	Connected            bool       `json:"connected"`
	LastConnected        *time.Time `json:"lastConnected,omitempty"`
	LastDisconnected     *time.Time `json:"lastDisconnected,omitempty"`
	CertStatus           string     `json:"certStatus"`
	IsInfrastructure     bool       `json:"isInfrastructure"`
	InfrastructureReason string     `json:"infrastructureReason,omitempty"`
}

type connectivityResponse struct {
	Nodes []nodeStatus `json:"nodes"`
}

func (h *Handlers) listConnected(w http.ResponseWriter, r *http.Request) {
	certStatuses := map[string]string{}
	if h.certStatus != nil {
		statuses, err := h.certStatus.Statuses(r.Context())
		if err != nil {
			// Every node just falls back to "unknown" below - a
			// transient CA outage shouldn't fail connectivity
			// reporting, which is independently useful on its own.
			h.logger.Warn("failed to fetch certificate statuses", "error", err)
		} else {
			certStatuses = statuses
		}
	}

	// The response's certname universe is the union of what the
	// connectivity registry and the CA both know about - not just the
	// registry's - so a node whose certificate was never signed (and so
	// never connected either) still gets a row showing that, instead of
	// being invisible exactly when this is most useful (see design.md).
	certnames := map[string]struct{}{}
	if h.registry != nil {
		for _, c := range h.registry.KnownCertnames() {
			certnames[c] = struct{}{}
		}
	}
	for c := range certStatuses {
		certnames[c] = struct{}{}
	}

	resp := connectivityResponse{Nodes: []nodeStatus{}}
	for certname := range certnames {
		status := nodeStatus{Certname: certname, CertStatus: certStatusUnknown}
		if h.registry != nil {
			status.Connected = h.registry.Lookup(certname)
			status.LastConnected = h.registry.LastConnected(certname)
			status.LastDisconnected = h.registry.LastDisconnected(certname)
		}
		if cs, ok := certStatuses[certname]; ok {
			status.CertStatus = cs
		}
		if h.infraCerts != nil {
			if reason, ok := h.infraCerts.Lookup(certname); ok {
				status.IsInfrastructure = true
				status.InfrastructureReason = reason
			}
		}
		resp.Nodes = append(resp.Nodes, status)
	}

	h.recordAuditRead(r, auditlog.Event{Action: "node.connectivity.viewed", ResourceType: "node"})
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

func (h *Handlers) signCert(w http.ResponseWriter, r *http.Request) {
	h.certAction(w, r, "signed", func(ctx context.Context, certname string) error {
		return h.certStatus.Sign(ctx, certname)
	})
}

func (h *Handlers) revokeCert(w http.ResponseWriter, r *http.Request) {
	h.certAction(w, r, "revoked", func(ctx context.Context, certname string) error {
		return h.certStatus.Revoke(ctx, certname)
	})
}

func (h *Handlers) cleanCert(w http.ResponseWriter, r *http.Request) {
	h.certAction(w, r, "cleaned", func(ctx context.Context, certname string) error {
		return h.certStatus.Clean(ctx, certname)
	})
}

// certAction runs a single sign/revoke/clean action against certname
// (taken from the request path), rejecting with a "not configured"
// error when no CertStatusClient is wired rather than attempting the
// action. verbPastTense names both the audit action suffix
// ("node.certificate.<verbPastTense>") and doubles as this action's
// identity in that log - "signed"/"revoked"/"cleaned" match the spec's
// requirement names exactly, so there's no separate mapping to keep in
// sync. An audit entry is only recorded on success - see "a rejected
// action is not falsely logged as successful" in
// specs/node-certificate-management/spec.md.
func (h *Handlers) certAction(w http.ResponseWriter, r *http.Request, verbPastTense string, action func(ctx context.Context, certname string) error) {
	certname := r.PathValue("certname")

	if h.certStatus == nil {
		http.Error(w, "certificate management is not configured", http.StatusServiceUnavailable)
		return
	}

	if err := action(r.Context(), certname); err != nil {
		writeCertActionError(w, err)
		return
	}

	h.recordAudit(r, auditlog.Event{Action: "node.certificate." + verbPastTense, ResourceType: "node", ResourceID: certname})
	w.WriteHeader(http.StatusNoContent)
}

// writeCertActionError maps a certstatus action error to an HTTP
// response: a wrong-state transition is a client error (409 - the
// request conflicts with the certificate's current state), an unknown
// certname is a 404, and anything else (a CA outage, a malformed
// response) is a 502, since the failure is on the CA's side, not the
// caller's.
func writeCertActionError(w http.ResponseWriter, err error) {
	var wrongState *certstatus.WrongStateError
	if errors.As(err, &wrongState) {
		http.Error(w, wrongState.Error(), http.StatusConflict)
		return
	}
	var notFound *certstatus.NotFoundError
	if errors.As(err, &notFound) {
		http.Error(w, notFound.Error(), http.StatusNotFound)
		return
	}
	http.Error(w, "certificate action failed: "+err.Error(), http.StatusBadGateway)
}
