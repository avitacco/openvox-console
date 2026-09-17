## ADDED Requirements

### Requirement: Only installed packages are reported
The system SHALL report, for a node, only packages that are actually
installed on it. A package that the node's package manager still records
but that is not installed - notably an apt package that has been removed
without being purged, whose configuration files are retained - SHALL NOT be
reported. An entry that does not represent installed software at all -
notably the repository signing keys that the `rpm` provider lists alongside
real packages once they have been imported - SHALL NOT be reported. A
package that is installed SHALL be reported regardless of the
administrator's selection state for it, so that a held or pinned package is
still reported and still assessed for vulnerabilities.

#### Scenario: An apt package removed but not purged is not reported
- **WHEN** a node running the current package-inventory fact has an apt
  package that was removed without being purged, so its package manager
  still records it with its configuration files retained
- **THEN** that package does not appear in the node's reported package
  inventory, and no vulnerability finding is raised against it

#### Scenario: An installed apt package is still reported
- **WHEN** a node running the current package-inventory fact has an apt
  package that is installed
- **THEN** that package appears in the node's reported package inventory
  with its name, version, and source package information, exactly as
  before this change

#### Scenario: A held apt package is still reported
- **WHEN** a node running the current package-inventory fact has an apt
  package that is installed but held, so that the administrator's
  selection state for it is not "install"
- **THEN** that package appears in the node's reported package inventory
  and is assessed for vulnerabilities like any other installed package

#### Scenario: RPM repository signing keys are not reported as packages
- **WHEN** a node running the current package-inventory fact uses the `rpm`
  provider and has imported one or more repository signing keys, which its
  package manager lists alongside real packages
- **THEN** those signing-key entries do not appear in the node's reported
  package inventory

#### Scenario: An erased rpm package is not reported
- **WHEN** a node running the current package-inventory fact has an rpm
  package that has been erased
- **THEN** that package does not appear in the node's reported package
  inventory

#### Scenario: A node still running an older package-inventory fact
- **WHEN** a node's package data was produced by a package-inventory fact
  that predates installed-state filtering
- **THEN** its packages are reported as that fact produced them, rather
  than being filtered after the fact by the console

#### Scenario: Corrected data replaces stale data on the node's next report
- **WHEN** a node that previously reported packages which were not
  installed begins reporting inventory produced by the current
  package-inventory fact
- **THEN** those packages no longer appear in the node's reported package
  inventory, and vulnerability findings that existed only because of them
  are closed
