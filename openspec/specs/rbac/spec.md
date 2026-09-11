# rbac Specification

## Purpose

Gates access to every capability in the console: local user/role/permission
management, JWT-based login and session refresh, fast token revocation,
and the authentication/authorization mechanism every other capability's
endpoints depend on.

## Requirements

### Requirement: User account management
The system SHALL allow creating, viewing, updating, and deleting local
user accounts, each with a username and password. Accounts may also be
created automatically through OIDC login (see "OIDC login provisions a
local user"); there is no general external directory sync (LDAP/SAML) of
arbitrary user attributes beyond that narrow, configured OIDC
provisioning path.

#### Scenario: Creating a user
- **WHEN** an administrator creates a user with a username and password
- **THEN** the user can subsequently log in with those credentials

#### Scenario: Deleting a user
- **WHEN** an administrator deletes a user
- **THEN** that user can no longer log in

### Requirement: Role and permission management
The system SHALL allow creating, viewing, updating, and deleting roles,
each granting a set of permissions, and assigning one or more roles to a
user.

#### Scenario: Creating a role with permissions
- **WHEN** an administrator creates a role granting a set of permissions
- **THEN** any user assigned that role has those permissions on their next
  login

#### Scenario: Assigning a role to a user
- **WHEN** an administrator assigns a role to a user
- **THEN** that user's next login's access token includes the role's
  permissions

### Requirement: Password-based login issues an access and refresh token
The system SHALL, given a valid username and password, issue a short-lived
(10-15 minute) signed access token and a longer-lived signed refresh
token. The access token's claims SHALL include the union of permissions
from every role assigned to the user at the moment of login.

#### Scenario: Successful login
- **WHEN** a user logs in with a valid username and password
- **THEN** the system issues an access token and a refresh token, and the
  access token's permissions match the user's currently assigned roles

#### Scenario: Invalid credentials rejected
- **WHEN** a login attempt uses an incorrect password
- **THEN** the system rejects it and issues no tokens

### Requirement: Refreshing an access token
The system SHALL, given a valid, unexpired, unrevoked refresh token, issue
a new access token and a new refresh token, and revoke the previous
refresh token (rotation - a refresh token is single-use).

#### Scenario: Refreshing with a valid refresh token
- **WHEN** a client presents a valid, unrevoked refresh token
- **THEN** the system issues a new access/refresh token pair and the old
  refresh token no longer works

#### Scenario: Refreshing with an invalid refresh token
- **WHEN** a client presents an expired or revoked refresh token
- **THEN** the system rejects the request and issues no tokens

### Requirement: Token revocation
The system SHALL allow revoking a specific token by its `jti`. A revoked
token SHALL be rejected on every subsequent request, and revocation SHALL
take effect on every running instance without restarting any of them.

#### Scenario: A revoked token is rejected immediately
- **WHEN** a token is revoked and then presented on a request to the same
  instance that revoked it
- **THEN** the request is rejected

#### Scenario: Revocation propagates without a restart
- **WHEN** a token is revoked on one running instance
- **THEN** every other running instance rejects that token on its next
  request, without being restarted

### Requirement: Endpoint authentication and authorization
The system SHALL reject any request to a permission-gated endpoint that
does not present a valid, unexpired, unrevoked access token carrying the
permission that endpoint requires.

#### Scenario: Missing or invalid token rejected
- **WHEN** a request to a permission-gated endpoint has no token, an
  invalid token, or an expired token
- **THEN** the system rejects the request without processing it

#### Scenario: Valid token without the required permission rejected
- **WHEN** a request presents a valid token that lacks the endpoint's
  required permission
- **THEN** the system rejects the request

#### Scenario: Valid token with the required permission allowed
- **WHEN** a request presents a valid, unrevoked token carrying the
  endpoint's required permission
- **THEN** the system processes the request normally

### Requirement: Service tokens for machine clients
The system SHALL allow an administrator to issue a named service token
scoped to a fixed set of permissions, for non-interactive clients with no
user to log in as. A service token SHALL be verified through the same
authentication mechanism as a user access token.

#### Scenario: Issuing a service token
- **WHEN** an administrator issues a service token scoped to one or more
  permissions
- **THEN** the token is shown once and authenticates requests carrying
  those permissions

#### Scenario: A service token authenticates a machine client
- **WHEN** a non-interactive client presents a valid service token on a
  request to an endpoint requiring one of that token's permissions
- **THEN** the system processes the request normally

### Requirement: Console login page
The system SHALL provide a login page that authenticates a user, retains
the issued access token for subsequent requests, and refreshes it before
expiry without requiring the user to log in again mid-session. When OIDC
is configured, the login page SHALL also offer an OIDC login option
alongside the existing username/password form; when OIDC is not
configured, the login page SHALL show only the username/password form.

#### Scenario: Logging in through the UI
- **WHEN** a user submits valid credentials on the login page
- **THEN** they are able to use the console's other pages without being
  prompted to log in again until their session expires

#### Scenario: An expired session redirects to the login page
- **WHEN** a user's session has expired (refresh also failed or was not
  possible)
- **THEN** the console redirects them to the login page

#### Scenario: OIDC option shown only when configured
- **WHEN** a user opens the login page and the console has OIDC
  configured
- **THEN** the page offers an OIDC login option in addition to the
  username/password form

#### Scenario: OIDC option absent when not configured
- **WHEN** a user opens the login page and the console has no OIDC
  provider configured
- **THEN** the page shows only the username/password form

### Requirement: OIDC login as an alternative authentication method
The system SHALL support logging in via a single configured OpenID
Connect provider, using the Authorization Code flow with PKCE, as an
alternative to username/password login. OIDC SHALL be inactive unless
explicitly configured (issuer, client ID/secret, redirect URI).

#### Scenario: Successful OIDC login
- **WHEN** a user completes authentication with the configured OIDC
  provider and is redirected back to the console with a valid
  authorization code
- **THEN** the system exchanges the code, validates the ID token, and
  issues the console's own access and refresh tokens, exactly as
  username/password login does

#### Scenario: Invalid or tampered callback rejected
- **WHEN** an OIDC callback arrives with a state that doesn't match a
  login the console initiated, or an ID token that fails validation
- **THEN** the system rejects the callback and issues no tokens

### Requirement: OIDC login provisions a local user
The system SHALL, on a successful OIDC login, find or create a local user
record keyed by a stable identifier from the ID token, so the same person
authenticates to the same local user account on every subsequent OIDC
login.

#### Scenario: First-time OIDC login creates a user
- **WHEN** a person authenticates via OIDC whose identifier does not
  match any existing local user
- **THEN** the system creates a local user for them and proceeds with
  login

#### Scenario: Returning OIDC login reuses the same user
- **WHEN** a person authenticates via OIDC whose identifier matches an
  existing local user (previously created via OIDC)
- **THEN** the system logs them in as that same existing user rather than
  creating a duplicate

### Requirement: OIDC group-to-role reconciliation preserves manually assigned roles
The system SHALL, on every successful OIDC login, reconcile the user's
OIDC-originated role assignments against a configured claim (mapped to
local role names via a configured mapping): roles named by the user's
current claim values SHALL be assigned if not already, and roles
previously assigned by this same mapping but no longer named by the
user's current claim values SHALL be unassigned. Roles assigned to the
user by an administrator through the existing Users page SHALL never be
added or removed by this reconciliation.

#### Scenario: A new group grants a role on next login
- **WHEN** a user's identity provider claim gains a value that maps to a
  local role the user does not yet have
- **THEN** the system assigns that role to the user during their next
  OIDC login

#### Scenario: A removed group revokes the mapped role on next login
- **WHEN** a user's identity provider claim loses a value that previously
  mapped to a role assigned via OIDC reconciliation
- **THEN** the system unassigns that role from the user during their next
  OIDC login

#### Scenario: Manually assigned roles survive reconciliation
- **WHEN** an administrator has assigned a role to a user directly (not
  through OIDC claim mapping), and that user subsequently logs in via
  OIDC
- **THEN** that manually assigned role remains assigned regardless of the
  user's current OIDC claim values

### Requirement: Console UI for user management
The system SHALL provide a UI, restricted to users with `rbac:admin`, to
list users, create a user (username and password), assign or unassign
roles on a user, and delete a user.

#### Scenario: Viewing the user list
- **WHEN** an administrator opens the user management page
- **THEN** the system displays every configured user with their assigned
  roles

#### Scenario: Creating a user via the UI
- **WHEN** an administrator submits the create-user form with a username
  and password
- **THEN** the new user appears in the user list and can log in with those
  credentials

#### Scenario: Assigning a role via the UI
- **WHEN** an administrator assigns a role to a user from the user
  management page
- **THEN** that user's next login's access token includes the role's
  permissions

#### Scenario: Deleting a user via the UI
- **WHEN** an administrator deletes a user from the user management page
- **THEN** that user no longer appears in the user list and can no longer
  log in

### Requirement: Console UI for role management
The system SHALL provide a UI, restricted to users with `rbac:admin`, to
list roles, create a role (name and permission set), edit an existing
role's permissions, and delete a role.

#### Scenario: Viewing the role list
- **WHEN** an administrator opens the role management page
- **THEN** the system displays every configured role with its permission
  set

#### Scenario: Creating a role via the UI
- **WHEN** an administrator submits the create-role form with a name and
  one or more permissions
- **THEN** the new role appears in the role list and can be assigned to a
  user

#### Scenario: Editing a role's permissions via the UI
- **WHEN** an administrator changes an existing role's permission set
- **THEN** any user assigned that role has the updated permissions on
  their next login

#### Scenario: Deleting a role via the UI
- **WHEN** an administrator deletes a role from the role management page
- **THEN** that role no longer appears in the role list or in any user's
  assigned roles

### Requirement: Console UI for service token management
The system SHALL provide a UI, restricted to users with `rbac:admin`, to
list service tokens (name, permissions, creation date - never the token
value after creation), create a service token (name and permission set),
and delete (revoke) a service token.

#### Scenario: Viewing the service token list
- **WHEN** an administrator opens the service token management page
- **THEN** the system displays every service token's name, permissions,
  and creation date, without its token value

#### Scenario: Creating a service token via the UI
- **WHEN** an administrator submits the create-service-token form with a
  name and one or more permissions
- **THEN** the new token's value is displayed once, and the token
  authenticates requests carrying those permissions

#### Scenario: Deleting a service token via the UI
- **WHEN** an administrator deletes a service token from the service token
  management page
- **THEN** that token no longer appears in the list and is rejected on any
  subsequent request

### Requirement: RBAC changes publish an activity event
The system SHALL publish an activity event whenever a user, role, or
service token is created, updated, or deleted, whenever a role is
assigned to or unassigned from a user, and whenever OIDC login creates a
user or changes a user's OIDC-derived role assignments.

#### Scenario: Creating a user publishes an activity event
- **WHEN** an administrator creates a new user
- **THEN** an activity event recording the creation, its actor, and the
  affected user is published

### Requirement: JWT signing key rotation
The system SHALL support more than one currently-valid token verification key at once, each identified by a key ID (`kid`) carried in a token's header, while signing every newly issued token with exactly one designated active key. Rotating keys - introducing a new active signing key while a prior key remains valid for verification only - SHALL NOT invalidate tokens already issued under the prior key before its own validity ends.

#### Scenario: A token signed by a previous key still verifies
- **WHEN** a key rotation introduces a new active signing key while the previous key remains in the configured verification set
- **THEN** a token issued and signed under the previous key still verifies successfully until that key is removed from the verification set

#### Scenario: New tokens are signed by the current active key
- **WHEN** a token is issued after a key rotation
- **THEN** it is signed with the newly active key and carries that key's `kid`

#### Scenario: A key removed from the verification set no longer verifies
- **WHEN** a previously-valid key is removed from the configured verification set
- **THEN** a token whose `kid` names that key is rejected

#### Scenario: Creating or deleting a user publishes an event
- **WHEN** an administrator creates or deletes a user
- **THEN** an activity event recording the action, the affected user, and
  the acting administrator is published

#### Scenario: Creating, updating, or deleting a role publishes an event
- **WHEN** an administrator creates, updates, or deletes a role
- **THEN** an activity event recording the action, the affected role, and
  the acting administrator is published

#### Scenario: Assigning or unassigning a role publishes an event
- **WHEN** an administrator assigns or unassigns a role on a user
- **THEN** an activity event recording the grant or revocation, the user,
  the role, and the acting administrator is published

#### Scenario: Creating or revoking a service token publishes an event
- **WHEN** an administrator creates or revokes a service token
- **THEN** an activity event recording the action, the token's name, and
  the acting administrator is published

#### Scenario: OIDC login provisioning or role reconciliation publishes an event
- **WHEN** an OIDC login creates a new local user, or changes a user's
  OIDC-derived role assignments
- **THEN** an activity event recording what changed and the affected user
  is published, with the acting "user" recorded as the system rather than
  an administrator
