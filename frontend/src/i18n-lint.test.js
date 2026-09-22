// Guards the two ways a translation call can silently stop being one.
//
// Run with `node --test frontend/src/`, which frontend/build.sh does.
//
// Neither failure shows up in a catalogue-completeness report - the .po
// reads 100% translated either way - so they are checked here instead.

import test from 'node:test';
import assert from 'node:assert/strict';
import { readdirSync, readFileSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';

const here = dirname(fileURLToPath(import.meta.url));

/** The frontend's own modules, excluding tests and generated files. */
function sourceFiles() {
  return readdirSync(here)
    .filter((f) => f.endsWith('.js'))
    .filter((f) => !f.endsWith('.test.js'))
    .filter((f) => f !== 'locales.js');
}

/** Strips comments so a name mentioned in prose is not a finding. */
function withoutComments(source) {
  return source.replace(/\/\*[\s\S]*?\*\//g, '').replace(/^\s*\/\/.*$/gm, '');
}

test('nothing shadows the translation functions', () => {
  // A parameter or variable named `t` turns every t('...') in its scope
  // into a call on whatever it holds. service-tokens.js did exactly
  // this - `.map((t) => ...)` over token objects - and the page died
  // with "t is not a function" rather than merely showing English.
  const shadows = [
    // (t) => ...   and   (t, i) => ...
    /\(\s*(t|tn|N_)\s*(?:,|\)\s*=>)/,
    // const t = ... / let t = ... / var t = ...
    /\b(?:const|let|var)\s+(t|tn|N_)\s*=/,
    // function (t) / function name(t, ...)
    /function\s*[\w$]*\s*\(\s*(t|tn|N_)\s*[,)]/,
    // catch (t)
    /catch\s*\(\s*(t|tn|N_)\s*\)/,
  ];

  const findings = [];
  for (const file of sourceFiles()) {
    const source = withoutComments(readFileSync(join(here, file), 'utf8'));
    source.split('\n').forEach((line, i) => {
      for (const pattern of shadows) {
        const m = pattern.exec(line);
        if (m) findings.push(`${file}:${i + 1} binds ${m[1]}: ${line.trim()}`);
      }
    });
  }

  assert.deepEqual(
    findings,
    [],
    'these bindings shadow an imported translation function - rename them:\n  ' +
      findings.join('\n  '),
  );
});

test('every name imported from app.js is actually exported by it', () => {
  // A module importing a name app.js does not export is a link-time
  // error, not a parse error: `node --check` passes, the build passes,
  // and the browser refuses to evaluate the entire module graph - so
  // the page loses all of its JavaScript, not just that one name.
  // index.js importing a not-yet-re-exported N_ did this, and the whole
  // dashboard rendered untranslated with no visible error.
  const app = readFileSync(join(here, 'app.js'), 'utf8');

  const exported = new Set();
  // Re-export blocks: export { a, b } from './i18n.js';
  for (const m of app.matchAll(/export\s*{([^}]*)}\s*from\s*'[^']+'/g)) {
    for (const name of m[1].split(',')) {
      const clean = name.trim().split(/\s+as\s+/).pop().trim();
      if (clean) exported.add(clean);
    }
  }
  // Direct declarations: export function foo / export const foo.
  for (const m of app.matchAll(/export\s+(?:async\s+)?(?:function\*?|const|let|var|class)\s+([\w$]+)/g)) {
    exported.add(m[1]);
  }

  const missing = [];
  for (const file of sourceFiles()) {
    if (file === 'app.js') continue;
    const source = withoutComments(readFileSync(join(here, file), 'utf8'));
    for (const m of source.matchAll(/import\s*{([^}]*)}\s*from\s*'\.\/app\.js'/g)) {
      for (const name of m[1].split(',')) {
        const clean = name.trim().split(/\s+as\s+/)[0].trim();
        if (clean && !exported.has(clean)) missing.push(`${file} imports ${clean}`);
      }
    }
  }

  assert.deepEqual(missing, [], `app.js does not export these:\n  ${missing.join('\n  ')}`);
});

test('every module that renders text imports the translation functions', () => {
  // A module can call no t() at all and look fine: it just renders
  // English in every language. This does not prove each string is
  // marked, only that the module was wired up at all.
  const exempt = new Set([
    'app.js', // defines the re-exports
    'i18n.js',
    'plural.js',
    'deploys-redirect.js', // redirect only, renders nothing
    'oidc-callback.js', // renders nothing but a spinner from its template
    'code.js', // dispatcher: delegates every render to the two modules it imports
  ]);

  const missing = [];
  for (const file of sourceFiles()) {
    if (exempt.has(file)) continue;
    const source = readFileSync(join(here, file), 'utf8');
    if (!/\bimport\s*{[^}]*\b(t|tn)\b[^}]*}\s*from\s*'\.\/app\.js'/.test(source)) {
      missing.push(file);
    }
  }

  assert.deepEqual(missing, [], `these modules never import t/tn, so they render English in every language: ${missing.join(', ')}`);
});
