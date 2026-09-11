# agent-distribution Specification

## Purpose

Gets openvoxagent and the console's own node-agent-client onto a node
via a single fetched install script, since neither OpenVox's packaging
nor any external release channel bundles a working orchestration
client - the console itself is the only place a current
node-agent-client build exists.

## Requirements

### Requirement: Install script route
The system SHALL serve an install script, appropriate to the
requesting node's operating system, that ensures openvoxagent is
installed (or upgraded to the version currently available from that
OS's normal package source), enrolls the node with the Puppet
certificate authority when it does not already hold a signed
certificate, installs this console's own node-agent artifact for the
node's platform via that platform's native installation mechanism (a
package manager on platforms where one is used, or an equivalent
self-installing mechanism providing the same safety guarantees),
registered under that OS's native service manager and configured to
connect to this console's NATS-based node transport, and sets up
package-inventory reporting for that node's platform so its installed
packages are included in its next Puppet run.

#### Scenario: Fetching the install script
- **WHEN** a request is made to the install script route for a
  supported operating system
- **THEN** the system returns a script, in a format runnable on that
  OS, that installs or updates openvoxagent, installs the node-agent
  artifact via that platform's native installation mechanism, registers
  it under that OS's native service manager (systemd on Linux, the
  Service Control Manager on Windows, launchd on macOS), pointed at
  this console's node transport address, and sets up package-inventory
  reporting for that platform

#### Scenario: Running the script on an already-provisioned node
- **WHEN** the install script runs on a node that already has
  openvoxagent and an up-to-date node-agent installed
- **THEN** it completes without disrupting the existing installation
  (idempotent - safe to re-run)

#### Scenario: A node reports its installed packages on its next run
- **WHEN** a node that has had the install script run on it next
  executes a Puppet run
- **THEN** the packages it reports are visible via openvoxdb's package
  inventory data for that node, without any separate opt-in step beyond
  having run the install script

#### Scenario: A Linux node's package manager is detected automatically
- **WHEN** the install script runs on a Linux node
- **THEN** it installs openvoxagent using that node's own package
  manager (`apt`/`apt-get` on Debian-family, `yum`/`dnf` on
  RedHat-family), without requiring the operator to specify which
  family the node belongs to

#### Scenario: Enrolling a node that has no certificate yet
- **WHEN** the install script runs on a node with no signed Puppet
  certificate
- **THEN** it submits a certificate signing request for that node and
  waits a bounded time for the request to be signed, rather than
  stopping and directing the operator to enroll the node themselves

#### Scenario: The request is signed while the script waits
- **WHEN** a node's certificate signing request is signed while the
  install script is waiting for it
- **THEN** the script continues and completes the rest of the
  installation in the same run, with no second invocation required

#### Scenario: The request is still unsigned when the wait expires
- **WHEN** a node's certificate signing request has not been signed by
  the time the script's wait expires
- **THEN** the script stops with a message identifying the node's
  pending request and directing the operator to this console, where
  certificates are signed, rather than reporting a generic failure

#### Scenario: A node that already holds a certificate is not re-enrolled
- **WHEN** the install script runs on a node that already has a signed
  Puppet certificate
- **THEN** it performs no enrollment and does not wait, leaving the
  node's existing certificate untouched

#### Scenario: Enrollment fails for a reason other than waiting to be signed
- **WHEN** enrollment fails because the certificate authority cannot be
  reached, cannot be resolved, or rejects the connection
- **THEN** the script reports that failure, distinguishably from a
  request that is merely waiting to be signed

### Requirement: Node-facing Puppet server address
The system SHALL let an operator configure the Puppet server address
that nodes themselves should use, separately from any address the
console uses for its own connections to that server, and SHALL use the
node-facing address in the install script it generates. When no
node-facing address is configured, the generated script SHALL leave a
node's existing Puppet server configuration as it is rather than
substituting a guessed address.

#### Scenario: Install script points a node at the configured server
- **WHEN** a node-facing Puppet server address is configured and a node
  runs the install script
- **THEN** the node is configured to use that address when enrolling
  and for subsequent Puppet runs

#### Scenario: No node-facing address configured
- **WHEN** no node-facing Puppet server address is configured and a
  node runs the install script
- **THEN** the script does not change the node's existing Puppet server
  configuration, so a node already pointed at a server by other means
  still enrolls against it

### Requirement: Platform-matched binary
The system SHALL serve a node-agent installable artifact matching the
requesting node's operating system and architecture, for every
platform this project supports: Linux (amd64, arm64), Windows (amd64),
and macOS (amd64, arm64). Windows is amd64-only because OpenVox
publishes no Windows ARM64 `openvoxagent` package for a node-agent
build to pair with. The artifact served may be a native package or a
self-installing binary, depending on the platform - what matters is
that installing it does not require the requesting node to perform its
own file-replacement handling.

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

### Requirement: Binary reflects the running console build
The system SHALL always serve a node-agent binary built from the same
source revision as the console binary currently serving it - there is
no separately-versioned release artifact that can drift out of sync
with the node transport it is meant to talk to.

#### Scenario: A newly-deployed console serves a matching agent build
- **WHEN** the console is deployed at a given version
- **THEN** the node-agent binaries it serves are the ones built
  alongside that same version, not a cached build from a different
  release
