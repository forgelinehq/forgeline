// Infrastructure → Google Cloud (design note 025, slice S5). Read-only: Forgeline runs the server user's own
// gcloud login and lists what Cloud Asset Inventory finds in one scope (a project, folder or organization).
// Nothing here can change a Google resource.
import { h, icon, mount, spinner, errorBox, notice, toast, field, confirmDialog } from '../lib/dom.js';
import { get, put, atLeast } from '../lib/api.js';
import { session } from '../app.js';
import { explorer } from './aws_explorer.js';

const SCOPE_HELP = 'A project id (my-project), or projects/my-project, folders/1234567890 or organizations/1234567890.';

export async function gcpTab(body) {
  const admin = atLeast(session.role, 'admin');
  const dev = atLeast(session.role, 'developer');
  let cfg;
  async function load() {
    try { cfg = await get('/v1/integrations/gcp'); } catch (e) { return mount(body, errorBox(e.message)); }
    cfg.enabled ? connected() : connectCard();
  }

  function connectCard() {
    const scope = h('input', { id: 'gcp-scope', class: 'input font-mono', placeholder: 'my-project', value: cfg.detected_project || '', disabled: !admin, 'aria-describedby': 'gcp-scope-help' });
    const err = h('div', { class: 'hidden' });
    const go = h('button', { class: 'btn-primary', type: 'submit', id: 'gcp-connect', disabled: !admin }, icon('check', 'size-4'), 'Connect');
    mount(body, h('form', { class: 'card card-pad max-w-2xl space-y-5', novalidate: true, onsubmit: async (e) => {
      e.preventDefault(); err.classList.add('hidden'); go.disabled = true;
      try { await put('/v1/integrations/gcp', { enabled: true, scope: scope.value }); toast('Google Cloud connected', 'ok'); await load(); }
      catch (x) { mount(err, errorBox(x.message)); err.classList.remove('hidden'); go.disabled = false; }
    } },
      h('div', {}, h('h2', { class: 'text-base font-semibold' }, 'Connect Google Cloud'),
        h('p', { class: 'muted mt-1 text-sm' }, 'Forgeline uses the gcloud login of the user the server runs as, and only reads. It lists the resources in one scope with Cloud Asset Inventory. It cannot create, change or delete anything in Google Cloud.')),
      !admin && notice('info', 'Only administrators can connect Google Cloud.'),
      field('What should Forgeline see?', scope, SCOPE_HELP + (cfg.detected_project ? ' The server\'s gcloud default project is filled in.' : ''), 'gcp-scope'),
      h('div', { class: 'rounded-lg bg-slate-50 p-3 text-sm dark:bg-slate-800/60' }, h('div', { class: 'mb-1 font-medium' }, 'What the login needs'),
        h('ul', { class: 'list-disc space-y-1 pl-5 text-slate-600 dark:text-slate-300' },
          h('li', {}, 'You have run ', h('code', { class: 'font-mono text-xs' }, 'gcloud auth login'), ' (or activated a service account) as the server\'s user.'),
          h('li', {}, 'That login has the Cloud Asset Viewer role (', h('code', { class: 'font-mono text-xs' }, 'roles/cloudasset.viewer'), ') on the scope.'),
          h('li', {}, 'The Cloud Asset API (', h('code', { class: 'font-mono text-xs' }, 'cloudasset.googleapis.com'), ') is enabled.'))),
      err, admin && h('div', {}, go)));
  }

  function connected() {
    const ex = explorer({ dev, url: '/v1/infra/gcp/resources', scope: 'scope' });
    mount(body, h('div', { class: 'space-y-4' },
      h('div', { class: 'card card-pad flex flex-wrap items-center gap-3' },
        h('span', { class: 'pill-ok' }, 'Connected'), h('span', { class: 'font-mono text-sm', id: 'gcp-scope-shown' }, cfg.scope),
        h('span', { class: 'muted text-sm' }, 'read-only, through the server\'s gcloud login'),
        h('div', { class: 'ml-auto flex flex-wrap gap-2' },
          h('a', { class: 'btn-secondary btn-sm', href: '#/integrations/gcp' }, icon('bolt', 'size-3.5'), 'Test connection'),
          admin && h('button', { class: 'btn-ghost btn-sm', type: 'button', id: 'gcp-off', onclick: turnOff }, 'Turn off'))),
      ex));
  }

  async function turnOff() {
    if (!await confirmDialog({ title: 'Turn off Google Cloud?', body: 'Forgeline stops reading it. Nothing in Google Cloud changes, and the gcloud login stays on the server.', confirmLabel: 'Turn off' })) return;
    try { await put('/v1/integrations/gcp', { enabled: false, scope: cfg.scope }); toast('Turned off', 'ok'); load(); } catch (e) { toast(e.message, 'error'); }
  }

  mount(body, spinner());
  await load();
}
