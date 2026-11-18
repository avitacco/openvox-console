// Choosing which nodes a job runs on: a list of nodes, a PQL query, or a
// node group (see specs/orchestrator's "Console UI for choosing
// targets"). Used by the Run Puppet and Run task dialogs when they are
// opened from the Jobs page, and by the schedule form.
//
// The preview asks the console which nodes the choice resolves to for
// this user right now. It is the same question a job would ask, so what
// it lists is what a job started now would run on - already narrowed to
// the nodes the user's role can act on - and what it refuses is what a
// job would be refused for.

import { fetchJSON, sendJSON, escapeHtml, treeOrder, t, tn } from './app.js';

// Shown in the empty query box.
const QUERY_EXAMPLE = 'inventory[certname] { facts.os.family = "Debian" }';

let pickerCount = 0;

/**
 * Sets a vox-select's value so that it also shows it. The component
 * copies its value to the select it wraps when its options arrive; a
 * value set after that is held but not shown unless the inner select
 * is told too.
 */
export function setSelectValue(el, value) {
  el.value = value;
  const inner = el.shadowRoot?.querySelector('select');
  if (inner) inner.value = value;
}

/**
 * Builds a target picker inside root.
 *
 * @param {HTMLElement} root
 * @param {object} [options]
 * @param {{targets?: string[], query?: string, group?: number}} [options.initial]
 * @param {boolean} [options.requirePreview] when true, isReady() is false
 *   until a preview of the current choice has succeeded.
 * @param {() => void} [options.onChange] called when the choice or its
 *   preview state changes.
 */
export function createTargetPicker(root, { initial = {}, requirePreview = false, onChange = () => {} } = {}) {
  const uid = `target-picker-${++pickerCount}`;
  root.classList.add('target-picker');
  root.innerHTML = `
    <vox-select data-target-kind label="${escapeHtml(t('Choose nodes by'))}">
      <option value="nodes">${escapeHtml(t('List of nodes'))}</option>
      <option value="query">${escapeHtml(t('Query'))}</option>
      <option value="group">${escapeHtml(t('Node group'))}</option>
    </vox-select>
    <div data-target-pane="nodes">
      <vox-textarea data-target-nodes rows="4" label="${escapeHtml(t('Nodes'))}"
        placeholder="web01.example.com&#10;web02.example.com"
        note="${escapeHtml(t('One certname per line. If any is outside your node groups, nothing is run on any of them.'))}"></vox-textarea>
    </div>
    <div data-target-pane="query" style="display: none">
      <div class="native-field">
        <label for="${uid}-query">${escapeHtml(t('PQL query'))}</label>
        <textarea id="${uid}-query" class="native-control target-query" rows="3" spellcheck="false" autocomplete="off"
          placeholder="${escapeHtml(QUERY_EXAMPLE)}" data-target-query></textarea>
        <p class="native-note">${escapeHtml(t('A query that returns certname. It is run when the job starts, and the job runs on the matching nodes in your node groups.'))}</p>
      </div>
    </div>
    <div data-target-pane="group" style="display: none">
      <vox-select data-target-group label="${escapeHtml(t('Node group'))}"
        note="${escapeHtml(t('The nodes in the group when the job starts.'))}"></vox-select>
    </div>
    <div class="target-preview-bar">
      <vox-button size="sm" data-target-preview>${escapeHtml(t('Preview nodes'))}</vox-button>
      <span data-target-count class="vox-ts-sm vox-color-text-2"></span>
    </div>
    <div data-target-error></div>
    <div data-target-result></div>`;

  const kindEl = root.querySelector('[data-target-kind]');
  const nodesEl = root.querySelector('[data-target-nodes]');
  const queryEl = root.querySelector('[data-target-query]');
  const groupEl = root.querySelector('[data-target-group]');
  const previewButton = root.querySelector('[data-target-preview]');
  const countEl = root.querySelector('[data-target-count]');
  const errorEl = root.querySelector('[data-target-error]');
  const resultEl = root.querySelector('[data-target-result]');

  let previewed = false;

  function kind() {
    return kindEl.value || 'nodes';
  }

  function showPane() {
    root.querySelectorAll('[data-target-pane]').forEach((pane) => {
      pane.style.display = pane.dataset.targetPane === kind() ? '' : 'none';
    });
  }

  // Any change to the choice makes the last preview about something
  // else.
  function invalidate() {
    previewed = false;
    countEl.textContent = '';
    resultEl.innerHTML = '';
    errorEl.innerHTML = '';
    onChange();
  }

  function getTarget() {
    switch (kind()) {
      case 'query':
        return { query: queryEl.value.trim() };
      case 'group':
        return groupEl.value ? { group: Number(groupEl.value) } : {};
      default:
        return {
          targets: [...new Set((nodesEl.value || '').split(/[\s,]+/).map((s) => s.trim()).filter(Boolean))],
        };
    }
  }

  function isEmpty() {
    const target = getTarget();
    return !(target.query || target.group !== undefined || (target.targets && target.targets.length > 0));
  }

  function showError(message) {
    // The console words a refusal as the rest of a sentence.
    const text = message ? message.charAt(0).toUpperCase() + message.slice(1) : '';
    errorEl.innerHTML = text ? `<vox-alert variant="danger">${escapeHtml(text)}</vox-alert>` : '';
  }

  async function preview() {
    showError('');
    resultEl.innerHTML = '';
    countEl.textContent = '';
    if (isEmpty()) {
      showError(emptyMessage());
      return false;
    }
    previewButton.disabled = true;
    try {
      const { nodes } = await sendJSON('/api/v1/orchestrator/targets/resolve', 'POST', getTarget());
      previewed = true;
      countEl.textContent = nodes.length === 0
        ? t('No nodes match at the moment.')
        : tn('{count} node', '{count} nodes', nodes.length);
      resultEl.innerHTML = nodes.length === 0
        ? ''
        : `<ul class="target-preview-list">${nodes.map((n) => `<li>${escapeHtml(n)}</li>`).join('')}</ul>`;
      onChange();
      return true;
    } catch (err) {
      previewed = false;
      showError(err.message);
      onChange();
      return false;
    } finally {
      previewButton.disabled = false;
    }
  }

  function emptyMessage() {
    switch (kind()) {
      case 'query':
        return t('Type a query first.');
      case 'group':
        return t('Choose a node group first.');
      default:
        return t('List at least one node first.');
    }
  }

  kindEl.addEventListener('change', () => {
    showPane();
    invalidate();
  });
  nodesEl.addEventListener('input', invalidate);
  queryEl.addEventListener('input', invalidate);
  groupEl.addEventListener('change', invalidate);
  previewButton.addEventListener('click', preview);

  // Node groups need classifier:read to list. Without it the choice
  // stays, with the reason, rather than vanishing; a group the form was
  // opened with is kept by id.
  async function loadGroups(selected) {
    let options = `<option value="">${escapeHtml(t('Choose a node group'))}</option>`;
    try {
      const { items } = await fetchJSON('/api/v1/groups?all=true');
      options += treeOrder(items)
        .map(({ group, depth }) => `<option value="${group.id}">${escapeHtml(`${'  '.repeat(depth)}${group.name}`)}</option>`)
        .join('');
      if (selected !== undefined && !items.some((g) => g.id === selected)) {
        options += `<option value="${selected}">${escapeHtml(t('Group {id} (deleted)', { id: selected }))}</option>`;
      }
    } catch {
      if (selected !== undefined) {
        options += `<option value="${selected}">${escapeHtml(t('Group {id}', { id: selected }))}</option>`;
      }
      groupEl.setAttribute('note', t('Node groups cannot be listed: your role needs to be able to read them.'));
    }
    groupEl.innerHTML = options;
    if (selected !== undefined) {
      groupEl.value = String(selected);
      requestAnimationFrame(() => setSelectValue(groupEl, String(selected)));
    }
  }

  // Initial choice.
  let initialKind = 'nodes';
  if (initial.query) {
    initialKind = 'query';
    queryEl.value = initial.query;
  } else if (initial.group !== undefined && initial.group !== null) {
    initialKind = 'group';
  } else if (initial.targets && initial.targets.length > 0) {
    nodesEl.value = initial.targets.join('\n');
  }
  setSelectValue(kindEl, initialKind);
  // The component is not upgraded with its options yet on a first
  // render; set it again once it is.
  customElements.whenDefined('vox-select').then(() => requestAnimationFrame(() => setSelectValue(kindEl, kindEl.value || initialKind)));
  showPane();
  const ready = loadGroups(initialKind === 'group' ? Number(initial.group) : undefined);

  // confirm is what a dialog calls before starting a job: nothing is
  // started until the console has said which nodes the choice means and
  // the user has seen them. It reports true only when the current
  // choice already has a preview on screen; otherwise it runs the
  // preview and says to look at it.
  async function confirm() {
    if (isEmpty()) {
      showError(emptyMessage());
      return false;
    }
    if (previewed) return true;
    if (await preview()) {
      errorEl.innerHTML = `<vox-alert variant="info">${escapeHtml(t('These are the nodes the job will run on. Check them, then start it.'))}</vox-alert>`;
    }
    return false;
  }

  return {
    getTarget,
    isEmpty,
    emptyMessage,
    preview,
    confirm,
    showError,
    /** Resolves once the node group list has loaded. */
    ready,
    /** True when the current choice has a successful preview (or none is required). */
    isReady: () => !isEmpty() && (!requirePreview || previewed),
  };
}

/**
 * A job's target source - the query or node group its nodes were chosen
 * by - as a short phrase, or '' for a job given a list of nodes.
 *
 * @param {{kind: string, query?: string, groupId?: number, groupName?: string}|undefined} source
 */
export function targetSourceHTML(source) {
  if (!source) return '';
  if (source.kind === 'group') {
    const name = escapeHtml(source.groupName || t('Group {id}', { id: source.groupId }));
    return `<a href="/group.html?id=${encodeURIComponent(source.groupId)}">${name}</a>`;
  }
  if (source.kind === 'query') {
    // Left out for a reader who is not shown every node the job ran on.
    return source.query ? `<code class="target-source-query">${escapeHtml(source.query)}</code>` : escapeHtml(t('a query'));
  }
  return '';
}
