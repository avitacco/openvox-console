## Context

See proposal.md - Why. Two facts about the component library shaped
every decision here, both established by reading its source rather than
its prose:

- **Unrecognised property values fail silently.** An alert's variant
  selects both a CSS class and an icon by lookup. A value outside the
  permitted set matches no class and no icon, so the element renders
  transparent and iconless. Measured: an alert with the wrong variant
  had a transparent background and zero icon paths; with the right one,
  a coloured background and a drawn icon.

- **A component's prose description is not its interface.** Two of this
  session's mistakes came from reading a description and skipping the
  usage example - a labelled value whose label turned out to be
  screen-reader-only, and an attached-control group whose own example
  uses `aria-label` precisely because a visible label breaks its
  alignment.

## Goals / Non-Goals

**Goals:**

- The console never leaves a view blank while it works.
- Every component property value is one the component accepts.
- Controls match the shape of their data.

**Non-Goals:**

- Using every component in the library. Most of the unused ones are
  marketing components for landing pages and have no place in a console.
- Changing any API, schema or behavioral contract. This is presentation.
- Reworking the filter rows into attached groups. That component joins
  controls into a single control; a row of independent labelled filters
  is not one control, and forcing it there would be wrong.

## Decisions

### The loading helper wraps the work rather than returning a canceller

First implemented as `showLoading(el)` returning a cancel function. That
was wrong and was caught before shipping: the guard went in at every
call site and the cancel did not, leaving a timer that fires after the
content has rendered and wipes it.

Wrapping the work puts the timer's cancellation in a `finally`, where it
cannot be forgotten on one branch:

    const page = await withLoading(results, () => fetchJSON(url));

The wrap goes around the fetch and not the render, so the indicator
covers exactly the wait.

### The indicator is delayed, not immediate

Most of these requests return in well under a tenth of a second against
a local console. An indicator that appears and disappears in that window
reads as a flicker - a fault, not feedback. Nothing is shown until the
work has been running long enough that a user has begun to wonder.

The alternative - showing it immediately and accepting the flash - was
rejected because the flash is worse than no feedback for the common
case, and the common case is the overwhelming majority.

### Type-ahead is applied by the shape of the option list, not uniformly

A filter whose options come from recorded data is unbounded: deploy refs
include every commit ever deployed. Those get type-ahead. A filter over
a fixed short enumeration - a status, an ordering - does not: typing to
narrow four options is slower than picking one, so converting those
would make the console worse while looking more consistent.

The type-ahead control takes the same option children, the same value
property and the same change event as the plain one, so this is a
template-level swap with no handler changes.

### Attached controls use a non-visible label

The group stretches its children to equal height. A visible label sits
above its control, making that child taller and stretching the adjacent
button into a slab beside it - which is exactly what happened on the
first attempt. The accessible name is kept; only the visible label is
dropped, and the selected value names itself.

## Risks / Trade-offs

**A delayed indicator means the slowest-feeling waits are the ones just
under the threshold** → Accepted. Those waits are short enough that
feedback would arrive as the content does.

**Silent property failures will recur** → The audit that found these can
be re-run: it resolves each component's declared properties through its
inheritance chain and compares them with what the console sets. Worth
keeping as a check rather than trusting review to catch a value that
produces no error.

**Type-ahead over a very large option list still renders every option**
→ Fine at the sizes seen (tens), and the control filters client-side.
A list large enough to need server-side search would be a different
change.

## Migration Plan

None. Presentation-only, no persisted state, no API surface. Reverting
is reverting the frontend build.
