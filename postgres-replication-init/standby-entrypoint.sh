#!/bin/bash
# Custom entrypoint for the standby side of the postgres-replication
# compose fixture (see docker-compose.yml). The official postgres image's
# own entrypoint only knows how to initialize a fresh, standalone
# database (initdb) - a streaming-replication standby needs its data
# directory populated by pg_basebackup FROM the primary instead, with
# standby.signal + primary_conninfo written automatically via
# pg_basebackup's -R flag (the modern, PG12+ way - no recovery.conf).
# This only runs once: on every subsequent container start, PGDATA is
# already populated and this script just starts postgres normally,
# exactly like the official entrypoint would.
set -euo pipefail

if [ -z "$(ls -A "$PGDATA" 2>/dev/null)" ]; then
  echo "standby: PGDATA is empty - waiting for the primary to accept connections..."
  until pg_isready -h postgres-primary -U console -q; do
    sleep 1
  done

  echo "standby: taking a base backup from the primary..."
  PGPASSWORD=replicator pg_basebackup \
    -h postgres-primary \
    -U replicator \
    -D "$PGDATA" \
    -Fp -Xs -P -R
  # This script runs as root, so pg_basebackup implicitly creates $PGDATA
  # AND its parent (e.g. /var/lib/postgresql/18) as root:root - chown just
  # $PGDATA isn't enough, since docker-entrypoint.sh below drops to the
  # postgres user internally and needs to traverse the parent too.
  chown -R postgres:postgres "$(dirname "$PGDATA")"
  chmod 0700 "$PGDATA"
  echo "standby: base backup complete, starting as a streaming replica."
fi

exec docker-entrypoint.sh postgres
