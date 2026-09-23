## Purpose

Lets one console image start as one of several roles - serving the web UI,
answering classification requests, terminating node connections, or running
background work - so that components with different load profiles can be
scaled and placed independently instead of every instance running everything.

## ADDED Requirements

### Requirement: Run mode selection
The system SHALL accept a run mode at startup that determines which of its
subsystems it activates. The system SHALL support the modes `all`, `web`,
`enc`, `orchestrator`, and `worker`. When no run mode is configured, the
system SHALL start in `all` mode.

The system SHALL refuse to start, with a non-zero exit status and a log
message naming the unrecognized value and listing the valid modes, when
configured with a run mode it does not recognize.

#### Scenario: No run mode configured
- **WHEN** the binary starts with no run mode configured
- **THEN** it starts in `all` mode
- **AND** it activates every subsystem it would have activated before run
  modes existed

#### Scenario: A valid run mode is configured
- **WHEN** the binary starts with a recognized run mode configured
- **THEN** it activates only the subsystems that mode defines
- **AND** it logs which mode it started in

#### Scenario: An unrecognized run mode is configured
- **WHEN** the binary starts with a run mode value that is not one of the
  supported modes
- **THEN** it exits with a non-zero status and a log message naming the
  unrecognized value and the valid modes
- **AND** it does not begin accepting requests on any listener

### Requirement: Mode-determined service surface
Each run mode SHALL activate a defined set of HTTP routes, network
listeners, and background workers, and SHALL NOT activate the others:

- `all` SHALL activate every surface, equivalent to the system's behavior
  before run modes existed.
- `web` SHALL serve the console UI and the REST API, including code
  deployment endpoints and webhooks, and SHALL NOT terminate node transport
  connections or run background workers.
- `enc` SHALL serve only the ENC classification endpoint and the system's own
  operational endpoints (health and metrics), and SHALL NOT serve the console
  UI, the REST API, node transport connections, or background workers.
- `orchestrator` SHALL terminate node transport connections and dispatch
  orchestration jobs, and SHALL NOT serve the console UI or the REST API.
- `worker` SHALL run background workers, and SHALL NOT serve the console UI,
  the REST API, or node transport connections.

A request to a route the active mode does not serve SHALL be answered with a
not-found response, and SHALL NOT be answered as though the route existed but
failed.

#### Scenario: A mode serves its own routes
- **WHEN** a client requests a route belonging to the active mode's surface
- **THEN** the system serves it exactly as it would in `all` mode

#### Scenario: A mode does not serve another mode's routes
- **WHEN** a client requests the console UI from an instance running in `enc`
  mode
- **THEN** the system responds with a not-found status
- **AND** the response does not indicate an internal failure

#### Scenario: A mode does not run another mode's background work
- **WHEN** an instance runs in `web` mode
- **THEN** it does not run the scheduled background workers that `worker`
  mode runs
- **AND** those workers still run on instances in `worker` or `all` mode

### Requirement: Mode-scoped configuration requirements
The system SHALL require at startup only the configuration the active run
mode actually uses, and SHALL refuse to start when configuration that mode
requires is missing or invalid.

A mode that does not issue access tokens SHALL NOT require a token signing
key, so that an instance deployed to a less trusted location cannot mint
tokens even if its configuration is compromised.

#### Scenario: A mode starts without configuration it does not use
- **WHEN** an instance starts in `enc` mode with no token signing key
  configured
- **THEN** it starts successfully and serves the ENC endpoint
- **AND** it is able to verify presented access tokens

#### Scenario: A mode refuses to start without configuration it requires
- **WHEN** an instance starts in `orchestrator` mode with no node transport
  listener address configured
- **THEN** it exits with a non-zero status and a log message naming the
  missing configuration
- **AND** it does not start in a state where every dispatch would fail

#### Scenario: An unused credential is not loaded
- **WHEN** an instance runs in a mode that does not issue access tokens
- **THEN** the system does not read the token signing key from disk

### Requirement: Mode-scoped health and readiness reporting
The system's health endpoint SHALL report the status of only those
dependencies the active run mode actually uses, so that an instance is not
reported unhealthy for a dependency its mode never contacts.

The system SHALL report its active run mode on the health endpoint, so an
operator or load balancer can confirm what an instance is serving.

#### Scenario: Health reflects only the active mode's dependencies
- **WHEN** a client requests the health endpoint of an instance running in a
  mode that does not use the node transport
- **THEN** the response does not report node transport status as a
  dependency
- **AND** the instance is reported healthy when the dependencies its mode
  does use are healthy

#### Scenario: Health names the active mode
- **WHEN** a client requests the health endpoint of any instance
- **THEN** the response identifies which run mode that instance is running in

#### Scenario: A mode is unhealthy when its own dependency fails
- **WHEN** an instance running in `enc` mode cannot reach the datastore its
  classification decisions depend on
- **THEN** the health endpoint responds with a non-success status naming that
  dependency

### Requirement: One image for every mode
Every supported run mode SHALL be startable from the same container image and
the same binary, selected by configuration alone, with no mode-specific build
artifact, image variant, or command-line entrypoint difference.

#### Scenario: Two modes from one image
- **WHEN** an operator starts two containers from the same image, configuring
  one for `web` mode and the other for `enc` mode
- **THEN** each starts in its configured mode
- **AND** neither requires an image built specifically for that mode
