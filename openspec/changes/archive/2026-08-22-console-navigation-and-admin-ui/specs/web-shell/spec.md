## ADDED Requirements

### Requirement: Primary navigation across pages
The system SHALL provide navigation links, present on every page, letting a
user move between the console's top-level sections (node inventory, node
groups) without typing a URL, with the current page marked so the user can
tell where they are.

#### Scenario: Navigating between top-level sections
- **WHEN** a user on one top-level page (e.g. node inventory) selects
  another section's nav link (e.g. groups)
- **THEN** the system navigates to that section

#### Scenario: Current page is marked in navigation
- **WHEN** a user is on a given top-level page
- **THEN** that page's nav link is marked as the current page

### Requirement: Admin navigation is permission-gated
The system SHALL show a navigation link to the admin section only to a user
whose current access token carries the `rbac:admin` permission.

#### Scenario: Admin link shown to an authorized user
- **WHEN** a user with `rbac:admin` on their access token views any page
- **THEN** the navigation includes a link to the admin section

#### Scenario: Admin link hidden from an unauthorized user
- **WHEN** a user without `rbac:admin` on their access token views any page
- **THEN** the navigation does not include a link to the admin section
