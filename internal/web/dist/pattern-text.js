// The text of a task allowlist field and the list it stands for. Apart
// from task-patterns.js so that it can be tested without a browser.

/** The patterns in a field: one per line, blank lines dropped. */
export function parsePatterns(text) {
  return String(text || '')
    .split(/\r?\n/)
    .map((line) => line.trim())
    .filter(Boolean);
}

/** The text of a field for a list of patterns. */
export function formatPatterns(patterns) {
  return (patterns || []).join('\n');
}
