## Why

Phase 3 added RBAC login/logout to the existing pages and a login page, but
never gave the console primary navigation between its own pages, or any
frontend for the user/role/service-token management the RBAC API already
supports. Right now an administrator can only manage users, roles, and
service tokens via raw `curl` calls to the API, and there's no way to get
from the node inventory to the group management page (or back) without
typing a URL.

## What Changes

- Add primary navigation links (Nodes, Groups) to every existing page's
  `vox-header`, using its existing nav slot, marking the current page with
  `aria-current="page"`.
- Add an Admin nav link, visible only to users whose access token carries
  `rbac:admin`, leading to a new admin section.
- Add a users management page: list users, create a user (username +
  password), assign/unassign roles, delete a user.
- Add a roles management page: list roles, create a role (name + permission
  set), edit a role's permissions, delete a role.
- Add a service tokens management page: list service tokens (name,
  permissions, created date - never the token value after creation), create
  a service token (name + permission set, showing the token value once),
  delete (revoke) a service token.
- All three admin pages call the existing `/api/v1/users`, `/api/v1/roles`,
  and `/api/v1/service-tokens` endpoints, already gated behind `rbac:admin`
  server-side; the pages add client-side gating (redirect away if the
  current token lacks `rbac:admin`) as a UX convenience, not a security
  boundary the server doesn't already enforce.

## Capabilities

### New Capabilities
(none - this extends existing capabilities)

### Modified Capabilities
- `web-shell`: adds primary navigation across pages
- `rbac`: adds console UI for user, role, and service token management

## Impact

- `frontend/src/*.html`: every existing page's `vox-header` gets nav links.
- `frontend/src/users.html`, `users.js`, `roles.html`, `roles.js`,
  `service-tokens.html`, `service-tokens.js`: new admin pages.
- `frontend/src/app.js`: likely needs a small helper to check the current
  token's permissions client-side (for nav visibility and page-level
  redirect), reusing the existing token/claims handling already there.
- No backend changes - the RBAC API (`internal/rbac/handlers.go`) already
  implements everything these pages need.
