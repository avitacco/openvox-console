// The account policy's password rules, shown beside a field in which a
// password is set, and marked when the console refuses one (see
// specs/account-policy's "Console UI for the account policy").
//
// The console is the authority: this only says what it will ask for,
// and shows which rules it said a password failed.

import { fetchJSON, escapeHtml, t, tn } from './app.js';

// A rule's code, as a refusal lists it, to the words shown for it.
function ruleText(code, policy) {
  switch (code) {
    case 'min_length':
      return tn('At least {count} character', 'At least {count} characters', policy.minLength);
    case 'lowercase':
      return t('A lowercase letter');
    case 'uppercase':
      return t('An uppercase letter');
    case 'digit':
      return t('A digit');
    case 'symbol':
      return t('A symbol, such as ! or #');
    case 'max_length':
      return t('No longer than 72 bytes');
    case 'username':
      return t('Not the same as the username');
    default:
      return code;
  }
}

// The rules a policy puts on every password, in the order they are
// listed.
function rulesOf(policy) {
  const rules = ['min_length'];
  if (policy.requireLowercase) rules.push('lowercase');
  if (policy.requireUppercase) rules.push('uppercase');
  if (policy.requireDigit) rules.push('digit');
  if (policy.requireSymbol) rules.push('symbol');
  rules.push('username');
  return rules;
}

/**
 * Lists the password rules in root.
 *
 * @param {HTMLElement} root
 * @returns {{showFailed: (codes: string[]) => void, clear: () => void}}
 *   showFailed marks the rules a refusal named; a rule not normally
 *   listed (the 72-byte limit) is added when it is the one that failed.
 */
export function createPasswordRules(root) {
  let policy = null;
  let failed = [];

  function render() {
    if (!policy) {
      root.innerHTML = '';
      return;
    }
    const rules = rulesOf(policy);
    for (const code of failed) {
      if (!rules.includes(code)) rules.push(code);
    }
    root.innerHTML = `
      <ul class="password-rules" aria-label="${escapeHtml(t('Password rules'))}">
        ${rules
          .map((code) => {
            const isFailed = failed.includes(code);
            const mark = isFailed ? `${escapeHtml(t('Not met:'))} ` : '';
            return `<li${isFailed ? ' data-failed' : ''}>${mark}${escapeHtml(ruleText(code, policy))}</li>`;
          })
          .join('')}
      </ul>`;
  }

  // Any signed-in user may read the policy. If it cannot be read the
  // list is simply absent; the console still applies the rules.
  fetchJSON('/api/v1/account-policy')
    .then((p) => {
      policy = p;
      render();
    })
    .catch(() => {});

  return {
    showFailed(codes) {
      failed = codes || [];
      render();
    },
    clear() {
      failed = [];
      render();
    },
  };
}
