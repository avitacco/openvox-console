---
title: Classify nodes
summary: Decide which classes, parameters and environment each node gets, with a tree of node groups matched by facts or pinned by name.
order: 1
---

openvoxserver asks the console what each node should be: which classes it
includes, with which parameters, and which environment its code comes
from. The console answers from its **node groups**. Classification
reaches openvoxserver only once it is connected, which both install
guides cover.

Viewing groups needs `classifier:read`; creating, changing and deleting
them needs `classifier:write`.

## Groups form a tree

Every group has a **parent**. At the top is **All Nodes**, a built-in
group that every node belongs to. You cannot delete, rename or move it,
and it has no rule or pinned nodes, but you can give it classes,
parameters and an environment, and they then apply to the whole fleet.

A group can only contain nodes that are also in its parent. So a group
under **Linux Servers** never picks up a Windows node, whatever its own
rule says, and a group's nodes are always a subset of its parent's. A
new group goes directly under All Nodes unless you choose another
parent.

A group cannot be moved under itself or under one of the groups beneath
it, and a group that still has groups beneath it cannot be deleted: move
or delete those first.

A group that a role is granted on cannot be deleted either, because
that would silently take away the access the role gives. Remove the
group from the role first, on the **Roles** page - see
[Users, roles and tokens](access.html).

## How a group decides which nodes it applies to

A group applies to a node when the node is in the group's parent and
either:

- the node is **pinned** to it by name, or
- the node's facts satisfy the group's match rule: **all** of its
  conditions, or **any** one of them, whichever the group is set to.

A group with no conditions and no pinned nodes applies to no node at all.
An empty rule deliberately does not mean "everyone", so a half-finished
group cannot take over the fleet.

A pin overrides the group's own rule, not its parent. A node pinned to a
group whose parent does not contain it is not in the group, and the
group's page lists such pins so that one with no effect is not silent.

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
2. Name it, choose its **parent group**, and choose its **environment**
   if it should set one. The environment is a dropdown. **Not set**
   (which says what the group then inherits from its parent) leaves the
   choice to the parent. The environments openvoxserver reports are
   listed below it. **Other...** opens a text field for a name that is
   not listed yet, such as a branch you have just pushed, or when the
   list could not be loaded.
3. Add **classes**, each with any parameters it needs, and any
   **top-scope parameters**. See [Choose classes](#choose-classes)
   below.
4. Build its **match rule**, one condition per row: a fact path, an
   operator and a value. Choose whether a node must satisfy **all of the
   conditions** or **any one** of them. The editor refuses to save a
   condition with no fact path, or a regular expression that does not
   compile, and shows which row is wrong.
5. Pin any nodes by certname that should belong whatever their facts.
6. Give it a **priority**, then save.

![](shot:group-editor)

The groups list shows the tree, each group beneath its parent, with how
many nodes each one matches right now, so a rule that matches nothing,
or everything, is visible straight away.

> [!NOTE]
> **Edit as JSON** shows the rule as raw JSON, for pasting a rule in or
> writing a shape the row editor does not offer. Its checks are the row
> editor's: a rule saved as JSON, or through the API, with an empty fact
> path or a broken regular expression is accepted, and simply never
> matches.

## Choose classes

The console reads the classes your deployed code defines from
openvoxserver, so a group's classes are picked, not typed.

1. Under **Classes**, start typing in **Add a class**, pick the class,
   and add it.
2. To set one of its parameters, pick it under **Set a parameter**. The
   field that appears fits the parameter's type: a choice for a Boolean
   or an Enum, a number for an Integer or Float, text for a String. Its
   note shows the type and the default from the code. Any other type is
   entered as JSON, so text needs double quotes: `"value"`,
   `["a", "b"]`.
3. Leave a parameter out to keep the value the code or Hiera gives it.
   Remove a parameter to go back to that.

The classes come from the group's **environment**: its own, or else the
nearest parent's that sets one, or else `production`. The page says
which, and when the list was read. A node that also matches another
group that sets a different environment compiles against that one, which
the group cannot know.

The list is reused for five minutes and read again after a code deploy.
**Refresh classes** reads it at once, and needs `classifier:write`.

> [!NOTE]
> openvoxserver keeps its own cache of each environment. If it is set to
> keep environments indefinitely, as the container image is, a class
> added by new code does not appear until that cache is flushed - the
> same flush a catalog needs to see the new code. See
> [Install with containers](containers.html).

**Edit as JSON** shows the classes as raw JSON, for pasting them in.

### Classes that do not match the code

A group can name a class the environment does not define, set a
parameter the class does not declare, or give a value that does not fit
the parameter's type. That happens when a class is renamed or removed in
the code, or when the group is saved before the code is deployed.

The group's page marks each one, and the groups list shows **Class
mismatch** on the group. These are advice only:

- The group saves as it is. Nothing is removed or changed.
- Nodes are still classified with the class. A node whose catalog needs
  a class that does not exist fails to compile, with `Could not find
  class`, until the code or the group is changed.
- Only types the console can check are checked. A value for any other
  type is never marked.

Nothing is marked as missing when one of the environment's manifests
fails to parse, since the class may be in that manifest. The page lists
the manifests and their errors instead.

### When the list is unavailable

The page says why, and classes and parameters are typed by name. Nothing
is marked, and saving and classification work as normal. The causes:

- **The console has no connection to openvoxserver.** Set
  `CONSOLE_PUPPET_SERVER_URL`.
- **openvoxserver does not allow the console to read classes.** Add the
  rule from the install guide:
  [containers](containers.html) or [packages](packages.html).
- **openvoxserver could not be reached.** If the list was read earlier,
  that earlier list is offered, marked as possibly out of date.

## When groups overlap

A node matching several groups gets everything from all of them: every
class, every parameter. Since a node in a group is also in every group
above it, a group inherits whatever those groups set and it does not.

Where two groups set the same thing differently - the same class
parameter, the same top-scope parameter, or the environment - two rules
decide, in this order:

1. **A group always wins over the groups above it.** A child's value
   beats its parent's, its grandparent's and so on, whatever their
   priorities.
2. Between groups where neither is above the other, the **higher
   priority wins**.

A group also counts as ranking at least as high as its parent. Take a
parent with priority 10, its child with priority 1, and an unrelated
group with priority 5, all matching one node. The child beats its parent
by the first rule, and it beats the unrelated group too, because it
ranks as 10: a refinement of a group outranks whatever that group
outranks.

### When a child overrides a higher priority

If a group sets something differently from a group above it that has a
**higher** priority, the child's value is still the one used, but the
priorities alone would have said otherwise. The console tells you: the
group's page names the parent and the values concerned when you open or
save it, the groups list marks the group **Overrides priority**, and the
change is noted on the **Activity** page. It is a warning, not an error.
Nothing is refused, and nodes are classified as normal. To clear it,
give the child the higher priority or remove the value from one of the
two groups.

Values set on All Nodes never raise it: every group is expected to
override those.

## Set configuration data

**Configuration data** is data your code reads with `lookup()`: a value
for `ntp::servers`, `profile::dns::resolvers` or any other Hiera key.
Setting it on a node group changes it for every node in the group without
a commit or a code deploy.

1. Open the group, and under **Classes** open **Configuration data
   (JSON)**.
2. Type a JSON object of keys and values, for example
   `{"ntp::servers": ["0.pool.ntp.org"], "dns::timeout": 5}`. A value
   can be any JSON: a string, a number, a boolean, a list, an object or
   `null`.
3. Save the group. Nodes pick it up on their next Puppet run.

A key is up to 255 characters of letters, digits and `_ : . -`. A group
holds up to 1000 keys, each value up to 64 KiB. The key `lookup_options`
is refused: it is how Hiera sets merge behavior, which configuration data
does not offer.

### Which value a node gets

Data is combined from every group the node is in by the same two rules
that combine class parameters (see [When groups
overlap](#when-groups-overlap)): a group beats the groups above it, and
between unrelated groups the higher priority wins. A key set by the
winning group replaces the same key from the others **whole**: two groups
that both set `app::opts` to an object do not have their objects merged
key by key.

### Console data beats the control repo

When a key is set both here and in your control repo's Hiera data, the
**console's value is used**. A key the console does not set is found in
the control repo as before. So configuration data is the right place for
a value you want to override per group, and the control repo remains the
place for everything else.

### Not for secrets

Configuration data is stored as plain text and shown to everyone who can
see node groups (`classifier:read`). Do not put passwords or keys in it.
The **Activity** page and the audit log record which keys changed, never
their values.

### If a node's run fails

openvoxserver asks the console for a node's data each time it compiles a
catalog. If the console cannot answer, the compile **fails** with an error
naming the console, instead of building a catalog from missing values. The
install guides show how the server is set up to ask.

## Check what a node gets

A group's page lists the nodes it matches right now. What a node finally
receives is every matching group merged, and the one place that shows
that whole answer is enc-bridge itself: ask it on the openvoxserver host,
as the install guides show, and it prints exactly what openvoxserver is
told. A node's [configuration data](#set-configuration-data) is separate:
`GET /api/v1/enc/<certname>/data`, with the same token, returns it.

Classification takes effect at the node's next Puppet run. To apply it
now, start a run from the group's page, which runs Puppet on every node
the group matches; see [Run jobs](run-jobs.html).

Every change to a group is recorded on the **Activity** page, with who
made it.

## Upgrading from a version without the tree

Upgrading changes no node's classification. Every existing group
becomes a direct child of All Nodes, keeps its rule, pins and priority,
and matches on all of its conditions as before.

If you already had a group called "All Nodes", it is renamed
**All Nodes (previous)** and carries on exactly as it was, with its own
rule. It is not turned into the built-in group, because that would apply
its classes to nodes its rule did not match. Once you have checked it,
you can move its classes onto the built-in All Nodes and delete it.

If you run several console instances, finish upgrading all of them
before you put one group under another. An instance still on the old
version ignores parents and would classify by priority alone. See
[Run more instances](cluster.html).
