## 1. Schema: users, roles, permissions, revocation

- [x] 1.1 Add a migration for users (username, bcrypt password hash) and
      verify it applies cleanly and is a no-op on rerun
- [x] 1.2 Add migrations for roles and permissions (a role has a name and
      a set of permission strings) and verify round-trip storage
- [x] 1.3 Add a migration for the user-role join table and verify a user
      can have multiple roles
- [x] 1.4 Add a migration for revoked tokens (jti, expires_at) and verify
      round-trip storage

## 2. rbac: user/role/permission store

- [x] 2.1 Implement user CRUD (create with bcrypt-hashed password, read,
      update, delete) and verify against a real Postgres instance
      (`make up`)
- [x] 2.2 Implement role CRUD with an associated permission set and verify
      round-trip storage
- [x] 2.3 Implement assigning/unassigning roles to a user and verify
      computing a user's effective permission set (union across all
      assigned roles)

## 3. rbac: JWT issuance

- [x] 3.1 Add ES256 signing key configuration (key file paths) to
      `internal/runtime.Config` and verify startup fails clearly if
      required and missing
- [x] 3.2 Implement password-based login: verify username/password via
      bcrypt, and verify an incorrect password is rejected without
      revealing whether the username exists
- [x] 3.3 Implement access token issuance on successful login, embedding
      the user's effective permissions (from 2.3) in the claims, and
      verify the issued token's claims match the user's current roles
- [x] 3.4 Implement refresh token issuance alongside the access token
      (longer expiry, `type: "refresh"` claim, its own `jti`) and verify
      both tokens are valid, parseable JWTs

## 4. rbac: refresh and rotation

- [x] 4.1 Implement `POST /api/v1/auth/refresh`: verify the refresh token,
      issue a new access/refresh pair, and revoke the previous refresh
      token's `jti`, and verify the old refresh token no longer works
      after use
- [x] 4.2 Verify an expired or already-revoked refresh token is rejected

## 5. rbac: revocation

- [x] 5.1 Implement revoking a token by `jti` (persisted to Postgres with
      its natural expiry) and verify it's stored
- [x] 5.2 Implement an in-memory revoked-`jti` set updated via NATS
      publish/subscribe, and verify: revoking on one `Bus` subscriber is
      observed by a second independent subscriber without a restart
- [x] 5.3 Implement loading unexpired revoked `jti`s from Postgres into
      the in-memory set at startup, and verify a previously-revoked,
      still-unexpired token is rejected immediately after a fresh start
- [x] 5.4 Verify already-expired revoked entries are not loaded into
      memory at startup (they're irrelevant - the token itself would
      already fail expiry validation)

## 6. rbac: middleware and authorization

- [x] 6.1 Implement token verification middleware (signature, expiry,
      revocation; sets claims on the request context) and verify: valid
      token passes through; missing, malformed, expired, and revoked
      tokens are all rejected
- [x] 6.2 Implement an `Authorize(permission, handler)` helper checking
      the context's claims for the named permission, and verify: token
      without the permission is rejected (403); token with the permission
      invokes the wrapped handler
- [x] 6.3 Change `inventory`, `reporting`, `classifier`, and `enc-api`'s
      `Register(mux)` signatures to accept the `authorize` function, wrap
      each route with its real required permission (`nodes:read`,
      `classifier:read`/`classifier:write`, `enc:read`), and verify via
      real HTTP requests that every existing endpoint now enforces its
      permission

## 7. rbac: service tokens

- [x] 7.1 Implement admin-issued service tokens (fixed permission set,
      shown once) and verify a real request authenticates using only a
      service token, no user login involved
- [x] 7.2 Verify a service token is revocable through the same mechanism
      as a user token

## 8. Frontend: login and session handling

- [x] 8.1 Add the login page and verify submitting valid credentials
      stores the access/refresh tokens and redirects into the console
- [x] 8.2 Wire every existing page's `fetch()` calls to attach the access
      token as a bearer token, and verify each existing page (nodes,
      node detail, reports, events, groups) still works end to end now
      that its API calls are authenticated
- [x] 8.3 Implement automatic refresh before access token expiry and
      verify a session survives past the access token's original expiry
      without the user being prompted to log in again
- [x] 8.4 Verify a session that fails to refresh (expired/revoked refresh
      token) redirects to the login page rather than showing broken pages

## 9. Bootstrap and integration verification

- [x] 9.1 Add a first-admin bootstrap mechanism for a fresh database
      (documented in README.md) and verify a clean database can get a
      working first admin user
- [x] 9.2 Issue `enc-bridge` a service token scoped to `enc:read`,
      configure it (new env var), and verify via `make openvox-test` that
      real classification still works end to end through the now-gated
      ENC endpoint
- [x] 9.3 Verify, with real HTTP requests against the running console,
      that every existing capability's endpoints (nodes, reports, events,
      groups, enc) reject unauthenticated requests and accept
      correctly-scoped authenticated ones, and that revoking a token takes
      effect without restarting the console (this phase's exit criteria)
