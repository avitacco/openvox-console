# orchestrator Specification

## Purpose

Lets an operator trigger a Puppet run, task, or plan against a node or
node group on demand from the console, tracks each request through to
completion over the node transport, and makes the result visible
alongside the report it produced - the console's answer to Puppet
Enterprise's orchestrator.

## Requirements

### Requirement: On-demand Puppet run
The system SHALL, given a request to run Puppet on one or more target
nodes, dispatch a run request to each connected target's node-agent
connection and track the resulting job to completion.

#### Scenario: Triggering a run against a connected node
- **WHEN** an operator triggers a run against a node with an active
  node-agent connection
- **THEN** the system dispatches a run request to that node and the job
  transitions from pending to running

#### Scenario: Triggering a run against a disconnected node
- **WHEN** an operator triggers a run against a node with no active
  node-agent connection
- **THEN** the job is recorded as failed with a reason indicating the
  node is not connected, rather than left pending indefinitely

### Requirement: Task and plan execution
The system SHALL support running a Puppet task against target nodes and
running a plan (a sequence of tasks and Puppet runs with defined
ordering), tracking each as a job the same way an on-demand run is
tracked. A plan's steps SHALL accept what an on-demand run or task
accepts. A plan SHALL take its target as a run does, resolved once and
used for every step. A plan with a step that would be refused SHALL be
refused whole, naming the step, before anything is created or sent.

#### Scenario: Triggering a task run
- **WHEN** an operator triggers a task with its required parameters
  against a target node
- **THEN** the system dispatches the corresponding task request and
  tracks its completion as a job

#### Scenario: A plan runs its steps in the declared order
- **WHEN** an operator triggers a plan composed of multiple steps
- **THEN** the system executes those steps in the plan's declared order
  and the plan's job is not reported complete until every step has
  finished

#### Scenario: A run step with options
- **WHEN** a plan has a run step asking for a no-op run
- **THEN** that step's run is sent to the nodes as a no-op run

#### Scenario: A task step in an environment
- **WHEN** a plan has a task step naming the environment `staging`
- **THEN** the task is taken from `staging` and its parameters are
  checked against that environment's task

#### Scenario: A plan targeted by a node group
- **WHEN** a plan is started on a node group
- **THEN** every step runs on the nodes that were in the group when the
  plan started

#### Scenario: A bad step refuses the whole plan
- **WHEN** a plan's third step names a task that does not exist
- **THEN** the request is refused naming step 3, and no step starts

### Requirement: Job status and history
The system SHALL record every job (trigger source, target, kind, status,
start/finish time, and per-target result) and expose it via an API and
console UI, restricted to users with `orchestrator:read`. Triggering a
run, task, or plan SHALL require `orchestrator:run`.

#### Scenario: Viewing job history
- **WHEN** a user with `orchestrator:read` opens the job history page
- **THEN** the system displays every recorded job, most recent first,
  with its status

#### Scenario: Triggering a job requires the run permission
- **WHEN** a request to trigger a run, task, or plan presents a token
  carrying `orchestrator:read` but not `orchestrator:run`
- **THEN** the system rejects the request

### Requirement: Job result correlation with reports
The system SHALL, for a completed Puppet run job, link the job's record
to the openvoxdb report that run produced, so a job's detail view can
show the same resource-level event detail the report/event capability
already provides.

#### Scenario: A completed run's job links to its report
- **WHEN** an on-demand run job completes and openvoxdb has ingested the
  resulting report
- **THEN** the job's detail view links to that report's event detail

### Requirement: Job actions recorded in the activity log
The system SHALL record a job trigger and its terminal outcome (success
or failure) in the activity log, the same way code deployments and
classification changes are recorded.

#### Scenario: Triggering a run is recorded
- **WHEN** an operator triggers a run, task, or plan
- **THEN** an activity log entry is recorded naming the actor and the
  target

#### Scenario: A job's completion is recorded
- **WHEN** a job reaches a terminal state (succeeded or failed)
- **THEN** an activity log entry is recorded naming the outcome

### Requirement: Initial Puppet run for a newly connected node
The system SHALL dispatch a Puppet run to a node when that node connects
over the node transport and the system holds no inventory record for it,
so that a freshly enrolled node reports its own facts, packages and state
without an operator triggering a run by hand.

The system SHALL dispatch at most one such automatic run per node
identity, and SHALL NOT retry automatically when that run fails or the
node never reports. A run dispatched this way SHALL be recorded as a
normal job - visible in the job list, in the activity log and in the audit
log - and SHALL be attributed to the system itself rather than to any
user, since no user requested it.

Nodes that already have an inventory record SHALL NOT be dispatched a run
on connection, so an ordinary reconnection, restart or network blip never
causes unrequested work on a managed node.

#### Scenario: A newly enrolled node connects for the first time
- **WHEN** a node with no inventory record connects over the node
  transport
- **THEN** the system dispatches a Puppet run to that node
- **AND** records it as a job attributed to the system rather than a user
- **AND** the node's facts and status become visible once the run
  completes and its report is submitted

#### Scenario: An already-inventoried node reconnects
- **WHEN** a node that already has an inventory record connects
- **THEN** the system dispatches no run
- **AND** creates no job record

#### Scenario: An agent reconnects repeatedly before its first run finishes
- **WHEN** a node with no inventory record connects, is dispatched an
  initial run, and then connects again one or more times before that run
  produces a report
- **THEN** the system dispatches no further automatic run for that node
- **AND** exactly one automatic job exists for it

#### Scenario: The initial run fails
- **WHEN** an automatically dispatched initial run fails
- **THEN** the job is recorded with its failure, visible to an operator
  like any other failed job
- **AND** the system does not dispatch a replacement run automatically

#### Scenario: The node is no longer reachable when dispatch is attempted
- **WHEN** a node with no inventory record connects but has disconnected
  again by the time the run is dispatched
- **THEN** the dispatch fails as it would for any unreachable target,
  recorded against the job
- **AND** the failure does not prevent other nodes' initial runs from
  being dispatched

#### Scenario: Many nodes enroll at once
- **WHEN** a number of nodes with no inventory records connect within a
  short interval, as when a provisioning run enrolls a batch of machines
- **THEN** the system dispatches an initial run for each of them without
  dropping any node's run
- **AND** limits how many it dispatches concurrently, so that a large
  batch degrades into a queue rather than a simultaneous burst

### Requirement: Puppet run options
The system SHALL accept, with a request to run Puppet, the options
`noop` (report what would change and change nothing), `environment`
(ask the node to run against the named environment) and `debug`
(debug-level output), in any combination. A request with no options
SHALL behave exactly as a run did before options existed. Options SHALL
NOT alter the permission a run needs, the node scope check, or its
all-or-nothing treatment of targets.

#### Scenario: A no-op run
- **WHEN** an operator triggers a run with `noop` against a connected
  node
- **THEN** the node performs a no-op Puppet run and nothing on it is
  changed

#### Scenario: A run against a named environment
- **WHEN** an operator triggers a run with an `environment`
- **THEN** the node is asked to run against that environment

#### Scenario: A run with no options
- **WHEN** an operator triggers a run with no options
- **THEN** the node receives the same request as before options existed,
  whatever version its client is

#### Scenario: An invalid environment name
- **WHEN** a run is requested with an `environment` that is not a valid
  environment name
- **THEN** the request is refused as invalid, and no job is created

#### Scenario: An environment the server does not have
- **WHEN** a run is requested with an `environment` the server is known
  not to have
- **THEN** the request is refused as invalid, naming the environment,
  and no job is created

#### Scenario: A target outside the caller's node groups
- **WHEN** a run with options names a node outside the caller's groups
  for `orchestrator:run`
- **THEN** the request is refused exactly as a run without options is

### Requirement: A run with options is never carried out as a plain run
The system SHALL send a run with options as a request that a node agent
client predating run options rejects. A target whose client rejects it
SHALL be recorded as failed with a reason saying its client must be
upgraded, and SHALL NOT have run Puppet.

#### Scenario: A no-op run sent to an older client
- **WHEN** a no-op run targets a node whose client predates run options
- **THEN** that target fails with a reason naming the client upgrade,
  and no Puppet run takes place on the node

#### Scenario: Mixed client versions
- **WHEN** a run with options targets one node with a current client
  and one with an older client
- **THEN** the first runs with the options and the second fails as
  above

### Requirement: Run options are recorded and shown
The system SHALL record a run's options on its job and return them with
the job. The jobs list and the job page SHALL show them, and the
activity log entry for the run SHALL name them. A run with options
SHALL record the agent's output for each target on the job.

#### Scenario: Viewing a no-op job
- **WHEN** a user views a job that was a no-op run
- **THEN** the job is shown as a no-op run, in the list and on its page

#### Scenario: Reading what a no-op run would change
- **WHEN** a no-op run finishes on a node with pending changes
- **THEN** the job page shows, for that node, output listing what would
  have changed

#### Scenario: Reading a debug run
- **WHEN** a run with `debug` finishes
- **THEN** the job page shows the debug output for each node, truncated
  with a notice when it exceeds the stored limit

#### Scenario: The activity log
- **WHEN** a no-op run against environment `feature_x` is triggered
- **THEN** its activity entry says it was a no-op run against
  `feature_x`

### Requirement: A requested environment that was not used is marked
The system SHALL, for a run that requested an environment, mark each
target whose resulting report records a different environment, naming
both. The run dialog SHALL state that a node whose classification sets
an environment runs in that one.

#### Scenario: Classification sets another environment
- **WHEN** a run requests `feature_x` on a node whose classification
  sets `production`, and the node's report records `production`
- **THEN** the job page marks that node as having run in `production`
  rather than `feature_x`

#### Scenario: The requested environment was used
- **WHEN** a run requests `feature_x` and the node's report records
  `feature_x`
- **THEN** no mark is shown for that node

#### Scenario: No report yet
- **WHEN** a target of such a run has no correlated report
- **THEN** no mark is shown for that node

### Requirement: Console UI for run options
The system SHALL offer the run options wherever the console starts a
Puppet run on chosen nodes - the nodes page, a node's page and a group's
page - in a dialog that names the nodes or their count, and SHALL start
a plain run when none is chosen.

#### Scenario: Running with no options chosen
- **WHEN** a user opens the run dialog and confirms without choosing an
  option
- **THEN** a plain run is started

#### Scenario: Choosing no-op
- **WHEN** a user ticks no-op and confirms
- **THEN** a no-op run is started and the user is taken to, or linked
  to, its job

#### Scenario: Choosing an environment
- **WHEN** a user opens the environment choice
- **THEN** the server's environments are offered, with a note that a
  node's classified environment takes precedence

### Requirement: Targets chosen by a query
The system SHALL accept, in place of a list of nodes, a PQL query as the
target of a Puppet run or a task. It SHALL run the query against
openvoxdb when the request is made and start the job on the distinct
`certname` values the query returns. Every other part of the request
SHALL be checked exactly as it is for a list of nodes.

#### Scenario: A run on the nodes a query matches
- **WHEN** an operator triggers a run with the query
  `inventory[certname] { facts.os.family = "Debian" }` and three nodes
  match
- **THEN** one job is created with those three nodes as its targets

#### Scenario: A task on the nodes a query matches
- **WHEN** an operator triggers a task with a query as its target
- **THEN** the task's parameters are validated as usual, and the job
  runs on the nodes the query returns

#### Scenario: A node returned more than once
- **WHEN** a query returns the same `certname` in several rows
- **THEN** the node is a target once

#### Scenario: More than one kind of target
- **WHEN** a request gives both a list of nodes and a query, or both a
  query and a node group
- **THEN** the request is refused as invalid, and no job is created

### Requirement: A query that cannot be used is refused
The system SHALL refuse a query target, creating no job, when the query
is empty, is longer than 4,000 characters, is not in PQL string form,
is rejected by openvoxdb, or returns a row without a `certname`. The
refusal SHALL say which, and SHALL carry openvoxdb's own message when
openvoxdb rejected the query.

#### Scenario: A query openvoxdb rejects
- **WHEN** a run is requested with a query that openvoxdb answers as a
  syntax error
- **THEN** the request is refused as invalid with openvoxdb's message,
  and no job is created

#### Scenario: A query that returns no certname
- **WHEN** a run is requested with `nodes[count()] { }`
- **THEN** the request is refused as invalid, saying the query must
  return `certname`

#### Scenario: A query in AST form
- **WHEN** a run is requested with a query written as a JSON array
- **THEN** the request is refused as invalid, saying a PQL string is
  required

#### Scenario: openvoxdb cannot be reached
- **WHEN** a run is requested with a query while openvoxdb is
  unreachable
- **THEN** the request fails as a gateway error, and no job is created

### Requirement: Targets chosen by a node group
The system SHALL accept a node group as the target of a Puppet run or a
task, and SHALL start the job on the nodes that are in that group when
the request is made.

#### Scenario: A run on a group
- **WHEN** an operator triggers a run naming the group "Web Servers",
  which four nodes are in
- **THEN** one job is created with those four nodes as its targets

#### Scenario: A group that does not exist
- **WHEN** a run is requested naming a node group id that does not
  exist
- **THEN** the request is refused as invalid, and no job is created

### Requirement: A target that resolves to no nodes starts nothing
The system SHALL refuse a run or a task whose query or node group
resolves to no nodes, creating no job, with a reason saying that
nothing matched.

#### Scenario: A query matching nothing
- **WHEN** a run is requested with a query no node matches
- **THEN** the request is refused as invalid, saying no nodes matched,
  and no job is created

#### Scenario: An empty group
- **WHEN** a task is requested on a node group with no nodes in it
- **THEN** the request is refused the same way

### Requirement: Target preview
The system SHALL let a caller with `orchestrator:run` ask which nodes a
list, a query or a node group resolves to, without starting anything.
The answer SHALL be the nodes a job started by that caller at that
moment would run on, and a refusal SHALL be the refusal that job would
have received.

#### Scenario: Previewing a query
- **WHEN** an operator previews a query that five nodes match
- **THEN** the five node names are returned, and no job is created

#### Scenario: Previewing an unusable query
- **WHEN** an operator previews a query openvoxdb rejects
- **THEN** the preview is refused with the same message a run would get

#### Scenario: A caller without the run permission
- **WHEN** a caller holding `orchestrator:read` only asks for a preview
- **THEN** the request is rejected

### Requirement: A job records where its targets came from
The system SHALL record, on a job whose targets came from a query or a
node group, the query text or the group, and on a job started by a
schedule, that schedule. Both SHALL be returned with the job in the job
list and the job detail. A job started from a list of nodes SHALL
record neither.

#### Scenario: A job started by a query
- **WHEN** a job started by a query is read
- **THEN** it carries the query text alongside the nodes it ran on

#### Scenario: A job started by a group that was later deleted
- **WHEN** a job started on a node group is read after the group has
  been deleted
- **THEN** it still carries the group's name as it was when the job
  started

#### Scenario: A job started by a schedule
- **WHEN** a job a schedule started is read
- **THEN** it names the schedule, and its "triggered by" is the
  schedule's owner

#### Scenario: A job started from a list of nodes
- **WHEN** a job started from a list of nodes is read
- **THEN** it carries no target source

### Requirement: Console UI for choosing targets
The console UI SHALL let a user holding `orchestrator:run` start a
Puppet run or a task from the Jobs page, choosing the targets as a list
of nodes, a query, or a node group, and SHALL show the nodes the choice
resolves to before the job is started. The Jobs page and a job's page
SHALL show a job's query or group and the schedule that started it.

#### Scenario: Starting a run from the Jobs page with a query
- **WHEN** a user opens Run Puppet on the Jobs page, chooses "Query",
  types a query and asks for the preview
- **THEN** the matching nodes and their count are shown, and confirming
  starts a job on them

#### Scenario: A query that is refused
- **WHEN** the query typed is refused
- **THEN** the reason is shown beside the query, and the job cannot be
  started until a preview succeeds

#### Scenario: The existing entry points
- **WHEN** a user starts a run from a node's page, a selection on the
  Nodes page, or a group's page
- **THEN** the dialog names those nodes as it does today, without the
  target chooser

#### Scenario: A user without the run permission
- **WHEN** a user holding `orchestrator:read` only opens the Jobs page
- **THEN** the buttons for starting a job are not offered

#### Scenario: A job's page
- **WHEN** a user opens a job that a schedule started with a query
- **THEN** the page shows the query and links to the schedule

### Requirement: A failed step ends the plan
The system SHALL treat a step as failed when it fails on any of its
nodes, SHALL NOT start any later step, and SHALL close each later step
as not run, recording which step failed as the reason. The plan's job
SHALL then be failed.

#### Scenario: The second of three steps fails on one node
- **WHEN** a plan's second step fails on one of its four nodes
- **THEN** the plan is failed, the third step is recorded as not run
  because step 2 failed, and nothing is sent to any node for it

#### Scenario: No step is left open
- **WHEN** a plan has ended, by success or by failure
- **THEN** none of its steps is still reported as running

### Requirement: A plan's job is read with its steps
The system SHALL return, when a plan's job is read, its steps in order,
each with its kind, task, options, state, start and finish time and
per-node results. The job list SHALL show a plan as one entry and SHALL
NOT list its steps as entries of their own. A step read by itself SHALL
name its plan.

#### Scenario: Reading a plan
- **WHEN** a caller with `orchestrator:read` reads a plan's job
- **THEN** the response carries the steps in their declared order with
  each step's state

#### Scenario: The job list
- **WHEN** a plan of three steps has run
- **THEN** the job list shows one entry for it, not four

#### Scenario: Reading a step
- **WHEN** a caller reads one step's job
- **THEN** the response names the plan it belongs to and its position

### Requirement: A job that reports progress is judged by its silence
The system SHALL fail a job that reports progress when it has reported none for
ten minutes, together with its unfinished targets, and SHALL NOT fail it for
its age alone. A job that does not report progress keeps the rule that fails it
two hours after it started.

#### Scenario: A long run that keeps reporting is not reaped
- **WHEN** a patch job or code plan job has run for five hours and its last progress report was a minute ago
- **THEN** it is left running

#### Scenario: A run that stopped reporting is failed
- **WHEN** a patch job or code plan job's last progress report was eleven minutes ago
- **THEN** the job and its unfinished targets are failed with a reason that says progress stopped being reported

#### Scenario: Other kinds are unchanged
- **WHEN** a Puppet run, task or saved plan has been running for two hours
- **THEN** it is failed as before, however recently anything was reported

### Requirement: A patch run is a job
The system SHALL record a patch run as a job of its own kind, with one target
per node, readable, listed and filtered like any other job and subject to the
same node-scope reading rules.

#### Scenario: A patch run appears in the job list
- **WHEN** a patch run has started
- **THEN** the job list shows it as a patch job with its targets and status

### Requirement: A task job may include agentless targets
The system SHALL let a task job, including a task step of a plan or a
scheduled task, name agentless targets together with nodes that have an agent,
checked against node scope and task permissions as any target is, and SHALL
track each target to its own result.

#### Scenario: A mixed job
- **WHEN** a task is run on one node with an agent and one agentless target
- **THEN** each gets its own result and the job completes when both have

### Requirement: Runs that need the agent are refused for agentless targets
The system SHALL refuse, before creating anything, a Puppet run, a patch run,
or a plan with a run step whose targets include an agentless target.

#### Scenario: A plan with a run step
- **WHEN** a plan with a run step is started on a group containing an agentless target
- **THEN** it is refused whole, naming the step and the target

### Requirement: A code plan is a job whose steps are jobs
The system SHALL record the run of a plan from environment code as a job of
its own kind, whose task steps are child jobs listed with it in the order they
ran, together with a trace of every step and the plan's returned value or the
reason it failed, readable under the same node-scope rules as any job.

#### Scenario: A plan run in the job list
- **WHEN** a code plan has run two task steps
- **THEN** the job list shows one code plan job, and reading it shows both steps' jobs and its trace

#### Scenario: A reader limited to some nodes
- **WHEN** a plan ran a task step on nodes the reader may not see
- **THEN** that step's targets are left out and the job says some are hidden
