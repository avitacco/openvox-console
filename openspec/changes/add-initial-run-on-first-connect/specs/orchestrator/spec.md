## ADDED Requirements

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

#### Scenario: Many nodes enrol at once
- **WHEN** a number of nodes with no inventory records connect within a
  short interval, as when a provisioning run enrols a batch of machines
- **THEN** the system dispatches an initial run for each of them without
  dropping any node's run
- **AND** limits how many it dispatches concurrently, so that a large
  batch degrades into a queue rather than a simultaneous burst
