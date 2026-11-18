# Spec Delta

## ADDED Requirements

### Requirement: Package versions order by build time
The system SHALL give the node-agent-client Linux packages it serves
versions that the node's package manager orders by when the package was
built, so a package from a later console build is always an upgrade over
one from an earlier build, including every package version served before
this requirement existed.

#### Scenario: Re-running the install script after a console upgrade
- **WHEN** the install script runs on a Linux node whose installed
  node-agent-client package came from an earlier console build
- **THEN** the package manager upgrades the package to the one the
  console now serves

#### Scenario: A node with a package from before versions were ordered
- **WHEN** a Linux node holding a package whose version predates this
  requirement installs from the console's repository
- **THEN** the newly served package is treated as newer and replaces it

#### Scenario: Two builds from the same source revision
- **WHEN** the console is rebuilt from the same source revision at a
  later time
- **THEN** the later build's package version is ordered after the earlier
  one's

### Requirement: Upgrade artifact for every supported platform
The system SHALL make available, for each supported operating system and
architecture, the artifact a running client needs to upgrade itself - the
package in the node's package format on Linux, the client binary on
Windows and macOS - together with that artifact's size and digest and the
digest and build time of the client it contains.

#### Scenario: A Linux client fetches its upgrade artifact
- **WHEN** a client requests the upgrade artifact for a Linux
  architecture and package format the console serves
- **THEN** the console returns that package, and it matches the size and
  digest the console holds for it

#### Scenario: A Windows or macOS client fetches its upgrade artifact
- **WHEN** a client requests the upgrade artifact for a supported Windows
  or macOS platform
- **THEN** the console returns the client binary for that platform, and
  it matches the size and digest the console holds for it

#### Scenario: Packages were not built for this console
- **WHEN** the console was built without Linux packages
- **THEN** it offers no upgrade artifact for package-installed Linux
  clients, and those nodes are not instructed to upgrade
