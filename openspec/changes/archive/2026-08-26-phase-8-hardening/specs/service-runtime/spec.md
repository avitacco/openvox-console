## ADDED Requirements

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

#### Scenario: An operator can determine what a failover preserves
- **WHEN** an operator consults the documented statelessness boundary
- **THEN** it states, for each category of in-memory state in the system, whether a request for that state can be safely routed to any running instance or only to the instance that created it

### Requirement: Postgres connection resilience
The system SHALL automatically recover its Postgres connection pool after a transient interruption (for example, a replica promotion during a failover), without requiring the process to be restarted, consistent with the system's existing posture of never treating a recoverable dependency outage as permanently fatal.

#### Scenario: Postgres becomes briefly unreachable and recovers
- **WHEN** the configured Postgres connection is interrupted and then becomes reachable again
- **THEN** the system resumes serving requests that depend on Postgres without being restarted
