## MODIFIED Requirements

### Requirement: Configuration loading
The system SHALL load its configuration from a configuration source (file
and/or environment variables) at startup, and SHALL fail to start with a
clear error message if required configuration is missing or invalid.

Which configuration values are required SHALL depend on the active run mode:
the system SHALL require the configuration that mode uses, and SHALL NOT
require configuration used only by subsystems that mode does not activate.

#### Scenario: Valid configuration
- **WHEN** the binary starts with a complete, valid configuration
- **THEN** it proceeds to initialize its dependencies (Postgres, NATS, HTTP
  server)

#### Scenario: Missing required configuration
- **WHEN** the binary starts without a required configuration value (for
  example, a Postgres connection string)
- **THEN** it exits with a non-zero status and a log message naming the
  missing value, without starting the HTTP server

#### Scenario: Configuration required only by an inactive subsystem
- **WHEN** the binary starts in a run mode that does not activate a given
  subsystem, without the configuration only that subsystem uses
- **THEN** it starts successfully
- **AND** it does not log that configuration as missing

### Requirement: Health check endpoint
The system SHALL expose an HTTP health check endpoint that reports the
status of the dependencies the active run mode uses, which SHALL include its
Postgres connection and its embedded NATS server in any mode that uses them.

The endpoint SHALL be served in every run mode, so that an instance of any
mode can be health-checked by a load balancer or orchestration platform
without knowing which mode it is running.

#### Scenario: All dependencies healthy
- **WHEN** a client requests the health check endpoint and both Postgres
  and the embedded NATS server are reachable
- **THEN** the endpoint responds with a success status and indicates both
  dependencies are healthy

#### Scenario: A dependency is unavailable
- **WHEN** a client requests the health check endpoint and Postgres is
  unreachable
- **THEN** the endpoint responds with a non-success status and indicates
  which dependency is unhealthy

#### Scenario: Health is available in every mode
- **WHEN** a client requests the health check endpoint of an instance in any
  supported run mode
- **THEN** the endpoint responds
- **AND** the response reflects that mode's own dependencies

### Requirement: Documented statelessness boundary
The system SHALL document, in operator-facing form, exactly which in-memory state is instance-local and does not survive a request being routed to a different running instance, and which state is shared (via Postgres and/or NATS) and does survive.

The documentation SHALL state which run modes may be run as multiple
concurrent instances and which may not, and SHALL describe the supported
multi-instance topology, including how instances are peered and which modes
must be reachable by which others.

#### Scenario: An operator can determine what a failover preserves
- **WHEN** an operator consults the documented statelessness boundary
- **THEN** it states, for each category of in-memory state in the system, whether a request for that state can be safely routed to any running instance or only to the instance that created it

#### Scenario: An operator can determine which modes scale horizontally
- **WHEN** an operator consults the documented statelessness boundary
- **THEN** it states, for each run mode, whether multiple concurrent
  instances of that mode are supported
- **AND** it describes how instances of that mode are peered

## ADDED Requirements

### Requirement: Background work runs once across the cluster
Scheduled background work that must not run concurrently SHALL be
coordinated so that exactly one instance performs a given unit of work at a
time, however many instances are running it.

When the instance performing a unit of scheduled work stops or becomes
unreachable, another instance SHALL become eligible to perform it without
operator intervention.

#### Scenario: Two instances do not duplicate scheduled work
- **WHEN** two instances that run scheduled background work are running
  concurrently
- **THEN** a given unit of that work is performed by exactly one of them at
  a time

#### Scenario: Work continues after an instance stops
- **WHEN** the instance performing a unit of scheduled work stops
- **THEN** another running instance becomes eligible to perform it
- **AND** no operator intervention is required
