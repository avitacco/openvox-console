// Tests for the Plural-Forms evaluator.
//
// Run with `node --test frontend/src/`, which frontend/build.sh does.
//
// These exist because a wrong plural rule fails silently: the page
// renders, the grammar is just wrong, and nobody who reads that language
// is necessarily looking. The expected values below are the standard
// gettext rules for each language, checked against the counts that
// actually distinguish the forms.

import test from 'node:test';
import assert from 'node:assert/strict';

import { parsePluralForms, DEFAULT_RULE } from './plural.js';

/** Asserts the form index chosen for each count in `cases`. */
function expectForms(rule, cases) {
  const { select } = parsePluralForms(rule);
  for (const [count, expected] of Object.entries(cases)) {
    assert.equal(
      select(Number(count)),
      expected,
      `${rule}\n  n=${count} should select form ${expected}, got ${select(Number(count))}`,
    );
  }
}

test('English and German: one vs everything else', () => {
  expectForms('nplurals=2; plural=(n != 1);', {
    0: 1, // "0 nodes"
    1: 0,
    2: 1,
    21: 1,
    100: 1,
  });
});

test('French: zero is singular', () => {
  // The distinction that makes French different from English.
  expectForms('nplurals=2; plural=(n > 1);', {
    0: 0, // "0 noeud", not "noeuds"
    1: 0,
    2: 1,
    100: 1,
  });
});

test('Chinese, Japanese, Korean, Indonesian: a single form', () => {
  const { nplurals, select } = parsePluralForms('nplurals=1; plural=0;');
  assert.equal(nplurals, 1);
  for (const n of [0, 1, 2, 5, 11, 100]) {
    assert.equal(select(n), 0, `n=${n} must select the only form`);
  }
});

test('Russian and Ukrainian: three forms', () => {
  const ru =
    'nplurals=3; plural=(n%10==1 && n%100!=11 ? 0 : ' +
    'n%10>=2 && n%10<=4 && (n%100<10 || n%100>=20) ? 1 : 2);';
  expectForms(ru, {
    1: 0,
    21: 0,
    101: 0,
    // 11 is the exception that catches a naive "ends in 1" rule.
    11: 2,
    111: 2,
    2: 1,
    3: 1,
    4: 1,
    22: 1,
    // 12-14 are the exception to the 2-4 rule.
    12: 2,
    13: 2,
    14: 2,
    5: 2,
    0: 2,
    25: 2,
  });
});

test('Polish: three forms, differing from Russian at n=1', () => {
  const pl =
    'nplurals=3; plural=(n==1 ? 0 : ' +
    'n%10>=2 && n%10<=4 && (n%100<10 || n%100>=20) ? 1 : 2);';
  expectForms(pl, {
    1: 0,
    // Unlike Russian, 21 is NOT the singular form in Polish.
    21: 2,
    2: 1,
    22: 1,
    12: 2,
    5: 2,
  });
});

test('Arabic: six forms, for when RTL support lands', () => {
  const ar =
    'nplurals=6; plural=(n==0 ? 0 : n==1 ? 1 : n==2 ? 2 : ' +
    'n%100>=3 && n%100<=10 ? 3 : n%100>=11 ? 4 : 5);';
  expectForms(ar, {
    0: 0,
    1: 1,
    2: 2,
    3: 3,
    10: 3,
    11: 4,
    99: 4,
    100: 5,
  });
});

test('an index beyond nplurals is clamped rather than reaching past the translations', () => {
  // A catalogue whose rule and form count disagree must not produce
  // `undefined` in the UI.
  const { select } = parsePluralForms('nplurals=2; plural=(n==0 ? 0 : n==1 ? 1 : 5);');
  assert.equal(select(7), 1, 'out-of-range index should clamp to the last form');
});

test('negative and fractional counts resolve to a real form', () => {
  const { select } = parsePluralForms('nplurals=3; plural=(n%10==1 && n%100!=11 ? 0 : n%10>=2 ? 1 : 2);');
  for (const n of [-1, -11, 1.5, 0]) {
    const form = select(n);
    assert.ok(Number.isInteger(form) && form >= 0 && form < 3, `n=${n} gave ${form}`);
  }
});

test('a missing or malformed rule falls back to English behaviour', () => {
  for (const bad of [undefined, '', 'nplurals=2; plural=(n <<>> 1);', 'gibberish']) {
    const { select } = parsePluralForms(bad);
    assert.equal(select(1), 0, `${JSON.stringify(bad)} should treat 1 as singular`);
    assert.equal(select(2), 1, `${JSON.stringify(bad)} should treat 2 as plural`);
  }
});

test('the default rule is English two-form', () => {
  const { nplurals, select } = parsePluralForms(DEFAULT_RULE);
  assert.equal(nplurals, 2);
  assert.equal(select(1), 0);
  assert.equal(select(0), 1);
});

test('operator precedence matches C', () => {
  // n%10==1 must parse as (n%10)==1, not n%(10==1).
  const { select } = parsePluralForms('nplurals=2; plural=(n%10==1 ? 0 : 1);');
  assert.equal(select(21), 0);
  assert.equal(select(22), 1);
});
