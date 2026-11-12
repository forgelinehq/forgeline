// Watch › Services: one tile per AWS service, each read on its own, so the page draws at once and fills in as
// each answer comes; a page per service lists only that service. Read-only, like the map.
import { h, icon, mount, pageHeader, spinner, errorBox, emptyState } from '../lib/dom.js';
import { get, atLeast } from '../lib/api.js';
import { pageSignal } from '../lib/router.js';
import { session } from '../app.js';
import { explorer } from './aws_explorer.js';

const ACCOUNT_KEY = 'forgeline-aws-account'; // shared with Watch › Map and Infrastructure › AWS
const CATEGORIES = ['Compute', 'Containers', 'Storage', 'Databases', 'Network', 'Messaging', 'AI', 'Management'];
const DOT = { Compute: 'bg-sky-500', Containers: 'bg-violet-500', Storage: 'bg-emerald-500', Databases: 'bg-amber-500', Network: 'bg-cyan-500', Messaging: 'bg-pink-500', AI: 'bg-fuchsia-500', Management: 'bg-slate-500' };
const AT_ONCE = 4; // reads in flight at a time: each is one AWS call on the server

function savedAccount() {
  try { return sessionStorage.getItem(ACCOUNT_KEY) || 'primary'; } catch { return 'primary'; }
}
function saveAccount(id) {
  try { sessionStorage.setItem(ACCOUNT_KEY, id); } catch { /* a blocked store only forgets the choice */ }
}

/** Loads the accounts and decides which one to show. Returns null (after drawing) when none is connected. */
async function pickAccount(el, title, subtitle) {
  let account = savedAccount();
  let accounts = [], v;
  try {
    // The services list answers at once and says whether AWS is connected; the account summary would make the page
    // wait for a full read of stacks and instances it does not show.
    [accounts, v] = await Promise.all([get('/v1/aws/accounts').then(r => r.accounts).catch(() => []), get('/v1/infra/aws/services?account=primary', { signal: pageSignal() })]);
  } catch (e) { mount(el, pageHeader(title, ''), errorBox(e.message)); return null; }
  if (account !== 'primary' && !accounts.some(a => a.id === account)) account = 'primary';
  if (!v.enabled && account === 'primary') account = accounts.find(a => !a.primary)?.id || 'primary';
  if (!v.enabled && accounts.length <= 1) {
    mount(el, pageHeader(title, subtitle), emptyState('cloud', 'No cloud account connected', 'Connect an AWS account and its services appear here.',
      atLeast(session.role, 'admin') && h('div', { class: 'mt-4 flex justify-center' }, h('a', { class: 'btn-primary', href: '#/integrations/aws' }, 'Connect AWS'))));
    return null;
  }
  return { account, accounts };
}

function accountPicker(accounts, account, onPick) {
  if (accounts.length <= 1) return null;
  return h('div', { class: 'inline-flex overflow-hidden rounded-lg border border-slate-300 text-sm dark:border-slate-700', role: 'group', 'aria-label': 'AWS account', id: 'services-accounts' },
    accounts.map(a => h('button', { type: 'button', 'data-account': a.id, 'aria-pressed': String(a.id === account),
      class: 'px-3 py-1.5 ' + (a.id === account ? 'bg-brand-600 text-white' : 'text-slate-600 hover:bg-slate-100 dark:text-slate-300 dark:hover:bg-slate-800'),
      onclick: () => onPick(a.id) }, a.name)));
}

function stateSummary(svc) {
  const counts = {};
  for (const r of svc.resources) if (r.state) counts[r.state] = (counts[r.state] || 0) + 1;
  return Object.entries(counts).sort((a, b) => b[1] - a[1]).slice(0, 2).map(([s, n]) => `${n} ${s.toLowerCase()}`).join(' · ');
}

export async function servicesPage() {
  const el = h('div', {});
  mount(el, pageHeader('Services', 'Each AWS service on its own, read one at a time. Open one to see everything in it.'), spinner());
  const picked = await pickAccount(el, 'Services', 'Each AWS service on its own.');
  if (!picked) return el;
  let { account } = picked;
  const { accounts } = picked;
  const body = h('div', {});
  let loads = 0;

  async function draw(refresh) {
    const n = ++loads;
    const signal = pageSignal();
    mount(el, pageHeader('Services', 'Each AWS service on its own, read one at a time. Open one to see everything in it.',
      accountPicker(accounts, account, (id) => { account = id; saveAccount(id); draw(false); }),
      h('button', { class: 'btn-secondary btn-sm', type: 'button', id: 'services-refresh', onclick: () => draw(true) }, icon('refresh', 'size-3.5'), 'Refresh')), body);
    mount(body, spinner());
    let list;
    try { list = await get('/v1/infra/aws/services?account=' + encodeURIComponent(account), { signal }); } catch (e) { return mount(body, errorBox(e.message)); }
    if (n !== loads) return;
    const tiles = new Map();
    const groups = CATEGORIES.map(cat => {
      const svcs = list.services.filter(s => s.category === cat);
      if (!svcs.length) return null;
      return h('section', { 'data-category': cat }, h('h2', { class: 'h-section mb-2 flex items-center gap-2' }, h('span', { class: 'size-2 rounded-full ' + DOT[cat] }), cat),
        h('div', { class: 'grid gap-3 sm:grid-cols-2 lg:grid-cols-4' }, svcs.map(s => { const t = tile(s); tiles.set(s.id, t); return t; })));
    }).filter(Boolean);
    mount(body, h('div', { class: 'space-y-6' }, ...groups, h('p', { class: 'muted text-xs', id: 'services-note' }, `Region ${list.region}. Global services such as S3 are listed too. Counts include every resource; a page shows up to 200 of the largest services.`)));
    // Read the services a few at a time, in the order shown, so the first tiles fill in first.
    const queue = list.services.slice();
    const worker = async () => {
      while (queue.length) {
        const s = queue.shift();
        if (n !== loads || !body.isConnected) return;
        await fill(s, tiles.get(s.id), signal, refresh);
      }
    };
    await Promise.all(Array.from({ length: AT_ONCE }, worker));
  }

  function tile(s) {
    return h('a', { href: '#/services/' + encodeURIComponent(s.id), 'data-service': s.id, 'data-state': 'loading',
      class: 'block rounded-xl border border-slate-200 p-3 transition hover:border-brand-400 dark:border-slate-800' },
      h('div', { class: 'flex items-center justify-between gap-2' }, h('span', { class: 'truncate text-sm font-medium' }, s.label), s.global ? h('span', { class: 'muted text-[10px] uppercase' }, 'global') : null),
      h('div', { class: 'mt-1 flex items-center gap-2 text-2xl font-semibold tabular-nums', 'data-count': '' }, h('span', { class: 'size-4 animate-spin rounded-full border-2 border-slate-300 border-t-brand-600', role: 'status', 'aria-label': 'Loading' })),
      h('div', { class: 'muted mt-0.5 truncate text-xs', 'data-sub': '' }, 'reading…'));
  }

  async function fill(s, t, signal, refresh) {
    const count = t.querySelector('[data-count]'), sub = t.querySelector('[data-sub]');
    try {
      const d = await get('/v1/infra/aws/resources?account=' + encodeURIComponent(account) + '&service=' + encodeURIComponent(s.id) + (refresh ? '&refresh=1' : ''), { signal });
      const svc = d.services && d.services[0];
      if (d.error || !svc) throw new Error(d.error || 'no answer');
      if (svc.error) {
        t.dataset.state = 'error';
        mount(count, h('span', { class: 'text-base font-medium text-amber-700 dark:text-amber-400' }, 'not listed'));
        mount(sub, svc.error);
        return;
      }
      t.dataset.state = svc.count ? 'ok' : 'empty';
      t.classList.toggle('opacity-60', !svc.count);
      mount(count, String(svc.count));
      mount(sub, stateSummary(svc) || (svc.count ? ' ' : 'nothing here'));
    } catch (e) {
      if (signal.aborted) return;
      t.dataset.state = 'error';
      mount(count, h('span', { class: 'text-base font-medium text-rose-700 dark:text-rose-400' }, 'could not read'));
      mount(sub, e.message);
    }
  }

  draw(false);
  return el;
}

export async function servicePage({ params }) {
  const el = h('div', {});
  const id = params.id;
  mount(el, pageHeader(id, ''), spinner());
  const picked = await pickAccount(el, id, '');
  if (!picked) return el;
  let { account } = picked;
  const { accounts } = picked;
  const dev = atLeast(session.role, 'developer');
  const body = h('div', {});
  async function draw() {
    // The region is the account's, so the list is read again when the account changes (it costs no AWS call).
    let list;
    try { list = await get('/v1/infra/aws/services?account=' + encodeURIComponent(account), { signal: pageSignal() }); } catch (e) { mount(el, pageHeader(id, ''), errorBox(e.message)); return; }
    const info = list.services.find(s => s.id === id);
    if (!info) {
      mount(el, pageHeader('Services', ''), emptyState('cloud', 'No such service', `Forgeline does not list "${id}".`, h('div', { class: 'mt-4 flex justify-center' }, h('a', { class: 'btn-secondary', href: '#/services' }, 'All services'))));
      return;
    }
    mount(el, h('a', { href: '#/services', class: 'muted mb-2 inline-flex items-center gap-1 text-sm hover:underline', id: 'service-back' }, '← All services'),
      pageHeader(info.label, `${info.category}${info.global ? ' · global' : ' · region ' + list.region}. Everything of this kind in the account, read on its own.`,
        accountPicker(accounts, account, (a) => { account = a; saveAccount(a); draw(); })), body);
    mount(body, explorer({ dev, account, service: id }));
  }
  await draw();
  return el;
}
