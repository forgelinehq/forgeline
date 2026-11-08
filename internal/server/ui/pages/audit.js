// Settings › Audit log: read it, filter it, download it (CSV or JSON Lines), and choose how long it is kept
// (design note 033). Every download and every change of the period is itself in the log.
import { h, icon, mount, pageHeader, relTime, absTime, emptyState, spinner, errorBox, toast, confirmDialog } from '../lib/dom.js';
import { get, put } from '../lib/api.js';

const PERIODS = [[0, 'Forever'], [90, '90 days'], [180, '180 days'], [365, '1 year'], [730, '2 years'], [2555, '7 years']];
const periodWord = d => d === 0 ? 'forever' : (PERIODS.find(([v]) => v === d)?.[1] || d + ' days');

function retentionCard() {
  const card = h('section', { class: 'card card-pad mt-6', id: 'audit-retention' }, spinner());
  (async () => {
    let st;
    try { st = await get('/v1/settings/audit'); } catch (e) { return mount(card, errorBox(e.message)); }
    const options = PERIODS.some(([v]) => v === st.retention_days) ? PERIODS : [...PERIODS, [st.retention_days, st.retention_days + ' days']];
    const sel = h('select', { class: 'input w-40', id: 'audit-keep', 'aria-label': 'Keep the audit log for' },
      options.map(([v, l]) => h('option', { value: String(v), selected: v === st.retention_days }, l)));
    const save = h('button', { type: 'button', class: 'btn-secondary', id: 'audit-keep-save', onclick: async () => {
      const d = Number(sel.value);
      if (d !== 0 && (st.retention_days === 0 || d < st.retention_days)) {
        const ok = await confirmDialog({ title: 'Keep the audit log for ' + periodWord(d) + '?', danger: true, confirmLabel: 'Delete older events',
          body: 'Events older than ' + periodWord(d) + ' will be deleted within the hour and cannot be brought back. Download the log first if an auditor may need them.' });
        if (!ok) return;
      }
      save.disabled = true;
      try { await put('/v1/settings/audit', { retention_days: d }); toast('The audit log is now kept ' + (d ? 'for ' : '') + periodWord(d), 'ok'); mount(card, spinner()); card.replaceWith(retentionCard()); }
      catch (e) { toast(e.message, 'error'); save.disabled = false; }
    } }, 'Save');
    const facts = [st.count + ' events', st.oldest && 'oldest ' + absTime(st.oldest), st.last_prune && 'last tidied ' + relTime(st.last_prune) + (st.last_pruned ? ' (' + st.last_pruned + ' removed)' : '')].filter(Boolean).join(' · ');
    mount(card,
      h('h2', { class: 'text-base font-semibold' }, 'How long to keep it'),
      h('p', { class: 'muted mt-1 text-sm' }, 'Kept forever unless you choose a period. Older events are deleted once an hour, and each deletion is recorded here. The shortest period is 30 days.'),
      h('div', { class: 'mt-3 flex flex-wrap items-center gap-2' }, sel, save),
      h('p', { class: 'hint mt-2' }, facts));
  })();
  return card;
}

export async function auditPage() {
  const el = h('div', {});
  const actor = h('input', { id: 'f-actor', class: 'input w-40', placeholder: 'Person', 'aria-label': 'Filter by person' });
  const action = h('input', { id: 'f-action', class: 'input w-48', placeholder: 'Action, e.g. secret', 'aria-label': 'Filter by action' });
  const from = h('input', { id: 'f-from', type: 'date', class: 'input w-40', 'aria-label': 'From (first day)' });
  const to = h('input', { id: 'f-to', type: 'date', class: 'input w-40', 'aria-label': 'To (last day)' });
  const denied = h('input', { id: 'f-denied', type: 'checkbox', class: 'size-4 rounded' });
  const body = h('div', {});
  const csv = h('a', { class: 'btn-secondary', id: 'audit-csv', download: '' }, icon('download', 'size-3.5'), 'Download CSV');
  const jsonl = h('a', { class: 'btn-ghost', id: 'audit-jsonl', download: '' }, 'JSON Lines');

  const query = () => {
    const q = new URLSearchParams();
    if (actor.value.trim()) q.set('actor', actor.value.trim());
    if (action.value.trim()) q.set('action', action.value.trim());
    if (from.value) q.set('from', from.value);
    if (to.value) q.set('to', to.value);
    if (denied.checked) q.set('denied', '1');
    return q;
  };

  async function load() {
    const q = query();
    csv.href = '/v1/audit/export?' + new URLSearchParams([...q, ['format', 'csv']]);
    jsonl.href = '/v1/audit/export?' + new URLSearchParams([...q, ['format', 'jsonl']]);
    q.set('limit', '200');
    let events;
    try { events = (await get('/v1/audit?' + q)).events; } catch (e) { return mount(body, errorBox(e.message)); }
    mount(body, h('div', { class: 'card overflow-x-auto' }, events.length ? h('table', { class: 'tbl' },
      h('thead', {}, h('tr', {}, ['When', 'Who', 'What', 'On', 'Detail', 'From'].map(x => h('th', {}, x)))),
      h('tbody', {}, events.map(e => h('tr', {},
        h('td', { class: 'muted whitespace-nowrap text-sm', title: absTime(e.time) }, relTime(e.time)),
        h('td', { class: 'whitespace-nowrap font-medium' }, e.actor, e.role && h('span', { class: 'muted ml-1 text-xs' }, e.role)),
        h('td', {}, h('span', { class: (e.allowed ? 'pill-idle' : 'pill-bad') + ' font-mono' }, e.allowed ? e.action : e.action + ' · refused')),
        h('td', { class: 'min-w-28 break-all font-mono text-xs' }, e.target || ''), h('td', { class: 'min-w-48 max-w-xs text-sm' }, e.detail || ''), h('td', { class: 'muted whitespace-nowrap font-mono text-xs' }, e.ip || ''))))
    ) : emptyState('shield', 'Nothing matches', 'Try fewer filters.')),
      events.length === 200 && h('p', { class: 'hint mt-2' }, 'Showing the newest 200. The download has every event that matches.'));
  }

  let t;
  for (const i of [actor, action]) i.addEventListener('input', () => { clearTimeout(t); t = setTimeout(load, 300); });
  for (const i of [denied, from, to]) i.addEventListener('change', load);
  mount(el, pageHeader('Audit log', 'Who did what, including everything that was refused. It can be read and downloaded here and never edited.',
    h('button', { class: 'btn-ghost', type: 'button', onclick: load }, icon('refresh', 'size-3.5'), 'Refresh'), jsonl, csv),
    h('div', { class: 'mb-4 flex flex-wrap items-center gap-3' }, actor, action,
      h('label', { class: 'flex items-center gap-2 text-sm' }, 'From', from), h('label', { class: 'flex items-center gap-2 text-sm' }, 'to', to),
      h('label', { class: 'flex items-center gap-2 text-sm' }, denied, 'Only refused actions')),
    body, retentionCard());
  await load();
  return el;
}
