#!/bin/sh
# Configures openvoxserver to use this console as its External Node
# Classifier: node_terminus = exec, pointing at the enc-bridge binary
# mounted into this container (see docker-compose.yml). puppet.conf is
# regenerated from env vars on every container start, so this must be
# idempotent and re-apply every boot - see README.md for the full setup.
set -e

CONF=/etc/puppetlabs/puppet/puppet.conf

if ! grep -q '^external_nodes' "$CONF"; then
  sed -i '/^\[server\]/a external_nodes = /usr/local/bin/enc-bridge' "$CONF"
  sed -i '/^\[server\]/a node_terminus = exec' "$CONF"
fi
