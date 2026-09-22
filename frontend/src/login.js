import { setTokens, escapeHtml, t } from './app.js';

const usernameEl = document.getElementById('username');
const passwordEl = document.getElementById('password');
const errorEl = document.getElementById('form-error');
const button = document.getElementById('login-button');

async function login() {
  errorEl.innerHTML = '';

  const username = usernameEl.value;
  const password = passwordEl.value;

  try {
    const res = await fetch('/api/v1/auth/login', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ username, password }),
    });

    if (!res.ok) {
      errorEl.innerHTML = `<vox-alert variant="danger">${t('Invalid username or password.')}</vox-alert>`;
      return;
    }

    const data = await res.json();
    setTokens(data.accessToken, data.refreshToken);
    window.location.href = '/';
  } catch (err) {
    errorEl.innerHTML = `<vox-alert variant="danger">${escapeHtml(err.message)}</vox-alert>`;
  }
}

button.addEventListener('click', login);
for (const el of [usernameEl, passwordEl]) {
  el.addEventListener('keydown', (e) => {
    if (e.key === 'Enter') login();
  });
}

// The OIDC option is shown only when the console actually has a
// provider configured - unconfigured means every OIDC endpoint 503s, so
// there's nothing useful for the button to link to.
fetch('/api/v1/auth/methods')
  .then((res) => res.json())
  .then((methods) => {
    if (methods.oidc) {
      document.getElementById('oidc-login').style.display = '';
    }
  })
  .catch(() => {});
