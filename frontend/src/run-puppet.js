import { fetchJSON, sendJSON, escapeHtml, t, tn } from './app.js';
import { createTargetPicker } from './target-picker.js';
import { scheduleInstead } from './schedule-draft.js';

// How many node names the dialog lists before it summarises the rest.
const PREVIEW = 5;

let runOptionsCount = 0;

/**
 * The run options part of a form: no-op, debug and environment. Shared
 * by the Run Puppet dialog and the schedule form.
 *
 * @param {HTMLElement} root
 * @param {{noop?: boolean, debug?: boolean, environment?: string}} [initial]
 * @returns {{getOptions: () => object}} getOptions returns only what was
 *   chosen: an empty object is a plain run.
 */
export function createRunOptions(root, initial = {}) {
  // A page can hold several of these - one for each run step of a plan
  // - so the ids that tie the label and the suggestions to the input
  // are unique to each.
  const uid = `run-environment-${++runOptionsCount}`;
  root.classList.add('run-options');
  root.innerHTML = `
    <vox-checkbox data-option="noop">${escapeHtml(t('No-op: report what would change, and change nothing'))}</vox-checkbox>
    <vox-checkbox data-option="debug">${escapeHtml(t('Debug output'))}</vox-checkbox>
    <div class="native-field">
      <label for="${uid}">${escapeHtml(t('Environment (optional)'))}</label>
      <input id="${uid}" class="native-control" list="${uid}-options" autocomplete="off" spellcheck="false" data-run-environment>
      <datalist id="${uid}-options"></datalist>
      <p class="native-note">${escapeHtml(t("Asks the node to run against this environment. A node whose node groups set an environment runs in that one instead; the job shows which was used."))}</p>
    </div>`;
  const noopEl = root.querySelector('[data-option="noop"]');
  const debugEl = root.querySelector('[data-option="debug"]');
  const environmentEl = root.querySelector('[data-run-environment]');
  noopEl.checked = Boolean(initial.noop);
  debugEl.checked = Boolean(initial.debug);
  environmentEl.value = initial.environment || '';

  // The environments are a convenience. Listing them needs a permission
  // (orchestrator:read) that someone allowed to run Puppet may not hold,
  // and a server connection the console may not have; without the list
  // the name is typed, and the console still checks it.
  fetchJSON('/api/v1/tasks/environments')
    .then(({ environments }) => {
      root.querySelector('datalist').innerHTML = (environments || [])
        .map((e) => `<option value="${escapeHtml(e)}"></option>`)
        .join('');
    })
    .catch(() => {});

  return {
    getOptions() {
      const options = {};
      if (noopEl.checked) options.noop = true;
      if (debugEl.checked) options.debug = true;
      const environment = environmentEl.value.trim();
      if (environment) options.environment = environment;
      return options;
    },
  };
}

/**
 * Opens the Run Puppet dialog and, on confirmation, starts the run and
 * goes to its job page.
 *
 * Given targets, the dialog names them and asks. Given none - opened
 * from the Jobs page - it offers the target chooser: a list of nodes, a
 * query, or a node group.
 *
 * Confirming with nothing chosen sends exactly the request a run has
 * always been - no `options` at all - so it behaves the same on every
 * node agent client version. Any option makes it a run with options,
 * which a client that predates them refuses rather than carrying out as
 * a plain run (see internal/orchestrator's actionRunWithOptions).
 *
 * @param {object} [options]
 * @param {string[]} [options.targets] certnames to run on.
 * @param {number} [options.group] the node group the targets are the
 *   matching nodes of, when there is one: scheduling from here then
 *   targets the group rather than the nodes it holds today.
 */
export function openRunPuppetDialog({ targets, group } = {}) {
  const dialog = document.createElement('vox-dialog');
  dialog.heading = t('Run Puppet');
  dialog.setAttribute('light-dismiss', '');

  let intro = '<div data-targets></div>';
  if (targets) {
    const preview = targets.slice(0, PREVIEW).map((name) => escapeHtml(name)).join(', ');
    const more = targets.length > PREVIEW ? escapeHtml(tn(', and {count} more', ', and {count} more', targets.length - PREVIEW)) : '';
    const question = targets.length === 1
      ? t('Run Puppet on {node}?', { node: targets[0] })
      : tn('Run Puppet on {count} node?', 'Run Puppet on {count} nodes?', targets.length);
    intro = `
      <p>${escapeHtml(question)}</p>
      ${targets.length > 1 ? `<p class="vox-ts-sm">${preview}${more}</p>` : ''}`;
  }

  dialog.innerHTML = `
    <div class="run-task-scroll">
      ${intro}
      <div data-options></div>
      <div data-error class="vox-m-top-md"></div>
    </div>
    <div slot="footer" class="dialog-footer-split">
      <vox-button variant="alt" data-action="schedule" class="dialog-footer-start">${escapeHtml(t('Schedule instead'))}</vox-button>
      <vox-button variant="alt" data-action="cancel">${escapeHtml(t('Cancel'))}</vox-button>
      <vox-button variant="brand" data-action="run">${escapeHtml(t('Run'))}</vox-button>
    </div>`;

  const errorEl = dialog.querySelector('[data-error]');
  const runButton = dialog.querySelector('[data-action="run"]');
  dialog.addEventListener('vox-close', () => dialog.remove());
  dialog.querySelector('[data-action="cancel"]').addEventListener('click', () => dialog.close());
  document.body.appendChild(dialog);
  dialog.show();

  const picker = targets ? null : createTargetPicker(dialog.querySelector('[data-targets]'));
  const runOptions = createRunOptions(dialog.querySelector('[data-options]'));

  function target() {
    return picker ? picker.getTarget() : { targets };
  }

  dialog.querySelector('[data-action="schedule"]').addEventListener('click', () => {
    scheduleInstead({ kind: 'run', options: runOptions.getOptions(), ...(group !== undefined ? { group } : target()) });
  });

  runButton.addEventListener('click', async () => {
    errorEl.innerHTML = '';
    if (picker && !(await picker.confirm())) return;
    const options = runOptions.getOptions();
    const request = { ...target() };
    if (Object.keys(options).length > 0) request.options = options;

    runButton.disabled = true;
    try {
      const job = await sendJSON('/api/v1/orchestrator/runs', 'POST', request);
      window.location.href = `/job.html?id=${job.id}`;
    } catch (err) {
      errorEl.innerHTML = `<vox-alert variant="danger">${escapeHtml(err.message)}</vox-alert>`;
      runButton.disabled = false;
    }
  });
}

/**
 * A run's options as badges, for the jobs list and the job page. Empty
 * for a plain run.
 *
 * @param {{noop?: boolean, environment?: string, debug?: boolean}|undefined} options
 */
export function runOptionBadges(options) {
  if (!options) return '';
  const badges = [];
  if (options.noop) badges.push(`<vox-badge variant="tip">${escapeHtml(t('No-op'))}</vox-badge>`);
  if (options.environment) badges.push(`<vox-badge variant="neutral">${escapeHtml(t('Environment: {environment}', { environment: options.environment }))}</vox-badge>`);
  if (options.debug) badges.push(`<vox-badge variant="neutral">${escapeHtml(t('Debug'))}</vox-badge>`);
  return badges.join(' ');
}
