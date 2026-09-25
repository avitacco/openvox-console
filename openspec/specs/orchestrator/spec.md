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
running a Puppet plan (a sequence of tasks/runs with defined ordering),
tracking each as a job the same way an on-demand run is tracked.

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
