#!/usr/bin/env bash
set -euo pipefail
PACKAGES=$(rpm -qa --queryformat '%{NAME}\t%|EPOCH?{%{EPOCH}:}:{}|%{VERSION}-%{RELEASE}\n' | awk -F'\t' '{printf "%s[\"%s\",\"%s\",\"rpm\"]", (NR>1?",":""), $1, $2}')
printf '{"_puppet_inventory_1":{"packages":[%s]},"console_package_inventory":{"format":2}}\n' "$PACKAGES"
