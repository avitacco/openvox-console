---
title: Users, roles and tokens
summary: Give people access through roles and permissions, sign in with your identity provider, and issue tokens for programs.
order: 6
---

Everything in the console is behind a permission, and every permission
reaches a person through a **role**. Programs, such as enc-bridge, use
**service tokens** instead.

Managing users, roles and tokens needs `rbac:admin`.

## The permissions

| Permission | Allows |
| --- | --- |
| `nodes:read` | seeing nodes, their facts, reports, packages, pending updates and certificate states |
| `nodes:manage` | deleting nodes, and setting how often the console scans for pending updates |
| `nodes:certs:manage` | signing, revoking and cleaning certificates - equivalent to CA admin |
| `classifier:read` | seeing node groups |
| `classifier:write` | creating, changing and deleting node groups |
| `enc:read` | reading classification, as openvoxserver does |
| `code:read` | seeing repositories and deploys |
| `code:deploy` | starting deploys |
| `orchestrator:read` | seeing jobs |
| `orchestrator:run` | starting jobs (Puppet runs, tasks, plans and patch runs), switching a node's package reporting, and scanning nodes for pending updates |
| `vulnerabilities:read` | seeing vulnerability findings |
| `vulnerabilities:manage` | configuring vulnerability providers |
| `agentless:manage` | adding, changing and deleting agentless SSH targets and shared credentials, and trusting their host keys |
| `activity:read` | seeing the activity log |
| `status:read` | seeing the System page |
| `agent-requests:read` | seeing what AI agents have asked to run |
| `agent-requests:approve` | approving and rejecting those requests |
| `rbac:admin` | managing users, roles, service tokens and agent tokens |

> [!NOTE]
> The bootstrap admin gets every permission. A permission added in a
> newer console version is never added to existing roles for you, so
> after an upgrade, grant new ones on the **Roles** page to the roles
> that should have them.

## Give someone access

1. Open **Admin**, then **Roles**, and create a role with the
   permissions the job needs - for example, `nodes:read`,
   `classifier:read` and `orchestrator:read` for someone who watches but
   does not change.
2. Open **Users**, create the user, and assign the role.

A user's permissions are the combination of all their roles. To give
the same roles to a whole team, use a [user group](#give-roles-to-a-group).

## Passwords and lockout

These rules cover accounts that log in with a username and password.
They do not cover people who sign in through your identity provider, or
service tokens. An administrator (`rbac:admin`) sets them under
**Admin**, then **Account policy**. A change applies at once, on every
console instance.

### Password rules

| Rule | Unless changed | Can be |
| --- | --- | --- |
| Minimum length | 12 characters | 8 to 72 |
| Must contain a lowercase letter, an uppercase letter, a digit, a symbol | none required | each on or off |

Two rules cannot be turned off: a password cannot be the account's
username, and cannot be longer than 72 bytes.

A password is checked when it is set: when an administrator creates a
user or sets a user's password, and when someone changes their own
under **Preferences**. Each of those forms lists the rules, and marks
the ones a refused password did not meet.

Passwords that already exist are not checked again. After you tighten
the rules, everyone can still log in; the new rules apply the next time
each password is changed.

### Lockout

After 10 failed password logins in a row, an account is locked for 15
minutes. Both numbers can be changed (3 to 100 logins; 1 to 1,440
minutes), and lockout can be turned off.

- While an account is locked, its password is refused even when it is
  right. Failed logins while it is locked do not make the lock longer.
- The lock ends by itself. A successful login sets the count back to
  zero.
- Failed logins are counted together across all console instances.
- **The login page does not say that an account is locked.** It gives
  the same answer as for a wrong password, because saying "locked"
  would tell an attacker which usernames exist. The message for every
  failed login mentions that accounts are locked after repeated
  attempts.

Lockout is about guessing passwords, so it stops password login only:

- Sessions that are already open carry on.
- Signing in through your identity provider still works.
- Service tokens are not affected.
- Changing your own password asks for the current one. A wrong current
  password counts as a failed login, and the change is refused while
  the account is locked.

### Unlock an account

Open **Admin**, then **Users**. A locked account shows **Locked
until** and the time. Choose **Unlock**, or **Set password** to give
the user a new password, which unlocks the account as well.

If the only administrator is locked out and nobody else can unlock the
account, wait for the lock to end. To stop someone keeping an account
locked by failing logins on purpose, put the console behind something
that limits requests to `/api/v1/auth/login`, or turn lockout off and
rely on that instead.

Locks, unlocks, refused passwords and changes to the policy are written
to the audit log; locks, unlocks and policy changes also appear on the
**Activity** page.

## Give roles to a group

A **user group** is a named set of users. Roles granted to a group are
held by every member, in addition to any roles they hold themselves, so
when a team changes you change the group instead of each account.

1. Open **Admin**, then **User groups**, give the group a name and choose
   **Create group**.
2. Choose **Change** under **Members** and tick the users who belong.
3. Tick the roles the group should give, in the **Roles** column.

A person who belongs to several groups, or who holds a role both
themselves and through a group, has it once. Removing the role from the
person leaves the group's grant, and removing them from the group
leaves what they hold themselves.

The **Users** page shows each person's groups and the roles they hold
through them, such as "Patcher via Web ops". Those cannot be unticked
there: change the group. The **Roles** page shows the groups each role
is granted to.

### Groups and limits

A group adds roles; it does not merge them. Each role still applies on
**its own** node groups and, if it has one, its own
[task list](#limit-a-role-to-some-tasks), as for roles assigned to a
person directly. Someone in a group that grants a role limited to
`patch::*` on the web servers, and who is also assigned a role allowing
every task on the database servers, can run patch tasks on the web
servers and anything on the database servers.

### Deleting and removing

- Deleting a group takes its roles away from its members. When it has
  members the console says how many people are affected before it
  deletes.
- Deleting a user removes them from their groups, and deleting a role
  removes it from the groups that hold it.
- The console will not delete a group, take a role from it or remove a
  member if that would leave nobody with `rbac:admin`.

### When a change takes effect

Joining or leaving a group, and granting or revoking a group's roles,
take effect the next time the person's session is refreshed, within 15
minutes, as any role change does. A [scheduled job](schedule-jobs.html)
is checked against its owner's groups each time it fires.

Every change to a group, its members and its roles is written to the
audit trail under the `rbac` category.

## Limit a role to some of your nodes

A role's permissions apply to **node groups**. A new role is granted on
**All Nodes**, which is the whole fleet. To limit a team to its own
servers, grant the role on that team's group instead:

1. Make sure a node group holds exactly the nodes the team should see -
   see [Classify nodes](classify.html).
2. Open **Admin**, then **Roles**.
3. In the role's **Node groups** column, choose **Change**, clear
   **All Nodes**, and tick the team's group.
4. Choose **Save node groups**.

A grant on a group covers every group beneath it, so a role on
*Linux Servers* also sees the nodes in *Debian Family*. The role's
members get the change the next time their session refreshes, within
15 minutes; a node that moves into or out of the group appears or
disappears within a minute.

Someone limited this way sees only their nodes everywhere node data
appears: the node list and each node's facts, reports, packages and
vulnerabilities, the dashboard figures, the package catalogue, the
vulnerability list, the **Jobs** page, and the node counts on the
**Groups** and **Code** pages. A node outside their groups answers
exactly as a node that does not exist. The **Nodes** page says which
groups they are seeing.

The same groups limit what they can **do**. Running Puppet, a task or a
plan, switching a node's package reporting, upgrading its agent,
deleting it, and signing, revoking or cleaning its certificate all
need the node to be in the groups of a role that grants that
permission. An action that names a node outside them is refused, and
the console says which nodes those were. When an action names several
nodes and one is outside, nothing is done on any of them.

A job's nodes can also be chosen by a query or a node group rather
than by name - see [Run jobs](run-jobs.html). Those are narrowed
instead of refused: someone limited to some node groups gets the
matching nodes that are in their groups, and is told nothing about the
rest. Their queries cannot contain a subquery, which could otherwise be
used to learn about nodes outside their groups.

A [scheduled job](schedule-jobs.html) runs as its owner, with the
owner's roles as they are each time it starts. Removing someone's role
stops their schedules starting anything; narrowing it narrows what
their schedules run on. People see and change their own schedules, and
only a role granted on **All Nodes** sees and changes everyone's.

### See more than you can change

Each permission is limited by the roles that grant it, so seeing and
acting can differ. To let a team see the whole fleet but run Puppet
only on its own servers, give its people two roles:

| Role | Permissions | Node groups |
| --- | --- | --- |
| Fleet readers | `nodes:read`, `classifier:read`, `orchestrator:read` | All Nodes |
| Web operators | `orchestrator:run` | Web Servers |

They see every node and every job, and a run on a database server is
refused.

### What node groups do not limit

- **The Activity page.** It is the audit trail and names every node.
  Give `activity:read` only to people who may know about the whole
  fleet.
- **Node group definitions.** Everyone with `classifier:read` sees
  every group, including the node names pinned to it, and
  `classifier:write` can change any group.
- **Service tokens**, which are always fleet-wide. They can be limited
  to some tasks, as a role can - see
  [Issue a token for a program](#issue-a-token-for-a-program).
- **Saved plans.** Everyone with `orchestrator:read` sees every
  [plan](run-plans.html), and everyone with `orchestrator:run` can
  change any of them. Running one is limited to your nodes.
- **Code deploys**, which are not about a node.

### Things to know

- A node that has never run Puppet has no facts, so it matches no
  group's rule. It is in a team's groups only if it is pinned to one.
  That includes nodes waiting for their certificate to be signed - see
  [Manage node certificates](certificates.html).
- A role with no node groups at all shows a **No node groups** warning.
  Its permissions apply to no node.
- Job history follows a node's current groups. When a node moves to
  another group, its past jobs move out of the old group's view with
  it.

## Limit a role to some tasks

`orchestrator:run` on its own lets someone run **every** task the
console knows about, on the nodes the role covers. To let a team run
the patch tasks and nothing else, give its role a **task allowlist**:

1. Open **Admin**, then **Roles**.
2. In the role's **Tasks** column, choose **Change**.
3. Type one pattern per line, then choose **Save tasks**. The **New
   role** form has the same field.

A pattern is one of:

| Pattern | Allows |
| --- | --- |
| `package::install` | that task only |
| `patch::*` | every task in the `patch` module, and the module's own task `patch` |
| `*` | every task |

Matching is exact and case-sensitive, so `patch::*` does not allow
`patching::apply`. Nothing else is accepted: a pattern with a wildcard
anywhere else, an empty line or a capital letter is refused, and the
form keeps what you typed. A role may hold up to 20 patterns of up to 64
characters.

A role with **no** patterns allows every task. That is how every role
you already have is left, so nothing changes until you add one. The
list only narrows `orchestrator:run`: it never grants anything, and
the role's node groups still decide which nodes.

### How a person's roles combine

Each role's list applies on that role's **own** node groups, not on
all of the person's. Someone with these two roles:

| Role | Node groups | Tasks |
| --- | --- | --- |
| Web patchers | Web Servers | `patch::*` |
| Database operators | Database Servers | *(every task)* |

can run `patch::apply` on a web server and any task on a database
server, and a task such as `package::install` is refused on a web
server. Having the second role does not widen the first.

When a task is started on nodes chosen by a query or a node group, it
runs on the matching nodes the person may run it on and is told nothing
about the rest, as with node groups. A [plan](run-plans.html) is
narrowed to the nodes **every** one of its task steps may run on, so
all its steps still run on the same nodes. A list of named nodes that
includes one the task is not allowed on is refused, naming it.

### What a refusal looks like

Starting a task the person's roles do not allow is refused with `403`
and the task's name; nothing is started. A plan names the step as well,
as `step 2 (package::install)`. A task that does not exist and one the
person may not run are refused alike, so the list does not reveal which
tasks exist. Each refusal is in the audit log as `task.denied`, with the
person, the task and the step, and never the task's parameters.

What the list does **not** limit:

- **Puppet runs**, which are not tasks.
- **Seeing jobs and their history.** It limits what someone may start.
- **People who can only read.** Someone with `orchestrator:read` and no
  `orchestrator:run` sees every task in the task list.

The **Run task** dialog, the plan editor and the schedule form offer
only the tasks the person may run.

### Plans and schedules

- **Saving a [plan](run-plans.html) or a [schedule](schedule-jobs.html)**
  with a task the person may not run is refused, naming the step and the
  task.
- **A plan someone else saved** shows every step to a person with a
  narrower list, with the steps they may not run marked. They cannot run
  it, and cannot save changes to it without removing those steps.
- **A schedule is checked every time it starts**, against its owner's
  roles as they are then. If the owner may no longer run one of its
  tasks, nothing starts and the firing is recorded as skipped, naming the
  task (and the step, for a plan).

### When a change takes effect

A role's list reaches a person's sessions the next time they refresh,
within 15 minutes, like any other permission. A schedule picks it up at
its next start.

> [!NOTE]
> A person's task lists travel in their access token. The limits above
> keep one role to about 1.3 KB at most, and most people carry a few
> short patterns. If you give someone many restricted roles, check that
> the reverse proxy in front of the console accepts request headers
> that large; some allow only 8 KB.

## Sign in with your identity provider

The console can also sign people in through OpenID Connect. Local
sign-in and service tokens keep working alongside it. Register the
console with your provider, with a redirect URL of
`https://console.example.com/api/v1/auth/oidc/callback`, and set:

| Setting | What it is |
| --- | --- |
| `CONSOLE_OIDC_ISSUER` | your provider's issuer URL |
| `CONSOLE_OIDC_CLIENT_ID`, `CONSOLE_OIDC_CLIENT_SECRET` | the console's registration |
| `CONSOLE_OIDC_REDIRECT_URL` | the redirect URL above |
| `CONSOLE_OIDC_SCOPES` | defaults to `openid profile email groups` |
| `CONSOLE_OIDC_USERNAME_CLAIM` | the claim a new user's name comes from; defaults to `email` |
| `CONSOLE_OIDC_ROLE_CLAIM` | the claim listing a person's groups; defaults to `groups` |
| `CONSOLE_OIDC_ROLE_MAPPING` | a JSON object from claim values to role names, such as `{"platform-admins":"admin"}`; each role must already exist, and unmapped values are ignored |

The sign-in page then offers **Log in with SSO**. At every sign-in, the
console gives the person the mapped roles their claims currently name,
and removes mapped roles they no longer name. A role an administrator
assigned by hand is never touched.

> [!WARNING]
> Your provider must put the username and group claims in the ID token
> itself, not only behind its user-info endpoint. Some providers leave
> them out of the ID token by default when they also issue an access
> token, and need a setting to include them.

## Issue a token for a program

A service token has a fixed set of permissions chosen when it is
created, and never expires on its own. Give each program its own, with
only what it needs:

1. Open **Admin**, then **Service tokens**.
2. Under **New service token**, name it and choose its permissions.
3. Optionally, under **Tasks it may run**, list the tasks it may start,
   one pattern per line, as for a [role](#limit-a-role-to-some-tasks).
   Leave it empty to allow every task. The list is fixed when the token
   is created.
4. Choose **Create service token**, and copy the token: it is shown
   once.

Deleting a service token revokes it on every console instance at once.
Signing out revokes the tokens of that session the same way.

To give an AI agent access, use an agent token instead: it expires, is
limited to node groups, and every change it asks for waits for someone's
approval. See [Let AI agents use the console](agents.html).

## Keep an audit trail

Every change made through the console is on the **Activity** page, which
needs `activity:read`. Separately, the console can write an audit log for
a log collector, with one level per category:

| Setting | Category |
| --- | --- |
| `CONSOLE_AUDIT_NODES` | nodes and certificates |
| `CONSOLE_AUDIT_CLASSIFIER` | node groups |
| `CONSOLE_AUDIT_RBAC` | users, user groups, roles and tokens |
| `CONSOLE_AUDIT_AUTH` | sign-ins and sign-outs |
| `CONSOLE_AUDIT_CODE` | code deploys |
| `CONSOLE_AUDIT_ORCHESTRATOR` | jobs, and tasks refused to someone whose roles do not allow them |
| `CONSOLE_AUDIT_VULNERABILITIES` | vulnerability providers |

Each is `off`, `writes` (the default: changes and sign-ins) or `full`
(also every view). Audit events go to the console's own log output unless
`CONSOLE_AUDIT_LOG_PATH` names a file.
