#!/usr/bin/env bash
# The whole marketing site refresh, in one command: `make marketing`.
#
#   1. starts whatever the screenshots need that is not already running -
#      the development stack with openvoxdb's retention off, and the
#      headless browser - after the one-time fixtures, if they are missing
#   2. captures every screenshot, in every locale (marketing/screenshots.sh)
#   3. rebuilds the site into docs/ (marketing/build.sh)
#   4. puts everything back the way it found it, whether the run
#      succeeded or not
#
# "The way it found it" is the point of step 4. Containers this started
# are stopped and removed; services that were already running are left
# running, on their normal configuration again (openvoxdb back on its
# usual retention, the headless browser gone). Everything the run itself
# created is removed: the demo fleet in openvoxdb, the demo console's
# database, and its deploy directory. Set KEEP_DEMO=1 to keep those for a
# look afterwards.
#
# Named volumes are never removed. They hold the development stack's CA,
# which the certificates in certs/ - and .env - were issued from; removing
# them would leave the development stack unable to start.
#
# To rebuild the site from the screenshots already committed, without any
# of this, use `make marketing-build`.
set -euo pipefail
cd "$(dirname "$0")/.."

DEV=(docker compose -f docker-compose.yml -f docker-compose.override.yml)
SHOTS=("${DEV[@]}" -f docker-compose.screenshots.yml)
DEMO_DB=${DEMO_DB:-console_demo}
POSTGRES_CONTAINER=${POSTGRES_CONTAINER:-enterprise-console-console-postgres-1}

log() { printf '\n==> %s\n' "$1"; }
die() { printf '\nmake marketing: %s\n' "$1" >&2; exit 1; }

# --- preflight -------------------------------------------------------
# Checked before anything starts, so a missing prerequisite costs nothing
# and leaves nothing to clean up.
docker info >/dev/null 2>&1 ||
  die "Docker is not reachable. Start the Docker daemon and run it again."
[ -f .env ] ||
  die "no .env. The screenshots run against the development stack - set that up first (see README.md, \"Local development\")."
[ -f certs/console-cert.pem ] && [ -f certs/rbac-signing-key.pem ] ||
  die "certs/ is missing the development credentials (console-cert.pem, rbac-signing-key.pem) - see README.md, \"Local development\"."

# One-time fixtures, made on first use rather than left for somebody to
# remember: the Code page only has deploys to show with both.
if [ ! -x bin/g10k ]; then
  log "Installing g10k (one-time)"
  make g10k-install
fi
if [ ! -f .dev/code-sources.yaml ]; then
  log "Creating the control repository fixtures (one-time)"
  make code-sources-fixture
fi

# --- what is already running ----------------------------------------
# Recorded before anything starts, so cleanup can tell what this run
# started from what it merely borrowed. The screenshot files' view, so a
# stack already in screenshot mode (headless-chrome up) is recognised.
mapfile -t BEFORE < <("${SHOTS[@]}" ps --status running --services 2>/dev/null || true)
was_running() {
  local s
  for s in "${BEFORE[@]}"; do [ "$s" = "$1" ] && return 0; done
  return 1
}

cleanup() {
  local status=$?
  set +e
  trap - EXIT INT TERM

  if [ "${KEEP_DEMO:-}" != 1 ]; then
    log "Removing the demo data"
    # Only the nodes the seed created; anything else in openvoxdb is
    # left alone.
    (
      set -a
      # shellcheck disable=SC1091
      . ./.env
      set +a
      go run ./cmd/demo-seed --remove --i-know-this-is-a-demo-console
    ) || echo "   could not remove the demo fleet from openvoxdb; it will be garbage-collected on openvoxdb's normal schedule"
    docker exec "$POSTGRES_CONTAINER" psql -U console -d postgres \
      -c "DROP DATABASE IF EXISTS ${DEMO_DB} WITH (FORCE);" >/dev/null 2>&1 &&
      echo "   dropped the ${DEMO_DB} database"
    rm -rf .dev/demo-code
  fi

  log "Putting the stack back"
  if was_running headless-chrome; then
    # It was already in screenshot mode when this started; leave it so.
    echo "   the screenshot stack was already up, so it is left running"
  elif [ "${#BEFORE[@]}" -eq 0 ]; then
    # Nothing was running: remove everything this started, network
    # included. Volumes are kept - see the top of this file.
    "${SHOTS[@]}" down --remove-orphans
  else
    # Some of the development stack was running. Stop and remove what
    # this run added, then bring back what was there on its normal
    # configuration - recreating openvoxdb without the screenshot
    # retention settings.
    local started=() s
    while read -r s; do
      [ -n "$s" ] && ! was_running "$s" && started+=("$s")
    done < <("${SHOTS[@]}" ps --all --services 2>/dev/null)
    if [ "${#started[@]}" -gt 0 ]; then
      "${SHOTS[@]}" rm --stop --force "${started[@]}"
    fi
    "${DEV[@]}" up -d --no-deps --wait "${BEFORE[@]}"
  fi

  if [ "$status" -eq 0 ]; then
    log "Done. docs/ is rebuilt - look at it with \`make marketing-serve\`, then commit it."
  else
    log "Failed (exit $status). The stack has been put back; see the output above for the cause."
  fi
  exit "$status"
}
trap cleanup EXIT
trap 'exit 130' INT TERM

# --- run -------------------------------------------------------------
log "Starting the screenshot stack"
"${SHOTS[@]}" up -d --wait

log "Capturing screenshots"
./marketing/screenshots.sh

log "Building the site"
./marketing/build.sh
