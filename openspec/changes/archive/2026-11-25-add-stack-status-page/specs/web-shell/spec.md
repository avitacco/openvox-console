## MODIFIED Requirements

### Requirement: Admin navigation is permission-gated
The system SHALL show a navigation link to a permission-gated section
only to a user whose current access token carries that section's
permission. This applies to the admin section, gated on `rbac:admin`, and
to the stack status section, gated on `status:read`.

A section's link SHALL be hidden rather than shown-and-refused, so the
navigation reflects what the user can actually reach.

#### Scenario: Admin link shown to an authorized user
- **WHEN** a user with `rbac:admin` on their access token views any page
- **THEN** the navigation includes a link to the admin section

#### Scenario: Admin link hidden from an unauthorized user
- **WHEN** a user without `rbac:admin` on their access token views any page
- **THEN** the navigation does not include a link to the admin section

#### Scenario: Status link shown to an authorized user
- **WHEN** a user with `status:read` on their access token views any page
- **THEN** the navigation includes a link to the stack status section

#### Scenario: Status link hidden from an unauthorized user
- **WHEN** a user without `status:read` on their access token views any
  page
- **THEN** the navigation does not include a link to the stack status
  section
