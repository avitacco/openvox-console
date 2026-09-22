// Shared helpers for the console frontend. Plain ES modules, no bundler -
// see design.md (phase-1-inventory-and-reporting) for why.

import { load as i18nLoad, preferredLanguage, translateDocument, t, tn, N_ } from './i18n.js';

// Re-exported so a page module imports one thing: `import { t } from
// './app.js'` alongside the helpers it already takes from here. `t` is
// also imported above, because a re-export does not bind the name in
// this module's own scope and the shared helpers below call it.
export {
  t,
  tn,
  N_,
  language,
  languages,
  setLanguage,
  preferredLanguage,
  storedLanguage,
  browserLanguage,
  AUTO,
  formatDateTime,
  formatDate,
  formatTime,
  statusLabel,
  kindLabel,
  formatNumber,
} from './i18n.js';

// Pairs with the head-blocking script in every page's <head>: that
// script hides <body> until data-vox-ready is set below (or a timeout
// elapses), so the correct dark/light background and the translated
// text are both in place before first paint instead of flashing the
// wrong one. Every page module imports this file, so this runs exactly
// once per page load.
//
// The catalogue is awaited at module scope, not inside the
// DOMContentLoaded handler below, and that placement is the whole
// point: every page module imports this file, so a top-level await here
// blocks their evaluation until the catalogue is in place.
//
// Loading it in the handler instead left t() racing the page's own
// fetches - whichever resolved first decided the language. The
// dashboard lost that race in practice and rendered its stat labels in
// English on a Japanese page, with nothing to re-render them.
//
// The cost is one small same-origin fetch before the first page module
// runs, and none at all for English, which returns without fetching.
try {
  await i18nLoad(preferredLanguage());
} catch (e) {
  // i18nLoad already falls back to English internally; this only
  // catches something unforeseen, and a dead page is worse than an
  // untranslated one.
  console.warn('Translation failed; showing English.', e);
}

function applyTranslations() {
  try {
    // Needs the parsed document; the catalogue itself is already loaded
    // by the time anything can call t().
    translateDocument();
  } catch (e) {
    console.warn('Applying translations to the document failed.', e);
  } finally {
    document.documentElement.setAttribute('data-vox-ready', '');
  }
}

/**
 * Runs `fn` once the document is parsed, whether or not that has already
 * happened.
 *
 * Every use of DOMContentLoaded in this module has to go through here.
 * The top-level await above defers this module's evaluation past the
 * event whenever the catalogue is actually fetched - which is every
 * language except English, since English returns without a fetch. A bare
 * addEventListener therefore works in English and silently never fires
 * in the other fourteen: that is how the sidenav's permission-gated
 * items (Jobs, Code, Activity, Admin, Vulnerabilities) came to be
 * missing from every translated page while English looked perfect.
 */
export function onDocumentReady(fn) {
  if (document.readyState === 'loading') {
    document.addEventListener('DOMContentLoaded', fn);
  } else {
    fn();
  }
}

onDocumentReady(applyTranslations);

const ACCESS_TOKEN_KEY = 'console.accessToken';
const REFRESH_TOKEN_KEY = 'console.refreshToken';
const REFRESH_MARGIN_MS = 60_000; // refresh this long before actual expiry

export function getAccessToken() {
  return localStorage.getItem(ACCESS_TOKEN_KEY);
}

export function getRefreshToken() {
  return localStorage.getItem(REFRESH_TOKEN_KEY);
}

export function setTokens(accessToken, refreshToken) {
  localStorage.setItem(ACCESS_TOKEN_KEY, accessToken);
  localStorage.setItem(REFRESH_TOKEN_KEY, refreshToken);
  scheduleRefresh();
}

export function clearTokens() {
  localStorage.removeItem(ACCESS_TOKEN_KEY);
  localStorage.removeItem(REFRESH_TOKEN_KEY);
}

function decodeJWTPayload(token) {
  try {
    const payload = token.split('.')[1];
    const base64 = payload.replace(/-/g, '+').replace(/_/g, '/');
    return JSON.parse(atob(base64));
  } catch {
    return null;
  }
}

// Reads the current access token's claims client-side, for UX decisions
// (nav visibility, redirecting away from admin pages) - not a security
// boundary, since the server independently enforces every permission
// check on the API itself.
export function getClaims() {
  const token = getAccessToken();
  if (!token) return null;
  return decodeJWTPayload(token);
}

export function hasPermission(permission) {
  const claims = getClaims();
  return !!claims && Array.isArray(claims.permissions) && claims.permissions.includes(permission);
}

let refreshTimer;

// Schedules a proactive refresh shortly before the current access token's
// exp claim - so a session survives past its original expiry without the
// user being prompted to log in again, rather than only reacting to a 401
// after the token has already expired.
function scheduleRefresh() {
  clearTimeout(refreshTimer);
  const token = getAccessToken();
  if (!token) return;
  const claims = decodeJWTPayload(token);
  if (!claims || !claims.exp) return;

  const delay = Math.max(claims.exp * 1000 - Date.now() - REFRESH_MARGIN_MS, 0);
  refreshTimer = setTimeout(() => {
    refreshAccessToken().catch(() => redirectToLogin());
  }, delay);
}

async function refreshAccessToken() {
  const refreshToken = getRefreshToken();
  if (!refreshToken) throw new Error('no refresh token');

  const res = await fetch('/api/v1/auth/refresh', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ refreshToken }),
  });
  if (!res.ok) throw new Error('refresh failed');

  const data = await res.json();
  setTokens(data.accessToken, data.refreshToken);
  return data.accessToken;
}

function redirectToLogin() {
  clearTimeout(refreshTimer);
  clearTokens();
  window.location.href = '/login.html';
}

export function logout() {
  const token = getAccessToken();
  clearTimeout(refreshTimer);
  clearTokens();
  if (token) {
    fetch('/api/v1/auth/logout', {
      method: 'POST',
      headers: { Authorization: `Bearer ${token}` },
    }).catch(() => {});
  }
  window.location.href = '/login.html';
}

// Every page's fetchJSON call attaches the access token and, on a 401,
// tries exactly one refresh-and-retry before giving up and sending the
// user to the login page - this is what "wires every existing page's
// fetch calls" without touching each page's own JS.
export async function fetchJSON(url, options = {}) {
  const attempt = async () => {
    const token = getAccessToken();
    const headers = { ...(options.headers || {}) };
    if (token) headers.Authorization = `Bearer ${token}`;
    // no-store: every response here is live, permission-gated data, and
    // this API sends no Cache-Control/ETag of its own - without this,
    // Chromium's default heuristic caching can serve a stale response
    // for an identical URL requested again shortly after (confirmed
    // live in add-node-deletion: a just-deleted node kept reappearing
    // in a reloaded list because of exactly this, invisible before
    // since nothing previously needed to see its own write reflected
    // immediately).
    return fetch(url, { ...options, headers, cache: 'no-store' });
  };

  let res = await attempt();

  if (res.status === 401) {
    try {
      await refreshAccessToken();
      res = await attempt();
    } catch {
      redirectToLogin();
      throw new Error('session expired');
    }
  }

  if (res.status === 401) {
    redirectToLogin();
    throw new Error('session expired');
  }

  if (!res.ok) {
    let message = `request failed: ${res.status}`;
    let fields;
    try {
      const body = await res.json();
      if (body && body.error) message = body.error;
      // Per-field validation messages, where an endpoint provides them
      // (e.g. vulnerability provider configuration).
      if (body && body.fields) fields = body.fields;
    } catch {
      // response wasn't JSON; keep the generic message
    }
    const err = new Error(message);
    err.status = res.status;
    if (fields) err.fields = fields;
    throw err;
  }
  if (res.status === 204) return null;
  return res.json();
}

// Convenience wrapper for POST/PUT bodies - fetchJSON handles the rest
// (auth header, refresh-and-retry, error extraction, 204-no-body
// responses).
export function sendJSON(url, method, body) {
  return fetchJSON(url, {
    method,
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(body),
  });
}

// The fixed permission set the backend defines (see cmd/console/main.go's
// allPermissions) - shared by the roles and service-tokens admin pages'
// permission pickers. There is no endpoint to discover this dynamically;
// it's a fixed, small set, not user-extensible.
export const ALL_PERMISSIONS = [
  'nodes:read',
  'nodes:certs:manage',
  'classifier:read',
  'classifier:write',
  'enc:read',
  'rbac:admin',
  'activity:read',
  'code:deploy',
  'code:read',
  'orchestrator:read',
  'orchestrator:run',
  'vulnerabilities:read',
  'vulnerabilities:manage',
];

// Redirects to / if the current session lacks the given permission - used
// by the admin pages. This is a UX convenience: the server independently
// enforces the same permission on every admin endpoint these pages call.
export function requirePermission(permission) {
  if (!hasPermission(permission)) {
    window.location.href = '/';
    return false;
  }
  return true;
}

export function qs(name) {
  return new URLSearchParams(window.location.search).get(name);
}

// How long a fetch has to be in flight before a spinner appears. A
// spinner that flashes for 80ms and vanishes reads as a glitch rather
// than as feedback, and most of these requests return faster than that
// against a local console - so nothing is shown until the wait is long
// enough to be worth acknowledging.
const LOADING_DELAY_MS = 150;

// withLoading runs work, showing a vox-loader in el if it is still
// running after LOADING_DELAY_MS, and returns whatever work returns.
//
// It wraps the work rather than handing back a cancel function so that
// clearing the timer cannot be forgotten on one branch: a timer that
// survives its fetch fires later and wipes out content that has already
// been rendered. Wrap the fetch, not the render, so the spinner covers
// exactly the wait:
//
//   const page = await withLoading(results, () => fetchJSON(url));
//   render(page);
export async function withLoading(el, work, label = 'Loading') {
  if (!el) return work();
  const timer = setTimeout(() => {
    el.innerHTML = `<div class="loading-pane"><vox-loader label="${escapeHtml(label)}"></vox-loader></div>`;
  }, LOADING_DELAY_MS);
  try {
    return await work();
  } finally {
    clearTimeout(timer);
  }
}

// Populates a filter <vox-select> (id has a leading "" All option
// already in the markup) with real recorded values from url, e.g.
// activity's /api/v1/audit-log/categories or code-deploys' .../refs -
// these are effectively enums (a bounded, discrete set of values) but
// not ones defined anywhere as a fixed list, so the option list has to
// come from what's actually been recorded rather than a hardcoded
// guess. Leaves just "All" if the fetch fails - the filter still works,
// it just won't have pre-populated choices.
export async function loadFilterOptions(selectEl, url) {
  try {
    const values = await fetchJSON(url);
    for (const value of values) {
      const option = document.createElement('option');
      option.value = value;
      option.textContent = value;
      selectEl.appendChild(option);
    }
  } catch {
    // leave just the "All" option
  }
}

// confirmDialog shows a vox-dialog modal in place of window.confirm(),
// resolving true only when the confirm button was clicked - every other
// exit (cancel button, backdrop click via light-dismiss, Escape, or the
// dialog's own built-in close button) routes through the same vox-close
// event and resolves false, since only the confirm click ever sets
// `confirmed`. body is trusted HTML (matching this codebase's existing
// innerHTML-template convention elsewhere, e.g. certActionsCell,
// showActionError) - callers must escapeHtml() any interpolated value.
export function confirmDialog({ heading, body, confirmLabel = 'Confirm', danger = false }) {
  return new Promise((resolve) => {
    let confirmed = false;
    const dialog = document.createElement('vox-dialog');
    dialog.heading = heading;
    dialog.setAttribute('light-dismiss', '');
    dialog.innerHTML = `
      ${body}
      <div slot="footer">
        <vox-button variant="alt" data-action="cancel">${t('Cancel')}</vox-button>
        <vox-button variant="${danger ? 'danger' : 'brand'}" data-action="confirm">${escapeHtml(confirmLabel)}</vox-button>
      </div>`;
    dialog.addEventListener('vox-close', () => {
      dialog.remove();
      resolve(confirmed);
    });
    dialog.querySelector('[data-action="cancel"]').addEventListener('click', () => dialog.close());
    dialog.querySelector('[data-action="confirm"]').addEventListener('click', () => {
      confirmed = true;
      dialog.close();
    });
    document.body.appendChild(dialog);
    dialog.show();
  });
}

export function escapeHtml(s) {
  return String(s).replace(/[&<>"']/g, (c) => ({
    '&': '&amp;',
    '<': '&lt;',
    '>': '&gt;',
    '"': '&quot;',
    "'": '&#39;',
  }[c]));
}

// Returns null when SubtleCrypto isn't available rather than throwing.
// crypto.subtle is only exposed in a secure context - HTTPS, or
// http://localhost during development - so on a console served over
// plain http it is undefined. That's a cosmetic avatar's problem alone,
// and it must not surface as a failure of the page around it: callers
// treat a null hash exactly like "no email", falling back to initials.
async function sha256Hex(text) {
  if (!globalThis.crypto?.subtle) return null;
  const bytes = new TextEncoder().encode(text);
  const digest = await crypto.subtle.digest('SHA-256', bytes);
  return [...new Uint8Array(digest)].map((b) => b.toString(16).padStart(2, '0')).join('');
}

// Resolves to true only if url actually loads an image - Gravatar's own
// default (a generic silhouette) is avoided with d=404 in gravatarURL,
// so "doesn't load" reliably means "this email has no Gravatar", not a
// network hiccup we should also treat as absent. Callers fall back to
// initials in that case, same as <vox-avatar> does when src is unset.
function imageLoads(url) {
  return new Promise((resolve) => {
    const img = new Image();
    img.onload = () => resolve(true);
    img.onerror = () => resolve(false);
    img.src = url;
  });
}

// Builds a Gravatar URL from email (see https://docs.gravatar.com -
// trim + lowercase, then SHA-256, not the legacy MD5 hash). Returns null
// for no email, or when the email can't be hashed here (see sha256Hex),
// so callers can skip the lookup entirely.
export async function gravatarURL(email, size = 80) {
  if (!email) return null;
  const hash = await sha256Hex(email.trim().toLowerCase());
  if (!hash) return null;
  return `https://www.gravatar.com/avatar/${hash}?s=${size}&d=404`;
}

export function initialsFor(firstName, lastName, username) {
  if (firstName || lastName) {
    return `${(firstName || '').charAt(0)}${(lastName || '').charAt(0)}`.toUpperCase();
  }
  return (username || '').slice(0, 2).toUpperCase();
}

// Populates a <vox-avatar> element from a profile - a real Gravatar
// image when the email has one, initials otherwise. Used for the
// header's own avatar (from the current session's claims) and the users
// admin table (one per row, from each listed user).
export async function applyAvatar(el, { firstName, lastName, email, username }) {
  el.initials = initialsFor(firstName, lastName, username);
  el.alt = [firstName, lastName].filter(Boolean).join(' ') || username || '';
  const url = await gravatarURL(email);
  if (url && (await imageLoads(url))) {
    el.src = url;
  }
}

const STATUS_VARIANTS = {
  success: 'tip',
  succeeded: 'tip',
  changed: 'tip',
  unchanged: 'neutral',
  failed: 'danger',
  failure: 'danger',
  noop: 'warning',
  running: 'warning',
};

export function statusVariant(status) {
  if (!status) return 'neutral';
  return STATUS_VARIANTS[status] || 'neutral';
}

// Renders a job's target node(s) from the lightweight targetCount/
// targetPreview fields ListJobs returns (see internal/orchestrator's
// Store.ListJobs - the full per-target detail isn't loaded for list
// views). A single-target job links straight to that node; a fan-out
// job shows a few certnames plus how many more there were.
export function targetSummaryHTML(job) {
  const preview = job.targetPreview || [];
  if (preview.length === 0) return '';

  const nodeLink = (certname) => `<a href="/node.html?name=${encodeURIComponent(certname)}">${escapeHtml(certname)}</a>`;

  if (job.targetCount === 1) return nodeLink(preview[0]);

  const shown = preview.map(nodeLink).join(', ');
  const more = job.targetCount - preview.length;
  return more > 0 ? `${shown}${tn(', +{count} more', ', +{count} more', more)}` : shown;
}

// Same summary as targetSummaryHTML, but plain text - for contexts like
// a title="" tooltip where markup can't render.
export function targetSummaryText(job) {
  const preview = job.targetPreview || [];
  if (preview.length === 0) return '';
  if (job.targetCount === 1) return preview[0];

  const shown = preview.join(', ');
  const more = job.targetCount - preview.length;
  return more > 0 ? `${shown}${tn(', +{count} more', ', +{count} more', more)}` : shown;
}

// value has already been decoded from JSON by fetchJSON (res.json()), so
// this only needs to decide how to *display* it - not parse it again.
export function formatValue(value) {
  if (value === undefined || value === null) return '';
  if (typeof value === 'string') return value;
  return JSON.stringify(value, null, 2);
}

// Builds a <vox-pagination> block for a paginated API response
// ({items, page, pageSize, total} - see internal/pagination). Returns ''
// when everything fits on one page, so callers can splice this straight
// into their results.innerHTML template without a conditional. Links
// carry a real ?page= href (so open-in-new-tab/bookmarking still does
// something sane) but callers must call bindPagination() after setting
// innerHTML to intercept clicks and re-fetch instead of navigating -
// this app never does full-page reloads for state changes.
export function paginationHTML(page, pageSize, total) {
  const totalPages = Math.max(1, Math.ceil(total / pageSize));
  if (totalPages <= 1) return '';

  const link = (targetPage, label, current) =>
    `<a href="?page=${targetPage}" data-page="${targetPage}"${current ? ' aria-current="page"' : ''}>${label}</a>`;

  const windowSize = 2;
  const pages = new Set([1, totalPages]);
  for (let p = page - windowSize; p <= page + windowSize; p++) {
    if (p >= 1 && p <= totalPages) pages.add(p);
  }
  const sorted = [...pages].sort((a, b) => a - b);

  let items = '';
  if (page > 1) items += link(page - 1, '←', false);
  let prevPage = 0;
  for (const p of sorted) {
    if (prevPage && p - prevPage > 1) items += '<span aria-hidden="true">&hellip;</span>';
    items += link(p, String(p), p === page);
    prevPage = p;
  }
  if (page < totalPages) items += link(page + 1, '→', false);

  return `<vox-pagination class="vox-m-top-lg" label="${t('Pagination')}">${items}</vox-pagination>`;
}

// Intercepts clicks on the links paginationHTML() rendered inside
// container, calling onNavigate(targetPage) instead of letting the
// browser navigate.
export function bindPagination(container, onNavigate) {
  container.querySelectorAll('vox-pagination a[data-page]').forEach((a) => {
    a.addEventListener('click', (ev) => {
      ev.preventDefault();
      onNavigate(Number(a.dataset.page));
    });
  });
}

// Side effects on import, so every page that uses app.js gets them for
// free: schedule a proactive refresh if a session already exists, and
// wire up a logout button if the page has one (id="logout-button").
scheduleRefresh();
onDocumentReady(() => {
  document.getElementById('logout-button')?.addEventListener('click', logout);
  if (hasPermission('rbac:admin')) {
    const adminLink = document.getElementById('nav-admin-link');
    if (adminLink) adminLink.style.display = '';
  }
  if (hasPermission('activity:read')) {
    const activityLink = document.getElementById('nav-activity-link');
    if (activityLink) activityLink.style.display = '';
  }
  if (hasPermission('code:read')) {
    const codeLink = document.getElementById('nav-code-link');
    if (codeLink) codeLink.style.display = '';
  }
  if (hasPermission('orchestrator:read')) {
    const jobsLink = document.getElementById('nav-jobs-link');
    if (jobsLink) jobsLink.style.display = '';
  }
  if (hasPermission('vulnerabilities:read')) {
    const vulnerabilitiesLink = document.getElementById('nav-vulnerabilities-link');
    if (vulnerabilitiesLink) vulnerabilitiesLink.style.display = '';
  }

  const avatarEl = document.getElementById('user-avatar');
  const claims = getClaims();
  if (avatarEl && claims) {
    // Fetches the current profile rather than reading firstName/lastName/
    // email off the token: those claims are only as fresh as the last
    // login/refresh, so editing your profile in Preferences and then
    // navigating elsewhere would still show the pre-edit avatar/initials
    // until the access token happened to be reissued. /api/v1/me is
    // always current. Falls back to the token's username-only initials
    // if the request itself fails, rather than showing nothing.
    fetchJSON('/api/v1/me')
      .then((me) => applyAvatar(avatarEl, { firstName: me.firstName, lastName: me.lastName, email: me.email, username: me.username }))
      .catch(() => applyAvatar(avatarEl, { username: claims.sub }));
  }

  // Footer version - see layout.html.tmpl. Absent on the header-less
  // pages (login, oidc-callback), hence the optional chaining.
  const footerVersionEl = document.getElementById('footer-version');
  if (footerVersionEl) {
    fetchJSON('/api/v1/version')
      .then((v) => { footerVersionEl.textContent = v.version; })
      .catch(() => { footerVersionEl.textContent = 'unknown'; });
  }
});

// Severity badges for vulnerability views (add-vulnerability-tracking).
// Badges have no "critical" variant, so critical and high share danger and
// the label carries the difference.
const SEVERITY_VARIANTS = { critical: 'danger', high: 'danger', medium: 'warning', low: 'neutral', unknown: 'neutral' };

// The API's severity values stay English (scripts match on them), so
// the display label is mapped here rather than title-casing the raw
// value - which rendered "Critical" on a Japanese page.
const SEVERITY_LABELS = {
  critical: N_('Critical'),
  high: N_('High'),
  medium: N_('Medium'),
  low: N_('Low'),
  unknown: N_('Unknown'),
};

export function severityBadge(severity) {
  const s = severity || 'unknown';
  const label = SEVERITY_LABELS[s];
  const text = label ? t(label) : s.charAt(0).toUpperCase() + s.slice(1);
  return `<vox-badge variant="${SEVERITY_VARIANTS[s] || 'neutral'}">${escapeHtml(text)}</vox-badge>`;
}

// Why a vulnerability provider didn't assess a node, in words.
const COVERAGE_REASONS = {
  unsupported_os: 'operating system not supported by this provider',
  no_package_data: 'no package inventory reported',
  agent_upgrade_required: 'node agent needs upgrading to report accurate package data',
  not_seen_by_tenable: 'no matching Tenable asset',
  not_yet_synced: 'provider has not synced yet',
};

export function coverageReasonText(reason) {
  return COVERAGE_REASONS[reason] || reason || 'not assessed';
}

const CLOSE_REASONS = { fixed: 'fixed', provider_disabled: 'provider disabled', node_removed: 'node removed' };

export function closeReasonText(reason) {
  return CLOSE_REASONS[reason] || reason || 'closed';
}

// Only https links from provider data are rendered as links, so a
// malformed or hostile reference URL can never become a javascript: link.
export function safeLink(url, label) {
  if (typeof url !== 'string' || !url.startsWith('https://')) return escapeHtml(label);
  return `<a href="${escapeHtml(url)}" target="_blank" rel="noopener noreferrer">${escapeHtml(label)}</a>`;
}
