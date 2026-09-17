#!/usr/bin/env bash
set -euo pipefail
# sources lists only binaries whose source package differs in name or
# version - a binary absent from it is its own source. Multi-arch copies
# of one package share a name, so each name is emitted once.
#
# db:Status-Status is selected as the first field and every row that
# isn't "installed" is dropped: dpkg-query -W also lists packages in
# config-files state (dpkg -l shows these as rc - removed, but their
# configuration files retained), which are not installed and must not be
# reported. Filtering happens before the aggregation below so both
# emitted structures are built from the same rows.
#
# This is install status, deliberately not selection state: a held
# package's selection is "hold" rather than "install" while the package
# is still installed, so filtering on selection would hide it from
# vulnerability scanning entirely.
#
# The packages separator counts emitted rows rather than using NR, which
# now also counts the rows filtered out above - keying it on NR would
# emit a leading comma whenever the first input row is dropped.
OUTPUT=$(dpkg-query -W -f='${db:Status-Status}\t${Package}\t${Version}\t${source:Package}\t${source:Version}\n' | awk -F'\t' '
$1 != "installed" { next }
{
  packages = packages (emitted++ ? "," : "") sprintf("[\"%s\",\"%s\",\"apt\"]", $2, $3)
  if (($4 != $2 || $5 != $3) && !seen[$2]++) {
    sources = sources (n++ ? "," : "") sprintf("\"%s\":[\"%s\",\"%s\"]", $2, $4, $5)
  }
}
END {
  printf "{\"_puppet_inventory_1\":{\"packages\":[%s]},\"console_package_inventory\":{\"format\":2,\"sources\":{%s}}}", packages, sources
}')
printf '%s\n' "$OUTPUT"
