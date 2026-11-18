## Why

Every role is assigned to people one at a time. In an installation with
tens of operators, "give the web team the patch role" means editing each
account, and "the web team has changed" means finding every account
again. Puppet Enterprise assigns roles to user groups so that membership
is the only thing that changes when a team does. This is week 8 of the
Puppet Enterprise parity plan, and it builds on week 7: task allowlists
and node group scopes are paired per role, and a group is a way to hold
several roles at once, so the pairing has to survive being reached
through a group.

## What Changes

- **A user group is a named set of local users.** Administrators create,
  rename and delete groups, and add and remove members. A user may
  belong to any number of groups.
- **Roles can be granted to a group.** Every member holds those roles.
  A user's effective roles are the roles granted to them directly plus
  the roles granted to each group they belong to, without duplicates.
- **Everything that reads a user's roles reads the effective roles:**
  permissions, node group scopes, task grants in the access token, and a
  schedule owner's authority when the schedule fires. Pairing is
  unchanged: each role keeps its own node groups and task allowlist,
  whichever way the user came to hold it.
- **Changes take effect as other role changes do:** at the user's next
  token refresh, and at the next firing for a schedule.
- **The console gets a Groups page** (create, rename, delete, members,
  roles). The Users page shows a user's groups and which roles come
  from a group, and the Roles page shows which groups hold a role.
- **Audit:** every group change (create, rename, delete, member added or
  removed, role granted or revoked) is recorded.
- **Deleting a group removes its roles from its members**, after saying
  how many people that affects. Deleting a user or a role removes them
  from groups.

## Capabilities

### New Capabilities

- `user-groups`: what a group is, membership, roles granted to a group,
  effective roles and how they combine, the API, the Groups page and
  audit.

### Modified Capabilities

- `rbac`: a user's permissions, node scopes and task grants come from
  their effective roles, and the Users and Roles pages show where a role
  comes from.

## Impact

- **Code:** `internal/rbac` (group store and handlers; the four queries
  that read `user_roles` read effective roles instead), `internal/app`
  (route wiring only, since `scheduleAuthority` already calls the same
  store methods).
- **Database:** migration 000035 adds `user_groups`,
  `user_group_members`, `user_group_roles`, and a view of effective
  roles; no existing row changes.
- **API:** `/api/v1/user-groups` (list, create, read, update, delete),
  member and role sub-resources; `GET /api/v1/users/{id}/roles` gains a
  `via` field per role. No existing field changes meaning.
- **Frontend:** a Groups page, group chips on the Users page, group
  names on the Roles page; every new string in all 15 languages.
- **Site guides:** `use/access.md` (groups, how roles combine, deleting a
  group), `operations.md`.
- **Compatibility:** with no groups, every query returns what it returns
  today. Tokens already issued stay valid and unchanged until they
  expire.
- **Out of scope:** mapping groups from LDAP, SAML or OIDC claims (OIDC
  role mapping continues to assign roles directly), nested groups, and
  groups of anything but users.
