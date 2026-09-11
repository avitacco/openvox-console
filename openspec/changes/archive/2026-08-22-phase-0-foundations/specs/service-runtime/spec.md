## Purpose

Defines the startup and operational behavior of the console binary itself -
how it loads configuration, logs, and reports its own health - independent
of any feature built on top of it.

## ADDED Requirements

### Requirement: Configuration loading
The system SHALL load its configuration from a configuration source (file
and/or environment variables) at startup, and SHALL fail to start with a
clear error message if required configuration is missing or invalid.

#### Scenario: Valid configuration
- **WHEN** the binary starts with a complete, valid configuration
- **THEN** it proceeds to initialize its dependencies (Postgres, NATS, HTTP
  server)

#### Scenario: Missing required configuration
- **WHEN** the binary starts without a required configuration value (for
  example, a Postgres connection string)
- **THEN** it exits with a non-zero status and a log message naming the
  missing value, without starting the HTTP server

### Requirement: Structured logging
The system SHALL emit structured (machine-parseable) log output for
startup, shutdown, and request handling.

#### Scenario: Startup logging
- **WHEN** the binary starts successfully
- **THEN** it emits a structured log entry indicating it is ready to serve
  requests

### Requirement: Health check endpoint
The system SHALL expose an HTTP health check endpoint that reports the
status of its Postgres connection and its embedded NATS server.

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
