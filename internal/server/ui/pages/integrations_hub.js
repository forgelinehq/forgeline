// The integrations hub (design note 025): every system Forgeline connects to, in one place. Click a card
// for its popup: the status and what to do, who enforces read-only, and a Test button that only reads.
// Status comes from the server (`GET /v1/integrations`); nothing here decides what is connected.
import { h, icon, mount, pageHeader, relTime, absTime, spinner, errorBox, notice, modal, emptyState } from '../lib/dom.js';
import { get, post, atLeast } from '../lib/api.js';
import { session } from '../app.js';
import { navigate } from '../lib/router.js';
import { group, matches, summaryLine, statusPill, statusText, tierText, readOnlyText, logo, attentionList, isPlanned, activityLabel } from '../lib/hub.js';
import { renderChecks } from './integration_parts.js';
import { connectWizard, wizardFor } from './integrations_wizard.js';

const STATUS_CHIPS = [['all', 'All'], ['connected', 'Connected'], ['attention', 'Needs attention'], ['detected', 'Found here'], ['soon', 'Coming soon']];

export async function integrationsHubPage({ params }) {
  const el = h('div', {});
  const admin = atLeast(session.role, 'admin');
  let list = [], status = 'all', category = 'all', query = '', opened = null;
  mount(el, pageHeader('Integrations', 'Everything Forgeline connects to, what is connected, and what is coming.'), spinner());

  async function load() {
    try { list = (await get('/v1/integrations')).integrations; } catch (e) { return mount(el, pageHeader('Integrations', ''), errorBox(e.message)); }
    draw();
    if (params && params.id && opened !== params.id) { opened = params.id; const it = list.find(i => i.id === params.id); if (it) open(it); }
  }

  function chip(label, on, onclick, extra = {}) {
    return h('button', { type: 'button', 'aria-pressed': String(on), class: 'rounded-full border px-3 py-1 text-xs ' + (on ? 'border-slate-900 bg-slate-900 text-white dark:border-slate-100 dark:bg-slate-100 dark:text-slate-900' : 'border-slate-300 text-slate-600 hover:bg-slate-100 dark:border-slate-700 dark:text-slate-300 dark:hover:bg-slate-800'), onclick, ...extra }, label);
  }

  /** What the card says: what to fix when something is wrong, the live state when connected, otherwise what the system is for. */
  const cardLine = (it) => it.status === 'attention' ? (it.problem || it.summary) : it.status === 'connected' ? it.summary : it.blurb;

  function card(it) {
    const planned = isPlanned(it), t = it.last_test;
    return h('button', { type: 'button', role: 'listitem', 'data-integration': it.id, onclick: () => open(it),
      class: 'flex min-w-0 flex-col gap-2 rounded-xl border p-3.5 text-left transition hover:border-brand-400 ' + (planned ? 'border-dashed border-slate-300 dark:border-slate-700' : 'border-slate-200 bg-white dark:border-slate-800 dark:bg-slate-900') },
      h('div', { class: 'flex items-center gap-2.5' },
        h('span', { class: 'flex size-8 shrink-0 items-center justify-center rounded-lg bg-slate-100 text-xs font-bold dark:bg-slate-800 ' + (planned ? 'text-slate-400' : '') }, logo(it.name)),
        h('span', { class: 'min-w-0 truncate text-sm font-semibold' }, it.name)),
      h('p', { class: 'muted line-clamp-2 text-xs leading-relaxed' }, cardLine(it)),
      h('div', { class: 'mt-auto flex flex-wrap items-center justify-between gap-1.5' },
        h('span', { class: statusPill(it.status) }, h('span', { class: 'size-1.5 rounded-full bg-current' }), statusText(it)),
        t ? h('span', { class: 'muted text-[11px]', title: absTime(t.at) }, (t.failed ? 'Test failed ' : t.warned ? 'Tested, with warnings ' : 'Tested ') + relTime(t.at)) : h('span', { class: 'muted text-[11px]' }, tierText(it.tier))));
  }

  function draw() {
    const shown = list.filter(i => matches(i, { status, category, query }));
    const attn = attentionList(list);
    const cats = ['all', ...new Set(list.map(i => i.category))];
    mount(el,
      pageHeader('Integrations', 'Everything Forgeline connects to, what is connected, and what is coming.'),
      h('p', { class: 'muted -mt-3 mb-4 text-sm', id: 'hub-summary' }, summaryLine(list)),
      attn.length ? h('div', { class: 'mb-4', id: 'hub-attention' }, notice('warn', h('strong', {}, attn.length === 1 ? 'One integration needs attention. ' : attn.length + ' integrations need attention. '),
        ...attn.flatMap((i, n) => [n ? ', ' : '', h('button', { type: 'button', class: 'underline', onclick: () => open(i) }, i.name)]))) : null,
      h('div', { class: 'mb-5 flex flex-wrap items-center gap-2' },
        STATUS_CHIPS.map(([k, label]) => chip(label, status === k, () => { status = k; draw(); }, { 'data-status': k })),
        h('span', { class: 'mx-1 h-5 w-px bg-slate-200 dark:bg-slate-700' }),
        cats.map(c => chip(c === 'all' ? 'Any category' : c, category === c, () => { category = c; draw(); }, { 'data-category': c })),
        h('input', { id: 'hub-search', class: 'input w-56', type: 'search', placeholder: 'Search integrations', 'aria-label': 'Search integrations', value: query,
          oninput: (e) => { query = e.target.value; drawGrid(); } })),
      h('div', { id: 'hub-grid' }));
    drawGrid();
  }

  function drawGrid() {
    const box = el.querySelector('#hub-grid');
    const g = group(list.filter(i => matches(i, { status, category, query })));
    mount(box, g.length ? g.map(({ category: c, items }) => h('section', { class: 'mb-6', 'aria-label': c },
      h('h2', { class: 'h-section mb-2' }, c),
      h('div', { class: 'grid gap-3 sm:grid-cols-2 xl:grid-cols-3 min-[1900px]:grid-cols-4', role: 'list' }, items.map(card))))
      : emptyState('plug', 'Nothing matches', 'Try another word, or clear the filters.'));
  }

  /** The popup for one system. */
  function open(it) {
    modal(it.name, (close) => body(it, close), { wide: true }).then(() => { if (params && params.id) navigate('/integrations'); });
  }

  /** The popup: Overview, and for administrators Activity. Planned and detected systems have no tabs. */
  function body(it, close) {
    const planned = isPlanned(it);
    const wrap = h('div', { class: 'space-y-4', id: 'integration-popup', 'data-integration': it.id });
    const pane = h('div', { class: 'space-y-4' });
    let tab = 'overview';
    const showTabs = admin && !planned && it.status !== 'detected';
    const draw = () => {
      if (tab === 'activity') return mount(pane, activityPane(it));
      mount(pane, overview(it, close, (el) => mount(pane, el)));
    };
    const tabs = showTabs ? h('div', { class: 'flex gap-1 border-b border-slate-200 dark:border-slate-800', role: 'tablist' }, ['overview', 'activity'].map(t => h('button', { type: 'button', role: 'tab', 'data-tab': t, 'aria-selected': String(tab === t),
      class: '-mb-px border-b-2 px-3 py-2 text-sm font-medium ' + (tab === t ? 'border-brand-600 text-brand-700 dark:border-brand-400 dark:text-brand-200' : 'border-transparent text-slate-500 hover:text-slate-800 dark:text-slate-400'),
      onclick: (e) => { tab = t; tabs.querySelectorAll('button').forEach(b2 => { const on = b2.dataset.tab === t; b2.setAttribute('aria-selected', String(on)); b2.className = '-mb-px border-b-2 px-3 py-2 text-sm font-medium ' + (on ? 'border-brand-600 text-brand-700 dark:border-brand-400 dark:text-brand-200' : 'border-transparent text-slate-500 hover:text-slate-800 dark:text-slate-400'); }); draw(); } },
      t === 'overview' ? 'Overview' : 'Activity'))) : null;
    wrap.append(...[tabs, pane].filter(Boolean));
    draw();
    wrap.append(h('div', { class: 'flex justify-end pt-1' }, h('button', { type: 'button', class: 'btn-ghost btn-sm', onclick: () => close() }, 'Close')));
    return wrap;
  }

  function overview(it, close, replacePane) {
    const planned = isPlanned(it);
    const out = h('div', { class: 'space-y-4' });
    const testBox = h('div', { class: 'space-y-1.5', 'aria-live': 'polite', id: 'integration-test' });
    out.append(h('div', { class: 'flex flex-wrap items-center gap-2' }, h('span', { class: statusPill(it.status) }, h('span', { class: 'size-1.5 rounded-full bg-current' }), statusText(it)),
      h('span', { class: 'pill-idle' }, tierText(it.tier)), planned && it.order ? h('span', { class: 'muted text-xs' }, 'Build order ' + it.order) : null));
    if (it.summary && !planned) out.append(h('p', { class: 'text-sm' }, it.summary));
    if (it.problem) out.append(notice('warn', it.problem));
    (it.warnings || []).forEach(w => out.append(notice('info', w)));
    if (it.found) out.append(h('div', { class: 'rounded-lg border border-dashed border-brand-400 bg-brand-50 p-3 text-sm dark:bg-brand-950/30' }, h('div', { class: 'mb-1 font-medium' }, 'Found on this server'), it.found,
      h('div', { class: 'muted mt-2 text-xs' }, 'Detection lists names only. It never reads or copies a credential, and an administrator decides what to do with each one.')));
    if (planned) {
      out.append(h('div', {}, h('div', { class: 'h-section mb-1' }, 'What it will do'), h('p', { class: 'text-sm' }, it.plan)),
        h('div', {}, h('div', { class: 'h-section mb-1' }, 'How it will sign in'), h('p', { class: 'text-sm' }, it.auth)));
    }
    if (it.details && it.details.length) out.append(h('dl', { class: 'grid grid-cols-[auto_1fr] gap-x-6 gap-y-1.5 text-sm' }, it.details.map(d => [h('dt', { class: 'muted' }, d.label), h('dd', { class: 'min-w-0 break-words' }, d.value)])));
    const ro = readOnlyText(it);
    if (ro) out.append(h('div', {}, h('div', { class: 'h-section mb-1' }, 'Read-only'), h('p', { class: 'text-sm' }, ro)));
    if (planned) out.append(h('p', { class: 'muted text-xs' }, 'Not available yet, so there is no Connect button. When it ships it opens this same popup, and every tool it adds starts at Needs approval.'));

    const actions = h('div', { class: 'flex flex-wrap items-center gap-2' });
    const wizard = admin && it.status === 'off' && wizardFor(it.id);
    if (wizard) {
      actions.append(h('button', { type: 'button', class: 'btn-primary', id: 'integration-connect', onclick: () => replacePane(connectWizard(it, { close, onDone: () => { load0(); } })) }, icon('plug', 'size-4'), 'Connect'));
    }
    if (it.testable && admin && !wizard) {
      const btn = h('button', { type: 'button', class: 'btn-primary', id: 'integration-test-run', onclick: async () => {
        btn.disabled = true; mount(testBox, spinner());
        try {
          const res = await post('/v1/integrations/' + encodeURIComponent(it.id) + '/checks');
          renderChecks(testBox, res); it.last_test = res; drawGrid(); // the card behind the popup shows "Tested just now" at once
        } catch (e) { mount(testBox, errorBox(e.message)); }
        btn.disabled = false;
      } }, icon('bolt', 'size-4'), 'Test connection');
      actions.append(btn);
    }
    if (it.manage && !planned && !wizard) actions.append(h('a', { class: 'btn-secondary', href: '#' + it.manage, onclick: () => close() }, it.status === 'off' || it.status === 'detected' ? 'Connect' : 'Open settings'));
    if (wizard) actions.append(h('a', { class: 'btn-ghost btn-sm', href: '#' + it.manage, onclick: () => close() }, 'Or use the full page'));
    if (actions.children.length) out.append(actions);
    if (it.testable && !admin) out.append(h('p', { class: 'muted text-xs' }, 'Only administrators can run a test.'));
    out.append(testBox);
    if (it.last_test) renderChecks(testBox, it.last_test, true);
    return out;
  }

  /** What was done to this integration and by whom, from the audit log (administrators only). Refusals are marked. */
  function activityPane(it) {
    const box = h('div', { class: 'space-y-1.5', id: 'integration-activity' }, spinner());
    get('/v1/integrations/' + encodeURIComponent(it.id) + '/activity').then(({ events }) => {
      mount(box, events.length ? h('ul', { class: 'divide-y divide-slate-100 rounded-lg border border-slate-200 dark:divide-slate-800 dark:border-slate-800' }, events.map(e => h('li', { class: 'flex items-start gap-3 px-3 py-2 text-sm', 'data-allowed': String(e.allowed) },
        h('div', { class: 'min-w-0 flex-1' }, h('div', { class: 'font-medium' }, activityLabel(e.action), e.allowed ? null : h('span', { class: 'pill-bad ml-2' }, 'Refused')),
          e.detail ? h('div', { class: 'muted break-words text-xs' }, e.detail) : null),
        h('div', { class: 'muted shrink-0 text-right text-xs' }, e.actor, h('div', { title: absTime(e.time) }, relTime(e.time))))))
        : h('p', { class: 'muted text-sm' }, 'Nothing has been recorded for this integration yet. Connecting, changing and testing it all appear here.'));
    }).catch(e => mount(box, errorBox(e.message)));
    return box;
  }

  /** Refresh the cards after something changed behind the popup (a connection made in the wizard). */
  async function load0() { try { list = (await get('/v1/integrations')).integrations; draw(); } catch { /* the wizard still shows its own result */ } }

  await load();
  return el;
}
