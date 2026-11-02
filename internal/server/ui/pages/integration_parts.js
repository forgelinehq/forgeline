// Pieces shared by the integrations hub popup and the connect wizard.
import { h, icon, mount, relTime } from '../lib/dom.js';
import { testSummary } from '../lib/hub.js';

const MARK = { ok: ['check', 'text-emerald-600 dark:text-emerald-400'], warn: ['alert', 'text-amber-600 dark:text-amber-400'], fail: ['x', 'text-rose-600 dark:text-rose-400'] };

/** The checks of a test, one line each: what it checked, what it found, and the fix when there is one. */
export function renderChecks(box, res, earlier = false) {
  const rows = res.checks.map(c => h('div', { class: 'flex items-start gap-3 rounded-lg border px-3 py-2 text-sm ' + (c.result === 'fail' ? 'border-transparent bg-rose-50 dark:bg-rose-500/10' : c.result === 'warn' ? 'border-transparent bg-amber-50 dark:bg-amber-500/10' : 'border-slate-200 dark:border-slate-800'), 'data-result': c.result },
    icon(MARK[c.result][0], 'mt-0.5 size-4 shrink-0 ' + MARK[c.result][1]),
    h('div', { class: 'min-w-0 flex-1' }, h('div', { class: 'font-medium' }, c.name), c.detail ? h('div', { class: 'muted text-xs' }, c.detail) : null,
      c.fix ? h('div', { class: 'mt-0.5 text-xs font-medium text-slate-700 dark:text-slate-300' }, 'To fix: ' + c.fix) : null),
    h('span', { class: 'muted shrink-0 text-xs tabular-nums' }, (c.took_ms / 1000).toFixed(1) + ' s')));
  mount(box, earlier ? h('div', { class: 'muted text-xs' }, 'Last test, ' + relTime(res.at) + (res.by ? ' by ' + res.by : '') + ':') : null, ...rows, h('p', { class: 'muted text-xs', id: 'integration-test-summary' }, testSummary(res)));
}
