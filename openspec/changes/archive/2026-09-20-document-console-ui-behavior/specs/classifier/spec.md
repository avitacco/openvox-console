## ADDED Requirements

### Requirement: Group listings report matching node counts
The system SHALL report, for each group in a listing, how many nodes
currently match it, so a group's reach is visible without opening it.

Counts SHALL be resolved against the current fleet rather than stored,
since a fact-matched group's membership changes as nodes report. Where
the counts cannot be determined - the inventory being unreachable, for
instance - the listing SHALL still render, with the counts reported as
unavailable rather than as zero.

#### Scenario: Viewing groups with their node counts
- **WHEN** a user with `classifier:read` opens the group list
- **THEN** each group is shown with the number of nodes currently
  matching it

#### Scenario: A group nothing matches
- **WHEN** a group's rule matches no node in the current fleet
- **THEN** it is listed with a count of zero rather than omitted

#### Scenario: Node data unavailable
- **WHEN** the node data needed to resolve counts cannot be retrieved
- **THEN** the group list is still displayed, with counts reported as
  unavailable rather than as zero
