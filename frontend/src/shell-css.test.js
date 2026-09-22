// Structural checks on shell.css.
//
// Run with `node --test frontend/src/`, which frontend/build.sh does.
//
// CSS fails silently: a parser that meets something it cannot read
// discards tokens until it finds a recovery point, taking whole rules
// with it and reporting nothing. Editing a comment here once left a
// block of prose outside its delimiters, which swallowed the rule that
// followed - the page rendered, one set of declarations was simply gone,
// and the only symptom was text wrapping where it should have
// ellipsised. `node --check` covers the JavaScript; nothing covered this.

import test from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';

const css = readFileSync(join(dirname(fileURLToPath(import.meta.url)), 'shell.css'), 'utf8');

/** Line number of a character offset, for a message worth reading. */
function lineAt(offset) {
  return css.slice(0, offset).split('\n').length;
}

test('every comment is opened and closed', () => {
  // Walked rather than regex-matched so the offset of the offender is
  // known and can be named.
  let i = 0;
  let depth = 0;
  let openedAt = 0;
  while (i < css.length - 1) {
    const two = css.slice(i, i + 2);
    if (two === '/*') {
      if (depth === 0) openedAt = i;
      depth += 1;
      i += 2;
      continue;
    }
    if (two === '*/') {
      assert.ok(depth > 0, `stray "*/" at line ${lineAt(i)} - the text above it is not inside a comment`);
      depth -= 1;
      i += 2;
      continue;
    }
    i += 1;
  }
  assert.equal(depth, 0, `unterminated comment opened at line ${lineAt(openedAt)}`);
});

test('braces balance outside comments and strings', () => {
  let depth = 0;
  let i = 0;
  let inComment = false;
  let quote = null;
  let lastOpen = 0;

  while (i < css.length) {
    const ch = css[i];
    const two = css.slice(i, i + 2);

    if (inComment) {
      if (two === '*/') { inComment = false; i += 2; continue; }
      i += 1;
      continue;
    }
    if (quote) {
      if (ch === '\\') { i += 2; continue; }
      if (ch === quote) quote = null;
      i += 1;
      continue;
    }
    if (two === '/*') { inComment = true; i += 2; continue; }
    if (ch === '"' || ch === "'") { quote = ch; i += 1; continue; }
    if (ch === '{') { if (depth === 0) lastOpen = i; depth += 1; }
    if (ch === '}') {
      depth -= 1;
      assert.ok(depth >= 0, `unmatched "}" at line ${lineAt(i)}`);
    }
    i += 1;
  }

  assert.equal(depth, 0, `unclosed "{" opened at line ${lineAt(lastOpen)}`);
});

test('no declaration sits outside a rule block', () => {
  // The shape the broken comment produced: prose or declarations at the
  // top level, between rules, where a selector was expected.
  let depth = 0;
  let i = 0;
  let inComment = false;
  const stray = [];
  let segmentStart = 0;

  while (i < css.length) {
    const two = css.slice(i, i + 2);
    if (inComment) {
      if (two === '*/') { inComment = false; i += 2; segmentStart = i; continue; }
      i += 1;
      continue;
    }
    if (two === '/*') { inComment = true; i += 2; continue; }
    const ch = css[i];
    if (ch === '{') {
      if (depth === 0) {
        // Everything since the last rule or comment is the selector.
        // A selector never contains a semicolon.
        const selector = css.slice(segmentStart, i);
        if (selector.includes(';')) {
          stray.push(`line ${lineAt(segmentStart)}: "${selector.trim().slice(0, 60)}"`);
        }
      }
      depth += 1;
    } else if (ch === '}') {
      depth -= 1;
      if (depth === 0) segmentStart = i + 1;
    }
    i += 1;
  }

  assert.deepEqual(stray, [], `these look like declarations outside any rule:\n  ${stray.join('\n  ')}`);
});
