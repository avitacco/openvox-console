## MODIFIED Requirements

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

## ADDED Requirements

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
