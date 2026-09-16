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
