import test from 'node:test';
import assert from 'node:assert/strict';
import { parsePatterns, formatPatterns } from './pattern-text.js';

test('parsePatterns takes one pattern per line and drops blanks and padding', () => {
  assert.deepEqual(parsePatterns('patch::*\n  package::install  \r\n\n*\n'), ['patch::*', 'package::install', '*']);
});

test('an empty field is an empty list, which allows every task', () => {
  assert.deepEqual(parsePatterns(''), []);
  assert.deepEqual(parsePatterns('  \n \n'), []);
  assert.deepEqual(parsePatterns(undefined), []);
});

test('formatPatterns is the inverse for a list of patterns', () => {
  const list = ['patch::*', 'deploy::app'];
  assert.deepEqual(parsePatterns(formatPatterns(list)), list);
  assert.equal(formatPatterns([]), '');
  assert.equal(formatPatterns(null), '');
});
