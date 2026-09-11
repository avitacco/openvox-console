## Why

A node group's match rule (`internal/classifier`'s `Condition[]` -
`factPath`/`operator`/`value`) is edited today as a raw JSON textarea on
the group page. This offers no operator hinting and no validation, so a
typo (an empty or misspelled `factPath`, an invalid operator) is
silently accepted and the group simply matches no nodes - discovered
live this session when a user's rule went in with an invalid `factPath`
and produced no error, just a group with zero matches. A structured,
per-condition builder removes this whole class of mistake without
changing what gets sent to the API.

## What Changes

- Replace the match-rule `<vox-textarea>` on the group page with a
  structured builder: one row per condition, each with a `factPath`
  text input, an `operator` `<vox-select>` constrained to the six valid
  operators (`=`, `!=`, `~`, `>`, `<`, `>=`, `<=`), and a `value` text
  input, plus add-row and remove-row controls.
- Add operator-specific help text: a note on `~`'s RE2 substring-match
  (not anchored) semantics when `~` is selected, and a note that
  `value` must parse as a number for `>`/`<`/`>=`/`<=`.
- Add client-side validation before save: a condition with an empty
  `factPath` or a `~` pattern that fails to compile as a regex is
  flagged and blocks submission, rather than silently saving a rule
  that can never match (today's backend behavior in
  `internal/classifier/match.go` treats these as an unconditional
  non-match, with no error surfaced anywhere).
- Keep an "Edit as JSON" toggle that switches to today's raw-JSON
  textarea for the same field, so pasting a rule copied from elsewhere,
  or a shape the builder doesn't cover, is never blocked.
- The `rule` field's wire shape is unchanged: still
  `[{factPath, operator, value}, ...]`, sent and received exactly as
  today. No backend, API, or database change.

## Capabilities

### New Capabilities

(none)

### Modified Capabilities

- `classifier`: the "Console UI for group management" requirement
  gains scenarios describing structured match-rule editing (per-
  condition fields instead of raw JSON) and client-side validation
  that blocks saving an unusable condition.

## Impact

- `frontend/templates/pages/group.tmpl`: replace the match-rule
  textarea markup with the builder's row template and a JSON-view
  toggle.
- `frontend/src/group.js`: replace direct JSON
  parse/stringify of the `rule` field with row-state management,
  render/serialize logic, and validation, while `classes`,
  `parameters`, and `pins` remain raw-JSON textareas (their shapes are
  more open-ended than `rule`'s fixed three keys, so a structured
  builder isn't attempted for them here).
- No changes to `internal/classifier` or any API endpoint - the `rule`
  array's shape and the backend's matching behavior are unchanged.
