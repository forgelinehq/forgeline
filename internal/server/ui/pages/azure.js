// Infrastructure → Azure (design note 025, slice S6). Read-only: Forgeline runs the server user's own az
// login and one Azure Resource Graph query over the subscriptions it can read, or the ones you choose.
// Nothing here can change an Azure resource.
import { h, icon, mount, spinner, errorBox, notice, toast, confirmDialog } from '../lib/dom.js';
import { get, put, atLeast } from '../lib/api.js';
import { session } from '../app.js';
import { explorer } from './aws_explorer.js';

export async function azureTab(body) {
  const admin = atLeast(session.role, 'admin');
  const dev = atLeast(session.role, 'developer');
  let cfg;
  async function load() {
    try { cfg = await get('/v1/integrations/azure'); } catch (e) { return mount(body, errorBox(e.message)); }
    cfg.enabled ? connected() : connectCard();
  }

  function connectCard() {
    const chosen = new Set();
    const err = h('div', { class: 'hidden' });
    const go = h('button', { class: 'btn-primary', type: 'submit', id: 'azure-connect', disabled: !admin }, icon('check', 'size-4'), 'Connect');
    const subs = cfg.available || [];
    const list = subs.length ? h('div', { class: 'space-y-1.5', role: 'group', 'aria-label': 'Subscriptions' }, subs.map(s => h('label', { class: 'flex items-center gap-2 text-sm' },
      h('input', { type: 'checkbox', class: 'size-4 rounded', 'data-sub': s.id, onchange: (e) => { if (e.target.checked) chosen.add(s.id); else chosen.delete(s.id); } }),
      h('span', { class: 'font-medium' }, s.name), h('span', { class: 'muted font-mono text-xs' }, s.id), s.state && s.state !== 'Enabled' ? h('span', { class: 'pill-idle' }, s.state) : null)))
      : h('p', { class: 'muted text-sm' }, admin ? 'No subscription is visible yet. Sign in on the server first (below); you can still connect and choose later.' : 'An administrator chooses the subscriptions.');
    mount(body, h('form', { class: 'card card-pad max-w-2xl space-y-5', novalidate: true, onsubmit: async (e) => {
      e.preventDefault(); err.classList.add('hidden'); go.disabled = true;
      try { await put('/v1/integrations/azure', { enabled: true, subscriptions: [...chosen] }); toast('Azure connected', 'ok'); await load(); }
      catch (x) { mount(err, errorBox(x.message)); err.classList.remove('hidden'); go.disabled = false; }
    } },
      h('div', {}, h('h2', { class: 'text-base font-semibold' }, 'Connect Azure'),
        h('p', { class: 'muted mt-1 text-sm' }, 'Forgeline uses the az login of the user the server runs as, and only reads. It lists the resources in your subscriptions with one Azure Resource Graph query. It cannot create, change or delete anything in Azure.')),
      !admin && notice('info', 'Only administrators can connect Azure.'),
      h('div', {}, h('div', { class: 'h-section mb-2' }, 'Which subscriptions?'), list,
        h('p', { class: 'muted mt-2 text-xs' }, 'Choose none to read every subscription the login can see. Narrower is safer.')),
      h('div', { class: 'rounded-lg bg-slate-50 p-3 text-sm dark:bg-slate-800/60' }, h('div', { class: 'mb-1 font-medium' }, 'What the login needs'),
        h('ul', { class: 'list-disc space-y-1 pl-5 text-slate-600 dark:text-slate-300' },
          h('li', {}, 'You have run ', h('code', { class: 'font-mono text-xs' }, 'az login --use-device-code'), ' (or signed in with a service principal) as the server\'s user.'),
          h('li', {}, 'That login has the Reader role on the subscriptions.'),
          h('li', {}, 'The Resource Graph extension is installed: ', h('code', { class: 'font-mono text-xs' }, 'az extension add --name resource-graph'), '.'))),
      err, admin && h('div', {}, go)));
  }

  function connected() {
    const ex = explorer({ dev, url: '/v1/infra/azure/resources', scope: 'scope' });
    const n = (cfg.subscriptions || []).length;
    mount(body, h('div', { class: 'space-y-4' },
      h('div', { class: 'card card-pad flex flex-wrap items-center gap-3' },
        h('span', { class: 'pill-ok' }, 'Connected'),
        h('span', { class: 'text-sm', id: 'azure-scope-shown' }, n ? n + ' subscription' + (n === 1 ? '' : 's') : 'every subscription the login can read'),
        h('span', { class: 'muted text-sm' }, 'read-only, through the server\'s az login'),
        h('div', { class: 'ml-auto flex flex-wrap gap-2' },
          h('a', { class: 'btn-secondary btn-sm', href: '#/integrations/azure' }, icon('bolt', 'size-3.5'), 'Test connection'),
          admin && h('button', { class: 'btn-ghost btn-sm', type: 'button', id: 'azure-off', onclick: turnOff }, 'Turn off'))),
      ex));
  }

  async function turnOff() {
    if (!await confirmDialog({ title: 'Turn off Azure?', body: 'Forgeline stops reading it. Nothing in Azure changes, and the az login stays on the server.', confirmLabel: 'Turn off' })) return;
    try { await put('/v1/integrations/azure', { enabled: false, subscriptions: cfg.subscriptions || [] }); toast('Turned off', 'ok'); load(); } catch (e) { toast(e.message, 'error'); }
  }

  mount(body, spinner());
  await load();
}
