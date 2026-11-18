## 1. Storage and role model

- [x] 1.1 Migration `000030_role_node_groups` (up/down): table with `role_id` (cascade) and `group_id` (restrict), unique pair; grant the root group to every existing role
- [x] 1.2 Migration test: existing roles end up on the root group; a console with no roles migrates cleanly; down drops the table
- [x] 1.3 `rbac.Role` gains `NodeGroups []int64`; create, get, list and update read and write it in the role's transaction
- [x] 1.4 Role store rejects an unknown group id with a typed error; omitted groups on create mean root, on update mean unchanged; an explicit empty list is stored
- [x] 1.5 `UserNodeScopes(userID)`: permission → group ids, unioned across the user's roles, collapsed to the root id when the root is present
- [x] 1.6 Store tests for 1.3–1.5, including two roles with the same permission and a role without it

## 2. Tokens and middleware

- [x] 2.1 `Claims.NodeScopes`; issuer always writes it for user access tokens; login, refresh and OIDC login populate it from 1.5
- [x] 2.2 `Claims.NodeGroupsFor(permission)` returning group ids and a fleet-wide flag (absent field → fleet-wide)
- [x] 2.3 Tests: user tokens always carry the field; a token without it is fleet-wide; service tokens are fleet-wide
- [x] 2.4 Role handlers accept and return `nodeGroups`; 400 on unknown group; activity event records the groups
- [x] 2.5 `GET /api/v1/me` returns `nodeScope` (fleet-wide flag and group ids with names) for `nodes:read`

## 3. Scope resolution

- [x] 3.1 New `internal/nodescope`: `Scope` (All / Allows / Certnames) and `Resolver.For(ctx, groupIDs)` over classifier groups and fleet facts
- [x] 3.2 Root fast path with no queries; unknown group ids ignored; resolver errors returned, never widened
- [x] 3.3 30-second in-process cache keyed by sorted group ids
- [x] 3.4 Tests: inheritance to descendants, sibling excluded, union of two groups, node without facts visible only via root or a pin, empty list allows nothing, cache hit and expiry
- [x] 3.5 In `internal/app`, wrap `authorize` so the gating permission's group ids reach the handler through a `scopeOf(r)` function; pass it to each node-data handler set

## 4. Enforcement per surface

- [x] 4.1 Inventory: node list, summary and node detail (not-found when out of scope)
- [x] 4.2 Reporting: a node's reports; a report's events checked against the report's node
- [x] 4.3 Node connectivity list, including pending certificate requests
- [x] 4.4 Package inventory: node packages, reporting status, search and catalogue via `PackageFilter.Certnames`, intersected with the group filter; empty intersection returns empty without querying
- [x] 4.5 Vulnerabilities: list, detail (not-found when no in-scope node is affected), affected-node counts and a node's vulnerabilities, filtered in SQL
- [x] 4.6 Group nodes and per-group node counts
- [x] 4.7 Code repository usage counts
- [x] 4.8 Fleet upgrade list and totals: these are carried by the connectivity listing (4.3), which is the only endpoint that returns them; confirmed the status page API returns no node data and listed it as exempt in the sweep
- [x] 4.9 Classifier store: deleting a granted group returns `ErrGroupGranted`; handler answers 409 with a message naming the cause
- [x] 4.10 Handler tests per surface: scoped caller sees only in-scope data, out-of-scope single node matches the nonexistent response byte for byte, fleet-wide caller unchanged
- [x] 4.11 Route sweep test: every route gated by a read permission, called with a narrowly scoped token against seeded data, returns no out-of-scope certname; actions, jobs, activity log and ENC are listed as explicit exemptions

## 5. Frontend

- [x] 5.1 Role editor: indented node group multi-select from the group tree, root preselected for a new role
- [x] 5.2 Roles list: show each role's groups, "All Nodes" for fleet-wide, warning badge for none
- [x] 5.3 Nodes page: notice naming the caller's groups when their scope is not fleet-wide
- [x] 5.4 Group page: show the 409 refusal when deleting a granted group
- [x] 5.5 Extract and merge strings; translate German; report the status of the other locales

## 6. Demo data and docs

- [x] 6.1 Demo seed: a "Linux Operators" role scoped to "Linux Servers" and a user holding it
- [x] 6.2 Guide `marketing/guides/use/access.md`: granting a role on node groups, inheritance, what a scoped user sees, nodes without facts, what is not yet scoped
- [x] 6.3 Guide `marketing/guides/use/classify.md`: a granted group cannot be deleted
- [x] 6.4 `architecture-summary.md` section 3 and `operations.md` (upgrade is a no-op for access; rollback widens narrowed roles)
- [x] 6.5 Rebuild the site (`make marketing`)

## 7. Verification

- [x] 7.1 `make test`, `go vet ./...`, gofmt, `openspec validate add-group-scoped-node-access --strict`
- [x] 7.2 Live: upgrade the dev database and confirm an existing user sees the same node list and dashboard figures as before
- [x] 7.3 Live: log in as the scoped demo user; check node list, dashboard, packages, vulnerabilities, group counts and a direct URL to an out-of-scope node in the browser
- [x] 7.4 Live: move a node into and out of the scoped group and confirm it appears and disappears within a minute without a new login
