// Watch › Map (note 029, frame 6; note 034): the AWS map on its own page, with the accounts to switch between.
// What a stack made sits in the stack's box; a line is drawn only where AWS states the link. Read-only:
// anything that changes the account is a proposal someone approves.
import { h, mount, pageHeader, spinner, errorBox, emptyState } from '../lib/dom.js';
import { get, atLeast } from '../lib/api.js';
import { pageSignal } from '../lib/router.js';
import { session } from '../app.js';
import { explorer } from './aws_explorer.js';

const ACCOUNT_KEY = 'forgeline-aws-account'; // shared with Infrastructure › AWS, so both open on the same account

export async function watchMapPage() {
  const el = h('div', {});
  const dev = atLeast(session.role, 'developer');
  let account = 'primary';
  try { account = sessionStorage.getItem(ACCOUNT_KEY) || 'primary'; } catch { /* a blocked store only forgets the choice */ }
  mount(el, pageHeader('Map', 'What runs in your cloud, grouped by the stack that made it and the network it is in.'), spinner());
  let accounts = [], v;
  try {
    [accounts, v] = await Promise.all([get('/v1/aws/accounts').then(r => r.accounts).catch(() => []), get('/v1/infra/aws?account=primary', { signal: pageSignal() })]);
  } catch (e) { mount(el, pageHeader('Map', ''), errorBox(e.message)); return el; }
  if (account !== 'primary' && !accounts.some(a => a.id === account)) account = 'primary';
  if (!v.enabled && account === 'primary') account = accounts.find(a => !a.primary)?.id || 'primary'; // only added accounts are connected
  if (!v.enabled && accounts.length <= 1) {
    mount(el, pageHeader('Map', 'What runs in your cloud.'), emptyState('cloud', 'No cloud account connected', 'Connect an AWS account and its resources appear here.',
      atLeast(session.role, 'admin') && h('div', { class: 'mt-4 flex justify-center' }, h('a', { class: 'btn-primary', href: '#/integrations/aws' }, 'Connect AWS'))));
    return el;
  }
  const body = h('div', {});
  const picker = () => accounts.length > 1 ? h('div', { class: 'inline-flex overflow-hidden rounded-lg border border-slate-300 text-sm dark:border-slate-700', role: 'group', 'aria-label': 'AWS account', id: 'map-accounts' },
    accounts.map(a => h('button', { type: 'button', 'data-account': a.id, 'aria-pressed': String(a.id === account),
      class: 'px-3 py-1.5 ' + (a.id === account ? 'bg-brand-600 text-white' : 'text-slate-600 hover:bg-slate-100 dark:text-slate-300 dark:hover:bg-slate-800'),
      onclick: () => { account = a.id; try { sessionStorage.setItem(ACCOUNT_KEY, account); } catch { /* fine */ } draw(); } }, a.name))) : null;
  function draw() {
    mount(el, pageHeader('Map', 'What runs in your cloud, grouped by the stack that made it and the network it is in. Pick something to see what it uses.', picker()), body);
    mount(body, explorer({ dev, account }));
  }
  draw();
  return el;
}
