# node-agent Specification

## Purpose

Runs on a managed node and is what actually executes an on-demand run,
task, or plan step the console requests, connecting to the console's
NATS-based node transport.

## Requirements

### Requirement: Connects using the node's existing Puppet certificate
The system SHALL authenticate to the console's node transport using the
certificate and private key already issued to the node by its normal
Puppet agent enrollment, without performing any separate certificate
request or enrollment step of its own.

#### Scenario: Starting on a node with an existing signed certificate
- **WHEN** the client starts on a node that has already completed
  normal Puppet agent certificate enrollment
- **THEN** it connects to the configured node transport using that
  certificate and key, with no additional enrollment step

#### Scenario: Starting on a node with no certificate yet
- **WHEN** the client starts on a node that has not yet completed
  Puppet agent enrollment (no certificate present)
- **THEN** it fails to start with an error identifying the missing
  certificate, rather than attempting to request one itself

### Requirement: Automatic reconnection
The system SHALL maintain a persistent connection to the configured
node transport, reconnecting automatically (with backoff) if the
connection is lost, so a transient network issue or console restart
does not require manual intervention on the node.

#### Scenario: Node transport connection is lost and recovers
- **WHEN** the client's connection to the node transport drops and it
  becomes reachable again
- **THEN** the client reconnects without requiring a restart or manual
  action on the node

### Requirement: Executes an on-demand run request
The system SHALL, on receiving a run request over its node transport
connection, execute a Puppet run on the node and report the outcome
(success/failure, exit code, and relevant output) back over that
connection.

#### Scenario: A run request completes successfully
- **WHEN** the client receives a run request
- **THEN** it executes a Puppet run on the node and reports success
  with the run's outcome once it finishes

#### Scenario: A run request fails
- **WHEN** a Puppet run triggered by a request fails
- **THEN** the client reports failure with enough detail (exit code,
  error output) to distinguish it from a successful run

### Requirement: Executes a task request
The system SHALL, on receiving a task request naming a task and its
parameters, execute that task on the node and report its outcome back
the same way a run's outcome is reported.

#### Scenario: A task request runs with its parameters
- **WHEN** the client receives a task request naming a task and a set
  of parameters
- **THEN** it executes that task with those parameters and reports the
  result

### Requirement: Single in-flight request per client
The system SHALL execute at most one request at a time per client,
rejecting (rather than silently queuing or dropping) a new request that
arrives while one is already running.

#### Scenario: A second request arrives while one is running
- **WHEN** a run or task request arrives while the client is still
  executing a previous one
- **THEN** the client rejects the new request with a response
  indicating it is busy, rather than queuing it silently or dropping
  the in-progress one

### Requirement: Executes a package-inventory toggle request
The system SHALL, on receiving a package-inventory toggle request, set
whether the node reports package inventory (by writing or removing its
`package_inventory` external fact accordingly) and, when a change was
made, immediately run a Puppet run so the effect is reflected without
waiting for the node's next scheduled run - and report the resulting
state back over the connection. The system SHALL also support a
status-only request that reports the current state without changing it
or triggering a run.

#### Scenario: Enabling reporting on a node that does not yet report it
- **WHEN** the client receives a toggle request to enable package
  inventory reporting, and the fact is not currently present
- **THEN** it writes the `package_inventory` external fact, runs a
  Puppet run, and reports that reporting is now enabled

#### Scenario: Disabling reporting on a node that currently reports it
- **WHEN** the client receives a toggle request to disable package
  inventory reporting, and the fact is currently present
- **THEN** it removes the `package_inventory` external fact, runs a
  Puppet run, and reports that reporting is now disabled

#### Scenario: Toggling to the state it is already in is a no-op run
- **WHEN** the client receives a toggle request for the state package
  inventory reporting is already in
- **THEN** it makes no filesystem change, does not trigger a Puppet
  run, and reports the current (unchanged) state

#### Scenario: A status request reports state without side effects
- **WHEN** the client receives a status-only request
- **THEN** it reports whether the `package_inventory` external fact is
  currently present, without writing, removing, or triggering a run

#### Scenario: A toggle request arrives while another request is running
- **WHEN** a package-inventory toggle or status request arrives while
  the client is already executing a run, task, or another
  package-inventory request
- **THEN** the client rejects the new request with a response
  indicating it is busy, per the existing single-in-flight-request
  constraint

### Requirement: Keeps an enabled package-inventory fact current
The system SHALL, when it starts, compare the node's `package_inventory`
external fact (if present) against the fact content built into the
running client, and rewrite it in place when they differ, so upgrading
the client delivers package-inventory fact fixes to nodes that already
report package inventory. The refresh SHALL NOT enable reporting on a
node where the fact is absent, and SHALL NOT trigger a Puppet run.

#### Scenario: Starting with a stale fact present
- **WHEN** the client starts on a node whose `package_inventory` external
  fact is present but differs from the content built into the client
- **THEN** it rewrites the fact with the built-in content, without
  triggering a Puppet run, and the node's next Puppet run reports data
  produced by the current fact

#### Scenario: Starting with a current fact present
- **WHEN** the client starts on a node whose `package_inventory` external
  fact already matches the content built into the client
- **THEN** it makes no filesystem change

#### Scenario: Starting with reporting disabled
- **WHEN** the client starts on a node with no `package_inventory`
  external fact
- **THEN** it does not create one, and package-inventory reporting stays
  disabled

#### Scenario: The refresh fails
- **WHEN** the client cannot rewrite a stale fact (for example because
  the facts directory is not writable)
- **THEN** it logs the failure and continues starting and connecting to
  the node transport normally, rather than failing to start
