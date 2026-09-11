## Purpose

Provides the console's only path for querying openvoxdb - executing PQL
queries over its HTTP API and returning parsed results - so every other
capability that needs node, fact, report, or event data goes through one
consistent, authenticated client rather than each building its own.

## ADDED Requirements

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
