## Purpose

Lets console users see what software packages are installed on a managed
node, and find which nodes across the fleet have a given package (and
optionally version) installed, sourced from openvoxdb's package inventory
data rather than requiring direct access to each node.

## ADDED Requirements

### Requirement: Node package list
The system SHALL report, for a requested node, every package openvoxdb
knows to be installed on it, including each package's name, version, and
the package manager (provider) that reported it.

#### Scenario: Viewing a node's installed packages
- **WHEN** a request asks for the installed packages of a node openvoxdb
  has package data for
- **THEN** the system reports every known package for that node, with its
  name, version, and provider

#### Scenario: A node with no package data reports an empty list
- **WHEN** a request asks for the installed packages of a node openvoxdb
  has no package data for (including a node openvoxdb has never heard of)
- **THEN** the system reports an empty package list, rather than an error

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
carrying the `nodes:read` permission on the node package list and
fleet-wide package search endpoints, matching the existing node inventory
endpoints' permission gate.

#### Scenario: Unauthenticated request rejected
- **WHEN** a request to the node package list or package search endpoint
  has no valid access token
- **THEN** the system rejects the request

#### Scenario: Authenticated request with the required permission succeeds
- **WHEN** a request to the node package list or package search endpoint
  presents a valid access token carrying `nodes:read`
- **THEN** the system processes the request as before
