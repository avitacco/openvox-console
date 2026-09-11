# node-connectivity Specification

## Purpose

Lets console users see, per node, whether the console currently has a
live node transport connection to it - the thing that determines whether
an on-demand run/task can actually be dispatched right now - distinct
from the node inventory's openvoxdb-sourced facts/report status.

## Requirements

### Requirement: Node connection status endpoint
The system SHALL report, for a requested node or set of nodes, whether
that node currently holds a live node transport connection, the
timestamp of that node's most recent connection and most recent
disconnection if known, and whether that certname is one of this
console's own infrastructure identities rather than a managed node -
and if so, a short, specific reason why.

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

#### Scenario: A node's last connected/disconnected timestamps are reported
- **WHEN** a request asks for the connection status of a node that has
  connected and/or disconnected at least once since this console
  instance started
- **THEN** the system reports the timestamp of that node's most recent
  connection and, if it has since disconnected, its most recent
  disconnection

#### Scenario: A node with no connection history reports no timestamps
- **WHEN** a request asks for the connection status of a node that has
  never connected to this console instance
- **THEN** the system reports no connection/disconnection timestamps for
  that node, rather than an error or a fabricated value

#### Scenario: An infrastructure certname is identified with a reason
- **WHEN** a request asks for the connection status of a certname the
  console has identified as one of its own infrastructure identities
  (a certname it presents as a client, or a certname presented to it by
  a server it connects to)
- **THEN** the system reports that certname as infrastructure, along
  with a short, specific reason identifying which connection or role it
  corresponds to

#### Scenario: A managed node is never reported as infrastructure
- **WHEN** a request asks for the connection status of a certname the
  console has not identified as one of its own infrastructure
  identities, including a node whose certificate is signed but has
  never yet connected or run
- **THEN** the system does not report that certname as infrastructure,
  regardless of whether it appears in openvoxdb's inventory yet

### Requirement: Dedicated nodes page
The system SHALL provide a page, separate from the existing node
inventory page, listing every node known to openvoxdb alongside its
current node transport connection status, its most recent connection/
disconnection timestamp (if known), and its Puppet certificate status
(if known). The system SHALL let the user sort this list by any of
those columns, and filter it by node name (substring match), connection
status, and certificate status, applied across every known node rather
than only those currently visible. The system SHALL exclude
infrastructure certnames from this list by default, and SHALL let the
user reveal them via an explicit control; when shown, an infrastructure
row SHALL be visually distinguishable from a managed node's row. The
system SHALL also provide a control that shows the operator how to
onboard a new node, listing the install script location for every
platform this project supports an install script for.

#### Scenario: Viewing the nodes page
- **WHEN** a user opens the nodes page
- **THEN** the system displays every known node's name alongside whether
  it currently has a live node transport connection, its most recent
  connection/disconnection timestamp, and its certificate status

#### Scenario: A node's connection status updates without a page reload requirement
- **WHEN** a node's connection status changes while a user is viewing the
  nodes page
- **THEN** the displayed status becomes accurate on the user's next
  refresh of the page (a live-updating requirement is not required for
  this capability)

#### Scenario: Sorting by a column
- **WHEN** a user selects a column to sort by (node name, connection
  status, last connected timestamp, or certificate status)
- **THEN** the system reorders every known node's row by that column, and
  reversing the selection reverses the order

#### Scenario: Filtering by node name
- **WHEN** a user enters text into the node name filter
- **THEN** the system displays only nodes whose name contains that text,
  across the entire known fleet, not only nodes on a given page

#### Scenario: Filtering by connection or certificate status
- **WHEN** a user selects a specific connection status or certificate
  status to filter by
- **THEN** the system displays only nodes matching that status, across
  the entire known fleet

#### Scenario: Combining sort and filter
- **WHEN** a user has both a filter and a sort order active
- **THEN** the system displays only the filtered nodes, in the selected
  sort order

#### Scenario: Infrastructure certs are hidden by default
- **WHEN** a user opens the nodes page without enabling the
  infrastructure-certs control
- **THEN** the system does not display any row the console has
  identified as an infrastructure identity

#### Scenario: Revealing infrastructure certs
- **WHEN** a user enables the infrastructure-certs control
- **THEN** the system displays infrastructure rows alongside managed
  nodes, each visually distinguishable as infrastructure

#### Scenario: Destructive actions on an infrastructure cert are specifically flagged
- **WHEN** a user revealed infrastructure certs and attempts to revoke
  or clean one
- **THEN** the system's confirmation names that specific certificate's
  role/reason before proceeding, rather than the generic node-focused
  confirmation wording

#### Scenario: Viewing how to add a new node
- **WHEN** a user selects the add-node control on the nodes page
- **THEN** the system displays the install script location for every
  supported platform, as a full URL the user can act on directly rather
  than a route the user must already know to construct

### Requirement: Certificate status reporting
The system SHALL report, for a requested node, that node's Puppet
certificate status (signed, requested, or revoked) as reported by the
configured OpenVox/Puppet Server CA, when certificate status reporting
is configured. When it is not configured, the system SHALL report an
unknown status for every node rather than failing the request.

#### Scenario: A node's certificate status is reported
- **WHEN** certificate status reporting is configured and a request asks
  for a node's certificate status
- **THEN** the system reports that node's certificate status as reported
  by the CA (signed, requested, or revoked)

#### Scenario: Certificate status reporting is not configured
- **WHEN** certificate status reporting is not configured
- **THEN** the system reports every node's certificate status as
  unknown, rather than failing the request

#### Scenario: A node the CA has never heard of reports unknown
- **WHEN** certificate status reporting is configured and a request asks
  for the certificate status of a node the CA has no record of
- **THEN** the system reports that node's certificate status as unknown,
  rather than an error

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

### Requirement: Dashboard connectivity summary
The system SHALL display, on the Dashboard, a count of nodes currently
holding a live node transport connection ("Connected") and a count of
known nodes currently without one ("Disconnected"), alongside the
Dashboard's existing fleet report-status summary.

#### Scenario: Viewing the Dashboard with connected and disconnected nodes
- **WHEN** a user opens the Dashboard and the console has connectivity
  data for one or more nodes
- **THEN** the system displays a "Connected" count of nodes currently
  holding a live node transport connection and a "Disconnected" count
  of known nodes currently without one

#### Scenario: Viewing the Dashboard with node transport disabled
- **WHEN** a user opens the Dashboard and node transport connectivity
  reporting is not configured
- **THEN** the system displays a "Connected" count of zero and a
  "Disconnected" count of zero, rather than omitting the summary or
  showing an error
