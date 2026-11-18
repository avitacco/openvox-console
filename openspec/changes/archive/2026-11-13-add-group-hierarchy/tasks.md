## 1. Schema

- [x] 1.1 Add migration `000029_node_group_hierarchy` (up): `parent_id` with `ON DELETE RESTRICT`, `is_root`, `rule_match` (check `all`/`any`, default `all`), nullable `priority` with a check that only the root may be null, a partial unique index allowing one root, and a check tying `is_root` to a null parent. Verify `make test` applies it to an empty database.
- [x] 1.2 In the same migration, create the root: rename any existing "All Nodes" group to "All Nodes (previous)" (it is never adopted, since the root matches every node and adopting would change classification), insert the root, then set every other group's `parent_id` to it. Verify with a migration test seeded four ways (no groups, ordinary groups, an existing "All Nodes" with a rule, and "All Nodes (previous)" already taken) that each ends with exactly one root and every other group parented to it with its rule intact.
- [x] 1.3 Write the down migration (drop the constraints and columns, delete the root, restore `NOT NULL` on `priority`). Verify up-down-up runs cleanly in the migration test.

## 2. Tree, matching and merge (`internal/classifier`)

- [x] 2.1 Add `ParentID`, `IsRoot` and `RuleMatch` to `Group`, make the root's priority absent, and update the package comment in `types.go` that says groups are flat. Verify the package builds and existing JSON round-trip tests pass with the new fields.
- [x] 2.2 Add the "any" match mode to rule evaluation, keeping "all" as the behaviour when the mode is empty. Verify with table tests covering the spec's Rule match mode scenarios, including an empty rule matching nothing under either mode.
- [x] 2.3 Add `NewTree(groups)` with `Matching(certname, facts)` and `Contains(groupID, certname, facts)`: root matches everything, a group matches only within its parent, pins narrow like rules, and a failed group's subtree is not visited. Remove the exported `Matches`. Verify with tests for each scenario under Fact-based group matching, Explicit node pinning and Built-in All Nodes root group, plus one asserting a subtree under a non-matching group is never evaluated.
- [x] 2.4 Change `Classify` to take matching groups from the tree and order them by (rank, depth, priority) as in design decision 4. Verify with tests for every scenario under Classification merge by hierarchy and priority, including the 10/1/5 case and the two lifted siblings, and that the pre-existing flat-group merge tests pass unchanged with every group parented to the root.
- [x] 2.5 Add `Warnings(groups)` returning, per group, each class parameter, top-scope parameter or environment it sets differently from a higher-priority non-root ancestor. Verify with tests for every scenario under Warning when hierarchy overrides priority that does not involve the API.

## 3. Store and API

- [x] 3.1 Persist and load `parent_id`, `is_root` and `rule_match` in the store; default a new group's parent to the root. Verify with store tests that create, read and update round-trip all three and that a group created with no parent is a child of the root.
- [x] 3.2 Enforce the hierarchy rules in the store under one transaction-scoped advisory lock: reject a missing parent, a self-parent and a move under a descendant; refuse deleting a group with children; refuse renaming, re-parenting, deleting, or adding a rule or pins to the root. Verify with a store test per rule, and a concurrency test in which two moves that would together form a loop cannot both succeed.
- [x] 3.3 Map the new store errors in the handlers to `409` (children, loop) and `400` (bad parent, bad mode, root restrictions) with messages that say what to do. Verify with handler tests for each.
- [x] 3.4 Return `parentId`, `isRoot`, `ruleMatch` and `warnings` from the group endpoints; on update, also return the descendants the save has left overriding the group. Verify with handler tests for the save, view, list and ancestor-edit warning scenarios.
- [x] 3.5 Add the unpaginated listing the tree view needs, leaving the paginated form as it is. Verify a handler test gets every group in one response and that the existing pagination tests still pass.
- [x] 3.6 Mention an override in the activity summary and log it at warn level when a save leaves one. Verify with a handler test asserting both, and one asserting neither happens for a clean save.

## 4. Callers of matching

- [x] 4.1 Move `internal/encapi` to the tree. Verify the existing ENC handler tests pass, add one for a child narrowed out by its parent, and one asserting the response of a node in a warned group has only `classes`, `parameters` and `environment`.
- [x] 4.2 Move `internal/groupnodes` (node list, node counts, `Resolver.GroupCertnames`, `AssignedEnvironments`) to the tree, loading all groups where a single group was fetched. Verify with tests for the two new node-count scenarios (child never exceeds parent, All Nodes counts the fleet) and that a group's job targets exclude a node outside its parent.
- [x] 4.3 Return, from the group-nodes endpoint, the pinned certnames that are not in the group. Verify with handler tests for the three scenarios under Pinned nodes excluded by an ancestor are indicated.
- [x] 4.4 Change `internal/demodata` to configure the built-in root instead of creating its own "All Nodes" group, and nest at least two of its groups so the demo shows a hierarchy. Verify `cmd/demo-seed` runs against a fresh database and leaves exactly one root.

## 5. Frontend

- [x] 5.1 Render the groups page as an indented tree built from `parentId`, using the unpaginated listing, with All Nodes first and a warning badge on groups that have warnings. Verify in the running console against the demo data that the nesting matches the seeded parents and counts still load.
- [x] 5.2 Add the parent selector to the group form (default All Nodes; the group and its descendants not offered) and the all/any choice to the rule editor, keeping the JSON rule view in step with it. Verify by creating a child group with an "any" rule in the running console and reloading it.
- [x] 5.3 Lock the name, parent, rule and pins fields when editing All Nodes. Verify in the running console that its classes can be saved and the locked fields cannot be changed.
- [x] 5.4 Show the override warning on the group page on load and after save, and after saving an ancestor name the descendants it now loses to. Verify in the running console by giving a parent a higher priority than a child that sets the same parameter.
- [x] 5.5 Mark pinned nodes that fall outside the parent, with the unavailable case handled, and show the server's message when a delete is refused because of children. Verify both in the running console.
- [x] 5.6 Run `go run ./i18n extract && go run ./i18n merge` in `frontend`, and translate the new strings in the German catalogue so it stays complete. Verify `go run ./i18n status` reports German at 100% and the i18n lint test passes.

## 6. Documentation

- [x] 6.1 Update `architecture-summary.md`'s classifier decision to describe the tree, the rank rule and why the flat-groups decision was reversed. Verify it no longer says precedence is by priority alone.
- [x] 6.2 Rewrite the precedence section of `marketing/guides/use/classify.md` (which says groups never inherit) to cover parents, narrowing, pins, child-wins with the 10/1/5 example, the warning, and any-of rules, and add the upgrade note about finishing a cluster rollout before nesting groups. Verify the guides tests pass (`go test ./marketing/guides/...`).
- [x] 6.4 Correct the classification feature page (`marketing/templates/pages/features/classification.tmpl`), which said groups are flat and merge by priority. Verify it no longer claims either.
- [x] 6.3 Rebuild the site with `make marketing` so the groups screenshots show the tree, and check the result with `make marketing-serve`. Verify the classify guide and classification feature page render with the new screenshots.

## 7. Integration check

- [x] 7.1 Against the dev stack (`make up`, `make run`), nest a group under another, run `make openvox-test`, and confirm the agent's catalog reflects the child's value over a higher-priority parent and omits a child whose parent the node does not match.
- [x] 7.2 Run `make test` and `openspec validate add-group-hierarchy --strict`, and confirm both pass.

## Workflow follow-up

- Archive the change once reviewed, so the classifier spec carries the hierarchy before the group-scoped access changes are proposed against it.
