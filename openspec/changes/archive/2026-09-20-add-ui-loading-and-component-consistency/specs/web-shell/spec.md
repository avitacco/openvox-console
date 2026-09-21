## ADDED Requirements

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

## MODIFIED Requirements

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
