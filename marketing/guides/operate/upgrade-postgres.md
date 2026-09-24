---
title: Upgrade the container databases to Postgres 18
summary: Move a container deployment's two databases from Postgres 17 to 18 by dumping and restoring them.
order: 3
---

The production compose file runs both databases on Postgres 18. Postgres
18 images keep their data in a version-numbered directory inside the
volume, so the volumes are mounted at `/var/lib/postgresql`, not
`/var/lib/postgresql/data` as before.

A Postgres 18 container started against a volume still holding Postgres
17 data refuses to start, with an error beginning `Error: in 18+, these
Docker images are configured to store database data in a format which is
compatible with "pg_ctlcluster"`. That is a refusal, not damage: the old
data is untouched. This guide moves it across.

The upgrade is a dump and restore: `pg_upgrade` would need both
versions in one image, and these images carry one.

## Dump both databases

Do this with the Postgres 17 containers still running. Stop what writes
to them, and leave the databases up:

```sh
docker compose -f docker-compose.yml stop console openvoxdb
```

```sh
docker compose -f docker-compose.yml exec -T openvoxdb-postgres \
  pg_dumpall -U openvoxdb > openvoxdb-17.sql
docker compose -f docker-compose.yml exec -T console-postgres \
  pg_dumpall -U console > console-17.sql
```

Check both files are there and not empty **before going on**:

```sh
ls -l openvoxdb-17.sql console-17.sql
```

## Replace the volumes

> [!CAUTION]
> This deletes the Postgres 17 data. From here the two dump files are the
> only copy, which is why the check above comes first.

```sh
docker compose -f docker-compose.yml down
docker volume rm openvox-console_openvoxdb-postgres-data openvox-console_console-postgres-data
```

The volume names follow the project directory's name; check them with
`docker volume ls`.

## Restore on Postgres 18

Start only the databases. They start empty, and openvoxdb's gets the
`pg_trgm` extension it needs from the compose file:

```sh
docker compose -f docker-compose.yml up -d openvoxdb-postgres console-postgres
```

```sh
docker compose -f docker-compose.yml exec -T openvoxdb-postgres \
  psql -U openvoxdb < openvoxdb-17.sql
docker compose -f docker-compose.yml exec -T console-postgres \
  psql -U console < console-17.sql
```

Then start everything else:

```sh
docker compose -f docker-compose.yml up -d
```

## If openvoxdb says pg_trgm is missing

openvoxdb refuses to start without the `pg_trgm` extension. The compose
file only adds it when the database is first created, so a database that
already existed needs it added by hand:

```sh
docker compose -f docker-compose.yml exec openvoxdb-postgres \
  psql -U openvoxdb -c 'CREATE EXTENSION IF NOT EXISTS pg_trgm;'
```

## Staying on Postgres 17

Staying on 17 takes two changes, not one: set
`OPENVOXDB_POSTGRES_VERSION=17-alpine` (and the console's equivalent,
`CONSOLE_POSTGRES_VERSION`), **and** move each volume's mount back to
`/var/lib/postgresql/data`. Changing only the version produces the error
above.
