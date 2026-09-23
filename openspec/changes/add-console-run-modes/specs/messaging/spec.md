## MODIFIED Requirements

### Requirement: Embedded NATS server
The system SHALL run a NATS server embedded in-process as part of normal
binary startup, with no separate process or external broker required.

When no cluster peers are configured, the embedded server SHALL run
unclustered and SHALL NOT open a network listener, preserving the
single-instance deployment's existing behavior. When cluster peers are
configured, the embedded server SHALL open a listener for peer connections
only, and SHALL NOT expose internal subjects to any client that is not an
authenticated peer.

#### Scenario: Binary starts with embedded messaging
- **WHEN** the console binary starts
- **THEN** an in-process NATS server is running and ready to accept
  internal publish and subscribe calls before the HTTP server begins
  accepting requests

#### Scenario: A single instance opens no messaging listener
- **WHEN** the console binary starts with no cluster peers configured
- **THEN** the embedded NATS server accepts internal publish and subscribe
  calls
- **AND** it does not listen on any network address for messaging

#### Scenario: A clustered instance listens only for peers
- **WHEN** the console binary starts with cluster peers configured
- **THEN** the embedded NATS server listens for peer connections
- **AND** a client that cannot authenticate as a peer is refused

## ADDED Requirements

### Requirement: Cross-instance event delivery
When multiple instances are configured as peers, the system SHALL deliver an
event published on any one instance to every subscriber on every other
instance in the cluster, so that internal events which must be acted on
fleet-wide are not confined to the instance that published them.

The system SHALL authenticate peer connections, and SHALL refuse a peer that
cannot present the configured cluster credentials, so that joining the
internal event bus is not available to anything that can merely reach the
listener.

#### Scenario: An event published on one instance reaches another
- **WHEN** an internal component on one instance publishes an event to a
  subject
- **AND** a component on a different instance in the same cluster is
  subscribed to that subject
- **THEN** the subscriber on the other instance receives the event

#### Scenario: Token revocation takes effect fleet-wide
- **WHEN** an access token is revoked on one instance
- **THEN** every other instance in the cluster rejects that token on
  subsequent requests
- **AND** no instance requires a restart for the revocation to take effect

#### Scenario: An unauthenticated peer is refused
- **WHEN** a process that cannot present the configured cluster credentials
  attempts to connect to an instance's peer listener
- **THEN** the connection is refused
- **AND** it receives no internal events

#### Scenario: An instance rejoins after losing contact
- **WHEN** an instance loses contact with its peers and later regains it
- **THEN** it resumes sending and receiving cross-instance events without
  being restarted

### Requirement: Edge instance event subscription
The system SHALL allow an instance to participate in the internal event bus
as a subscriber without being a full cluster peer, so that an instance
deployed outside the core cluster's network can still act on events it
depends on without every core instance needing to reach it.

#### Scenario: An edge instance receives events it depends on
- **WHEN** an instance is configured to attach to the cluster as an edge
  subscriber rather than a peer
- **THEN** it receives events published anywhere in the cluster
- **AND** the core instances do not require network reachability to it
