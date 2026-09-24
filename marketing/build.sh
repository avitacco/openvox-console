#!/usr/bin/env bash
# Builds the marketing site into docs/, which is what GitHub Pages
# serves.
#
# The site is built here and committed, not built in CI. `make marketing`
# regenerates docs/, you look at the result, and you push it - so what is
# published is something somebody has actually seen, rather than whatever
# a workflow produced from the last commit.
#
# docs/ is therefore committed build output, with one exception:
# docs/assets/screenshots is not output at all. Those images come from
# `make marketing-screenshots` and are the site's content. They live
# under docs/ rather than being copied there so that they are committed
# once instead of twice.
#
# Like frontend/build.sh, this is an asset copy rather than a bundler
# step - voxblocks ships a self-contained CDN bundle - plus gen/main.go,
# a build-time-only Go program, to render the HTML.
set -euo pipefail
cd "$(dirname "$0")"

OUT=../docs
VOXBLOCKS=../frontend/node_modules/@openvoxproject/voxblocks

# The design system comes from the frontend's lockfile rather than a
# second package.json of our own. The site advertises the console, so the
# two must render with the same component versions; sourcing from one
# lockfile makes that structural instead of a coincidence somebody has to
# keep checking.
if [ ! -d "$VOXBLOCKS" ]; then
  echo "voxblocks is not installed; running the frontend's install step"
  (cd ../frontend && npm install --no-audit --no-fund)
fi

# The locales the site is published in. English is the source and lives
# at the site root; each other locale gets its own directory. The list is
# shorter than the console's fifteen on purpose - see siteLocales in
# gen/main.go.
#
# "code:direction" - the direction is what gen stamps on <html dir>,
# which is the whole of voxblocks' RTL contract.
LOCALES=(zh:ltr hi:ltr es:ltr fr:ltr de:ltr ja:ltr ar:rtl)

# Remove what this script generates, and only that. A blanket rm would
# take the screenshots with it, and they are not regenerable from here -
# recapturing them needs a seeded console and several minutes.
rm -rf "$OUT/vendor" "$OUT/features" "$OUT/guides"
rm -f "$OUT"/*.html "$OUT"/*.css "$OUT"/*.js
for entry in "${LOCALES[@]}"; do
  rm -rf "$OUT/${entry%%:*}"
done
mkdir -p "$OUT/vendor" "$OUT/assets/screenshots"

cp "$VOXBLOCKS/dist/cdn/voxblocks.js" "$OUT/vendor/voxblocks.js"
cp "$VOXBLOCKS/dist/cdn/voxblocks.css" "$OUT/vendor/voxblocks.css"
cp src/*.css src/*.js "$OUT/"

# Tells GitHub Pages to serve the directory as-is rather than running it
# through Jekyll, which would otherwise skip anything whose name begins
# with an underscore.
touch "$OUT/.nojekyll"

# Catalogues compiled to the same JSON the console's browser fetches, so
# there is one PO implementation in the tree rather than two. They go to
# a scratch directory, not into docs/: the marketing site is static and
# translated at build time, so a visitor never downloads a catalogue.
catalogues="$(mktemp -d)"
trap 'rm -rf "$catalogues"' EXIT
(cd ../frontend && go run ./i18n \
  -source ../marketing/templates,../marketing/gen,../marketing/shots,../marketing/guides \
  -locales ../marketing/locales \
  -pot site.pot \
  -no-module \
  compile "$catalogues")

go run ./gen "$OUT"
for entry in "${LOCALES[@]}"; do
  code="${entry%%:*}"
  dir="${entry##*:}"
  go run ./gen -locale "$code" -dir "$dir" -catalogue "$catalogues/$code.json" "$OUT"
done

echo
echo "Marketing site built into docs/ (en + ${#LOCALES[@]} translations)"
echo "Look at it before pushing:  (cd docs && python3 -m http.server 8777)"
