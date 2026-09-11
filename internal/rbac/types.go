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
}

// Role grants a set of permissions to whichever users it's assigned to.
type Role struct {
	ID          int64    `json:"id,omitempty"`
	Name        string   `json:"name"`
	Permissions []string `json:"permissions"`
}

// ServiceToken is an admin-issued, non-interactive credential for machine
// clients (e.g. enc-bridge) that have no user to log in as.
type ServiceToken struct {
	ID          int64     `json:"id,omitempty"`
	Name        string    `json:"name"`
	JTI         string    `json:"jti"`
	Permissions []string  `json:"permissions"`
	CreatedAt   time.Time `json:"createdAt,omitempty"`
}
