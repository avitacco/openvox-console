## Why

Every endpoint built so far (inventory, reporting, classifier, ENC) is
wide open. Phase 3 adds access control gating everything already built
and everything after, per architecture-summary.md's RBAC design: signed
(not encrypted) short-lived JWTs with fast `jti`-based revocation. This
needs to land before any component is exposed outside a trusted dev
environment.

## What Changes

- Add user/role/permission data model and CRUD (local accounts only - no
  LDAP/SAML sync, a stated v1 non-goal).
- Add token issuance: signed JWTs (ES256), short expiry (10-15 min), with
  a refresh flow (a longer-lived, also-revocable refresh token).
- Add `jti`-based revocation: persisted in Postgres (source of truth,
  survives restarts) and propagated to every running instance's in-memory
  revoked set via NATS, so checking revocation never needs a database call
  on the request hot path.
- Add auth middleware, applied to every existing endpoint
  (inventory, reporting, classifier, enc-api) and every endpoint from here
  on. Permissions are embedded in the token at issuance (the user's
  flattened role permissions at login time), not looked up per request.
- Add service tokens: a separate, admin-issued, non-interactive token kind
  for machine clients - specifically `enc-bridge`, which has no user to
  log in as. Verified through the same middleware and revocation
  mechanism as user tokens, just issued differently (see design.md).
- Add a console login page (and wire the frontend to attach the access
  token to API calls and refresh it before expiry).

## Capabilities

### New Capabilities
- `rbac`: user/role/permission CRUD, JWT issuance/refresh, `jti`
  revocation, the auth middleware and permission-checking every other
  capability's endpoints now depend on, service tokens, and the console
  login page.

### Modified Capabilities
- `inventory`: its endpoints now require a valid access token with the
  `nodes:read` permission.
- `reporting`: its endpoints now require a valid access token with the
  `nodes:read` permission.
- `classifier`: its read endpoints now require `classifier:read`; its
  write endpoints (create/update/delete) now require `classifier:write`.
- `enc-api`: the ENC endpoint now requires a valid access token with the
  `enc:read` permission - issued to `enc-bridge` as a service token, not
  a user login.

## Impact

- New Go package (`rbac`) plus new middleware composition in every
  existing HTTP-registering package's wiring in `cmd/console/main.go`.
- New Postgres tables (users, roles, permissions, user-role assignments,
  revoked tokens) in the console's shared schema.
- New dependency: `golang-jwt/jwt` for JWS signing/verification (per
  architecture-summary.md - do not hand-roll), `golang.org/x/crypto/bcrypt`
  for password hashing.
- `enc-bridge` needs a configured service token (new env var) to keep
  authenticating to the now-gated ENC endpoint.
- `/health` stays unauthenticated (infra/load-balancer probing); embedded
  frontend static assets stay unauthenticated (only API calls are gated,
  the app shell itself is not sensitive) - both are explicit scope
  decisions, see design.md.
- Breaking change, **BREAKING**: every existing API endpoint (nodes,
  reports, events, groups, enc) now rejects unauthenticated requests that
  previously succeeded.
