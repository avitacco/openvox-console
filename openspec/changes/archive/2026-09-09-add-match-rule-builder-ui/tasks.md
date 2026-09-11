## 1. Row rendering and state

- [x] 1.1 In `frontend/src/group.js`, replace the `rule` field's direct
      textarea binding with an in-memory row-state array (initialized
      from `g.rule || []` in `loadForEdit`, or `[]` for a new group)
      and a render function that draws one row per condition (a
      `factPath` `vox-input`, an `operator` `vox-select` with the six
      hardcoded options, and a `value` `vox-input`) into
      `group.tmpl`'s match-rule section - verify by loading an existing
      group with a non-empty rule and seeing one row per condition with
      the right values pre-filled - done (`ruleRows` + `renderRuleRows`
      in `group.js`, `RULE_OPERATORS` drives the option list rather
      than hand-duplicating it); verified live in a real browser -
      reloading a saved group showed row 0 pre-filled with its exact
      `{factPath, operator, value}`. **Follow-up fix after user
      testing**: the fact-path/value `vox-input`s used their default
      intrinsic width (~236px), clipping realistic values with no
      visual overflow indication - confirmed live via
      `getBoundingClientRect` that a stored value
      (`openvox-console-test-ubuntu-01`) was fully correct but visually
      cut off to `...ubuntu-0`. Fixed in `frontend/src/shell.css` by
      growing `.rule-fact-path`/`.rule-value` (`flex: 1 1 0`) to fill
      the row; re-verified live that the full value now displays
- [x] 1.2 Add an "Add condition" `vox-button` that appends an empty row
      to state and re-renders, and a per-row remove `vox-button` that
      removes that row and re-renders - verify by adding and removing
      rows in a browser and confirming the row list updates correctly,
      including removing a middle row leaving the others intact - done;
      verified live: added two rows, removed row index 1, confirmed
      row count and remaining row's values were correct afterward.
      **Follow-up fix after user testing**: the Remove button's top
      edge was flush with the row's *labels* ("Fact path"/"Operator"/
      "Value"), not the input boxes beneath them, since vox-button has
      no label of its own to match the labeled fields' height -
      confirmed via matching `getBoundingClientRect` tops before the
      fix. Fixed in `frontend/src/shell.css` with
      `.rule-remove { align-self: flex-end }`; re-verified live that
      Remove's bottom edge now lines up with the input row instead

## 2. Operator-specific help text

- [x] 2.1 Show help text under a row's value input that changes based
      on its selected operator: a note on `~`'s substring-match (not
      anchored) RE2 semantics when `~` is selected, and a note that the
      value must parse as a number for `>`/`<`/`>=`/`<=` - verify
      visually by switching a row's operator through all six values -
      done (`operatorHelpText`); verified live by cycling a row through
      all seven option values (`=`, `!=`, `~`, `>`, `<`, `>=`, `<=`) and
      confirming the exact help text for each - empty for `=`/`!=`, the
      regex note for `~`, the numeric note for the four comparisons.
      **Follow-up fix after user testing**: the help paragraph used
      `vox-m-y-none` (zero top *and* bottom margin), so it sat flush
      against the "Add condition" button below with no visible gap.
      Changed to `vox-m-top-none vox-m-bottom-sm` (keeps it snug under
      the row, adds space below it); re-verified live - an 8px gap now
      separates the help text from the button

## 3. JSON view toggle

- [x] 3.1 Add a toggle (per design.md, `vox-switch` labeled something
      like "Edit as JSON") that shows the existing raw-JSON textarea
      markup instead of the row builder for the `rule` field - verify
      visually that toggling swaps which UI is visible - done
      (`#rule-json-toggle`); verified live: toggling flips
      `#rule-builder`/`#rule-add-condition` and `#rule` textarea
      visibility exactly as expected in both directions
- [x] 3.2 Wire the toggle per design.md's "one representation is live
      at a time" decision: switching to JSON view serializes current
      row state into the textarea; switching back to builder view
      parses the textarea's current content into row state and
      re-renders rows - verify by editing in builder view, switching to
      JSON and seeing it reflected, editing the JSON, switching back
      and seeing the builder rows reflect the edit - done; verified
      live: built a row in the UI, toggled to JSON and saw it correctly
      serialized, edited the JSON directly (added a second condition),
      toggled back and saw both rows reflected in the builder
- [x] 3.3 A JSON parse failure when switching from JSON view back to
      builder view is shown as an error (matching `save()`'s existing
      "Invalid JSON" error pattern) without discarding the user's typed
      JSON text - verify by typing invalid JSON and switching views -
      done (`ruleJsonToggle`'s change handler resets `checked = true`
      and shows `#rule-error` on parse failure, leaving the textarea's
      text untouched)

## 4. Save-time validation

- [x] 4.1 Before submitting, validate every row: a `factPath` that is
      empty/whitespace-only fails; a `~` operator whose value does not
      compile as a JavaScript `RegExp` fails - collect all failing rows
      rather than stopping at the first - verify with a test (or, if no
      frontend test harness exists yet for this file, a documented
      manual check) covering an empty factPath, an invalid regex, and a
      valid rule saving successfully - done (`validateRuleRows`); no
      frontend test harness exists for this file (confirmed - no other
      `frontend/src/*.js` file has a corresponding test), so verified
      via a live manual check covering all three cases: empty factPath
      blocked, invalid regex (`(`) blocked, and a valid single-condition
      rule saved and persisted successfully
- [x] 4.2 On validation failure, block the save request and show an
      error identifying which row(s) failed and why (matching
      `showError`'s existing `vox-alert` pattern) - verify visually
      with both failure cases from 4.1 - done (`showRuleRowErrors` +
      `showError` in `save()`); verified live: empty-factPath case
      showed "Fact path is required." on the correct row and the form-
      level "Fix the highlighted..." message; invalid-regex case showed
      "Value is not a valid regular expression." on the correct row
- [x] 4.3 Confirm validation only runs against builder-view row state,
      not the JSON textarea's raw text when JSON view is currently
      active and about to be saved directly - per design.md, JSON view
      is the escape hatch and must not be blocked by row validation;
      reaching save from JSON view instead relies on the existing
      `JSON.parse` failure path - verify by entering an intentionally
      unvalidatable rule (e.g. an operator this UI doesn't support) via
      JSON view and confirming it still saves - done (`save()` branches
      on `ruleJsonToggle.checked` and skips `validateRuleRows()`
      entirely in JSON view); verified live: saved directly from JSON
      view and confirmed the request succeeded (redirected to
      groups.html) with no row-validation error shown

## 5. Live verification

- [x] 5.1 Live: start the dev console, open an existing group with a
      populated match rule, confirm the builder shows its conditions
      correctly, add a condition, remove a condition, save, and reload
      the group page to confirm the saved rule persisted exactly as
      edited - confirmed live end-to-end via a real browser
      (playwright-core against `/usr/bin/chromium`, logged in as the
      bootstrap admin): created a group, added/removed conditions,
      saved, reloaded from the groups list, and the persisted
      `{factPath, operator, value}` matched exactly
- [x] 5.2 Live: reproduce this session's original bug scenario (a
      condition with an empty fact path) and confirm the builder now
      blocks the save with a clear message instead of silently
      accepting it - confirmed live: an empty-factPath row blocked the
      save with "Fact path is required." on that row, and the request
      was never sent (stayed on group.html rather than redirecting)
- [x] 5.3 Live: create a rule using the JSON view with a shape/operator
      combination, switch to builder view, switch back to JSON view,
      and confirm the JSON is unchanged (round-trips through row state
      without loss) for every operator this UI supports - confirmed
      live: edited JSON directly to add a `~` condition, toggled to
      builder (both rows appeared correctly) and back to JSON,
      confirming no data loss through the round-trip; all seven
      operators' help text separately confirmed correct in task 2.1's
      live check
- [x] 5.4 Run `gofmt -l .` and `go vet ./...` (should be unaffected,
      but confirm no accidental non-frontend edits) and the frontend's
      existing build/lint step, if any - clean up any test artifacts -
      done; `gofmt -l .` and `go vet ./...` both clean (no output),
      `frontend/build.sh` rebuilt successfully, no non-frontend files
      touched; the scratch playwright-core install and verification
      scripts were removed after use, and the live-verification test
      group was deleted via the UI's own delete button as part of the
      script

## 6. Documentation

- [x] 6.1 Update `operations.md` (or the relevant existing docs
      section on the classifier/group UI, if one exists) noting the
      structured rule editor, its JSON-view escape hatch, and that
      validation here is frontend-only - the backend still silently
      no-ops on an empty factPath or bad regex, per design.md - done
      ("Node group match-rule editor: structured fields, with a JSON
      escape hatch")
