# Live verification on the dev stack

Run 2026-11-17 against a single console instance (mode `all`) with the real
Puppet server and one connected node (`task-e2e-agent`). The second node,
`openvoxserver`, has no node agent: a job started on it is recorded and fails
with "agent not connected", which is all these checks need to show it was
allowed. Users, roles, groups and schedules were created for the run and
removed afterwards. Other accounts on the dev stack are real people, so the
Users screenshot is of the test user's row only.

## Setup

- Node groups `g9-web` (pins the connected node) and `g9-db` (pins
  `openvoxserver`).
- Roles, each with `orchestrator:read`, `orchestrator:run`, `nodes:read`,
  `classifier:read`: `g9-echo-on-web` (task list `task_fixture::echo`, on
  `g9-web`) and `g9-any-on-db` (every task, on `g9-db`).
- A user `g9-member`, a user group `g9-web-ops`.

## A user who holds roles through a group

| Step | Answer |
| --- | --- |
| a user with no roles starts a task | **403**, missing `orchestrator:run` |
| grant `g9-echo-on-web` to the group, add the user to it, assign `g9-any-on-db` to the user directly | 204 each |
| `GET /users/{id}/roles` | `g9-echo-on-web` via `["g9-web-ops"]`, `g9-any-on-db` via `["direct"]` |
| `echo` on the web node (the group's role) | started |
| `plain` on the web node | **403**: `your roles do not allow the task "task_fixture::plain" on this node` |
| `plain` on the database node (the direct role) | started |
| `plain` on both, named | **403**, naming the web node |

This is the pairing across a group and a direct role: the group's role allows
only `echo`, and only on the web tier; the direct role allows everything on the
database tier; having both widens neither.

## Leaving the group

- Removed from the group (204): a **new login** starting `echo` on the web node
  is refused (the node is outside what the user's remaining role covers); the
  same user can still start `plain` on the database node, through the direct
  role.
- A schedule saved by the member for `echo` on the web node, with the member
  then removed from the group before it came due: when it fired the outcome was
  **skipped**, `this node is not in the node groups the caller can act on:
  task-e2e-agent.example.com`, and no job started. (An earlier schedule that
  fired after the member had been added back to the group started its job, as
  it should.)

## Deleting a group

`DELETE /user-groups/{id}` with a member: **409**, `memberCount: 1`; the group
stays. With `?confirm=true`: 204. The console asks "1 person will lose the
roles this group gives them." first (`delete-confirm.png`).

## Console

- `groups-page.png`: the User groups page, with member counts and the roles
  each group holds as checkboxes.
- `members-dialog.png`: changing a group's members (other accounts hidden).
- `delete-confirm.png`: the confirmation for a group with members.
- `users-page-via-group.png`: the test user holds `g9-any-on-db` directly
  (ticked), is in `g9-web-ops`, and the role from the group is shown as
  "g9-echo-on-web via g9-web-ops" and cannot be unticked.
- `roles-page.png`: the role shows "Granted to groups: g9-web-ops" under its
  name.
- With the language set to German the page, the member count ("1 Mitglied") and
  the confirmation ("1 Person verliert die Rollen, die diese Gruppe ihr gibt.")
  are translated.

## Things found

- Nothing needed changing. The last-administrator refusal cannot be shown live
  (the bootstrap administrator holds `rbac:admin` directly, so no group change
  can leave nobody); it is covered by `TestGroups_TheLastAdministratorCannot
  BeRemovedThroughAGroup`, which runs against a scratch database where the
  group is the only way anyone holds it.
