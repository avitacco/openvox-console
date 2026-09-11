import { fetchJSON, sendJSON, escapeHtml, applyAvatar } from './app.js';

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
    passwordSuccess.innerHTML = `<vox-alert variant="tip">Password changed.</vox-alert>`;
  } catch (err) {
    passwordError.innerHTML = `<vox-alert variant="danger">${escapeHtml(err.message)}</vox-alert>`;
  }
});

load();
