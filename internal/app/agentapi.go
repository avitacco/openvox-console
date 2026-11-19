package app

import (
	"context"
	"errors"
	"net/http"
	"slices"
	"time"

	"github.com/voxpupuli/enterprise-console/internal/agentrequests"
	"github.com/voxpupuli/enterprise-console/internal/nodescope"
	"github.com/voxpupuli/enterprise-console/internal/rbac"
	"github.com/voxpupuli/enterprise-console/internal/taskscope"
)

// agentTokens is the part of *rbac.Store the agent endpoint's authority
// is looked up with.
type agentTokens interface {
	GetAgentToken(ctx context.Context, id int64) (rbac.AgentToken, error)
	GetAgentTokenByJTI(ctx context.Context, jti string) (rbac.AgentToken, error)
}

// agentAuthority answers, at the moment a request is approved, what the
// agent's token may do as it stands then: it must still exist (revoking one
// deletes it), not have expired, and still carry orchestrator:run, and its
// reach is the node groups and tasks it was issued with.
func agentAuthority(tokens agentTokens, scopes scopeForGroups, now func() time.Time) agentrequests.Authority {
	return func(ctx context.Context, id int64) (agentrequests.Authorization, error) {
		token, err := tokens.GetAgentToken(ctx, id)
		if errors.Is(err, rbac.ErrNotFound) {
			return agentrequests.Authorization{}, agentrequests.ErrTokenGone
		}
		if err != nil {
			return agentrequests.Authorization{}, err
		}
		if token.Expired(now()) || !slices.Contains(token.Permissions, runPermission) {
			return agentrequests.Authorization{}, agentrequests.ErrTokenGone
		}
		scope, err := scopes.For(ctx, token.NodeGroups)
		if err != nil {
			return agentrequests.Authorization{}, err
		}
		var grants []taskscope.Grant
		if len(token.TaskPatterns) > 0 {
			grants = []taskscope.Grant{{Tasks: token.TaskPatterns, Groups: token.NodeGroups}}
		}
		tasks, err := taskscope.Resolve(ctx, grants, scopes)
		if err != nil {
			return agentrequests.Authorization{}, err
		}
		return agentrequests.Authorization{Scope: scope, Tasks: tasks, Owner: token.Owner}, nil
	}
}

// agentCaller says what an agent token may reach when it makes a request:
// the same node scope and task scope the console works out for any caller
// holding these claims, and the token's registry entry for its id.
func agentCaller(tokens agentTokens, nodeScope nodescope.Func, taskScope taskscope.Func) func(r *http.Request, claims *rbac.Claims) (agentrequests.Caller, error) {
	return func(r *http.Request, claims *rbac.Claims) (agentrequests.Caller, error) {
		token, err := tokens.GetAgentTokenByJTI(r.Context(), claims.ID)
		if err != nil {
			return agentrequests.Caller{}, err
		}
		groups, fleetWide := claims.NodeGroupsFor(runPermission)
		ctx := rbac.WithClaims(nodescope.WithGrant(r.Context(), groups, fleetWide), claims)
		scoped := r.WithContext(ctx)
		scope, err := nodeScope(scoped)
		if err != nil {
			return agentrequests.Caller{}, err
		}
		tasks, err := taskScope(scoped)
		if err != nil {
			return agentrequests.Caller{}, err
		}
		return agentrequests.Caller{TokenID: token.ID, TokenName: token.Name, Scope: scope, Tasks: tasks}, nil
	}
}

// approverFor describes the person deciding a request: who they are and
// what their own roles let them run. Someone without orchestrator:run can
// run nothing, whatever agent-requests:approve says.
func approverFor(scopes scopeForGroups, name func(r *http.Request) string) func(r *http.Request) (agentrequests.Approver, error) {
	return func(r *http.Request) (agentrequests.Approver, error) {
		approver := agentrequests.Approver{Name: name(r)}
		claims, ok := rbac.ClaimsFromContext(r.Context())
		if !ok || !claims.HasPermission(runPermission) {
			return approver, nil
		}
		groups, fleetWide := claims.NodeGroupsFor(runPermission)
		if fleetWide {
			approver.Scope = nodescope.Everything()
		} else {
			scope, err := scopes.For(r.Context(), groups)
			if err != nil {
				return agentrequests.Approver{}, err
			}
			approver.Scope = scope
		}
		tasks, err := taskscope.Resolve(r.Context(), claims.TaskGrants, scopes)
		if err != nil {
			return agentrequests.Approver{}, err
		}
		approver.Tasks = tasks
		return approver, nil
	}
}
