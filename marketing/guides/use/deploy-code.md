---
title: Deploy code
summary: Deploy Puppet code from one or more control repositories into the environments openvoxserver compiles from.
order: 2
---

The console deploys Puppet code from control repositories - a Puppetfile
and manifests in git - into the environments directory openvoxserver
reads, using g10k. Each branch of a control repository becomes one
environment.

Viewing deploys needs `code:read`; starting one needs `code:deploy`.

## Turn code deployment on

Code deployment stays off until the console knows where g10k is, what to
deploy, and where to put it:

| Setting | What it is |
| --- | --- |
| `CONSOLE_G10K_BIN_PATH` | the g10k program. The console's container image includes it and sets this already |
| `CONSOLE_CONTROL_REPO_URL` | a single control repository to deploy from |
| `CONSOLE_CODE_SOURCES_PATH` | or, instead, a file declaring several (see below) |
| `CONSOLE_CODE_DIR_PATH` | the directory openvoxserver reads its code from; the console deploys into its `environments` directory |
| `CONSOLE_CODE_WEBHOOK_SECRET` | the secret git host webhooks are signed with |

`CONSOLE_CONTROL_REPO_URL` and `CONSOLE_CODE_SOURCES_PATH` cannot both
be set: the console refuses to start rather than combine them.

The console and openvoxserver must share that directory. With packages
on one host, point `CONSOLE_CODE_DIR_PATH` at `/etc/puppetlabs/code` and
let the console's user write to its `environments` directory.

### With the container stack

The production compose file passes none of these settings through to the
console, and does not give it openvoxserver's code volume. Add both in a
`compose.code.yml` beside it:

```yaml
services:
  console:
    environment:
      CONSOLE_CONTROL_REPO_URL: ${CONSOLE_CONTROL_REPO_URL:?set the control repository}
      CONSOLE_CODE_DIR_PATH: /etc/puppetlabs/code
      CONSOLE_CODE_WEBHOOK_SECRET_FILE: /run/secrets/code_webhook_secret
    volumes:
      - openvoxserver-code:/etc/puppetlabs/code
    secrets:
      - code_webhook_secret

secrets:
  code_webhook_secret:
    file: ./secrets/code_webhook_secret
```

Then pass it after the main file in every command:

```sh
docker compose -f docker-compose.yml -f compose.code.yml up -d console
```

> [!NOTE]
> This combination has not yet been walked through end to end the way
> the install guides have. If a deploy succeeds but openvoxserver does
> not see the code, check that both containers show the same files
> under `/etc/puppetlabs/code/environments`.

## Deploy from several control repositories

To deploy from more than one repository, declare them in a YAML file and
point `CONSOLE_CODE_SOURCES_PATH` at it:

```yaml
sources:
  control:
    remote: git@github.com:org/control-repo.git
    prefix: false        # branch production -> environment production
  team_a:
    remote: git@github.com:org/team-a-control.git
    prefix: true         # branch production -> environment team_a_production
    private_key: /etc/openvox-console/keys/team_a
    webhook_secret_file: /etc/openvox-console/team_a_webhook
```

- A source's name may contain only lower-case letters, digits and `_`,
  because it becomes part of environment names.
- `prefix` decides the environment names a source's branches produce:
  `true` prefixes them with the source's name, `false` or unset leaves
  them bare, and a string is used as the prefix. Two sources may not end
  up with the same prefix, so at most one can be unprefixed; the console
  refuses to start otherwise.
- Each source can have its own deploy key and its own webhook secret.

> [!TIP]
> A new prefixed source deploys successfully and changes nothing, until
> nodes are classified into its new environment names. Nothing moves
> nodes there for you.

## Deploy

Open **Code**. The **Repositories** tab lists each repository with its
environments and last deploy; **Deploy** on a repository deploys its
`production` branch. **Deploy now** on the **Deploy history** tab does
the same for the default repository.

Deploys of the same environment never overlap, however many console
instances run. Each appears in the deploy history, with who started it,
and on the **Activity** page.

## Deploy on every push

To deploy whenever a branch is pushed, add a webhook to the repository
on your git host:

- **URL:** `https://console.example.com/api/v1/code-deploys/webhook`,
  or, for one source among several,
  `https://console.example.com/api/v1/code-deploys/webhook/<source name>`
- **Content type:** JSON
- **Secret:** the webhook secret for that source
- **Events:** pushes

The console checks each delivery's signature (the `X-Hub-Signature-256`
header) against the secret, and deploys the branch that was pushed.

## Read the Repositories tab

Each repository shows two node counts, and the gap between them is the
useful part:

- **Assigned**: nodes classification sends to one of the repository's
  environments.
- **Reporting**: nodes whose latest Puppet run used one of them.

Assigned with nothing reporting means code that has deployed but no node
has run yet. Reporting with nothing assigned means nodes running code no
group sends them to - usually an environment left behind by a removed
source, or a node with an `environment` set in its own `puppet.conf`.

**Size** is the size of the deployed files, not disk use: g10k shares
modules between environments, so repositories' sizes overlap.

> [!WARNING]
> Removing a source from the configuration stops its deploys, but leaves
> its environments on disk, where openvoxserver keeps compiling from
> them. Delete them yourself if that is what you meant.
