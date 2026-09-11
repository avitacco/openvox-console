## Context

See proposal.md for motivation. The existing `rbac` package
(`internal/rbac`) already implements password login, access/refresh
token issuance (ES256 JWTs via `golang-jwt/jwt/v5`), revocation, and
service tokens - see `internal/rbac/auth.go`, `tokens.go`, `revoke.go`.
`AuthService.Login`/`Refresh` both end by calling a private `issuePair`
helper that issues an access+refresh pair carrying the user's current
`UserPermissions(ctx, userID)`. The `users` table
(`internal/persistence/migrations/000006_users.up.sql`) requires a
`password_hash`; `user_roles` (`000008_user_roles.up.sql`) is a plain
`(user_id, role_id)` join table with no provenance tracking. Config is
loaded via `internal/runtime.Config`/`LoadConfig`, one `CONSOLE_*` env
var per field, required fields enforced at startup
(`internal/runtime/config.go`). `cmd/console/main.go` wires every
`rbac` component together and registers `rbacHandlers.Register(mux)`
unconditionally.

## Goals / Non-Goals

**Goals:**
- OIDC login reuses the existing token issuance, refresh, revocation, and
  permission-gating mechanisms unchanged - it only changes how a user
  gets to holding a valid console access/refresh token pair, not what
  that pair is or how it's checked afterward.
- OIDC is entirely optional and inert when unconfigured: no schema
  behavior change, no new required env vars, existing password login and
  service tokens keep working exactly as before.
- A single static claim-to-role mapping, evaluated fresh on every OIDC
  login, without ever touching roles an admin assigned by hand.

**Non-Goals:**
- Multiple simultaneous OIDC providers, or per-provider configuration.
- General directory sync of arbitrary user attributes (display name,
  phone, org unit, etc.) - only the identifier used for account matching
  and the one claim used for role mapping are read from the ID token.
- Refreshing against the OIDC provider itself after login (no stored
  provider refresh token, no periodic re-check of group membership
  between logins) - reconciliation happens at login time only, matching
  how the console already treats permissions as fixed for the life of an
  access token.
- An admin UI for editing the claim-to-role mapping - it's static
  configuration (env var), like the OIDC client registration itself.
  A future change could move this into the database with a UI, but nothing
  in this design blocks that later.

## Decisions

**Discovery/config failure at startup is logged, not fatal.** Unlike
Postgres (already "log and keep running" per the service-runtime spec),
if `CONSOLE_OIDC_ISSUER` is set but the provider's discovery document
can't be fetched at startup, the console logs an error and starts with
OIDC inactive (`GET /api/v1/auth/methods` reports `oidc: false`, and the
login/callback endpoints return 503) rather than refusing to start.
Local login and service tokens must never be held hostage by a
transient identity-provider outage or a misconfigured issuer URL found
after the fact.

**`sub` is the permanent lookup key; a separate configurable claim
supplies the display username.** Email/preferred-username can change at
the provider; `sub` per the OIDC spec does not. A new nullable, unique
`users.oidc_subject` column stores it. `CONSOLE_OIDC_USERNAME_CLAIM`
(default `email`) picks which claim seeds the human-readable `username`
column *only at first provisioning* - it is not re-synced on later
logins, so a renamed/changed claim value doesn't collide with another
account or silently rewrite a username an admin may have since changed.
This is a real limitation (display username can go stale) accepted for
simplicity; documented in Risks below.

**OIDC-provisioned users get an unusable random password hash, not a
nullable column.** Making `password_hash` nullable and special-casing
`NULL` through `AuthService.Login`'s bcrypt comparison touches
security-sensitive code for a marginal schema simplification. Instead,
`CreateOIDCUser` hashes a random UUID nobody knows and stores that -
password login for that account fails the same way a wrong password
always does, no special-casing needed anywhere else.

**Role provenance is tracked per-assignment, not per-user.** A user can
hold both admin-assigned and OIDC-mapped roles simultaneously (e.g. an
IdP group grants `viewer` while an admin separately grants `rbac:admin`
by hand). `user_roles` gets an `assigned_via` column
(`'manual'` default, or `'oidc'`). Reconciliation on OIDC login:
1. Compute `desired` = role IDs from the current ID token's mapped claim
   values.
2. Insert `(user_id, role_id, 'oidc')` for any `desired` role the user
   doesn't already hold, `ON CONFLICT (user_id, role_id) DO NOTHING` -
   if a `manual` row already grants that role, it's left untouched and
   still reads `manual`.
3. Delete `user_roles` rows for this user where `assigned_via = 'oidc'`
   and `role_id NOT IN desired` - only ever removes rows this same
   mapping put there.

A role that is both manually assigned and currently claim-mapped is
never removed by reconciliation, satisfying "manually assigned roles
survive reconciliation" regardless of which happened first.

**Token delivery after the OIDC callback uses a URL fragment, not a
cookie or new storage model.** The console's frontend already keeps
tokens in `localStorage` and attaches them as a bearer header
(`frontend/src/app.js`); switching to cookies for OIDC only would mean
two different session models. Instead,
`GET /api/v1/auth/oidc/callback` finishes the exchange server-side and
302s to `/oidc-callback.html#access_token=...&refresh_token=...` - the
fragment never reaches the server (not logged, not sent in the
`Referer` header), and a small `oidc-callback.js` reads
`location.hash`, calls the existing `setTokens()`, clears the fragment,
and redirects to `/`, exactly like `login.js` does after a password
login.

**`issuePair` becomes a shared package-level function.** Both password
login/refresh and OIDC login need to turn "a user + their current
permissions" into a token pair via the same `Issuer`. Rather than
duplicate `AuthService`'s two `IssueToken` calls in a new OIDC service,
`issuePair(issuer *Issuer, subject string, permissions []string)
(TokenPair, error)` moves to a package-level function in `internal/rbac`
that both `AuthService` and the new `OIDCService` call.

**PKCE state lives in Postgres, not in-memory or a signed cookie.** The
architecture favors stateless services reading from Postgres over
in-process state for failover simplicity (see architecture-summary.md).
A new `oidc_login_state` table (`state` primary key, `pkce_verifier`,
`nonce`, `expires_at`) means the login-initiation request and the
callback request can land on different console instances behind a load
balancer. Each row is deleted on first use (single-use, like a refresh
token) and expired rows are opportunistically swept when a new one is
inserted.

**Libraries: `golang.org/x/oauth2` + `github.com/coreos/go-oidc/v3`.**
Both are the de facto standard Go OIDC stack (go-oidc wraps discovery,
JWKS fetching/caching, and ID token verification on top of x/oauth2's
Authorization Code + PKCE support) - writing JWKS fetching/caching and
ID token validation by hand would duplicate exactly the kind of
security-sensitive code `golang-jwt/jwt/v5` already saves this project
from writing for its own access/refresh tokens.

**Claims are read from the ID token only, not a supplemental userinfo
call.** go-oidc's own documentation recommends this (the ID token is
already signature-verified; a second userinfo round trip adds latency
and a second thing to trust). This does mean the identity provider must
be configured to actually include the username-seeding and role-mapping
claims in the ID token itself, not only via userinfo - some providers
(including this change's own test provider, Duende IdentityServer-based)
withhold non-essential claims from the ID token by default when an
access token is also issued, requiring an explicit
`AlwaysIncludeUserClaimsInIdToken` (or equivalent) client setting.
Documented in README.md's OIDC setup section as a real-world
configuration requirement, not something the console works around code-side.

## Risks / Trade-offs

- [Display username can go stale after the first OIDC login if the
  seeding claim's value changes at the provider] → Accepted; an admin
  can still rename the user via the existing Users page. `oidc_subject`,
  not `username`, is the actual identity used for repeat-login matching,
  so this is cosmetic, not a security or access issue.
- [A user removed from every mapped IdP group loses all OIDC-derived
  roles on their next login, possibly down to zero permissions, with no
  warning] → Matches the spec's explicit requirement; an admin can
  always grant a manual role as a safety net for a specific account.
- [Group/role reconciliation only happens at login time, not
  continuously] → Explicit Non-Goal; consistent with how the console
  already treats a live access token's permissions as fixed until its
  next refresh.
- [Adding two new external dependencies (`x/oauth2`, `go-oidc`)] →
  Both are widely used, actively maintained, and narrowly scoped to
  exactly this problem; the alternative is hand-rolling OIDC token
  validation, a worse security trade-off.
