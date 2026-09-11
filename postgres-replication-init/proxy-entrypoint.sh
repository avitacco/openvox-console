#!/bin/sh
# A stable TCP address for a client (e.g. the console) to hold a DSN
# against across a Postgres failover. Real deployments get this from a
# VIP, DNS cutover, or a connection pooler (pgbouncer/HAProxy) - this is
# the local-fixture equivalent, existing solely so
# postgres-replication's failover can be tested without the console's
# own CONSOLE_POSTGRES_DSN or process needing to change: only this
# proxy's target is repointed (see operations.md's failover runbook
# verification and Makefile's postgres-replication-failover target).
set -eu
TARGET="${POSTGRES_PROXY_TARGET:-postgres-primary}"
echo "postgres-proxy: forwarding to ${TARGET}:5432"
exec socat -d TCP-LISTEN:5432,fork,reuseaddr "TCP:${TARGET}:5432"
