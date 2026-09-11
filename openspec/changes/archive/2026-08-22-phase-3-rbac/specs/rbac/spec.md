## Purpose

Gates access to every capability in the console: local user/role/permission
management, JWT-based login and session refresh, fast token revocation,
and the authentication/authorization mechanism every other capability's
endpoints depend on.

## ADDED Requirements

### Requirement: User account management
The system SHALL allow creating, viewing, updating, and deleting local
user accounts, each with a username and password. There is no external
directory sync (LDAP/SAML) - accounts are managed locally.

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
expiry without requiring the user to log in again mid-session.

#### Scenario: Logging in through the UI
- **WHEN** a user submits valid credentials on the login page
- **THEN** they are able to use the console's other pages without being
  prompted to log in again until their session expires

#### Scenario: An expired session redirects to the login page
- **WHEN** a user's session has expired (refresh also failed or was not
  possible)
- **THEN** the console redirects them to the login page
