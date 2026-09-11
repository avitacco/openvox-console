#!/usr/bin/env bash
# Builds the embedded frontend bundle. Voxblocks ships a self-contained CDN
# bundle (Lit included), so this is a plain asset copy for CSS/JS, not a JS
# bundler step - see design.md for why. HTML pages are generated from
# templates/ (shared head/header boilerplate + per-page content) via
# gen/main.go, a build-time-only Go program - see gen/main.go. Output goes
# to internal/web/dist, not a local dist/, because Go's go:embed can only
# embed files at or below the directory of the .go file that embeds them
# (internal/web/web.go).
set -euo pipefail
cd "$(dirname "$0")"

npm install --no-audit --no-fund

OUT=../internal/web/dist
rm -rf "$OUT"
mkdir -p "$OUT/vendor"

cp node_modules/@openvoxproject/voxblocks/dist/cdn/voxblocks.js "$OUT/vendor/voxblocks.js"
cp node_modules/@openvoxproject/voxblocks/dist/cdn/voxblocks.css "$OUT/vendor/voxblocks.css"
cp src/*.css src/*.js "$OUT/"
cp ../LICENSE "$OUT/LICENSE"

go run ./gen "$OUT"

echo "Frontend assets built into $OUT/"
