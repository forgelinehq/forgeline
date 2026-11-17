// Settings › Resource types (design note 040, step 7): which AWS resource types the generic resource block may make,
// per account. Developers can read the list (so they know what they can draw); administrators change it.
import { h, icon, mount, pageHeader, spinner, errorBox, notice, toast } from '../lib/dom.js';
import { get, put, atLeast } from '../lib/api.js';
import { session } from '../app.js';

export async function resourceTypesPage() {
  const el = h('div', {});
  const admin = atLeast(session.role, 'admin');
  const title = () => pageHeader('Resource types', 'A resource block can make any AWS resource type allowed here for its account, with settings checked against the AWS provider’s own schema. Forgeline chooses none of its settings: the guardrails, policy packs and a different administrator’s approval check what people set. IAM users, access keys and account-wide settings can never be allowed.');
  mount(el, title(), spinner());
  let accounts = [], allowed = {};
  try {
    const [rt, a] = await Promise.all([get('/v1/settings/resource-types'), get('/v1/aws/accounts').catch(() => ({ accounts: [] }))]);
    allowed = rt.accounts || {};
    accounts = (a.accounts || []).map(x => ({ id: x.id, name: x.name || x.id }));
    if (!accounts.some(x => x.id === 'primary')) accounts.unshift({ id: 'primary', name: 'Primary' });
  } catch (e) { mount(el, title(), errorBox(e.message)); return el; }

  let account = accounts[0].id;
  const draft = Object.fromEntries(accounts.map(a => [a.id, [...(allowed[a.id] || [])]]));
  const list = h('div', { id: 'resource-types' });
  const results = h('div', { id: 'type-results', class: 'mt-2' });
  const err = h('div', { class: 'hidden' });

  function drawList() {
    const types = draft[account];
    mount(list, types.length
      ? h('ul', { class: 'flex flex-wrap gap-2' }, types.map(t => h('li', { class: 'flex items-center gap-1 rounded-lg border border-slate-200 px-2.5 py-1 font-mono text-[13px] dark:border-slate-700', 'data-type': t }, t,
          admin && h('button', { type: 'button', class: 'btn-ghost btn-sm', 'aria-label': 'Stop allowing ' + t, onclick: () => { draft[account] = draft[account].filter(x => x !== t); drawList(); } }, icon('x', 'size-3.5')))))
      : h('p', { class: 'muted text-sm' }, 'None: people can use only Forgeline’s own blocks and templates in this account.'));
  }

  const q = h('input', { id: 'type-search', class: 'input', placeholder: 'sns topic, cloudfront, route53 record…', 'aria-label': 'Find a resource type' });
  let seq = 0, timer = 0;
  q.addEventListener('input', () => {
    clearTimeout(timer);
    timer = setTimeout(async () => {
      const mine = ++seq, text = q.value.trim();
      if (!text) { mount(results); return; }
      mount(results, spinner());
      try {
        const r = await get('/v1/resource-types/search?q=' + encodeURIComponent(text));
        if (mine !== seq) return;
        mount(results, r.types.length ? h('ul', { class: 'divide-y divide-slate-200 dark:divide-slate-800' }, r.types.map(t => h('li', { class: 'flex items-center justify-between gap-2 py-1.5' },
          h('span', {}, h('code', { class: 'text-[13px]' }, t.type), h('span', { class: 'muted ml-2 text-xs' }, t.label)),
          draft[account].includes(t.type) ? h('span', { class: 'muted text-xs' }, 'allowed')
            : h('button', { type: 'button', class: 'btn-secondary btn-sm', 'data-allow': t.type, onclick: () => { draft[account] = [...draft[account], t.type].sort(); drawList(); q.dispatchEvent(new Event('input')); } }, icon('plus', 'size-3.5'), 'Allow'))))
          : h('p', { class: 'muted text-sm' }, 'No resource type matches.'),
          h('p', { class: 'hint mt-2' }, 'AWS provider ' + (r.provider_version || '') + ', ' + r.count + ' resource types.'));
      } catch (e) { if (mine === seq) mount(results, errorBox(e.message)); }
    }, 250);
  });

  const pick = h('select', { id: 'type-account', class: 'input w-64' }, accounts.map(a => h('option', { value: a.id }, a.name)));
  pick.addEventListener('change', () => { account = pick.value; drawList(); mount(results); q.value = ''; });

  const save = h('button', { type: 'button', class: 'btn-primary', id: 'types-save', onclick: async () => {
    err.classList.add('hidden'); save.disabled = true;
    try {
      const r = await put('/v1/settings/resource-types', { accounts: draft });
      for (const a of accounts) draft[a.id] = [...((r.accounts || {})[a.id] || [])];
      toast('Resource types saved', 'ok'); drawList();
    } catch (e) { mount(err, errorBox(e.message)); err.classList.remove('hidden'); }
    save.disabled = false;
  } }, 'Save');

  drawList();
  mount(el, title(),
    admin ? null : h('div', { class: 'mb-4' }, notice('info', 'Only administrators can change which resource types are allowed.')),
    h('section', { class: 'card card-pad space-y-4' },
      accounts.length > 1 ? h('label', { class: 'block' }, h('span', { class: 'label' }, 'AWS account'), pick) : null,
      h('div', {}, h('div', { class: 'label' }, 'Allowed'), list),
      admin && h('div', {}, h('label', { class: 'label', for: 'type-search' }, 'Allow another'), q, results,
        h('p', { class: 'hint' }, 'The first search reads the AWS provider’s schema, which takes a minute once.'))),
    err,
    admin && h('div', { class: 'mt-4 flex justify-end' }, save));
  return el;
}
