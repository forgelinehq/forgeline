// Build › Blueprints (design note 035, slice 1): drafts of what should exist in a cloud account, what each
// one means in plain words, and the OpenTofu code it generates. The canvas to edit blocks, the preview and
// the approval come in the next slices; nothing here touches the cloud.
import { h, icon, mount, pageHeader, relTime, spinner, errorBox, notice, toast, modal, confirmDialog, field, emptyState } from '../lib/dom.js';
import { get, post, del, atLeast } from '../lib/api.js';
import { navigate } from '../lib/router.js';
import { session } from '../app.js';
import { KINDS, blockFacts, linkWords, STARTERS } from '../lib/blueprint.js';

export async function blueprintsPage() {
  const el = h('div', {});
  const header = () => pageHeader('Blueprints', 'Designs of what should exist in your cloud. Nothing is built until a different administrator approves it.',
    h('button', { class: 'btn-primary', type: 'button', id: 'new-blueprint', onclick: newBlueprint }, icon('plus', 'size-4'), 'New blueprint'));
  mount(el, header(), spinner());
  let list;
  try { list = (await get('/v1/blueprints')).blueprints; } catch (e) { mount(el, header(), errorBox(e.message)); return el; }
  mount(el, header(), list.length
    ? h('div', { class: 'card divide-y divide-slate-100 dark:divide-slate-800', id: 'blueprint-list' }, list.map(b =>
      h('a', { href: '#/blueprints/' + b.id, class: 'flex flex-wrap items-center gap-x-4 gap-y-1 px-5 py-3.5 hover:bg-slate-50 dark:hover:bg-slate-800/50', 'data-blueprint': b.id },
        h('span', { class: 'min-w-0 flex-1' }, h('span', { class: 'block font-medium' }, b.name), h('span', { class: 'muted text-xs' }, `${b.blocks} block${b.blocks === 1 ? '' : 's'} · ${b.account} · ${b.region}`)),
        b.warnings ? h('span', { class: 'pill-wait' }, `${b.warnings} to look at`) : h('span', { class: 'pill-ok' }, 'no warnings'),
        h('span', { class: 'muted text-xs' }, 'changed by ' + b.updated_by + ' · ' + relTime(b.updated_at)))))
    : h('div', { class: 'card' }, emptyState('layers', 'No blueprints yet', 'A blueprint draws what should exist in your cloud: a front door, apps, tables, files and queues, with safe settings already chosen.',
      h('button', { class: 'btn-primary', type: 'button', onclick: newBlueprint }, 'Make the first one'))));
  return el;
}

async function newBlueprint() {
  let accounts = [];
  try { accounts = (await get('/v1/aws/accounts')).accounts; } catch { /* the primary account is still offered */ }
  if (!accounts.length) accounts = [{ id: 'primary', name: 'Primary', region: '' }];
  await modal('New blueprint', (close) => {
    const name = h('input', { id: 'bp-name', class: 'input', maxlength: '80', placeholder: 'Shop backend' });
    const acct = h('select', { id: 'bp-account', class: 'input' }, accounts.map(a => h('option', { value: a.id }, a.name + (a.region ? ' · ' + a.region : ''))));
    const region = h('input', { id: 'bp-region', class: 'input', placeholder: 'ap-south-1', value: accounts[0].region || '' });
    acct.addEventListener('change', () => { region.value = accounts.find(a => a.id === acct.value)?.region || region.value; });
    let start = 'webapp';
    const starters = h('div', { class: 'grid gap-2 sm:grid-cols-3', role: 'radiogroup', 'aria-label': 'Start from' }, Object.entries(STARTERS).map(([k, s]) => {
      const b = h('button', { type: 'button', role: 'radio', 'aria-checked': String(k === start), 'data-starter': k, class: 'rounded-xl border p-3 text-left text-sm aria-checked:border-brand-500 aria-checked:bg-brand-50 dark:aria-checked:bg-brand-500/10 border-slate-200 dark:border-slate-700',
        onclick: () => { start = k; starters.querySelectorAll('[role=radio]').forEach(x => x.setAttribute('aria-checked', String(x === b))); } },
        h('b', { class: 'block' }, s.label), h('span', { class: 'muted text-xs' }, s.text));
      return b;
    }));
    const err = h('div', {});
    const go = h('button', { class: 'btn-primary', type: 'button', onclick: async () => {
      const s = STARTERS[start];
      try {
        const b = await post('/v1/blueprints', { name: name.value, target: { cloud: 'aws', account: acct.value, region: region.value.trim() }, blocks: s.blocks, links: s.links });
        close(true);
        navigate('/design/' + b.id);
      } catch (e) { mount(err, h('div', { class: 'mt-3' }, errorBox(e.message))); }
    } }, 'Create');
    return h('div', { class: 'space-y-4' }, field('Name', name), h('div', { class: 'grid gap-3 sm:grid-cols-2' }, field('AWS account', acct), field('Region', region)),
      h('div', {}, h('div', { class: 'label' }, 'Start from'), starters), err,
      h('div', { class: 'flex justify-end gap-2' }, h('button', { class: 'btn-secondary', type: 'button', onclick: () => close(false) }, 'Cancel'), go));
  });
}

export async function blueprintPage({ params }) {
  const el = h('div', {});
  mount(el, spinner());
  let b;
  try { b = await get('/v1/blueprints/' + encodeURIComponent(params.id)); } catch (e) { mount(el, pageHeader('Blueprint', ''), errorBox(e.message)); return el; }
  const mayDelete = b.created_by === session.name || atLeast(session.role, 'admin');
  const codeBox = h('div', { id: 'blueprint-code' });
  let codeShown = false;
  async function toggleCode() {
    codeShown = !codeShown;
    if (!codeShown) return mount(codeBox, '');
    mount(codeBox, spinner());
    try {
      const res = await fetch('/v1/blueprints/' + encodeURIComponent(b.id) + '/code', { credentials: 'same-origin' });
      const text = await res.text();
      if (!res.ok) throw new Error(text);
      mount(codeBox, h('section', { class: 'card mt-4 overflow-hidden' },
        h('div', { class: 'flex items-center justify-between border-b border-slate-200 px-5 py-2.5 text-sm dark:border-slate-800' }, h('span', { class: 'font-medium' }, 'main.tf.json · OpenTofu'),
          h('a', { class: 'btn-ghost btn-sm', href: '/v1/blueprints/' + encodeURIComponent(b.id) + '/code', download: 'main.tf.json' }, icon('download', 'size-3.5'), 'Download')),
        h('pre', { class: 'max-h-[32rem] overflow-auto bg-slate-950 p-4 font-mono text-xs leading-5 text-slate-100' }, text)));
    } catch (e) { mount(codeBox, errorBox(e.message)); }
  }
  const remove = async () => {
    if (!await confirmDialog({ title: 'Delete ' + b.name + '?', body: 'Only the draft is deleted. Nothing in your cloud changes.', confirmLabel: 'Delete', danger: true })) return;
    try { await del('/v1/blueprints/' + encodeURIComponent(b.id)); toast('Deleted', 'ok'); navigate('/blueprints'); } catch (e) { toast(e.message, 'error'); }
  };
  mount(el,
    pageHeader(b.name, `${b.target.account} · ${b.target.region} · changed by ${b.updated_by} ${relTime(b.updated_at)}`,
      h('a', { class: 'btn-primary', href: '#/design/' + encodeURIComponent(b.id), id: 'open-canvas' }, icon('edit', 'size-4'), 'Open on the canvas'),
      h('button', { class: 'btn-secondary', type: 'button', id: 'show-code', onclick: toggleCode }, icon('git', 'size-4'), 'Code'),
      mayDelete && h('button', { class: 'btn-ghost text-rose-600 dark:text-rose-400', type: 'button', onclick: remove }, icon('trash', 'size-4'), 'Delete')),
    h('div', { class: 'mb-4' }, notice('info', 'Change it on the design canvas. Previewing what AWS would do and sending it for approval come next.')),
    h('div', { class: 'grid gap-4 lg:grid-cols-[minmax(0,1fr)_22rem]' },
      h('section', { class: 'card card-pad', id: 'blueprint-blocks' },
        h('h2', { class: 'h-section mb-3' }, 'Blocks'),
        b.blocks.length ? h('ul', { class: 'grid gap-2 sm:grid-cols-2' }, b.blocks.map(x => {
          const k = KINDS[x.kind] || { label: x.kind, tech: '', icon: 'box' };
          return h('li', { class: 'flex items-start gap-3 rounded-xl border p-3 ' + (x.kind === 'existing' ? 'border-dashed border-slate-300 dark:border-slate-700' : 'border-slate-200 dark:border-slate-800'), 'data-block': x.id },
            h('span', { class: 'rounded-lg bg-slate-100 p-2 text-slate-600 dark:bg-slate-800 dark:text-slate-300' }, icon(k.icon, 'size-4')),
            h('span', { class: 'min-w-0' }, h('b', { class: 'block' }, x.name), h('span', { class: 'muted block text-xs' }, k.label + ' · ' + k.tech), h('span', { class: 'block break-all text-xs' }, blockFacts(x))));
        })) : h('p', { class: 'muted text-sm' }, 'No blocks yet.'),
        b.links.length ? h('div', { class: 'mt-5' }, h('h3', { class: 'h-section mb-2' }, 'How they connect'),
          h('ul', { class: 'space-y-1 text-sm', id: 'blueprint-links' }, b.links.map(l => h('li', { class: 'flex items-center gap-2' }, icon('link', 'size-3.5 text-slate-400'), linkWords(b, l))))) : null),
      h('aside', { class: 'card card-pad self-start', id: 'blueprint-checks', 'aria-label': 'Checks' },
        h('h2', { class: 'h-section mb-3' }, 'What it means'),
        h('ul', { class: 'space-y-2 text-sm' }, b.checks.map(c => h('li', { class: 'flex items-start gap-2', 'data-level': c.level },
          h('span', { class: c.level === 'ok' ? 'text-emerald-600 dark:text-emerald-400' : 'text-amber-600 dark:text-amber-400' }, icon(c.level === 'ok' ? 'check' : 'alert', 'mt-0.5 size-4')), c.text))))),
    codeBox);
  return el;
}
