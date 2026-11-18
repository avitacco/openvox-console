## Context

A user's roles live in `user_roles (user_id, role_id, assigned_via)`.
Four queries in `internal/rbac/roles.go` read it to answer questions about
what a user may do: `ListUserRoles`, `UserPermissions`, `UserNodeScopes`
and `UserTaskGrants`. Those feed the access token (login, refresh, OIDC)
and `scheduleAuthority`, which uses the same store methods at each
schedule firing. `assigned_via` marks rows the OIDC reconciler owns, so a
login can replace its own assignments without touching manual ones.

Week 7 pairs each role's task allowlist with that role's node groups, per
role. Anything that merges roles before that pairing is computed would
break it.

## Goals / Non-Goals

**Goals:**

- A group holds roles; its members hold them, counted once.
- Every consumer of a user's roles sees the same effective set, so no
  path (token, schedule, role list) disagrees.
- Pairing from week 7 is untouched.

**Non-Goals:** external group mapping (LDAP, SAML, OIDC claims), nested
groups, groups of non-user principals, group-level node scopes or
allowlists (those belong to roles).

## Decisions

### 1. Three tables and one view

`user_groups (id, name, lower(name) unique, created_at)`,
`user_group_members (group_id, user_id)` and `user_group_roles
(group_id, role_id)`, each with cascading foreign keys and a unique pair.
`user_effective_roles (user_id, role_id)` is a view: direct rows from
`user_roles` unioned with members joined to group roles.

The four queries replace `user_roles ur` with `user_effective_roles ur`.
Nothing else in them changes, which is what keeps the pairing: each role
is still one row joined to its own node groups and patterns. The view
deduplicates, so a role held twice counts once, and `DISTINCT`/`GROUP BY
r.id` in the existing queries already cope.

*Alternative.* Copy group roles into `user_roles` on membership change.
Needs reconciliation on every group, role and membership change, and
removing a direct assignment would have to know whether a group still
grants it. A view cannot drift.

### 2. Role writes keep using `user_roles`

Assign and unassign, and OIDC reconciliation, still touch only direct
rows. A user's groups are changed only from the group side. The Users
page therefore shows two lists: direct roles (removable) and roles via a
group (read-only, with the group's name).

`ListUserRoles` keeps returning every effective role, each with `via`
(`direct` or a group name; a role held several ways lists them all, direct
first), so the existing consumers see a superset with a new field.

### 3. Groups are managed with `rbac:admin`

No new permission. Group routes sit beside users and roles and are gated
the same way. A group can hold a role the editor could not themselves
grant elsewhere; there is no such thing here because `rbac:admin` already
grants anything.

### 4. Names and sizes

Group names are unique case-insensitively, 1 to 64 characters after
trimming, no other character rules (they are shown, never used as keys).
No limit on members or roles per group beyond the database.

### 5. Deleting a group needs confirmation when it has members

`DELETE /api/v1/user-groups/{id}` answers `409` with `memberCount` when
members exist and `confirm=true` is absent. Deleting a user or a role is
handled by cascading foreign keys. Audit entries for those already exist;
their payloads do not need to list group memberships.

### 6. Effect on sessions and schedules

Tokens carry no group information, only the roles' results, so a group
change is felt at the next refresh (at most 15 minutes), as any role
change is. A schedule's authority is read at each firing from the same
store methods, so it follows groups with no extra work. The existing
`scheduleAuthority` test double is extended; the production wiring does
not change.

### 7. API and audit

`/api/v1/user-groups` (GET, POST), `/{id}` (GET, PUT rename, DELETE),
`/{id}/members/{userId}` (POST, DELETE), `/{id}/roles/{roleId}` (POST,
DELETE). A group reads as `{id, name, memberCount, members:[{id,
username}], roles:[{id, name}]}` (members and roles only on the single
read). Audit entries use the rbac category and the event names
`group.created`, `group.renamed`, `group.deleted`, `group.member.added`,
`group.member.removed`, `group.role.granted`, `group.role.revoked`.

### 8. The Roles page shows the groups that hold a role

`GET /api/v1/roles` is unchanged; a role's groups come from the group
list (the page already loads it), avoiding a new field on a hot route.

## Risks / Trade-offs

- **A query reading `user_roles` directly escapes the view.** → A grep
  guard test fails if any non-test file outside the role assignment code
  reads `user_roles` (the same arrangement as other sweep tests); the task
  list names the four queries.
- **A view per request is slower than a table.** → The tables are tiny and
  indexed on both columns; token issuance runs three queries per login.
- **An administrator expects removing a user's role to remove it.** →
  The Users page shows the role as via-group and read-only, with the
  group's name, so the way to remove it is visible.
- **Last administrator.** A group could be the only way a user holds
  `rbac:admin`, and deleting it would lock everyone out. → Deleting a
  group, revoking a role from it, or removing a member is refused when it
  would leave no user holding `rbac:admin` (`409`, naming that
  reason). Existing direct paths have no such guard today; this change
  adds it only for the new group paths and does not alter the old ones.

## Migration Plan

Migration 000035 creates the tables, indexes and view; the down migration
drops them. No data moves. Roll back by running it down; the older binary
reads `user_roles` as before.

## Implementation Notes

What differs from the design above, or was decided while building it.

- **`via` is a list.** Decision 2 said `via` is `direct` or a group's name; a role
  can be held several ways, so `Role.Via` is a list (direct first, then each
  granting group by name), omitted everywhere but `GET /users/{id}/roles`.
- **`GET /api/v1/users/{id}/groups`** was added, beyond the routes in decision 7,
  so the Users page can show a user's groups, including one that grants nothing.
- **The last-administrator guard is in the store** (`changeKeepingAnAdmin`, a
  transaction that counts holders of `rbac:admin` through the view before and
  after), so every caller gets it, and it only refuses a change that takes the
  count from some to none. It is tested on a scratch database
  (`testdb.ScratchPool`, new), because the shared one always has other
  administrators.
- **Repeating an add is audited**, as every request that succeeds is; the
  membership itself does not change.
- **The Users page** ticks only direct assignments (the checkboxes change those),
  and lists group-granted roles beside them as "role via group". The split lives
  in `role-sources.js` so it has a unit test.
- **The Roles page** reads `/api/v1/user-groups` best-effort for "Granted to
  groups", as decision 8 said, so it still loads for a caller who cannot.
- **The page is called "User groups"**, since "Groups" already means node groups
  in the navigation.
- **The node-scope sweep** lists the group routes as exempt ("rbac
  administration"), as the user and role routes are.
- **A schedule that fires after its owner left the group** is skipped with the
  node-scope reason (they no longer cover the node) rather than a task reason,
  when the lost role was also the only one covering that node; the task reason
  appears when the node is still covered.
