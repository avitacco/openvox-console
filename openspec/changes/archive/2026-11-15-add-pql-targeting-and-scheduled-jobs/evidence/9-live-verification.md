# Live verification on the dev stack (2026-11-15)

Console built from this change, run on the host against the compose
stack (`CONSOLE_PUPPET_SERVER_URL=https://localhost:8140`, a scratch
`CONSOLE_SECRETS_KEY_FILE`), with the `task-e2e-agent.example.com`
container as the connected node, running the node agent client built
from this tree. `openvoxserver` is the fleet's other node and has no
agent client, so it is "not connected" in every job. Pages were driven
in headless Chromium; screenshots are in `screenshots/`.

## Queries against the real openvoxdb (1.3)

| Query | Status | Answer |
|---|---|---|
| `inventory[certname] { facts.os.family = "Debian" }` | 200 | both nodes |
| `nodes[certname] { certname in facts[certname] { name = "kernel" } limit 3 }` (explicit subquery) | 200 | both nodes |
| `nodes[certname] { facts { name = "kernel" } limit 3 }` (implicit subquery) | 200 | both nodes |
| `nodes[certname] { certname = }` | 400 | `PQL parse error at line 1, column 30: …` (recorded as the test fixture) |
| `nodes[count()] { }` | 200 | `[{"count":2}]`, no `certname` |
| `nodes[certname] { certname ~ 'task-e2e' }` | 200 | single-quoted strings are accepted |
| `nodes[certname] { certname ~ "a{2,3}" }` | 200 | a brace inside a string is not a block |

Both subquery forms need a second brace block, as the design assumes.

## Targets and node scope (9.2)

A user `w5-runner` was given `orchestrator:run` and `orchestrator:read`
on a group holding only `task-e2e-agent.example.com`.

| Request | As admin | As `w5-runner` |
|---|---|---|
| preview `inventory[certname] { facts.os.family = "Debian" }` | both nodes | `task-e2e-agent.example.com` only |
| preview group "Debian Family" (both nodes) | both nodes | `task-e2e-agent.example.com` only |
| preview the explicit subquery | both nodes | 400 "subqueries need access to every node…" |
| preview `nodes[certname] { certname = "openvoxserver" }` | that node | "No nodes match at the moment." |
| run on `[task-e2e-agent…, openvoxserver]` by name | - | 403, `outOfScope: ["openvoxserver"]` |

The same four cases were driven through the Run Puppet dialog on the
Jobs page as `w5-runner`, with the same answers shown in the dialog.

## Schedules (9.2)

Two console instances, clustered, both running the scheduler
(`orchestrator-scheduler` in each one's "workers started" line), the
node connected to the first.

| Schedule | Due | Outcome |
|---|---|---|
| 223 "Debian no-op check": no-op run on group Debian Family, every 5 minutes, created through the form | 14:49, 14:54, 14:59 | one job each time (2756, 2757, 2760), `triggeredBy: admin`, `runOptions: {noop: true}`, target source the group, naming the schedule. `openvoxserver` failed "agent not connected" in each, as in an on-demand job |
| 224: no-op run on the Debian query, owned by `w5-runner` | 14:55 | job 2758 on `task-e2e-agent.example.com` only, `triggeredBy: w5-runner` |
| 225: run on the node by name, owned by `w5-runner`, whose role was removed at 14:56:15 | 14:57 | skipped: "the schedule's owner no longer holds orchestrator:run". No job |
| 226: task `task_fixture::secret` with `password`, owned by admin | 14:56 | job 2759 succeeded, result `{"length": 17}` (the 17-character value reached the node); the schedule, the job's parameters and every API response show `Sensitive [value redacted]` |

The no-op runs reached Puppet on the node (`Info: Using environment
'production'` … `Notice: Catalog compiled by openvoxserver`) and then
failed in Puppet: a seeded dev group assigns `profile::apt`, which the
deployed code does not have. That is the dev stack's state, not this
change.

The first firing of 223 (job 2756) failed on the node with `exec:
"puppet": executable file not found`: the client had been started by
hand without `NODE_AGENT_PUPPET_BIN_PATH`. Restarted with it before the
14:54 firing.

## Missed firings (9.2)

Both console instances were stopped at 15:00:40 and one was started
again at 15:19:34, on the real clock.

| Schedule | Due while down | When the console came back |
|---|---|---|
| 227 "w5 maintenance window": one-off no-op run | 15:02 | recorded **missed**: "found 18m0s after it was due, with no console instance running the scheduler in between; not started". No job. State finished |
| 223: every 5 minutes | 15:04, 15:09, 15:14, 15:19 | **one** missed firing, for 15:04 ("found 16m0s after it was due…"). No job. Next start 15:24, on its grid |

Both appear on the Activity page as `schedule.missed`, with the owner
as actor, beside `schedule.created`, `schedule.paused`,
`schedule.resumed`, `schedule.deleted` and the earlier
`schedule.skipped`.

After the schedules were deleted, job 2757 still reads `schedule:
{name: "Debian no-op check"}`, with no id.

## Pages (7.1 to 7.6)

- **Jobs page, Run Puppet, by query** (`jobs-run-puppet-query.png`): a
  rejected query shows openvoxdb's parse error beside the query box;
  editing clears it; choosing Run with no preview lists the two nodes
  and says to check them, and starts nothing.
- **Jobs page, Run task** (`jobs-run-task.png`): Run stays disabled
  until a task is picked; an empty required parameter and an empty node
  list are each refused in place; the preview is shown before anything
  starts.
- **Schedule form** (`schedule-form-refused.png`): saving with no name,
  then with a start time in the past, is refused beside that field and
  keeps what was typed. Saved with a group, no-op, a start two minutes
  ahead and "every 5 minutes"; reopened for editing with every field
  as saved.
- **Schedule instead, from a group's page**
  (`schedule-form-from-group.png`): the form opens with No-op ticked and
  the group as the target, not its nodes.
- **Schedule page** (`schedule-detail.png`): state, action, nodes,
  start, repeat, next start and owner, with each firing and its job.
  Pause and Resume switch the state and the buttons; resuming a
  one-off whose time has passed is refused with "This schedule's start
  time has passed; give it a new start time to run it"; Delete asks,
  then returns to the list, and the schedule answers 404.
- **Scheduled jobs list** (`schedules-list.png`): the administrator
  sees all four; `w5-runner` sees their two.
- **Job page** (`job-started-by-schedule.png`): "Node group: Debian
  Family" and "Started by schedule: Debian no-op check".
- **A user with `orchestrator:read` only**: no Run buttons on Jobs, no
  New schedule, no controls on a schedule's page, and the schedule form
  redirects away.

Not driven in the browser: the hidden query on a partly visible job
(covered by `TestJobOrigin_StoredAndRead`).

## Left on the dev stack

Removed: the five schedules, the users `w5-runner` and `w5-reader`,
their two roles, the group "w5 agent only", and the client binary
copied into the node container. The console instances and the node
container are stopped.

Left: jobs 2756 to 2760 in the job history. The scheduler tests share
this database and also leave their jobs behind (triggered by
`sched-test-…`), some at "running" because their fake transport was
holding them when the test ended; the job reaper fails those in time.
