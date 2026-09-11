## Context

`voxblocks` already ships `vox-dialog` (`frontend/node_modules/
@openvoxproject/voxblocks/src/components/dialog/vox-dialog.ts`): a
modal built on the native `<dialog>` element (real focus trapping,
native Escape-to-close), with a `heading` attribute, a boolean `open`
property plus `show()`/`close()` methods, a default slot for body
content, a named `footer` slot for action buttons, an optional
`light-dismiss` attribute (click-outside-to-close), and a `vox-close`
event fired whenever the dialog closes for *any* reason (confirm
button, cancel button, Escape, backdrop click, or its own built-in ✕
button) - there is exactly one exit event, not one per dismissal
reason.

The only two native popups in this codebase today are
`frontend/src/nodes.js`'s Revoke and Clean confirmations (confirmed by
searching the whole frontend before writing proposal.md).

## Goals / Non-Goals

**Goals:**
- A reusable, Promise-based confirmation helper any future feature can
  reach for instead of `window.confirm()`.
- Preserve the exact existing confirmation copy, including the
  infrastructure-aware reason-specific wording from
  `add-infrastructure-cert-detection` - only the presentation mechanism
  changes.

**Non-Goals:**
- No `window.alert()` replacement - none exist in this codebase today,
  and this proposal is scoped to confirmations specifically.
- No global dialog-queue/stacking behavior - only one confirmation is
  ever in flight at a time in this codebase today (a user can't trigger
  a second Revoke/Clean while one's dialog is open, since the
  triggering buttons are part of the same row being confirmed on).

## Decisions

**`confirmDialog({ heading, body, confirmLabel, danger })` returns a
`Promise<boolean>`, resolved via a single `vox-close` listener, not one
listener per button.** Both the confirm and cancel buttons just call
`dialog.close()`; a local `confirmed` flag (set `true` only by the
confirm button's click handler, before it calls `close()`) is what the
`vox-close` handler actually resolves with. This works uniformly
whether the dialog closed via Confirm, Cancel, Escape, backdrop click,
or the built-in ✕ button - only the confirm path ever sets the flag
true, so every other exit correctly resolves `false`.

```js
export function confirmDialog({ heading, body, confirmLabel = 'Confirm', danger = false }) {
  return new Promise((resolve) => {
    let confirmed = false;
    const dialog = document.createElement('vox-dialog');
    dialog.heading = heading;
    dialog.setAttribute('light-dismiss', '');
    dialog.innerHTML = `
      ${body}
      <div slot="footer">
        <vox-button variant="secondary" data-action="cancel">Cancel</vox-button>
        <vox-button variant="${danger ? 'danger' : 'primary'}" data-action="confirm">${escapeHtml(confirmLabel)}</vox-button>
      </div>`;
    dialog.addEventListener('vox-close', () => {
      dialog.remove();
      resolve(confirmed);
    });
    dialog.querySelector('[data-action="cancel"]').addEventListener('click', () => dialog.close());
    dialog.querySelector('[data-action="confirm"]').addEventListener('click', () => {
      confirmed = true;
      dialog.close();
    });
    document.body.appendChild(dialog);
    dialog.show();
  });
}
```

`light-dismiss` is enabled: clicking the backdrop only ever routes
through `dialog.close()` (never sets `confirmed`), so it's a strictly
safe extra way to cancel, never a way to accidentally confirm.

**`body` is trusted HTML, not auto-escaped plain text** - matching
this codebase's existing convention everywhere else that builds
`innerHTML` from a template literal (`certActionsCell`,
`showActionError`, etc.): the caller is responsible for escaping any
dynamic value it interpolates. This is a real, necessary behavior
change at the two call sites: `window.confirm()`'s message was always
plain text, so `nodes.js`'s existing Revoke/Clean confirmation strings
never escaped the interpolated `certname`/`infrastructureReason`. Once
that text renders as real HTML inside a dialog, those interpolations
need `escapeHtml()` wrapped around them for the first time - a
correctness fix this change surfaces, not a hypothetical.

**`nodes.js`'s two click handlers become `async`, calling `await
confirmDialog(...)` in place of `if (!window.confirm(...)) return;`.**
Same early-return control flow, just non-blocking - see proposal.md's
Impact section.

## Risks / Trade-offs

- **A dynamically-created-and-removed dialog element per confirmation**
  (rather than one static dialog reused across calls) - simplest
  correct implementation given confirmations here are infrequent,
  user-initiated, one-at-a-time actions; not worth the extra state
  management a reusable singleton dialog would need for no real benefit
  at this scale.
