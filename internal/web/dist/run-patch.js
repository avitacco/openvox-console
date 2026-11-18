// Patch runs (add-patch-runs): the dialog that starts one, and the way a
// run's per-node, per-package results are drawn on the job page, the node
// page and the Patching page.

import { sendJSON, escapeHtml, statusVariant, statusLabel, t, tn } from './app.js';

// How many node names the dialog lists before it summarises the rest.
const PREVIEW = 5;

const OUTCOMES = {
  upgraded: ['success', 'Upgraded'],
  would_upgrade: ['tip', 'Would upgrade'],
  not_upgraded: ['danger', 'Not upgraded'],
  nothing_to_apply: ['neutral', 'Nothing to apply'],
};

function outcomeBadge(outcome) {
  const [variant, label] = OUTCOMES[outcome] || ['neutral', outcome];
  // The labels are listed in N_ form in i18n.js's catalogue so that the
  // extractor finds them; t() looks them up here.
  return `<vox-badge variant="${variant}">${escapeHtml(t(label))}</vox-badge>`;
}

/** One line naming what a patch run applies, for badges. */
export function patchOptionBadges(options) {
  if (!options) return '';
  const badges = [];
  if (options.scope === 'security') badges.push(`<vox-badge variant="danger">${escapeHtml(t('Security updates'))}</vox-badge>`);
  if (options.scope === 'all') badges.push(`<vox-badge variant="neutral">${escapeHtml(t('All updates'))}</vox-badge>`);
  if (options.scope === 'packages') badges.push(`<vox-badge variant="neutral">${escapeHtml(t('Packages: {names}', { names: (options.packages || []).join(', ') }))}</vox-badge>`);
  if (options.dryRun) badges.push(`<vox-badge variant="tip">${escapeHtml(t('Dry run'))}</vox-badge>`);
  if (options.reboot) badges.push(`<vox-badge variant="warning">${escapeHtml(t('Reboots if required'))}</vox-badge>`);
  return badges.join(' ');
}

/**
 * The line under a patch job's summary: how it is batched and how far it
 * has got.
 */
export function patchProgressHTML(job) {
  const o = job.patchOptions;
  if (!o) return '';
  const batches = new Set((job.targets || []).map((x) => x.batch || 1));
  const done = (job.targets || []).filter((x) => x.status === 'succeeded' || x.status === 'failed').length;
  const parts = [
    tn('{count} node', '{count} nodes', job.targets?.length || 0),
    tn('in {count} batch of up to {size}', 'in {count} batches of up to {size}', batches.size, { size: o.batchSize }),
    t('{done} finished', { done }),
    o.stopAfterFailures > 0
      ? tn('stops after {count} failed node', 'stops after {count} failed nodes', o.stopAfterFailures)
      : t('never stops for failures'),
  ];
  return `<p class="vox-ts-sm vox-color-text-2 vox-m-top-md vox-m-bottom-none">${escapeHtml(parts.join(' · '))}</p>`;
}

/** A node's packages and what happened to each. */
export function patchPackagesTable(result) {
  const packages = result?.packages || [];
  if (packages.length === 0) {
    return `<p class="vox-ts-sm vox-color-text-2 vox-m-y-none">${escapeHtml(t('No package was selected.'))}</p>`;
  }
  const rows = packages.map((p) => `
    <tr>
      <th scope="row">${escapeHtml(p.name)}${p.security ? ` <vox-badge variant="danger">${escapeHtml(t('Security'))}</vox-badge>` : ''}</th>
      <td>${escapeHtml(p.from || '') || '—'}</td>
      <td>${escapeHtml(p.to || '') || '—'}</td>
      <td>${outcomeBadge(p.outcome)}</td>
    </tr>`).join('');
  return `
    <div class="vox-table-wrap">
      <table class="vox-table vox-table--striped">
        <thead><tr><th scope="col">${t('Package')}</th><th scope="col">${t('From')}</th><th scope="col">${t('To')}</th><th scope="col">${t('Result')}</th></tr></thead>
        <tbody>${rows}</tbody>
      </table>
    </div>`;
}

function packageSummary(result) {
  const counts = {};
  for (const p of result?.packages || []) counts[p.outcome] = (counts[p.outcome] || 0) + 1;
  const parts = [];
  if (counts.upgraded) parts.push(tn('{count} upgraded', '{count} upgraded', counts.upgraded));
  if (counts.would_upgrade) parts.push(tn('{count} would be upgraded', '{count} would be upgraded', counts.would_upgrade));
  if (counts.not_upgraded) parts.push(tn('{count} not upgraded', '{count} not upgraded', counts.not_upgraded));
  if (result?.rebooted) parts.push(t('rebooted'));
  else if (result?.rebootRequired) parts.push(t('needs a reboot'));
  return parts.join(', ');
}

/**
 * A patch job's nodes, by batch: status, what was done to its packages,
 * and (collapsed) the package manager's output.
 */
export function patchTargetsHTML(job) {
  const targets = job.targets || [];
  if (targets.length === 0) return `<vox-empty-state heading="${t('No targets')}"></vox-empty-state>`;
  const rows = targets.map((target) => {
    const detail = target.result || target.output
      ? `
        <tr>
          <td colspan="6" style="max-width: 0">
            <vox-disclosure summary="${escapeHtml(t('Details for {node}', { node: target.certname }))}">
              ${target.result ? patchPackagesTable(target.result) : ''}
              ${target.output ? `<div class="job-output"><vox-code-block language="text" no-header>${escapeHtml(target.output)}</vox-code-block></div>` : ''}
            </vox-disclosure>
          </td>
        </tr>`
      : '';
    return `
        <tr>
          <td><a href="/node.html?name=${encodeURIComponent(target.certname)}">${escapeHtml(target.certname)}</a></td>
          <td>${target.batch ?? ''}</td>
          <td><vox-badge variant="${statusVariant(target.status)}">${escapeHtml(statusLabel(target.status))}</vox-badge></td>
          <td>${escapeHtml(packageSummary(target.result))}</td>
          <td>${target.exitCode === undefined || target.exitCode === null ? '' : target.exitCode}</td>
          <td>${target.errorDetail ? escapeHtml(target.errorDetail) : ''}</td>
        </tr>${detail}`;
  }).join('');
  return `
      <div class="vox-table-wrap">
        <table class="vox-table vox-table--striped">
          <thead>
            <tr><th scope="col">${t('Node')}</th><th scope="col">${t('Batch')}</th><th scope="col">${t('Status')}</th><th scope="col">${t('Packages')}</th><th scope="col">${t('Exit code')}</th><th scope="col">${t('Error')}</th></tr>
          </thead>
          <tbody>${rows}</tbody>
        </table>
      </div>`;
}

/**
 * Opens the patch dialog. Given the nodes (or the node group they are the
 * matching nodes of) it names them and asks; on confirmation it starts the
 * run and goes to its job page.
 *
 * @param {object} options
 * @param {string[]} [options.targets] certnames to patch.
 * @param {number} [options.group] a node group id to patch.
 * @param {string} [options.groupName] its name, for the question.
 */
export function openPatchDialog({ targets, group, groupName } = {}) {
  const dialog = document.createElement('vox-dialog');
  dialog.heading = t('Patch nodes');
  dialog.setAttribute('light-dismiss', '');
  dialog.innerHTML = `
    <div class="run-task-scroll">
      <div data-question></div>
      <vox-select data-field="scope" label="${escapeHtml(t('Updates to apply'))}">
        <option value="security">${escapeHtml(t('Security updates only'))}</option>
        <option value="all">${escapeHtml(t('All pending updates'))}</option>
        <option value="packages">${escapeHtml(t('Named packages'))}</option>
      </vox-select>
      <div data-packages-field style="display: none">
        <vox-input data-field="packages" label="${escapeHtml(t('Packages'))}" placeholder="${escapeHtml(t('curl openssl'))}"></vox-input>
        <div data-error-for="packages"></div>
        <p class="vox-ts-sm vox-color-text-2 vox-m-top-xs">${escapeHtml(t('Separate package names with spaces or commas. Only those with an update pending are upgraded.'))}</p>
      </div>
      <vox-checkbox data-field="dryRun">${escapeHtml(t('Dry run: report what would be upgraded, and change nothing'))}</vox-checkbox>
      <vox-checkbox data-field="reboot">${escapeHtml(t('Reboot a node that needs it once it is patched'))}</vox-checkbox>
      <div data-error-for="reboot"></div>
      <div class="vox-display-flex vox-gap-md vox-flex-wrap vox-m-top-md">
        <div>
          <vox-input data-field="batchSize" class="field-narrow" type="number" min="1" max="100" step="1" label="${escapeHtml(t('Nodes at a time'))}" value="10"></vox-input>
          <div data-error-for="batchSize"></div>
        </div>
        <div>
          <vox-input data-field="stopAfterFailures" class="field-narrow" type="number" min="0" max="1000" step="1" label="${escapeHtml(t('Stop after failed nodes'))}" value="3"></vox-input>
          <div data-error-for="stopAfterFailures"></div>
        </div>
      </div>
      <p class="vox-ts-sm vox-color-text-2">${escapeHtml(t('Nodes are patched a batch at a time. When this many nodes have failed, no further batch is started. 0 never stops.'))}</p>
      <div data-error class="vox-m-top-md"></div>
    </div>
    <div slot="footer" class="dialog-footer-split">
      <vox-button variant="alt" data-action="cancel">${escapeHtml(t('Cancel'))}</vox-button>
      <vox-button variant="brand" data-action="patch">${escapeHtml(t('Patch'))}</vox-button>
    </div>`;

  const field = (name) => dialog.querySelector(`[data-field="${name}"]`);
  const errorEl = dialog.querySelector('[data-error]');
  const patchButton = dialog.querySelector('[data-action="patch"]');
  const questionEl = dialog.querySelector('[data-question]');
  const packagesField = dialog.querySelector('[data-packages-field]');
  dialog.addEventListener('vox-close', () => dialog.remove());
  dialog.querySelector('[data-action="cancel"]').addEventListener('click', () => dialog.close());
  document.body.appendChild(dialog);
  dialog.show();

  function question(names) {
    const preview = names.slice(0, PREVIEW).map((n) => escapeHtml(n)).join(', ');
    const more = names.length > PREVIEW ? escapeHtml(tn(', and {count} more', ', and {count} more', names.length - PREVIEW)) : '';
    const text = names.length === 1
      ? t('Patch {node}?', { node: names[0] })
      : tn('Patch {count} node?', 'Patch {count} nodes?', names.length);
    questionEl.innerHTML = `<p>${escapeHtml(text)}</p>${names.length > 1 ? `<p class="vox-ts-sm">${preview}${more}</p>` : ''}`;
  }
  if (targets) {
    question(targets);
  } else {
    questionEl.innerHTML = `<p>${escapeHtml(t('Finding the nodes in {group}…', { group: groupName || '' }))}</p>`;
    sendJSON('/api/v1/orchestrator/targets/resolve', 'POST', { group })
      .then((r) => {
        if (r.count === 0) {
          questionEl.innerHTML = `<vox-alert variant="warning">${escapeHtml(t('No node is in this group, so there is nothing to patch.'))}</vox-alert>`;
          patchButton.disabled = true;
        } else {
          question(r.nodes);
        }
      })
      .catch((err) => {
        questionEl.innerHTML = `<vox-alert variant="danger">${escapeHtml(err.message)}</vox-alert>`;
        patchButton.disabled = true;
      });
  }

  field('scope').addEventListener('change', () => {
    packagesField.style.display = field('scope').value === 'packages' ? '' : 'none';
  });
  field('dryRun').addEventListener('change', () => {
    const dry = field('dryRun').checked;
    if (dry) field('reboot').checked = false;
    field('reboot').disabled = dry;
  });

  patchButton.addEventListener('click', async () => {
    errorEl.innerHTML = '';
    dialog.querySelectorAll('[data-error-for]').forEach((el) => { el.innerHTML = ''; });
    const scope = field('scope').value;
    const options = {
      scope,
      dryRun: field('dryRun').checked,
      reboot: field('reboot').checked,
      batchSize: Number(field('batchSize').value),
      stopAfterFailures: Number(field('stopAfterFailures').value),
    };
    if (scope === 'packages') options.packages = String(field('packages').value || '').split(/[\s,]+/).filter(Boolean);
    const request = { ...(targets ? { targets } : { group }), options };

    patchButton.disabled = true;
    try {
      const job = await sendJSON('/api/v1/orchestrator/patches', 'POST', request);
      window.location.href = `/job.html?id=${job.id}`;
    } catch (err) {
      const place = (err.field && dialog.querySelector(`[data-error-for="${CSS.escape(err.field)}"]`)) || errorEl;
      const message = err.message.charAt(0).toUpperCase() + err.message.slice(1);
      place.innerHTML = `<vox-alert variant="danger">${escapeHtml(message)}</vox-alert>`;
      patchButton.disabled = false;
    }
  });
}
