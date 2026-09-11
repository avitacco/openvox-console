# messaging Specification

## Purpose

Provides an embedded, in-process publish/subscribe layer that internal
console components use to communicate, without requiring a separately
deployed message broker.

## Requirements

### Requirement: Embedded NATS server
The system SHALL run a NATS server embedded in-process, in unclustered
mode, as part of normal binary startup, with no separate process or
external broker required.

#### Scenario: Binary starts with embedded messaging
- **WHEN** the console binary starts
- **THEN** an in-process NATS server is running and ready to accept
  internal publish and subscribe calls before the HTTP server begins
  accepting requests

### Requirement: Internal publish/subscribe availability
The system SHALL provide an internal mechanism for Go packages within the
binary to publish messages to, and subscribe to messages from, NATS
subjects, without going through an external network hop.

#### Scenario: In-process publish and subscribe
- **WHEN** one internal component publishes a message to a subject
- **THEN** any internal component subscribed to that subject receives the
  message
