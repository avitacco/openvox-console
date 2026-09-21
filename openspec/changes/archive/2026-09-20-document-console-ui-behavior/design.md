## Context

See proposal.md - Why. This change is retroactive: every behavior it
describes is already implemented, merged and in use. Nothing here is a
plan for work; it is a reconciliation of the specs with the console that
exists.

That inverts the usual order, and the risk that comes with it is the
only thing worth designing around: a spec written by reading code tends
to describe *how* the code happens to work rather than *what* the system
must do, and then locks in incidental detail as though it were a
contract.

## Goals / Non-Goals

**Goals:**

- Specs that describe the console as it is, at the same level of
  abstraction as the requirements already there.
- Capture the constraints that are load-bearing, so a future change
  cannot quietly drop them without the spec objecting.

**Non-Goals:**

- Any behavior change. If writing these revealed a behavior that seems
  wrong, the fix belongs in its own change, not here.
- Describing implementation. No requirement here names a component, an
  element, an endpoint path or a column.
- Retrofitting the parts of this work that changed no behavior at all -
  dead-code removal, a struct conversion, test isolation fixes. Specs
  describe behavior; those changed none.

## Decisions

### Requirements are written from intent, not from the code's shape

Each requirement states what an operator must be able to do and what the
system must not do to them, and leaves the mechanism out. The node
detail requirement says facts must be reachable in a structured form
*and* in full, and says why - the structured form is deliberately a
subset - rather than naming tabs, a toggle, or a JSON view. The
classifier requirement says group listings report how many nodes match,
not that there is a counts endpoint.

The test applied throughout: could the implementation be replaced
without the requirement changing? Where the answer was no, the
requirement was too specific and was raised a level.

### Constraints are recorded where dropping them would be a regression

Three are worth stating because they are easy to lose and were
deliberate:

- **Selection cannot outlive visibility.** Nodes filtered or paged out
  of view are deselected, so a bulk run can only target what the user
  could see. Without this, a filter change silently arms a run against
  nodes the user is no longer looking at.
- **Group filtering on the package catalogue needs permission to read
  groups.** The catalogue is readable by anyone who may read nodes;
  without the extra check, filtering by group discloses group membership
  to someone not permitted to know it.
- **Counts degrade rather than fail.** An unreachable inventory leaves
  group counts unavailable, not zero - the two mean different things,
  and rendering one as the other misreports an empty group.

### Fleet-resolved counts are specified as current, not stored

A fact-matched group's membership changes whenever nodes report, so the
requirement says counts are resolved against the current fleet. That is
a behavioral commitment (the number is never stale) rather than an
implementation note, and it rules out caching them without addressing
staleness.

## Risks / Trade-offs

**A retroactive spec can entrench a bug as a requirement** → Mitigated
by writing from intent and by only describing behavior that was a
deliberate decision at the time. Where behavior looked accidental it was
left undescribed rather than blessed.

**Tasks are verification, not implementation** → Every task here checks
that the written requirement matches what the code already does. A task
that fails means the spec is wrong, not the code - and is fixed by
editing the spec.

## Migration Plan

None. No code changes, no migration, nothing to roll back. Archiving
this change merges the deltas into the main specs.
