## Why

An audit of the console against the voxblocks component library found
two classes of problem.

**Nothing told the user the console was working.** `vox-loader` was used
nowhere, and eighteen of the console's modules fetch data and render
nothing until it arrives. On a fleet-wide query - resolving every node's
classification, say - that is a blank page for as long as the query
takes, indistinguishable from a broken one.

**Six component properties were silently wrong.** `vox-alert` accepts
`info|success|warning|danger`; five call sites passed `tip` or
`neutral`, which are `vox-badge` variants. A sixth passed `primary` to
`vox-button`, which accepts `brand|alt|danger|ghost`. An unrecognised
variant matches no style rule and no icon, so those alerts rendered with
no colour and no icon, and that button rendered unstyled - with no error
anywhere to notice.

## What Changes

- **Slow requests show a loading indicator.** Applied across the
  console's data views. Deliberately delayed: an indicator that appears
  and vanishes inside a tenth of a second reads as a glitch, so nothing
  is shown until the wait is worth acknowledging.

- **Component properties are corrected** to values the components
  actually accept, restoring the colour and iconography those alerts and
  that button were meant to have.

- **Filters over unbounded option lists became type-ahead.** A filter
  whose options come from recorded data - deploy sources, refs, activity
  categories and actions, node groups - can hold dozens of entries, and
  a plain select is the wrong control at that size. Filters over fixed
  short enumerations were deliberately left as they were.

- **The deploy trigger and its source picker became one attached
  control**, since they are one action.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `web-shell`: strengthens the voxblocks requirement from "is built on
  the library" to "uses it as documented", and adds a requirement that
  the console acknowledges work in progress.

## Impact

Presentation only - no API, schema, or behavioral contract changes.

- `frontend/src/app.js`: shared delayed-loading helper.
- 18 modules under `frontend/src/`: wrapped their primary fetch.
- `frontend/src/{node,nodes,preferences,code-repositories}.js`: variant
  corrections.
- `frontend/templates/pages/{code,activity,packages}.tmpl`: type-ahead
  filters, attached deploy control.
