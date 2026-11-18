// The account policy page: password rules and login lockout for local
// accounts, for an administrator (see specs/account-policy).

import { fetchJSON, sendJSON, escapeHtml, requirePermission, withLoading, t, formatDateTime } from './app.js';

if (requirePermission('rbac:admin')) {
  const form = document.getElementById('policy-form');
  const formError = document.getElementById('form-error');
  const saved = document.getElementById('saved');
  const minLength = document.getElementById('min-length');
  const kinds = {
    requireLowercase: document.getElementById('require-lowercase'),
    requireUppercase: document.getElementById('require-uppercase'),
    requireDigit: document.getElementById('require-digit'),
    requireSymbol: document.getElementById('require-symbol'),
  };
  const lockoutOn = document.getElementById('lockout-on');
  const threshold = document.getElementById('lockout-threshold');
  const minutes = document.getElementById('lockout-minutes');
  const saveButton = document.getElementById('save');

  // What the threshold was before lockout was turned off in the form,
  // so that turning it back on restores it.
  let lastThreshold = 10;

  function showLockoutFields() {
    document.getElementById('lockout-fields').style.display = lockoutOn.checked ? '' : 'none';
  }

  function show(policy) {
    minLength.value = String(policy.minLength);
    for (const [name, el] of Object.entries(kinds)) el.checked = Boolean(policy[name]);
    lockoutOn.checked = policy.lockoutThreshold > 0;
    if (policy.lockoutThreshold > 0) lastThreshold = policy.lockoutThreshold;
    threshold.value = String(lastThreshold);
    minutes.value = String(policy.lockoutMinutes);
    showLockoutFields();
    document.getElementById('last-changed').textContent = policy.updatedBy
      ? t('Last changed by {user} on {when}.', { user: policy.updatedBy, when: formatDateTime(policy.updatedAt) })
      : t('These are the defaults: the policy has not been changed.');
  }

  function clearErrors() {
    formError.innerHTML = '';
    saved.innerHTML = '';
    form.querySelectorAll('[data-error-for]').forEach((el) => {
      el.innerHTML = '';
    });
  }

  lockoutOn.addEventListener('change', showLockoutFields);

  saveButton.addEventListener('click', async () => {
    clearErrors();
    const body = {
      minLength: Number(minLength.value),
      lockoutThreshold: lockoutOn.checked ? Number(threshold.value) : 0,
      lockoutMinutes: Number(minutes.value),
    };
    for (const [name, el] of Object.entries(kinds)) body[name] = el.checked;
    saveButton.disabled = true;
    try {
      show(await sendJSON('/api/v1/account-policy', 'PUT', body));
      saved.innerHTML = `<vox-alert variant="success">${escapeHtml(t('Saved. The policy is in force now, on every console instance.'))}</vox-alert>`;
    } catch (err) {
      // The console words a refusal as the rest of a sentence.
      const message = err.message.charAt(0).toUpperCase() + err.message.slice(1);
      const place = (err.field && form.querySelector(`[data-error-for="${CSS.escape(err.field)}"]`)) || formError;
      place.innerHTML = `<vox-alert variant="danger">${escapeHtml(message)}</vox-alert>`;
      place.scrollIntoView({ block: 'center', behavior: 'smooth' });
    } finally {
      saveButton.disabled = false;
    }
  });

  async function load() {
    try {
      const policy = await withLoading(document.getElementById('loading'), () => fetchJSON('/api/v1/account-policy'));
      show(policy);
      form.style.display = '';
    } catch (err) {
      formError.innerHTML = `<vox-alert variant="danger">${escapeHtml(err.message)}</vox-alert>`;
    }
  }

  load();
}
