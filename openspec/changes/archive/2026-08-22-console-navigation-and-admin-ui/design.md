## Context

The frontend is plain ES modules with no bundler or SPA framework (see
phase-1's design.md) - each page is its own `.html` + `.js` pair sharing
`frontend/src/app.js` for token storage, refresh, and `fetchJSON`.
`app.js` already has a private `decodeJWTPayload()` used internally for
refresh scheduling; it's not exported. Every page already renders
`<vox-header site-title="OpenVox Console" href="/">` with an `actions`
slot holding the logout button; `vox-header`'s default slot is documented
for nav links (plain `<a>` tags, `aria-current="page"` on the current
one) but nothing uses it yet. The RBAC API
(`internal/rbac/handlers.go`) already implements full CRUD for users,
roles, and service tokens behind `rbac:admin` - see the `rbac` spec.

## Goals / Non-Goals

**Goals:**
- Reuse the existing page-per-file pattern and `app.js` helpers - no new
  frontend architecture.
- Client-side permission checks are a UX convenience (hide links, redirect
  away) - the server already enforces `rbac:admin` on every admin
  endpoint, so the frontend doesn't need to duplicate that enforcement
  correctness, only reflect it.

**Non-Goals:**
- No client-side routing/SPA shell - each admin page stays a separate
  `.html` file, consistent with `index.html`/`groups.html`/etc.
- No schema changes, and no new backend endpoints beyond the one gap
  found during implementation (see Decisions: "One new backend endpoint
  was needed after all") - everything else the RBAC API already
  supports.
- No general role/permission editor for *arbitrary* future permissions
  beyond the fixed set the backend already defines
  (`nodes:read`, `classifier:read`, `classifier:write`, `enc:read`,
  `rbac:admin`) - the permission picker just offers that fixed list.

## Decisions

**Nav links are static `<a>` tags per page, not a shared component.**
Each page already duplicates its own `<vox-header>` block (index.html,
node.html, groups.html, etc.) - adding the same two or three `<a>` tags
to each page's existing header follows that established duplication
pattern rather than introducing a shared-partial mechanism this codebase
doesn't have. `aria-current="page"` is set per-file since each file
already knows which page it is.

**Permission check is a decoded-claims read, not a new API call.**
`app.js` already decodes the access token's JWT payload for refresh
scheduling. Exporting that as `getClaims()` and adding
`hasPermission(permission)` (checks `claims.permissions.includes(...)`)
costs nothing extra over the network - the access token already carries
the effective permission set (see rbac spec: "the union of permissions
from every role assigned to the user at the moment of login"). This is
used both to decide whether to render the Admin nav link and, on the
three admin pages themselves, to redirect to `/` if the current token
lacks `rbac:admin` (defense in depth for UX - a direct URL visit without
the permission still gets a real 401/403 from the API either way).

**Three separate pages (users/roles/service-tokens), not one combined
admin page.** Matches the existing one-concern-per-page pattern
(`groups.html` vs `group.html` for list vs single-item forms). Each of
the three has different fields and different relationships (users have
roles; roles have permissions; service tokens have permissions but no
password/role concept), so a combined page would need as much
conditional UI as three separate ones without saving real code.

**Service token value is only ever shown from the create response.**
The backend's `POST /api/v1/service-tokens` returns the token value once
and the store never persists it in retrievable form (see rbac spec's
existing "Issuing a service token" scenario) - the new UI's create flow
displays that response's `token` field in a dismissable one-time panel,
matching the CLI/API's existing "shown once" behavior rather than
inventing a different disclosure model for the UI.

**One new backend endpoint was needed after all.** The RBAC API never
exposed which roles are assigned to a given user - only the flattened
permission-string union (`Store.UserPermissions`, used internally at
login, no HTTP handler) exists. The users page needs actual role
identities (id + name) to render and to drive
assign/unassign checkboxes. Added `GET /api/v1/users/{id}/roles` (admin
only) backed by a new `Store.ListUserRoles(userID)`, mirroring the
existing `rolePermissions()` query's join pattern
(`user_roles` -> `role_permissions`/`roles`). This is additive - no
existing endpoint's response shape changes.

## Risks / Trade-offs

- [Client-side nav/redirect gating could be seen as a security boundary
  by a future contributor] → Document in code comments (per this
  project's convention of explaining non-obvious constraints) that
  `rbac:admin` enforcement is the server's job; the frontend check is
  UX-only.
- [Duplicating nav markup across five+ HTML files risks drift if a page
  is added later and someone forgets the nav links] → Accepted: matches
  the project's existing per-page duplication (each page already repeats
  its own `<vox-header>`/`<vox-footer>` boilerplate); not worth a new
  templating mechanism for this project's current size.
