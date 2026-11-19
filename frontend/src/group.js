import { fetchJSON, sendJSON, escapeHtml, hasPermission, qs, withLoading, t } from './app.js';
import { openRunTaskDialog } from './run-task.js';
import { openRunPuppetDialog } from './run-puppet.js';
import { createClassEditor } from './class-editor.js';

const id = qs('id');
const isEdit = Boolean(id);

const nameEl = document.getElementById('name');
const priorityEl = document.getElementById('priority');
const parentEl = document.getElementById('parent');
const parentFieldEl = document.getElementById('parent-field');
const ruleMatchEl = document.getElementById('rule-match');
const matchingSectionEl = document.getElementById('matching-section');
const warningsEl = document.getElementById('override-warnings');
const rootNoteEl = document.getElementById('root-note');
const pinsOutsideEl = document.getElementById('pins-outside');
// The environment is a choice - not set, one the server reports, or
// "Other" with a typed name. environmentEl is that typed name, shown only
// for Other; read and write it through getEnvironment/setEnvironment.
const environmentEl = document.getElementById('environment');
const environmentChoiceEl = document.getElementById('environment-choice');
const OTHER_ENVIRONMENT = '\u0000other';
let knownEnvironments = [];
const classEditor = createClassEditor({ root: document.getElementById('classes-editor') });
const parametersEl = document.getElementById('parameters');
const configDataEl = document.getElementById('config-data');
const configDataErrorEl = document.getElementById('config-data-error');
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
const runTaskButton = document.getElementById('run-task');

let matchingCertnames = [];

// Every group's own-or-inherited environment, by group id, from the
// group list - what a group with no environment of its own takes from
// the parent chosen for it.
const inheritedEnvironment = new Map();

// The environment this group's classes are looked up in: its own, else
// the one its parent passes down, else production. Recomputed, and the
// class list reloaded, whenever the environment or the parent changes.
function refreshClassEnvironment() {
  const own = getEnvironment();
  const inherited = isRoot ? '' : inheritedEnvironment.get(Number(parentEl.value)) || '';
  return classEditor.setEnvironment(own || inherited || 'production');
}

function getEnvironment() {
  return environmentChoiceEl.value === OTHER_ENVIRONMENT ? environmentEl.value.trim() : environmentChoiceEl.value;
}

function setEnvironment(name) {
  const known = name === '' || knownEnvironments.includes(name);
  environmentEl.value = known ? '' : name;
  renderEnvironmentChoices(known ? name : OTHER_ENVIRONMENT);
}

// Rebuilds the dropdown: "not set" first (saying what the group then
// inherits), the server's environments, then Other. A saved name the
// server does not list stays selectable, as Other with the name filled.
function renderEnvironmentChoices(selected) {
  const inherited = isRoot ? '' : inheritedEnvironment.get(Number(parentEl.value)) || '';
  const unset = inherited ? t('Not set (inherits {env})', { env: inherited }) : t('Not set');
  environmentChoiceEl.innerHTML =
    `<option value="">${escapeHtml(unset)}</option>` +
    knownEnvironments.map((e) => `<option value="${escapeHtml(e)}">${escapeHtml(e)}</option>`).join('') +
    `<option value="${OTHER_ENVIRONMENT}">${escapeHtml(t('Other...'))}</option>`;
  environmentChoiceEl.value = selected;
  environmentChoiceEl.requestUpdate?.();
  environmentEl.closest('.native-field').hidden = selected !== OTHER_ENVIRONMENT;
}

// The server's environments, offered in the dropdown. A name not among
// them is still accepted through Other - the environment may not be
// deployed yet - and no list at all just leaves Other to type in.
async function loadEnvironmentOptions() {
  try {
    const { environments } = await fetchJSON('/api/v1/classes/environments');
    knownEnvironments = environments || [];
  } catch {
    knownEnvironments = [];
  }
  setEnvironment(getEnvironment());
}

// True once the group being edited is known to be the built-in All
// Nodes group, whose name, parent, rule and pins are fixed.
let isRoot = false;
// The pinned certnames as last loaded or saved, for marking the ones
// that fall outside the parent.
let savedPins = [];

// Guards against saving an empty form over a real group: every field is
// populated by loadForEdit's fetch, so if that fetch never succeeded
// (network error, console restarting, expired token) the form still
// holds its blank defaults - and saving would write those over the
// stored classes, parameters, rule, and pins with no warning.
let loadedForEdit = false;

// The reason a save was refused, shown on the configuration data field
// with its disclosure opened, so it is not hidden behind a closed one.
function showConfigDataError(message) {
  document.getElementById('config-data-disclosure').open = true;
  configDataErrorEl.innerHTML = `<vox-alert variant="danger">${escapeHtml(message)}</vox-alert>`;
  configDataErrorEl.scrollIntoView({ behavior: 'smooth', block: 'center' });
}

function showError(message) {
  errorEl.innerHTML = `<vox-alert variant="danger">${escapeHtml(message)}</vox-alert>`;
}

// Shows mode ('all' or 'any') in the match-mode select.
//
// The options are rebuilt each time rather than only the value being
// set: vox-select copies which option is selected into its shadow
// <select> when its options change, so setting `value` alone on a
// select that has already rendered leaves the old choice on screen
// while the property - and so what gets saved - says otherwise.
function setRuleMatch(mode) {
  ruleMatchEl.innerHTML = `
    <option value="all">${escapeHtml(t('all of the conditions'))}</option>
    <option value="any">${escapeHtml(t('any one of the conditions'))}</option>`;
  ruleMatchEl.value = mode === 'any' ? 'any' : 'all';
  ruleMatchEl.requestUpdate?.();
}
setRuleMatch('all');

// Offers every group as a parent except the one being edited and the
// groups beneath it - choosing one of those would make a loop, which
// the console refuses anyway.
async function loadParents(selectedId) {
  let groups;
  try {
    groups = (await fetchJSON('/api/v1/groups?all=true')).items;
  } catch (err) {
    showError(err.message);
    return;
  }

  const excluded = new Set();
  if (isEdit) {
    excluded.add(Number(id));
    let grew = true;
    while (grew) {
      grew = false;
      for (const g of groups) {
        if (g.parentId != null && excluded.has(g.parentId) && !excluded.has(g.id)) {
          excluded.add(g.id);
          grew = true;
        }
      }
    }
  }

  for (const g of groups) inheritedEnvironment.set(g.id, g.classEnvironment || '');

  const root = groups.find((g) => g.isRoot);
  const options = groups
    .filter((g) => !excluded.has(g.id))
    .sort((a, b) => Number(b.isRoot) - Number(a.isRoot) || a.name.localeCompare(b.name))
    .map((g) => `<option value="${g.id}">${escapeHtml(g.name)}</option>`)
    .join('');
  parentEl.innerHTML = options;
  const chosen = selectedId ?? root?.id;
  if (chosen != null) parentEl.value = String(chosen);
  parentEl.requestUpdate?.();
}

parentEl.addEventListener('change', () => {
  renderEnvironmentChoices(environmentChoiceEl.value);
  refreshClassEnvironment();
});
environmentEl.addEventListener('change', refreshClassEnvironment);
environmentChoiceEl.addEventListener('change', () => {
  environmentEl.closest('.native-field').hidden = environmentChoiceEl.value !== OTHER_ENVIRONMENT;
  if (environmentChoiceEl.value === OTHER_ENVIRONMENT) environmentEl.focus();
  refreshClassEnvironment();
});

function warningText(w) {
  const vars = { ancestor: w.ancestorName, key: w.key, class: w.class };
  if (w.kind === 'environment') return t('a different environment from {ancestor}', vars);
  if (w.kind === 'parameter') return t('top-scope parameter {key}, which {ancestor} sets differently', vars);
  return t('parameter {key} of class {class}, which {ancestor} sets differently', vars);
}

// Shows where the hierarchy has overridden the priorities: the values
// this group sets against a higher-priority parent, and - after saving a
// parent - the groups beneath it that now do the same to it. Nothing is
// wrong with either; it is said so the priorities are not misread.
function renderOverrides(warnings, overriddenBy) {
  let html = '';
  if (warnings && warnings.length > 0) {
    const items = warnings.map((w) => `<li>${escapeHtml(warningText(w))}</li>`).join('');
    html += `
      <vox-alert variant="warning">
        <strong>${t('This group overrides a parent with a higher priority.')}</strong>
        ${t("Its value is the one used, because a group always wins over the groups above it. Priority alone would have chosen the parent's. This group sets:")}
        <ul>${items}</ul>
      </vox-alert>`;
  }
  if (overriddenBy && overriddenBy.length > 0) {
    const items = overriddenBy
      .map((o) => `<li><a href="/group.html?id=${o.groupId}">${escapeHtml(o.groupName)}</a></li>`)
      .join('');
    html += `
      <vox-alert variant="warning">
        <strong>${t('Groups beneath this one override it despite their lower priority.')}</strong>
        ${t('Their values are the ones used for their nodes:')}
        <ul>${items}</ul>
      </vox-alert>`;
  }
  warningsEl.innerHTML = html;
}

// The configuration data field: a JSON object (an empty field is {}).
// Anything else is refused on the field before a request is made.
function parseConfigData() {
  const data = parseJSONField(configDataEl, {});
  if (data === null || typeof data !== 'object' || Array.isArray(data)) {
    const err = new Error(t('Configuration data must be a JSON object: key/value pairs inside { }.'));
    err.configData = true;
    throw err;
  }
  return data;
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
    isRoot = Boolean(g.isRoot);
    if (isRoot) {
      // All Nodes has no parent, priority, rule or pins to edit, and
      // cannot be renamed or deleted.
      nameEl.disabled = true;
      parentFieldEl.style.display = 'none';
      priorityEl.style.display = 'none';
      matchingSectionEl.style.display = 'none';
      deleteButton.style.display = 'none';
      rootNoteEl.style.display = '';
    } else {
      priorityEl.value = String(g.priority);
      await loadParents(g.parentId);
    }
    setRuleMatch(g.ruleMatch);
    savedPins = g.pins || [];
    renderOverrides(g.warnings, null);
    setEnvironment(g.environment || '');
    // The class list first, so each stored value gets the control its
    // parameter's type calls for.
    await refreshClassEnvironment();
    classEditor.setClasses(g.classes || [], g.classWarnings);
    parametersEl.value = JSON.stringify(g.parameters || {}, null, 2);
    configDataEl.value = JSON.stringify(g.configData || {}, null, 2);
    ruleRows = (g.rule || []).map((c) => ({ factPath: c.factPath ?? '', operator: c.operator ?? '=', value: c.value ?? '' }));
    renderRuleRows();
    pinsEl.value = JSON.stringify(g.pins || [], null, 2);

    // These fields sit behind a disclosure so they don't dominate the
    // page - but a group that actually has classes, parameters, or pins
    // shouldn't hide them from someone opening it to edit.
    openIfPopulated('parameters-disclosure', g.parameters);
    openIfPopulated('config-data-disclosure', g.configData);
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
    renderPinsOutside(resp.pinsOutsideGroup || []);

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
      runTaskButton.style.display = '';
    }
  } catch (err) {
    matchingNodesEl.innerHTML = `<vox-alert variant="danger">${escapeHtml(err.message)}</vox-alert>`;
    // Without node data there is no telling which pins take effect, so
    // none is marked and the page says why.
    if (savedPins.length > 0) {
      pinsOutsideEl.innerHTML = `<p class="vox-ts-sm vox-color-text-2">${t('Whether each pinned node is currently in this group is unavailable.')}</p>`;
      document.getElementById('pins-disclosure').open = true;
    }
  }
}

// A pin overrides this group's own rule but not its parent: a pinned
// node the parent does not match is not in the group. Said here so a
// pin with no effect is visible rather than silent.
function renderPinsOutside(certnames) {
  if (certnames.length === 0) {
    pinsOutsideEl.innerHTML = '';
    return;
  }
  const items = certnames.map((certname) => `<li>${escapeHtml(certname)}</li>`).join('');
  pinsOutsideEl.innerHTML = `
    <vox-alert variant="warning">
      ${t('These pinned nodes are not currently in this group, because they are not in its parent group (or are not known to the console):')}
      <ul>${items}</ul>
    </vox-alert>`;
  document.getElementById('pins-disclosure').open = true;
}

runTaskButton.addEventListener('click', () => openRunTaskDialog({ targets: matchingCertnames, group: Number(id) }));

runNowButton.addEventListener('click', () => {
  runNowError.innerHTML = '';
  openRunPuppetDialog({ targets: matchingCertnames, group: Number(id) });
});

async function save() {
  errorEl.innerHTML = '';
  configDataErrorEl.innerHTML = '';
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
      environment: getEnvironment() || null,
      classes: classEditor.getClasses(),
      parameters: parseJSONField(parametersEl, {}),
      configData: parseConfigData(),
      rule: ruleJsonToggle.checked ? parseJSONField(ruleEl, []) : rule,
      ruleMatch: ruleMatchEl.value || 'all',
      pins: parseJSONField(pinsEl, []),
    };
    if (isRoot) {
      // The fixed fields are sent as they stand; the console refuses
      // anything else for All Nodes.
      delete body.priority;
      body.rule = [];
      body.pins = [];
    } else if (parentEl.value) {
      body.parentId = Number(parentEl.value);
    }
  } catch (err) {
    if (err.configData) {
      showConfigDataError(err.message);
      return;
    }
    showError(err.classEditor ? err.message : `Invalid JSON in one of the fields: ${err.message}`);
    return;
  }

  if (!body.name) {
    showError('Name is required.');
    return;
  }
  if (!isRoot && !Number.isFinite(body.priority)) {
    showError('Priority must be a number.');
    return;
  }

  try {
    const saved = isEdit
      ? await sendJSON(`/api/v1/groups/${encodeURIComponent(id)}`, 'PUT', body)
      : await sendJSON('/api/v1/groups', 'POST', body);

    // A save that leaves the hierarchy overriding the priorities stays
    // on the group so the warning is seen, instead of going back to the
    // list where it is only a badge.
    // Likewise a save that leaves classes or parameters not matching
    // the environment's code: the group page is where they are marked.
    const overrides = (saved.warnings || []).length > 0 || (saved.overriddenBy || []).length > 0;
    const mismatches = (saved.classWarnings || []).length > 0;
    if (!overrides && !mismatches) {
      window.location.href = '/groups.html';
      return;
    }
    if (!isEdit) {
      window.location.href = `/group.html?id=${saved.id}`;
      return;
    }
    savedPins = saved.pins || [];
    renderOverrides(saved.warnings, saved.overriddenBy);
    classEditor.setClasses(saved.classes || [], saved.classWarnings);
    (overrides ? warningsEl : document.getElementById('classes-editor')).scrollIntoView({ behavior: 'smooth', block: 'center' });
    loadMatchingNodes();
  } catch (err) {
    if (err.fields && err.fields.configData) {
      showConfigDataError(err.fields.configData);
      return;
    }
    showError(err.message);
  }
}

async function remove() {
  try {
    await sendJSON(`/api/v1/groups/${encodeURIComponent(id)}`, 'DELETE');
    window.location.href = '/groups.html';
  } catch (err) {
    // A role's permissions apply to this group's nodes; the server
    // refuses rather than silently take that access away.
    if (err.status === 409 && /granted to one or more roles/.test(err.message)) {
      showError(t('This group is granted to one or more roles. Remove it from those roles on the Roles page before deleting it.'));
      return;
    }
    showError(err.message);
  }
}

saveButton.addEventListener('click', save);
deleteButton.addEventListener('click', remove);

renderEnvironmentChoices('');
loadEnvironmentOptions();
if (isEdit) {
  loadForEdit();
} else {
  loadParents().then(refreshClassEnvironment);
}
