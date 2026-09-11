## Purpose

Deploys Puppet code from a control repo (Puppetfile plus manifests) into
the live environment directory `openvoxserver` reads from, on a webhook
push or a manual trigger, and tracks the result - so every other
capability's classification and catalog compilation work against code
the console itself manages, not code that was hand-placed on disk.

## ADDED Requirements

### Requirement: Control-repo deploy execution
The system SHALL, on a deploy trigger, fetch the configured control
repo's current state and run a code deployment (module resolution per
its Puppetfile) into the live environment directory, replacing whatever
was there for that environment.

#### Scenario: A deploy resolves the control repo's Puppetfile
- **WHEN** a deploy runs against a control repo containing a Puppetfile
- **THEN** the modules that Puppetfile declares are present in the
  deployed environment directory afterward

#### Scenario: A deploy failure leaves the previous environment intact
- **WHEN** a deploy fails partway (e.g. an unreachable module source)
- **THEN** the live environment directory still serves the last
  successfully deployed code, not a partial deploy

### Requirement: Atomic deploy activation
The system SHALL make a successful deploy's code live as a single atomic
operation, such that no request being served concurrently can observe a
partially-deployed environment.

#### Scenario: A concurrent request during deploy sees one consistent version
- **WHEN** a deploy completes while requests against the live
  environment are in flight
- **THEN** each request is served entirely from either the pre-deploy or
  the post-deploy code, never a mix of both

### Requirement: Deploy triggers
The system SHALL support triggering a deploy two ways: a webhook
endpoint accepting a signed push notification from the control repo's
git host, and a manual trigger through the console UI/API.

#### Scenario: A webhook push triggers a deploy
- **WHEN** the control repo's git host sends a validly signed webhook
  notification for a push
- **THEN** the system runs a deploy

#### Scenario: An invalidly signed webhook is rejected
- **WHEN** a webhook request's signature does not match the configured
  secret
- **THEN** the system rejects the request and does not run a deploy

#### Scenario: A manual trigger runs a deploy
- **WHEN** an administrator triggers a deploy through the console
- **THEN** the system runs a deploy the same way a webhook-triggered one
  would

### Requirement: Deploy status and history
The system SHALL record every deploy attempt (trigger source, control
repo ref, status, start/finish time, and error detail on failure) and
expose it via an API and UI, restricted to users with `code:read`.
Triggering a deploy SHALL require `code:deploy`.

#### Scenario: Viewing deploy history
- **WHEN** a user with `code:read` opens the deploy history page
- **THEN** the system displays every recorded deploy attempt, most
  recent first, with its status

#### Scenario: Triggering a deploy requires the write permission
- **WHEN** a request to manually trigger a deploy presents a token
  carrying `code:read` but not `code:deploy`
- **THEN** the system rejects the request

### Requirement: Deployment-ready event
The system SHALL publish an event on successful deploy completion,
naming the deployed control repo ref, for other components to subscribe
to - the contract other in-cluster consumers (e.g. a future compiler
distribution mechanism) rely on, independent of how many subscribers
exist at any given time.

#### Scenario: A successful deploy publishes an event
- **WHEN** a deploy completes successfully
- **THEN** an event naming the deployed ref is published, observable by
  any subscriber without the console needing to know who's listening

#### Scenario: A failed deploy does not publish a deployment-ready event
- **WHEN** a deploy fails
- **THEN** no deployment-ready event is published for that attempt
