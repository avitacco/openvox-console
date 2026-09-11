## Purpose

Lets console users inspect the history of catalog runs (reports) for a node
and drill into the resource-level events within a specific run, to
diagnose configuration drift or failures.

## ADDED Requirements

### Requirement: Report history view
The system SHALL display the catalog run history for a given node, ordered
by most recent run first.

#### Scenario: Viewing a node's report history
- **WHEN** a user opens the report history for a node with prior catalog
  runs
- **THEN** the system displays each run with its timestamp and overall
  status (success, failure, or noop), sourced from openvoxdb

#### Scenario: No reports yet for a node
- **WHEN** a user opens the report history for a node with no catalog runs
  recorded
- **THEN** the system displays an empty state rather than an error

### Requirement: Resource-level event drill-down
The system SHALL display the resource-level events for a selected report.

#### Scenario: Viewing events within a report
- **WHEN** a user selects a specific report from a node's history
- **THEN** the system displays every resource event in that report,
  including the resource, the event status, and any change made

### Requirement: Report and event search and filtering
The system SHALL let a user filter report history by run status, and
filter resource events within a report by event status.

#### Scenario: Filtering report history by status
- **WHEN** a user filters a node's report history to failed runs only
- **THEN** the displayed report list narrows to runs with a failure status

#### Scenario: Filtering events by status
- **WHEN** a user filters a report's events to failed events only
- **THEN** the displayed event list narrows to events with a failure
  status
