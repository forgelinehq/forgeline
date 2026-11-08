// Settings › Connections (design notes 025 and 029, frame 2). What is connected is listed on the left; the
// one picked opens on the right with Overview (status, Test, Connect), Services (AWS: what the AI may do with
// each service), What the AI may do (the tool ceiling and each agent's access) and Activity. Everything not
// connected yet, including what is coming, is behind "Add a connection". Status comes from the server
// (`GET /v1/integrations`); nothing here decides what is connected.
import { h, icon, mount, relTime, absTime, spinner, errorBox, notice, emptyState } from '../lib/dom.js';
import { get, post, atLeast } from '../lib/api.js';
import { session } from '../app.js';
import { navigate } from '../lib/router.js';
import { group, matches, statusPill, statusText, tierText, readOnlyText, logo, isPlanned, activityLabel, connectionGroups, defaultConnection } from '../lib/hub.js';
import { renderChecks } from './integration_parts.js';
import { connectWizard, wizardFor } from './integrations_wizard.js';
import { accessPane } from './integration_access.js';

const ADD = 'add';
const LOGO_TONE = { aws: 'bg-brand-600', github: 'bg-slate-800 dark:bg-slate-600', claude: 'bg-violet-600', docker: 'bg-sky-700', kubernetes: 'bg-blue-600', 'aws-mcp': 'bg-amber-700' };

export async function integrationsHubPage({ params }) {
  const el = h('div', {});
  const admin = atLeast(session.role, 'admin');
  let list = [], status = 'all', category = 'all', query = '';
  const wanted = params && params.id;
  let selected = wanted === ADD ? ADD : null;
  let tab = 'overview';
  mount(el, spinner());

  async function load() {
    try { list = (await get('/v1/integrations')).integrations; } catch (e) { return mount(el, errorBox(e.message)); }
    if (selected !== ADD) selected = defaultConnection(list, wanted) || ADD;
    draw();
  }

  const pick = (id) => navigate('/integrations/' + encodeURIComponent(id));
  const logoBox = (it, size = 'size-8 text-[10px]') => h('span', { class: 'flex shrink-0 items-center justify-center rounded-lg font-bold text-white ' + size + ' ' + (isPlanned(it) || it.status === 'off' ? 'bg-slate-300 text-slate-600 dark:bg-slate-700 dark:text-slate-300' : (LOGO_TONE[it.id] || 'bg-slate-500')), 'aria-hidden': 'true' }, logo(it.name));
  const dot = (it) => h('span', { class: 'size-1.5 rounded-full ' + (it.status === 'connected' ? 'bg-emerald-500' : it.status === 'attention' ? 'bg-amber-500' : 'bg-sky-500') });

  function sideItem(it) {
    const on = selected === it.id;
    return h('a', { href: '#/integrations/' + encodeURIComponent(it.id), 'data-integration': it.id, 'aria-current': on ? 'page' : null,
      class: 'flex items-center gap-2.5 rounded-xl px-2.5 py-2 transition-colors ' + (on ? 'bg-white shadow-sm ring-1 ring-slate-200 dark:bg-slate-800 dark:ring-slate-700' : 'hover:bg-slate-200/50 dark:hover:bg-slate-800/60') },
      logoBox(it), h('span', { class: 'min-w-0' }, h('span', { class: 'block truncate text-sm font-medium' }, it.name),
        h('span', { class: 'flex items-center gap-1.5 text-xs ' + (it.status === 'attention' ? 'text-amber-700 dark:text-amber-300' : 'muted') }, dot(it), it.status === 'attention' ? 'Needs attention' : it.status === 'detected' ? 'Found, not connected' : 'Working')));
  }

  function aside(g) {
    return h('aside', { class: 'space-y-1', 'aria-label': 'Your connections', id: 'connections-list' },
      h('div', { class: 'h-section px-2.5 pb-1' }, 'Connected · ' + g.connected.length),
      g.connected.length ? g.connected.map(sideItem) : h('p', { class: 'muted px-2.5 text-sm' }, 'Nothing yet.'),
      g.found.length ? h('div', { class: 'h-section px-2.5 pb-1 pt-4' }, 'Found on this server') : null,
      g.found.map(sideItem),
      h('div', { class: 'pt-4' }, h('a', { href: '#/integrations/' + ADD, id: 'add-connection', 'aria-current': selected === ADD ? 'page' : null,
        class: (selected === ADD ? 'btn-secondary' : 'btn-primary') + ' w-full' }, icon('plus', 'size-4'), 'Add a connection')),
      h('p', { class: 'muted px-2.5 pt-2 text-xs' }, g.rest.length + ' more you can connect, or that are coming, are in Add a connection.'));
  }

  function draw() {
    const g = connectionGroups(list);
    const it = list.find(x => x.id === selected);
    mount(el,
      h('div', { class: 'mb-5' }, h('h1', { class: 'h-page' }, 'Connections'), h('p', { class: 'muted mt-1 text-sm' }, 'What Forgeline is connected to, and what the AI may do with each.')),
      h('div', { class: 'grid gap-6 lg:grid-cols-[16rem_minmax(0,1fr)]' }, aside(g),
        h('div', { class: 'min-w-0', id: 'connection-detail' }, selected === ADD || !it ? catalog(g) : detail(it))));
  }

  /** Everything not connected yet: pick one to see what it does and connect it. */
  function catalog(g) {
    const box = h('div', { class: 'space-y-4' });
    const chip = (label, on, onclick, extra = {}) => h('button', { type: 'button', 'aria-pressed': String(on), onclick, ...extra,
      class: 'rounded-full border px-3 py-1 text-xs ' + (on ? 'border-slate-900 bg-slate-900 text-white dark:border-slate-100 dark:bg-slate-100 dark:text-slate-900' : 'border-slate-300 text-slate-600 hover:bg-slate-100 dark:border-slate-700 dark:text-slate-300 dark:hover:bg-slate-800') }, label);
    const grid = h('div', { id: 'hub-grid' });
    const cats = ['all', ...new Set(g.rest.map(i => i.category))];
    const drawGrid = () => {
      const shown = g.rest.filter(i => matches(i, { status: status === 'all' ? 'all' : status, category, query }));
      mount(grid, shown.length ? group(shown).map(({ category: c, items }) => h('section', { class: 'mb-6', 'aria-label': c },
        h('h2', { class: 'h-section mb-2' }, c),
        h('div', { class: 'grid gap-3 sm:grid-cols-2 xl:grid-cols-3', role: 'list' }, items.map(card))))
        : emptyState('plug', 'Nothing matches', 'Try another word, or clear the filters.'));
    };
    const drawAll = () => mount(box,
      h('div', {}, h('h2', { class: 'font-display text-xl font-bold' }, 'Add a connection'), h('p', { class: 'muted text-sm' }, 'Pick a system to see what it does, how it signs in and what it lets the AI do. Coming soon ones show what they will do.')),
      h('div', { class: 'flex flex-wrap items-center gap-2' },
        [['all', 'All'], ['off', 'Ready to connect'], ['soon', 'Coming soon']].map(([k, label]) => chip(label, status === k, () => { status = k; drawAll(); }, { 'data-status': k })),
        h('span', { class: 'mx-1 h-5 w-px bg-slate-200 dark:bg-slate-700' }),
        cats.map(c => chip(c === 'all' ? 'Any kind' : c, category === c, () => { category = c; drawAll(); }, { 'data-category': c })),
        h('input', { id: 'hub-search', class: 'input w-56', type: 'search', placeholder: 'Search', 'aria-label': 'Search connections', value: query, oninput: (e) => { query = e.target.value; drawGrid(); } })),
      grid);
    drawAll(); drawGrid();
    return box;
  }

  function card(it) {
    const planned = isPlanned(it);
    return h('a', { href: '#/integrations/' + encodeURIComponent(it.id), role: 'listitem', 'data-integration': it.id,
      class: 'flex min-w-0 flex-col gap-2 rounded-xl border p-3.5 text-left transition hover:border-brand-400 ' + (planned ? 'border-dashed border-slate-300 dark:border-slate-700' : 'border-slate-200 bg-white dark:border-slate-800 dark:bg-slate-900') },
      h('div', { class: 'flex items-center gap-2.5' }, logoBox(it), h('span', { class: 'min-w-0 truncate text-sm font-semibold' }, it.name)),
      h('p', { class: 'muted line-clamp-2 text-xs leading-relaxed' }, it.blurb),
      h('span', { class: statusPill(it.status) + ' self-start' }, h('span', { class: 'size-1.5 rounded-full bg-current' }), statusText(it)));
  }

  /** One connection, opened: a header and its tabs. */
  function detail(it) {
    const planned = isPlanned(it);
    const names = [['overview', 'Overview']];
    if (!planned && it.status !== 'detected' && it.status !== 'off') {
      if (it.id === 'aws' && it.has_policy) names.push(['services', 'Services']);
      if (it.has_policy) names.push(['access', 'What the AI may do']);
      if (admin) names.push(['activity', 'Activity']);
    }
    if (!names.some(([k]) => k === tab)) tab = 'overview';
    const pane = h('div', { class: 'pt-5' });
    const drawPane = () => {
      if (tab === 'activity') return mount(pane, activityPane(it));
      if (tab === 'services') return mount(pane, accessPane(it, admin, () => load0(), 'services'));
      if (tab === 'access') return mount(pane, accessPane(it, admin, () => load0(), 'tools'));
      mount(pane, overview(list.find(x => x.id === it.id) || it, () => load0(), (node) => mount(pane, node)));
    };
    const tabsBar = h('div', { class: 'flex gap-1 overflow-x-auto overflow-y-hidden border-b border-slate-200 dark:border-slate-800', role: 'tablist' });
    const drawTabs = () => mount(tabsBar, names.map(([k, label]) => h('button', { type: 'button', role: 'tab', 'data-tab': k, 'aria-selected': String(tab === k),
      class: '-mb-px whitespace-nowrap border-b-2 px-3 py-2 text-sm font-medium ' + (tab === k ? 'border-brand-600 text-brand-700 dark:border-brand-400 dark:text-brand-200' : 'border-transparent text-slate-500 hover:text-slate-800 dark:text-slate-400'),
      onclick: () => { tab = k; drawTabs(); drawPane(); } }, label)));
    drawTabs(); drawPane();
    return h('section', { class: 'card card-pad', id: 'integration-popup', 'data-integration': it.id },
      h('div', { class: 'mb-4 flex flex-wrap items-start gap-3' }, logoBox(it, 'size-11 text-xs'),
        h('div', { class: 'min-w-0 flex-1' }, h('h2', { class: 'font-display text-2xl font-bold' }, it.name),
          h('p', { class: 'muted mt-0.5 text-sm' }, it.status === 'attention' ? (it.problem || it.summary) : (it.summary || it.blurb))),
        h('span', { class: statusPill(it.status) }, h('span', { class: 'size-1.5 rounded-full bg-current' }), statusText(it))),
      tabsBar, pane);
  }

  function overview(it, close, replacePane) {
    const planned = isPlanned(it);
    const out = h('div', { class: 'space-y-4' });
    const testBox = h('div', { class: 'space-y-1.5', 'aria-live': 'polite', id: 'integration-test' });
    // The status and summary are in the header above; here, what kind of integration it is.
    out.append(h('div', { class: 'flex flex-wrap items-center gap-2' }, h('span', { class: 'pill-idle' }, tierText(it.tier)), planned && it.order ? h('span', { class: 'muted text-xs' }, 'Build order ' + it.order) : null));
    if (it.problem && it.problem !== it.summary) out.append(notice('warn', it.problem));
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
    if (planned) out.append(h('p', { class: 'muted text-xs' }, 'Not available yet, so there is no Connect button. When it ships it opens here, and every tool it adds starts at Needs approval.'));

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
          renderChecks(testBox, res); it.last_test = res;
        } catch (e) { mount(testBox, errorBox(e.message)); }
        btn.disabled = false;
      } }, icon('bolt', 'size-4'), 'Test connection');
      actions.append(btn);
    }
    if (it.manage && !planned && !wizard) actions.append(h('a', { class: 'btn-secondary', href: '#' + it.manage }, it.status === 'off' || it.status === 'detected' ? 'Connect' : 'Open settings'));
    if (wizard) actions.append(h('a', { class: 'btn-ghost btn-sm', href: '#' + it.manage }, 'Or use the full page'));
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

  /** Refresh after something changed (a connection made in the wizard, a level changed). */
  async function load0() { try { list = (await get('/v1/integrations')).integrations; draw(); } catch { /* the pane still shows its own result */ } }

  await load();
  return el;
}
