---
title: Classify nodes
summary: Decide which classes, parameters and environment each node gets, with node groups matched by facts or pinned by name.
order: 1
---

openvoxserver asks the console what each node should be: which classes it
includes, with which parameters, and which environment its code comes
from. The console answers from its **node groups**. Classification
reaches openvoxserver only once it is connected, which both install
guides cover.

Viewing groups needs `classifier:read`; creating, changing and deleting
them needs `classifier:write`.

## How a group decides which nodes it applies to

A group applies to a node when either:

- the node is **pinned** to it by name, or
- the node's facts satisfy **every** condition in the group's match rule.

A group with no conditions and no pinned nodes applies to no node at all.
An empty rule deliberately does not mean "everyone", so a half-finished
group cannot take over the fleet.

Each condition compares one fact with a value:

| Operator | Matches when the fact |
| --- | --- |
| `=` | equals the value |
| `!=` | differs from the value |
| `~` | matches the value as a regular expression |
| `>` `<` `>=` `<=` | compares that way, numerically where both sides are numbers |

Facts are named by their path, with dots for structured facts:
`os.family`, `os.release.major`, `networking.domain`.

## Create a group

1. Open **Groups** and create a group.
2. Name it, and choose its **environment** if it should set one.
3. Add **classes**, each with any parameters it needs, and any
   **top-scope parameters**.
4. Build its **match rule**, one condition per row: a fact path, an
   operator and a value. The editor refuses to save a condition with no
   fact path, or a regular expression that does not compile, and shows
   which row is wrong.
5. Pin any nodes by certname that should belong whatever their facts.
6. Give it a **priority**, then save.

The groups list shows how many nodes each group matches right now, so a
rule that matches nothing, or everything, is visible straight away.

> [!NOTE]
> **Edit as JSON** shows the rule as raw JSON, for pasting a rule in or
> writing a shape the row editor does not offer. Its checks are the row
> editor's: a rule saved as JSON, or through the API, with an empty fact
> path or a broken regular expression is accepted, and simply never
> matches.

## When groups overlap

A node matching several groups gets everything from all of them: every
class, every parameter. Where two groups set the same thing differently -
the same class parameter, the same top-scope parameter, or the
environment - the group with the **higher priority wins**. Groups never
inherit from each other; priority is the only rule.

## Check what a node gets

A group's page lists the nodes it matches right now. What a node finally
receives is every matching group merged, and the one place that shows
that whole answer is enc-bridge itself: ask it on the openvoxserver host,
as the install guides show, and it prints exactly what openvoxserver is
told.

Classification takes effect at the node's next Puppet run. To apply it
now, start a run from the group's page, which runs Puppet on every node
the group matches; see [Run jobs](run-jobs.html).

Every change to a group is recorded on the **Activity** page, with who
made it.
