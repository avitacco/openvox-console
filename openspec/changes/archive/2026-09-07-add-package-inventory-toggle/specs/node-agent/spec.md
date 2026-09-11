## ADDED Requirements

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
