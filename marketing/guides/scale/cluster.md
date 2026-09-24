---
title: Run more instances
summary: Cluster a second console instance, split instances into run modes, and place classification next to the compilers.
order: 2
---

Several console instances share one database and join one cluster. The
cluster is how they tell each other about things that must reach every
instance at once - a revoked login, a revoked node certificate - and how
a job started on one instance reaches a node connected to another.

Everything durable lives in Postgres, so any instance can serve any
request once they are clustered, and a load balancer can send requests to
whichever is healthy.

> [!CAUTION]
> Instances sharing a database must be clustered. An instance that is
> not only learns of a revoked login when it next re-reads revocations
> from the database, up to 30 seconds later, and cannot reach the nodes
> connected to any other instance.

## How instances find each other

Each instance runs an embedded NATS server. The **core** modes (`all`,
`web`, `orchestrator` and `worker`) connect to each other directly, as
routed peers. `enc` instances instead attach as **leaves**: the
connection runs outward only, from the `enc` instance to a core one, so
the core never needs a way into the network where the compilers are.

```text
  web-1 --+
  web-2 --+-- routed peers (:6222)
  orch-1 -+
  worker -+
      ^
      | leaf connections, outbound only
      | (to a core instance's leaf listener, :6223)
      |
  enc-1, enc-2  (beside the compilers)
```

A leaf carries the console's own messages between instances, such as
revocations and status, and never node traffic: an `enc` instance can
never reach a managed node.

## Before you start

Every peer connection is encrypted and authenticated in two ways:

- **mutual TLS**, with each instance's certificate from the CA. By
  default an instance uses the same certificate it uses for openvoxdb,
  the one from `CONSOLE_OPENVOXDB_CERT_FILE`, so there is nothing new to
  issue. A peer that dials an instance checks that its certificate is
  valid for the name it dialled, so each instance must be reached by a
  name its certificate carries.
- **a shared secret**, the same on every core instance. A managed node
  also holds a certificate from the CA, and the secret is what keeps its
  certificate from being enough to join.

Leaves use a second secret of their own, which must differ from the
cluster secret: an `enc` host is the least trusted place a console runs,
and must not be able to join as a full peer.

Generate both secrets, and keep them out of the process environment with
the `_FILE` form of each setting:

```sh
openssl rand -base64 32 > cluster-secret
openssl rand -base64 32 > cluster-leaf-secret
```

## Add a second instance

Start with two `all` instances. That exercises clustering before any role
is split out.

On the **first** instance, open a peer listener and restart it:

```ini
CONSOLE_CLUSTER_ADDR=:6222
CONSOLE_CLUSTER_SECRET_FILE=/etc/openvox-console/cluster-secret
```

On the **second**, give it the same database, the same token signing key
and the same secrets, its own peer listener, and the first instance as a
peer, by a name in the first instance's certificate:

```ini
CONSOLE_CLUSTER_ADDR=:6222
CONSOLE_CLUSTER_PEERS=console-1.example.com:6222
CONSOLE_CLUSTER_SECRET_FILE=/etc/openvox-console/cluster-secret
```

Only one side needs to name the other. Every instance that joins later
names any existing one, and the cluster connects the rest.

The second instance also needs its own node listener and node transport
certificate if nodes will connect to it, and every instance needs the
same `CONSOLE_BASE_URL`, so links and install scripts point through the
load balancer.

> [!NOTE]
> Instances learn about each other's addresses from the cluster itself,
> and by default those are IP addresses, which a certificate rarely
> carries. With three or more routed instances, each with its own
> certificate, set `CONSOLE_CLUSTER_ADVERTISE` on each to a name in its
> own certificate, such as `console-2.example.com:6222`.

### Check the cluster formed

Each instance logs `cluster joined` as it starts, with the peers it was
given. Then, as a user with the `status:read` permission, open
**System** in the console. The **Fleet Status** tab lists every instance
that answered, grouped by mode; both instances appear there.

> [!TIP]
> `status:read` is not added to existing roles automatically. Grant it to
> the roles that should see the page, on the **Roles** page.

## Split instances into modes

Once two `all` instances run together, split roles out as load demands:
background work to `worker`, node connections to `orchestrator`,
everything a person uses to `web`. Set `CONSOLE_RUN_MODE` on each
instance and restart it; see [Run modes](run-modes.html) for what each
mode needs, including the verification keys for modes that do not issue
tokens.

Any mode can run as several instances:

- work that must happen once, such as a vulnerability sync, a code deploy
  of one environment, or cleaning up abandoned jobs, is coordinated
  through the database, so two `worker` instances never both do it;
- revocations reach every instance, so each one's view stays current;
- activity is recorded once, by the instance where it happened.

Point the load balancer at the `web` instances for the console and its
API, and point nodes at the `orchestrator` instances' node transport
through `CONSOLE_NODE_TRANSPORT_PUBLIC_ADDR`.

## Put classification beside the compilers

`enc` instances answer classification for the compilers they sit beside,
and attach to the cluster as leaves.

On **one or more core instances**, open a leaf listener, protected by
the leaf secret:

```ini
CONSOLE_CLUSTER_LEAF_ADDR=:6223
CONSOLE_CLUSTER_LEAF_SECRET_FILE=/etc/openvox-console/cluster-leaf-secret
```

On each **`enc` instance**, attach to a core instance's leaf listener.
It holds the leaf secret only, never the cluster secret:

```ini
CONSOLE_RUN_MODE=enc
CONSOLE_CLUSTER_MODE=leaf
CONSOLE_CLUSTER_PEERS=console-1.example.com:6223
CONSOLE_CLUSTER_LEAF_SECRET_FILE=/etc/openvox-console/cluster-leaf-secret
CONSOLE_RBAC_VERIFICATION_KEYS_DIR=/etc/openvox-console/rbac-verification-keys
```

Then point each compiler's `enc-bridge` at the `enc` instance beside it,
with `ENC_BRIDGE_URL`.

## Check the whole fleet

The **Fleet Status** tab gathers its picture live, asking every instance
and collecting what answers within one second. Two things on it are worth
knowing:

- If fewer instances answered than should have, it says so above the
  list, for example "1 of 4 instances did not reply within 1s". Take that
  seriously: an instance that is running but cannot reach the others is
  usually one that is stuck.
- Each dependency, such as openvoxdb or the CA, is shown as the serving
  instance sees it. One that instances disagree about is marked
  **differs by instance**, which means some instances can reach it and
  others cannot.

The page does not refresh itself; reload it for a fresh picture.

### Watch for dropped messages

`console_nats_async_errors_total`, on each instance's `/metrics`, counts
problems on its cluster connections. The one that matters is a
subscriber falling behind, which drops messages; each is also logged at
error level as "NATS subscriber fell behind; messages were dropped". A
rising count deserves a look.

## Undo it

Every step can be undone: set `CONSOLE_RUN_MODE=all` and remove the
cluster settings. Nothing in the database depends on the split.
