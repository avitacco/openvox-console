## MODIFIED Requirements

### Requirement: Endpoints require authentication
The system SHALL require a valid, unexpired, unrevoked access token
carrying the `nodes:read` permission on the node package list,
fleet-wide package search, and per-node reporting-toggle status
endpoints, matching the existing node inventory endpoints' permission
gate. The system SHALL require the `orchestrator:run` permission on the
per-node reporting-toggle set endpoint, matching the permission that
gates other node-affecting dispatched actions.

#### Scenario: Unauthenticated request rejected
- **WHEN** a request to the node package list, package search,
  reporting-toggle status, or reporting-toggle set endpoint has no
  valid access token
- **THEN** the system rejects the request

#### Scenario: Authenticated request with the required permission succeeds
- **WHEN** a request to the node package list, package search, or
  reporting-toggle status endpoint presents a valid access token
  carrying `nodes:read`, or a request to the reporting-toggle set
  endpoint presents one carrying `orchestrator:run`
- **THEN** the system processes the request as before

#### Scenario: Read permission is not sufficient to change the toggle
- **WHEN** a request to the reporting-toggle set endpoint presents a
  valid access token carrying `nodes:read` but not `orchestrator:run`
- **THEN** the system rejects the request

## ADDED Requirements

### Requirement: Per-node reporting toggle
The system SHALL let an authorized user read whether a node currently
reports package inventory, and enable or disable it, by dispatching to
that node over the node transport rather than maintaining its own
separate record of the setting - the node's own reported state is
authoritative.

#### Scenario: Reading a node's current reporting state
- **WHEN** a user requests the reporting-toggle status for a node
- **THEN** the system dispatches a status request to that node and
  returns the state it reports

#### Scenario: Enabling reporting for a node
- **WHEN** a user requests that a node's package inventory reporting be
  enabled
- **THEN** the system dispatches an enable request to that node and
  returns the resulting state

#### Scenario: Disabling reporting for a node
- **WHEN** a user requests that a node's package inventory reporting be
  disabled
- **THEN** the system dispatches a disable request to that node and
  returns the resulting state

### Requirement: Toggling requires the node to be currently connected
The system SHALL reject a reporting-toggle read or set request for a
node that is not currently connected to the node transport, identifying
the node as offline, rather than queuing the request for a future
connection.

#### Scenario: Toggling a disconnected node
- **WHEN** a reporting-toggle read or set request targets a node that
  is not currently connected
- **THEN** the system rejects the request, identifying the node as
  offline, and does not queue it for later delivery

### Requirement: Toggling is audited
The system SHALL emit an audit event for an enable or disable
reporting-toggle request under the existing `nodes/inventory` audit
category, subject to that category's configured audit level, matching
how other mutating actions in that category are audited.

#### Scenario: Enabling or disabling reporting is audited
- **WHEN** a user's request to enable or disable a node's package
  inventory reporting is processed, and the `nodes/inventory` category's
  audit level is `writes` or `full`
- **THEN** an audit event records the action, the acting identity, and
  the affected node
