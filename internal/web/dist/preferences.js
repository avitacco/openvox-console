import { LANGUAGE_NAMES } from './locales.js';
import {
  fetchJSON,
  sendJSON,
  escapeHtml,
  applyAvatar,
  t,
  languages,
  setLanguage,
  storedLanguage,
  browserLanguage,
  AUTO,
} from './app.js';

const avatarEl = document.getElementById('preferences-avatar');
const usernameEl = document.getElementById('preferences-username');
const firstNameEl = document.getElementById('first-name');
const lastNameEl = document.getElementById('last-name');
const emailEl = document.getElementById('email');
const profileError = document.getElementById('profile-error');
const currentPasswordEl = document.getElementById('current-password');
const newPasswordEl = document.getElementById('new-password');
const passwordError = document.getElementById('password-error');
const passwordSuccess = document.getElementById('password-success');

let me = null;

async function load() {
  try {
    me = await fetchJSON('/api/v1/me');
    usernameEl.textContent = me.username;
    firstNameEl.value = me.firstName || '';
    lastNameEl.value = me.lastName || '';
    emailEl.value = me.email || '';
    await applyAvatar(avatarEl, { firstName: me.firstName, lastName: me.lastName, email: me.email, username: me.username });
  } catch (err) {
    profileError.innerHTML = `<vox-alert variant="danger">${escapeHtml(err.message)}</vox-alert>`;
  }
}

document.getElementById('save-profile').addEventListener('click', async () => {
  profileError.innerHTML = '';
  try {
    await sendJSON('/api/v1/me', 'PUT', {
      firstName: firstNameEl.value.trim(),
      lastName: lastNameEl.value.trim(),
      email: emailEl.value.trim(),
    });
    await load();
  } catch (err) {
    profileError.innerHTML = `<vox-alert variant="danger">${escapeHtml(err.message)}</vox-alert>`;
  }
});

document.getElementById('save-password').addEventListener('click', async () => {
  passwordError.innerHTML = '';
  passwordSuccess.innerHTML = '';
  const currentPassword = currentPasswordEl.value;
  const newPassword = newPasswordEl.value;
  if (!currentPassword || !newPassword) {
    passwordError.innerHTML = `<vox-alert variant="danger">Current and new password are both required.</vox-alert>`;
    return;
  }
  try {
    await sendJSON('/api/v1/me', 'PUT', { currentPassword, newPassword });
    currentPasswordEl.value = '';
    newPasswordEl.value = '';
    passwordSuccess.innerHTML = `<vox-alert variant="success">Password changed.</vox-alert>`;
  } catch (err) {
    passwordError.innerHTML = `<vox-alert variant="danger">${escapeHtml(err.message)}</vox-alert>`;
  }
});

// --- language ------------------------------------------------------
//
// The choice is per browser rather than per account: it is a rendering
// preference like the theme, it must apply on the login page before any
// account is known, and storing it server-side would mean a round trip
// before the first paint.

const languageEl = document.getElementById('language');
const saveLanguageEl = document.getElementById('save-language');

if (languageEl && saveLanguageEl) {
  // storedLanguage(), not preferredLanguage(): the control has to
  // distinguish "the user chose German" from "the browser happens to
  // ask for German". Only the first should select a named language;
  // the second is what Automatic means.
  const stored = storedLanguage();

  // Naming what Automatic currently resolves to saves the user from
  // guessing which language they are about to get.
  const autoName = LANGUAGE_NAMES[browserLanguage()] || browserLanguage();
  const options = [
    { value: AUTO, name: `${t('Automatic')} (${autoName})`, selected: stored === null },
    ...languages().map((code) => ({
      value: code,
      name: LANGUAGE_NAMES[code] || code,
      selected: code === stored,
    })),
  ];

  // The selected option is marked in the markup rather than by setting
  // .value afterwards: vox-select clones its options into a shadow
  // <select> and, on syncing, writes that select's value back over the
  // host's - so an assignment made before the sync is silently lost and
  // the control shows the first option instead of the active one.
  languageEl.innerHTML = options
    .map(
      (o) =>
        `<option value="${escapeHtml(o.value)}"${o.selected ? ' selected' : ''}>${escapeHtml(o.name)}</option>`,
    )
    .join('');

  saveLanguageEl.addEventListener('click', () => {
    setLanguage(languageEl.value);
    // A reload rather than re-translating in place: the catalogue is
    // applied before first paint, and every already-rendered table was
    // built with the old one.
    window.location.reload();
  });
}

load();
