## MODIFIED Requirements

### Requirement: Primary navigation across pages
The system SHALL provide navigation links, present on every page, letting a
user move between the console's top-level sections (node inventory, nodes,
node groups) without typing a URL, with the current page marked so the user
can tell where they are.

#### Scenario: Navigating between top-level sections
- **WHEN** a user on one top-level page (e.g. node inventory) selects
  another section's nav link (e.g. groups)
- **THEN** the system navigates to that section

#### Scenario: Current page is marked in navigation
- **WHEN** a user is on a given top-level page
- **THEN** that page's nav link is marked as the current page
