// Package rbac gates access to every capability in the console: local
// user/role/permission management, JWT-based login and session refresh,
// fast jti-based token revocation, and the authentication/authorization
// mechanism every other capability's endpoints depend on.
//
// See design.md in the change that introduced this package for the key
// decisions: permissions are embedded in the access token at issuance
// (not looked up per request), revocation is persisted in Postgres and
// propagated to running instances over NATS, and service tokens reuse the
// same verification path as user tokens.
package rbac

import "time"

// User is a local console account. OIDCSubject is set only for accounts
// provisioned via OIDC login (see CreateOIDCUser) and is the permanent
// lookup key for that identity - Username is a display value only and
// may not be unique to the identity provider across time.
type User struct {
	ID           int64     `json:"id,omitempty"`
	Username     string    `json:"username"`
	PasswordHash string    `json:"-"` // never serialized to a client
	OIDCSubject  *string   `json:"-"` // internal lookup key, not client-facing
	FirstName    *string   `json:"firstName,omitempty"`
	LastName     *string   `json:"lastName,omitempty"`
	Email        *string   `json:"email,omitempty"`
	CreatedAt    time.Time `json:"createdAt,omitempty"`

	// FailedLogins is the number of consecutive failed password logins
	// counted against the account. LockedUntil is set only while the
	// account is locked: a lock that has passed reads as nil. See
	// AccountPolicy.
	FailedLogins int        `json:"-"`
	LockedUntil  *time.Time `json:"-"`
}

// Locked reports whether password login is refused for the account at
// the moment it was read.
func (u User) Locked() bool { return u.LockedUntil != nil }

// Role grants a set of permissions to whichever users it's assigned to.
//
// NodeGroups are the node groups those permissions apply to: the nodes
// in each named group and, by inheritance, in every descendant of it. A
// role on the root group is fleet-wide; a role with none covers no
// nodes. The field is a pointer-free slice on purpose, so a role read
// from the store always serializes a list (possibly empty).
type Role struct {
	ID          int64    `json:"id,omitempty"`
	Name        string   `json:"name"`
	Permissions []string `json:"permissions"`
	NodeGroups  []int64  `json:"nodeGroups"`
	// TaskPatterns limits which tasks the role's orchestrator:run lets
	// its holders start: task names, "module::*" and "*" (see
	// internal/taskscope). Empty means every task. Like NodeGroups it
	// always serializes a list.
	TaskPatterns []string `json:"taskPatterns"`
	// Via says how a user holds the role when it comes from
	// ListUserRoles: "direct" and/or the name of each group granting it.
	// Empty (and omitted) everywhere else.
	Via []string `json:"via,omitempty"`
}

// ServiceToken is an admin-issued, non-interactive credential for machine
// clients (e.g. enc-bridge) that have no user to log in as.
type ServiceToken struct {
	ID          int64    `json:"id,omitempty"`
	Name        string   `json:"name"`
	JTI         string   `json:"jti"`
	Permissions []string `json:"permissions"`
	// TaskPatterns limits which tasks the token may run, as a role's
	// does. Empty means every task. Fixed for the token's life.
	TaskPatterns []string  `json:"taskPatterns"`
	CreatedAt    time.Time `json:"createdAt,omitempty"`
}
