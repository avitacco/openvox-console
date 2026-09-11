## Context

`frontend/src/group.js` and `frontend/templates/pages/group.tmpl` hold
four JSON-shaped group fields (`classes`, `parameters`, `rule`, `pins`)
as raw `<vox-textarea>` elements. `save()` parses each with
`JSON.parse` via a shared `parseJSONField` helper and PUTs/POSTs the
resulting object as-is; `loadForEdit()` does the reverse with
`JSON.stringify`. This proposal only touches the `rule` field - see
proposal.md for why `classes`/`parameters` stay as JSON.

`rule` is `internal/classifier.Condition[]`, always exactly
`{factPath: string, operator: string, value: string}` per element -
see `openspec/specs/classifier/spec.md` and
`internal/classifier/match.go`. This fixed, flat, three-key shape (no
optional fields, no nesting) is what makes a structured per-row builder
tractable without a general-purpose JSON-schema-driven form.

This project's frontend has no existing repeatable-row (add/remove a
row of inputs) UI pattern to reuse - confirmed by search. This will be
the first one; the pattern below is written to be reusable for a
future one (e.g. `classes`), but this change does not attempt that
itself.

## Goals / Non-Goals

**Goals:**
- Build `rule` as N rows of (`factPath` input, `operator` select,
  `value` input) with add/remove controls, serializing to/from the
  exact same `Condition[]` shape.
- Surface the two mistakes this session actually hit: an empty
  `factPath` and an invalid `~` regex, both of which the backend
  currently accepts silently and just never matches.
- Preserve a raw-JSON escape hatch so no existing capability (pasting
  a rule, an edge case the builder doesn't model) is lost.

**Non-Goals:**
- No change to `classes`, `parameters`, or `pins` editing.
- No change to `internal/classifier` or the group API - validation
  added here is a frontend affordance only (see Decisions).
- No general-purpose reusable "repeatable row" component extraction -
  this change builds the pattern once, for `rule`; a future change can
  extract it if a second consumer appears.

## Decisions

**Builder state is the source of truth; JSON view is a projection, not
a second copy.** Rather than keeping row state and a JSON string in
sync as two independently-editable representations, the JSON textarea
is generated from row state on every switch to JSON view, and row
state is rebuilt from parsed JSON on every switch back to builder view.
Only one representation is ever "live" at a time (matching which view
is currently visible). This avoids the class of bug where the two
views silently drift and the save button ends up serializing whichever
one is stale.
- *Alternative considered*: keep both in sync live on every keystroke.
  Rejected - re-parsing the JSON textarea on every keystroke to
  refresh row state (or vice versa) is wasted work for a field that's
  edited in bursts, not continuously, and risks a half-typed JSON
  string producing a parse error mid-keystroke.

**Toggling to JSON view when row state is invalid (a `~` pattern that
doesn't compile) still succeeds and shows the invalid value as-is.**
The JSON view is also where a user fixes a shape the builder can't
express, so it must never be blocked by the same validation that
blocks *save*. Toggling back to builder view re-validates and
re-flags rows as usual.

**Validation blocks save, not row entry.** A row's `factPath` is
allowed to be empty and a `~` value is allowed to be an invalid regex
while editing (an operator or value typed in a different order than
`factPath` shouldn't show an error mid-edit) - flagging happens for
all rows at once, on save. This matches `save()`'s existing pattern
(name/priority validation already happens at save time, not per
keystroke).

**Validation is a frontend affordance only; the backend is unchanged.**
`internal/classifier/match.go`'s existing behavior - an empty
`factPath` or bad regex making a condition unconditionally false,
rather than erroring - stays exactly as it is. Per proposal.md's scope,
this change is UI-only: a group saved via the JSON escape hatch, the
raw API, or a future non-console client can still produce a
rule that silently never matches. Making the backend itself reject
these shapes is out of scope here and would be a separate,
`classifier`-capability change if wanted later.

**Operator select's options are hardcoded to the six known operators**
(`=`, `!=`, `~`, `>`, `<`, `>=`, `<=`), matching
`internal/classifier/match.go`'s `switch c.Operator`. Not fetched from
an API - this set changes only alongside a backend code change, at
which point the frontend constant is updated in the same change, the
same way `nodeagent`/`orchestrator`'s mirrored action constants are
kept in sync by convention elsewhere in this project.

**Row identity uses array index, not a generated id.** Conditions have
no natural identity (no name, no id from the API) and are always
re-serialized in full on save, so index-based row keys (matching how
`renderDeploys`/`renderNodes` elsewhere in this codebase key list
rendering) are sufficient - no risk of stale-id bugs since the whole
row list is always rebuilt from current state on every render, never
patched in place.

**An empty rule (zero rows) renders as zero rows plus the existing
"Add condition" button, not a placeholder message.** A group with an
empty rule and no pins matches nothing (see
`openspec/specs/classifier/spec.md`'s "Fact-based group matching"), but
that's already true today via an empty JSON array (`[]`) in the
textarea; the builder's empty state is the same thing, just visually
zero rows instead of empty-array text. `pins` already covers the "pin
nodes explicitly instead" path, so no additional messaging is added
here.

## Risks / Trade-offs

[Risk] A user relies on today's JSON textarea muscle memory and is
surprised by the new default view → Mitigation: default to builder
view (it is strictly more discoverable for a new user, which is the
majority case this session's bug came from), but the JSON toggle is
one click away and pre-populated with the current rule, not blanked.

[Risk] The regex-validity check (`~`) client-side uses JavaScript's
`RegExp`, not Go's RE2 - the two engines disagree on some syntax
(backreferences, lookaround are JS-only features Go's `regexp` package
rejects). A pattern that validates client-side could still fail to
compile server-side → Mitigation: this is strictly better than today's
zero validation, and a wrong-but-JS-valid pattern still fails safely
the same way it does today (silently matches nothing) rather than
newly breaking anything. Not fixed here; flagged rather than hidden -
closing the RE2/JS gap exactly would need shipping an RE2 engine to
the browser, out of proportion to this change's scope.

## Migration Plan

None needed - this is a frontend-only bundle change with no data
migration and no wire-format change. Existing groups' `rule` arrays
load into the builder unchanged the next time the group page is
rebuilt and the console is restarted (same deploy step as any other
frontend change in this project - `make frontend && make build`, per
this session's own restart of the console for the connectivity-summary
fix).
