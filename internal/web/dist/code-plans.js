// Plans from environment code (add-code-plans): listing them, a form to
// give a plan its parameters and run it, and the trace a run leaves.

import { fetchJSON, sendJSON, escapeHtml, hasPermission, statusVariant, statusLabel, t, tn, formatDateTime } from './app.js';
import { createTargetPicker } from './target-picker.js';

const STATE_VARIANT = { succeeded: 'success', failed: 'danger', running: 'info', not_run: 'neutral' };

function stateBadge(state) {
  const label = state === 'not_run' ? t('Not run') : statusLabel(state);
  return `<vox-badge variant="${STATE_VARIANT[state] || 'neutral'}">${escapeHtml(label)}</vox-badge>`;
}

// ---- the list ----

function paramSummary(p) {
  const bits = [p.name];
  if (p.required) bits.push('*');
  return bits.join('');
}

/**
 * Draws the code plans of an environment into root.
 * @param {HTMLElement} root
 * @param {string} [environment]
 */
export async function renderCodePlans(root, environment = 'production') {
  root.innerHTML = '';
  let data;
  try {
    data = await fetchJSON(`/api/v1/code-plans?environment=${encodeURIComponent(environment)}`);
  } catch (err) {
    root.innerHTML = `<vox-alert variant="danger">${escapeHtml(err.message)}</vox-alert>`;
    return;
  }
  if (data.available === false) {
    root.innerHTML = `<vox-alert variant="info">${escapeHtml(t('Code plans are not available on this console instance: it does not hold the deployed code. Use an instance that does.'))}</vox-alert>`;
    return;
  }
  if (data.items.length === 0) {
    root.innerHTML = `<p class="vox-ts-sm vox-color-text-2">${escapeHtml(t('No plans in the modules of {environment}.', { environment }))}</p>`;
    return;
  }
  const canRun = hasPermission('orchestrator:run');
  const rows = data.items.map((p) => `
    <tr>
      <th scope="row">${escapeHtml(p.name)}</th>
      <td>${p.usable
        ? escapeHtml(p.description || '')
        : `<vox-badge variant="warning">${escapeHtml(t('Cannot be run'))}</vox-badge> <span class="vox-ts-sm">${escapeHtml(p.reason)}</span>`}</td>
      <td class="vox-ts-sm">${escapeHtml((p.parameters || []).map(paramSummary).join(', '))}</td>
      ${canRun ? `<td>${p.usable ? `<vox-button size="sm" variant="brand" data-run="${escapeHtml(p.name)}">${escapeHtml(t('Run'))}</vox-button>` : ''}</td>` : ''}
    </tr>`).join('');
  root.innerHTML = `
    <div class="vox-table-wrap">
      <table class="vox-table vox-table--striped">
        <thead><tr><th scope="col">${t('Plan')}</th><th scope="col">${t('Description')}</th><th scope="col">${t('Parameters')}</th>${canRun ? '<th scope="col"></th>' : ''}</tr></thead>
        <tbody>${rows}</tbody>
      </table>
    </div>`;
  root.querySelectorAll('[data-run]').forEach((b) => b.addEventListener('click', async () => {
    b.disabled = true;
    try {
      openCodePlanDialog(await fetchJSON(`/api/v1/code-plans/${encodeURIComponent(b.dataset.run)}?environment=${encodeURIComponent(environment)}`), environment);
    } catch (err) {
      root.insertAdjacentHTML('afterbegin', `<vox-alert variant="danger" class="vox-m-bottom-md">${escapeHtml(err.message)}</vox-alert>`);
    } finally {
      b.disabled = false;
    }
  }));
}

// ---- the run dialog ----

// widgetFor chooses a control from a parameter's type. The server is the
// authority on whether a value is valid; this only picks a widget and
// reads what was typed back as the right JSON type.
function unwrapType(type) {
  let inner = type.trim();
  let sensitive = false;
  for (;;) {
    const m = /^(Optional|Sensitive)\[(.*)\]$/s.exec(inner);
    if (!m) break;
    if (m[1] === 'Sensitive') sensitive = true;
    inner = m[2].trim();
  }
  return { inner, sensitive };
}

function widgetFor(p) {
  if (p.target) return 'target';
  const { inner } = unwrapType(p.type);
  if (/^Integer(\[|$)/.test(inner)) return 'integer';
  if (/^(Float|Numeric)(\[|$)/.test(inner)) return 'number';
  if (inner === 'Boolean') return 'boolean';
  if (/^Enum\[/.test(inner)) return 'enum';
  if (/^String(\[|$)/.test(inner)) return 'string';
  return 'json';
}

function enumOptions(type) {
  const { inner } = unwrapType(type);
  return [...inner.matchAll(/'([^']*)'|"([^"]*)"/g)].map((m) => m[1] ?? m[2]);
}

function fieldHTML(p, uid) {
  const name = escapeHtml(p.name);
  const label = escapeHtml(p.required ? `${p.name} *` : p.name);
  const notes = [p.type];
  if (p.description) notes.unshift(p.description);
  if (p.default !== undefined) notes.push(t('Default: {value}', { value: JSON.stringify(p.default) }));
  const note = escapeHtml(notes.join(' · '));
  const common = `data-param="${name}" label="${label}" note="${note}"`;
  switch (widgetFor(p)) {
    case 'target':
      return `<fieldset class="vox-m-y-md"><legend class="vox-ts-sm">${label}</legend><p class="vox-ts-sm vox-color-text-2 vox-m-top-none">${note}</p><div data-target-param="${name}" id="${uid}-${name}"></div></fieldset>`;
    case 'boolean':
      return `<vox-select ${common}><option value="">${escapeHtml(t('Not set'))}</option><option value="true">true</option><option value="false">false</option></vox-select>`;
    case 'enum':
      return `<vox-select ${common}><option value="">${escapeHtml(t('Not set'))}</option>${enumOptions(p.type).map((o) => `<option value="${escapeHtml(o)}">${escapeHtml(o)}</option>`).join('')}</vox-select>`;
    case 'integer':
      return `<vox-input type="number" step="1" ${common}></vox-input>`;
    case 'number':
      return `<vox-input type="number" step="any" ${common}></vox-input>`;
    case 'string':
      return p.sensitive
        ? `<vox-input type="password" autocomplete="off" ${common}></vox-input>`
        : `<vox-input ${common}></vox-input>`;
    default:
      return p.sensitive
        ? `<vox-input type="password" autocomplete="off" ${common}></vox-input>`
        : `<vox-textarea rows="3" placeholder="${escapeHtml(t('JSON, for example ["a", "b"]'))}" ${common}></vox-textarea>`;
  }
}

let dialogCount = 0;

/**
 * Opens the dialog that gives a plan its parameters and runs it.
 * @param {object} plan a plan as /api/v1/code-plans/{name} describes it.
 */
export function openCodePlanDialog(plan, environment = 'production') {
  const uid = `code-plan-${++dialogCount}`;
  const dialog = document.createElement('vox-dialog');
  dialog.heading = t('Run {plan}', { plan: plan.name });
  dialog.setAttribute('light-dismiss', '');
  const steps = (plan.steps || []).map((s) => {
    const what = s.kind === 'task' ? s.task : s.kind === 'plan' ? s.plan : t('message');
    return `<li>${escapeHtml(s.description || s.name || what)} <span class="vox-color-text-2">· ${escapeHtml(what)}</span></li>`;
  }).join('');
  dialog.innerHTML = `
    <div class="run-task-scroll">
      ${plan.description ? `<p>${escapeHtml(plan.description)}</p>` : ''}
      ${steps ? `<details class="vox-m-bottom-md"><summary>${escapeHtml(tn('{count} step', '{count} steps', plan.steps.length))}</summary><ol class="vox-ts-sm">${steps}</ol></details>` : ''}
      ${(plan.parameters || []).map((p) => fieldHTML(p, uid)).join('') || `<p class="vox-ts-sm vox-color-text-2">${escapeHtml(t('This plan takes no parameters.'))}</p>`}
      <div data-error class="vox-m-top-md"></div>
    </div>
    <div slot="footer" class="dialog-footer-split">
      <vox-button variant="alt" data-action="cancel">${escapeHtml(t('Cancel'))}</vox-button>
      <vox-button variant="brand" data-action="run">${escapeHtml(t('Run'))}</vox-button>
    </div>`;
  const errorEl = dialog.querySelector('[data-error]');
  const runButton = dialog.querySelector('[data-action="run"]');
  dialog.addEventListener('vox-close', () => dialog.remove());
  dialog.querySelector('[data-action="cancel"]').addEventListener('click', () => dialog.close());
  document.body.appendChild(dialog);
  dialog.show();

  const pickers = {};
  dialog.querySelectorAll('[data-target-param]').forEach((el) => {
    pickers[el.dataset.targetParam] = createTargetPicker(el);
  });

  // read turns what was entered into the parameters to send: a field left
  // empty is left out, so the plan's default applies.
  function read() {
    const out = {};
    for (const p of plan.parameters || []) {
      const kind = widgetFor(p);
      if (kind === 'target') {
        const target = pickers[p.name].getTarget();
        if (target.query) out[p.name] = { query: target.query };
        else if (target.group !== undefined) out[p.name] = { group: target.group };
        else if (target.targets && target.targets.length > 0) out[p.name] = target.targets;
        continue;
      }
      const el = dialog.querySelector(`[data-param="${CSS.escape(p.name)}"]`);
      const raw = String(el.value ?? '').trim();
      if (raw === '') continue;
      switch (kind) {
        case 'integer':
        case 'number':
          out[p.name] = Number(raw);
          break;
        case 'boolean':
          out[p.name] = raw === 'true';
          break;
        case 'json':
          if (p.sensitive) {
            out[p.name] = raw;
          } else {
            try {
              out[p.name] = JSON.parse(raw);
            } catch {
              throw Object.assign(new Error(t('{name} is not valid JSON.', { name: p.name })), { field: p.name });
            }
          }
          break;
        default:
          out[p.name] = raw;
      }
    }
    return out;
  }

  runButton.addEventListener('click', async () => {
    errorEl.innerHTML = '';
    // A target chosen by group or query is previewed first, so the user
    // has seen which nodes the plan will use.
    for (const p of plan.parameters || []) {
      if (widgetFor(p) === 'target' && pickers[p.name] && !pickers[p.name].isEmpty() && !(await pickers[p.name].confirm())) return;
    }
    let parameters;
    try {
      parameters = read();
    } catch (err) {
      errorEl.innerHTML = `<vox-alert variant="danger">${escapeHtml(err.message)}</vox-alert>`;
      return;
    }
    runButton.disabled = true;
    try {
      const job = await sendJSON('/api/v1/orchestrator/code-plans', 'POST', { plan: plan.name, environment, parameters });
      window.location.href = `/job.html?id=${job.id}`;
    } catch (err) {
      const message = err.message.charAt(0).toUpperCase() + err.message.slice(1);
      errorEl.innerHTML = `<vox-alert variant="danger">${escapeHtml(message)}</vox-alert>`;
      runButton.disabled = false;
    }
  });
}

// ---- the trace ----

function entryLabel(e) {
  if (e.kind === 'task') return e.task;
  if (e.kind === 'plan') return e.planCalled;
  return t('message');
}

/** A code plan run's steps, in the order they began, plans it called indented. */
export function codePlanTraceHTML(job) {
  const trace = job.planTrace || [];
  const params = job.params?.parameters;
  const parts = [];
  if (job.error) parts.push(`<vox-alert variant="danger" class="vox-m-bottom-md">${escapeHtml(job.error)}</vox-alert>`);
  if (job.traceHidden) {
    parts.push(`<vox-alert variant="info" class="vox-m-bottom-md">${escapeHtml(t('This plan also ran on nodes outside your node groups, so what it printed and returned is not shown.'))}</vox-alert>`);
  }
  if (trace.length === 0) {
    parts.push(`<vox-empty-state heading="${escapeHtml(t('No steps yet'))}"></vox-empty-state>`);
  } else {
    const rows = trace.map((e) => `
      <tr>
        <td style="padding-left: ${0.5 + e.depth * 1.5}rem"><span class="vox-ts-sm vox-color-text-2">${escapeHtml(e.path)}</span> ${escapeHtml(e.name || entryLabel(e))}
          <div class="vox-ts-sm vox-color-text-2">${escapeHtml(e.description || (e.name ? entryLabel(e) : ''))}</div></td>
        <td>${stateBadge(e.state)}</td>
        <td>${e.jobId ? `<a href="/job.html?id=${e.jobId}">${escapeHtml(t('Job #{id}', { id: e.jobId }))}</a>` : ''}</td>
        <td>${e.message ? `<code class="vox-ts-sm">${escapeHtml(e.message)}</code>` : ''}${e.error ? `<div class="vox-ts-sm">${escapeHtml(e.error)}</div>` : ''}</td>
      </tr>`).join('');
    parts.push(`
      <div class="vox-table-wrap">
        <table class="vox-table vox-table--striped">
          <thead><tr><th scope="col">${t('Step')}</th><th scope="col">${t('Status')}</th><th scope="col">${t('Job')}</th><th scope="col">${t('Output')}</th></tr></thead>
          <tbody>${rows}</tbody>
        </table>
      </div>`);
  }
  if (job.result !== undefined && job.result !== null) {
    parts.push(`<vox-disclosure class="vox-m-top-md" summary="${escapeHtml(t('Returned value'))}"><vox-code-block language="json" no-header>${escapeHtml(JSON.stringify(job.result, null, 2))}</vox-code-block></vox-disclosure>`);
  }
  if (params && Object.keys(params).length > 0) {
    parts.push(`<vox-disclosure class="vox-m-top-md" summary="${escapeHtml(t('Parameters'))}"><vox-code-block language="json" no-header>${escapeHtml(JSON.stringify(params, null, 2))}</vox-code-block></vox-disclosure>`);
  }
  return parts.join('');
}

export { statusVariant, formatDateTime };
