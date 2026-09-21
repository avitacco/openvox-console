# package-inventory Specification

## Purpose

Lets console users see what software packages are installed on a managed
node, and find which nodes across the fleet have a given package (and
optionally version) installed, sourced from openvoxdb's package inventory
data rather than requiring direct access to each node.

## Requirements

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

### Requirement: Fleet-wide package search
The system SHALL let a user search for every node that has a specified
package installed, optionally narrowed to a specific version, returning
each matching node's name, the matching package's version, and provider.

#### Scenario: Searching by package name
- **WHEN** a user searches for a package name with no version specified
- **THEN** the system returns every node reporting a package with that
  name, at any version

#### Scenario: Searching by package name and version
- **WHEN** a user searches for a package name with a specific version
- **THEN** the system returns only nodes reporting that package at
  exactly that version

#### Scenario: No nodes match a search
- **WHEN** a user searches for a package (optionally with a version) that
  no known node has installed
- **THEN** the system returns an empty result, rather than an error

### Requirement: Endpoints require authentication
The system SHALL require a valid, unexpired, unrevoked access token
carrying the `nodes:read` permission on the node package list,
fleet-wide package search, and per-node reporting-toggle status
endpoints, matching the existing node inventory endpoints' permission
gate. The system SHALL require the `orchestrator:run` permission on the
per-node reporting-toggle set endpoint, matching the permission that
gates other node-affecting dispatched actions.

#### Scenario: Unauthenticated request rejected
- **WHEN** a request to the node package list, package search,
  reporting-toggle status, or reporting-toggle set endpoint has no
  valid access token
- **THEN** the system rejects the request

#### Scenario: Authenticated request with the required permission succeeds
- **WHEN** a request to the node package list, package search, or
  reporting-toggle status endpoint presents a valid access token
  carrying `nodes:read`, or a request to the reporting-toggle set
  endpoint presents one carrying `orchestrator:run`
- **THEN** the system processes the request as before

#### Scenario: Read permission is not sufficient to change the toggle
- **WHEN** a request to the reporting-toggle set endpoint presents a
  valid access token carrying `nodes:read` but not `orchestrator:run`
- **THEN** the system rejects the request

### Requirement: Per-node reporting toggle
The system SHALL let an authorized user read whether a node currently
reports package inventory, and enable or disable it, by dispatching to
that node over the node transport rather than maintaining its own
separate record of the setting - the node's own reported state is
authoritative.

#### Scenario: Reading a node's current reporting state
- **WHEN** a user requests the reporting-toggle status for a node
- **THEN** the system dispatches a status request to that node and
  returns the state it reports

#### Scenario: Enabling reporting for a node
- **WHEN** a user requests that a node's package inventory reporting be
  enabled
- **THEN** the system dispatches an enable request to that node and
  returns the resulting state

#### Scenario: Disabling reporting for a node
- **WHEN** a user requests that a node's package inventory reporting be
  disabled
- **THEN** the system dispatches a disable request to that node and
  returns the resulting state

### Requirement: Toggling requires the node to be currently connected
The system SHALL reject a reporting-toggle read or set request for a
node that is not currently connected to the node transport, identifying
the node as offline, rather than queuing the request for a future
connection.

#### Scenario: Toggling a disconnected node
- **WHEN** a reporting-toggle read or set request targets a node that
  is not currently connected
- **THEN** the system rejects the request, identifying the node as
  offline, and does not queue it for later delivery

### Requirement: Toggling is audited
The system SHALL emit an audit event for an enable or disable
reporting-toggle request under the existing `nodes/inventory` audit
category, subject to that category's configured audit level, matching
how other mutating actions in that category are audited.

#### Scenario: Enabling or disabling reporting is audited
- **WHEN** a user's request to enable or disable a node's package
  inventory reporting is processed, and the `nodes/inventory` category's
  audit level is `writes` or `full`
- **THEN** an audit event records the action, the acting identity, and
  the affected node

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

### Requirement: Package catalogue browsing
The system SHALL let a user browse the packages installed across the
fleet as a paginated catalogue, without first knowing a package's name.

The catalogue SHALL be narrowable by package name, by version, by
provider, and by node group, in any combination. Filtering by node group
SHALL require the permission needed to read groups: the catalogue is
otherwise readable by anyone who may read nodes, and group filtering
would otherwise disclose which nodes belong to a group to someone not
permitted to know.

#### Scenario: Browsing without a search term
- **WHEN** a user with `nodes:read` opens the package catalogue
- **THEN** the system returns a page of packages installed across the
  fleet, rather than requiring a package name first

#### Scenario: Narrowing the catalogue
- **WHEN** a user applies any combination of name, version, provider and
  group filters
- **THEN** only packages matching every applied filter are returned

#### Scenario: Paging through the catalogue
- **WHEN** more packages match than fit on one page
- **THEN** the results are paginated, reporting the total number of
  matches

#### Scenario: Group filtering without permission to read groups
- **WHEN** a catalogue request filters by node group but does not carry
  the permission to read groups
- **THEN** the system rejects the request rather than returning results

#### Scenario: Nothing matches the filters
- **WHEN** no package matches the applied filters
- **THEN** the system returns an empty result rather than an error
