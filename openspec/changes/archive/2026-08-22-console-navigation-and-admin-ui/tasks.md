## 1. Shared helpers for client-side permission checks

- [x] 1.1 Export `getClaims()` from `app.js` (the existing private
      `decodeJWTPayload()` applied to the current access token) and
      `hasPermission(permission)`, and verify with a unit-style check (or
      browser console) that a decoded token's permissions are read
      correctly, including when there is no token (returns false, not a
      throw)

## 2. Primary navigation

- [x] 2.1 Add Nodes/Groups nav links (with `aria-current="page"` on the
      current page) to every existing page's `vox-header`
      (index/node/report/groups/group.html), and verify via browser that
      navigating between Nodes and Groups works from every page and the
      current page is visually marked
- [x] 2.2 Add an Admin nav link, rendered only when `hasPermission('rbac:admin')`
      is true, and verify via browser with both an admin-permission token
      and a token lacking it that the link only appears for the former

## 3. Users management page

- [x] 3.1 Add `GET /api/v1/users/{id}/roles` (admin-only) backed by a new
      `Store.ListUserRoles(userID)`, since no existing endpoint exposes a
      user's assigned roles (design.md: "One new backend endpoint was
      needed after all"), and verify via curl against a real Postgres
      instance that it returns the correct roles for a user with
      multiple assigned roles and an empty list for a user with none
- [x] 3.2 Add `users.html`/`users.js` listing all users (username,
      assigned roles fetched per-user via the new endpoint) via
      `GET /api/v1/users`, gated by client-side `rbac:admin` redirect,
      and verify via browser it renders real users and their real
      assigned roles from a live console
- [x] 3.3 Add a create-user form (username, password) calling
      `POST /api/v1/users`, and verify via browser a newly created user
      appears in the list and can log in with those credentials
- [x] 3.4 Add role assignment/unassignment on the user list (calling
      `POST`/`DELETE /api/v1/users/{id}/roles/{roleId}`), and verify via
      browser assigning a role updates what's displayed and the user's
      next login's access token includes that role's permissions
- [x] 3.5 Add a delete-user action calling `DELETE /api/v1/users/{id}`,
      and verify via browser the user disappears from the list and can no
      longer log in

## 4. Roles management page

- [x] 4.1 Add `roles.html`/`roles.js` listing all roles (name,
      permissions) via `GET /api/v1/roles`, gated by client-side
      `rbac:admin` redirect, and verify via browser it renders real roles
      from a live console
- [x] 4.2 Add a create-role form (name, permission checkboxes from the
      fixed permission set) calling `POST /api/v1/roles`, and verify via
      browser a newly created role appears in the list and can be
      assigned to a user
- [x] 4.3 Add an edit-permissions action calling `PUT /api/v1/roles/{id}`,
      and verify via browser that changing a role's permissions is
      reflected in a user assigned that role's next-login access token
- [x] 4.4 Add a delete-role action calling `DELETE /api/v1/roles/{id}`,
      and verify via browser the role disappears from the list and from
      any user it was assigned to

## 5. Service tokens management page

- [x] 5.1 Add `service-tokens.html`/`service-tokens.js` listing all
      service tokens (name, permissions, created date, no token value)
      via `GET /api/v1/service-tokens`, gated by client-side `rbac:admin`
      redirect, and verify via browser it renders real service tokens
      from a live console
- [x] 5.2 Add a create-service-token form (name, permission checkboxes)
      calling `POST /api/v1/service-tokens`, displaying the returned
      token value once in a dismissable panel, and verify via browser the
      shown token value actually authenticates a request carrying its
      permissions
- [x] 5.3 Add a delete-service-token action calling
      `DELETE /api/v1/service-tokens/{id}`, and verify via browser the
      token disappears from the list and is rejected on a subsequent
      request

## 6. Final verification

- [x] 6.1 Verify, with a token lacking `rbac:admin`, that direct
      navigation to `/users.html`, `/roles.html`, and
      `/service-tokens.html` redirects away (client-side) and that the
      underlying API calls those pages would make are independently
      rejected server-side (proving the client redirect is UX only, not
      the enforcement boundary)
- [x] 6.2 `gofmt -l .`, `go vet ./...`, `make test` all pass with no
      regressions (this change is frontend-only, so backend tests should
      be unaffected, but confirm)
