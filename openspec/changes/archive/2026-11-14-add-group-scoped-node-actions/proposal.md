## Why

`add-group-scoped-node-access` limited what a user can **see** to the
node groups their roles are granted on, and left what they can **do**
fleet-wide. Today someone whose roles show them only the web servers can
still run Puppet on, remove, or revoke the certificate of any node whose
name they know, and the Jobs page still names every node. Until actions
follow the same groups, node group scope is a view filter, not access
control.

## What Changes

- **Actions are limited to the caller's node groups** for the permission
  that gates the action, using the scope mechanism already in place:
  - Puppet runs, tasks and plans (`orchestrator:run`)
  - switching a node's package reporting (`orchestrator:run`)
  - upgrading a node's client and removing a node (`nodes:manage`)
  - signing, revoking and cleaning a certificate (`nodes:certs:manage`)
- An action naming any node outside the caller's groups is **refused
  whole** with a 403 that lists those nodes; nothing is started for the
  nodes that were in scope. A name that matches no node is refused the
  same way, so the refusal does not reveal which nodes exist.
- **Jobs are limited too** (`orchestrator:read`): a job is listed when
  at least one of its nodes is in the caller's groups, and shows only
  those nodes' results. A job with none answers as a job that does not
  exist.
- Read and action scope stay independent, per permission: a role can
  read All Nodes while another lets the same person run Puppet on one
  group only.
- The console shows the refusal in place - which nodes were outside the
  caller's groups - on the nodes page, node page, group page and task
  dialog.
- The route sweep test is extended from GET routes to every
  permission-gated route, so a new action endpoint must be either
  scoped or listed as exempt.
- **BREAKING** for narrowed roles only: a user whose role was narrowed
  under the previous change loses the ability to act on nodes outside
  those groups. Roles on All Nodes, and service tokens, are unaffected.

Decided here, closing questions the read-side change left open:

- **The activity log stays unscoped.** It is the audit surface;
  `activity:read` remains a fleet-wide permission and the guide says to
  grant it accordingly.
- **Nodes that have never reported** (including pending certificate
  requests) are still in no group but All Nodes unless pinned. A team
  signs its own new nodes' certificates when the node is pinned into
  the team's group first; otherwise signing needs a fleet-wide role.
  Nothing about certificate signing itself changes.

Not in this change: scoping group definitions (`classifier:write` on a
subtree), service tokens, code deploys (not a node action), the ENC
endpoint, and the automatic client upgrades the console performs by
itself.

## Capabilities

### New Capabilities

None.

### Modified Capabilities
- `node-access-scope`: actions and jobs come under node group scope;
  the list of unscoped surfaces shrinks.

## Impact

- **Code**: `internal/orchestrator` (trigger handlers, job list and
  detail, store filter), `internal/agentupgrade`, `internal/inventory`
  (node removal), `internal/nodeconnectivity` (certificate actions),
  `internal/packageinventory` (reporting toggle), `internal/nodescope`
  (a helper for "which of these nodes are out of scope"), wiring in
  `internal/app`, and the route sweep test.
- **API**: the action endpoints gain a 403 response with
  `{"error", "outOfScope": [certnames]}`. Job list and detail responses
  are unchanged in shape; a scoped caller receives fewer jobs and fewer
  targets.
- **Database**: none.
- **Frontend**: `nodes.js`, `node.js`, `group.js`, `run-task.js`,
  `jobs.js`, `job.js`; new strings in all locales.
- **Guides**: `marketing/guides/use/access.md` (the "see, not do"
  warning is replaced by what is and is not limited),
  `marketing/guides/use/run-jobs.md`,
  `marketing/guides/nodes/certificates.md` (signing as a scoped team),
  `marketing/guides/nodes/remove.md`,
  `marketing/guides/nodes/agent-upgrades.md`.
- **Docs**: `architecture-summary.md` section 3, `operations.md`.
