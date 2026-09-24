---
title: Users, roles and tokens
summary: Give people access through roles and permissions, sign in with your identity provider, and issue tokens for programs.
order: 4
---

Everything in the console is behind a permission, and every permission
reaches a person through a **role**. Programs, such as enc-bridge, use
**service tokens** instead.

Managing users, roles and tokens needs `rbac:admin`.

## The permissions

| Permission | Allows |
| --- | --- |
| `nodes:read` | seeing nodes, their facts, reports, packages and certificate states |
| `nodes:manage` | deleting nodes |
| `nodes:certs:manage` | signing, revoking and cleaning certificates - equivalent to CA admin |
| `classifier:read` | seeing node groups |
| `classifier:write` | creating, changing and deleting node groups |
| `enc:read` | reading classification, as openvoxserver does |
| `code:read` | seeing repositories and deploys |
| `code:deploy` | starting deploys |
| `orchestrator:read` | seeing jobs |
| `orchestrator:run` | starting jobs, and switching a node's package reporting |
| `vulnerabilities:read` | seeing vulnerability findings |
| `vulnerabilities:manage` | configuring vulnerability providers |
| `activity:read` | seeing the activity log |
| `status:read` | seeing the System page |
| `rbac:admin` | managing users, roles and service tokens |

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

A user's permissions are the combination of all their roles.

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
3. Choose **Create service token**, and copy the token: it is shown
   once.

Deleting a service token revokes it on every console instance at once.
Signing out revokes the tokens of that session the same way.

## Keep an audit trail

Every change made through the console is on the **Activity** page, which
needs `activity:read`. Separately, the console can write an audit log for
a log collector, with one level per category:

| Setting | Category |
| --- | --- |
| `CONSOLE_AUDIT_NODES` | nodes and certificates |
| `CONSOLE_AUDIT_CLASSIFIER` | node groups |
| `CONSOLE_AUDIT_RBAC` | users, roles and tokens |
| `CONSOLE_AUDIT_AUTH` | sign-ins and sign-outs |
| `CONSOLE_AUDIT_CODE` | code deploys |
| `CONSOLE_AUDIT_ORCHESTRATOR` | jobs |
| `CONSOLE_AUDIT_VULNERABILITIES` | vulnerability providers |

Each is `off`, `writes` (the default: changes and sign-ins) or `full`
(also every view). Audit events go to the console's own log output unless
`CONSOLE_AUDIT_LOG_PATH` names a file.
