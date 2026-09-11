// Package encapi exposes the console's classification decisions to
// openvox-server as an External Node Classifier (ENC): a JSON HTTP
// endpoint, consumed by the cmd/enc-bridge executable that translates it
// into the YAML format Puppet's exec-based classifier terminus requires.
// Kept separate from internal/classifier so this external contract can be
// audited/versioned independently of the internal group/merge model.
package encapi

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/voxpupuli/enterprise-console/internal/classifier"
	"github.com/voxpupuli/enterprise-console/internal/openvoxdb"
)

// groupLister is the subset of classifier.Store this package needs.
type groupLister interface {
	ListAllGroups(ctx context.Context) ([]classifier.Group, error)
}

// factFetcher is the subset of openvoxdb.Client this package needs.
type factFetcher interface {
	Facts(ctx context.Context, certname string) ([]openvoxdb.Fact, error)
}

// Handlers serves the ENC HTTP API.
type Handlers struct {
	groups groupLister
	facts  factFetcher
}

// NewHandlers builds Handlers backed by groups and facts.
func NewHandlers(groups groupLister, facts factFetcher) *Handlers {
	return &Handlers{groups: groups, facts: facts}
}

// Register wires this package's routes onto mux, requiring the enc:read
// permission - expected to be held by a service token issued to
// cmd/enc-bridge, not a user login. See internal/rbac.Verifier.Authorize
// for what authorize does.
func (h *Handlers) Register(mux *http.ServeMux, authorize func(permission string, next http.HandlerFunc) http.HandlerFunc) {
	mux.HandleFunc("GET /api/v1/enc/{certname}", authorize("enc:read", h.classify))
}

// Response is the ENC endpoint's JSON shape - what cmd/enc-bridge
// consumes and translates to Puppet's exec-terminus YAML format.
type Response struct {
	Classes     map[string]map[string]any `json:"classes"`
	Parameters  map[string]any            `json:"parameters"`
	Environment *string                   `json:"environment,omitempty"`
}

func (h *Handlers) classify(w http.ResponseWriter, r *http.Request) {
	certname := r.PathValue("certname")
	ctx := r.Context()

	groups, err := h.groups.ListAllGroups(ctx)
	if err != nil {
		writeError(w, err)
		return
	}

	factRows, err := h.facts.Facts(ctx, certname)
	if err != nil {
		writeError(w, err)
		return
	}

	result := classifier.Classify(groups, certname, factsMap(factRows))
	writeJSON(w, Response{
		Classes:     result.Classes,
		Parameters:  result.Parameters,
		Environment: result.Environment,
	})
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

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, err error) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusBadGateway)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
}
