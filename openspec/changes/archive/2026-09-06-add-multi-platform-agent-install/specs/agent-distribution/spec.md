## MODIFIED Requirements

### Requirement: Install script route
The system SHALL serve an install script, appropriate to the
requesting node's operating system, that ensures openvoxagent is
installed (or upgraded to the version currently available from that
OS's normal package source) and installs this console's own node-agent
build for the node's platform, registered under that OS's native
service manager and configured to connect to this console's NATS-based
node transport.

#### Scenario: Fetching the install script
- **WHEN** a request is made to the install script route for a
  supported operating system
- **THEN** the system returns a script, in a format runnable on that
  OS, that installs or updates openvoxagent, installs the node-agent
  binary, and registers it under that OS's native service manager
  (systemd on Linux, the Service Control Manager on Windows, launchd on
  macOS), pointed at this console's node transport address

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
The system SHALL serve a node-agent binary matching the requesting
node's operating system and architecture, for every platform this
project supports: Linux (amd64, arm64), Windows (amd64), and macOS
(amd64, arm64). Windows is amd64-only because OpenVox publishes no
Windows ARM64 `openvoxagent` package for a node-agent build to pair
with.

#### Scenario: Requesting the binary for a supported platform
- **WHEN** a request for the node-agent binary names a supported
  OS/architecture combination
- **THEN** the system returns a binary built for that platform

#### Scenario: Requesting an unsupported platform
- **WHEN** a request names an OS/architecture combination this project
  does not support
- **THEN** the system responds with an error identifying the platform
  as unsupported, rather than returning a binary that will not run

## ADDED Requirements

### Requirement: Native service supervision on every supported OS
The system SHALL register the installed node-agent binary under each
supported operating system's native service manager, so it restarts on
crash and starts automatically on boot, matching the reliability
expectations of a system service on that platform rather than a
manually-started foreground process.

#### Scenario: Linux node-agent restarts after a crash
- **WHEN** the installed node-agent-client process on a Linux node
  exits unexpectedly
- **THEN** systemd restarts it without operator intervention

#### Scenario: Windows node-agent restarts after a crash
- **WHEN** the installed node-agent-client process on a Windows node
  exits unexpectedly
- **THEN** the Service Control Manager restarts it without operator
  intervention

#### Scenario: macOS node-agent restarts after a crash
- **WHEN** the installed node-agent-client process on a macOS node
  exits unexpectedly
- **THEN** launchd restarts it without operator intervention

#### Scenario: A node-agent starts automatically after a reboot
- **WHEN** a node with node-agent-client installed and registered as a
  service reboots
- **THEN** node-agent-client is running again after startup completes,
  without an operator manually starting it
