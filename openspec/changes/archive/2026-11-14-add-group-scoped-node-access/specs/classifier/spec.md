# Spec Delta

## ADDED Requirements

### Requirement: A group granted to a role cannot be deleted
The system SHALL refuse to delete a node group that any role is granted
on, and SHALL say in the refusal that roles still use the group.

#### Scenario: Deleting a granted group
- **WHEN** a user deletes a node group that a role is granted on
- **THEN** the deletion is refused with a conflict response saying the
  group is granted to one or more roles, and the group is unchanged

#### Scenario: Deleting after the grant is removed
- **WHEN** the group is removed from every role and then deleted
- **THEN** the deletion succeeds
