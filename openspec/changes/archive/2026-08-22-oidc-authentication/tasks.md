## 1. Schema

- [x] 1.1 Add a migration adding nullable, unique `users.oidc_subject`
      and verify it applies cleanly, is a no-op on rerun, and a duplicate
      `oidc_subject` insert is rejected
- [x] 1.2 Add a migration adding `user_roles.assigned_via` (`'manual'`
      default, CHECK IN `('manual','oidc')`) and verify existing rows
      backfill to `'manual'` and the constraint rejects any other value
- [x] 1.3 Add a migration for `oidc_login_state` (`state` primary key,
      `pkce_verifier`, `nonce`, `expires_at`) and verify round-trip
      storage and that inserting a duplicate `state` is rejected

## 2. rbac: OIDC user provisioning

- [x] 2.1 Implement `Store.GetUserByOIDCSubject(ctx, subject)` and
      `Store.CreateOIDCUser(ctx, subject, username)` (stores a random,
      never-derivable password hash - see design.md), and verify against
      real Postgres: creating one, fetching it back by subject, and that
      a second `CreateOIDCUser` with the same subject is rejected
- [x] 2.2 Implement `Store.GetRoleByName(ctx, name)` and verify it
      against real Postgres, including the not-found case

## 3. rbac: OIDC role reconciliation

- [x] 3.1 Implement the reconciliation function from design.md
      (`assigned_via='oidc'` insert-if-desired /
      delete-if-no-longer-desired, only ever touching `'oidc'` rows),
      and verify against real Postgres: a role present in the desired
      set gets assigned, a previously oidc-assigned role absent from the
      desired set gets unassigned, and a manually-assigned role (
      `assigned_via='manual'`) is untouched whether or not it's in the
      desired set
- [x] 3.2 Extract the shared `issuePair` package-level function (see
      design.md) used by both `AuthService` and the new OIDC service,
      and verify `internal/rbac`'s existing tests still pass unchanged
      (pure refactor, no behavior change)

## 4. rbac: OIDC client and provider discovery

- [x] 4.1 Add `CONSOLE_OIDC_ISSUER`, `CONSOLE_OIDC_CLIENT_ID`,
      `CONSOLE_OIDC_CLIENT_SECRET`, `CONSOLE_OIDC_REDIRECT_URL`,
      `CONSOLE_OIDC_SCOPES` (default `openid profile email groups`),
      `CONSOLE_OIDC_USERNAME_CLAIM` (default `email`),
      `CONSOLE_OIDC_ROLE_CLAIM` (default `groups`), and
      `CONSOLE_OIDC_ROLE_MAPPING` (JSON object, claim-value -> role-name)
      to `internal/runtime.Config`, all optional, and verify the console
      starts with OIDC inactive when none are set
- [x] 4.2 Implement `rbac.NewOIDCService(ctx, cfg)` performing provider
      discovery (`go-oidc`) when configured, logging and returning an
      inactive service (not a fatal error) on discovery failure per
      design.md, and verify both paths: a real (test) issuer succeeds,
      an unreachable/invalid issuer URL logs and leaves the service
      inactive rather than panicking or exiting

## 5. rbac: OIDC login and callback endpoints

- [x] 5.1 Implement `GET /api/v1/auth/oidc/login`: generate state, PKCE
      verifier/challenge, and nonce; persist them; redirect to the
      provider's authorization endpoint, and verify via a real HTTP
      request that the redirect's query parameters are well-formed
      (client_id, redirect_uri, response_type=code, code_challenge,
      code_challenge_method=S256, state, scope) and the state row exists
      in Postgres
- [x] 5.2 Implement `GET /api/v1/auth/oidc/callback`: look up and
      single-use-delete the state row, exchange the code, verify the ID
      token (issuer, audience, nonce, expiry), provision/reconcile the
      user (tasks 2-3), issue the console's own token pair, and redirect
      to `/oidc-callback.html#access_token=...&refresh_token=...`, and
      verify end to end against a real OIDC test provider (see task 6):
      completing a real browser-driven authorization flow yields a
      working console session
- [x] 5.3 Verify rejection cases against the real endpoint: a callback
      with an unknown/expired state is rejected without issuing tokens,
      and a request to either OIDC endpoint when OIDC is not configured
      returns 503
- [x] 5.4 Implement `GET /api/v1/auth/methods` (no auth required)
      returning which login methods are available, and verify it
      reports `oidc: false` when unconfigured and `oidc: true` once a
      real test provider is wired up

## 6. Dev: real OIDC test provider

- [x] 6.1 Add a real OIDC test provider (e.g. `oidc-server-mock`, chosen
      for straightforward per-user custom claims including a `groups`
      array) to `docker-compose.yml` under a new `oidc` profile, with at
      least one static test user carrying a `groups` claim, and verify
      via `make oidc-up` (or equivalent) plus a direct request to its
      `/.well-known/openid-configuration` that it's a real, spec-compliant
      discovery document
- [x] 6.2 Document the local dev setup (env vars pointing at the test
      provider, test user credentials) in README.md

## 7. Frontend: OIDC login option

- [x] 7.1 Add an "OIDC login" option to `login.html`/`login.js`, shown
      only when `GET /api/v1/auth/methods` reports `oidc: true`, linking
      to `/api/v1/auth/oidc/login`, and verify via browser: with OIDC
      configured the option appears; with it unconfigured only the
      password form appears
- [x] 7.2 Add `oidc-callback.html`/`oidc-callback.js` reading the
      fragment tokens, calling the existing `setTokens()`, and
      redirecting to `/`, and verify via a real browser-driven login
      through the test provider (task 6) that the session ends up fully
      authenticated (a protected page loads real data, matching the
      existing password-login browser tests' pattern)

## 8. Final verification

- [x] 8.1 Verify, via a real browser-driven OIDC login against the test
      provider, the full provisioning/reconciliation story: first login
      creates a new local user with the mapped role(s); logging out,
      changing the test user's `groups` claim in the test provider
      config, and logging in again updates the assigned role(s)
      accordingly
- [x] 8.2 Verify a manually-assigned role (granted via the existing
      Users page to an OIDC-provisioned user) survives repeated OIDC
      logins regardless of the test user's `groups` claim
- [x] 8.3 Verify local username/password login, service tokens, and
      every existing permission-gated endpoint are unaffected (spot
      -check a representative few) - OIDC is additive, not a replacement
- [x] 8.4 `gofmt -l .`, `go vet ./...`, `make test` all pass with no
      regressions
