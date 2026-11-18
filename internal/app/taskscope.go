package app

import (
	"net/http"

	"github.com/voxpupuli/enterprise-console/internal/nodescope"
	"github.com/voxpupuli/enterprise-console/internal/rbac"
	"github.com/voxpupuli/enterprise-console/internal/taskscope"
)

// taskScopeFunc is how handlers learn which tasks their caller may run,
// from the grants in the caller's access token.
//
// It reads the claims rather than the per-permission node grant the
// authorize middleware records, because the task catalogue is gated on
// orchestrator:read while a task allowlist narrows orchestrator:run. A
// caller who does not hold orchestrator:run is not limited: they can
// start nothing, and the limit is on starting. A request with no claims
// at all gets a scope allowing nothing, so a route wired without
// authentication fails closed.
func taskScopeFunc(groups taskscope.Groups) taskscope.Func {
	return func(r *http.Request) (taskscope.Scope, error) {
		claims, ok := rbac.ClaimsFromContext(r.Context())
		if !ok {
			return taskscope.Scope{}, nil
		}
		if !claims.HasPermission(runPermission) {
			return taskscope.Unrestricted(), nil
		}
		return taskscope.Resolve(r.Context(), claims.TaskGrants, groups)
	}
}

var _ taskscope.Groups = (*nodescope.Resolver)(nil)
