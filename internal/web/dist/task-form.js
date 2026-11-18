// A task's environment, name, parameters and timeout as a form: built
// from the picked task's metadata, and read back as the fields of a
// task request. Shared by the Run task dialog and the schedule form.
//
// The server is the authority on whether parameters are valid: the form
// only catches what it can see cheaply (an empty required field, JSON
// that does not parse) and otherwise shows the server's per-parameter
// messages against their fields.

import { fetchJSON, escapeHtml, withLoading, t } from './app.js';

// What the console shows in place of a sensitive value it stored.
const REDACTED = 'Sensitive [value redacted]';

// A parameter's form control, from the input kind the API suggests.
function fieldHTML(param, kept, withoutSensitive) {
  if (withoutSensitive && param.sensitive) {
    // A saved plan never holds a sensitive value: it is asked for when
    // the plan is run.
    return `<p class="vox-ts-sm vox-color-text-2 vox-m-y-none"><strong>${escapeHtml(param.name)}</strong> · ${escapeHtml(
      t('A sensitive value. It is not kept in the plan, and is asked for each time the plan is run.'),
    )}</p>`;
  }
  const name = escapeHtml(param.name);
  const label = escapeHtml(param.input.required ? `${param.name} *` : param.name);
  const noteParts = [param.type];
  if (param.description) noteParts.unshift(param.description);
  if (param.default !== undefined) noteParts.push(t('Default: {value}', { value: JSON.stringify(param.default) }));
  if (kept) noteParts.push(t('A value is stored. Leave this empty to keep it.'));
  const note = escapeHtml(noteParts.join(' · '));
  const common = `data-param="${name}" data-kind="${escapeHtml(param.input.kind)}" label="${label}" note="${note}"`;

  switch (param.input.kind) {
    case 'boolean':
    case 'enum': {
      const options = param.input.kind === 'boolean' ? ['true', 'false'] : param.input.options || [];
      const unset = `<option value="">${escapeHtml(t('Not set'))}</option>`;
      return `<vox-select ${common}>${unset}${options
        .map((o) => `<option value="${escapeHtml(o)}">${escapeHtml(o)}</option>`)
        .join('')}</vox-select>`;
    }
    case 'integer':
      return `<vox-input type="number" step="1" ${common}></vox-input>`;
    case 'number':
      return `<vox-input type="number" step="any" ${common}></vox-input>`;
    case 'string':
      return param.sensitive
        ? `<vox-input type="password" autocomplete="off" ${common}></vox-input>`
        : `<vox-input ${common}></vox-input>`;
    default:
      return `<vox-textarea rows="3" placeholder="${escapeHtml(t('JSON, for example ["a", "b"]'))}" ${common}></vox-textarea>`;
  }
}

function taskPath(name) {
  const [module, task = 'init'] = name.split('::');
  return `/api/v1/tasks/${encodeURIComponent(module)}/${encodeURIComponent(task)}`;
}

/**
 * Builds the task form inside body, reporting problems in errorEl.
 *
 * @param {HTMLElement} body
 * @param {HTMLElement} errorEl
 * @param {object} [options]
 * @param {(ready: boolean) => void} [options.onReady] called when a task
 *   becomes picked or unpicked.
 * @param {() => void} [options.onUnavailable] called when tasks are not
 *   configured on this console.
 * @param {{task?: string, environment?: string, params?: object, timeoutSeconds?: number}} [options.initial]
 *   a saved task to show. A sensitive value stored for it reads as
 *   redacted, and is offered as "leave empty to keep".
 * @param {boolean} [options.withoutSensitive] leave out the fields for
 *   sensitive parameters, saying they are asked for later: the form of
 *   a saved plan's step.
 */
export function createTaskForm(body, errorEl, { onReady = () => {}, onUnavailable = () => {}, initial = null, withoutSensitive = false } = {}) {
  let current = null; // the picked task's details
  let pending = initial; // applied once, when its task loads

  function showError(message) {
    errorEl.innerHTML = message ? `<vox-alert variant="danger">${escapeHtml(message)}</vox-alert>` : '';
  }

  function setCurrent(value) {
    current = value;
    onReady(Boolean(value));
  }

  async function load() {
    let environments;
    try {
      ({ environments } = await withLoading(body, () => fetchJSON('/api/v1/tasks/environments')));
    } catch (err) {
      if (err.status === 503) {
        body.innerHTML = `<vox-alert variant="warning">${escapeHtml(
          t('Tasks are not configured on this console. An administrator sets CONSOLE_PUPPET_SERVER_URL to enable them.'),
        )}</vox-alert>`;
        onUnavailable();
      } else {
        showError(err.message);
      }
      return;
    }
    let first = environments.includes('production') ? 'production' : environments[0] || '';
    if (pending?.environment && environments.includes(pending.environment)) first = pending.environment;
    body.innerHTML = `
      <vox-select data-environment label="${escapeHtml(t('Environment'))}">
        ${environments.map((e) => `<option value="${escapeHtml(e)}"${e === first ? ' selected' : ''}>${escapeHtml(e)}</option>`).join('')}
      </vox-select>
      <div data-task-picker class="vox-m-top-md"></div>
      <div data-task-form class="vox-m-top-md"></div>`;
    const envSelect = body.querySelector('[data-environment]');
    envSelect.value = first;
    envSelect.addEventListener('change', () => {
      pending = null;
      loadTasks(envSelect.value);
    });
    await loadTasks(first);
  }

  async function loadTasks(environment) {
    const picker = body.querySelector('[data-task-picker]');
    const form = body.querySelector('[data-task-form]');
    form.innerHTML = '';
    showError('');
    setCurrent(null);
    let tasks;
    try {
      ({ tasks } = await withLoading(picker, () => fetchJSON(`/api/v1/tasks?environment=${encodeURIComponent(environment)}`)));
    } catch (err) {
      picker.innerHTML = '';
      showError(err.message);
      return;
    }
    // Private tasks are hidden from pickers, as in Bolt and PE.
    const visible = tasks.filter((task) => !task.private);
    if (visible.length === 0) {
      picker.innerHTML = `<vox-empty-state heading="${escapeHtml(t('No tasks in this environment'))}"></vox-empty-state>`;
      return;
    }
    picker.innerHTML = `
      <vox-select data-task label="${escapeHtml(t('Task'))}">
        <option value="">${escapeHtml(t('Choose a task'))}</option>
        ${visible.map((task) => `<option value="${escapeHtml(task.name)}">${escapeHtml(task.name)}</option>`).join('')}
      </vox-select>`;
    const taskSelect = picker.querySelector('[data-task]');
    taskSelect.addEventListener('change', (ev) => {
      pending = null;
      loadTask(environment, ev.target.value);
    });
    if (pending?.task) {
      if (visible.some((task) => task.name === pending.task)) {
        taskSelect.value = pending.task;
        await loadTask(environment, pending.task);
      } else {
        showError(t('The task {task} is no longer in the {environment} environment. Choose another.', { task: pending.task, environment }));
        pending = null;
      }
    }
  }

  async function loadTask(environment, name) {
    const form = body.querySelector('[data-task-form]');
    showError('');
    setCurrent(null);
    if (!name) {
      form.innerHTML = '';
      return;
    }
    let task;
    try {
      task = await withLoading(form, () => fetchJSON(`${taskPath(name)}?environment=${encodeURIComponent(environment)}`));
    } catch (err) {
      form.innerHTML = '';
      showError(err.message);
      return;
    }
    const saved = pending && pending.task === name ? pending : null;
    pending = null;
    const savedParams = saved?.params || {};
    const description = task.description ? `<p>${escapeHtml(task.description)}</p>` : '';
    let fields;
    if (task.acceptsAnyParameters) {
      fields = `<vox-textarea rows="4" data-any-params data-param="(parameters)" label="${escapeHtml(t('Parameters'))}"
        note="${escapeHtml(t('This task declares no parameters, so any JSON object is passed to it as is.'))}"></vox-textarea>`;
    } else if (task.parameters.length === 0) {
      fields = `<p class="vox-ts-sm">${escapeHtml(t('This task takes no parameters.'))}</p>`;
    } else {
      fields = task.parameters
        .map((p) => `<div class="vox-m-bottom-md">${fieldHTML(p, savedParams[p.name] === REDACTED, withoutSensitive)}</div>`)
        .join('');
    }
    form.innerHTML = `
      ${description}
      ${fields}
      <vox-input type="number" min="1" max="55" step="1" data-timeout label="${escapeHtml(t('Timeout (minutes)'))}"
        placeholder="30" note="${escapeHtml(t('Stopped if still running after this long. 30 minutes if left empty.'))}"></vox-input>`;

    // A stored sensitive value stays on the server: its field is left
    // empty, and an empty field keeps it.
    const kept = new Set();
    if (saved) {
      if (task.acceptsAnyParameters) {
        form.querySelector('[data-any-params]').value = Object.keys(savedParams).length ? JSON.stringify(savedParams, null, 2) : '';
      } else {
        for (const param of task.parameters) {
          const value = savedParams[param.name];
          if (value === undefined) continue;
          if (value === REDACTED) {
            kept.add(param.name);
            continue;
          }
          const el = form.querySelector(`[data-param="${CSS.escape(param.name)}"]`);
          if (!el) continue;
          el.value = typeof value === 'string' ? value : JSON.stringify(value);
        }
      }
      if (saved.timeoutSeconds) form.querySelector('[data-timeout]').value = String(Math.round(saved.timeoutSeconds / 60));
    }
    setCurrent({ environment, task, kept });
  }

  // Reads the form into a params object. Empty fields are left out, so
  // the task's declared default applies. Returns { params, problems }.
  function collectParams(form, { task, kept }) {
    const params = {};
    const problems = {};
    if (task.acceptsAnyParameters) {
      const raw = form.querySelector('[data-any-params]').value.trim();
      if (raw) {
        try {
          const parsed = JSON.parse(raw);
          if (parsed === null || typeof parsed !== 'object' || Array.isArray(parsed)) throw new Error();
          Object.assign(params, parsed);
        } catch {
          problems['(parameters)'] = t('must be a JSON object');
        }
      }
      return { params, problems };
    }
    for (const param of task.parameters) {
      const el = form.querySelector(`[data-param="${CSS.escape(param.name)}"]`);
      if (!el) continue; // a sensitive parameter left out of the form
      const raw = (el.value ?? '').trim();
      if (raw === '') {
        if (param.input.required && !kept.has(param.name)) problems[param.name] = t('required');
        continue;
      }
      switch (param.input.kind) {
        case 'boolean':
          params[param.name] = raw === 'true';
          break;
        case 'integer':
        case 'number':
          params[param.name] = Number(raw);
          break;
        case 'json':
          try {
            params[param.name] = JSON.parse(raw);
          } catch {
            problems[param.name] = t('is not valid JSON');
          }
          break;
        default:
          // Kept exactly as typed: whitespace can matter in a value.
          params[param.name] = el.value;
      }
    }
    return { params, problems };
  }

  // Marks each field named in problems with its message, restoring the
  // note of any field marked earlier, and lists every problem.
  function showProblems(problems, summary) {
    const form = body.querySelector('[data-task-form]');
    if (!form) return;
    form.querySelectorAll('[data-param]').forEach((el) => {
      const base = el.dataset.note ?? el.getAttribute('note') ?? '';
      el.dataset.note = base;
      const problem = problems[el.dataset.param];
      el.setAttribute('note', problem ? `${t('Problem:')} ${problem}` : base);
    });
    const items = Object.entries(problems).map(([name, message]) => `<li>${escapeHtml(name)}: ${escapeHtml(message)}</li>`);
    errorEl.innerHTML = Object.keys(problems).length
      ? `<vox-alert variant="danger">${escapeHtml(summary)}<ul>${items.join('')}</ul></vox-alert>`
      : '';
  }

  /**
   * The task request's own fields - task, environment, params and, if
   * set, timeoutSeconds - or null when the form has problems, which it
   * then shows.
   */
  function collect() {
    if (!current) {
      showError(t('Choose a task first.'));
      return null;
    }
    const form = body.querySelector('[data-task-form]');
    const { params, problems } = collectParams(form, current);
    if (Object.keys(problems).length) {
      showProblems(problems, t('Fix these parameters first:'));
      return null;
    }
    showProblems({}, '');
    const request = { task: current.task.name, environment: current.environment, params };
    const minutes = form.querySelector('[data-timeout]').value.trim();
    if (minutes) request.timeoutSeconds = Math.round(Number(minutes) * 60);
    return request;
  }

  return {
    load,
    collect,
    showError,
    /** Shows the parameter problems a refused request named. */
    showServerProblems: (fields) => showProblems(fields, t('The console refused these parameters:')),
    isReady: () => Boolean(current),
  };
}
