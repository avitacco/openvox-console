#!/bin/sh
# Runs on both install and upgrade (deb postinst / rpm %post - nfpm
# shares this one script between both formats). Re-derives everything
# node- and console-specific from scratch every time it runs, rather
# than relying on values baked in at package-build time (ConsoleBaseURL
# and TransportAddr are runtime console config, not compile-time
# constants - see design.md) or left over from a previous install, so
# an upgrade is self-healing if any of those answers change.
set -e

PUPPET_BIN=/opt/puppetlabs/bin/puppet
CERTNAME=$("$PUPPET_BIN" config print certname)
SSL_DIR=$("$PUPPET_BIN" config print ssldir)
CERT_FILE="${SSL_DIR}/certs/${CERTNAME}.pem"
KEY_FILE="${SSL_DIR}/private_keys/${CERTNAME}.pem"
CA_FILE="${SSL_DIR}/certs/ca.pem"

TRANSPORT_ADDR=""
if [ -f /etc/node-agent-client/console.env ]; then
  # shellcheck disable=SC1091
  . /etc/node-agent-client/console.env
  TRANSPORT_ADDR="$NODE_AGENT_TRANSPORT_ADDR"
fi

install -d -m 0755 /etc/node-agent-client
cat > /etc/node-agent-client/environment <<ENV
NODE_AGENT_TRANSPORT_ADDR=${TRANSPORT_ADDR}
NODE_AGENT_CERT_FILE=${CERT_FILE}
NODE_AGENT_KEY_FILE=${KEY_FILE}
NODE_AGENT_CA_FILE=${CA_FILE}
NODE_AGENT_PUPPET_BIN_PATH=${PUPPET_BIN}
ENV
chmod 0644 /etc/node-agent-client/environment

if command -v systemctl >/dev/null 2>&1; then
  systemctl daemon-reload
  systemctl enable node-agent-client
  systemctl restart node-agent-client
fi
