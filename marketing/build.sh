#!/usr/bin/env bash
# Builds the marketing site into marketing/dist.
#
# The same shape as frontend/build.sh deliberately: voxblocks ships a
# self-contained CDN bundle, so this is an asset copy rather than a
# bundler step, and the HTML is rendered from templates/ by gen/main.go,
# a build-time-only Go program.
#
# Unlike the frontend's, the output is a plain dist/ beside this script.
# frontend/ writes into internal/web/dist only because go:embed cannot
# reach above its own directory - nothing embeds this site, so it has no
# reason to inherit that.
set -euo pipefail
cd "$(dirname "$0")"

OUT=dist
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

rm -rf "$OUT"
mkdir -p "$OUT/vendor"

cp "$VOXBLOCKS/dist/cdn/voxblocks.js" "$OUT/vendor/voxblocks.js"
cp "$VOXBLOCKS/dist/cdn/voxblocks.css" "$OUT/vendor/voxblocks.css"
cp src/*.css src/*.js "$OUT/"

# Screenshots are committed, not generated here. gen fails if one a page
# needs is missing, so a forgotten capture stops the build rather than
# publishing a broken image.
mkdir -p "$OUT/assets/screenshots"
if compgen -G "assets/screenshots/*.png" > /dev/null; then
  cp assets/screenshots/*.png "$OUT/assets/screenshots/"
fi

go run ./gen "$OUT"

echo "Marketing site built into marketing/$OUT"
