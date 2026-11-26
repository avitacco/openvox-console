# service-runtime Specification

## Purpose

Defines the startup and operational behavior of the console binary itself -
how it loads configuration, logs, and reports its own health - independent
of any feature built on top of it.

## Requirements

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

### Requirement: Structured logging
The system SHALL emit structured (machine-parseable) log output for
startup, shutdown, and request handling.

#### Scenario: Startup logging
- **WHEN** the binary starts successfully
- **THEN** it emits a structured log entry indicating it is ready to serve
  requests

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

### Requirement: Metrics endpoint
The system SHALL expose an HTTP endpoint reporting operational metrics in a Prometheus-compatible format, covering at minimum HTTP request counts/latencies and the health-check dependencies (Postgres, embedded NATS).

#### Scenario: Scraping metrics
- **WHEN** a client requests the metrics endpoint
- **THEN** the system responds with metrics in a Prometheus-compatible text format

### Requirement: Operational visibility into broker and orchestrator state
The system's metrics endpoint SHALL report the number of agents currently connected to this instance's node transport, the number of orchestrator jobs this instance currently has in-flight dispatches for, and the number of currently-tracked token revocations - so an operator has a documented way to observe this state beyond logs or direct database queries.

#### Scenario: Viewing broker/orchestrator counts
- **WHEN** a client requests the metrics endpoint
- **THEN** the reported metrics include the current connected-agent count, in-flight dispatch count, and revocation-list size for this instance

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

### Requirement: Postgres connection resilience
The system SHALL automatically recover its Postgres connection pool after a transient interruption (for example, a replica promotion during a failover), without requiring the process to be restarted, consistent with the system's existing posture of never treating a recoverable dependency outage as permanently fatal.

#### Scenario: Postgres becomes briefly unreachable and recovers
- **WHEN** the configured Postgres connection is interrupted and then becomes reachable again
- **THEN** the system resumes serving requests that depend on Postgres without being restarted

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
