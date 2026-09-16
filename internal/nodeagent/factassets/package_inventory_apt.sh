#!/usr/bin/env bash
set -euo pipefail
# sources lists only binaries whose source package differs in name or
# version - a binary absent from it is its own source. Multi-arch copies
# of one package share a name, so each name is emitted once.
OUTPUT=$(dpkg-query -W -f='${Package}\t${Version}\t${source:Package}\t${source:Version}\n' | awk -F'\t' '
{
  packages = packages (NR > 1 ? "," : "") sprintf("[\"%s\",\"%s\",\"apt\"]", $1, $2)
  if (($3 != $1 || $4 != $2) && !seen[$1]++) {
    sources = sources (n++ ? "," : "") sprintf("\"%s\":[\"%s\",\"%s\"]", $1, $3, $4)
  }
}
END {
  printf "{\"_puppet_inventory_1\":{\"packages\":[%s]},\"console_package_inventory\":{\"format\":2,\"sources\":{%s}}}", packages, sources
}')
printf '%s\n' "$OUTPUT"
