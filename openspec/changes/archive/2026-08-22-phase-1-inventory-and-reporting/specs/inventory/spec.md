## Purpose

Lets console users browse the inventory of OpenVox-managed nodes - their
facts, current status, and how recently they last checked in - and find
specific nodes through search and filtering.

## ADDED Requirements

### Requirement: Node list view
The system SHALL display a list of nodes known to openvoxdb, showing each
node's name, status, and last check-in time.

#### Scenario: Viewing the node list
- **WHEN** a user opens the node inventory view
- **THEN** the system displays every known node with its status and last
  check-in time, sourced from openvoxdb

#### Scenario: No nodes known yet
- **WHEN** a user opens the node inventory view and openvoxdb reports no
  nodes
- **THEN** the system displays an empty state rather than an error

### Requirement: Node detail view
The system SHALL display a single node's full fact set on request.

#### Scenario: Viewing node facts
- **WHEN** a user selects a node from the inventory
- **THEN** the system displays that node's facts as reported by openvoxdb

### Requirement: Node search and filtering
The system SHALL let a user filter the node list by node name and by fact
value.

#### Scenario: Filtering by name
- **WHEN** a user enters a search term matching part of a node's name
- **THEN** the displayed node list narrows to nodes whose name matches the
  term

#### Scenario: Filtering by fact value
- **WHEN** a user filters by a specific fact name and value
- **THEN** the displayed node list narrows to nodes reporting that fact
  value
