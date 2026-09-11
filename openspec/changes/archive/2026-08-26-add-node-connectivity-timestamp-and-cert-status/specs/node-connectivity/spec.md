## MODIFIED Requirements

### Requirement: Node connection status endpoint
The system SHALL report, for a requested node or set of nodes, whether
that node currently holds a live node transport connection, and the
timestamp of that node's most recent connection and most recent
disconnection, if known.

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

### Requirement: Dedicated nodes page
The system SHALL provide a page, separate from the existing node
inventory page, listing every node known to openvoxdb alongside its
current node transport connection status, its most recent connection/
disconnection timestamp (if known), and its Puppet certificate status
(if known).

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

## ADDED Requirements

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
