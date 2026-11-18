---
title: Schedule jobs
summary: Save a Puppet run, a task or a plan to start later, once or repeatedly, on nodes that are worked out each time it starts.
order: 5
---

A **schedule** is a Puppet run, a task or a [plan](run-plans.html) saved to start by itself: once,
at a time you choose, or repeatedly. It does what the same job would do
if you started it by hand at that moment - see [Run jobs](run-jobs.html) -
and each time it starts, it is recorded as a job like any other.

Creating and changing schedules needs `orchestrator:run`; seeing them
needs `orchestrator:read`.

## Create a schedule

Open **Jobs**, then **Scheduled jobs**, then **New schedule**. Or fill
in **Run Puppet**, **Run task** or **Run plan** anywhere and choose **Schedule
instead**, which opens the same form with what you had filled in. From
a node group's page, that schedules the group, not the nodes in it
today.

1. Give it a **Name**. Jobs it starts are labelled with it.
2. Choose **What to run**: Puppet, with any
   [run options](run-jobs.html#run-options); a task, with its
   parameters; or a plan, chosen from the saved plans.
3. Choose **Which nodes**: a list, a query, or a node group, as in
   [Choose the nodes](run-jobs.html#choose-the-nodes). **Preview nodes**
   shows what it would run on now.
4. Choose **When**: the start time and, if it should repeat, how often.
5. **Save schedule**.

The console checks a schedule when you save it exactly as it would
check the job if you started it now, and refuses it with the reason
beside the part that is wrong. One difference: a query or a group that
matches no nodes yet can be saved.

### Nodes are worked out each time

A query or a node group is worked out again every time the schedule
starts. A node that joins the group next week is included next week; a
node that stops matching the query is left out.

### Times and repeating

Times are entered and shown in your browser's time zone, which the form
names. Someone in another zone sees the same moment in theirs.

A schedule that does not repeat starts once, and is then shown as
**Finished**. A repeating schedule starts at its start time and then
every so many minutes, hours, days or weeks after it. The shortest
repeat is 5 minutes.

> [!NOTE]
> A repeat is a fixed length of time: a day is 24 hours and a week is
> seven of them. Where the clocks change for daylight saving, a daily
> schedule keeps its time in UTC and moves by an hour on the local
> clock. There are no calendar rules such as "the first Monday".

A repeating schedule may be given a start time in the past. It next
starts at its first repeat that is still ahead.

## What a schedule runs as

A schedule runs as its **owner**: the person who last saved it. Each
time it starts, the console applies the owner's roles as they are at
that moment, not as they were when it was saved:

- If the owner can no longer run jobs - their role was removed, or
  their account or service token deleted - nothing is started.
- If the owner's role is granted on particular node groups, a query or
  a group gives the matching nodes in those groups. Nodes named in a
  list must all still be in them, or nothing is started.
- If the owner's role is limited to some tasks, the schedule starts only
  tasks they may still run, and only on the nodes they may run them on.
  Saving a schedule for a task you may not run is refused, naming it.

Saving a change to a schedule makes you its owner, and the form says so
when you open someone else's. That is how to take over a schedule from
someone who has left: open it and save it. Pausing and resuming do not
change the owner.

You can see and change the schedules you own. Someone whose role is
granted on **All Nodes** can see and change everyone's. See
[Users, roles and tokens](access.html).

## When a schedule does not start

A schedule's page lists what happened each time it came due.

| Outcome | What it means |
| --- | --- |
| **Started** | A job was started. The line links to it. |
| **Skipped** | Nothing was started, for the reason shown. The schedule carries on at its next time. |
| **Missed** | No console was running the scheduler when it came due, and by the time one was, it was more than 15 minutes late. |
| **Interrupted** | The console instance starting it stopped before it could. |

A schedule is skipped when:

- its query or group matches no nodes, or none the owner can act on;
- its owner can no longer run jobs, or a node it names has left the
  owner's node groups;
- its task, or one of its plan's tasks, is one its owner's roles no
  longer allow (the firing names the task, and the step for a plan) -
  see [Limit a role to some tasks](access.html#limit-a-role-to-some-tasks);
- its task, a task parameter, its environment or its node group no
  longer exists as the schedule describes it;
- openvoxdb or the Puppet server could not be reached to work out the
  nodes or the task;
- the job it started last time is still running.

A firing that did not start is never retried. A run meant for 02:00
does not start at 09:00 because the console was down overnight, and a
task is never started twice for one due time. A repeating schedule that
was missed over several repeats records one missed firing and resumes
at its next time.

Skipped and missed firings are also recorded on the **Activity** page,
and the **Scheduled jobs** list shows each schedule's last outcome.

A node that is not connected when a schedule starts fails in that job
with "agent not connected", as in a job started by hand; the job still
starts for the other nodes.

### A scheduled plan keeps its own steps

When you choose a plan, the schedule takes a copy of its steps and
shows them. From then on the schedule runs that copy. Changing or
deleting the saved plan does not change what the schedule runs.

To pick up a change to the plan, edit the schedule, choose the plan
under **Plan** again, and save. Leaving **Plan** at "Keep the steps
shown below" keeps the copy as it is.

The reason is that a schedule runs with its owner's access, while
everyone who can run jobs can change a plan - see
[Run plans](run-plans.html#schedule-a-plan).

If a step can no longer run when the schedule comes due - its task has
been removed from the code, say - nothing is started, and that time is
recorded as **Skipped**, naming the step.

## Pause, change or delete a schedule

On the schedule's page:

- **Pause** stops it coming due. **Resume** starts it again from its
  next time still ahead; nothing is started for the times that passed.
  A schedule that does not repeat, and whose time passed while it was
  paused, needs a new start time instead.
- **Edit** opens the form. Saving checks it again, as a new schedule.
- **Delete** removes it. The jobs it started stay in the job history,
  still naming it.

Deleting a node group does not delete the schedules that target it.
They are skipped, saying the group no longer exists, until you change
or delete them.

## Sensitive task parameters

A task parameter marked `sensitive` - a password, a token - has to be
kept until the schedule starts. The console stores it encrypted with
its secrets key and shows `Sensitive [value redacted]` everywhere the
schedule or its jobs appear. The same goes for the sensitive values of
a scheduled plan's task steps, which the schedule form asks for under
each step.

- Without a secrets key, a schedule with a sensitive value is refused.
  Set `CONSOLE_SECRETS_KEY_FILE` - see
  [Track vulnerabilities](vulnerabilities.html), which uses the same
  key - on every instance that serves the console or runs the
  scheduler.
- When you edit your own schedule, leave a sensitive field empty to
  keep the stored value.
- When you save someone else's schedule, you must enter its sensitive
  values again. The console does not hand a stored secret to a new
  owner.
- If the secrets key changes, the stored values can no longer be read.
  The schedule is skipped, saying so, until it is saved again with the
  values.

## Through the API

```sh
curl -X POST https://console.example.com/api/v1/orchestrator/schedules \
  -H "Authorization: Bearer <access token>" \
  -H 'Content-Type: application/json' \
  -d '{"name":"Weekly no-op",
       "kind":"run",
       "options":{"noop":true},
       "group":12,
       "startAt":"2026-01-04T03:00:00Z",
       "repeat":{"every":1,"unit":"week"}}'
```

`kind` is `run` (with `options`), `task` (with `task`, `params`,
`environment` and `timeoutSeconds`, as in a task request) or `plan`
(with `planName` and `steps`, as in a
[plan request](run-plans.html#through-the-api)). The nodes
are `targets`, `query` or `group`. `repeat.unit` is `minute`, `hour`,
`day` or `week`; leave `repeat` out for a schedule that starts once.

| Request | Does |
| --- | --- |
| `GET /api/v1/orchestrator/schedules` | lists the schedules you can see |
| `GET /api/v1/orchestrator/schedules/<id>` | one schedule, with what happened each time it came due |
| `PUT /api/v1/orchestrator/schedules/<id>` | replaces it, and makes you its owner |
| `POST /api/v1/orchestrator/schedules/<id>/pause` and `/resume` | pauses or resumes it |
| `DELETE /api/v1/orchestrator/schedules/<id>` | deletes it |

A refused request answers `400` (or `403` for a node outside your node
groups) with an `error`, and a `field` naming the part of the schedule
it concerns - and, for a plan, the `step` that was refused. A schedule owned by a service token runs for as long as
that token exists and holds `orchestrator:run`.

## Where schedules are started

Schedules are started by instances in the `all` and `orchestrator`
[run modes](run-modes.html). When several are running, each due time is
started by exactly one of them.
