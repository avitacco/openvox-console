## MODIFIED Requirements

### Requirement: Install script route
The system SHALL serve an install script, appropriate to the
requesting node's operating system, that ensures openvoxagent is
installed (or upgraded to the version currently available from that
OS's normal package source) and installs this console's own node-agent
artifact for the node's platform via that platform's native
installation mechanism (a package manager on platforms where one is
used, or an equivalent self-installing mechanism providing the same
safety guarantees), registered under that OS's native service manager
and configured to connect to this console's NATS-based node transport.

#### Scenario: Fetching the install script
- **WHEN** a request is made to the install script route for a
  supported operating system
- **THEN** the system returns a script, in a format runnable on that
  OS, that installs or updates openvoxagent, installs the node-agent
  artifact via that platform's native installation mechanism, and
  registers it under that OS's native service manager (systemd on
  Linux, the Service Control Manager on Windows, launchd on macOS),
  pointed at this console's node transport address

#### Scenario: Running the script on an already-provisioned node
- **WHEN** the install script runs on a node that already has
  openvoxagent and an up-to-date node-agent installed
- **THEN** it completes without disrupting the existing installation
  (idempotent - safe to re-run)

#### Scenario: A Linux node's package manager is detected automatically
- **WHEN** the install script runs on a Linux node
- **THEN** it installs openvoxagent using that node's own package
  manager (`apt`/`apt-get` on Debian-family, `yum`/`dnf` on
  RedHat-family), without requiring the operator to specify which
  family the node belongs to

### Requirement: Platform-matched binary
The system SHALL serve a node-agent installable artifact matching the
requesting node's operating system and architecture, for every
platform this project supports: Linux (amd64, arm64), Windows
(amd64), and macOS (amd64, arm64). Windows is amd64-only because
OpenVox publishes no Windows ARM64 `openvoxagent` package for a
node-agent build to pair with. The artifact served may be a native
package or a self-installing binary, depending on the platform - what
matters is that installing it does not require the requesting node to
perform its own file-replacement handling.

#### Scenario: Requesting the binary for a supported platform
- **WHEN** a request for the node-agent artifact names a supported
  OS/architecture combination
- **THEN** the system returns an installable artifact built for that
  platform

#### Scenario: Requesting an unsupported platform
- **WHEN** a request names an OS/architecture combination this project
  does not support
- **THEN** the system responds with an error identifying the platform
  as unsupported, rather than returning an artifact that will not run

## ADDED Requirements

### Requirement: Installing or upgrading node-agent-client tolerates a running instance
The system's node-agent-client delivery mechanism SHALL support
installing or upgrading node-agent-client on a node where a previous
version is already installed and actively running as a service,
without the install script needing to manually orchestrate stopping,
replacing, and restarting that running instance to work around the
operating system's protections against overwriting an executing file.

#### Scenario: Re-running install while node-agent-client is actively running
- **WHEN** the install process runs on a node where node-agent-client
  is already installed and running as an active service
- **THEN** the installation or upgrade completes without error, and
  afterward the service is running the newly served version of
  node-agent-client

#### Scenario: A genuine install failure is reported clearly
- **WHEN** the install or upgrade fails for a reason unrelated to
  replacing a running instance (for example, insufficient disk space
  or a network failure fetching the artifact)
- **THEN** the operator sees an error message describing the actual
  failure, rather than a low-level write error that could be mistaken
  for the running-instance replacement problem
