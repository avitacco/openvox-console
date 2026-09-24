import { fetchJSON, escapeHtml, requirePermission, withLoading, t, formatDateTime } from './app.js';

if (requirePermission('status:read')) {
  const incompleteNotice = document.getElementById('incomplete-notice');
  const instances = document.getElementById('instances');
  const dependencies = document.getElementById('dependencies');

  // Health values the API reports, mapped to badge variants. Kept as a
  // lookup rather than inline conditionals so an unrecognised value from
  // a newer server falls back to neutral rather than rendering nothing.
  const HEALTH_VARIANT = {
    healthy: 'tip',
    degraded: 'warning',
    unreachable: 'danger',
    'not-configured': 'neutral',
  };

  // Each label is a literal inside t(), not a lookup the value is fed
  // through. The extractor scans for t('...') with a literal argument,
  // so t(someVariable) contributes nothing to the catalogue - the string
  // is never offered for translation and renders English forever, in
  // every language, silently. That is exactly what happened here first
  // time round.
  function healthLabel(health) {
    switch (health) {
      case 'healthy':
        return t('Healthy');
      case 'degraded':
        return t('Degraded');
      case 'unreachable':
        return t('Unreachable');
      case 'not-configured':
        return t('Not configured');
      default:
        // An unrecognised value from a newer server: show it as-is
        // rather than nothing.
        return health;
    }
  }

  function healthBadge(health) {
    const variant = HEALTH_VARIANT[health] || 'neutral';
    return `<vox-badge variant="${variant}">${escapeHtml(healthLabel(health))}</vox-badge>`;
  }

  // Rendered from the start timestamp each instance reports rather than
  // a duration it computed: a duration is wrong by however long the
  // reply took, and this also makes clock skew between instances visible
  // instead of hiding it.
  function uptime(startedAt) {
    const started = new Date(startedAt);
    const seconds = Math.floor((Date.now() - started.getTime()) / 1000);

    if (seconds < 0) {
      // The instance believes it started in the future, which means its
      // clock disagrees with this one. Say so rather than rendering a
      // nonsensical negative age.
      return t('clock skew');
    }
    if (seconds < 60) return `${seconds}s`;
    if (seconds < 3600) return `${Math.floor(seconds / 60)}m`;
    if (seconds < 86400) return `${Math.floor(seconds / 3600)}h`;
    return `${Math.floor(seconds / 86400)}d`;
  }

  // One badge per worker, wrapped, rather than a comma-joined string.
  // An `all`-mode instance runs seven of them, and as running text they
  // read as one long line nobody scans - the point of the column is to
  // let someone check at a glance whether a particular worker is
  // running here.
  function workerChips(workers) {
    if (!workers || workers.length === 0) {
      return `<span class="vox-ts-sm">${escapeHtml(t('none'))}</span>`;
    }
    return `<div class="worker-chips">${workers
      .map((w) => `<vox-badge variant="neutral">${escapeHtml(w)}</vox-badge>`)
      .join('')}</div>`;
  }

  function renderIncomplete(stack) {
    if (!stack.incomplete) {
      incompleteNotice.hidden = true;
      incompleteNotice.innerHTML = '';
      return;
    }

    const reason = stack.incompleteReason || t('Some instances did not reply.');
    incompleteNotice.hidden = false;
    incompleteNotice.innerHTML = `
      <vox-alert variant="warning">
        <strong>${escapeHtml(t('This picture may be incomplete.'))}</strong>
        ${escapeHtml(reason)}
      </vox-alert>`;
  }

  function renderInstances(stack) {
    if (!stack.modes || stack.modes.length === 0) {
      instances.innerHTML = `
        <vox-empty-state heading="${t('No instances reported')}">
          <vox-icon slot="icon" name="pulse" size="lg"></vox-icon>
          ${t('No console instance answered the status request.')}
        </vox-empty-state>`;
      return;
    }

    instances.innerHTML = stack.modes
      .map((group) => {
        const rows = group.instances
          .map(
            (i) => `
          <tr>
            <td>${escapeHtml(i.hostname || '')}</td>
            <td><code>${escapeHtml(i.address || '')}</code></td>
            <td>${healthBadge(i.health)}</td>
            <td>${escapeHtml(uptime(i.startedAt))}</td>
            <td>${escapeHtml(i.version || '')}</td>
            <td>${workerChips(i.workers)}</td>
          </tr>`
          )
          .join('');

        return `
          <section class="vox-m-bottom-lg">
            <h2 class="vox-ts-lg vox-m-bottom-sm">
              ${escapeHtml(group.mode)}
              <vox-badge variant="neutral">${group.count}</vox-badge>
            </h2>
            <div class="vox-table-wrap">
              <table class="vox-table vox-table--striped">
                <thead>
                  <tr>
                    <th scope="col">${t('Host')}</th>
                    <th scope="col">${t('Address')}</th>
                    <th scope="col">${t('Health')}</th>
                    <th scope="col">${t('Uptime')}</th>
                    <th scope="col">${t('Version')}</th>
                    <th scope="col">${t('Workers')}</th>
                  </tr>
                </thead>
                <tbody>${rows}</tbody>
              </table>
            </div>
          </section>`;
      })
      .join('');
  }

  function renderDependencies(stack) {
    const deps = stack.dependencies || [];
    if (deps.length === 0) {
      dependencies.innerHTML = `
        <vox-empty-state heading="${t('No dependencies reported')}">
          <vox-icon slot="icon" name="plug" size="lg"></vox-icon>
        </vox-empty-state>`;
      return;
    }

    const disagreements = new Set(stack.disagreements || []);
    const rows = deps
      .map((d) => {
        // A dependency other instances see differently is called out:
        // "reachable from here" and "reachable" are different claims,
        // and the difference is usually the whole story.
        const note = disagreements.has(d.name)
          ? ` <vox-badge variant="warning">${escapeHtml(t('differs by instance'))}</vox-badge>`
          : '';
        return `
        <tr>
          <td>${escapeHtml(d.name)}${note}</td>
          <td><code>${escapeHtml(d.target || '')}</code></td>
          <td>${healthBadge(d.health)}</td>
          <td>${escapeHtml(d.detail || '')}</td>
        </tr>`;
      })
      .join('');

    dependencies.innerHTML = `
      <div class="vox-table-wrap">
        <table class="vox-table vox-table--striped">
          <thead>
            <tr>
              <th scope="col">${t('Service')}</th>
              <th scope="col">${t('Target')}</th>
              <th scope="col">${t('Health')}</th>
              <th scope="col">${t('Detail')}</th>
            </tr>
          </thead>
          <tbody>${rows}</tbody>
        </table>
      </div>`;
  }

  async function load() {
    const stack = await withLoading(instances, () => fetchJSON('/api/v1/status'));
    renderIncomplete(stack);
    renderInstances(stack);
    renderDependencies(stack);
  }

  load();
}
