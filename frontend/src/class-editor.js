// The classes editor on a node group's page: classes chosen from the
// code of the group's environment, their declared parameters, and a
// value input suited to each parameter's type - see design.md in
// add-class-discovery-and-run-options.
//
// The list of classes is a help, never a gate. A class or parameter
// that is not in it can still be entered and saved - the code may just
// not be deployed yet - and is marked rather than refused. When the list
// cannot be had at all, everything is entered by name and nothing is
// marked.
import { fetchJSON, sendJSON, escapeHtml, hasPermission, t, tn, formatDateTime } from './app.js';

// How a parameter's value is entered, from its declared Puppet type.
// The server stays the judge of whether a value fits; this only picks a
// control. Anything not recognised is entered as JSON, which can express
// every value a class parameter can hold.
export function inputKind(type) {
  let s = (type || '').trim();
  // Optional[T] and Sensitive[T] hold a T as far as entering one goes.
  for (;;) {
    const m = /^(Optional|Sensitive)\[(.*)\]$/s.exec(s);
    if (!m) break;
    s = m[2].trim();
  }
  if (s === 'Boolean') return { kind: 'boolean' };
  if (/^Integer(\[.*\])?$/s.test(s)) return { kind: 'integer' };
  if (/^(Float|Numeric)(\[.*\])?$/s.test(s)) return { kind: 'number' };
  if (/^(String|Pattern)(\[.*\])?$/s.test(s)) return { kind: 'string' };
  const e = /^Enum\[(.*)\]$/s.exec(s);
  if (e) {
    const options = [];
    const re = /'((?:[^'\\]|\\.)*)'|"((?:[^"\\]|\\.)*)"/g;
    let m;
    while ((m = re.exec(e[1])) !== null) options.push(m[1] ?? m[2]);
    if (options.length > 0) return { kind: 'enum', options };
  }
  return { kind: 'json' };
}

// Whether value is the JavaScript type the control for kind produces.
// A stored value that is not - text on an Integer parameter, say - is
// shown as JSON instead, so that opening and saving the group leaves it
// exactly as it was.
function fits(input, value) {
  switch (input.kind) {
    case 'boolean':
      return typeof value === 'boolean';
    case 'integer':
      return Number.isInteger(value);
    case 'number':
      return typeof value === 'number';
    case 'string':
      return typeof value === 'string';
    case 'enum':
      return typeof value === 'string' && input.options.includes(value);
    default:
      return true;
  }
}

// An error the group page shows as it is, rather than as a JSON syntax
// error in "one of the fields".
function editorError(message) {
  const err = new Error(message);
  err.classEditor = true;
  return err;
}

function unavailableText(reason, environment) {
  switch (reason) {
    case 'not_configured':
      return t('The list of classes is unavailable: this console has no connection to the Puppet server. An administrator sets CONSOLE_PUPPET_SERVER_URL to enable it. Classes and parameters can still be entered by name.');
    case 'forbidden':
      return t("The list of classes is unavailable: the Puppet server does not allow the console to read classes. An administrator adds a rule for /puppet/v3/environment_classes to the server's auth.conf - see the installation guide. Classes and parameters can still be entered by name.");
    case 'not_found':
      return t('The Puppet server has no environment named {environment}, so its classes cannot be listed. Classes and parameters can still be entered by name.', { environment });
    default:
      return t('The list of classes is unavailable: the Puppet server could not be reached. Classes and parameters can still be entered by name.');
  }
}

/**
 * Builds the editor inside root (the markup is in group.tmpl).
 *
 * @param {object} options
 * @param {HTMLElement} options.root the .classes-editor element.
 * @returns {{
 *   setClasses(classes: object[], serverWarnings?: object[]): void,
 *   getClasses(): object[],
 *   setEnvironment(environment: string): Promise<void>,
 * }} getClasses throws an Error whose message says what to fix.
 */
export function createClassEditor({ root }) {
  const builderEl = root.querySelector('[data-classes-builder]');
  const jsonEl = root.querySelector('[data-classes-json]');
  const jsonToggle = root.querySelector('[data-classes-json-toggle]');
  const sourceEl = root.querySelector('[data-classes-source]');
  const noticeEl = root.querySelector('[data-classes-notice]');
  const errorEl = root.querySelector('[data-classes-error]');
  const addRowEl = root.querySelector('[data-classes-add]');
  const addInput = root.querySelector('[data-class-add-name]');
  const addButton = root.querySelector('[data-class-add-button]');
  const classOptions = root.querySelector('#class-options');
  const flagsEl = root.querySelector('[data-classes-flags]');

  // The classes being edited: [{ name, params: [{ name, value, kind,
  // text }] }]. `value` is the value as stored; `text` is what is in a
  // JSON or number control while it is being typed.
  let rows = [];
  // The environment's classes, or null when they are unavailable.
  let catalog = null;
  let environment = 'production';
  // The server's type warnings for the classes as last loaded, keyed
  // "class\nparameter". Dropped for a parameter once its value is edited.
  let typeWarnings = new Map();
  let loadSeq = 0;

  const canRefresh = hasPermission('classifier:write');

  function declared(className) {
    if (!catalog) return null;
    return catalog.classes.find((c) => c.name === className) || null;
  }

  // Whether what the catalogue says can be relied on to call something
  // missing: not when it is an earlier answer, and not when a manifest
  // failed to parse - the class may be in that manifest.
  function canFlagMissing() {
    return catalog !== null && !catalog.stale && catalog.errors.length === 0;
  }

  function makeParam(className, name, value) {
    const decl = declared(className)?.parameters.find((p) => p.name === name);
    let input = decl ? inputKind(decl.type) : { kind: 'json' };
    if (!fits(input, value)) input = { kind: 'json' };
    return { name, value, input, text: input.kind === 'json' ? JSON.stringify(value) : String(value ?? '') };
  }

  function toRows(classes) {
    return (classes || []).map((c) => ({
      name: String(c.name ?? ''),
      params: Object.entries(c.parameters || {}).map(([name, value]) => makeParam(c.name, name, value)),
    }));
  }

  // The classes as they will be saved. Throws when a value cannot be
  // read, naming the class and parameter.
  function fromRows() {
    return rows.map((row) => {
      const parameters = {};
      for (const p of row.params) {
        const where = { class: row.name, parameter: p.name };
        switch (p.input.kind) {
          case 'boolean':
          case 'string':
          case 'enum':
            parameters[p.name] = p.value;
            break;
          case 'integer':
          case 'number': {
            const n = Number(p.text);
            if (p.text.trim() === '' || !Number.isFinite(n)) {
              throw editorError(t('Parameter {parameter} of class {class} needs a number. Enter one, or remove the parameter to leave it unset.', where));
            }
            parameters[p.name] = n;
            break;
          }
          default:
            try {
              parameters[p.name] = JSON.parse(p.text);
            } catch {
              throw editorError(t('Parameter {parameter} of class {class} is not valid JSON. Text needs double quotes, like "value".', where));
            }
        }
      }
      return { name: row.name, parameters };
    });
  }

  function paramControl(row, p, ci, pi) {
    const decl = declared(row.name)?.parameters.find((d) => d.name === p.name);
    const notes = [];
    if (decl?.type) notes.push(decl.type);
    if (decl?.defaultSource) notes.push(t('Default: {default}', { default: decl.defaultSource }));
    else if (decl?.required) notes.push(t('No default in the code'));
    if (p.input.kind === 'json') notes.push(t('Entered as JSON'));
    const common = `data-ci="${ci}" data-pi="${pi}" class="class-param-value" label="${escapeHtml(p.name)}" note="${escapeHtml(notes.join(' · '))}"`;

    switch (p.input.kind) {
      case 'boolean':
        return `<vox-select ${common} data-kind="boolean">
          <option value="true"${p.value === true ? ' selected' : ''}>true</option>
          <option value="false"${p.value === false ? ' selected' : ''}>false</option>
        </vox-select>`;
      case 'enum':
        return `<vox-select ${common} data-kind="enum">${p.input.options
          .map((o) => `<option value="${escapeHtml(o)}"${o === p.value ? ' selected' : ''}>${escapeHtml(o)}</option>`)
          .join('')}</vox-select>`;
      case 'integer':
        return `<vox-input type="number" step="1" ${common} data-kind="integer" value="${escapeHtml(p.text)}"></vox-input>`;
      case 'number':
        return `<vox-input type="number" step="any" ${common} data-kind="number" value="${escapeHtml(p.text)}"></vox-input>`;
      case 'string':
        return `<vox-input ${common} data-kind="string" value="${escapeHtml(p.text)}"></vox-input>`;
      default:
        return `<vox-input ${common} data-kind="json" value="${escapeHtml(p.text)}" placeholder='"text", 42, true, ["a", "b"]'></vox-input>`;
    }
  }

  function paramFlag(row, p) {
    const decl = declared(row.name);
    if (decl && !decl.parameters.some((d) => d.name === p.name) && !catalog.stale) {
      return `<vox-badge variant="warning">${escapeHtml(t('Not a parameter of this class'))}</vox-badge>`;
    }
    const detail = typeWarnings.get(`${row.name}\n${p.name}`);
    if (detail) {
      return `<vox-badge variant="warning">${escapeHtml(t('Does not fit the type: {detail}', { detail }))}</vox-badge>`;
    }
    return '';
  }

  function classCard(row, ci) {
    const decl = declared(row.name);
    const missing = !decl && canFlagMissing();
    const unset = decl ? decl.parameters.filter((d) => !row.params.some((p) => p.name === d.name)) : [];

    const params = row.params
      .map((p, pi) => `
        <div class="class-param">
          <div class="class-param-field">${paramControl(row, p, ci, pi)}</div>
          <vox-button class="class-param-remove" variant="alt" size="sm" data-remove-param data-ci="${ci}" data-pi="${pi}"
            aria-label="${escapeHtml(t('Remove parameter {parameter}', { parameter: p.name }))}">${escapeHtml(t('Remove'))}</vox-button>
          <div class="class-param-flag">${paramFlag(row, p)}</div>
        </div>`)
      .join('');

    return `
      <div class="class-card vox-border-all vox-radius-md vox-bg-soft vox-p-all-md vox-m-bottom-md">
        <div class="vox-display-flex vox-justify-between vox-items-center vox-gap-md vox-flex-wrap">
          <div class="vox-display-flex vox-items-center vox-gap-sm vox-flex-wrap">
            <code class="class-name">${escapeHtml(row.name)}</code>
            ${missing ? `<vox-badge variant="warning">${escapeHtml(t('Not defined in {environment}', { environment }))}</vox-badge>` : ''}
          </div>
          <vox-button variant="danger" size="sm" data-remove-class data-ci="${ci}"
            aria-label="${escapeHtml(t('Remove class {class}', { class: row.name }))}">${escapeHtml(t('Remove'))}</vox-button>
        </div>
        ${params ? `<div class="class-params vox-m-top-md">${params}</div>` : `<p class="vox-ts-sm vox-color-text-2 vox-m-top-sm vox-m-bottom-none">${escapeHtml(t("No parameters set: the code's own defaults and Hiera apply."))}</p>`}
        <div class="class-param-add vox-m-top-md">
          <div class="native-field">
            <label for="param-add-${ci}">${escapeHtml(t('Set a parameter'))}</label>
            <input id="param-add-${ci}" class="native-control" list="param-options-${ci}" data-param-add-name data-ci="${ci}"
              autocomplete="off" spellcheck="false" placeholder="${escapeHtml(decl ? t('Choose or type a parameter') : t('Parameter name'))}">
            <datalist id="param-options-${ci}">${unset
              .map((d) => `<option value="${escapeHtml(d.name)}">${escapeHtml(d.type || '')}</option>`)
              .join('')}</datalist>
          </div>
          <vox-button size="sm" data-add-param data-ci="${ci}">${escapeHtml(t('Add parameter'))}</vox-button>
        </div>
      </div>`;
  }

  function renderBuilder() {
    builderEl.innerHTML = rows.length === 0
      ? `<p class="vox-ts-sm vox-color-text-2">${escapeHtml(t('No classes yet. Nodes in this group get only what their other groups give them.'))}</p>`
      : rows.map(classCard).join('');

    builderEl.querySelectorAll('.class-param-value').forEach((el) => {
      const p = rows[Number(el.dataset.ci)].params[Number(el.dataset.pi)];
      const className = rows[Number(el.dataset.ci)].name;
      const update = () => {
        switch (el.dataset.kind) {
          case 'boolean':
            p.value = el.value === 'true';
            break;
          case 'string':
          case 'enum':
            p.value = el.value;
            break;
          default:
            p.text = el.value;
        }
        // The server's remark was about the value as loaded.
        typeWarnings.delete(`${className}\n${p.name}`);
      };
      el.addEventListener('input', update);
      el.addEventListener('change', update);
    });

    builderEl.querySelectorAll('[data-remove-class]').forEach((el) => {
      el.addEventListener('click', () => {
        rows.splice(Number(el.dataset.ci), 1);
        render();
      });
    });
    builderEl.querySelectorAll('[data-remove-param]').forEach((el) => {
      el.addEventListener('click', () => {
        rows[Number(el.dataset.ci)].params.splice(Number(el.dataset.pi), 1);
        render();
      });
    });

    const addParam = (ci) => {
      const input = builderEl.querySelector(`[data-param-add-name][data-ci="${ci}"]`);
      const name = input.value.trim();
      const row = rows[ci];
      errorEl.innerHTML = '';
      if (!name) return;
      if (row.params.some((p) => p.name === name)) {
        showError(t('Class {class} already sets parameter {parameter}.', { class: row.name, parameter: name }));
        return;
      }
      // Starts from the code's default when it is a literal, so the
      // field shows what the node would otherwise have had.
      const decl = declared(row.name)?.parameters.find((d) => d.name === name);
      let initial = '';
      if (decl && decl.default !== undefined) initial = decl.default;
      else if (decl) {
        const input = inputKind(decl.type);
        if (input.kind === 'boolean') initial = false;
        else if (input.kind === 'enum') initial = input.options[0];
        else if (input.kind === 'integer' || input.kind === 'number') initial = 0;
      }
      row.params.push(makeParam(row.name, name, initial));
      render();
      builderEl.querySelector(`.class-param-value[data-ci="${ci}"][data-pi="${row.params.length - 1}"]`)?.focus();
    };
    builderEl.querySelectorAll('[data-add-param]').forEach((el) => {
      el.addEventListener('click', () => addParam(Number(el.dataset.ci)));
    });
    builderEl.querySelectorAll('[data-param-add-name]').forEach((el) => {
      el.addEventListener('keydown', (ev) => {
        if (ev.key === 'Enter') {
          ev.preventDefault();
          addParam(Number(el.dataset.ci));
        }
      });
    });
  }

  function renderSource() {
    if (!catalog) {
      sourceEl.innerHTML = '';
      return;
    }
    const text = t('Choosing from the classes in the {environment} environment, as read at {time}.', {
      environment: catalog.environment,
      time: formatDateTime(catalog.fetchedAt),
    });
    sourceEl.innerHTML = `
      <span>${escapeHtml(text)}</span>
      ${canRefresh ? `<vox-button variant="alt" size="sm" data-classes-refresh>${escapeHtml(t('Refresh classes'))}</vox-button>` : ''}`;
    sourceEl.querySelector('[data-classes-refresh]')?.addEventListener('click', () => load(true));
  }

  function renderNotices(unavailable) {
    const notices = [];
    if (unavailable) {
      notices.push(`<vox-alert variant="info">${escapeHtml(unavailableText(unavailable, environment))}</vox-alert>`);
    }
    if (catalog?.stale) {
      notices.push(`<vox-alert variant="warning">${escapeHtml(t('The Puppet server could not be reached just now, so this list of classes may be out of date. Nothing is marked as missing until it can be read again.'))}</vox-alert>`);
    }
    if (catalog && catalog.errors.length > 0) {
      const items = catalog.errors
        .map((e) => `<li><code>${escapeHtml(e.file)}</code>: ${escapeHtml(e.error)}</li>`)
        .join('');
      notices.push(`<vox-alert variant="warning">${escapeHtml(t('These manifests could not be parsed, so their classes are not in the list, and no class is marked as undefined:'))}<ul>${items}</ul></vox-alert>`);
    }
    noticeEl.innerHTML = notices.join('');
  }

  function renderOptions() {
    const used = new Set(rows.map((r) => r.name));
    classOptions.innerHTML = (catalog?.classes || [])
      .filter((c) => !used.has(c.name))
      .map((c) => `<option value="${escapeHtml(c.name)}"></option>`)
      .join('');
    addInput.placeholder = catalog ? t('Choose or type a class') : t('Class name, like profile::base');
  }

  // How many classes, parameters and values are marked, said once above
  // the list so a mark far down a long group is not missed.
  function renderFlagSummary() {
    let count = typeWarnings.size;
    for (const row of rows) {
      const decl = declared(row.name);
      if (!decl) {
        if (canFlagMissing()) count += 1;
        continue;
      }
      if (catalog.stale) continue;
      count += row.params.filter((p) => !decl.parameters.some((d) => d.name === p.name)).length;
    }
    flagsEl.innerHTML = count === 0 || jsonToggle.checked
      ? ''
      : `<vox-alert variant="warning">${escapeHtml(tn(
        '{count} class or parameter below does not match the code in the {environment} environment. It is saved and applied as it is; a node whose catalog needs it will fail to compile until the code or the group is changed.',
        '{count} classes or parameters below do not match the code in the {environment} environment. They are saved and applied as they are; a node whose catalog needs them will fail to compile until the code or the group is changed.',
        count,
        { count, environment },
      ))}</vox-alert>`;
  }

  function render() {
    renderOptions();
    if (!jsonToggle.checked) renderBuilder();
    renderFlagSummary();
  }

  function showError(message) {
    errorEl.innerHTML = `<vox-alert variant="danger">${escapeHtml(message)}</vox-alert>`;
  }

  function addClass() {
    const name = addInput.value.trim();
    errorEl.innerHTML = '';
    if (!name) return;
    if (rows.some((r) => r.name === name)) {
      showError(t('This group already has class {class}.', { class: name }));
      return;
    }
    rows.push({ name, params: [] });
    addInput.value = '';
    render();
  }
  addButton.addEventListener('click', addClass);
  addInput.addEventListener('keydown', (ev) => {
    if (ev.key === 'Enter') {
      ev.preventDefault();
      addClass();
    }
  });

  // Only one of {rows, the JSON text} is live at a time, matching the
  // view on screen - the same arrangement as the match rule's editor.
  // Switching to JSON always works; switching back needs the JSON to
  // parse into a list of classes, and stays put if it does not.
  jsonToggle.addEventListener('change', () => {
    errorEl.innerHTML = '';
    if (jsonToggle.checked) {
      let classes;
      try {
        classes = fromRows();
      } catch (err) {
        showError(err.message);
        jsonToggle.checked = false;
        return;
      }
      jsonEl.value = JSON.stringify(classes, null, 2);
      builderEl.style.display = 'none';
      addRowEl.style.display = 'none';
      jsonEl.style.display = '';
      renderFlagSummary();
      return;
    }
    let parsed;
    try {
      parsed = parseJSON();
    } catch (err) {
      showError(err.message);
      jsonToggle.checked = true;
      return;
    }
    rows = toRows(parsed);
    jsonEl.style.display = 'none';
    builderEl.style.display = '';
    addRowEl.style.display = '';
    render();
  });

  function parseJSON() {
    const raw = jsonEl.value.trim();
    let parsed;
    try {
      parsed = raw === '' ? [] : JSON.parse(raw);
    } catch (err) {
      throw editorError(t('Invalid JSON in the classes: {error}', { error: err.message }));
    }
    const ok = Array.isArray(parsed) && parsed.every((c) => c && typeof c === 'object' && typeof c.name === 'string'
      && (c.parameters === undefined || c.parameters === null || (typeof c.parameters === 'object' && !Array.isArray(c.parameters))));
    if (!ok) {
      throw editorError(t('Classes must be a JSON list of objects, each with a "name" and optional "parameters".'));
    }
    return parsed.map((c) => ({ name: c.name, parameters: c.parameters || {} }));
  }

  async function load(refresh) {
    const seq = ++loadSeq;
    const url = `/api/v1/classes${refresh ? '/refresh' : ''}?environment=${encodeURIComponent(environment)}`;
    let next = null;
    let unavailable = null;
    try {
      next = refresh ? await sendJSON(url, 'POST') : await fetchJSON(url);
      next.classes = next.classes || [];
      next.errors = next.errors || [];
    } catch (err) {
      unavailable = err.status === 404 ? 'not_found' : err.reason || 'unreachable';
    }
    // A slower answer for an environment since changed is discarded.
    if (seq !== loadSeq) return;

    // The rows are rebuilt against the new catalogue, since which
    // control a value gets depends on the type its class declares.
    let classes = null;
    try {
      classes = jsonToggle.checked ? null : fromRows();
    } catch {
      classes = null;
    }
    catalog = next;
    if (classes) rows = toRows(classes);
    renderSource();
    renderNotices(unavailable);
    render();
  }

  return {
    setClasses(classes, serverWarnings) {
      typeWarnings = new Map(
        (serverWarnings || [])
          .filter((w) => w.kind === 'parameter-type')
          .map((w) => [`${w.class}\n${w.parameter}`, w.detail || '']),
      );
      rows = toRows(classes);
      if (jsonToggle.checked) jsonEl.value = JSON.stringify(classes || [], null, 2);
      render();
    },
    getClasses() {
      return jsonToggle.checked ? parseJSON() : fromRows();
    },
    setEnvironment(next) {
      if (next === environment && (catalog !== null || loadSeq > 0)) return Promise.resolve();
      environment = next;
      return load(false);
    },
  };
}
