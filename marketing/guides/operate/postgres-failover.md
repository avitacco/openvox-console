---
title: Fail over to a standby database
summary: Recover from losing the console's database by promoting a streaming-replication standby.
order: 1
---

The console keeps everything durable in its Postgres database, and
nothing in the console itself needs recovering. So losing the primary
database is recovered by promoting a standby and pointing the console at
it. This runbook assumes a standby kept current by Postgres's own
streaming replication.

## Check the standby before you need it

On the standby:

```sh
psql -U console -d console -c "SELECT pg_is_in_recovery();"
```

It answers `t`: it is still a replica. A write to it is refused with
`cannot execute INSERT in a read-only transaction`, which is also what a
healthy replica does.

## Promote the standby

```sh
psql -U console -d console -c "SELECT pg_promote();"
```

Within a few seconds `pg_is_in_recovery()` answers `f`: the standby is
now an independent, writable database.

> [!WARNING]
> The promoted database does not resume replicating from the old
> primary if that comes back. Treat the old primary as retired from this
> point. To have a standby again, rebuild the old primary as a new
> standby of the promoted one, with `pg_basebackup -R`.

## Point the console at it

Change `CONSOLE_POSTGRES_DSN` to the promoted database's address, and
restart the console. The console opens its connections once, at startup,
so a restart is required. With several instances, restart every one.

> [!TIP]
> A connection pooler or proxy in front of the database gives the
> console one address that survives a failover. Then only the proxy is
> repointed, and the console's own configuration never changes.

## Confirm the console recovered

`/health` reports Postgres as `ok` again. Then log in: that writes to the
database, so it proves writes work, not only reads.
