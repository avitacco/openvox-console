---
title: Run modes
summary: What each of the console's run modes serves and runs, and what each one needs configured.
order: 1
---

One image, one binary. `CONSOLE_RUN_MODE` selects which parts of the
console an instance runs, so parts with very different loads can be
scaled and placed separately. Unset, it is `all`: everything in one
instance, which is what a single-instance deployment always runs.

## What each mode runs

| Mode | Serves | Listens | Runs in the background |
| --- | --- | --- | --- |
| `all` (the default) | everything | HTTP, node transport | every background task |
| `web` | the console, its API, code-deploy webhooks and classification | HTTP | nothing |
| `enc` | classification only | HTTP | nothing |
| `orchestrator` | nothing beyond health and metrics | HTTP, node transport | job dispatch, and the first run of new nodes |
| `worker` | nothing beyond health and metrics | HTTP | vulnerability syncs, and cleanup of abandoned jobs |

Every mode serves `/health` and `/metrics`, so any instance can be
checked without knowing its mode, and `/health` reports the mode it is
running.

Two choices in that table are deliberate:

- **`web` answers classification too.** `enc` exists so classification
  *can* run on its own, next to the compilers, not so that it is missing
  everywhere else. A split that quietly stopped classifying nodes would
  be far worse than `web` carrying some of that load.
- **`orchestrator` serves no API.** It holds the node connections and
  sends jobs down them. Starting a job is `web`'s job, and `web` reaches
  those connections through the cluster.

## What each mode needs

Each mode requires only what it uses, and refuses to start without it,
naming the missing setting:

| Mode | Requires |
| --- | --- |
| every mode | `CONSOLE_POSTGRES_DSN` and the four `CONSOLE_OPENVOXDB_*` settings |
| `all`, `web` | `CONSOLE_RBAC_SIGNING_KEY_FILE`: these are the only modes that issue login tokens |
| `enc`, `orchestrator`, `worker` | `CONSOLE_RBAC_VERIFICATION_KEYS_DIR`: they check tokens, but never issue them |
| `orchestrator` | `CONSOLE_NODE_TRANSPORT_ADDR`: without a node listener every job would fail "not connected" on an instance that looked healthy |

### Give non-issuing modes the verification keys

An instance that only checks tokens reads the keys to check them against
from a directory, one file per key, named after the key's ID. A
deployment that has never rotated its key uses the ID `default`, so the
directory holds one file, `default.pem`, which is the signing key file
itself:

```sh
sudo install -d -m 0750 -o root -g openvox-console /etc/openvox-console/rbac-verification-keys
sudo install -m 0640 -o root -g openvox-console \
  rbac-signing-key.pem /etc/openvox-console/rbac-verification-keys/default.pem
```

```ini
CONSOLE_RUN_MODE=enc
CONSOLE_RBAC_VERIFICATION_KEYS_DIR=/etc/openvox-console/rbac-verification-keys
```

> [!WARNING]
> The verification directory currently accepts keys only in the same
> format as the signing key: the private key, of which the console uses
> only the public half. So an instance given the directory also holds a
> key that could sign tokens. Protect it on every host exactly as you
> protect the signing key, including `enc` instances beside the
> compilers.

When you rotate the signing key, every instance's verification directory
needs the new key too; [Rotate the token signing key](rotate-signing-key.html) covers the order.

## Changing an instance's mode

Set `CONSOLE_RUN_MODE` and restart the instance. No data moves: every
mode reads and writes the same database. Setting it back to `all` undoes
the change.

A lone instance can change mode, but a mode other than `all` only makes
sense among clustered instances that between them run every mode. See
[Run more instances](cluster.html).
