import { fetchJSON, sendJSON, escapeHtml, hasPermission, qs, withLoading, t } from './app.js';

const id = qs('id');
const isEdit = Boolean(id);

const nameEl = document.getElementById('name');
const priorityEl = document.getElementById('priority');
const environmentEl = document.getElementById('environment');
const classesEl = document.getElementById('classes');
const parametersEl = document.getElementById('parameters');
const ruleEl = document.getElementById('rule');
const ruleBuilderEl = document.getElementById('rule-builder');
const ruleAddButton = document.getElementById('rule-add-condition');
const ruleJsonToggle = document.getElementById('rule-json-toggle');
const ruleErrorEl = document.getElementById('rule-error');
const pinsEl = document.getElementById('pins');
const errorEl = document.getElementById('form-error');
const saveButton = document.getElementById('save');
const deleteButton = document.getElementById('delete');
const matchingNodesSection = document.getElementById('matching-nodes-section');
const matchingNodesEl = document.getElementById('matching-nodes');
const runNowButton = document.getElementById('run-now');
const runNowError = document.getElementById('run-now-error');

let matchingCertnames = [];

// Guards against saving an empty form over a real group: every field is
// populated by loadForEdit's fetch, so if that fetch never succeeded
// (network error, console restarting, expired token) the form still
// holds its blank defaults - and saving would write those over the
// stored classes, parameters, rule, and pins with no warning.
let loadedForEdit = false;

function showError(message) {
  errorEl.innerHTML = `<vox-alert variant="danger">${escapeHtml(message)}</vox-alert>`;
}

function parseJSONField(el, fallback) {
  const raw = el.value.trim();
  if (raw === '') return fallback;
  return JSON.parse(raw);
}

// Match-rule builder - see design.md in add-match-rule-builder-ui.
// ruleRows is the live state while the builder view is showing; the
// JSON textarea (ruleEl) is only ever a projection of it, regenerated
// on every switch to JSON view, never kept in sync live - see
// design.md's "Builder state is the source of truth" decision.
const RULE_OPERATORS = [
  { value: '=', label: '=' },
  { value: '!=', label: '!=' },
  { value: '~', label: '~ (regex)' },
  { value: '>', label: '>' },
  { value: '<', label: '<' },
  { value: '>=', label: '>=' },
  { value: '<=', label: '<=' },
];
let ruleRows = [];

function operatorHelpText(operator) {
  if (operator === '~') {
    return 'Regular expression (RE2 syntax) - matches if found anywhere in the value, not anchored to the whole value.';
  }
  if (['>', '<', '>=', '<='].includes(operator)) {
    return 'Value must parse as a number.';
  }
  return '';
}

function renderRuleRows() {
  if (ruleRows.length === 0) {
    ruleBuilderEl.innerHTML = '';
    return;
  }

  ruleBuilderEl.innerHTML = ruleRows
    .map(
      (_, i) => `
      <div class="rule-condition vox-border-all vox-radius-md vox-bg-soft vox-p-all-md vox-m-bottom-md" data-index="${i}">
        <div class="vox-m-bottom-md">
          <vox-input class="rule-fact-path" label="${t('Fact path')}" placeholder="${t('os.family')}"></vox-input>
        </div>
        <div class="vox-display-flex vox-gap-sm">
          <vox-select class="rule-operator" label="${t('Operator')}">
            ${RULE_OPERATORS.map((op) => `<option value="${escapeHtml(op.value)}">${escapeHtml(op.label)}</option>`).join('')}
          </vox-select>
          <vox-input class="rule-value" label="${t('Value')}"></vox-input>
          <div class="rule-remove-wrap">
            <vox-button class="rule-remove" variant="danger" size="sm" data-index="${i}">${t('Remove')}</vox-button>
          </div>
        </div>
        <p class="rule-help vox-ts-sm vox-m-top-sm vox-m-bottom-none" data-index="${i}"></p>
        <div class="rule-row-error" data-index="${i}"></div>
      </div>`
    )
    .join('');

  ruleBuilderEl.querySelectorAll('.rule-condition').forEach((rowEl) => {
    const i = Number(rowEl.dataset.index);
    const cond = ruleRows[i];
    const factPathInput = rowEl.querySelector('.rule-fact-path');
    const operatorSelect = rowEl.querySelector('.rule-operator');
    const valueInput = rowEl.querySelector('.rule-value');
    const helpEl = ruleBuilderEl.querySelector(`.rule-help[data-index="${i}"]`);

    factPathInput.value = cond.factPath;
    operatorSelect.value = cond.operator;
    valueInput.value = cond.value;
    helpEl.textContent = operatorHelpText(cond.operator);

    factPathInput.addEventListener('input', () => {
      ruleRows[i].factPath = factPathInput.value;
    });
    valueInput.addEventListener('input', () => {
      ruleRows[i].value = valueInput.value;
    });
    operatorSelect.addEventListener('change', () => {
      ruleRows[i].operator = operatorSelect.value;
      helpEl.textContent = operatorHelpText(operatorSelect.value);
    });
    rowEl.querySelector('.rule-remove').addEventListener('click', () => {
      ruleRows.splice(i, 1);
      renderRuleRows();
    });
  });
}

ruleAddButton.addEventListener('click', () => {
  ruleRows.push({ factPath: '', operator: '=', value: '' });
  renderRuleRows();
});

function showRuleBuilderView() {
  ruleBuilderEl.style.display = '';
  ruleAddButton.style.display = '';
  ruleEl.style.display = 'none';
}

function showRuleJsonView() {
  ruleBuilderEl.style.display = 'none';
  ruleAddButton.style.display = 'none';
  ruleEl.style.display = '';
}

// Only one of {ruleRows, ruleEl's text} is ever "live" at a time,
// matching which view is showing - see design.md. Switching to JSON
// view always succeeds (it's the escape hatch for anything the
// builder can't model - see task 4.3). Switching back to builder view
// can fail (invalid JSON) - see task 3.3 - in which case the JSON text
// is left untouched and the toggle stays in JSON view.
ruleJsonToggle.addEventListener('change', () => {
  ruleErrorEl.innerHTML = '';

  if (ruleJsonToggle.checked) {
    ruleEl.value = JSON.stringify(ruleRows, null, 2);
    showRuleJsonView();
    return;
  }

  let parsed;
  try {
    const raw = ruleEl.value.trim();
    parsed = raw === '' ? [] : JSON.parse(raw);
    if (!Array.isArray(parsed)) throw new Error('match rule must be a JSON array');
  } catch (err) {
    ruleErrorEl.innerHTML = `<vox-alert variant="danger">Invalid JSON: ${escapeHtml(err.message)}</vox-alert>`;
    ruleJsonToggle.checked = true;
    return;
  }

  ruleRows = parsed.map((c) => ({ factPath: c.factPath ?? '', operator: c.operator ?? '=', value: c.value ?? '' }));
  showRuleBuilderView();
  renderRuleRows();
});

function isValidRegex(pattern) {
  try {
    return Boolean(new RegExp(pattern));
  } catch {
    return false;
  }
}

// Validates builder-view row state only - see task 4.3/design.md: JSON
// view is the escape hatch and is never blocked by this, relying
// instead on parseJSONField's existing JSON.parse failure path.
function validateRuleRows() {
  const rowErrors = new Map();
  ruleRows.forEach((cond, i) => {
    if (!cond.factPath.trim()) {
      rowErrors.set(i, 'Fact path is required.');
    } else if (cond.operator === '~' && !isValidRegex(cond.value)) {
      rowErrors.set(i, 'Value is not a valid regular expression.');
    }
  });
  return rowErrors;
}

function clearRuleRowErrors() {
  ruleBuilderEl.querySelectorAll('.rule-row-error').forEach((el) => {
    el.innerHTML = '';
  });
}

function showRuleRowErrors(rowErrors) {
  rowErrors.forEach((message, i) => {
    const el = ruleBuilderEl.querySelector(`.rule-row-error[data-index="${i}"]`);
    if (el) el.innerHTML = `<vox-alert variant="danger">${escapeHtml(message)}</vox-alert>`;
  });
}

function openIfPopulated(disclosureId, value) {
  const populated = Array.isArray(value) ? value.length > 0 : Object.keys(value || {}).length > 0;
  if (populated) document.getElementById(disclosureId).open = true;
}

async function loadForEdit() {
  document.getElementById('heading').textContent = 'Edit group';
  document.getElementById('breadcrumb-current').textContent = 'Edit group';
  deleteButton.style.display = '';
  matchingNodesSection.style.display = '';

  try {
    const g = await fetchJSON(`/api/v1/groups/${encodeURIComponent(id)}`);
    nameEl.value = g.name;
    priorityEl.value = String(g.priority);
    environmentEl.value = g.environment || '';
    classesEl.value = JSON.stringify(g.classes || [], null, 2);
    parametersEl.value = JSON.stringify(g.parameters || {}, null, 2);
    ruleRows = (g.rule || []).map((c) => ({ factPath: c.factPath ?? '', operator: c.operator ?? '=', value: c.value ?? '' }));
    renderRuleRows();
    pinsEl.value = JSON.stringify(g.pins || [], null, 2);

    // These fields sit behind a disclosure so they don't dominate the
    // page - but a group that actually has classes, parameters, or pins
    // shouldn't hide them from someone opening it to edit.
    openIfPopulated('classes-disclosure', g.classes);
    openIfPopulated('parameters-disclosure', g.parameters);
    openIfPopulated('pins-disclosure', g.pins);

    loadedForEdit = true;
  } catch (err) {
    showError(err.message);
  }

  loadMatchingNodes();
}

async function loadMatchingNodes() {
  try {
    const resp = await withLoading(matchingNodesEl, () => fetchJSON(`/api/v1/groups/${encodeURIComponent(id)}/nodes`));
    matchingCertnames = resp.certnames || [];

    if (matchingCertnames.length === 0) {
      matchingNodesEl.innerHTML = `<vox-empty-state heading="${t('No nodes currently match this group')}"></vox-empty-state>`;
    } else {
      const items = matchingCertnames
        .map((certname) => `<li><a href="/node.html?name=${encodeURIComponent(certname)}">${escapeHtml(certname)}</a></li>`)
        .join('');
      matchingNodesEl.innerHTML = `<ul>${items}</ul>`;
    }

    if (hasPermission('orchestrator:run') && matchingCertnames.length > 0) {
      runNowButton.style.display = '';
    }
  } catch (err) {
    matchingNodesEl.innerHTML = `<vox-alert variant="danger">${escapeHtml(err.message)}</vox-alert>`;
  }
}

runNowButton.addEventListener('click', async () => {
  runNowError.innerHTML = '';
  runNowButton.disabled = true;
  try {
    const job = await sendJSON('/api/v1/orchestrator/runs', 'POST', { targets: matchingCertnames });
    window.location.href = `/job.html?id=${job.id}`;
  } catch (err) {
    runNowError.innerHTML = `<vox-alert variant="danger">${escapeHtml(err.message)}</vox-alert>`;
  } finally {
    runNowButton.disabled = false;
  }
});

async function save() {
  errorEl.innerHTML = '';
  ruleErrorEl.innerHTML = '';
  clearRuleRowErrors();

  if (isEdit && !loadedForEdit) {
    showError("This group hasn't loaded yet, so saving now would overwrite it with empty values. Reload the page and try again.");
    return;
  }

  let rule;
  if (ruleJsonToggle.checked) {
    // JSON view is the escape hatch - validated only by JSON.parse
    // below, same as classes/parameters/pins. See task 4.3/design.md.
  } else {
    const rowErrors = validateRuleRows();
    if (rowErrors.size > 0) {
      showRuleRowErrors(rowErrors);
      showError('Fix the highlighted match-rule condition(s) before saving.');
      return;
    }
    rule = ruleRows.map((c) => ({ factPath: c.factPath.trim(), operator: c.operator, value: c.value }));
  }

  let body;
  try {
    body = {
      name: nameEl.value.trim(),
      priority: Number(priorityEl.value),
      environment: environmentEl.value.trim() || null,
      classes: parseJSONField(classesEl, []),
      parameters: parseJSONField(parametersEl, {}),
      rule: ruleJsonToggle.checked ? parseJSONField(ruleEl, []) : rule,
      pins: parseJSONField(pinsEl, []),
    };
  } catch (err) {
    showError(`Invalid JSON in one of the fields: ${err.message}`);
    return;
  }

  if (!body.name) {
    showError('Name is required.');
    return;
  }
  if (!Number.isFinite(body.priority)) {
    showError('Priority must be a number.');
    return;
  }

  try {
    if (isEdit) {
      await sendJSON(`/api/v1/groups/${encodeURIComponent(id)}`, 'PUT', body);
    } else {
      await sendJSON('/api/v1/groups', 'POST', body);
    }
    window.location.href = '/groups.html';
  } catch (err) {
    showError(err.message);
  }
}

async function remove() {
  try {
    await sendJSON(`/api/v1/groups/${encodeURIComponent(id)}`, 'DELETE');
    window.location.href = '/groups.html';
  } catch (err) {
    showError(err.message);
  }
}

saveButton.addEventListener('click', save);
deleteButton.addEventListener('click', remove);

if (isEdit) {
  loadForEdit();
}
