## ADDED Requirements

### Requirement: Deployment artifacts provide the code deployment toolchain
A deployment artifact of the console that is not installed through a
package manager - a container image, for instance - SHALL provide
everything a code deploy needs to run, including the code deployment
tool itself and the version control tooling and transports that tool
invokes.

Such an artifact SHALL identify the provided tool to the console by
default, so that configuring a control repo is the only step an operator
must take. Providing the toolchain SHALL NOT by itself enable code
deployment: it remains inactive until a control repo is configured.

The provided tool SHALL report its own version, so an operator can
establish what is running when a deploy misbehaves.

#### Scenario: Deploying from a container image
- **WHEN** the console runs from a published image with a control repo
  configured
- **THEN** a deploy succeeds without the operator installing anything
  into the image

#### Scenario: Version control transports are present
- **WHEN** a configured control repo is reached over HTTPS or SSH
- **THEN** the image provides the tooling and trust material those
  transports need

#### Scenario: The toolchain alone does not enable deployment
- **WHEN** the console runs from that image with no control repo
  configured
- **THEN** code deployment reports itself as not configured, and the
  console starts normally

#### Scenario: Identifying the bundled tool
- **WHEN** an operator asks the bundled deployment tool for its version
- **THEN** it reports a version identifying the build, rather than an
  empty value
