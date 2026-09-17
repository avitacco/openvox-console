#!/usr/bin/env bash
set -euo pipefail
# gpg-pubkey rows are the repository signing keys rpm records once they
# have been imported, not installed software, so they are dropped rather
# than reported as packages. rpm needs no install-state filter beyond
# that: erasing a package removes its rpmdb entry outright, so there is
# no analogue of dpkg's config-files state.
#
# The separator counts emitted rows rather than using NR, which also
# counts the rows filtered out above - keying it on NR would emit a
# leading comma whenever the first row is a signing key.
PACKAGES=$(rpm -qa --queryformat '%{NAME}\t%|EPOCH?{%{EPOCH}:}:{}|%{VERSION}-%{RELEASE}\n' | awk -F'\t' '
$1 == "gpg-pubkey" { next }
{ printf "%s[\"%s\",\"%s\",\"rpm\"]", (emitted++ ? "," : ""), $1, $2 }')
printf '{"_puppet_inventory_1":{"packages":[%s]},"console_package_inventory":{"format":2}}\n' "$PACKAGES"
