## Purpose

Lets console users see, per node, whether the console currently has a
live node transport connection to it - the thing that determines whether
an on-demand run/task can actually be dispatched right now - distinct
from the node inventory's openvoxdb-sourced facts/report status.

## ADDED Requirements

### Requirement: Node connection status endpoint
The system SHALL report, for a requested node or set of nodes, whether
that node currently holds a live node transport connection.

#### Scenario: A connected node reports connected
- **WHEN** a request asks for the connection status of a node that
  currently has a live node transport connection
- **THEN** the system reports that node as connected

#### Scenario: A node with no live connection reports not connected
- **WHEN** a request asks for the connection status of a node with no
  live node transport connection (including a node openvoxdb has never
  heard of)
- **THEN** the system reports that node as not connected, rather than an
  error

### Requirement: Dedicated nodes page
The system SHALL provide a page, separate from the existing node
inventory page, listing every node known to openvoxdb alongside its
current node transport connection status.

#### Scenario: Viewing the nodes page
- **WHEN** a user opens the nodes page
- **THEN** the system displays every known node's name alongside whether
  it currently has a live node transport connection

#### Scenario: A node's connection status updates without a page reload requirement
- **WHEN** a node's connection status changes while a user is viewing the
  nodes page
- **THEN** the displayed status becomes accurate on the user's next
  refresh of the page (a live-updating requirement is not required for
  this capability)

### Requirement: Endpoint requires authentication
The system SHALL require a valid, unexpired, unrevoked access token
carrying the `nodes:read` permission on the node connection status
endpoint, matching the existing node inventory endpoints' permission
gate.

#### Scenario: Unauthenticated request rejected
- **WHEN** a request to the node connection status endpoint has no valid
  access token
- **THEN** the system rejects the request

#### Scenario: Authenticated request with the required permission succeeds
- **WHEN** a request to the node connection status endpoint presents a
  valid access token carrying `nodes:read`
- **THEN** the system processes the request as before
