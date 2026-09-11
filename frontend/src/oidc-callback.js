import { setTokens, escapeHtml } from './app.js';

// The server delivers tokens via the URL fragment (never sent to the
// server, not logged - see design.md's "Token delivery" decision in the
// oidc-authentication change), exactly mirroring what login.js does
// after a password login.
const params = new URLSearchParams(window.location.hash.slice(1));
const accessToken = params.get('access_token');
const refreshToken = params.get('refresh_token');

if (accessToken && refreshToken) {
  setTokens(accessToken, refreshToken);
  // Clear the fragment so the tokens don't linger in browser history.
  history.replaceState(null, '', window.location.pathname);
  window.location.href = '/';
} else {
  document.getElementById('form-error').innerHTML =
    `<vox-alert variant="danger">${escapeHtml('OIDC sign-in failed: no tokens received.')}</vox-alert>`;
}
