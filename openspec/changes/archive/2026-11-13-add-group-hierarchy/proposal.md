## Why

Access to nodes is going to be controlled by node group, with a grant on
a group covering everything beneath it. That needs groups to form a
tree in which a child's nodes are always a subset of its parent's -
which the classifier's flat, priority-only groups cannot express. This
change builds that tree; the access control that sits on it follows in
separate changes.

It deliberately reverses a decision. `architecture-summary.md` and the
classifier spec chose flat groups because Puppet Enterprise's
inheritance model confuses people. The hierarchy comes back because
inherited access needs it, but the confusing part - a parent silently
overriding its own child - is not brought back: a child always wins,
and the console says so when that overrides the priorities.

Rules also gain an "any of" mode, since a tree of AND-only rules cannot
express "nodes in either datacentre" without duplicating groups.

## What Changes

- **Group hierarchy.** Every group has exactly one parent. A built-in
  **All Nodes** group is the root: it matches every node, and cannot be
  deleted, renamed, moved, or given a rule or pins. Existing groups
  become its children, so classification is unchanged on upgrade.
- **Membership narrows downward.** **BREAKING** for pins: a group
  matches a node only if its parent also matches it. This applies to
  pinned nodes as well as rule matches, so a pin no longer overrides
  everything - it overrides the group's own rule, within its parent.
- **A child always wins.** **BREAKING** for the merge: when a group and
  one of its ancestors set the same class parameter, top-scope parameter
  or environment, the descendant's value is used whatever their
  priorities. Priority still decides between groups that are not
  ancestor and descendant.
- **Override warning.** When a child wins over an ancestor that has the
  higher priority, the console warns: on the group's page, in the group
  list, in the save response, in the activity log and in the server log.
  It never fails a save or a classification, and it never appears in the
  ENC response openvoxserver reads.
- **"Any of" rules.** A group's rule matches on all of its conditions
  (as now, and the default) or on any of them.
- **Tree view.** The groups page shows the hierarchy; creating or
  editing a group chooses its parent. Moving a group under itself or one
  of its descendants is refused, and so is deleting a group that still
  has children.
- **Counts and group targets follow the tree.** Matching-node counts, a
  group's node list, and anything that resolves "the nodes in this
  group" (job targets, package views, code repository usage) respect
  parent narrowing.

Out of scope: any permission or RBAC change, class discovery, PQL
targeting, scheduled jobs.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `classifier`: groups gain a parent and a rule match mode; matching
  narrows by ancestry; the merge requirement changes from "priority
  only, no hierarchy" to "descendant wins, priority otherwise", with a
  warning when the two disagree; the group UI gains a tree view and a
  parent selector; node counts follow the hierarchy.

## Impact

- **Schema:** a migration adds a parent, a root marker and a rule match
  mode to `node_groups`, creates the root, and re-parents existing
  groups under it.
- **Code:** `internal/classifier` (types, store, matching, merge,
  handlers), `internal/groupnodes` and `internal/encapi` (both move to
  hierarchy-aware matching), `internal/demodata` (its seeded "All Nodes"
  group becomes the built-in root).
- **API:** `GET/POST/PUT /api/v1/groups` gain `parentId`, `ruleMatch`,
  `isRoot` and `warnings`. The ENC endpoint's response shape is
  unchanged.
- **Frontend:** `groups.js` (tree), `group.js` (parent, match mode,
  warnings, pins outside the parent), new strings in every locale
  catalogue.
- **Docs:** `architecture-summary.md` (the classifier decision),
  `marketing/guides/use/classify.md` (which today says groups never
  inherit), and the groups screenshots on the site.
- **Behaviour on upgrade:** none for existing groups until one is moved
  under another. Classification output for an unchanged group set is
  identical.
