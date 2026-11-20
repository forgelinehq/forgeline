// Settings › Agent registries (design note 046, step 3): where agents may be pushed. A registry's token goes into the
// vault and is never shown again. Pushing is a proposal another administrator approves, from an agent's page.
import { h, icon, mount, pageHeader, spinner, errorBox, notice, toast, modal, field, emptyState, relTime, confirmDialog } from '../lib/dom.js';
import { get, post, put, del } from '../lib/api.js';

const EXAMPLES = [
  ['GitHub', 'ghcr.io', 'your-org/agents', 'A personal access token with write:packages.'],
  ['Docker Hub', 'registry-1.docker.io', 'your-user', 'A personal access token with Read & Write.'],
];

export async function registriesPage() {
  const el = h('div', {});
  async function load() {
    mount(el, pageHeader('Agent registries', ''), spinner());
    let list;
    try { list = (await get('/v1/ai/registries')).registries; } catch (e) { mount(el, pageHeader('Agent registries', ''), errorBox(e.message)); return; }
    mount(el, pageHeader('Agent registries', 'Container registries agents can be pushed to, so they run anywhere with docker agent run. A push is always a proposal another administrator approves.',
      h('button', { class: 'btn-primary', type: 'button', id: 'registry-add', onclick: () => form(null) }, icon('plus', 'size-4'), 'Add registry')),
      list.length ? h('div', { class: 'card divide-y divide-slate-200 dark:divide-slate-800', id: 'registry-list' }, list.map(row))
        : emptyState('box', 'No registries yet', 'Add GitHub Container Registry, Docker Hub or any OCI registry. Then an agent\'s page offers Push to a registry.'));
  }

  function row(g) {
    const status = h('span', { class: 'muted text-xs', 'data-test-result': g.id });
    return h('div', { class: 'flex flex-wrap items-center justify-between gap-3 px-5 py-4', 'data-registry': g.id },
      h('div', { class: 'min-w-0' },
        h('div', { class: 'font-medium' }, g.name),
        h('div', { class: 'muted break-all font-mono text-xs' }, g.host + (g.namespace ? '/' + g.namespace : '') + '/…'),
        h('div', { class: 'muted mt-0.5 text-xs' }, [g.username ? 'signs in as ' + g.username : 'no user name', g.has_token ? 'token in the vault' : 'no token', 'changed ' + relTime(g.updated_at) + ' by ' + (g.updated_by || g.created_by)].join(' · '))),
      h('div', { class: 'flex flex-wrap items-center gap-2' }, status,
        h('button', { class: 'btn-secondary btn-sm', type: 'button', 'data-test': g.id, onclick: async (e) => {
          e.target.disabled = true; status.textContent = 'Checking…';
          try { await post('/v1/ai/registries/' + g.id + '/test'); status.textContent = g.username ? 'Signed in, it works' : 'Reached, it works'; status.className = 'pill-ok'; }
          catch (err) { status.textContent = err.message; status.className = 'text-xs text-rose-600 dark:text-rose-400'; }
          e.target.disabled = false;
        } }, icon('refresh', 'size-3.5'), 'Check'),
        h('button', { class: 'btn-secondary btn-sm', type: 'button', onclick: () => form(g) }, icon('edit', 'size-3.5'), 'Change'),
        h('button', { class: 'btn-ghost btn-sm text-rose-600 dark:text-rose-400', type: 'button', 'aria-label': 'Remove ' + g.name, title: 'Remove', onclick: async () => {
          if (!await confirmDialog({ title: 'Remove ' + g.name + '?', body: 'Its token is deleted from the vault. Pushes waiting for approval to it can no longer be approved. Agents already pushed stay in the registry.', confirmLabel: 'Remove', danger: true })) return;
          try { await del('/v1/ai/registries/' + g.id); toast('Removed', 'ok'); load(); } catch (err) { toast(err.message, 'error'); }
        } }, icon('trash', 'size-3.5'))));
  }

  function form(g) {
    modal(g ? 'Change ' + g.name : 'Add a registry', (close) => {
      const input = (id, attrs = {}) => h('input', { id, class: 'input', ...attrs });
      const name = input('reg-name', { value: g?.name || '', maxlength: '60', placeholder: 'GitHub packages' });
      const host = input('reg-host', { value: g?.host || '', class: 'input font-mono', placeholder: 'ghcr.io', spellcheck: 'false' });
      const ns = input('reg-namespace', { value: g?.namespace || '', class: 'input font-mono', placeholder: 'your-org/agents', spellcheck: 'false' });
      const user = input('reg-user', { value: g?.username || '', autocomplete: 'off', spellcheck: 'false' });
      const token = input('reg-token', { type: 'password', autocomplete: 'new-password', placeholder: g?.has_token ? 'Leave empty to keep the stored token' : '' });
      const out = h('div', {});
      const save = h('button', { class: 'btn-primary', type: 'button', id: 'reg-save', onclick: async () => {
        save.disabled = true;
        const body = { name: name.value, host: host.value, namespace: ns.value, username: user.value };
        if (token.value) body.token = token.value;
        try {
          if (g) await put('/v1/ai/registries/' + g.id, body); else await post('/v1/ai/registries', body);
          toast('Saved', 'ok'); close(); load();
        } catch (e) { mount(out, errorBox(e.message)); save.disabled = false; }
      } }, 'Save');
      return h('div', { class: 'space-y-4' },
        h('div', { class: 'grid gap-2 sm:grid-cols-2' }, EXAMPLES.map(([n, hst, nsp, tip]) => h('button', { type: 'button', class: 'rounded-xl border border-slate-200 p-3 text-left text-sm hover:border-brand-400 dark:border-slate-700', onclick: () => {
          if (!name.value) name.value = n;
          host.value = hst; if (!ns.value) ns.value = nsp;
        } }, h('span', { class: 'block font-medium' }, n), h('span', { class: 'muted block text-xs' }, tip)))),
        field('Name', name, null, 'reg-name'),
        field('Host', host, 'The registry\'s host, with a port if it has one. Plain http is used only for localhost and 127.0.0.1.', 'reg-host'),
        field('Namespace', ns, 'Optional. Every repository goes under it, for example your-org/agents.', 'reg-namespace'),
        field('User name', user, null, 'reg-user'),
        field('Token', token, 'Kept in the vault and never shown again. Use a token that may push, not a password.', 'reg-token'),
        notice('info', 'Forgeline signs in only when a push is approved, or when you press Check.'),
        out,
        h('div', { class: 'flex justify-end gap-2' }, h('button', { class: 'btn-secondary', type: 'button', onclick: () => close() }, 'Cancel'), save));
    });
  }

  await load();
  return el;
}
