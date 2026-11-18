## Context

See `proposal.md` for why the hierarchy is coming back.

Today `internal/classifier` holds flat groups. `Matches(g, certname,
facts)` judges one group in isolation: pinned, or a non-empty rule whose
conditions all hold. `Classify` sorts the matching groups by their
unique `priority` and lets later writes win. Three callers use these:
`internal/encapi` (the ENC response), and `internal/groupnodes` for a
group's node list, the per-group counts, the `Resolver` other packages
use to turn a group into certnames, and `AssignedEnvironments`.

`node_groups.priority` is `NOT NULL UNIQUE`. `internal/demodata` already
seeds an ordinary group named "All Nodes" at priority 10.

Nothing on a node's page shows its effective classification, so the
only places a person meets classification are the groups pages.

## Goals / Non-Goals

**Goals:**

- A single ordering rule for the merge that is total and deterministic,
  so the same groups always produce the same classification.
- One place that knows how ancestry affects matching, used by every
  caller, so the ENC answer and the counts cannot disagree.
- An upgrade that changes no node's classification.

**Non-Goals:**

- Permissions on groups. This change only makes the tree they will
  hang from.
- Nested rule expressions. A rule is still one flat list of conditions,
  now with a mode.
- Showing a node's effective classification, or a per-node warning.
- Inheriting a parent's rule by copying it into the child. Narrowing is
  evaluated, not stored.

## Decisions

### 1. Ancestry is a `parent_id` column, resolved in memory

`node_groups` gains `parent_id BIGINT REFERENCES node_groups(id) ON
DELETE RESTRICT`, `is_root BOOLEAN NOT NULL DEFAULT false`, and
`rule_match TEXT NOT NULL DEFAULT 'all'` with a check for `all`/`any`.
A partial unique index allows one root, and a check ties `is_root` to
`parent_id IS NULL`.

Every caller already loads all groups (`ListAllGroups`) before matching,
so the tree is built in memory from `parent_id`. No recursive SQL is
needed on the read path.

*Alternatives:* a materialised path or closure table would make
"descendants of X" a single query, which the access-control changes may
want. It can be added then, when the queries that need it exist; adding
it now means maintaining it on every move for no current reader.

`ON DELETE RESTRICT` backs up the "delete with children is refused"
rule in the database, so a bug in the handler cannot orphan a subtree.

### 2. All Nodes is a real row, marked `is_root`, with no priority

The root is stored like any group so that parents, classes and
parameters need no special case. What is special is enforced in the
store: no rename, no parent, no rule, no pins, no delete.

Its priority is `NULL`. `priority` becomes nullable with a check that
only the root may be null. The root must rank below everything (see
decision 4), and a real number would both occupy a slot in the unique
range and, under the rank rule, lift every group in the fleet to at
least that number.

The API reports the root with `isRoot: true` and no priority. In Go the
root's `Priority` reads as zero and is never consulted; the response
type leaves the field out for the root.

*Alternative:* a virtual root that exists only in code. Rejected:
All Nodes must carry classes (PE users expect it to), and the later
access-control work needs a row to grant on.

### 3. Matching becomes a property of the tree, not of one group

`Matches(g, ...)` cannot answer for a child without its ancestors, so it
is replaced by a tree built once per request:

```
tree := classifier.NewTree(groups)
tree.Matching(certname, facts) []Group   // every group the node is in
tree.Contains(groupID, certname, facts)  // one group, walking up
```

`Matching` walks down from the root and does not descend into a group
the node fails, so a node in none of a subtree costs one check for it.
A group's own test is: root, or pinned, or a non-empty rule satisfied
under its mode. The "empty rule matches nobody" safety stays for every
group but the root.

`encapi`, and all four uses in `groupnodes`, move to the tree.
`groupnodes`' `groupGetter` currently fetches one group for the node
list and for `Resolver.GroupCertnames`; both now need the group's
ancestors, so they load all groups instead. That is the same query
`nodeCounts` already runs.

The old `Matches` is removed rather than kept, so a new caller cannot
pick the version that ignores ancestry.

### 4. The merge order is (rank, depth, priority)

"Child wins, priority otherwise" is not an ordering on its own. With a
parent at 10, its child at 1 and an unrelated group at 5: the child
beats the parent, the parent beats the unrelated group, and the
unrelated group beats the child. Any implementation has to break that
loop, and should do it the same way every time.

Each group gets a **rank**: its own priority, or its parent's rank if
higher. The root ranks below everything. Matching groups are sorted
ascending by rank, then depth, then own priority, and applied in that
order with later writes winning, exactly as now.

- A descendant's rank is never below its ancestor's, and at equal rank
  it is deeper, so it always sorts after: the child wins.
- Unrelated groups with no lifted rank compare by priority, as today.
- Priority is unique, so the order is total.

The consequence to be aware of: a low-priority child of a high-priority
parent also outranks unrelated groups its parent outranks. That is the
natural reading of "this group is a refinement of its parent", and the
spec states it as a scenario.

*Alternative:* resolve each conflicting key separately, preferring a
descendant where one exists and priority elsewhere. It gives different
answers depending on which groups happen to conflict on a key, and
cannot be explained as one list.

For an upgraded deployment every group is a child of the root, no rank
is lifted, and the order is the old one.

### 5. The warning is computed from group configuration, not from nodes

A warning exists when a group has a lower priority than a non-root
ancestor and sets a class parameter, top-scope parameter or environment
that the ancestor sets to a different value. That is decidable from the
groups alone, so it needs no node data, is the same on every instance,
and is not stored.

It is computed whenever groups are returned: each group in `GET
/api/v1/groups` and `GET /api/v1/groups/{id}`, and the responses to
create and update, carry `warnings: [{ancestorId, ancestorName, kind,
class?, key?}]`. An update to an ancestor also returns the descendants
it has put into that state, since that save is what caused it.

On a save that leaves any warning, the activity summary mentions it and
the server logs it at warn level. It is not logged per ENC request: that
would repeat the same line for every node on every run.

The ENC response is untouched. openvoxserver's exec terminus expects
classes, parameters and environment only.

*Not covered:* two groups can be ancestor and descendant, conflict, and
never both match the same node (the child's rule excludes everything).
The warning still shows. It describes the configuration, which is what
the user can act on.

### 6. Hierarchy writes are serialised

Two concurrent moves can each pass a cycle check and together create a
loop. Create, update and delete take one transaction-scoped Postgres
advisory lock before checking ancestry. Group writes are rare and
human-paced, so one lock for all of them is cheaper to reason about
than row locks up a chain.

A cycle is detected by walking up from the proposed parent; reaching
the group being moved means a loop.

### 7. Listing returns the whole tree

`GET /api/v1/groups` is paginated, ordered by priority. A tree cannot be
drawn from one page of it. The endpoint gains an unpaginated form
(`?all=true`) the groups page uses (the classifier already loads every group on each ENC
request, so the size is not a new concern), and the page builds the tree
client-side from `parentId`. The paginated form stays for API clients.

voxblocks has no tree component. The tree is an indented table: the
existing columns, with each row indented by depth and a `vox-badge
variant="warning"` on rows with warnings. The warning detail on the
group page is a `vox-alert`.

### 8. Pins outside the parent are reported by the group-nodes endpoint

`GET /api/v1/groups/{id}/nodes` already resolves the group's nodes. It
additionally returns the pinned certnames that are not in that result,
which the group page marks. This reuses its "node data unavailable"
path for the unavailable case.

## Risks / Trade-offs

- **A pin stops working when a group is moved** → the group page marks
  pins outside the parent (decision 8), and the upgrade moves nothing,
  so no existing pin changes.
- **An existing group is already called "All Nodes"** (the demo data's
  was) → the migration never adopts it as the root. The root matches
  every node, so adopting a group would apply its classes to nodes its
  rule did not match, and break "the upgrade changes no node's
  classification". The existing group is renamed "All Nodes (previous)"
  and keeps its rule, pins and priority; the classify guide's upgrade
  section says so. `internal/demodata` is changed to configure the root
  instead of creating its own.
- **The rank rule surprises someone** who expected priority to be
  absolute between unrelated groups → the override warning fires on
  exactly the groups whose rank is lifted and which conflict with the
  ancestor; the classify guide explains the rule with the 10/1/5
  example.
- **Counts get slower** → no: the tree prunes subtrees a node is not in,
  so per-node work falls for deep trees and is unchanged for flat ones.
- **Rollback loses the hierarchy** → the down migration re-flattens:
  drops the columns and deletes the root. Classification after a
  rollback is priority-only again, which differs wherever a child had
  been lifted or narrowed.

## Migration Plan

1. Migration `000029`: add the columns and constraints; make `priority`
   nullable; rename any existing "All Nodes" group out of the way and
   create the root; set every other group's `parent_id` to it; default
   `rule_match` to `all`.
2. Deploy. With every group directly under the root and no rule mode
   changed, classification is identical; the existing classification
   tests are kept and must pass unchanged against migrated data.
3. In a cluster, instances on the old version ignore the new columns and
   keep classifying flat, so a mixed-version window is safe only until
   the first group is moved. The upgrade note says to finish the rollout
   before using the hierarchy.

Rollback: the down migration, as above.

## Open Questions

- Whether the tree should be collapsible. It does not affect the specs
  or the API; decide when the page is in front of someone with a large
  group set.
