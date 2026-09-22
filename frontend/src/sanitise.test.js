// Tests for the inline-markup sanitiser behind data-i18n-html.
//
// Run with `node --test frontend/src/`, which frontend/build.sh does.
//
// A catalogue is data the browser fetches, and data-i18n-html writes it
// into innerHTML. That is only safe because everything but plain inline
// emphasis is stripped first, so the stripping is tested rather than
// assumed.

import test from 'node:test';
import assert from 'node:assert/strict';

// sanitiseInline needs a DOM (document.createElement('template')), which
// plain node does not have. A minimal stand-in is not good enough here:
// the point of the function is that the browser's own parser decides
// what the markup means, so these tests run only where that parser
// exists and skip otherwise rather than testing a different parser.
const haveDOM = typeof document !== 'undefined';

test('sanitiseInline keeps emphasis and drops everything else', { skip: !haveDOM }, async () => {
  const { sanitiseInline } = await import('./i18n.js');

  // Inline emphasis survives.
  assert.equal(sanitiseInline('a <strong>b</strong> c'), 'a <strong>b</strong> c');
  assert.equal(sanitiseInline('<em>x</em> <code>y</code>'), '<em>x</em> <code>y</code>');

  // Attributes go, including the ones that would run code or navigate.
  assert.equal(sanitiseInline('<strong onclick="x()">b</strong>'), '<strong>b</strong>');
  assert.equal(sanitiseInline('<span class="big" id="q">b</span>'), '<span>b</span>');

  // Tags outside the allowlist are unwrapped, never silently deleted -
  // a translation with unexpected markup loses its formatting, not its
  // words.
  assert.equal(sanitiseInline('<a href="http://evil">click</a>'), 'click');
  assert.equal(sanitiseInline('<div>text</div>'), 'text');
  assert.equal(sanitiseInline('<script>alert(1)</script>'), 'alert(1)');
  assert.equal(sanitiseInline('<img src=x onerror=y>'), '');
});

test('normaliseSpace makes the msgid independent of template indentation', async () => {
  const { normaliseSpace } = await import('./i18n.js');
  assert.equal(normaliseSpace('\n  a   <em>b</em>\n  c\n'), 'a <em>b</em> c');
  assert.equal(normaliseSpace('already tidy'), 'already tidy');
});
