#!/bin/sh
# Only tears down what postinst itself created (the service's running
# state and its generated environment file) - /etc/node-agent-client/
# console.env is written by the install script, not this package, so
# it's left alone as a harmless leftover rather than removed here.
#
# Only on a real removal. Both package managers also run this script
# while UPGRADING, as part of removing the old version:
#
#   dpkg  postrm upgrade <new-version>   before the new postinst
#   rpm   %postun with $1 = 1            AFTER the new %post
#
# Tearing down then is wrong on both, and on rpm it was fatal: the new
# package had already restarted the service, and this stopped it,
# disabled it and deleted its environment file, so every rpm upgrade -
# by the install script or by the console - left the node with no
# client running. dpkg calls a real removal "remove" (then "purge");
# rpm calls it 0.
set -e

case "${1:-}" in
  remove | purge | 0) ;;
  *) exit 0 ;;
esac

if command -v systemctl >/dev/null 2>&1; then
  systemctl stop node-agent-client 2>/dev/null || true
  systemctl disable node-agent-client 2>/dev/null || true
fi
rm -f /etc/node-agent-client/environment
