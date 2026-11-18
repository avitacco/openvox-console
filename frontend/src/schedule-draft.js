// Carries a half-filled run or task from a dialog to the schedule form
// ("Schedule instead"). The draft lives in sessionStorage for the one
// navigation: it holds what was typed, which can include a task's
// sensitive value, so it is removed as soon as the form has read it and
// is never put in the URL.

const KEY = 'console.scheduleDraft';

/**
 * Opens the schedule form filled in with draft.
 *
 * @param {object} draft the fields of a schedule that are known so far:
 *   kind, and any of options, task, environment, params, timeoutSeconds,
 *   targets, query, group.
 */
export function scheduleInstead(draft) {
  try {
    sessionStorage.setItem(KEY, JSON.stringify(draft));
  } catch {
    // Without storage the form simply opens empty.
  }
  window.location.href = '/schedule-edit.html?draft=1';
}

/** Returns the draft left by scheduleInstead, once, or null. */
export function takeDraft() {
  try {
    const raw = sessionStorage.getItem(KEY);
    sessionStorage.removeItem(KEY);
    return raw ? JSON.parse(raw) : null;
  } catch {
    return null;
  }
}
