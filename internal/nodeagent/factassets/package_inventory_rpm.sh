#!/usr/bin/env bash
set -euo pipefail
PACKAGES=$(rpm -qa --queryformat '%{NAME}\t%{VERSION}-%{RELEASE}\n' | awk -F'\t' '{printf "%s[\"%s\",\"%s\",\"rpm\"]", (NR>1?",":""), $1, $2}')
printf '{"_puppet_inventory_1":{"packages":[%s]}}\n' "$PACKAGES"
