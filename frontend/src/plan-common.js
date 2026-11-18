// What the plan pages share: how a step is worded, and the part of a
// form that shows a plan's steps and asks for the sensitive values they
// need (see specs/plans).
//
// A saved plan never holds a sensitive value, so every place a plan is
// run or scheduled from asks for them: the Run plan dialog and the
// schedule form both use createPlanSteps.

import { escapeHtml, t, tn, N_ } from './app.js';
import { runOptionBadges } from './run-puppet.js';

// What the console shows in place of a sensitive value it stored.
const REDACTED = 'Sensitive [value redacted]';

/** "Step 2 of 5". */
export function stepLabel(index) {
  return t('Step {number}', { number: index + 1 });
}

/**
 * What a step does, in a line: "Puppet run" with its option badges, or
 * the task's name and environment.
 *
 * @param {{kind: string, taskName?: string, environment?: string, options?: object, runOptions?: object}} step
 */
export function stepActionHTML(step) {
  if (step.kind === 'task') {
    const environment = step.environment ? ` <span class="vox-color-text-2">(${escapeHtml(step.environment)})</span>` : '';
    return `${escapeHtml(t('Task'))} <code>${escapeHtml(step.taskName || '')}</code>${environment}`;
  }
  return `${escapeHtml(t('Puppet run'))} ${runOptionBadges(step.options || step.runOptions)}`;
}

/** A task step's stored parameters and timeout, as a short definition. */
/**
 * What a step's problem says, or nothing when it has none. A task the
 * reader's roles do not allow is not a fault in the plan, and is said
 * differently: editing the plan would not help them.
 */
export function stepProblemHTML(step) {
  if (step.notPermitted) {
    return `<vox-alert variant="warning">${escapeHtml(t('Your roles do not allow the task {task}, so you cannot run this step.', { task: step.taskName }))}</vox-alert>`;
  }
  if (!step.problem) return '';
  return `<vox-alert variant="warning">${escapeHtml(t('This step can no longer run as saved: {problem}', { problem: step.problem }))}</vox-alert>`;
}

export function stepDetailHTML(step) {
  if (step.kind !== 'task') return '';
  const parts = [];
  const params = step.params || {};
  if (Object.keys(params).length > 0) {
    parts.push(`<vox-code-block language="json" no-header>${escapeHtml(JSON.stringify(params, null, 2))}</vox-code-block>`);
  }
  if (step.timeoutSeconds) {
    const minutes = Math.round(step.timeoutSeconds / 60);
    parts.push(`<p class="vox-ts-sm vox-color-text-2 vox-m-y-none">${escapeHtml(tn('Timeout: {count} minute', 'Timeout: {count} minutes', minutes))}</p>`);
  }
  return parts.join('');
}

/** "3 steps". */
export function stepCountText(count) {
  return tn('{count} step', '{count} steps', count);
}

// N_ marks each label for translation; t() looks it up when shown.
const STEP_STATES = {
  waiting: ['neutral', N_('Waiting')],
  running: ['info', N_('Running')],
  succeeded: ['tip', N_('Succeeded')],
  failed: ['danger', N_('Failed')],
  not_run: ['neutral', N_('Not run')],
};

/** A plan step's state as a badge. */
export function stepStateBadge(state) {
  const [variant, label] = STEP_STATES[state] || ['neutral', state];
  return `<vox-badge variant="${variant}">${escapeHtml(t(label))}</vox-badge>`;
}

/**
 * The sensitive values a step asks for.
 *
 * A saved plan's step lists them in `needs`. A schedule's own copy of a
 * step has no such list, but shows each stored one as redacted - a
 * value that is kept if its field is left empty.
 */
function stepNeeds(step) {
  if (step.kind !== 'task') return [];
  const params = step.params || {};
  const needs = (step.needs || []).map((n) => ({ param: n.param, required: n.required, kept: params[n.param] === REDACTED }));
  for (const [name, value] of Object.entries(params)) {
    if (value === REDACTED && !needs.some((n) => n.param === name)) needs.push({ param: name, required: false, kept: true });
  }
  return needs;
}

/**
 * Shows a plan's steps in root and asks for the sensitive values they
 * need.
 *
 * @param {HTMLElement} root
 * @param {Array<object>} steps a saved plan's steps, or a schedule's.
 * @returns {{getSteps: () => (Array<object>|null), hasProblem: boolean, hasNotPermitted: boolean, showStepError: (step: number, message: string) => void}}
 *   getSteps returns the steps as a plan request takes them, with the
 *   values typed in - or null, having marked the fields, when a
 *   required one is empty.
 */
export function createPlanSteps(root, steps) {
  let hasProblem = false;
  let hasNotPermitted = false;
  root.classList.add('plan-steps');
  root.innerHTML = steps
    .map((step, i) => {
      if (step.problem) hasProblem = true;
      if (step.notPermitted) hasNotPermitted = true;
      const needs = stepNeeds(step);
      const fields = needs
        .map((need) => {
          const typed = step.params && step.params[need.param] !== undefined && step.params[need.param] !== REDACTED ? step.params[need.param] : '';
          const label = `${need.param}${need.required && !need.kept ? ' *' : ''}`;
          const note = need.kept
            ? t('A value is stored. Leave this empty to keep it.')
            : t('A sensitive value: it is sent to the nodes and never shown again.');
          return `<vox-input type="password" autocomplete="off" data-step="${i}" data-need="${escapeHtml(need.param)}"
            data-required="${need.required && !need.kept}" label="${escapeHtml(label)}" note="${escapeHtml(note)}" value="${escapeHtml(String(typed))}"></vox-input>`;
        })
        .join('');
      // The stored parameters, without the ones asked for here.
      const shown = { ...step, params: Object.fromEntries(Object.entries(step.params || {}).filter(([name]) => !needs.some((n) => n.param === name))) };
      const detail = stepDetailHTML(shown);
      return `
      <li class="plan-step" data-step-item="${i}">
        <div class="plan-step-heading"><span class="plan-step-number">${escapeHtml(stepLabel(i))}</span> ${stepActionHTML(step)}</div>
        ${stepProblemHTML(step)}
        ${detail ? `<vox-disclosure summary="${escapeHtml(t('Parameters'))}">${detail}</vox-disclosure>` : ''}
        ${fields ? `<div class="plan-step-needs">${fields}</div>` : ''}
        <div data-step-error="${i}"></div>
      </li>`;
    })
    .join('');
  root.innerHTML = `<ol class="plan-step-list">${root.innerHTML}</ol>`;

  function showStepError(stepNumber, message) {
    root.querySelectorAll('[data-step-error]').forEach((el) => {
      el.innerHTML = '';
    });
    const el = root.querySelector(`[data-step-error="${stepNumber - 1}"]`);
    if (!el) return false;
    el.innerHTML = message ? `<vox-alert variant="danger">${escapeHtml(message)}</vox-alert>` : '';
    if (message) el.scrollIntoView({ block: 'center', behavior: 'smooth' });
    return true;
  }

  function getSteps() {
    let missing = null;
    const out = steps.map((step, i) => {
      if (step.kind !== 'task') {
        const run = { kind: 'run' };
        const options = step.options || step.runOptions;
        if (options && Object.keys(options).length > 0) run.options = options;
        return run;
      }
      const params = { ...(step.params || {}) };
      root.querySelectorAll(`[data-step="${i}"]`).forEach((el) => {
        const base = el.dataset.note ?? el.getAttribute('note') ?? '';
        el.dataset.note = base;
        el.setAttribute('note', base);
        const value = el.value ?? '';
        if (value !== '') {
          params[el.dataset.need] = value;
        } else if (el.dataset.required === 'true') {
          el.setAttribute('note', `${t('Problem:')} ${t('required')}`);
          missing = missing || el;
        } else if (params[el.dataset.need] !== REDACTED) {
          delete params[el.dataset.need];
        }
      });
      const task = { kind: 'task', taskName: step.taskName, params };
      if (step.environment) task.environment = step.environment;
      if (step.timeoutSeconds) task.timeoutSeconds = step.timeoutSeconds;
      return task;
    });
    if (missing) {
      missing.scrollIntoView({ block: 'center', behavior: 'smooth' });
      return null;
    }
    return out;
  }

  return { getSteps, hasProblem, hasNotPermitted, showStepError };
}
