## MODIFIED Requirements

### Requirement: On-demand Puppet run
The system SHALL, given a request to run Puppet on one or more target
nodes, dispatch a run request to each connected target's node-agent
connection and track the resulting job to completion.

A target SHALL be treated as connected when it holds a node transport
connection to any instance in the cluster, not only to the instance handling
the request. The system SHALL NOT report a target as not connected merely
because its connection is terminated at a different instance.

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

#### Scenario: Triggering a run against a node connected to another instance
- **WHEN** an operator triggers a run through one instance against a node
  whose transport connection is terminated at a different instance in the
  same cluster
- **THEN** the system dispatches the run to that node and the job
  transitions from pending to running
- **AND** the job is not recorded as failed for want of a connection

#### Scenario: Triggering a run without sticky routing
- **WHEN** an operator triggers runs against the same connected node through
  different instances in the cluster on successive requests
- **THEN** each run is dispatched to that node
- **AND** the outcome does not depend on which instance received the request

## ADDED Requirements

### Requirement: Job completion tracking survives the dispatching instance
The system SHALL record a job's terminal outcome durably, so that an
operator reading job history from any instance sees the same result.

When the instance that dispatched a job stops before that job reaches a
terminal state, the system SHALL record the job as failed with a reason
indicating it could not be tracked to completion, rather than leaving it
recorded as running indefinitely.

#### Scenario: Job history is consistent across instances
- **WHEN** a job dispatched through one instance completes
- **AND** an operator reads that job's record from a different instance
- **THEN** the job's status and per-target results are the same on both

#### Scenario: The dispatching instance stops mid-job
- **WHEN** the instance that dispatched a job stops before the job reaches a
  terminal state
- **THEN** the job is eventually recorded as failed with a reason indicating
  it could not be tracked to completion
- **AND** it is not left recorded as running indefinitely
