// Forgeline web console. See docs/design/006-console.md.
import { h, icon, mount, toast, spinner, errorBox } from './lib/dom.js';
import { get, post, atLeast, onSignedOut } from './lib/api.js';
import { route, start, stop, currentPath } from './lib/router.js';
import { loginPage, setupPage } from './pages/auth.js';
import { overviewPage } from './pages/overview.js';
import { runPage } from './pages/run.js';
import { pipelinesPage } from './pages/pipelines.js';
import { pipelineEditorPage } from './pages/editor.js';
import { actionsPage } from './pages/actions.js';
import { proposalsPage } from './pages/proposals.js';
import { agentsPage, agentPage, skillPage } from './pages/agents.js';
import { workflowPage, workflowRunPage } from './pages/workflows.js';
import { guardPage } from './pages/guard.js';
import { kubernetesPage } from './pages/kubernetes.js';
import { environmentsPage } from './pages/environments.js';
import { marketplacePage } from './pages/marketplace.js';
import { infraPage } from './pages/infra.js';
import { githubSettingsPage, claudeSettingsPage } from './pages/integrations.js';
import { integrationsHubPage } from './pages/integrations_hub.js';
import { secretsPage } from './pages/secrets.js';
import { peoplePage } from './pages/people.js';
import { auditPage } from './pages/audit.js';
import { accountPage } from './pages/account.js';
import { memoryPage } from './pages/memory.js';
import { assistantButton } from './lib/assistant.js';
import { visibleGroups, isSettingsPath, activeItem, canOpenSettings } from './lib/settingsnav.js';
import { settingsPage } from './pages/settings.js';
import { ssoPage } from './pages/sso.js';
import { approvalRulesPage } from './pages/approval_rules.js';
import { teamPage } from './pages/team.js';
import { mcpServersPage } from './pages/mcp_servers.js';
import { watchMapPage } from './pages/watch_map.js';
import { servicesPage, servicePage } from './pages/services.js';
import { aiUsePage } from './pages/ai_use.js';
import { missionsPage, missionPage } from './pages/missions.js';
import { builtPage } from './pages/built.js';
import { setupPage as setupStepsPage } from './pages/setup.js';
import { blueprintsPage, blueprintPage } from './pages/blueprints.js';
import { designHomePage, designPage } from './pages/design.js';
import { spacesFor, activeSpace, tabsFor, tabMatches } from './lib/nav.js';

const root = document.getElementById('root');
export const session = { role: 'viewer', name: '', user: null };
let approvalsTimer = 0; // one poll of the approvals count, kept across sign-ins
let approvalsOnNav = null; // the hashchange listener that refreshes it, so it is added once

function setTheme(dark) {
  document.documentElement.classList.toggle('dark', dark);
  try { localStorage.setItem('forgeline-theme', dark ? 'dark' : 'light'); } catch { /* storage blocked */ }
}


function shell() {
  const outlet = h('main', { id: 'main', tabindex: '-1', class: 'mx-auto w-full max-w-7xl flex-1 px-4 py-6 outline-none sm:px-6 lg:px-8 min-[1800px]:max-w-[96rem] min-[2300px]:max-w-[120rem]' });
  const nav = h('nav', { class: 'flex flex-col gap-0.5 px-3', 'aria-label': 'Main' });
  const gearSlot = h('div', { class: 'mt-auto' });
  const tabBar = h('div', { id: 'space-tabs' });
  const spaceLink = (s, on) =>
    h('a', {
      href: '#' + s.path, onclick: () => sidebar.classList.add('hidden'), 'data-space': s.id,
      class: 'flex items-center gap-3 rounded-xl px-3 py-2.5 text-[15px] font-medium transition-colors ' + (on
        ? 'bg-white text-brand-700 shadow-sm ring-1 ring-slate-200 dark:bg-brand-500/15 dark:text-brand-200 dark:ring-0'
        : 'text-slate-600 hover:bg-slate-200/60 hover:text-slate-900 dark:text-slate-300 dark:hover:bg-slate-800'),
      'aria-current': on ? 'page' : null,
    }, icon(s.icon, 'size-[18px]'), s.label);
  // The tabs of the space the page belongs to, above the page. Settings has its own menu, so none there.
  const renderTabs = () => {
    const path = currentPath();
    const sp = activeSpace(path);
    const tabs = sp && !isSettingsPath(path) ? tabsFor(sp, session.role) : [];
    if (tabs.length < 2) return mount(tabBar);
    mount(tabBar, h('div', { class: 'border-b border-slate-200 bg-white/60 dark:border-slate-800 dark:bg-slate-900/40' },
      h('nav', { class: 'mx-auto flex w-full max-w-7xl items-center gap-1 overflow-x-auto px-4 sm:px-6 lg:px-8 min-[1800px]:max-w-[96rem]', 'aria-label': sp.label },
        h('span', { class: 'mr-3 hidden font-display text-base font-bold sm:block' }, sp.label),
        tabs.map(t => t.planned
          ? h('span', { class: 'flex items-center gap-1.5 whitespace-nowrap px-3 py-3 text-sm text-slate-400 dark:text-slate-600', title: 'Coming soon' }, t.label, h('span', { class: 'rounded bg-slate-100 px-1 font-mono text-[9px] uppercase tracking-wider dark:bg-slate-800' }, 'soon'))
          : h('a', { href: '#' + t.path, 'data-tab': t.path, 'aria-current': tabMatches(t, path) ? 'page' : null,
              class: 'whitespace-nowrap border-b-2 px-3 py-3 text-sm font-medium transition-colors ' + (tabMatches(t, path)
                ? 'border-brand-600 text-brand-700 dark:border-brand-400 dark:text-brand-200'
                : 'border-transparent text-slate-600 hover:text-slate-900 dark:text-slate-400 dark:hover:text-white') }, t.label,
              t.path === '/proposals' ? approvalsBadge(true) : null)))));
  };
  const renderNav = () => {
    const sp = activeSpace(currentPath());
    mount(nav, spacesFor(session.role).map(s => spaceLink(s, !isSettingsPath(currentPath()) && sp && sp.id === s.id)));
    renderTabs();
    mount(gearSlot, canOpenSettings(session.role) ? h('a', { href: '#/settings', 'aria-current': isSettingsPath(currentPath()) ? 'page' : null,
      class: 'mx-3 mb-2 flex items-center gap-3 rounded-lg px-3 py-2 text-sm font-medium transition-colors ' + (isSettingsPath(currentPath())
        ? 'bg-brand-50 text-brand-700 dark:bg-brand-500/15 dark:text-brand-200'
        : 'text-slate-600 hover:bg-slate-100 dark:text-slate-300 dark:hover:bg-slate-800') }, icon('gear', 'size-[18px]'), 'Settings') : null);
  };

  const sidebar = h('aside', { class: 'fixed inset-y-0 left-0 z-40 hidden w-64 flex-col border-r border-slate-200 bg-slate-100/80 pt-4 backdrop-blur lg:flex dark:border-slate-800 dark:bg-slate-900' },
    h('a', { href: '#/', class: 'mb-5 flex items-center gap-2.5 px-6' },
      h('img', { src: '/ui/favicon.svg', alt: '', class: 'size-8' }), h('span', { class: 'font-display text-xl font-bold tracking-tight' }, 'Forgeline')),
    nav, gearSlot,
    h('div', { class: 'border-t border-slate-200 p-4 text-xs text-slate-500 dark:border-slate-800 dark:text-slate-400' }, h('div', { id: 'build-info' }, 'Delivery where AI agents run safely')));

  const dark = () => document.documentElement.classList.contains('dark');
  const themeBtn = h('button', { class: 'btn-ghost btn-sm', type: 'button', 'aria-label': 'Toggle dark mode', onclick: () => { setTheme(!dark()); themeBtn.replaceChildren(icon(dark() ? 'sun' : 'moon')); } }, icon(dark() ? 'sun' : 'moon'));

  const userMenu = h('div', { class: 'relative' });
  const renderUser = () => {
    const open = { v: false };
    const menu = h('div', { class: 'absolute right-0 top-full z-30 mt-2 hidden w-56 rounded-xl border border-slate-200 bg-white p-1.5 shadow-xl dark:border-slate-700 dark:bg-slate-900' },
      h('div', { class: 'px-3 py-2' }, h('div', { class: 'truncate text-sm font-medium' }, session.user?.name || session.name), h('div', { class: 'muted truncate text-xs' }, session.name + ' · ' + session.role)),
      h('hr', { class: 'my-1 border-slate-200 dark:border-slate-800' }),
      session.user && h('a', { href: '#/account', class: 'flex items-center gap-2 rounded-lg px-3 py-2 text-sm hover:bg-slate-100 dark:hover:bg-slate-800', onclick: () => menu.classList.add('hidden') }, icon('lock'), 'Account & password'),
      h('button', { type: 'button', class: 'flex w-full items-center gap-2 rounded-lg px-3 py-2 text-sm hover:bg-slate-100 dark:hover:bg-slate-800', onclick: signOut }, icon('logout'), 'Sign out'));
    const btn = h('button', { type: 'button', class: 'flex items-center gap-2 rounded-lg px-2 py-1.5 text-sm hover:bg-slate-100 dark:hover:bg-slate-800', 'aria-haspopup': 'menu', onclick: (e) => { e.stopPropagation(); menu.classList.toggle('hidden'); } },
      h('span', { class: 'flex size-7 items-center justify-center rounded-full bg-brand-600 text-xs font-semibold text-white' }, (session.name || '?').slice(0, 1).toUpperCase()),
      h('span', { class: 'hidden max-w-32 truncate sm:block' }, session.name), icon('chevron', 'size-3.5 rotate-90'));
    document.addEventListener('click', () => menu.classList.add('hidden'));
    mount(userMenu, btn, menu);
  };

  // Approvals waiting, in the header on every page (design note 029). Counts what is waiting; whether this
  // person may approve it is the server's decision, shown on the Approvals page.
  let waiting = 0;
  const approvalsBadge = (small) => waiting ? h('span', { class: (small ? 'ml-1.5 ' : '') + 'rounded-full bg-brand-600 px-1.5 text-[11px] font-semibold leading-5 text-white' }, String(waiting)) : null;
  const approvalsButton = h('a', { href: '#/proposals', class: 'btn-ghost btn-sm', id: 'approvals-button', title: 'Changes waiting for approval' });
  const renderApprovals = () => { mount(approvalsButton, icon('check', 'size-4'), h('span', { class: 'hidden sm:inline' }, 'Approvals'), approvalsBadge(false)); approvalsButton.setAttribute('aria-label', 'Approvals, ' + waiting + ' waiting'); };
  const loadApprovals = async () => {
    const [a, c] = await Promise.all([get('/v1/ai/proposals').catch(() => null), get('/v1/cloud/changes').catch(() => null)]);
    const n = [...(a?.proposals || []), ...(c?.changes || [])].filter(x => x.status === 'proposed').length;
    if (n !== waiting) { waiting = n; renderApprovals(); renderTabs(); }
  };
  renderApprovals(); loadApprovals();
  clearInterval(approvalsTimer);
  approvalsTimer = setInterval(() => { if (!document.hidden) loadApprovals(); }, 60000);
  if (approvalsOnNav) removeEventListener('hashchange', approvalsOnNav); // once, however many times someone signs in
  approvalsOnNav = loadApprovals;
  addEventListener('hashchange', approvalsOnNav);

  const top = h('header', { class: 'sticky top-0 z-30 flex h-14 items-center gap-3 border-b border-slate-200 bg-white/85 px-4 backdrop-blur lg:pl-72 dark:border-slate-800 dark:bg-slate-900/85' },
    h('button', { class: 'btn-ghost btn-sm lg:hidden', type: 'button', 'aria-label': 'Open menu', onclick: () => { sidebar.classList.toggle('hidden'); sidebar.classList.toggle('flex'); } }, icon('menu', 'size-5')),
    h('div', { class: 'flex-1' }), approvalsButton, assistantButton(session.role), themeBtn, userMenu);

  renderUser(); renderNav();
  addEventListener('hashchange', renderNav);
  mount(root, sidebar, top, h('div', { class: 'flex min-h-[calc(100vh-3.5rem)] flex-col lg:pl-64 lg:pr-[var(--assistant-w,0px)]' }, tabBar, outlet));
  fetch('/healthz').catch(() => {});
  return outlet;
}

async function signOut() {
  stop(); // end this page's polling first, or it would hit the server with a dead session
  try { await post('/v1/auth/logout'); } catch { /* already signed out */ }
  boot();
}

function guard(min, page) {
  return (ctx) => atLeast(session.role, min) ? page(ctx) : forbidden();
}

/** One entry of the Settings menu: a link, or a dimmed label when it is planned. */
function settingsMenuItem(i, here) {
  if (i.planned) {
    return h('li', {}, h('span', { class: 'muted flex items-center justify-between rounded-md px-2 py-1 opacity-70', title: i.note },
      i.label, h('span', { class: 'text-[10px] uppercase tracking-wide' }, 'soon')));
  }
  const on = here && here.path === i.path;
  const cls = on ? 'bg-brand-50 font-medium text-brand-700 dark:bg-brand-500/15 dark:text-brand-200' : 'text-slate-600 hover:bg-slate-100 dark:text-slate-300 dark:hover:bg-slate-800';
  return h('li', {}, h('a', { href: '#' + i.path, 'aria-current': on ? 'page' : null, class: 'block rounded-md px-2 py-1 ' + cls }, i.label));
}

/** Setup pages show the Settings menu on the left, so the way to the others is one click away. */
function inSettings(page) {
  return async (ctx) => {
    const el = await page(ctx);
    if (!(el instanceof Node)) return el;
    const here = activeItem(currentPath());
    const groups = visibleGroups(session.role).map(g =>
      h('div', {}, h('div', { class: 'h-section mb-1' }, g.title), h('ul', { class: 'space-y-0.5' }, g.items.map(i => settingsMenuItem(i, here)))));
    const menu = h('nav', { 'aria-label': 'Settings', class: 'mb-6 space-y-4 text-sm lg:sticky lg:top-20 lg:mb-0 lg:self-start' },
      h('a', { href: '#/settings', class: 'flex items-center gap-2 font-semibold hover:underline' }, icon('gear', 'size-4'), 'Settings'), groups);
    const wrap = h('div', { class: 'lg:grid lg:grid-cols-[13rem_minmax(0,1fr)] lg:gap-10' }, menu, h('div', { class: 'min-w-0' }, el));
    wrap.cleanup = el.cleanup;
    return wrap;
  };
}

function forbidden() {
  return h('div', { class: 'card card-pad mx-auto mt-10 max-w-md text-center' }, icon('lock', 'mx-auto size-8 text-slate-400'),
    h('h1', { class: 'mt-3 text-lg font-semibold' }, 'You do not have access to this page'),
    h('p', { class: 'muted mt-1 text-sm' }, 'Your role is ' + session.role + '. Ask an administrator if you need more.'),
    h('a', { href: '#/', class: 'btn-secondary mt-4' }, 'Back to the overview'));
}

function notFound(err) {
  return h('div', { class: 'card card-pad mx-auto mt-10 max-w-md text-center' }, icon('alert', 'mx-auto size-8 text-slate-400'),
    h('h1', { class: 'mt-3 text-lg font-semibold' }, err ? 'Something went wrong' : 'Page not found'),
    err && h('div', { class: 'mt-3 text-left' }, errorBox(err.message || String(err))),
    h('a', { href: '#/', class: 'btn-secondary mt-4' }, 'Back to the overview'));
}

let routesRegistered = false;
let signedOutHandled = false;

async function boot() {
  stop();
  // Signed out (or about to find out): nothing of the last session keeps polling the server.
  clearInterval(approvalsTimer);
  approvalsTimer = 0;
  if (approvalsOnNav) removeEventListener('hashchange', approvalsOnNav);
  approvalsOnNav = null;
  mount(root, spinner());
  let state;
  try { state = await get('/v1/auth/state'); } catch (e) {
    mount(root, h('div', { class: 'mx-auto mt-24 max-w-md p-4' }, errorBox(e.message))); return;
  }
  if (state.setup_needed) { setupPage(root, boot); return; }
  if (!state.authenticated) { loginPage(root, boot, state.sso); return; }
  session.role = state.role; session.name = state.actor; session.user = state.user;
  signedOutHandled = false;

  const outlet = shell();
  if (!routesRegistered) {
    routesRegistered = true;
    route('/', guard('viewer', overviewPage));
    route('/pipelines', guard('viewer', pipelinesPage));
    route('/pipelines/new', guard('developer', (c) => pipelineEditorPage({ ...c, params: { id: null } })));
    route('/pipelines/:id', guard('viewer', pipelineEditorPage));
    route('/runs/:id', guard('viewer', runPage));
    route('/actions', guard('developer', actionsPage));
    route('/environments', guard('developer', environmentsPage));
    route('/design', guard('developer', designHomePage));
    route('/design/:id', guard('developer', designPage));
    route('/blueprints', guard('developer', blueprintsPage));
    route('/blueprints/:id', guard('developer', blueprintPage));
    route('/marketplace', guard('developer', marketplacePage));
    route('/kubernetes', guard('viewer', kubernetesPage));
    route('/kubernetes/:id', guard('viewer', kubernetesPage));
    route('/guard', guard('viewer', guardPage));
    route('/team', guard('developer', teamPage));
    route('/agents', guard('developer', agentsPage));
    route('/agents/:id', guard('developer', agentPage));
    route('/skills/:id', guard('developer', skillPage));
    route('/workflows/:id', guard('developer', workflowPage));
    route('/workflow-runs/:id', guard('developer', workflowRunPage));
    route('/proposals', guard('viewer', proposalsPage));
    route('/proposals/:id', guard('viewer', proposalsPage));
    route('/map', guard('viewer', watchMapPage));
    route('/services', guard('viewer', servicesPage));
    route('/services/:id', guard('viewer', servicePage));
    route('/ai-use', guard('developer', aiUsePage));
    route('/missions', guard('developer', missionsPage));
    route('/built', guard('developer', builtPage));
    route('/setup', guard('developer', setupStepsPage));
    route('/missions/:id', guard('developer', missionPage));
    route('/infrastructure', guard('viewer', infraPage));
    route('/settings', guard('developer', settingsPage));
    route('/integrations', guard('developer', inSettings(integrationsHubPage)));
    route('/mcp-servers', guard('admin', inSettings(mcpServersPage)));
    route('/integrations/github/settings', guard('developer', inSettings(githubSettingsPage)));
    route('/integrations/claude/settings', guard('developer', inSettings(claudeSettingsPage)));
    route('/integrations/:id', guard('developer', inSettings(integrationsHubPage))); // opens that integration's popup over the hub
    route('/secrets', guard('developer', inSettings(secretsPage)));
    route('/memory', guard('developer', inSettings(memoryPage)));
    route('/people', guard('admin', inSettings(peoplePage)));
    route('/sso', guard('admin', inSettings(ssoPage)));
    route('/approval-rules', guard('developer', inSettings(approvalRulesPage)));
    route('/audit', guard('admin', inSettings(auditPage)));
    route('/account', guard('viewer', accountPage));
  }
  start(outlet, notFound);
}

onSignedOut(() => {
  if (signedOutHandled) return;
  signedOutHandled = true;
  toast('Your session ended. Please sign in again.', 'error');
  boot();
});

boot();
