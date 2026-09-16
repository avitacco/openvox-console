## MODIFIED Requirements

### Requirement: Node package list
The system SHALL report, for a requested node, every package openvoxdb
knows to be installed on it, including each package's name, version, and
the package manager (provider) that reported it. For a package reported
by the `rpm` provider that has an epoch, the version SHALL be in
`epoch:version-release` form. For a package reported by the `apt`
provider, the system SHALL also report the name and version of the
source package it was built from, when the node has reported that
information.

#### Scenario: Viewing a node's installed packages
- **WHEN** a request asks for the installed packages of a node openvoxdb
  has package data for
- **THEN** the system reports every known package for that node, with its
  name, version, and provider

#### Scenario: A node with no package data reports an empty list
- **WHEN** a request asks for the installed packages of a node openvoxdb
  has no package data for (including a node openvoxdb has never heard of)
- **THEN** the system reports an empty package list, rather than an error

#### Scenario: An RPM package with an epoch reports it in its version
- **WHEN** a node running the current package-inventory fact has an RPM
  package installed whose epoch is set (for example `openssl` with epoch
  `1`, version `3.0.7`, release `24.el9`)
- **THEN** that package's reported version is `1:3.0.7-24.el9`

#### Scenario: An RPM package without an epoch is unchanged
- **WHEN** a node running the current package-inventory fact has an RPM
  package installed with no epoch
- **THEN** that package's reported version is `version-release`, exactly
  as before this change

#### Scenario: An apt package built from a differently-named source package
- **WHEN** a node running the current package-inventory fact has an apt
  package installed whose source package differs from it in name or
  version (for example binary `libssl3` built from source `openssl`)
- **THEN** that package's entry reports its own binary name and version,
  plus the source package's name and version

#### Scenario: An apt package whose source matches it
- **WHEN** an apt package's source package has the same name and version
  as the binary package itself
- **THEN** the entry reports that same name and version as its source
  package

#### Scenario: A node still running an older package-inventory fact
- **WHEN** a node's package data was produced by a package-inventory fact
  that predates source-package reporting
- **THEN** its apt packages are reported without source package
  information, rather than with a guessed source package
