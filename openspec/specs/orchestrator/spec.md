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
