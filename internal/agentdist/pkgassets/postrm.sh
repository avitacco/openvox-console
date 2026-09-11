#!/bin/sh
# Only tears down what postinst itself created (the service's running
# state and its generated environment file) - /etc/node-agent-client/
# console.env is written by the install script, not this package, so
# it's left alone as a harmless leftover rather than removed here.
set -e

if command -v systemctl >/dev/null 2>&1; then
  systemctl stop node-agent-client 2>/dev/null || true
  systemctl disable node-agent-client 2>/dev/null || true
fi
rm -f /etc/node-agent-client/environment
