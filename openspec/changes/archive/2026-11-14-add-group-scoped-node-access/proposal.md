## Why

Every role today is fleet-wide: a user who may read nodes may read all of
them. Teams that share one console cannot be limited to their own
servers, which is the main access-control gap against Puppet Enterprise.
The node group hierarchy (`add-group-hierarchy`) was built to be the
unit of that control. This change uses it for what a user can **see**;
what a user can **do** to nodes follows in a separate change.

## What Changes

- A role gains a list of node groups. The role's permissions apply to
  the nodes in those groups and, by inheritance, in every descendant
  group. A role granted the built-in **All Nodes** root is fleet-wide.
- A user's scope for a permission is the union of the groups of every
  role that gives them that permission. Read and (later) action scope
  can therefore differ: read everything, act on one subtree.
- Existing roles are migrated to the root group, so nobody sees more or
  less than before until an administrator narrows a role.
- Every endpoint that returns node data filters it to the caller's
  scope for the permission that gates the endpoint: node list, node
  detail, facts, reports and their events, connectivity, packages,
  vulnerabilities, group node listings and counts, code repository
  usage counts, the fleet upgrade list, and the dashboard figures built
  from them.
- A single node outside the caller's scope answers "not found", exactly
  as a node that does not exist. Counts and summaries cover only
  in-scope nodes.
- Scope is carried in the access token beside the permissions, and
  resolved to nodes at request time, so a node that moves between
  groups moves in and out of view without a new login.
- A node group granted to a role cannot be deleted until the grant is
  removed.
- The role editor gains a node group picker; the roles list shows each
  role's groups; the nodes page tells a scoped user which groups they
  are seeing.

Not in this change:

- **Actions** (Puppet runs, tasks, plans, agent upgrades, certificate
  actions, node removal, package reporting toggle) keep today's
  permission checks, unscoped. That is the next change.
- **Jobs and the activity log** stay unscoped: a job is the record of an
  action and is scoped with actions; the activity log is an audit
  surface with its own permission.
- **Group definitions** stay visible to anyone with `classifier:read`;
  delegating edit rights over a subtree is not part of this.
- **Service tokens** stay fleet-wide.
- The ENC endpoint is unaffected.

## Capabilities

### New Capabilities
- `node-access-scope`: how a caller's node groups limit the node data
  each endpoint returns - inheritance, union across roles, hidden
  nodes, scoped counts, and which surfaces are and are not scoped.

### Modified Capabilities
- `rbac`: roles carry node groups; tokens carry the resulting scope;
  role management UI edits it.
- `classifier`: a group granted to a role cannot be deleted.

## Impact

- **Database**: migration `000030` adds `role_node_groups` and grants
  the root group to every existing role.
- **Code**: `internal/rbac` (role store, handlers, token claims, login
  and refresh), a new `internal/nodescope` package that resolves a
  scope to nodes, and scope filtering in `internal/inventory`,
  `reporting`, `nodeconnectivity`, `packageinventory`, `vulnerability`,
  `groupnodes`, `codemanager` (usage counts), `stackstatus` /
  `agentupgrade` (fleet upgrade list); wiring in `internal/app`;
  `internal/classifier` store (delete refusal); `internal/demodata`
  and `cmd/demo-seed` (a scoped demo role).
- **API**: roles gain `nodeGroups`; `GET /api/v1/me` gains the caller's
  read scope. A role created without `nodeGroups` is fleet-wide, so
  existing API clients behave as before.
- **Frontend**: `roles.js`, `nodes.js`, role templates, new strings in
  all locales.
- **Guides**: `marketing/guides/use/access.md` (granting a role to node
  groups, what a scoped user sees, what is not yet scoped) and
  `marketing/guides/use/classify.md` (a granted group cannot be
  deleted).
- **Docs**: `architecture-summary.md` section 3 (scope in the token,
  resolved per request) and `operations.md`.
- **Behaviour after upgrade**: unchanged for every existing user and
  token.
