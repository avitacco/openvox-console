package agentdist

// installScriptTemplate is rendered by installScript (handlers.go) with
// Config as its data. Covers Debian/Ubuntu-family (apt-get) and
// RedHat-family (yum/dnf) Linux - see design.md in
// add-multi-platform-agent-install for why those two package-manager
// families specifically (matching what OpenVox itself publishes for
// Linux) and not, say, SUSE/zypper. Windows and macOS get their own
// templates (install_script_windows.go, install_script_macos.go) -
// see design.md for why one script can't cover every OS. Idempotent:
// safe to re-run on a node that already has everything installed (see
// specs/agent-distribution's "Running the script on an already-
// provisioned node" scenario).
const installScriptTemplate = `#!/usr/bin/env bash
# Installs openvoxagent (if not already present) and this console's own
# node-agent-client. openvoxagent itself has no native package this
# script can just point a package manager at; node-agent-client does, as of
# add-native-agent-packaging - this script's own job for node-agent-
# client is now just "add the console's package repo and install the
# package", not writing its files directly (see that design.md for why
# and its Migration Plan for nodes enrolled before this change). Safe to
# re-run.
set -euo pipefail

CONSOLE_URL={{printf "%q" .ConsoleBaseURL}}
TRANSPORT_ADDR={{printf "%q" .TransportAddr}}
# Empty when the console has no node-facing Puppet server address
# configured - the enrollment step below then leaves this node's own
# server setting alone rather than guessing one (see design.md in
# add-install-script-auto-enrollment).
PUPPET_SERVER={{printf "%q" .PuppetServerAddr}}
# Waiting for someone to sign this node's certificate request when the
# CA doesn't autosign. These are puppet's own two settings and they mean
# different things (confirmed in the agent's defaults.rb, not assumed):
# waitforcert is how often to re-ask, maxwaitforcert is the total time
# before giving up. Setting only the former polls forever - which is
# exactly the indefinite hang design.md rejected.
CERT_POLL_SECONDS=15
CERT_MAX_WAIT_SECONDS=300

if [ "$(id -u)" -ne 0 ]; then
  echo "This script must be run as root (it installs a package and a systemd service)." >&2
  exit 1
fi

if ! command -v apt-get >/dev/null 2>&1 && ! command -v yum >/dev/null 2>&1 && ! command -v dnf >/dev/null 2>&1; then
  echo "This installer currently only supports Debian/Ubuntu-family (apt-get) and RedHat-family (yum/dnf) systems." >&2
  exit 1
fi

if ! command -v curl >/dev/null 2>&1; then
  echo "Installing curl (required by this script)..."
  if command -v apt-get >/dev/null 2>&1; then
    apt-get update -qq
    apt-get install -y curl ca-certificates
  else
    (command -v dnf >/dev/null 2>&1 && dnf install -y curl ca-certificates) || yum install -y curl ca-certificates
  fi
fi

if ! command -v puppet >/dev/null 2>&1 && [ ! -x /opt/puppetlabs/bin/puppet ]; then
  echo "Installing openvoxagent..."
  OS_ID=$(. /etc/os-release && echo "$ID")
  OS_VERSION_ID=$(. /etc/os-release && echo "$VERSION_ID")

  if command -v apt-get >/dev/null 2>&1; then
    RELEASE_DEB="openvox8-release-${OS_ID}${OS_VERSION_ID}.deb"
    curl -fsSL -o "/tmp/${RELEASE_DEB}" "https://apt.voxpupuli.org/${RELEASE_DEB}"
    apt-get update -qq
    apt-get install -y ca-certificates "/tmp/${RELEASE_DEB}"
    rm -f "/tmp/${RELEASE_DEB}"
    apt-get update -qq
    apt-get install -y openvox-agent
  else
    # RedHat family - covers what yum.voxpupuli.org actually publishes
    # openvox8-release packages for (confirmed live): el (RHEL, CentOS,
    # Rocky, AlmaLinux - major version only), fedora, and Amazon Linux.
    # SLES lives in the same repo tree but uses zypper, not yum/dnf -
    # out of scope for this branch (see design.md's Non-Goals).
    case "$OS_ID" in
      rhel|centos|rocky|almalinux)
        FAMILY="el"
        FAMILY_VERSION="${OS_VERSION_ID%%.*}"
        ;;
      fedora)
        FAMILY="fedora"
        FAMILY_VERSION="${OS_VERSION_ID}"
        ;;
      amzn)
        FAMILY="amazon"
        FAMILY_VERSION="${OS_VERSION_ID}"
        ;;
      *)
        echo "Unrecognized RedHat-family distro (ID=${OS_ID}) - this installer supports RHEL/CentOS/Rocky/AlmaLinux, Fedora, and Amazon Linux." >&2
        exit 1
        ;;
    esac
    YUM_BIN=$(command -v dnf || command -v yum)
    RELEASE_RPM="openvox8-release-${FAMILY}-${FAMILY_VERSION}.noarch.rpm"
    "$YUM_BIN" install -y "https://yum.voxpupuli.org/${RELEASE_RPM}"
    "$YUM_BIN" install -y openvox-agent
  fi
else
  echo "openvoxagent already installed, skipping."
fi

PUPPET_BIN=/opt/puppetlabs/bin/puppet
CERTNAME=$("$PUPPET_BIN" config print certname)
SSL_DIR=$("$PUPPET_BIN" config print ssldir)
CERT_FILE="${SSL_DIR}/certs/${CERTNAME}.pem"

if [ ! -f "$CERT_FILE" ]; then
  # This node has never enrolled. Do it here rather than making the
  # operator run puppet themselves and start the install over - see
  # add-install-script-auto-enrollment. A node that already has a
  # certificate never reaches this block, so re-running stays a no-op.
  if [ -n "$PUPPET_SERVER" ]; then
    echo "Pointing this node at Puppet server ${PUPPET_SERVER}..."
    "$PUPPET_BIN" config set server "$PUPPET_SERVER"
  fi

  ENROLL_SERVER=$("$PUPPET_BIN" config print server)
  echo "Enrolling ${CERTNAME} with the Puppet CA at ${ENROLL_SERVER} (waiting up to ${CERT_MAX_WAIT_SECONDS}s for the certificate to be signed)..."

  # 'ssl bootstrap' requests and downloads a certificate and nothing
  # else - deliberately not 'agent -t', whose catalog run can fail for
  # reasons that have nothing to do with enrollment and would be
  # reported as an enrollment failure. Its own --waitforcert polling is
  # the wait; we don't loop. Output is captured (and echoed) so the
  # branch below can tell a CA it couldn't reach from a request that is
  # merely still unsigned - their exit codes are both 1 and carry no
  # information (confirmed against openvox-agent 8.28.1, see design.md).
  ENROLL_OUTPUT=$("$PUPPET_BIN" ssl bootstrap --waitforcert "$CERT_POLL_SECONDS" --maxwaitforcert "$CERT_MAX_WAIT_SECONDS" 2>&1) || true
  printf '%s\n' "$ENROLL_OUTPUT"

  if [ ! -f "$CERT_FILE" ]; then
    if printf '%s' "$ENROLL_OUTPUT" | grep -qE 'No more routes to ca|Failed to open TCP connection|certificate verify failed|Connection refused'; then
      echo "" >&2
      echo "Could not reach the Puppet CA at ${ENROLL_SERVER} - see the error above." >&2
      echo "This is a connectivity or TLS problem, not a certificate waiting to be signed." >&2
      echo "Check that ${ENROLL_SERVER} resolves from this node, that port 8140 is reachable, and that the name matches the CA certificate's SANs." >&2
      exit 1
    fi

    echo "" >&2
    echo "This node's certificate request was submitted but has not been signed yet." >&2
    echo "Sign it in the console (Nodes page): ${CONSOLE_URL}/nodes.html" >&2
    echo "Then re-run this script - it will pick up from here." >&2
    exit 1
  fi

  echo "Certificate signed and installed at ${CERT_FILE}."
fi

# One-time migration for a node enrolled under this project's older
# script-managed install (raw binary and hand-written unit file, neither
# package-tracked) - see design.md's Migration Plan in
# add-native-agent-packaging. Only runs if that old install is actually
# present; a no-op on every later re-run once it's cleaned up.
if [ -f /etc/systemd/system/node-agent-client.service ] && [ ! -f /usr/lib/systemd/system/node-agent-client.service ]; then
  echo "Migrating node-agent-client from the old script-managed install to a package-managed install..."
  systemctl stop node-agent-client 2>/dev/null || true
  systemctl disable node-agent-client 2>/dev/null || true
  rm -f /etc/systemd/system/node-agent-client.service /opt/openvox-console/bin/node-agent-client
  systemctl daemon-reload 2>/dev/null || true
fi

# The console's transport address is runtime config, not known when
# make agent-packages built the package below - its own postinst
# reads this file (and re-derives everything node-specific itself) on
# every install and upgrade, so it stays correct even if the console's
# own config changes later (see design.md).
install -d -m 0755 /etc/node-agent-client
cat > /etc/node-agent-client/console.env <<EOF
NODE_AGENT_TRANSPORT_ADDR=${TRANSPORT_ADDR}
EOF
chmod 0644 /etc/node-agent-client/console.env

echo "Installing node-agent-client..."
if command -v apt-get >/dev/null 2>&1; then
  curl -fsSL -o /etc/apt/sources.list.d/openvox-console.list "${CONSOLE_URL}/packages/apt/node-agent-client.list"
  apt-get update -qq
  apt-get install -y node-agent-client
else
  curl -fsSL -o /etc/yum.repos.d/openvox-console.repo "${CONSOLE_URL}/packages/yum/node-agent-client.repo"
  (command -v dnf >/dev/null 2>&1 && dnf install -y node-agent-client) || yum install -y node-agent-client
fi

if ! command -v systemctl >/dev/null 2>&1; then
  echo "systemctl not found (this system/container has no systemd) - node-agent-client is installed at /usr/bin/node-agent-client but not registered as a service."
  echo "Start it manually with the environment the package generated, e.g.: env \$(cat /etc/node-agent-client/environment) /usr/bin/node-agent-client"
fi
`
