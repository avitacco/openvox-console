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
#   4. clusters three more instances with it - a worker, an orchestrator
#      and an ENC instance attached as a leaf - so the fleet status
#      screenshot shows a real multi-instance deployment
#   5. captures every declared screenshot
#   6. stops every demo instance
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

# The demo cluster (step 4). The instances peer over mutual TLS, and a
# peer checks the certificate of the one it dials against the name it
# dialled - so CLUSTER_HOST must be a name in the certificate they share.
# The development node transport certificate carries "localhost".
CLUSTER_HOST=${CLUSTER_HOST:-localhost}
CLUSTER_ROUTE_PORT=${CLUSTER_ROUTE_PORT:-16222}
CLUSTER_LEAF_PORT=${CLUSTER_LEAF_PORT:-16229}
WORKER_HTTP_PORT=${WORKER_HTTP_PORT:-8091}
WORKER_ROUTE_PORT=${WORKER_ROUTE_PORT:-16223}
ORCH_HTTP_PORT=${ORCH_HTTP_PORT:-8092}
ORCH_ROUTE_PORT=${ORCH_ROUTE_PORT:-16224}
ORCH_TRANSPORT_PORT=${ORCH_TRANSPORT_PORT:-8144}
# marketing/shots' fleet-status shot looks for this address on the page,
# as proof the leaf attached - change both together.
ENC_HTTP_PORT=${ENC_HTTP_PORT:-8093}

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

# Throwaway credentials for the demo cluster. Two secrets because the
# console refuses a leaf secret equal to the cluster secret.
CLUSTER_SECRET=$(openssl rand -hex 24)
CLUSTER_LEAF_SECRET=$(openssl rand -hex 24)
CLUSTER_TLS_CERT=${CONSOLE_NODE_TRANSPORT_CERT_FILE:-$PWD/certs/node-transport-cert.pem}
CLUSTER_TLS_KEY=${CONSOLE_NODE_TRANSPORT_KEY_FILE:-$PWD/certs/node-transport-key.pem}
CLUSTER_TLS_CA=${CONSOLE_NODE_TRANSPORT_CA_FILE:-$PWD/certs/ca.pem}

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
CONSOLE_CLUSTER_ADDR="${CLUSTER_HOST}:${CLUSTER_ROUTE_PORT}" \
CONSOLE_CLUSTER_LEAF_ADDR="${CLUSTER_HOST}:${CLUSTER_LEAF_PORT}" \
CONSOLE_CLUSTER_SECRET="${CLUSTER_SECRET}" \
CONSOLE_CLUSTER_LEAF_SECRET="${CLUSTER_LEAF_SECRET}" \
CONSOLE_CLUSTER_TLS_CERT_FILE="${CLUSTER_TLS_CERT}" \
CONSOLE_CLUSTER_TLS_KEY_FILE="${CLUSTER_TLS_KEY}" \
CONSOLE_CLUSTER_TLS_CA_FILE="${CLUSTER_TLS_CA}" \
  ./bin/console >"$DEMO_LOG" 2>&1 &
DEMO_PID=$!
EXTRA_PIDS=()

# Stop the demo console however this script ends, including on failure
# or interrupt - a stray console holding a port is a confusing thing to
# leave behind.
cleanup() {
  local status=$?
  local pid
  for pid in "${EXTRA_PIDS[@]}" "$DEMO_PID"; do
    if kill -0 "$pid" 2>/dev/null; then
      kill "$pid" 2>/dev/null || true
      wait "$pid" 2>/dev/null || true
    fi
  done
  rm -rf "${VERIFY_KEYS_DIR:-}"
  # The log is only worth keeping when something went wrong.
  if [ "$status" -eq 0 ]; then
    rm -f "$DEMO_LOG"
  else
    echo "The demo console's log is at $DEMO_LOG"
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

# --- 4. a demo cluster -----------------------------------------------
# Three more instances, joined to the demo console, so the fleet status
# page has a real deployment to show: a worker and an orchestrator as
# routed peers, and an ENC instance attached as a leaf, the way one
# beside a compiler would be. Started after seeding, so the database is
# already migrated and populated.
#
# The non-issuing modes verify tokens against a directory of keys named
# by key ID; a copy of the signing key under its ID is that directory.
VERIFY_KEYS_DIR=$(mktemp -d -t openvox-demo-keys-XXXXXX)
cp "${CONSOLE_RBAC_SIGNING_KEY_FILE:-$PWD/certs/rbac-signing-key.pem}" \
  "${VERIFY_KEYS_DIR}/${CONSOLE_RBAC_SIGNING_KEY_ID:-default}.pem"

# start_instance <name> <http port> <env...>: starts one more demo
# instance against the demo database, logging beside the demo console's.
start_instance() {
  local name=$1 port=$2
  shift 2
  env \
    CONSOLE_HTTP_ADDR=":${port}" \
    CONSOLE_BASE_URL="http://localhost:${DEMO_HTTP_PORT}" \
    CONSOLE_POSTGRES_DSN="postgres://${POSTGRES_USER}:console@localhost:5432/${DEMO_DB}?sslmode=disable" \
    CONSOLE_RBAC_VERIFICATION_KEYS_DIR="${VERIFY_KEYS_DIR}" \
    CONSOLE_CLUSTER_TLS_CERT_FILE="${CLUSTER_TLS_CERT}" \
    CONSOLE_CLUSTER_TLS_KEY_FILE="${CLUSTER_TLS_KEY}" \
    CONSOLE_CLUSTER_TLS_CA_FILE="${CLUSTER_TLS_CA}" \
    CONSOLE_CODE_SOURCES_PATH="${CODE_SOURCES_PATH}" \
    CONSOLE_G10K_BIN_PATH="${G10K_BIN_PATH}" \
    CONSOLE_CODE_DIR_PATH="${CODE_DIR_PATH}" \
    CONSOLE_CONTROL_REPO_URL="" \
    CONSOLE_OIDC_ISSUER="" \
    CONSOLE_NODE_TRANSPORT_ADDR="" \
    CONSOLE_CLUSTER_ADDR="" CONSOLE_CLUSTER_LEAF_ADDR="" CONSOLE_CLUSTER_PEERS="" \
    CONSOLE_CLUSTER_SECRET="" CONSOLE_CLUSTER_LEAF_SECRET="" CONSOLE_CLUSTER_MODE="" \
    "$@" \
    ./bin/console >"${DEMO_LOG%.log}-${name}.log" 2>&1 &
  EXTRA_PIDS+=($!)
  local pid=$!

  printf '   %s on :%s' "$name" "$port"
  for _ in $(seq 1 60); do
    # Any answer from /health means it is up; its dependencies' health
    # is the page's business, not this script's.
    if curl -s -o /dev/null "http://localhost:${port}/health" 2>/dev/null; then
      echo " - up"
      return 0
    fi
    if ! kill -0 "$pid" 2>/dev/null; then
      echo
      echo "The ${name} instance exited during startup. Its log:"
      tail -20 "${DEMO_LOG%.log}-${name}.log"
      exit 1
    fi
    sleep 1
  done
  echo
  echo "The ${name} instance did not come up. Its log:"
  tail -20 "${DEMO_LOG%.log}-${name}.log"
  exit 1
}

log "Starting a demo cluster"
start_instance worker "$WORKER_HTTP_PORT" \
  CONSOLE_RUN_MODE=worker \
  CONSOLE_CLUSTER_ADDR="${CLUSTER_HOST}:${WORKER_ROUTE_PORT}" \
  CONSOLE_CLUSTER_PEERS="${CLUSTER_HOST}:${CLUSTER_ROUTE_PORT}" \
  CONSOLE_CLUSTER_SECRET="${CLUSTER_SECRET}"
start_instance orchestrator "$ORCH_HTTP_PORT" \
  CONSOLE_RUN_MODE=orchestrator \
  CONSOLE_NODE_TRANSPORT_ADDR=":${ORCH_TRANSPORT_PORT}" \
  CONSOLE_NODE_TRANSPORT_PUBLIC_ADDR="localhost:${ORCH_TRANSPORT_PORT}" \
  CONSOLE_CLUSTER_ADDR="${CLUSTER_HOST}:${ORCH_ROUTE_PORT}" \
  CONSOLE_CLUSTER_PEERS="${CLUSTER_HOST}:${CLUSTER_ROUTE_PORT}" \
  CONSOLE_CLUSTER_SECRET="${CLUSTER_SECRET}"
start_instance enc "$ENC_HTTP_PORT" \
  CONSOLE_RUN_MODE=enc \
  CONSOLE_CLUSTER_MODE=leaf \
  CONSOLE_CLUSTER_PEERS="${CLUSTER_HOST}:${CLUSTER_LEAF_PORT}" \
  CONSOLE_CLUSTER_LEAF_SECRET="${CLUSTER_LEAF_SECRET}"

# Routes and the leaf connect in the background after each instance is
# up. The status page asks the cluster once, when it loads, so capture
# must not start until every instance answers: poll the same API the page
# uses until it reports all four and nothing missing.
printf '   waiting for all four to answer'
TOKEN=$(curl -fsS -X POST "http://localhost:${DEMO_HTTP_PORT}/api/v1/auth/login" \
  -H 'Content-Type: application/json' \
  -d "{\"username\":\"${DEMO_ADMIN_USER}\",\"password\":\"${DEMO_ADMIN_PASSWORD}\"}" |
  sed -n 's/.*"accessToken":"\([^"]*\)".*/\1/p')
cluster_ready=
for _ in $(seq 1 60); do
  STATUS=$(curl -fsS -H "Authorization: Bearer ${TOKEN}" \
    "http://localhost:${DEMO_HTTP_PORT}/api/v1/status" 2>/dev/null || true)
  if printf '%s' "$STATUS" | grep -q '"replied":4' &&
    printf '%s' "$STATUS" | grep -q '"incomplete":false'; then
    cluster_ready=1
    break
  fi
  printf '.'
  sleep 2
done
if [ -z "$cluster_ready" ]; then
  echo
  echo "The demo cluster never answered as four complete instances. Last status:"
  echo "$STATUS"
  exit 1
fi
echo " - ready"

# --- 5. capture ------------------------------------------------------
# One pass per locale, against the same seeded console. The language is
# a browser-side preference the capture tool writes into localStorage,
# so nothing about the console or its data changes between passes -
# only what the page renders.
#
# English first and unsuffixed, keeping the filenames the committed
# screenshots already have.
CAPTURE_LOCALES="${CAPTURE_LOCALES:-en zh hi es fr de ja ar}"

for locale in $CAPTURE_LOCALES; do
  log "Capturing screenshots (${locale})"
  CONSOLE_BOOTSTRAP_ADMIN_USERNAME="${DEMO_ADMIN_USER}" \
  CONSOLE_BOOTSTRAP_ADMIN_PASSWORD="${DEMO_ADMIN_PASSWORD}" \
    go run ./marketing/capture \
      --console-url "http://localhost:${DEMO_HTTP_PORT}" \
      --browser-console-url "${BROWSER_CONSOLE_URL}" \
      --locale "${locale}"
done

log "Screenshots captured"
echo "The demo instances have been stopped. \`make marketing\` removes ${DEMO_DB} and the demo"
echo "fleet next; run on its own, this leaves them for inspection until the next run."
