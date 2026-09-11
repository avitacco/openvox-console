## Why

`internal/agentdist` now serves install scripts for three platforms
(`GET /packages/install.sh`, `install.ps1`, `install-macos.sh` - see
`add-multi-platform-agent-install`), but nothing in the console UI tells
an operator these routes exist. Onboarding a new node today requires
already knowing the exact route paths (undocumented in the UI) - a real
usability gap now that there's a real answer to "how do I add a node"
across three platforms, not just one.

## What Changes

- Add an "Add node" control to the Nodes page that opens a modal (built
  on the same `vox-dialog`/`confirmDialog` pattern from
  `add-modal-confirmation-dialogs`) listing each supported platform
  (Linux, Windows, macOS) with the full, absolute URL to that
  platform's install script, so an operator can copy the right one for
  a new node without knowing the routes exist beforehand.
- No new backend routes and no new dependencies - this is a pure
  frontend surface over `agent-distribution`'s existing, already-built
  routes.

## Capabilities

### New Capabilities
(none)

### Modified Capabilities
- `node-connectivity`: extends the existing "Dedicated nodes page"
  requirement so the nodes page also surfaces how to onboard a new
  node, alongside its existing node-listing/sorting/filtering behavior.

## Impact

- **Code**: `frontend/templates/pages/nodes.tmpl` (new control),
  `frontend/src/nodes.js` (modal content/wiring). No Go changes - the
  three routes this surfaces already exist and are unauthenticated by
  design (a node running its own install script has no console user
  token to present), so the modal's content is just static route paths
  resolved against the browser's own origin, not a new API call.
