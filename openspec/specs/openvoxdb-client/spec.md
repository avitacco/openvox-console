# openvoxdb-client Specification

## Purpose

Provides the console's only path to openvoxdb - executing PQL queries
and submitting commands over its HTTP API and returning parsed results
- so every other capability that needs to read or change node, fact,
report, or event data goes through one consistent, authenticated client
rather than each building its own.

## Requirements

### Requirement: PQL query execution
The system SHALL execute PQL (Puppet Query Language) queries against a
configured openvoxdb instance over HTTPS and return parsed, typed results
to the calling component.

#### Scenario: Successful query
- **WHEN** an internal component issues a valid PQL query while openvoxdb
  is reachable
- **THEN** the system returns the parsed result set to the caller

#### Scenario: Malformed query
- **WHEN** an internal component issues a syntactically invalid PQL query
- **THEN** the system returns an error identifying the query as invalid,
  without sending it to openvoxdb

### Requirement: Authenticated connection to openvoxdb
The system SHALL authenticate to openvoxdb using a client certificate
issued by the same OpenVox CA that OpenVox agents and infrastructure
already use, rather than a separate credential scheme.

#### Scenario: Certificate-authenticated query
- **WHEN** the system connects to openvoxdb
- **THEN** it presents a client certificate issued by the OpenVox CA and
  the connection is accepted

### Requirement: Unreachable openvoxdb is a reported error, not a crash
The system SHALL report a clear, specific error to the calling component
when openvoxdb is unreachable or returns an error response, and SHALL NOT
crash the console process.

#### Scenario: openvoxdb unreachable
- **WHEN** an internal component issues a query while openvoxdb is
  unreachable
- **THEN** the system returns an error identifying openvoxdb as
  unreachable, and the console process continues running

### Requirement: Command submission to openvoxdb
The system SHALL support submitting a command to openvoxdb's command
API (in addition to executing PQL queries), authenticated the same way
as query execution, for internal components that need to change
openvoxdb's state rather than only read it - starting with deactivating
a node.

#### Scenario: Successful command submission
- **WHEN** an internal component submits a valid command while
  openvoxdb is reachable
- **THEN** the system submits it to openvoxdb's command API and reports
  success

#### Scenario: openvoxdb rejects the command
- **WHEN** openvoxdb rejects a submitted command as invalid
- **THEN** the system returns an error identifying the rejection,
  rather than reporting success

#### Scenario: openvoxdb is unreachable during command submission
- **WHEN** an internal component submits a command while openvoxdb is
  unreachable
- **THEN** the system returns an error identifying openvoxdb as
  unreachable, matching how an unreachable query is already reported

### Requirement: Single-node lookup by certname
The system SHALL support looking up a single node by its exact
certname, distinct from the general node-listing query, since openvoxdb
excludes a deactivated or expired node from the general listing
unconditionally but still returns it from a lookup by its specific
certname - confirmed live against a real openvoxdb instance: a
collection query, even with an explicit filter naming that certname,
returns nothing for a deactivated node, while looking it up directly by
certname still returns its full record. A certname openvoxdb has no
record of at all SHALL be reported as a distinct "not found" condition,
not conflated with a general query failure.

#### Scenario: Looking up an active node
- **WHEN** an internal component looks up a certname that is a known,
  active node
- **THEN** the system returns that node's record

#### Scenario: Looking up an already-deactivated node
- **WHEN** an internal component looks up a certname that is
  deactivated or expired in openvoxdb
- **THEN** the system still returns that node's record, distinguishing
  this case from an unknown certname

#### Scenario: Looking up an unknown certname
- **WHEN** an internal component looks up a certname openvoxdb has no
  record of at all
- **THEN** the system reports a distinct "not found" condition, rather
  than an empty result indistinguishable from other errors
