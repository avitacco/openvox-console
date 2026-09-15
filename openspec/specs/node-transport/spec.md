# node-transport Specification

## Purpose

Accepts a managed node's connection to the console's embedded NATS
server, authenticates it by its existing OpenVox CA client certificate,
and scopes it to only that node's own subjects - the transport every
orchestration capability dispatches jobs over.

## Requirements

### Requirement: Mutual TLS session establishment
The system SHALL accept a node's NATS connection only when it presents a
client certificate signed by the configured OpenVox CA, and SHALL
associate the resulting connection with the certificate's identity
(certname).

#### Scenario: A node with a valid CA-signed certificate connects
- **WHEN** a node opens a NATS connection presenting a client
  certificate signed by the configured OpenVox CA
- **THEN** the system accepts the connection and associates it with the
  certificate's certname

#### Scenario: A node with an untrusted certificate is rejected
- **WHEN** a client presents a certificate not signed by the configured
  OpenVox CA
- **THEN** the system refuses the connection

### Requirement: Per-node subject isolation
The system SHALL restrict each connected node's NATS permissions to only
the subjects scoped to its own certname, so one node can never observe
or publish another node's job traffic.

#### Scenario: A node cannot see another node's job traffic
- **WHEN** a node with an established connection attempts to subscribe
  to or publish on a subject scoped to a different node's certname
- **THEN** the system denies that subscription or publish

### Requirement: Node connection lookup
The system SHALL provide a lookup, given a node identity, that returns
either that node's live connection state (for routing a dispatch to it)
or an indication that the node is not currently connected, behind an
interface that does not assume a single-instance deployment.

#### Scenario: Looking up a connected node
- **WHEN** a lookup is performed for a node identity with an active
  connection
- **THEN** the lookup indicates that node is connected and reachable

#### Scenario: Looking up a disconnected node
- **WHEN** a lookup is performed for a node identity with no active
  connection
- **THEN** the lookup indicates the node is not connected, rather than
  raising an error indistinguishable from a system failure

### Requirement: Connection cleanup on disconnect
The system SHALL remove a node's connection from the connection lookup
as soon as its NATS connection closes, for any reason.

#### Scenario: A dropped connection is no longer routable
- **WHEN** a node's NATS connection closes (client disconnect, network
  failure, or server-initiated close)
- **THEN** a subsequent connection lookup for that node's identity
  indicates it is not connected

### Requirement: Node connection notifications
The system SHALL make node connection events observable by other parts of
the console, so that a node connecting can trigger work rather than only
update a status that something else must poll for.

A notification SHALL identify the node that connected. Notifications
SHALL be delivered without blocking the transport itself: a slow or
failing observer SHALL NOT delay, drop or otherwise affect a node's
ability to connect, to stay connected, or to be dispatched work.

Because a connection notification can now cause work to happen, the
system SHALL tolerate a notification being delivered more than once for
what is logically the same connection - as can happen when a node
reconnects during a network partition, or when more than one console
instance observes the same event. Observers SHALL therefore be able to
make their own handling idempotent, rather than relying on the transport
to deliver exactly once.

#### Scenario: A node connects
- **WHEN** a node establishes a connection over the node transport
- **THEN** observers are notified that the node connected
- **AND** the notification identifies which node it was

#### Scenario: An observer is slow or fails
- **WHEN** an observer of connection notifications blocks, errors, or
  panics while handling one
- **THEN** the node's connection is unaffected
- **AND** the transport continues to accept and route traffic for that
  node and every other node

#### Scenario: The same node connects more than once
- **WHEN** a node disconnects and reconnects, or is otherwise observed
  connecting more than once
- **THEN** a notification is delivered for each connection
- **AND** the transport makes no claim that the notification was
  delivered exactly once for a given node
