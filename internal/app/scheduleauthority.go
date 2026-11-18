package app

import (
	"context"
	"errors"
	"slices"

	"github.com/voxpupuli/enterprise-console/internal/nodescope"
	"github.com/voxpupuli/enterprise-console/internal/rbac"
	"github.com/voxpupuli/enterprise-console/internal/scheduler"
	"github.com/voxpupuli/enterprise-console/internal/taskscope"
)

// runPermission is what starting a job needs, on demand or scheduled.
const runPermission = "orchestrator:run"

// scheduleOwners is the part of *rbac.Store a schedule's owner is
// looked up with.
type scheduleOwners interface {
	GetUserByUsername(ctx context.Context, username string) (rbac.User, error)
	UserPermissions(ctx context.Context, userID int64) ([]string, error)
	UserNodeScopes(ctx context.Context, userID int64) (map[string][]int64, error)
	UserTaskGrants(ctx context.Context, userID int64) ([]taskscope.Grant, error)
	ListServiceTokens(ctx context.Context) ([]rbac.ServiceToken, error)
}

// scopeForGroups turns granted node groups into a node scope.
// *nodescope.Resolver's For is one.
type scopeForGroups interface {
	For(ctx context.Context, groupIDs []int64) (nodescope.Scope, error)
}

// scheduleAuthority answers what a schedule's owner may act on at the
// moment it fires, from their roles as they stand in the database
// then - not from the token they held when they saved it, which may
// have been issued under roles since taken away.
//
// A user must still exist and still hold orchestrator:run; their scope
// is the node groups their roles grant that permission on. A service
// token must still exist (revoking one deletes it) and carry the
// permission; it is fleet-wide, as service tokens are.
func scheduleAuthority(owners scheduleOwners, scopes scopeForGroups) scheduler.Authority {
	return func(ctx context.Context, owner scheduler.Owner) (scheduler.Authorization, error) {
		switch owner.Kind {
		case scheduler.OwnerUser:
			user, err := owners.GetUserByUsername(ctx, owner.Name)
			if errors.Is(err, rbac.ErrNotFound) {
				return scheduler.Authorization{}, scheduler.ErrNoAuthority
			}
			if err != nil {
				return scheduler.Authorization{}, err
			}
			permissions, err := owners.UserPermissions(ctx, user.ID)
			if err != nil {
				return scheduler.Authorization{}, err
			}
			if !slices.Contains(permissions, runPermission) {
				return scheduler.Authorization{}, scheduler.ErrNoAuthority
			}
			groups, err := owners.UserNodeScopes(ctx, user.ID)
			if err != nil {
				return scheduler.Authorization{}, err
			}
			scope, err := scopes.For(ctx, groups[runPermission])
			if err != nil {
				return scheduler.Authorization{}, err
			}
			// The tasks the owner may run are worked out from their roles
			// now too: a schedule must stop running a task the moment its
			// owner's role stops allowing it.
			grants, err := owners.UserTaskGrants(ctx, user.ID)
			if err != nil {
				return scheduler.Authorization{}, err
			}
			tasks, err := taskscope.Resolve(ctx, grants, scopes)
			if err != nil {
				return scheduler.Authorization{}, err
			}
			return scheduler.Authorization{Scope: scope, Tasks: tasks}, nil

		case scheduler.OwnerServiceToken:
			tokens, err := owners.ListServiceTokens(ctx)
			if err != nil {
				return scheduler.Authorization{}, err
			}
			for _, token := range tokens {
				if token.Name == owner.Name && slices.Contains(token.Permissions, runPermission) {
					tasks, err := taskscope.Resolve(ctx, rbac.ServiceTokenGrants(token.TaskPatterns), scopes)
					if err != nil {
						return scheduler.Authorization{}, err
					}
					return scheduler.Authorization{Scope: nodescope.Everything(), Tasks: tasks}, nil
				}
			}
		}
		return scheduler.Authorization{}, scheduler.ErrNoAuthority
	}
}
