# Live verification on the dev stack

Run 2026-11-16 against a single console instance (mode `all`) with the real
Puppet server, openvoxdb and one connected node (`task-e2e-agent`). The second
node, `openvoxserver`, has no node agent: a job started on it is recorded and
fails with "agent not connected", which is all these checks need to show it was
allowed. Users, roles, groups, plans, schedules and the token were created for
the run and removed afterwards.

## Setup

- Node groups: `w8-web` (pins the connected node), `w8-db` (pins
  `openvoxserver`), `w8-both` (pins both).
- Roles, each with `orchestrator:read`, `orchestrator:run`, `nodes:read`,
  `classifier:read`:
  - `w8-echo-on-web`: task list `task_fixture::echo`, granted on `w8-web`.
  - `w8-any-on-db`: no task list, granted on `w8-db`.
- A user holding both roles; a second user holding only a role limited to
  `task_fixture::echo` and `task_fixture::secret`.

## A user with both roles (task 7.2)

| Request | Answer |
| --- | --- |
| `echo` on the web node, named | started (job 4509) |
| `plain` on the web node, named | **403**, outside: the web node |
| `plain` on the database node, named | started (job 4510) |
| `plain` on both, named | **403**, naming the web node |
| `echo` on `w8-both`, a group | started on both nodes (job 4511) |
| `plain` on `w8-both`, a group | started on the database node only (job 4512) |
| plan `[run, echo, plain]` on `w8-both` | started; **every step** (run, echo, plain) holds the database node only (job 4513) |
| plan `[run, echo, plain]` on the web node, named | **403**, `step: 3`, naming `task_fixture::plain` and the web node |
| plan of Puppet runs on the web node | started (job 4517) |

The task picker for this user offers every task, since the second role allows
every task on the database tier.

After a later change, a refusal of this kind says so in words: `your roles do
not allow the task "task_fixture::echo" on this node: task-e2e-agent.example.com`
(the node is inside the user's groups; the task is not allowed on it), with
`task` in the body. Before it, the answer read as if the node were hidden.

## A task-limited user (task 5.3)

- The task list returned `task_fixture::echo` and `task_fixture::secret`; an
  administrator sees all 8.
- Starting `task_fixture::plain` answered **403** `you are not allowed to run
  the task "task_fixture::plain"` with `task`; its details answered **404**,
  as for a task that does not exist.
- A plan saved by an administrator with an `echo` step and a `plain` step: read
  by this user, the `plain` step is returned with `notPermitted: true` and its
  `problem`; the plan page shows it with "Your roles do not allow the task ...",
  and **Run** opens a dialog with Run switched off and "You cannot run this
  plan: a step uses a task your roles do not allow." The plan editor shows the
  step's task as unchosen.
- The Run task dialog offers exactly the two permitted tasks.

Screenshots: `operator-task-picker.png`, `operator-plan-step-not-permitted.png`,
`operator-run-plan-switched-off.png`, `operator-plan-editor.png`.

## Roles and service tokens (tasks 5.1, 5.2)

- New role form with `patch::*` and `pa*ch`: refused, the reason shown under the
  field, both lines kept (`role-form-refused-pattern.png`). With `patch::*` and
  `package::install` (blank lines and padding in the field) the role is created
  and the field cleared.
- Changed in place from the Roles page: `deploy::*` saved; `deploy::*` plus
  `NOT A TASK` refused with the editor still open and the text kept
  (`role-editor-refused-pattern.png`); an empty field saved as "Every task".
- Service token form: `pa*ch` refused on the field; `deploy::*` created and
  listed (`service-token-allowlist.png`).
- `roles-task-allowlists.png` shows the Tasks column.

## A limited service token

A token created with `deploy::*`: starting `task_fixture::echo` answered **403**
naming the task; a Puppet run started.

## Schedules

- Saved by the two-role user for `task_fixture::echo` on the web node while the
  first role allowed it. The role was then narrowed to `task_fixture::secret`.
  When it came due: **skipped**, `your roles do not allow the task
  "task_fixture::echo" on this node: task-e2e-agent.example.com`; no job
  started.
- Saving the same schedule while the role was already narrowed: **403** with
  the same reason, `field: target`.

## Things found

- The message above is the one change made during this run: the first version
  of the out-of-scope refusal for a task did not name the task, so a skipped
  firing named only the node. The spec says it names the task.
- The plan editor shows a step whose task the user may not run as "Choose a
  task" with "Choose a task first." rather than naming the refusal. It is
  accurate (the task cannot be chosen) and the save is refused by the server in
  any case, so it was left as it is.
