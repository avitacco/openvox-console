#!/usr/bin/env bash
# Refreshes every marketing screenshot.
#
# Screenshots must come from a console holding nothing but the demo
# fleet. A development console accumulates job rows, deploys and user
# accounts from test runs and day-to-day work, and those outrank the demo
# data in every "newest first" list - so a capture against it publishes
# somebody's leftover test fixtures.
#
# Rather than asking a developer to wipe their own console, this runs a
# second console of its own, against its own database, on its own port,
# and captures from that. The developer's console is never touched and
# does not even need to be running.
#
# What it does, in order:
#   1. creates the demo database (dropping any previous one)
#   2. starts a console against it on its own port
#   3. seeds it with the demo fleet
#   4. captures every declared screenshot
#   5. stops the demo console
#
# Everything it creates is disposable. Nothing it does writes to the
# development console's database.
set -euo pipefail
cd "$(dirname "$0")/.."

# --- configuration ---------------------------------------------------
# Ports are deliberately not the console's usual ones, so this can run
# while a development console is up.
DEMO_DB=${DEMO_DB:-console_demo}
DEMO_HTTP_PORT=${DEMO_HTTP_PORT:-8090}
DEMO_TRANSPORT_PORT=${DEMO_TRANSPORT_PORT:-8143}
DEMO_ADMIN_USER=${DEMO_ADMIN_USER:-admin}
DEMO_ADMIN_PASSWORD=${DEMO_ADMIN_PASSWORD:-demo-only-not-a-real-password}

# The browser runs in a container, so it reaches a host-run console by
# the gateway name rather than localhost.
BROWSER_CONSOLE_URL=${BROWSER_CONSOLE_URL:-http://host.docker.internal:${DEMO_HTTP_PORT}}

POSTGRES_CONTAINER=${POSTGRES_CONTAINER:-enterprise-console-console-postgres-1}
POSTGRES_USER=${POSTGRES_USER:-console}

# Code deployment is what the Code page screenshots, and it only has
# anything to show if the demo console knows where to deploy from. The
# fixture repositories `make code-sources-fixture` creates are the ones
# the seed deploys, so point at them unless told otherwise.
CODE_SOURCES_PATH=${CONSOLE_CODE_SOURCES_PATH:-$PWD/.dev/code-sources.yaml}
# g10k is what actually performs a deploy; without it the console reports
# code deployment as unconfigured however many sources are declared.
# `make g10k-install` puts it here.
G10K_BIN_PATH=${CONSOLE_G10K_BIN_PATH:-$PWD/bin/g10k}
# Deploy into a directory of its own rather than the one the development
# stack bind-mounts into openvoxserver. The screenshots show deploy
# history, not served code, so there is nothing to gain from writing over
# what a developer has deployed.
CODE_DIR_PATH=${CONSOLE_CODE_DIR_PATH:-$PWD/.dev/demo-code}

log() { printf '\n== %s\n' "$1"; }

# --- 1. a database of its own ----------------------------------------
log "Creating the ${DEMO_DB} database"
# Dropped and recreated every run: the whole point is starting from
# nothing, and a leftover database from a previous refresh would
# reintroduce exactly the staleness this script exists to avoid.
docker exec "$POSTGRES_CONTAINER" psql -U "$POSTGRES_USER" -d postgres \
  -c "DROP DATABASE IF EXISTS ${DEMO_DB} WITH (FORCE);" \
  -c "CREATE DATABASE ${DEMO_DB};" >/dev/null
echo "   ${DEMO_DB} created"

# --- 2. a console of its own -----------------------------------------
log "Starting a demo console on :${DEMO_HTTP_PORT}"
make build >/dev/null

# Read the dev .env for the things that are genuinely shared - the
# openvoxdb client certificate, the RBAC signing key - then override
# everything that must not be.
set -a
# shellcheck disable=SC1091
[ -f .env ] && . ./.env
set +a

DEMO_LOG=$(mktemp -t openvox-demo-console-XXXXXX.log)

CONSOLE_HTTP_ADDR=":${DEMO_HTTP_PORT}" \
CONSOLE_BASE_URL="http://localhost:${DEMO_HTTP_PORT}" \
CONSOLE_POSTGRES_DSN="postgres://${POSTGRES_USER}:console@localhost:5432/${DEMO_DB}?sslmode=disable" \
CONSOLE_NODE_TRANSPORT_ADDR=":${DEMO_TRANSPORT_PORT}" \
CONSOLE_NODE_TRANSPORT_PUBLIC_ADDR="localhost:${DEMO_TRANSPORT_PORT}" \
CONSOLE_BOOTSTRAP_ADMIN_USERNAME="${DEMO_ADMIN_USER}" \
CONSOLE_BOOTSTRAP_ADMIN_PASSWORD="${DEMO_ADMIN_PASSWORD}" \
CONSOLE_CODE_SOURCES_PATH="${CODE_SOURCES_PATH}" \
CONSOLE_G10K_BIN_PATH="${G10K_BIN_PATH}" \
CONSOLE_CODE_DIR_PATH="${CODE_DIR_PATH}" \
CONSOLE_CONTROL_REPO_URL="" \
CONSOLE_OIDC_ISSUER="" \
  ./bin/console >"$DEMO_LOG" 2>&1 &
DEMO_PID=$!

# Stop the demo console however this script ends, including on failure
# or interrupt - a stray console holding a port is a confusing thing to
# leave behind.
cleanup() {
  if kill -0 "$DEMO_PID" 2>/dev/null; then
    kill "$DEMO_PID" 2>/dev/null || true
    wait "$DEMO_PID" 2>/dev/null || true
  fi
}
trap cleanup EXIT INT TERM

printf '   waiting for it to come up'
for _ in $(seq 1 60); do
  if curl -fsS -o /dev/null "http://localhost:${DEMO_HTTP_PORT}/" 2>/dev/null; then
    echo " - ready"
    break
  fi
  if ! kill -0 "$DEMO_PID" 2>/dev/null; then
    echo
    echo "The demo console exited during startup. Its log:"
    tail -20 "$DEMO_LOG"
    exit 1
  fi
  printf '.'
  sleep 2
done

if ! curl -fsS -o /dev/null "http://localhost:${DEMO_HTTP_PORT}/" 2>/dev/null; then
  echo
  echo "The demo console did not become ready. Its log:"
  tail -20 "$DEMO_LOG"
  exit 1
fi

# --- 3. seed it ------------------------------------------------------
log "Seeding the demo fleet"
CONSOLE_BOOTSTRAP_ADMIN_USERNAME="${DEMO_ADMIN_USER}" \
CONSOLE_BOOTSTRAP_ADMIN_PASSWORD="${DEMO_ADMIN_PASSWORD}" \
CONSOLE_POSTGRES_DSN="postgres://${POSTGRES_USER}:console@localhost:5432/${DEMO_DB}?sslmode=disable" \
  go run ./cmd/demo-seed \
    --console-url "http://localhost:${DEMO_HTTP_PORT}" \
    --reset \
    --i-know-this-is-a-demo-console

# --- 4. capture ------------------------------------------------------
log "Capturing screenshots"
CONSOLE_BOOTSTRAP_ADMIN_USERNAME="${DEMO_ADMIN_USER}" \
CONSOLE_BOOTSTRAP_ADMIN_PASSWORD="${DEMO_ADMIN_PASSWORD}" \
  go run ./marketing/capture \
    --console-url "http://localhost:${DEMO_HTTP_PORT}" \
    --browser-console-url "${BROWSER_CONSOLE_URL}"

log "Done"
echo "The demo console has been stopped; ${DEMO_DB} is left in place for inspection"
echo "and dropped at the start of the next run."
