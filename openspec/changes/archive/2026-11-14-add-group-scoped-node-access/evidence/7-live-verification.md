# Live verification (tasks 7.2 - 7.4)

Run on 2026-11-14 against the local dev stack (Postgres, openvoxdb and
openvoxserver in Docker; the console built from this change and run on
the host). The dev fleet holds one node, `task-e2e-agent.example.com`,
a Linux/Debian-family node, and the demo node groups.

## 7.2 Upgrade changes nothing for existing users

The dev database was on migration 29 with six roles. After migration 30:

```
1   admin              nodeGroups [203]
231 Auditor            nodeGroups [203]
232 Operator           nodeGroups [203]
233 Classifier Editor  nodeGroups [203]
234 Release Engineer   nodeGroups [203]
235 Security Analyst   nodeGroups [203]
```

203 is the root group, All Nodes. The bootstrap admin's `/api/v1/me`
reports `"nodeScope":{"all":true,...}`, the node list total is 1 and the
dashboard summary total is 1, as before.

## 7.3 A scoped user, through the API and in the browser

Two users were created, each with one role carrying `nodes:read`,
`classifier:read`, `vulnerabilities:read` and `code:read`:

- `live-scope-linux`, role granted on **Debian Family** (the node is in it)
- `live-scope-windows`, role granted on **Windows Servers** (the node is not)

| Request | Debian Family user | Windows Servers user |
| --- | --- | --- |
| `/api/v1/me` nodeScope | not all, Debian Family | not all, Windows Servers |
| `/api/v1/nodes` | 1 node | 0 nodes, total 0 |
| `/api/v1/nodes/summary` | total 1 | total 0 |
| `/api/v1/nodes/{node}` | full facts | `{"certname":…,"facts":{}}` |
| `/api/v1/nodes/{node}/reports` | report history | `[]` |
| `/api/v1/nodes/{node}/packages` | package list | `[]` |
| `/api/v1/groups/node-counts` | root 1, Linux Servers 1, Debian Family 1 | every group 0 |
| `/api/v1/packages/catalog` | 91 packages, coverage 1 of 1 | empty, coverage 0 of 0 |
| `/api/v1/vulnerabilities` coverage | 1 node not assessed | 0 nodes |
| `/api/v1/groups/187/nodes` | the node | `[]` |

In headless Chromium, signed in as each user:

- **Nodes page** - Debian Family user: the notice "You are seeing the
  nodes in this node group and the groups beneath it: Debian Family"
  above the one node. Windows Servers user: the same notice naming
  Windows Servers, and the empty-fleet message.
  (`screenshots/nodes-page-scoped-to-*.png`)
- **Dashboard** - Debian Family user: the node in the inventory table.
  Windows Servers user: "No nodes matched".
- **Node page by direct URL** - Debian Family user: the full node page.
  Windows Servers user: the page of a node with no data, the same as
  for a certname that does not exist.
- **Roles page as admin** - the Node groups column shows All Nodes for
  the six existing roles and the granted group for the two new ones;
  **Change** opens the group tree with the role's group ticked; the new
  role form has All Nodes preselected.
  (`screenshots/roles-page-editor-open.png`)

Also confirmed against the live API:

- Deleting Debian Family while a role was granted on it: `409`,
  "this group is granted to one or more roles: remove it from those
  roles first"; the group was unchanged.
- Creating a role naming group id 999999: `400`, "role names a node
  group that does not exist".

Not exercised live: the connectivity listing. This dev run has no CA
client configured and the node was not connected, so the listing is
empty for every caller. It is covered by
`internal/nodeconnectivity/scope_test.go` and by the route sweep.

## 7.4 A node moving between groups, without a new login

Using the Windows Servers user's existing access token throughout:

```
before:                       0 nodes
node pinned into the group:   1 node  after 9 s
pin removed:                  0 nodes after 30 s
```

Both inside the one minute the spec allows; the 30 s is the scope
cache's lifetime.

## Clean-up

The two users and two roles were deleted and the pin removed; the
roles list is back to the original six.
