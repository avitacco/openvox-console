## ADDED Requirements

### Requirement: Node group changes publish an activity event
The system SHALL publish an activity event whenever a node group is
created, updated, or deleted, since a group change can change how nodes
are classified.

#### Scenario: Creating a group publishes an event
- **WHEN** a node group is created
- **THEN** an activity event recording the creation, the group, and the
  acting user is published

#### Scenario: Updating a group publishes an event
- **WHEN** a node group is updated
- **THEN** an activity event recording the update, the group, and the
  acting user is published

#### Scenario: Deleting a group publishes an event
- **WHEN** a node group is deleted
- **THEN** an activity event recording the deletion, the group, and the
  acting user is published
