## ADDED Requirements

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
