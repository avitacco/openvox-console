// Translation for the console's UI.
//
// Messages are keyed by their English source text, the gettext way
// round. A string with no translation therefore renders as correct
// English on its own - there is no key table to drift out of step with
// the code, and no way for a page to render "nodes.title" because
// somebody mistyped a key.
//
// Catalogues are compiled from frontend/locales/*.po into
// dist/locales/<lang>.json at build time (see frontend/i18n).

import { parsePluralForms, DEFAULT_RULE } from './plural.js';
import { AVAILABLE } from './locales.js';

const STORAGE_KEY = 'console.language';

// The key a compiled catalogue stores its own metadata under. Empty
// string is gettext's own convention for the header entry.
const HEADER_KEY = '';

let catalog = {};
let active = 'en';
// How the active language forms plurals. Replaced when a catalogue
// loads; English two-form until then.
let plural = parsePluralForms(DEFAULT_RULE);

/**
 * Translates one string.
 *
 * @param {string} text English source text, which is also its key.
 * @param {Object<string, string|number>} [vars] Values for {name}
 *   placeholders. Interpolation happens after lookup so a translator
 *   sees the placeholder, not a pre-substituted value.
 * @returns {string}
 */
export function t(text, vars) {
  const hit = catalog[text];
  // A plural entry is an array; asking for it with t() is a mistake in
  // the caller, and returning "one,two" would be a baffling bug to
  // chase. Fall back to the English source instead.
  let out = typeof hit === 'string' ? hit : text;
  if (vars) {
    for (const [key, value] of Object.entries(vars)) {
      out = out.replaceAll(`{${key}}`, String(value));
    }
  }
  return out;
}

/**
 * Marks a string for extraction without translating it here.
 *
 * Needed wherever the English text is written somewhere the catalogue
 * cannot be consulted yet - a module-level table, say, evaluated before
 * any catalogue is loaded. N_() records the string for the extractor
 * and returns it unchanged; the actual lookup happens later, with t().
 *
 * Without it such a string is invisible to extraction, never reaches a
 * translator, and renders in English while everything around it is
 * translated - a failure with no error attached to it.
 *
 * The name is gettext's own convention for this.
 *
 * @param {string} text
 * @returns {string} text, unchanged.
 */
export function N_(text) {
  return text;
}

/**
 * Translates a string that varies with a count.
 *
 * The number of forms and how a count maps to one come from the
 * catalogue's own Plural-Forms header, so Russian's three forms and
 * Chinese's single form are both handled - see plural.js.
 *
 * @param {string} one   English singular, and the catalogue key.
 * @param {string} many  English plural, used when no catalogue applies.
 * @param {number} count Decides the form.
 * @param {Object<string, string|number>} [vars] {name} placeholders.
 * @returns {string}
 */
export function tn(one, many, count, vars) {
  const forms = catalog[one];
  let out;
  if (Array.isArray(forms) && forms.length > 0) {
    // The catalogue's own rule decides which form - Russian picks a
    // different one at 1, 3 and 11, and Chinese has only ever one.
    const index = plural.select(count);
    out = forms[index] !== undefined ? forms[index] : forms[forms.length - 1];
  } else {
    // No translation: fall back to the English pair, which follows
    // English's rule, not the target language's.
    out = count === 1 ? one : many;
  }
  const all = { count, ...(vars || {}) };
  for (const [key, value] of Object.entries(all)) {
    out = out.replaceAll(`{${key}}`, String(value));
  }
  return out;
}

/**
 * Formats a date and time in the language the console is showing.
 *
 * Not plain toLocaleString(): that follows the *browser's* locale, so a
 * user who picked German on an en-US browser would read German labels
 * beside 6/16/2026, 9:12:00 AM. The chosen language is what the rest of
 * the page is in, so dates follow it too.
 *
 * @param {string|number|Date} value Anything Date accepts.
 * @param {Intl.DateTimeFormatOptions} [options]
 * @returns {string} Empty string for a missing or unparseable value,
 *   so a caller can interpolate it without guarding.
 */
export function formatDateTime(value, options) {
  if (value === null || value === undefined || value === '') return '';
  const date = value instanceof Date ? value : new Date(value);
  if (Number.isNaN(date.getTime())) return '';
  return date.toLocaleString(active, options);
}

/** As formatDateTime, but the date only. */
export function formatDate(value, options) {
  if (value === null || value === undefined || value === '') return '';
  const date = value instanceof Date ? value : new Date(value);
  if (Number.isNaN(date.getTime())) return '';
  return date.toLocaleDateString(active, options);
}

/** As formatDateTime, but the time only. */
export function formatTime(value, options) {
  if (value === null || value === undefined || value === '') return '';
  const date = value instanceof Date ? value : new Date(value);
  if (Number.isNaN(date.getTime())) return '';
  return date.toLocaleTimeString(active, options);
}

/**
 * Formats a number in the language the console is showing - thousands
 * separators differ (1,234 against 1.234), and following the browser
 * while the page is in another language is the same mismatch as dates.
 */
export function formatNumber(value, options) {
  if (value === null || value === undefined || value === '') return '';
  const n = typeof value === 'number' ? value : Number(value);
  if (Number.isNaN(n)) return '';
  return n.toLocaleString(active, options);
}

/**
 * The display label for a status or kind the API reports.
 *
 * The API deliberately stays English (its values are matched on by
 * scripts and appear in logs), so the mapping to display text lives
 * here. An unknown value is shown as-is rather than blanked: a status
 * this build does not know about is still information.
 */
export function statusLabel(value) {
  if (!value) return '';
  const known = {
    succeeded: N_('Succeeded'),
    failed: N_('Failed'),
    running: N_('Running'),
    pending: N_('Pending'),
    changed: N_('Changed'),
    unchanged: N_('Unchanged'),
    noop: N_('Noop'),
    skipped: N_('Skipped'),
    corrected: N_('Corrected'),
    connected: N_('Connected'),
    'not-connected': N_('Not connected'),
    signed: N_('Signed'),
    requested: N_('Requested'),
    revoked: N_('Revoked'),
    unknown: N_('Unknown'),
  };
  const source = known[String(value).toLowerCase()];
  return source ? t(source) : String(value);
}

/**
 * The display label for a job's kind.
 *
 * Separate from statusLabel, and "Puppet run" rather than "Run",
 * because the msgid is the English text: a bare "Run" here is the same
 * string as the Run button, and a translator seeing one entry cannot
 * give the noun and the verb different words. German wants "Puppet-Run"
 * in this column and "Ausführen" on the button.
 *
 * gettext solves this generally with msgctxt, which this catalogue does
 * not implement. Until it does, a collision is resolved by making the
 * English distinct - which here also reads better than "run".
 */
export function kindLabel(value) {
  const known = {
    run: N_('Puppet run'),
    task: N_('Task'),
    plan: N_('Plan'),
  };
  const source = known[String(value || '').toLowerCase()];
  return source ? t(source) : String(value || '');
}

/** The language currently in effect. */
export function language() {
  return active;
}

/** Every language the console can be shown in, English included. */
export function languages() {
  return ['en', ...AVAILABLE];
}

/**
 * The value meaning "no stored choice - follow the browser".
 *
 * A distinct sentinel rather than an empty string, so that clearing the
 * preference is an explicit act in the calling code rather than
 * something that happens to fall out of a falsy value.
 */
export const AUTO = 'auto';

/**
 * The explicit choice the user has stored, or null if they have none.
 */
export function storedLanguage() {
  try {
    const stored = localStorage.getItem(STORAGE_KEY);
    if (stored && (stored === 'en' || AVAILABLE.includes(stored))) return stored;
  } catch (e) {
    // Private windows and blocked site data both throw. No stored
    // choice is the right reading, not an error.
  }
  return null;
}

/**
 * The language the browser asks for, as far as we can serve it.
 *
 * Exported because the preferences page shows what "automatic" would
 * currently resolve to - "Automatic (Deutsch)" tells the user what they
 * are choosing, where a bare "Automatic" leaves them guessing.
 */
export function browserLanguage() {
  for (const tag of navigator.languages || [navigator.language || 'en']) {
    // Match on the primary subtag, so de-AT gets the German catalogue
    // rather than falling through to English.
    const primary = String(tag).toLowerCase().split('-')[0];
    if (AVAILABLE.includes(primary)) return primary;
    if (primary === 'en') return 'en';
  }
  return 'en';
}

/**
 * The language to use: an explicit choice if the user has made one,
 * otherwise the best match for what the browser asks for, otherwise
 * English.
 */
export function preferredLanguage() {
  return storedLanguage() || browserLanguage();
}

/**
 * Records an explicit choice, or clears it with AUTO so the console goes
 * back to following the browser. Callers reload to apply it.
 */
export function setLanguage(lang) {
  try {
    if (lang === AUTO) {
      localStorage.removeItem(STORAGE_KEY);
    } else {
      localStorage.setItem(STORAGE_KEY, lang);
    }
  } catch (e) {
    // Nothing to do: the choice will not persist, but the reload still
    // applies it for this page.
  }
}

/**
 * Loads the catalogue for `lang` and applies it to the document.
 *
 * Called before the page is revealed (see app.js), so a translated page
 * never flashes English first.
 */
export async function load(lang) {
  active = lang || 'en';
  document.documentElement.setAttribute('lang', active);

  if (active === 'en') {
    catalog = {};
    plural = parsePluralForms(DEFAULT_RULE);
    return;
  }

  try {
    const response = await fetch(`/locales/${active}.json`, { cache: 'no-cache' });
    if (!response.ok) throw new Error(`HTTP ${response.status}`);
    catalog = await response.json();
    const header = catalog[HEADER_KEY] || {};
    plural = parsePluralForms(header['plural-forms']);
  } catch (e) {
    // A missing or broken catalogue must not take the console down with
    // it. English is always a correct rendering, so fall back to it and
    // say so once rather than failing the page.
    console.warn(`Could not load the ${active} translation; showing English.`, e);
    catalog = {};
    plural = parsePluralForms(DEFAULT_RULE);
    active = 'en';
    document.documentElement.setAttribute('lang', 'en');
  }
}

/**
 * Translates the static markup of the current document.
 *
 * Two markings, matching what the extractor looks for:
 *   <h1 data-i18n>Nodes</h1>
 *   <vox-input data-i18n-attr="label" label="Node name">
 *
 * The English text stays in the HTML as the source string, so a page
 * with JavaScript disabled, or a catalogue that failed to load, still
 * reads correctly.
 */
export function translateDocument(root = document) {
  // Components that copy their light DOM into a shadow root need telling
  // when that light DOM changes; see resyncClonedSlots below.
  const needResync = new Set();

  for (const el of root.querySelectorAll('[data-i18n]')) {
    const source = el.getAttribute('data-i18n') || el.textContent.trim();
    const translated = t(source);
    if (translated !== el.textContent) {
      el.textContent = translated;
      const host = el.closest('vox-select');
      if (host) needResync.add(host);
    }
  }

  for (const el of root.querySelectorAll('[data-i18n-attr]')) {
    for (const name of el.getAttribute('data-i18n-attr').split(',')) {
      const attr = name.trim();
      if (!attr) continue;
      const source = el.getAttribute(attr);
      if (!source) continue;
      const translated = t(source);
      if (translated !== source) el.setAttribute(attr, translated);
    }
  }

  resyncClonedSlots(needResync);
}

/**
 * Makes vox-select notice that its options were translated.
 *
 * vox-select does not slot its <option> children - it clones them into
 * a <select> inside its shadow root, and re-clones on slotchange.
 * Changing an option's text is a character-data mutation, which does
 * not fire slotchange, so the clone keeps the English text and the
 * dropdown stays English while the label beside it is translated.
 *
 * Re-appending the same option elements is a child-list mutation, which
 * does fire slotchange. The nodes are moved rather than recreated, so
 * nothing loses state.
 */
function resyncClonedSlots(hosts) {
  for (const host of hosts) {
    const options = [...host.children];
    if (options.length) host.append(...options);
  }
}
