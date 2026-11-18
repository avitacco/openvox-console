## Context

Permissions are plain strings on roles (`role_permissions`), unioned per
user and embedded in a 15-minute access token at login and refresh
(`rbac.Claims.Permissions`). Every route is registered through
`authorize(permission, handler)`; nothing below that knows who is
asking. Node data comes from three places: openvoxdb (nodes, facts,
reports, packages), Postgres (vulnerabilities, `node_agents`) and the
node transport (connections, certificate requests).

`add-group-hierarchy` gave the classifier a single tree with a root
group that matches every node, parent narrowing, and
`classifier.Tree.Contains(groupID, certname, facts)`.
`groupnodes.Resolver.GroupCertnames` already turns one group into its
certnames with one fleet-wide fact query.

## Goals / Non-Goals

**Goals:**
- A role's permissions apply to named node groups and their
  descendants; root means fleet-wide.
- Every node-data read is filtered, including counts, with no way to
  tell a hidden node from a missing one.
- No change in what anyone sees on upgrade.
- A shape the action side can reuse unchanged next.

**Non-Goals:**
- Scoping actions, jobs, the activity log, group definitions or service
  tokens (see proposal).
- Per-user grants; grants are on roles only.
- Deny rules or exclusions ("all but this group").

## Decisions

### 1. Grants live on the role, and scope is per permission
`role_node_groups(role_id, group_id)`. A user's scope for permission P
is the union of the groups of the roles that carry P.

*Why per permission, not one scope per user:* the common request is
"see everything, run Puppet only on our servers". One scope per user
cannot express it; one scope per (role, permission) row is more than
the UI needs. Per-role groups with per-permission evaluation gives the
first without the second.

*Alternative considered:* scope encoded into the permission string
(`nodes:read@12`). Rejected: every existing `HasPermission` call and
the role editor's checkbox list would have to parse it.

### 2. One rule for which scope an endpoint uses
An endpoint filters node data by the caller's scope for **the
permission that gates that endpoint**. So `GET /groups/{id}/nodes`
uses the `classifier:read` scope, the fleet upgrade list uses
`status:read`, repository usage counts use `code:read`.

*Why:* it needs no table of "which permission governs which data", it
cannot be got wrong per handler, and after migration (every role on
root) it reproduces today's behaviour exactly. The cost is that an
administrator narrowing a team must narrow the role that carries all
of that team's read permissions, which is how roles are normally built.

### 3. Scope travels in the token; nodes are resolved per request
Claims gain `nodeScopes`: permission → group ids. If a permission's
groups include the root, only the root id is written. The issuer always
writes the field for user tokens, empty if the user has none.

- **Absent field = fleet-wide.** This covers access tokens issued
  before the upgrade (at most 15 minutes) and service tokens, which
  stay unscoped. It grants nothing those tokens did not already have.
- **Group ids, not certnames, in the token.** Membership changes with
  facts; a token must not freeze it. Role changes still take effect at
  the next refresh, as permission changes do today.
- Unknown group ids are ignored, so a deleted group fails closed.

*Alternative considered:* look the caller's roles up in Postgres on
every request. Rejected: it reverses the architecture's "permissions
are in the token" decision for one feature.

### 4. `internal/nodescope` resolves and caches
A new package composes classifier and openvoxdb, as `groupnodes` and
`encapi` do, so neither imports the other and `rbac` imports neither.

```go
type Scope interface {
    All() bool                      // root granted: do not filter
    Allows(certname string) bool
    Certnames() []string            // nil when All()
}
func (r *Resolver) For(ctx, groupIDs []int64) (Scope, error)
```

`authorize` is wrapped in `internal/app` so that, after the permission
check, the caller's group ids for that permission are put on the
request context; handlers call a small injected `scopeOf(r)` function.
Handlers keep their current package boundaries.

- **Fast path:** root in the list → `All()`, no queries. Every existing
  user takes this path, so there is no cost until a role is narrowed.
- **Otherwise:** all groups + `FleetFacts` once, then
  `tree.Contains` for each granted group. Results are cached in
  process, keyed by the sorted group ids, for 30 seconds. The cache is
  derived and disposable, so it does not conflict with stateless
  instances; it bounds staleness to well inside the one minute the spec
  promises.
- A resolver error fails the request (502), never falls back to
  unfiltered.

### 5. Nodes without facts
A node openvoxdb has no facts for (never ran, or a pending certificate
request) can only match a group through a pin whose ancestors it also
matches, which in practice means it is visible to fleet-wide callers
only. This is stated in the spec and the guide rather than worked
around: pending certificate requests are an administrative surface.

### 6. Filtering at each source
- **In memory** where the handler already holds the full list:
  node list and summary, connectivity, group nodes and counts, usage
  counts, fleet upgrade list.
- **In the query** where the source aggregates: openvoxdb package
  queries via the existing `PackageFilter.Certnames` (intersected with
  any group filter; an empty intersection returns empty without
  querying); vulnerability queries gain `certname = ANY($n)`.
- **Single node:** `Allows(certname)` before any lookup; false returns
  the handler's existing not-found body. Report events look up the
  report's certname first.

Large scopes produce large `certname in [...]` lists. Fleet-wide
callers never build one; for the rest it is bounded by the size of
their scope, which is the point of scoping.

### 7. Deleting a granted group is refused
`role_node_groups.group_id` references `node_groups` with `ON DELETE
RESTRICT`; the classifier store maps the violation to a new
`ErrGroupGranted` → 409. Removing a role cascades its rows.

*Alternative considered:* cascade the grant away. Rejected: a
`classifier:write` user could silently strip another team's access.

### 8. Role API and UI
Role JSON gains `nodeGroups: [id]`. Omitted on create → root, so
existing API clients are unaffected; omitted on update → unchanged;
`[]` is allowed and means "no nodes". Redundant entries (a group and
its descendant) are stored as given. `GET /api/v1/me` gains
`nodeScope: { all, groups: [{id, name}] }` for `nodes:read`.

The role editor shows the group tree (from `GET /groups?all=true`) as
an indented multi-select. The roles list shows the groups; a role with
none gets a warning badge. The nodes page shows a `vox-alert` naming
the groups when `nodeScope.all` is false.

## Risks / Trade-offs

- **A missed endpoint leaks nodes.** → One test walks every registered
  route gated by a read permission with a narrowly scoped token and
  fails on any certname outside scope; new node endpoints fail it until
  they filter.
- **Absent claim means fleet-wide.** A bug that drops the claim widens
  access. → The verifier cannot tell a pre-upgrade access token from one
  that lost its claim, so the guard is an issuer test that every user
  token carries the field, even when empty.
- **Scoped requests cost a fleet fact query** on a cache miss. → 30 s
  cache; fleet-wide path is free.
- **Jobs and the activity log still name out-of-scope nodes** to users
  who hold those permissions. → Stated in the guide; closed for jobs by
  the action-side change.
- **Up to 30 s + 15 min of staleness** between an administrator
  narrowing a role and it taking effect. → Same window as permission
  changes today; revoking the user's tokens forces it.

## Migration Plan

1. Migration `000030`: create `role_node_groups`; insert (role, root)
   for every role. Down migration drops the table.
2. Deploy. Old tokens are fleet-wide until they expire; refreshed
   tokens carry root scope. Nothing changes for any user.
3. Administrators narrow roles when ready.

Rollback: deploy the previous build (it ignores the unknown claim) and
run the down migration. Narrowed roles become fleet-wide again, which
must be called out in `operations.md`.

In a cluster, instances on the old build ignore scope during a rolling
upgrade; no role can be narrowed until an upgraded instance serves the
role editor, so the window is only as wide as the rollout.

## Open Questions

- Whether the action-side change should also scope the activity log's
  node-subject events, or leave it an administrator surface.

## Implementation Notes

Where the build differs from, or sharpens, the decisions above.

- **`internal/nodescope` does not compose the classifier itself**
  (decision 4). It holds the `Scope` type, the cache and the request
  glue, and is handed "which nodes are in these groups" as a function.
  That function is `groupnodes.Resolver.NodesInGroups`, beside the
  existing `GroupCertnames`. Reason: `packageinventory` and `inventory`
  take a `Scope`, and `packageinventory` deliberately does not depend on
  the classifier; a `nodescope` that imported it would have pulled it in
  for both.
- **A descendant needs no separate walk.** A child group only holds
  nodes its parent holds, so "in the granted group or any group beneath
  it" is exactly `tree.Contains(grantedGroup, …)`.
- **Pinned nodes without facts are in scope.** The resolver considers
  every pinned certname as well as every node openvoxdb has facts for,
  so a node pinned into a granted group is visible before its first
  run, provided the group's ancestors hold it too (decision 5).
- **"Not found" means "what an unknown node answers".** Several of these
  endpoints never returned 404 for an unknown node: node detail answers
  the certname with no facts, reports and packages answer an empty
  list, a vulnerability answers no affected nodes, package reporting
  status answers "node is offline". An out-of-scope node gets that same
  answer, byte for byte, which is the property the spec is after. Where
  it can, the handler produces it without querying, so timing does not
  distinguish the two either.
- **Report events carry their node.** A report is addressed by hash, so
  `openvoxdb.Event` gained `Certname` and events are filtered on it.
- **The fleet upgrade list is the connectivity listing.** Client builds
  and upgrade state are returned only by `GET /api/v1/node-connectivity`
  (task 4.3). The status page API returns console instances, no node
  data, and is exempt in the sweep.
- **Package provider names** for a scoped caller are read off their own
  nodes' packages rather than the fleet-wide aggregate, so the provider
  filter does not name a provider only other teams use.
- **Handlers default to unscoped, the middleware defaults to closed.** A
  handler set nobody called `SetScope` on shows everything, so unit
  tests and other callers keep working; a request that reaches the
  resolver without a grant recorded gets nothing. The route sweep
  (`internal/app/node_scope_sweep_test.go`) is what catches a handler
  set left unscoped: it reads every permission-gated GET route from the
  source and requires each to be either swept or listed as exempt with
  a reason. Removing one `SetScope` call was confirmed to fail it.
- **Role changes are recorded in the audit event**, which carries the
  permissions and node groups before and after. The activity entry's
  summary stays prose ("updated role X").
- **Role admins without `classifier:read`** cannot load the group tree.
  The roles page then leaves `nodeGroups` out of every request, so a
  permission edit never touches a role's groups, and says that a new
  role will cover all nodes.

