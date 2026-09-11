## Purpose

Manages the console's connection to Postgres and the migration tooling that
keeps the shared schema - used by every later capability (classifier, RBAC,
activity, code manager, orchestrator) - up to date.

## ADDED Requirements

### Requirement: Postgres connection management
The system SHALL establish a connection (pool) to Postgres using configured
credentials at startup, and SHALL make that connection available to all
internal components that need it.

#### Scenario: Successful connection at startup
- **WHEN** the binary starts with valid Postgres connection configuration
- **THEN** it establishes a connection pool before reporting itself healthy

#### Scenario: Postgres unreachable at startup
- **WHEN** the binary starts and cannot reach the configured Postgres
  instance
- **THEN** it logs the failure and the health check endpoint reports
  Postgres as unhealthy, without crashing the process

### Requirement: Schema migration tooling
The system SHALL provide a mechanism to apply pending schema migrations
against a single shared Postgres schema, and SHALL track which migrations
have already been applied so migrations are not re-applied.

#### Scenario: Applying pending migrations
- **WHEN** migration tooling runs against a database with unapplied
  migrations
- **THEN** it applies each pending migration in order and records it as
  applied

#### Scenario: No pending migrations
- **WHEN** migration tooling runs against a database that is already at the
  latest schema version
- **THEN** it makes no changes and reports the schema as up to date
