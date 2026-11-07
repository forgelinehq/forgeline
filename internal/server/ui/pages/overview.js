import { h, icon, mount, pill, sparkline, relTime, absTime, fmtSeconds, duration, pct, kpi, pageHeader, emptyState, spinner, errorBox, toast, bytes } from '../lib/dom.js';
import { versionLabel } from '../lib/version.js';
import { get, post, atLeast } from '../lib/api.js';
import { navigate } from '../lib/router.js';
import { session } from '../app.js';
import { buildInbox, pulse } from '../lib/inbox.js';

export async function overviewPage() {
  const el = h('div', {});
  let timer = null;
  let first = true;
  let extra = {}; // the inbox's other sources, read every INBOX_EVERY loads
  let loads = 0;
  const INBOX_EVERY = 8; // 8 × 4 s: approvals, findings and connections change slowly

  // Each source is read only when this role may read it, and a failed read leaves it out: the
  // inbox then says less, never something wrong.
  async function loadExtra() {
    const dev = atLeast(session.role, 'developer');
    const [proposals, changes, findings, scans, integrations] = await Promise.all([
      get('/v1/ai/proposals').catch(() => null),
      get('/v1/cloud/changes').catch(() => null),
      get('/v1/guard/findings').catch(() => null),
      get('/v1/guard/scans').catch(() => null),
      dev ? get('/v1/integrations').catch(() => null) : null,
    ]);
    extra = { proposals: proposals?.proposals, changes: changes?.changes, findings, scans, integrations };
  }

  async function load() {
    try {
      if (loads++ % INBOX_EVERY === 0) await loadExtra();
      const [ov, rt] = await Promise.all([get('/v1/overview'), get('/v1/infra/runtime').catch(() => null)]);
      render(ov, rt);
    } catch (e) {
      if (first) mount(el, errorBox(e.message));
    }
    first = false;
  }

  function render(ov, rt) {
    const t = ov.totals;
    const finished = t.succeeded + t.failed;
    const rate = finished ? t.succeeded / finished : null;
    const canRun = atLeast(session.role, 'developer');

    const runNow = async (p) => {
      try {
        const run = await post('/v1/pipelines/' + encodeURIComponent(p.name) + '/run');
        toast('Started ' + p.name, 'ok');
        navigate('/runs/' + run.id);
      } catch (e) { toast(e.message, 'error'); }
    };

    const pipelineRows = ov.pipelines.map(p => {
      const open = () => navigate(p.saved ? '/pipelines/' + encodeURIComponent(p.name) : (p.latest ? '/runs/' + p.latest.id : '/pipelines'));
      return h('tr', { class: 'row-link', onclick: open, tabindex: '0', onkeydown: (e) => { if (e.key === 'Enter') open(); } },
        h('td', {}, h('div', { class: 'font-medium' }, p.name, !p.saved && h('span', { class: 'pill-idle ml-2 align-middle', title: 'Runs exist, but there is no saved definition in the console' }, 'not saved')),
          p.description && h('div', { class: 'muted max-w-xs truncate text-xs' }, p.description)),
        h('td', {}, p.latest ? pill(p.latest.status) : h('span', { class: 'muted text-xs' }, 'never run')),
        h('td', {}, sparkline(p.recent)),
        h('td', { class: 'tabular-nums' }, pct(p.success_rate)),
        h('td', { class: 'tabular-nums' }, p.avg_seconds != null ? fmtSeconds(p.avg_seconds) : '–'),
        h('td', { class: 'whitespace-nowrap text-sm' }, p.latest ? h('span', { title: absTime(p.latest.created_at) }, relTime(p.latest.created_at)) : '–',
          p.latest?.triggered_by && h('div', { class: 'muted text-xs' }, 'by ' + p.latest.triggered_by)),
        h('td', { class: 'text-right' }, canRun && p.saved && h('button', { class: 'btn-secondary btn-sm', type: 'button', title: 'Run ' + p.name,
          onclick: (e) => { e.stopPropagation(); runNow(p); } }, icon('play', 'size-3'), 'Run')));
    });

    const recentRows = ov.recent.map(r => h('tr', { class: 'row-link', onclick: () => navigate('/runs/' + r.id), tabindex: '0', onkeydown: (e) => { if (e.key === 'Enter') navigate('/runs/' + r.id); } },
      h('td', { class: 'font-medium' }, r.pipeline), h('td', {}, pill(r.status)),
      h('td', { class: 'muted text-sm' }, r.triggered_by ? r.triggered_by + ' · ' + r.trigger : r.trigger, r.source?.sha && h('span', { class: 'ml-1 font-mono text-xs' }, r.source.sha.slice(0, 7))),
      h('td', { class: 'whitespace-nowrap text-sm tabular-nums' }, duration(r.started_at, r.finished_at) || '–'),
      h('td', { class: 'muted whitespace-nowrap text-sm', title: absTime(r.created_at) }, relTime(r.created_at))));

    const inbox = buildInbox({ ...extra, overview: ov }, { name: session.name, role: session.role });
    const pl = pulse({ ...extra, overview: ov }, inbox);

    mount(el,
      pageHeader('Home', 'What needs you, and how everything is doing.',
        canRun && h('a', { href: '#/pipelines/new', class: 'btn-primary' }, icon('plus'), 'New pipeline')),
      pulseTiles(pl),
      inboxSection(inbox),
      platformStrip(rt),
      h('section', { class: 'mt-8' },
        h('div', { class: 'mb-3 flex flex-wrap items-baseline justify-between gap-2' },
          h('h2', { class: 'h-section' }, 'Pipelines'),
          h('span', { class: 'muted text-xs tabular-nums' }, `${t.running} running · ${t.queued} queued · ${pct(rate)} passed of the last ${t.window} runs`)),
        h('div', { class: 'card overflow-x-auto' }, ov.pipelines.length
          ? h('table', { class: 'tbl' }, h('thead', {}, h('tr', {}, ['Pipeline', 'Status', 'Last 12 runs', 'Success', 'Avg time', 'Last run', ''].map(x => h('th', {}, x)))), h('tbody', {}, pipelineRows))
          : emptyState('flow', 'No pipelines yet', 'Create your first pipeline in the editor, or send one from the command line.',
              canRun && h('a', { href: '#/pipelines/new', class: 'btn-primary' }, icon('plus'), 'Create a pipeline')))),
      ov.recent.length > 0 && h('section', { class: 'mt-8' },
        h('h2', { class: 'h-section mb-3' }, 'Recent runs'),
        h('div', { class: 'card overflow-x-auto' }, h('table', { class: 'tbl' },
          h('thead', {}, h('tr', {}, ['Pipeline', 'Status', 'Started by', 'Duration', 'When'].map(x => h('th', {}, x)))), h('tbody', {}, recentRows)))));
  }

  function pulseTiles(pl) {
    const tiles = [];
    tiles.push(kpi('Approvals', pl.act, pl.act ? 'waiting for you' : (pl.waiting ? pl.waiting + ' waiting on someone else' : 'nothing waiting'), pl.act ? 'bad' : null));
    if (pl.pipelines) tiles.push(kpi('Pipelines', pl.pipelines.ran ? `${pl.pipelines.healthy} of ${pl.pipelines.ran}` : '–',
      pl.pipelines.ran ? 'passed on their last run' : (pl.pipelines.total ? 'none has run yet' : 'none saved yet'),
      pl.pipelines.ran ? (pl.pipelines.healthy === pl.pipelines.ran ? 'ok' : 'bad') : null));
    const sec = pl.security;
    if (sec && sec.scanned === false) tiles.push(kpi('Security', '–', 'Guard has not scanned yet'));
    else if (sec) tiles.push(kpi('Security', sec.serious,
      (sec.serious ? 'serious findings open · ' + sec.open + ' in all' : sec.open + ' minor findings open') + (sec.last ? ' · scanned ' + relTime(sec.last) : ''), sec.serious ? 'bad' : 'ok'));
    if (pl.connections) tiles.push(kpi('Connections', pl.connections.connected, pl.connections.attention ? pl.connections.attention + ' need attention' : 'connected and working', pl.connections.attention ? 'bad' : 'ok'));
    return h('div', { class: 'grid grid-cols-2 gap-4 lg:grid-cols-4' }, tiles);
  }

  function inboxSection(items) {
    const SHOW = 6;
    const bar = { act: 'bg-brand-600', bad: 'bg-rose-500', warn: 'bg-amber-500', wait: 'bg-slate-300 dark:bg-slate-600' };
    const verb = { act: 'Review', bad: 'Open', warn: 'Open', wait: 'View' };
    const row = (i) => h('li', {},
      h('a', { href: '#' + i.href, class: 'card flex items-stretch gap-4 overflow-hidden pr-4 transition-colors hover:border-brand-300 dark:hover:border-brand-500/50' },
        h('span', { class: 'w-1 shrink-0 ' + bar[i.tone], 'aria-hidden': 'true' }),
        h('div', { class: 'min-w-0 flex-1 py-3' },
          h('div', { class: 'truncate text-sm font-medium' }, i.title),
          h('div', { class: 'muted truncate text-xs' }, i.detail, i.at && h('span', { title: absTime(i.at) }, ' · ' + relTime(i.at)))),
        h('span', { class: (i.tone === 'act' ? 'btn-primary' : 'btn-secondary') + ' btn-sm self-center' }, verb[i.tone])));
    const more = items.length - SHOW;
    return h('section', { class: 'mt-6', 'aria-labelledby': 'needs-you' },
      h('h2', { id: 'needs-you', class: 'h-section mb-3' }, 'Needs you'),
      items.length
        ? h('div', {}, h('ul', { class: 'space-y-2' }, items.slice(0, SHOW).map(row)),
            more > 0 && h('div', { class: 'muted mt-2 text-xs' }, `and ${more} more: see `, h('a', { href: '#/proposals', class: 'underline' }, 'Approvals'), ' and ', h('a', { href: '#/guard', class: 'underline' }, 'Guard')))
        : h('div', { class: 'card card-pad flex items-center gap-3 text-sm' }, icon('check', 'size-4 text-emerald-600 dark:text-emerald-400'),
            h('span', {}, 'Nothing needs you. Approvals, failed pipelines, serious security findings and broken connections show up here.')));
  }

  function platformStrip(rt) {
    if (!rt) return null;
    const d = rt.docker, s = rt.server;
    const item = (ok, label, detail) => h('div', { class: 'flex items-center gap-2 text-sm' },
      h('span', { class: 'size-2 rounded-full ' + (ok ? 'bg-emerald-500' : 'bg-rose-500') }), h('span', { class: 'font-medium' }, label), h('span', { class: 'muted' }, detail));
    return h('a', { href: '#/infrastructure', class: 'card mt-4 flex flex-wrap items-center gap-x-8 gap-y-2 px-5 py-3 transition-colors hover:border-brand-300 dark:hover:border-brand-500/50' },
      item(d.available, 'Docker', d.available ? `${d.version} · ${rt.steps.length} step container${rt.steps.length === 1 ? '' : 's'} running` : (d.error || 'not reachable')),
      item(true, 'Server', `${versionLabel(s.version)} · ${s.running} run${s.running === 1 ? '' : 's'} active, ${s.queued} queued · ${s.max_runs} worker${s.max_runs === 1 ? '' : 's'}`),
      s.disk_total_bytes ? item(s.disk_free_bytes / s.disk_total_bytes > 0.1, 'Disk', bytes(s.disk_free_bytes) + ' free') : null,
      h('span', { class: 'muted ml-auto flex items-center gap-1 text-xs' }, 'Infrastructure', icon('chevron', 'size-3')));
  }

  mount(el, spinner());
  await load();
  timer = setInterval(() => { if (!document.hidden) load(); }, 4000);
  el.cleanup = () => clearInterval(timer);
  return el;
}
