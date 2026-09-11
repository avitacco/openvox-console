## Why

Every account today is a locally-managed username/password. For teams that
already run an identity provider (Okta, Azure AD, Keycloak, etc.), that
means a second credential to provision and revoke by hand. OIDC login lets
the console federate to that existing identity provider instead, while
keeping local login and service tokens intact for accounts (like the
bootstrap admin) that don't come from an IdP.

Note: `architecture-summary.md` currently states "LDAP/SAML directory sync
for RBAC. Local user/role management only" as a v1 non-goal. This proposal
supersedes that non-goal specifically for OIDC federated login with a
narrow group-to-role mapping - not general directory sync of arbitrary
user attributes, which remains out of scope.

## What Changes

- Add OIDC (OpenID Connect) login as an alternative to local
  username/password login. The login page offers both; OIDC does not
  replace local login.
- Support exactly one configured OIDC provider (issuer URL, client
  ID/secret, redirect URI, scopes), configured via `CONSOLE_OIDC_*`
  env vars, matching the project's existing env-var config pattern.
  Standard Authorization Code flow with PKCE.
- On a successful OIDC login: find-or-create a local user keyed by a
  stable identifier from the ID token, then reconcile that user's role
  assignments against a configured claim (default `groups`) mapped to
  local role names via a static config-driven mapping
  (`CONSOLE_OIDC_ROLE_MAPPING`). Only role assignments that originated
  from this mapping are added/removed on each login; roles an admin
  assigned manually via the existing Users page are left untouched.
- After provisioning/reconciliation, issue the console's own
  access/refresh token pair exactly like password login does - every
  downstream mechanism (permission-gated endpoints, refresh, revocation,
  the frontend's token handling) is unchanged.
- **MODIFIED**: the `rbac` capability's "User account management"
  requirement, which currently states accounts are locally-managed only
  with no external directory sync - this no longer holds once OIDC is
  configured. The "Console login page" requirement is also modified to
  offer the OIDC option alongside the existing username/password form
  (this requirement lives in the `rbac` capability, not `web-shell` -
  the login page itself is `rbac`'s UI, while `web-shell` only covers
  the frontend's asset-embedding and page-shell mechanics).

## Capabilities

### New Capabilities
(none - OIDC login is additional behavior within the existing `rbac`
capability, not a separate one)

### Modified Capabilities
- `rbac`: adds OIDC login as an alternative authentication method,
  automatic user provisioning and OIDC-group-to-role reconciliation on
  OIDC login, revises "User account management" to reflect that accounts
  may now originate from OIDC as well as local creation, and revises
  "Console login page" to offer the OIDC option

## Impact

- New Postgres migration: an `oidc_login_state` table (state, PKCE
  verifier, nonce, expiry) for the redirect round-trip, and a way to mark
  which of a user's role assignments came from OIDC mapping (so
  reconciliation never touches admin-assigned roles).
- `internal/rbac`: new OIDC client/provider wiring
  (`golang.org/x/oauth2` + `github.com/coreos/go-oidc/v3`), new
  `POST /api/v1/auth/oidc/login` (redirect to provider) and
  `GET /api/v1/auth/oidc/callback` (exchange code, provision/reconcile,
  issue tokens) endpoints, and a `GET /api/v1/auth/methods` endpoint the
  frontend uses to know whether to show the OIDC option.
- `internal/runtime`: new `CONSOLE_OIDC_*` config fields, all optional -
  OIDC is inactive unless configured.
- `frontend/src/login.html`/`login.js`: an OIDC login button, shown only
  when `GET /api/v1/auth/methods` reports OIDC is configured.
- New Go module dependencies: `golang.org/x/oauth2`,
  `github.com/coreos/go-oidc/v3`.
