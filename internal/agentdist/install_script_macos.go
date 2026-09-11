package agentdist

import "text/template"

// installScriptMacOSTemplate is rendered by installScriptMacOS
// (handlers.go) with Config as its data - the macOS counterpart to
// installScriptTemplate/installScriptWindowsTemplate. See design.md in
// add-multi-platform-agent-install for why macOS needs its own script
// (launchd, not systemd; .dmg/.pkg, not apt/yum/msi).
//
// Unlike Windows (one hardcoded version, confirmed against OpenVox's own
// bootstrap module's identical workaround), macOS builds are split by
// *both* macOS major version and architecture, with inconsistent
// coverage per combination (confirmed live: macOS 15 has an arm64 build
// directory but no x86_64 one at all) - there's no single version to
// hardcode that's correct for every combination, and OpenVox's own
// bootstrap module has no macOS support at all yet to mirror (an
// explicit TODO in its own README). So this script detects the node's
// macOS major version and architecture, then reads the real directory
// listing for that exact combination and picks the newest non-RC
// release found there, rather than guessing a version that might not
// exist for that combination. See design.md's Risks - this could not be
// verified live (no macOS host in this project's dev environment).
const installScriptMacOSTemplate = `#!/usr/bin/env bash
# Installs openvoxagent (if not already present) and this console's own
# node-agent-client, registered under launchd. Safe to re-run.
set -euo pipefail

CONSOLE_URL={{printf "%q" .ConsoleBaseURL}}
TRANSPORT_ADDR={{printf "%q" .TransportAddr}}
# Empty when the console has no node-facing Puppet server address
# configured - enrollment below then leaves this node's own server
# setting alone (see design.md in add-install-script-auto-enrollment).
PUPPET_SERVER={{printf "%q" .PuppetServerAddr}}
# waitforcert is the poll interval, maxwaitforcert the total wait before
# giving up - setting only the former polls forever (see design.md).
CERT_POLL_SECONDS=15
CERT_MAX_WAIT_SECONDS=300

if [ "$(id -u)" -ne 0 ]; then
  echo "This script must be run as root (it installs a package and a launchd service)." >&2
  exit 1
fi

PUPPET_BIN=/opt/puppetlabs/bin/puppet

if [ ! -x "$PUPPET_BIN" ]; then
  echo "Installing openvoxagent..."
  MACOS_MAJOR=$(sw_vers -productVersion | cut -d. -f1)
  case "$(uname -m)" in
    arm64) ARCH=arm64 ;;
    x86_64) ARCH=x86_64 ;;
    *) echo "Unsupported architecture: $(uname -m)" >&2; exit 1 ;;
  esac

  INDEX_URL="https://downloads.voxpupuli.org/mac/openvox8/${MACOS_MAJOR}/${ARCH}/"
  DMG_LIST=$(curl -fsSL "$INDEX_URL" | grep -oE 'href="openvox-agent-[0-9][^"]*\.dmg"' | sed -E 's/^href="//; s/"$//')
  if [ -z "$DMG_LIST" ]; then
    echo "No openvoxagent build found for macOS ${MACOS_MAJOR} (${ARCH}) at ${INDEX_URL} - check https://downloads.voxpupuli.org/mac/openvox8/ manually for a supported combination." >&2
    exit 1
  fi

  # Skip release candidates, sort remaining stable releases by dotted
  # version (zero-padded into one sortable string - portable across GNU
  # and BSD sort, since BSD sort, macOS's default, has no -V flag), take
  # the newest.
  DMG_FILE=$(echo "$DMG_LIST" | grep -v -- '-rc[0-9]*-' | while IFS= read -r f; do
    v=$(echo "$f" | sed -E 's/^openvox-agent-([0-9]+)\.([0-9]+)\.([0-9]+)-.*/\1 \2 \3/')
    read -r maj min patch <<< "$v"
    printf '%03d%03d%03d %s\n' "$maj" "$min" "$patch" "$f"
  done | sort | tail -1 | awk '{print $2}')

  if [ -z "$DMG_FILE" ]; then
    echo "Found builds for macOS ${MACOS_MAJOR} (${ARCH}) but all are release candidates - refusing to auto-install a pre-release. Install manually from ${INDEX_URL}." >&2
    exit 1
  fi

  DMG_PATH="/tmp/${DMG_FILE}"
  curl -fsSL -o "$DMG_PATH" "${INDEX_URL}${DMG_FILE}"

  MOUNT_POINT=$(mktemp -d /tmp/openvox-agent-mount.XXXXXX)
  hdiutil -quiet attach "$DMG_PATH" -nobrowse -mountpoint "$MOUNT_POINT"
  PKG_PATH=$(find "$MOUNT_POINT" -maxdepth 2 -name "*.pkg" | head -1)
  if [ -z "$PKG_PATH" ]; then
    hdiutil -quiet detach "$MOUNT_POINT" || true
    echo "No .pkg found inside ${DMG_FILE} - unexpected disk image layout." >&2
    exit 1
  fi
  installer -pkg "$PKG_PATH" -target /
  hdiutil -quiet detach "$MOUNT_POINT"
  rm -f "$DMG_PATH"
  rmdir "$MOUNT_POINT" 2>/dev/null || true
else
  echo "openvoxagent already installed, skipping."
fi

echo "Setting up package-inventory reporting..."
install -d -m 0755 /opt/puppetlabs/facter/facts.d
cat > /opt/puppetlabs/facter/facts.d/package_inventory.sh <<'FACT'
#!/usr/bin/env bash
# External fact (see design.md in add-package-inventory-reporting):
# recomputed on every Facter run, not just once at install time.
set -euo pipefail
PACKAGES=$(pkgutil --pkgs | while IFS= read -r pkg_id; do
  version=$(pkgutil --pkg-info "$pkg_id" 2>/dev/null | awk -F': ' '/^version:/{print $2}')
  [ -z "$version" ] && version="unknown"
  printf '["%s","%s","pkgutil"]\n' "$pkg_id" "$version"
done | paste -sd, -)
printf '{"_puppet_inventory_1":{"packages":[%s]}}\n' "$PACKAGES"
FACT
chmod 0755 /opt/puppetlabs/facter/facts.d/package_inventory.sh

CERTNAME=$("$PUPPET_BIN" config print certname)
SSL_DIR=$("$PUPPET_BIN" config print ssldir)
CERT_FILE="${SSL_DIR}/certs/${CERTNAME}.pem"
KEY_FILE="${SSL_DIR}/private_keys/${CERTNAME}.pem"
CA_FILE="${SSL_DIR}/certs/ca.pem"

if [ ! -f "$CERT_FILE" ]; then
  # Same enrollment flow as the Linux script - see design.md in
  # add-install-script-auto-enrollment. Static-verified only: this
  # project's dev environment has no macOS host to run it on.
  if [ -n "$PUPPET_SERVER" ]; then
    echo "Pointing this node at Puppet server ${PUPPET_SERVER}..."
    "$PUPPET_BIN" config set server "$PUPPET_SERVER"
  fi

  ENROLL_SERVER=$("$PUPPET_BIN" config print server)
  echo "Enrolling ${CERTNAME} with the Puppet CA at ${ENROLL_SERVER} (waiting up to ${CERT_MAX_WAIT_SECONDS}s for the certificate to be signed)..."

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

case "$(uname -m)" in
  arm64) NODE_AGENT_ARCH=arm64 ;;
  x86_64) NODE_AGENT_ARCH=amd64 ;;
  *) echo "Unsupported architecture: $(uname -m)" >&2; exit 1 ;;
esac

echo "Downloading node-agent-client (darwin/${NODE_AGENT_ARCH})..."
install -d -m 0755 /opt/openvox-console/bin
# Downloaded to a temp file and moved into place rather than written
# directly to the final path - on a re-run, that path may be the
# currently-running launchd daemon's own executable, and macOS (like
# Linux) refuses to open a running binary for writing, which curl
# reports as "client returned ERROR on write". A same-directory rename
# is atomic and does not disturb the already-running process's handle.
curl -fsSL -o /opt/openvox-console/bin/node-agent-client.new \
  "${CONSOLE_URL}/packages/node-agent-client?os=darwin&arch=${NODE_AGENT_ARCH}"
chmod 0755 /opt/openvox-console/bin/node-agent-client.new
mv /opt/openvox-console/bin/node-agent-client.new /opt/openvox-console/bin/node-agent-client

PLIST_PATH=/Library/LaunchDaemons/org.voxpupuli.node-agent-client.plist
cat > "$PLIST_PATH" <<PLIST
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>Label</key>
  <string>org.voxpupuli.node-agent-client</string>
  <key>ProgramArguments</key>
  <array>
    <string>/opt/openvox-console/bin/node-agent-client</string>
  </array>
  <key>RunAtLoad</key>
  <true/>
  <key>KeepAlive</key>
  <true/>
  <key>EnvironmentVariables</key>
  <dict>
    <key>NODE_AGENT_TRANSPORT_ADDR</key>
    <string>${TRANSPORT_ADDR}</string>
    <key>NODE_AGENT_CERT_FILE</key>
    <string>${CERT_FILE}</string>
    <key>NODE_AGENT_KEY_FILE</key>
    <string>${KEY_FILE}</string>
    <key>NODE_AGENT_CA_FILE</key>
    <string>${CA_FILE}</string>
    <key>NODE_AGENT_PUPPET_BIN_PATH</key>
    <string>${PUPPET_BIN}</string>
  </dict>
</dict>
</plist>
PLIST

chown root:wheel "$PLIST_PATH"
chmod 644 "$PLIST_PATH"

# bootout-then-bootstrap makes this idempotent: unloading a
# not-currently-loaded label is a harmless no-op error, ignored.
launchctl bootout system/org.voxpupuli.node-agent-client >/dev/null 2>&1 || true
launchctl bootstrap system "$PLIST_PATH"
launchctl enable system/org.voxpupuli.node-agent-client

echo "node-agent-client installed and started."
`

var installTmplMacOS = template.Must(template.New("install-macos.sh").Parse(installScriptMacOSTemplate))
