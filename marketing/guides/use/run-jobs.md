---
title: Run jobs
summary: Run Puppet or a Puppet task on demand on one node, a selection, the nodes a query matches, or a node group, and see what each node did.
order: 3
---

The console can run Puppet on nodes right now, rather than at their next
scheduled run, and run the Puppet tasks your deployed code contains. Each
request is a **job**, recorded with every node it targeted and what
happened on each.

Starting a job needs `orchestrator:run`; seeing jobs needs
`orchestrator:read`. A node must be connected to be reached: see
[Add nodes](add.html).

If your role is granted on particular node groups, you can start jobs
only on nodes in those groups, and you see only their jobs. A job that
names a node outside them is refused whole - nothing runs on the other
nodes either - and the console says which nodes to leave out. See
[Users, roles and tokens](access.html).

To carry out several runs and tasks in order, see
[Run plans](run-plans.html). To start a job later, or repeatedly, see
[Schedule jobs](schedule-jobs.html).

## Run Puppet

From wherever you are looking at the nodes you want:

- **One node:** on the node's page, run Puppet.
- **A selection:** on the **Nodes** page, select nodes, then run Puppet
  on the selection.
- **Every node a group matches:** on the group's page, run Puppet on its
  matching nodes.

Each opens a dialog naming the nodes. Confirm it to start a job, which
you are taken to.

Or start from the **Jobs** page, where **Run Puppet** and **Run task**
let you say which nodes: see [Choose the nodes](#choose-the-nodes).

### Run options

The dialog offers three options. With none chosen, the run is an
ordinary one.

- **No-op** reports what the run would change and changes nothing. The
  job shows the list for each node.
- **Debug output** records Puppet's debug log for each node on the job.
- **Environment** asks the node to run against that environment, for
  trying a node against a branch.

> [!WARNING]
> The environment is a request, not an override. When a node's node
> groups set an environment, Puppet uses that one whatever the run asks
> for. The job marks each node that ran in a different environment from
> the one requested, once the node's report arrives.

A run with options records what Puppet printed. Open **Output** under a
node's line on the job to read it. Long output is cut off at 256 KB,
and says so.

Run options need a node agent client from this version of the console
or later. An older client refuses the request, and the node's line
fails saying the client is too old. Puppet is not run on that node at
all, so a no-op request is never carried out as a real run. Upgrade the
client - see [Keep node agents current](agent-upgrades.html) - and run again.
A run with no options works on every client version.

Through the API, with an access token:

```sh
curl -X POST https://console.example.com/api/v1/orchestrator/runs \
  -H "Authorization: Bearer <access token>" \
  -H 'Content-Type: application/json' \
  -d '{"targets":["web01.example.com","web02.example.com"]}'
```

Add `options` for a run with options. Any of the three may be left out:

```sh
  -d '{"targets":["web01.example.com"],
       "options":{"noop":true,"environment":"feature_x","debug":true}}'
```

The request is refused, and no job is created, if the environment name
is not valid, if openvoxserver has no such environment, or if it names
an option the console does not know.

## Choose the nodes

**Run Puppet** and **Run task** on the **Jobs** page ask which nodes to
run on, in one of three ways:

- **List of nodes:** certnames, one per line.
- **Query:** a [PQL](https://www.puppet.com/docs/puppetdb/latest/api/query/v4/pql.html)
  query that returns `certname`. The console runs it against openvoxdb
  when the job starts, and the job runs on the nodes it returns:

  ```
  inventory[certname] { facts.os.family = "Debian" and facts.os.release.major = "12" }
  ```

- **Node group:** the nodes in the group when the job starts.

**Preview nodes** shows the nodes your choice means right now. The
dialog always shows them to you before it starts anything: choosing
**Run** the first time runs the preview, and the second time starts the
job. A query or a group that matches no nodes starts nothing.

A query must be in PQL's own syntax and return `certname`. A query the
console or openvoxdb cannot use is refused with the reason, which for a
syntax error is openvoxdb's own message pointing at the mistake.

If your role is granted on particular node groups, a query or a group
gives you the matching nodes that are in *your* groups, and says
nothing about any others. Your query also cannot contain a subquery -
one entity's braces inside another's - because such a query could be
used to find out about nodes you cannot see. Query a single entity, as
in the example above; `inventory` reaches every fact and trusted fact
of a node. Nodes you name in a list are still all or nothing.

Through the API, give `query` or `group` (a node group's ID) in place
of `targets`:

```sh
curl -X POST https://console.example.com/api/v1/orchestrator/runs \
  -H "Authorization: Bearer <access token>" \
  -H 'Content-Type: application/json' \
  -d '{"query":"inventory[certname] { facts.os.family = \"Debian\" }",
       "options":{"noop":true}}'
```

`POST /api/v1/orchestrator/targets/resolve` takes the same `targets`,
`query` or `group` and answers with the nodes, starting nothing.

## Run a task

A task is a script shipped in a module's `tasks` directory, with a
metadata file describing its parameters. The console runs the ones in
the code you have deployed - see [Deploy code](deploy-code.html) - so
there is nothing to install for a task beyond deploying the module that
contains it.

Tasks are offered in the same places as Puppet runs: **Run task** on a
node's page, on the **Nodes** page's selection, on a group's matching
nodes, and on the **Jobs** page. In the dialog:

1. Pick the **Environment** to take the task from. Tasks are read from
   that environment's code as it is deployed right now.
2. Pick the **Task**. Its description, and a field for each parameter,
   appear below it. A field marked `*` is required; one left empty gets
   the default its metadata declares.
3. Optionally, set a **Timeout**. A task still running when it runs out
   is stopped, along with anything it started, and reported as timed out.
   It is 30 minutes unless you say otherwise, and at most 55.
4. **Run task** starts the job and takes you to it.

The console checks the parameters against the task's metadata before it
starts anything, and marks each field it refuses with the reason. No job
is created until they are right.

A role can be limited to some tasks, and then the **Task** list offers
only the ones you may run. A task you may not run is refused with `403`
and its name, whether you pick it from the list or ask for it through the
API - see [Limit a role to some tasks](access.html#limit-a-role-to-some-tasks).

A parameter the task's metadata marks `sensitive` - a password, a token -
is typed into a masked field. Its value is sent to the nodes and nowhere
else: the job's stored parameters, its page, the activity log and the
audit log all show `Sensitive [value redacted]` instead.

Through the API, naming the task, its parameters and, optionally, the
environment and the timeout in seconds:

```sh
curl -X POST https://console.example.com/api/v1/orchestrator/tasks \
  -H "Authorization: Bearer <access token>" \
  -H 'Content-Type: application/json' \
  -d '{"targets":["web01.example.com"],
       "task":"service",
       "params":{"action":"restart","name":"nginx"},
       "environment":"production",
       "timeoutSeconds":600}'
```

The API checks the same way: an unknown task, a missing or unexpected
parameter, or a value of the wrong type is refused with `400`, and its
`fields` name each problem. The task catalogue behind the dialog is in
the API too: `GET /api/v1/tasks/environments`,
`GET /api/v1/tasks?environment=production`, and
`GET /api/v1/tasks/<module>/<task>?environment=production` (a module's
own task is `<module>/init`).

### What the node does

The node's agent downloads the task's files from the Puppet server its
own Puppet agent uses, with its own certificate, and runs nothing unless
every file matches the checksum the console looked up. It keeps verified
files, so running the same task again downloads nothing.

A task's script is run directly, so its first line decides what runs it
(`#!/bin/sh`, `#!/usr/bin/env python3`). Ruby tasks run with the Ruby
that comes with the Puppet agent, and on Windows, PowerShell tasks run in
PowerShell. Where a task offers several implementations, the node picks
the one that suits it.

A node finds its Puppet server by asking its own Puppet configuration.
A node that finds it through DNS SRV records instead has no address to
read there; set `NODE_AGENT_PUPPET_SERVER=puppet.example.com:8140` in
its agent's environment.

Nodes need an agent from a console with task support. A node whose agent
is older fails every task with `Error: Unknown subcommand 'task'`: run
the node's install script again to upgrade it - see
[Add nodes](add.html).

## Follow a job

The **Jobs** page lists every job, newest first. A job's page shows each
target on its own line - its status, exit code and output - so a job that
worked on nine nodes and failed on one says exactly that.

![](shot:job-result)

If a job also ran on nodes outside your node groups, its page shows
only your nodes and says so. The job's overall status still covers
every node it ran on, so it can read **failed** when all the nodes you
can see succeeded.

For a task, each target's line opens to show the task's result: the
JSON it printed, or its text under `_output`. A task fails when it exits
with anything but 0, or reports an `_error`, whose message is shown as
the line's error.

A node that was not connected when the job started fails at once with
"agent not connected", instead of holding the job open.

When a run finishes, its line links to the report that run produced, with
every resource it changed.

The jobs list and a job's page show a run's options: **No-op**, the
environment it asked for, and **Debug**.

They also show where a job's nodes came from, when it was not a list:
the query or the node group. A job that a schedule started names the
schedule, and is recorded as triggered by the schedule's owner. If a
job started by a query also ran on nodes outside your node groups, you
are shown your nodes but not the query.

Every job's start and outcome is also recorded on the **Activity** page.

A [plan](run-plans.html) is one line in the list. Its page shows its
steps in order, each with its own state and node results.

## A job whose instance stopped

A job is followed to the end by the console instance that started it. If
that instance stops before its targets finish, the job would otherwise
show as running forever; instead, it is marked failed once it has been
running far longer than any run could take.
