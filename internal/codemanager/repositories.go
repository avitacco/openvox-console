package codemanager

import (
	"net/http"
	"time"

	"github.com/voxpupuli/enterprise-console/internal/auditlog"
)

// Repository is one configured control repo as the overview reports it.
//
// Every count and measurement is a pointer, and deliberately: "no node
// uses this" and "we could not find out" are different answers, and a
// repository that has deployed to nothing must not read as one that has
// deployed to zero bytes. JSON omits them when absent, so the page can
// render the distinction rather than printing 0.
type Repository struct {
	Name string `json:"name"`
	// Remote has any embedded credentials redacted - see redactRemote.
	Remote string `json:"remote"`
	// Prefix is the source's effective environment prefix, empty for an
	// unprefixed source.
	Prefix string `json:"prefix"`
	// Environments are those this repository has successfully deployed,
	// empty for one that never has.
	Environments []string `json:"environments"`
	// AssignedNodes is how many nodes classification sends to one of
	// this repository's environments; ReportingNodes how many last
	// reported from one. Nil means the node data could not be read.
	AssignedNodes  *int `json:"assignedNodes,omitempty"`
	ReportingNodes *int `json:"reportingNodes,omitempty"`
	// SizeBytes is the content size recorded at this repository's most
	// recent successful deploy. Nil when it has never deployed, or
	// deployed only before sizes were measured.
	SizeBytes *int64 `json:"sizeBytes,omitempty"`
	// LastDeployedAt is when this repository last deployed
	// successfully. Nil when it never has.
	LastDeployedAt *time.Time `json:"lastDeployedAt,omitempty"`
}

// RepositoriesResponse is the overview endpoint's JSON shape.
type RepositoriesResponse struct {
	Repositories []Repository `json:"repositories"`
	// NodeCountsAvailable is false when the node data could not be
	// read, so the page can say "unavailable" rather than rendering
	// every repository as unused.
	NodeCountsAvailable bool `json:"nodeCountsAvailable"`
	// UnattributedAssigned and UnattributedReporting count nodes whose
	// environment no configured repository is recorded as having
	// deployed. Reported here rather than folded into a repository:
	// attributing them to the unprefixed source would be a fabrication,
	// and their existence is itself worth surfacing - usually an
	// environment left behind by a removed source, or a node with a
	// hardcoded environment.
	UnattributedAssigned  *int `json:"unattributedAssigned,omitempty"`
	UnattributedReporting *int `json:"unattributedReporting,omitempty"`
	// NodesWithoutEnvironment counts nodes carrying no environment at
	// all - never reported, or unclassified.
	NodesWithoutEnvironment *int `json:"nodesWithoutEnvironment,omitempty"`
}

// listRepositories reports every configured source with what deploy
// history and node inventory know about it.
//
// The four inputs fail independently. Config and Postgres supply the
// remote, prefix, environments, size and last-deploy; only the node
// counts need openvoxdb, so an unreachable openvoxdb leaves those nil
// and still returns everything else.
func (h *Handlers) listRepositories(w http.ResponseWriter, r *http.Request) {
	summaries, err := h.store.DeploySummaryBySource(r.Context())
	if err != nil {
		writeError(w, http.StatusBadGateway, err)
		return
	}
	owners := attributeEnvironments(summaries)

	response := RepositoriesResponse{Repositories: make([]Repository, 0)}

	usage, usageErr := h.usage.Usage(r.Context())
	if usageErr != nil {
		// Warn, not error: the page still renders, and an operator
		// running without openvoxdb reachable does not need this in
		// their logs as a failure.
		h.logger.Warn("could not compute repository node counts", "error", usageErr)
	} else {
		response.NodeCountsAvailable = true
		response.UnattributedAssigned = intPtr(usage.Assigned.unattributed(owners))
		response.UnattributedReporting = intPtr(usage.Reporting.unattributed(owners))
		response.NodesWithoutEnvironment = intPtr(usage.Reporting.Unknown)
	}

	for _, source := range h.deployer.Sources() {
		repo := Repository{
			Name:         source.Name,
			Remote:       redactRemote(source.Remote),
			Prefix:       source.EffectivePrefix(),
			Environments: make([]string, 0),
		}

		if summary, deployed := summaries[source.Name]; deployed {
			repo.Environments = summary.Environments
			repo.LastDeployedAt = summary.LastDeployAt
			repo.SizeBytes = summary.SizeBytes
		}

		if response.NodeCountsAvailable {
			repo.AssignedNodes = intPtr(usage.Assigned.forSource(source.Name, owners))
			repo.ReportingNodes = intPtr(usage.Reporting.forSource(source.Name, owners))
		}

		response.Repositories = append(response.Repositories, repo)
	}

	h.recordAuditRead(r, auditlog.Event{Action: "code.repositories.viewed", ResourceType: "code_repository"})
	writeJSON(w, http.StatusOK, response)
}

func intPtr(v int) *int { return &v }
