## MODIFIED Requirements

### Requirement: Node connection lookup
The system SHALL provide a lookup, given a node identity, that returns
either that node's live connection state (for routing a dispatch to it)
or an indication that the node is not currently connected, behind an
interface that does not assume a single-instance deployment.

When multiple instances terminate node connections as peers, the lookup
SHALL reflect connections held by any instance in the cluster, not only
those held by the instance performing the lookup. A node connected to one
instance SHALL NOT be reported as disconnected by another.

#### Scenario: Looking up a connected node
- **WHEN** a lookup is performed for a node identity with an active
  connection
- **THEN** the lookup indicates that node is connected and reachable

#### Scenario: Looking up a disconnected node
- **WHEN** a lookup is performed for a node identity with no active
  connection
- **THEN** the lookup indicates the node is not connected, rather than
  raising an error indistinguishable from a system failure

#### Scenario: Looking up a node connected to a different instance
- **WHEN** a lookup is performed on one instance for a node whose
  connection is terminated at a different instance in the same cluster
- **THEN** the lookup indicates that node is connected and reachable
- **AND** it does not report the node as disconnected

### Requirement: Connection cleanup on disconnect
The system SHALL remove a node's connection from the connection lookup
as soon as its NATS connection closes, for any reason.

When multiple instances terminate node connections as peers, a node's
disconnection SHALL be reflected in the lookup on every instance, not only
on the instance whose connection closed.

#### Scenario: A dropped connection is no longer routable
- **WHEN** a node's NATS connection closes (client disconnect, network
  failure, or server-initiated close)
- **THEN** a subsequent connection lookup for that node's identity
  indicates it is not connected

#### Scenario: A disconnect is visible on every instance
- **WHEN** a node's connection to one instance closes
- **THEN** a subsequent lookup on any other instance in the cluster
  indicates that node is not connected

#### Scenario: An instance terminating connections stops
- **WHEN** an instance holding node connections stops or becomes
  unreachable
- **THEN** the nodes it held are reported as disconnected by the remaining
  instances until those nodes reconnect
- **AND** the remaining instances continue serving dispatches for every
  other connected node

## ADDED Requirements

### Requirement: Cross-instance dispatch routing
When multiple instances terminate node connections as peers, the system
SHALL deliver a dispatch published by any instance to the target node,
regardless of which instance holds that node's connection, and SHALL return
the node's response to the instance that published the dispatch.

Per-node subject isolation SHALL hold across the cluster: routing a dispatch
between instances SHALL NOT allow any node to observe or publish another
node's job traffic.

#### Scenario: Dispatching to a node connected elsewhere
- **WHEN** an instance dispatches a request to a node whose connection is
  terminated at a different instance in the same cluster
- **THEN** the node receives the request
- **AND** the dispatching instance receives the node's response

#### Scenario: Isolation holds across instances
- **WHEN** a node connected to one instance attempts to subscribe to or
  publish on a subject scoped to a node connected to a different instance
- **THEN** the system denies that subscription or publish

#### Scenario: Dispatching to a node connected nowhere
- **WHEN** an instance dispatches a request to a node that holds no
  connection to any instance in the cluster
- **THEN** the dispatch reports that the node is not connected
- **AND** it does so promptly, rather than only after the dispatch's full
  timeout has elapsed

### Requirement: Authenticated transport peering
The system SHALL authenticate a peer instance joining the node transport,
using credentials distinct from those a managed node presents, so that a
node certificate can never be used to join the transport as a peer.

#### Scenario: A peer presents valid peer credentials
- **WHEN** an instance connects to another instance's transport peer
  listener presenting the configured peer credentials
- **THEN** the peering is established and dispatches route between them

#### Scenario: A node certificate cannot establish peering
- **WHEN** a client presents a managed node's client certificate to a
  transport peer listener
- **THEN** the peering is refused
- **AND** the client receives no other node's traffic
