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

Components SHALL be used according to their documented interfaces. A
property value SHALL be one the component accepts: these components
ignore unrecognised values rather than reporting them, so a wrong value
degrades the element silently - losing its colour, its icon or its
styling - with nothing to alert anyone.

Where the library offers a control suited to a particular shape of data,
the console SHALL use it rather than a less suitable one. In particular,
a filter over a set of options large enough to be awkward to scan SHALL
offer type-ahead narrowing, while a filter over a short fixed set SHALL
NOT - the extra interaction costs more than it saves there.

#### Scenario: Placeholder shell uses voxblocks components
- **WHEN** the placeholder UI shell renders
- **THEN** it is composed of voxblocks components rather than bespoke
  markup/styling for elements voxblocks already provides

#### Scenario: A component is given a value it accepts
- **WHEN** the console sets a component property that has a defined set
  of permitted values
- **THEN** the value is one of those values, so the component renders
  with its intended styling and iconography

#### Scenario: Filtering a large set of options
- **WHEN** a filter's options are drawn from recorded data and can grow
  to a size that is awkward to scan
- **THEN** the filter lets the user narrow the options by typing

#### Scenario: Filtering a short fixed set
- **WHEN** a filter's options are a short fixed enumeration
- **THEN** the filter presents them directly for selection

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

### Requirement: Work in progress is acknowledged
The console SHALL indicate that a request is in progress when the wait
is long enough for a user to notice, rather than leaving a view empty
while it loads. An empty view is indistinguishable from a broken one,
and the console's fleet-wide queries are among its slowest.

The indication SHALL NOT appear for waits short enough that it would
flash and vanish, which reads as a fault rather than as feedback.

An indicator SHALL NOT outlive the work it describes: once the result
has rendered, no indicator may remain or reappear.

#### Scenario: A slow request
- **WHEN** a view's data takes long enough to fetch to be noticeable
- **THEN** the console shows that work is in progress, and replaces it
  with the result when it arrives

#### Scenario: A fast request
- **WHEN** a view's data arrives quickly
- **THEN** no loading indicator is shown at all

#### Scenario: A failed request
- **WHEN** a view's data cannot be fetched
- **THEN** the error is displayed and no loading indicator remains
