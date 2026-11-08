import { h, icon, mount, pill, sparkline, relTime, absTime, fmtSeconds, duration, pct, emptyState, spinner, errorBox, toast, bytes } from '../lib/dom.js';
import { versionLabel } from '../lib/version.js';
import { get, post, atLeast } from '../lib/api.js';
import { navigate } from '../lib/router.js';
import { session } from '../app.js';
import { buildInbox, pulse, greeting, summaryLine, recentChanges } from '../lib/inbox.js';
import { openAssistant } from '../lib/assistant.js';
import { setupCard } from './setup.js';

export async function overviewPage() {
  const el = h('div', { class: 'space-y-6' });
  const dyn = h('div', {}); // redrawn every few seconds; the ask bar above it is not, so typing is never lost
  let spend = null; // { text, sub } once loaded: Cost Explorer bills each call, so only on request
  let timer = null;
  let first = true;
  let extra = {}; // the inbox's other sources, read every INBOX_EVERY loads
  let loads = 0;
  const INBOX_EVERY = 8; // 8 × 4 s: approvals, findings and connections change slowly

  // Each source is read only when this role may read it, and a failed read leaves it out: the
  // inbox then says less, never something wrong.
  async function loadExtra() {
    const dev = atLeast(session.role, 'developer');
    const [proposals, changes, findings, scans, integrations, agents] = await Promise.all([
      get('/v1/ai/proposals').catch(() => null),
      get('/v1/cloud/changes').catch(() => null),
      get('/v1/guard/findings').catch(() => null),
      get('/v1/guard/scans').catch(() => null),
      dev ? get('/v1/integrations').catch(() => null) : null,
      dev ? get('/v1/ai/agents').catch(() => null) : null,
    ]);
    extra = { proposals: proposals?.proposals, changes: changes?.changes, findings, scans, integrations, agents: agents?.agents };
  }

  async function load() {
    try {
      if (loads++ % INBOX_EVERY === 0) await loadExtra();
      const [ov, rt] = await Promise.all([get('/v1/overview'), get('/v1/infra/runtime').catch(() => null)]);
      render(ov, rt);
    } catch (e) {
      if (first) mount(dyn, errorBox(e.message));
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

    const recent = recentChanges(extra);
    mount(dyn, h('div', { class: 'space-y-6' },
      h('div', { class: 'flex flex-wrap items-end justify-between gap-3' },
        h('div', { class: 'min-w-0' }, h('h1', { class: 'h-page', id: 'home-greeting' }, greeting(new Date().getHours(), session.user?.name || session.name)),
          h('p', { class: 'muted mt-1 text-sm', id: 'home-summary' }, summaryLine(inbox))),
        canRun && h('a', { href: '#/pipelines/new', class: 'btn-secondary' }, icon('plus'), 'New pipeline')),
      pulseTiles(pl),
      inboxSection(inbox),
      h('div', { class: 'grid gap-4 lg:grid-cols-2' }, startCard(), recentCard(recent)),
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
          h('thead', {}, h('tr', {}, ['Pipeline', 'Status', 'Started by', 'Duration', 'When'].map(x => h('th', {}, x)))), h('tbody', {}, recentRows))))));
  }

  // A tile: a label, a figure and a line under it; `sub` may be an element (a button).
  function tile(id, label, value, sub, tone) {
    const toneCls = tone === 'bad' ? 'text-rose-600 dark:text-rose-400' : tone === 'ok' ? 'text-emerald-600 dark:text-emerald-400' : '';
    return h('div', { class: 'card card-pad', 'data-tile': id }, h('div', { class: 'h-section' }, label),
      h('div', { class: 'mt-2 font-display text-[1.7rem] font-bold leading-tight tabular-nums ' + toneCls }, value), h('div', { class: 'muted mt-1 text-xs' }, sub));
  }

  async function loadSpend(btn) {
    btn.disabled = true; btn.textContent = 'Loading…';
    try {
      const c = await get('/v1/infra/aws/cost');
      spend = c.available ? { text: c.unit + ' ' + Number(c.total).toFixed(2), sub: 'last 30 days · ' + (c.services?.[0] ? 'most on ' + c.services[0].service : 'all services') }
        : { text: '–', sub: c.error || 'Cost data is not available for this account' };
    } catch (e) { spend = { text: '–', sub: e.message }; }
    load();
  }

  function pulseTiles(pl) {
    const dev = atLeast(session.role, 'developer');
    const tiles = [];
    if (pl.pipelines) tiles.push(tile('pipelines', 'Apps and pipelines', pl.pipelines.ran ? `${pl.pipelines.healthy} of ${pl.pipelines.ran}` : pl.pipelines.total + ' saved',
      pl.pipelines.ran ? 'passed on their last run' : (pl.pipelines.total ? 'none has run yet' : 'create one to start'),
      pl.pipelines.ran ? (pl.pipelines.healthy === pl.pipelines.ran ? 'ok' : 'bad') : null));
    if (dev) tiles.push(tile('spend', 'Spend', spend ? spend.text : '–', spend ? spend.sub
      : h('button', { type: 'button', class: 'font-medium text-brand-700 underline dark:text-brand-300', title: 'AWS bills each look at Cost Explorer, about a cent', onclick: (e) => loadSpend(e.target) }, 'Load from AWS')));
    if (dev && extra.agents) {
      const on = extra.agents.filter(a => !a.disabled);
      tiles.push(tile('team', 'Your AI team', on.length + ' on', extra.agents.length ? (extra.agents.length - on.length) + ' off · ' + extra.agents.length + ' in all' : 'no agents yet'));
    }
    const sec = pl.security;
    if (sec && sec.scanned === false) tiles.push(tile('security', 'Security', '–', 'Guard has not scanned yet'));
    else if (sec) tiles.push(tile('security', 'Security', sec.serious ? sec.serious + ' serious' : 'All clear',
      (sec.serious ? (sec.open - sec.serious) + ' minor' : sec.open + ' minor findings') + (sec.last ? ' · scanned ' + relTime(sec.last) : ''), sec.serious ? 'bad' : 'ok'));
    return h('div', { class: 'grid grid-cols-2 gap-4 lg:grid-cols-4' }, tiles);
  }

  function startCard() {
    const item = (n, title, text, attrs) => h(attrs.href ? 'a' : 'button', { type: attrs.href ? null : 'button', ...attrs,
      class: 'flex w-full items-center gap-3 rounded-xl border border-slate-200 p-3 text-left transition-colors hover:border-brand-300 dark:border-slate-700 dark:hover:border-brand-500/50' },
      h('span', { class: 'flex size-8 shrink-0 items-center justify-center rounded-lg bg-brand-50 font-display font-bold text-brand-700 dark:bg-brand-500/15 dark:text-brand-200' }, String(n)),
      h('span', { class: 'min-w-0' }, h('span', { class: 'block text-sm font-medium' }, title), h('span', { class: 'muted block text-xs' }, text)));
    const dev = atLeast(session.role, 'developer');
    return h('section', { class: 'card card-pad space-y-3', id: 'home-start' }, h('h2', { class: 'font-display text-lg font-bold' }, 'Start something'),
      atLeast(session.role, 'developer') && item(1, 'Build and test a GitHub repository', 'The assistant writes the pipeline; you press Confirm', { onclick: () => openAssistant('Set up a pipeline that builds and tests one of my GitHub repositories.') }),
      atLeast(session.role, 'developer') && item(2, 'Write a pipeline yourself', 'YAML with a live check and a graph', { href: '#/pipelines/new' }),
      dev && item(3, 'Hire an agent', 'From the marketplace: cost watcher, cluster doctor and more', { href: '#/marketplace' }),
      !dev && h('p', { class: 'muted text-sm' }, 'Ask an administrator for the developer role to create pipelines and agents.'));
  }

  function recentCard(items) {
    const dot = { ok: 'bg-emerald-500', bad: 'bg-rose-500', idle: 'bg-slate-400' };
    return h('section', { class: 'card card-pad flex flex-col gap-3', id: 'home-recent' }, h('h2', { class: 'font-display text-lg font-bold' }, 'Recent changes'),
      items.length ? h('ul', { class: 'space-y-2.5' }, items.map(r => h('li', {}, h('a', { href: '#' + r.href, class: 'flex gap-2.5 text-sm hover:underline' },
        h('span', { class: 'mt-1.5 size-2 shrink-0 rounded-full ' + dot[r.tone], 'aria-hidden': 'true' }),
        h('span', { class: 'min-w-0' }, h('span', { class: 'block truncate font-medium' }, r.title), h('span', { class: 'muted block truncate text-xs' }, r.detail, r.at ? ' · ' + relTime(r.at) : ''))))))
        : h('p', { class: 'muted text-sm' }, 'No changes yet. A change the assistant or a person proposes shows here once it is decided.'),
      h('a', { href: '#/proposals', class: 'mt-auto text-sm font-medium text-brand-700 underline dark:text-brand-300' }, 'All approvals'));
  }

  // Ask or tell Forgeline: opens the assistant with the question (design note 029). Developers and up.
  function askBar() {
    if (!atLeast(session.role, 'developer')) return null;
    const input = h('input', { id: 'home-ask', class: 'min-w-0 flex-1 bg-transparent py-1 text-[15px] placeholder:text-slate-400 focus:outline-none', maxlength: '4000', autocomplete: 'off',
      placeholder: 'Ask or tell Forgeline… “why did my last run fail?”', 'aria-label': 'Ask or tell Forgeline' });
    return h('form', { class: 'flex items-center gap-3 rounded-2xl border-[1.5px] border-brand-500 bg-white px-4 py-2.5 shadow-sm focus-within:ring-4 focus-within:ring-brand-500/15 dark:bg-slate-900', id: 'home-ask-form',
      onsubmit: (e) => { e.preventDefault(); const q = input.value.trim(); if (!q) return; if (openAssistant(q)) input.value = ''; } },
      icon('sparkle', 'size-5 text-brand-600 dark:text-brand-300'), input,
      h('button', { type: 'submit', class: 'btn-primary btn-sm' }, 'Ask'));
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

  mount(el, askBar(), setupCard(), dyn);
  mount(dyn, spinner());
  await load();
  timer = setInterval(() => { if (!document.hidden) load(); }, 4000);
  el.cleanup = () => clearInterval(timer);
  return el;
}
