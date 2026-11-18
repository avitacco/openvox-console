// What the schedule pages share: how a schedule's action, nodes, timing,
// state and firings are worded.

import { escapeHtml, t, tn, N_, formatDateTime } from './app.js';
import { runOptionBadges } from './run-puppet.js';
import { stepCountText } from './plan-common.js';

/** The browser's time zone, as "Europe/Berlin", or '' if unknown. */
export function timeZoneName() {
  try {
    return Intl.DateTimeFormat().resolvedOptions().timeZone || '';
  } catch {
    return '';
  }
}

/**
 * A moment in the browser's time zone, with the zone named - a schedule
 * is read by people in other zones than the one who wrote it.
 */
export function formatWhen(value) {
  return formatDateTime(value, {
    year: 'numeric', month: 'short', day: 'numeric', hour: '2-digit', minute: '2-digit', timeZoneName: 'short',
  });
}

/**
 * A reason as the console words it - the rest of a sentence - made to
 * start one, for showing by itself.
 */
export function sentence(text) {
  return text ? text.charAt(0).toUpperCase() + text.slice(1) : '';
}

/** "Puppet run" with its option badges, the task's name, or the plan's. */
export function actionHTML(schedule) {
  if (schedule.kind === 'plan') {
    const steps = stepCountText((schedule.steps || []).length);
    return `${escapeHtml(t('Plan'))} <strong>${escapeHtml(schedule.planName || '')}</strong> <span class="vox-color-text-2">(${escapeHtml(steps)})</span>`;
  }
  if (schedule.kind === 'task') {
    return `${escapeHtml(t('Task'))} <code>${escapeHtml(schedule.task || '')}</code>`;
  }
  return `${escapeHtml(t('Puppet run'))} ${runOptionBadges(schedule.options)}`;
}

/**
 * Which nodes a schedule runs on. groupNames maps node group id to
 * name, where the caller could list groups.
 */
export function targetHTML(schedule, groupNames = new Map()) {
  if (schedule.query) {
    return `${escapeHtml(t('Nodes matching'))} <code class="target-source-query">${escapeHtml(schedule.query)}</code>`;
  }
  if (schedule.group !== undefined && schedule.group !== null) {
    const name = groupNames.get(schedule.group) || t('Group {id}', { id: schedule.group });
    return `${escapeHtml(t('Node group'))} <a href="/group.html?id=${encodeURIComponent(schedule.group)}">${escapeHtml(name)}</a>`;
  }
  const nodes = schedule.targets || [];
  const shown = nodes.slice(0, 3).map((n) => escapeHtml(n)).join(', ');
  const more = nodes.length > 3 ? ` ${escapeHtml(tn('and {count} more', 'and {count} more', nodes.length - 3))}` : '';
  return `${shown}${more}`;
}

/** "Once" or "Every 2 weeks". */
export function repeatText(schedule) {
  if (!schedule.repeat) return t('Once');
  const count = schedule.repeat.every;
  // One literal call per unit, so that each is found for translation
  // with its own plural forms.
  switch (schedule.repeat.unit) {
    case 'minute':
      return tn('Every {count} minute', 'Every {count} minutes', count);
    case 'hour':
      return tn('Every {count} hour', 'Every {count} hours', count);
    case 'day':
      return tn('Every {count} day', 'Every {count} days', count);
    case 'week':
      return tn('Every {count} week', 'Every {count} weeks', count);
    default:
      return `${count} ${schedule.repeat.unit}`;
  }
}

/** A schedule's state as a badge. */
export function stateBadge(schedule) {
  const states = {
    active: ['tip', t('Active')],
    paused: ['warning', t('Paused')],
    finished: ['neutral', t('Finished')],
  };
  const [variant, label] = states[schedule.state] || ['neutral', schedule.state];
  return `<vox-badge variant="${variant}">${escapeHtml(label)}</vox-badge>`;
}

// N_ marks each label for translation; t() looks it up when shown.
const OUTCOMES = {
  started: ['tip', N_('Started')],
  skipped: ['warning', N_('Skipped')],
  missed: ['warning', N_('Missed')],
  interrupted: ['danger', N_('Interrupted')],
  firing: ['neutral', N_('Starting')],
};

/** A firing's outcome as a badge. */
export function outcomeBadge(firing) {
  const [variant, label] = OUTCOMES[firing.outcome] || ['neutral', firing.outcome];
  return `<vox-badge variant="${variant}">${escapeHtml(t(label))}</vox-badge>`;
}
