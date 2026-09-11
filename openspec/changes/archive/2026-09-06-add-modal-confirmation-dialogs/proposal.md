## Why

The Nodes page's Revoke/Clean confirmations use the browser's native
`window.confirm()` - an unstyled OS-level popup, jarring against the
console's own theme, and the exact thing `add-node-certificate-
management`'s design.md flagged at the time as a stopgap ("this
codebase has no existing modal/dialog component to reach for... more
UI-framework work than a confirmation step warrants"). That's no longer
true: `voxblocks` (the console's own design system, already vendored)
ships a `vox-dialog` modal component built on the native `<dialog>`
element (focus trapping, Escape-to-close) that was never wired up.
Every confirmation in the app should use it instead of a native popup.

## What Changes

- Add a reusable `confirmDialog({ heading, body, confirmLabel, danger })`
  helper to `frontend/src/app.js`, returning a Promise that resolves
  `true`/`false` - built on `vox-dialog`, with explicit Confirm/Cancel
  footer buttons, matching the console's own theme and this
  design system's existing conventions.
- Replace both `window.confirm()` calls in `frontend/src/nodes.js`
  (Revoke and Clean) with this helper - the confirmation copy and the
  infrastructure-aware reason-specific wording (from
  `add-infrastructure-cert-detection`) are unchanged, only the
  presentation mechanism changes.
- No other native popups exist in this codebase today (confirmed by
  searching for `window.confirm`/`window.alert`/`window.prompt` across
  the whole frontend before writing this proposal) - this change's
  scope is exactly these two call sites plus the reusable helper itself,
  so future confirmations have it ready rather than reaching for
  `window.confirm()` again.

## Capabilities

### Modified Capabilities
- `web-shell`: adds a "Confirmation dialogs use modal components"
  requirement, extending the existing "Voxblocks component library"
  convention to specifically cover confirmations.

## Impact

- **Code**: `frontend/src/app.js` (new `confirmDialog` export),
  `frontend/src/nodes.js` (both `window.confirm()` call sites replaced
  with `await confirmDialog(...)`, and their surrounding click handlers
  become `async`).
- **Behavior change worth calling out**: `window.confirm()` is
  synchronous and blocks the calling script; `vox-dialog` is
  asynchronous (event-driven). The Revoke/Clean click handlers already
  return early on cancellation today, so converting them to `async`
  functions that `await` the dialog's result before proceeding
  preserves the exact same control flow, just non-blocking.
- **No new dependency**: `vox-dialog` is already part of the vendored
  `voxblocks` package this project already uses.
