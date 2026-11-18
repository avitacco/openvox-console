# Spec Delta

## ADDED Requirements

### Requirement: Reports its build identity
The system SHALL answer a build-identity request with a digest of the
client's own executable, the client's build time and version, the
operating system and architecture it runs on, how it was installed
(a Linux package format, or a directly installed binary), and whether it
will accept upgrades.

#### Scenario: The console asks for the build identity
- **WHEN** the client receives a build-identity request
- **THEN** it replies with its executable digest, build time, version,
  operating system, architecture, install method and whether upgrades
  are accepted, without changing anything on the node

#### Scenario: The client is busy when asked
- **WHEN** the client receives a build-identity request while it is
  executing a run, a task, or another request
- **THEN** it replies with its build identity at once, the request in
  progress is unaffected, and answering does not cause a request that
  arrives meanwhile to be rejected as busy

#### Scenario: A client built without a build stamp
- **WHEN** a client built without a stamped build time receives a
  build-identity request
- **THEN** it reports its executable digest with no build time, so the
  console can tell it from a stamped build

### Requirement: Performs a verified upgrade on request
The system SHALL, on receiving an upgrade request naming an artifact, its
size and its digest, install that artifact only after confirming it
matches the request and is a working client of the requested build, and
SHALL reply before restarting, saying whether the upgrade was started or
why it was not. A failed check SHALL leave the installed client
unchanged.

#### Scenario: A valid upgrade request
- **WHEN** the client receives an upgrade request and the downloaded
  artifact matches the requested size and digest
- **THEN** it replies that the upgrade has started, installs the
  artifact, and the service comes back running the requested build

#### Scenario: The artifact does not match its digest
- **WHEN** the downloaded artifact's digest or size differs from the
  request
- **THEN** the client discards it, replies with a failure naming the
  mismatch, and keeps running its existing build

#### Scenario: The artifact cannot be downloaded
- **WHEN** the client cannot download the artifact
- **THEN** it replies with a failure describing the download error and
  keeps running its existing build

#### Scenario: The artifact is not a working client
- **WHEN** the downloaded artifact matches its digest but does not run,
  or does not identify itself as the requested build
- **THEN** the client does not install it and replies with a failure

#### Scenario: Upgrading a package-installed Linux client
- **WHEN** a client installed from a Linux package upgrades
- **THEN** the node's package database records the new package version,
  and the installation completes even though it restarts the client that
  started it

#### Scenario: An install method that cannot be upgraded in place
- **WHEN** the client cannot determine how it was installed, or was
  installed by a method it cannot upgrade
- **THEN** it replies with a failure saying the install script must be
  re-run, and changes nothing

#### Scenario: Upgrade artifacts are kept out of reach of other users
- **WHEN** the client downloads an upgrade artifact
- **THEN** it is stored where only an administrator can read or write it,
  and removed once the upgrade has succeeded or failed

### Requirement: A node can decline upgrades
The system SHALL let a node's own configuration turn off acceptance of
upgrade requests, in which case the client SHALL refuse every upgrade
request and report in its build identity that upgrades are not accepted.

#### Scenario: Upgrades are declined on a node
- **WHEN** a client configured to decline upgrades receives an upgrade
  request
- **THEN** it replies with a refusal, changes nothing, and its build
  identity reports that upgrades are not accepted

## MODIFIED Requirements

### Requirement: Single in-flight request per client
The system SHALL execute at most one request at a time per client,
rejecting (rather than silently queuing or dropping) a new request that
arrives while one is already running. A build-identity request is the
exception: it changes nothing on the node, is answered whatever else is
running, and does not count as a request in progress.

#### Scenario: A second request arrives while one is running
- **WHEN** a run or task request arrives while the client is still
  executing a previous one
- **THEN** the client rejects the new request with a response
  indicating it is busy, rather than queuing it silently or dropping
  the in-progress one

#### Scenario: The rejection is immediate
- **WHEN** a request arrives while the client is executing a
  long-running task
- **THEN** the busy response is sent straight away, not when the task
  finishes, and the rejected request is never executed later
