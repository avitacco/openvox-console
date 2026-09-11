#!/usr/bin/env bash
set -euo pipefail
PACKAGES=$(dpkg-query -W -f='${Package}\t${Version}\n' | awk -F'\t' '{printf "%s[\"%s\",\"%s\",\"apt\"]", (NR>1?",":""), $1, $2}')
printf '{"_puppet_inventory_1":{"packages":[%s]}}\n' "$PACKAGES"
