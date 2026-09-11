# web-shell Specification

## Purpose

Serves the console's web frontend directly from the single binary, using
assets embedded at build time, so no separate web server or asset
deployment step is needed.

## Requirements

### Requirement: Embedded frontend assets
The system SHALL embed its frontend static assets into the compiled binary
at build time, such that no external asset files are required at runtime.

#### Scenario: Serving from a binary with no external files
- **WHEN** the binary runs in an environment with no frontend source or
  build files present on disk
- **THEN** it still serves its frontend assets successfully

### Requirement: Placeholder UI shell
The system SHALL serve a minimal UI shell at its root HTTP path, confirming
the frontend asset pipeline is wired end to end even before feature UI
exists.

#### Scenario: Loading the root page
- **WHEN** a client requests the console's root path
- **THEN** the system responds with the embedded UI shell content

### Requirement: Voxblocks component library
The frontend SHALL be built on the `voxblocks` component library (the
OpenVox project's shared web-component design system) for its UI elements,
rather than a separate or ad hoc component set, so the console's look and
interaction patterns stay consistent with other OpenVox web properties.

#### Scenario: Placeholder shell uses voxblocks components
- **WHEN** the placeholder UI shell renders
- **THEN** it is composed of voxblocks components rather than bespoke
  markup/styling for elements voxblocks already provides

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

### Requirement: Admin navigation is permission-gated
The system SHALL show a navigation link to the admin section only to a user
whose current access token carries the `rbac:admin` permission.

#### Scenario: Admin link shown to an authorized user
- **WHEN** a user with `rbac:admin` on their access token views any page
- **THEN** the navigation includes a link to the admin section

#### Scenario: Admin link hidden from an unauthorized user
- **WHEN** a user without `rbac:admin` on their access token views any page
- **THEN** the navigation does not include a link to the admin section

### Requirement: Confirmation dialogs use modal components
The system SHALL confirm a consequential or destructive action using
the `voxblocks` modal dialog component, with explicit confirm and
cancel actions, rather than the browser's native `confirm()` popup.

#### Scenario: Confirming a destructive action
- **WHEN** a user triggers an action that requires confirmation (e.g.
  revoking or cleaning a certificate)
- **THEN** the system presents a modal dialog naming the action and its
  consequence, with distinct confirm and cancel controls, rather than a
  native browser popup

#### Scenario: Cancelling via the dialog does not perform the action
- **WHEN** a user dismisses the confirmation dialog (via its cancel
  control, its close control, or pressing Escape) without confirming
- **THEN** the system does not perform the action that required
  confirmation

#### Scenario: Confirming via the dialog performs the action
- **WHEN** a user selects the dialog's confirm control
- **THEN** the system proceeds with the action that required
  confirmation
