## Context

See `proposal.md` for motivation and the `rbac` spec (plus the small
deltas on `inventory`/`reporting`/`classifier`/`enc-api`) for requirements.
architecture-summary.md already fixes several of this phase's decisions:
signed (JWS), not encrypted (JWE), ES256 asymmetric signing, 10-15 minute
access token expiry, `jti` revocation kept warm via NATS rather than a
per-request database check, and `golang-jwt/jwt` as the library (don't
hand-roll token parsing).

## Goals / Non-Goals

**Goals:**
- Gate every existing endpoint (inventory, reporting, classifier, ENC)
  and every endpoint from here on, with zero per-request database calls
  on the hot path for either signature verification or revocation checks.
- Give `enc-bridge` (a machine client with no user to log in as) a real
  way to authenticate, through the same verification mechanism as
  everything else.

**Non-Goals:**
- No LDAP/SAML directory sync - stated project-level non-goal, local
  accounts only.
- No encrypted (JWE) tokens - stated project-level non-goal.
- No live permission lookup per request. A token's permissions are the
  user's role permissions *as of login*, embedded in the token claims.
  Changing a user's roles takes effect on their next login/refresh, or
  immediately only if the token is also explicitly revoked. This is a
  standard, accepted JWT trade-off, and the short expiry already bounds
  how long stale permissions can persist.
- No gating of the embedded frontend's static assets (HTML/JS/CSS) - only
  API calls are gated. The app shell isn't sensitive; data is.
- No auth on `/health` - infrastructure/load-balancer probes need it to
  stay reachable without credentials.

## Decisions

**Permissions are embedded in the access token at issuance, not looked up
per request.** This is what makes "no per-request database check" actually
true for authorization, not just for revocation. At login (or refresh),
the system computes the union of permissions across all of the user's
currently-assigned roles and writes that set directly into the JWT's
claims. Verifying a request becomes: check the signature, check the `jti`
isn't in the in-memory revoked set, read permissions straight off the
already-verified claims. Alternative considered: look up permissions from
Postgres on every request - rejected, since it reintroduces exactly the
per-request database dependency the revocation design already avoids for
a different reason; there's no reason to hold that line for revocation and
drop it for authorization.

**Refresh tokens are also JWTs, longer-lived, rotated on use.** Rather
than a separate opaque-token subsystem, a refresh token is a signed JWT
with `type: "refresh"` and its own `jti`, verified through the same
signature/revocation path as an access token, just with a longer expiry
(hours, not minutes) and only accepted at `/api/v1/auth/refresh`. Using it
issues a new access/refresh pair and revokes the old refresh token's
`jti` (rotation - a stolen refresh token that's already been used becomes
useless). Alternative considered: opaque, database-stored refresh tokens
- rejected because it means building a second token mechanism instead of
reusing the one this phase already has to build correctly.

**Revocation: Postgres is the source of truth, NATS is the propagation
layer.** A revoked `jti` (with its natural expiry, for garbage collection)
is written to Postgres first, then published on a NATS subject every
running instance subscribes to, updating its own in-memory revoked-set
immediately. On startup, an instance loads any still-unexpired revoked
`jti`s from Postgres into memory, so a restart doesn't silently undo a
revocation. This is exactly what architecture-summary.md's "`jti`-based
revocation list... kept warm via NATS-published revocation events rather
than a database check per request" describes: the database is for
durability and cold start, NATS is for hot-path-free propagation to
already-running instances.

**Service tokens reuse the same token format and verification path,
differ only in issuance.** A service token is a JWT with a fixed
permission set baked in at issuance time (by an admin action, not a
login), typically with a long or no expiry, revocable exactly like any
other token via its `jti`. `enc-bridge` is configured with one (a new env
var) and sends it as a bearer token on its ENC request. Alternative
considered: a separate mTLS-based service-auth mechanism - rejected as
more machinery than needed when the existing JWT verification path
already does everything required (identity, permission scope,
revocability); mTLS would be justified if service identity itself needed
strong cryptographic proof beyond "holds a valid signed token," which
isn't the bar here.

**Middleware composition: each capability's `Register` declares its own
required permission per route, wrapped by a shared `rbac.Authorize`
helper passed in from `main.go`.** Rather than a central table mapping
routes to permissions (which drifts from the routes themselves), each
package's existing `Register(mux)` method becomes
`Register(mux, authorize func(permission string, h http.HandlerFunc) http.HandlerFunc)`,
and each route registration wraps its handler with
`authorize("nodes:read", h.listNodes)`. `main.go` constructs the
`authorize` closure once, backed by the token-verification middleware
already applied globally (which parses/verifies the token and puts claims
on the request context - `authorize` just checks the claims for the named
permission). This keeps "what permission does this route need" visible
next to the route itself, not in a file that has to be kept in sync with
routing changes elsewhere.

**Password hashing: bcrypt.** Established, standard library-adjacent
(`golang.org/x/crypto/bcrypt`), no parameters to get wrong for a v1 local
account system. Argon2id would be the more modern choice for very
high-value deployments; bcrypt is the well-trodden default and consistent
with "don't hand-roll, don't over-engineer" for a first pass.

## Risks / Trade-offs

- **[Risk]** Embedding permissions in the token means a permission change
  isn't instant. → **Mitigation**: bounded by the 10-15 minute access
  token expiry; an admin who needs a change to take effect immediately can
  revoke the affected user's current tokens, which *is* instant (NATS
  propagation, no restart).
- **[Risk]** `enc-bridge`'s service token is a long-lived credential
  mounted into the `openvoxserver` container. → **Mitigation**: revocable
  the same way any token is, and scoped to exactly one permission
  (`enc:read`) - a compromise of that credential doesn't grant anything
  beyond what `enc-bridge` itself needs.
- **[Risk]** Refactoring every existing `Register(mux)` signature touches
  four already-shipped packages (`inventory`, `reporting`, `classifier`,
  `enc-api`). → **Mitigation**: mechanical, well-scoped change (add a
  parameter, wrap each existing route registration) with full test
  coverage already in place for each package to catch regressions.

## Migration Plan

New Postgres migration adding users, roles, permissions, a user-role
join table, and a revoked-tokens table to the console's shared schema -
no data migration, fresh tables. Rollout: merge, apply migrations, create
a first admin user (a one-time bootstrap task - see tasks.md), issue
`enc-bridge` a service token and set it in its environment, redeploy
`enc-bridge`'s configuration, confirm existing endpoints now reject
unauthenticated requests and accept authenticated ones.

## Open Questions

- Whether the first admin user is bootstrapped via a CLI flag/command or
  a one-time setup endpoint - an implementation detail of tasks.md's
  bootstrap task, doesn't change any spec requirement.
- Exact refresh token lifetime (a few hours vs. a day) - doesn't change
  the specs (which only fix that it's "longer-lived" and revocable), safe
  to tune during implementation.
