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

# There is no bundler here, so nothing else would notice a syntax error
# until the page loaded blank in a browser. node parses each module
# without running it, which is cheap and catches exactly that.
# Plural rules are the one part of translation that fails silently: a
# wrong rule renders fine and is merely ungrammatical, in a language the
# person who added it probably does not read.
# src/locales.js is generated from the catalogues that exist (see
# frontend/i18n). It has to be regenerated BEFORE the tests run, not
# with the rest of the compile step further down: i18n.js imports it,
# so a stale copy fails the whole suite at import time with a
# "does not provide an export named ..." that looks like a code error
# rather than a build-order one. Adding a language to languages.go and
# watching the tests fail is exactly how that was found.
echo "Regenerating the locale list"
locales_tmp="$(mktemp -d)"
trap 'rm -rf "$locales_tmp"' EXIT
go run ./i18n compile "$locales_tmp" >/dev/null

echo "Running JavaScript tests"
node --test src/ >/dev/null || {
  echo "JavaScript tests failed; not building" >&2
  node --test src/ 2>&1 | tail -20 >&2
  exit 1
}

echo "Checking JavaScript syntax"
syntax_errors=0
for f in src/*.js; do
  if ! node --check "$f"; then
    syntax_errors=$((syntax_errors + 1))
  fi
done
if [ "$syntax_errors" -ne 0 ]; then
  echo "$syntax_errors file(s) failed to parse; not building" >&2
  exit 1
fi

OUT=../internal/web/dist
rm -rf "$OUT"
mkdir -p "$OUT/vendor"

cp node_modules/@openvoxproject/voxblocks/dist/cdn/voxblocks.js "$OUT/vendor/voxblocks.js"
cp node_modules/@openvoxproject/voxblocks/dist/cdn/voxblocks.css "$OUT/vendor/voxblocks.css"
# Message catalogues: locales/*.po compiled to the compact JSON the
# browser fetches, plus src/locales.js listing what exists. English is
# the source language and has no catalogue - an untranslated string falls
# back to its own msgid, which is the English text (see frontend/i18n).
#
# Before the copy below, because it regenerates src/locales.js.
go run ./i18n compile "$OUT/locales"

cp src/*.css "$OUT/"
# Everything but the tests: *.test.js is for `node --test` above and has
# no business in a shipped bundle - it imports node:test, which no
# browser can resolve.
for f in src/*.js; do
  case "$f" in
    *.test.js) continue ;;
  esac
  cp "$f" "$OUT/"
done
cp ../LICENSE "$OUT/LICENSE"

go run ./gen "$OUT"

echo "Frontend assets built into $OUT/"
