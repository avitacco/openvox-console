// A role's or service token's task allowlist, as the Roles and Service
// tokens pages show and edit it (see specs/task-permissions's "Console
// UI for task permissions").
//
// The console is the authority on what a pattern may be; this only
// turns the text of a field into a list and a list into badges.

import { escapeHtml, t } from './app.js';

export { parsePatterns, formatPatterns } from './pattern-text.js';

/** What a list of patterns says: each as a badge, or "Every task". */
export function patternBadges(patterns) {
  if (!patterns || patterns.length === 0) {
    return `<vox-badge variant="tip">${escapeHtml(t('Every task'))}</vox-badge>`;
  }
  return patterns.map((p) => `<vox-badge variant="neutral">${escapeHtml(p)}</vox-badge>`).join(' ');
}

/**
 * Shows a refusal of the patterns on the field it concerns, with what
 * was entered left alone. Returns false when err is not about the
 * patterns, so the caller can show it where it shows any other error.
 *
 * @param {HTMLElement} place where the reason goes, beside the field
 * @param {Error & {fields?: Record<string, string>}} err
 */
export function showPatternError(place, err) {
  if (!err || !err.fields || !err.fields.taskPatterns) return false;
  const message = err.fields.taskPatterns;
  place.innerHTML = `<vox-alert variant="danger">${escapeHtml(message)}</vox-alert>`;
  return true;
}
