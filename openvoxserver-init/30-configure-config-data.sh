#!/bin/sh
# Lets openvoxserver read a node group's "Configuration data" with
# lookup(), ahead of the control repo's own Hiera data.
#
# Two things, both re-applied on every boot because neither survives a
# container recreate (a restart keeps them):
#
#   1. the console_config_data function is copied into the openvox gem's
#      own puppet/functions directory. The server finds a global-layer
#      data_hash function there and nowhere else (not vendor_ruby, the
#      plugin cache lib or the server's var dir lib - all tried). The
#      directory is found by glob because it carries the gem's version.
#   2. a global hiera.yaml with one entry for it, written only when there
#      is none, or when it is the one this script wrote (marked on its
#      first line). An operator's own global hiera.yaml is never edited:
#      the entry to add is logged instead.
#
# Safe to run any number of times; a second run changes nothing. It never
# fails the boot: a console without this feature still has to start, and a
# missing function shows up as a clear error on the first compile.
#
# Overridable, for tests: CONFIG_DATA_FUNCTION_SRC, CONFIG_DATA_JRUBY_GEMS,
# CONFIG_DATA_HIERA.

SELF_DIR=$(dirname "$0")
SRC=${CONFIG_DATA_FUNCTION_SRC:-$SELF_DIR/console_config_data.rb}
GEMS=${CONFIG_DATA_JRUBY_GEMS:-/opt/puppetlabs/server/data/puppetserver/jruby-gems/gems}
HIERA=${CONFIG_DATA_HIERA:-/etc/puppetlabs/puppet/hiera.yaml}
MARKER='# Managed by OpenVox Console'

say() { echo "console-config-data: $*"; }

entry() {
  cat <<EOF
  - name: "Console configuration data"
    data_hash: console_config_data
EOF
}

# --- 1. the function --------------------------------------------------
installed=0
if [ ! -f "$SRC" ]; then
  say "ERROR: $SRC not found; node group configuration data will not be readable"
else
  for dir in "$GEMS"/openvox-[0-9]*/lib/puppet/functions "$GEMS"/puppet-[0-9]*/lib/puppet/functions; do
    [ -d "$dir" ] || continue
    if ! cmp -s "$SRC" "$dir/console_config_data.rb"; then
      cp "$SRC" "$dir/console_config_data.rb" && chmod 0644 "$dir/console_config_data.rb"
    fi
    installed=1
  done
  if [ "$installed" -eq 0 ]; then
    say "ERROR: no openvox gem found under $GEMS; node group configuration data will not be readable"
  fi
fi

# --- 2. the global hiera.yaml -----------------------------------------
want=$(mktemp)
{
  echo "$MARKER"
  echo "# Consults a node's configuration data from the console before the"
  echo "# environment's Hiera data. Rewritten on every start; to add your own"
  echo "# entries, remove the first line of this file and it will be left alone."
  echo "---"
  echo "version: 5"
  echo "hierarchy:"
  entry
} > "$want"

if [ ! -e "$HIERA" ]; then
  mkdir -p "$(dirname "$HIERA")" && cp "$want" "$HIERA" && chmod 0644 "$HIERA"
  say "wrote $HIERA"
elif [ "$(head -n 1 "$HIERA")" = "$MARKER" ]; then
  if ! cmp -s "$want" "$HIERA"; then
    cp "$want" "$HIERA" && chmod 0644 "$HIERA"
    say "updated $HIERA"
  fi
elif grep -q console_config_data "$HIERA"; then
  : # the operator already added it
else
  say "WARNING: $HIERA exists and was not written by the console, so it is left alone."
  say "Add this to its hierarchy, first, for node group configuration data to be used:"
  entry | sed 's/^/console-config-data:   /'
fi
rm -f "$want"
exit 0
