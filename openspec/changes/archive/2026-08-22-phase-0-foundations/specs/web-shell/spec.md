## Purpose

Serves the console's web frontend directly from the single binary, using
assets embedded at build time, so no separate web server or asset
deployment step is needed.

## ADDED Requirements

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
