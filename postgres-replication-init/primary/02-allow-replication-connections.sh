#!/bin/bash
# Runs once, on the primary's first startup against an empty data
# directory (standard docker-entrypoint-initdb.d behavior) - lets the
# standby's pg_basebackup/streaming replication connection authenticate
# as the "replicator" role created in 01-create-replication-role.sql.
# Scoped to the compose network only (this fixture is dev/test-only, see
# docker-compose.yml's "postgres-replication" profile), not a real
# production pg_hba.conf.
set -euo pipefail
echo "host replication replicator all md5" >> "$PGDATA/pg_hba.conf"
