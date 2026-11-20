// Security › Policy packs (design note 040, step 6): rule sets that check every blueprint preview, on top of the
// built-in guardrails. Developers can read them (so they know what a preview is checked by); administrators change them.
import { h, icon, mount, pageHeader, spinner, errorBox, notice, toast, emptyState } from '../lib/dom.js';
import { get, put, atLeast } from '../lib/api.js';
import { session } from '../app.js';

const ABOUT = {
  trivy: 'Known misconfigurations, each rated low to critical. Everything is a warning unless you choose a severity that denies.',
  checkov: 'A wider set of known misconfigurations. It does not rate them when it runs offline, so everything is a warning unless you list rules that deny.',
  conftest: 'Your own rules, written in Rego. A deny rule stops the plan; a warn rule is shown to the approver.',
};
const EXAMPLE = `package main

import rego.v1

# Every log group keeps its logs at least 30 days.
deny contains {"msg": msg, "address": rc.address} if {
	some rc in input.resource_changes
	rc.type == "aws_cloudwatch_log_group"
	rc.change.after.retention_in_days < 30
	msg := "logs must be kept at least 30 days"
}
`;

const slug = (s) => s.toLowerCase().replace(/[^a-z0-9]+/g, '-').replace(/^-+|-+$/g, '').slice(0, 31);
const words = (s) => s.split(/[\s,]+/).filter(Boolean);

export async function policyPacksPage() {
  const el = h('div', {});
  const admin = atLeast(session.role, 'admin');
  let packs, tools = [], builtin = '';
  const title = () => pageHeader('Policy packs', 'Every blueprint preview is checked by the built-in guardrails' + (builtin ? ' (' + builtin + ')' : '') + '. A pack adds more rules. Each pack runs in a container with no network, on a copy of the plan without its secret values. A pack that cannot run stops the plan.');
  mount(el, title(), spinner());
  try {
    const r = await get('/v1/settings/policy-packs');
    packs = r.packs || []; tools = r.tools || []; builtin = r.builtin || '';
  } catch (e) { mount(el, title(), errorBox(e.message)); return el; }
  const toolOf = (name) => tools.find(t => t.name === name) || { label: name, version: '' };

  const list = h('div', { class: 'space-y-4', id: 'policy-packs' });
  const err = h('div', { class: 'hidden' });

  function fileRow(name, body) {
    const row = h('div', { class: 'space-y-1', 'data-file': '' });
    row.append(
      h('div', { class: 'flex items-center gap-2' },
        h('input', { class: 'input w-56 font-mono text-[13px]', value: name, placeholder: 'rules.rego', maxlength: '46', 'data-file-name': '', 'aria-label': 'File name', disabled: !admin }),
        admin && h('button', { type: 'button', class: 'btn-ghost btn-sm', 'aria-label': 'Remove this file', onclick: () => row.remove() }, icon('trash', 'size-4'))),
      h('textarea', { class: 'input h-48 font-mono text-[13px]', spellcheck: 'false', wrap: 'off', 'data-file-body': '', 'aria-label': 'Rego rules', disabled: !admin }, body));
    return row;
  }

  function packCard(p) {
    const card = h('section', { class: 'card card-pad space-y-4', 'data-pack': '', 'data-id': p.id || '' });
    const name = h('input', { class: 'input', value: p.name || '', placeholder: 'Known misconfigurations', maxlength: '60', 'data-name': '', disabled: !admin });
    const tool = h('select', { class: 'input w-56', 'data-tool': '', disabled: !admin }, tools.map(t => h('option', { value: t.name, selected: t.name === p.tool }, t.label + ' ' + t.version)));
    const on = h('input', { type: 'checkbox', class: 'size-4 accent-brand-600', 'data-enabled': '', checked: !!p.enabled, disabled: !admin });
    const fields = h('div', { class: 'space-y-4' });
    const drawFields = () => {
      const t = tool.value;
      const ids = (label, attr, value, hint) => h('label', { class: 'block' }, h('span', { class: 'label' }, label),
        h('input', { class: 'input font-mono text-[13px]', value: (value || []).join(' '), [attr]: '', disabled: !admin, placeholder: t === 'checkov' ? 'CKV_AWS_38 CKV_AWS_39' : 'AWS-0040 AWS-0041' }), h('p', { class: 'hint' }, hint));
      if (t === 'conftest') {
        const files = h('div', { class: 'space-y-3', 'data-files': '' }, Object.entries(p.rules || {}).sort().map(([n, b]) => fileRow(n, b)));
        mount(fields, h('p', { class: 'muted text-sm' }, ABOUT[t]), h('div', {}, h('div', { class: 'label' }, 'Rule files'), files,
          admin && h('button', { type: 'button', class: 'btn-secondary btn-sm mt-2', 'data-add-file': '', onclick: () => files.append(fileRow(files.children.length ? '' : 'rules.rego', files.children.length ? '' : EXAMPLE)) }, icon('plus', 'size-4'), 'Add a file'),
          h('p', { class: 'hint' }, 'The plan is the input, as OpenTofu prints it (resource_changes, planned_values, configuration). A rule can return an object with msg and address to name the resource.')));
        return;
      }
      mount(fields, h('p', { class: 'muted text-sm' }, ABOUT[t]),
        t === 'trivy' ? h('label', { class: 'block' }, h('span', { class: 'label' }, 'Deny from'),
          h('select', { class: 'input w-56', 'data-deny-at': '', disabled: !admin }, [['', 'Never (warnings only)'], ['critical', 'Critical'], ['high', 'High and above'], ['medium', 'Medium and above'], ['low', 'Low and above']]
            .map(([v, l]) => h('option', { value: v, selected: (p.deny_at || '') === v }, l)))) : null,
        ids('Rules that always deny', 'data-deny', p.deny, 'Rule ids, separated by spaces. A plan that breaks one cannot be sent for approval.'),
        ids('Rules to skip', 'data-skip', p.skip, 'Rule ids your organization decided not to apply. Skipped findings are counted on the preview.'));
    };
    tool.addEventListener('change', drawFields);
    drawFields();
    card.append(
      h('div', { class: 'flex flex-wrap items-end gap-3' },
        h('label', { class: 'min-w-56 flex-1' }, h('span', { class: 'label' }, 'Name'), name),
        h('label', {}, h('span', { class: 'label' }, 'Checked by'), tool),
        h('label', { class: 'flex items-center gap-2 pb-2 text-sm' }, on, 'On'),
        admin && h('button', { type: 'button', class: 'btn-ghost btn-sm', 'aria-label': 'Remove this pack', onclick: () => card.remove() }, icon('trash', 'size-4'))),
      fields);
    return card;
  }

  const draw = () => mount(list, packs.length ? packs.map(packCard) : emptyState('check', 'No policy packs', 'Previews are checked by the built-in guardrails only. Add a pack for known misconfigurations, or for your own rules.'));
  draw();

  const read = () => {
    const used = new Set();
    return [...list.querySelectorAll('[data-pack]')].map(card => {
      const name = card.querySelector('[data-name]').value.trim();
      let id = card.dataset.id || slug(name) || 'pack';
      for (let n = 2; used.has(id); n++) id = (card.dataset.id || slug(name) || 'pack').slice(0, 28) + '-' + n;
      used.add(id);
      const tool = card.querySelector('[data-tool]').value;
      const p = { id, name, tool, enabled: card.querySelector('[data-enabled]').checked };
      if (tool === 'conftest') {
        p.rules = {};
        card.querySelectorAll('[data-file]').forEach(f => { p.rules[f.querySelector('[data-file-name]').value.trim()] = f.querySelector('[data-file-body]').value; });
      } else {
        if (tool === 'trivy') p.deny_at = card.querySelector('[data-deny-at]').value;
        p.deny = words(card.querySelector('[data-deny]').value);
        p.skip = words(card.querySelector('[data-skip]').value);
      }
      return p;
    });
  };

  const save = h('button', { type: 'button', class: 'btn-primary', id: 'packs-save', onclick: async () => {
    err.classList.add('hidden'); save.disabled = true;
    try { packs = (await put('/v1/settings/policy-packs', { packs: read() })).packs; toast('Policy packs saved', 'ok'); draw(); }
    catch (e) { mount(err, errorBox(e.message)); err.classList.remove('hidden'); }
    save.disabled = false;
  } }, 'Save packs');

  mount(el, title(),
    admin ? h('div', { class: 'mb-4' }, notice('info', 'A pack needs Docker on this server. After a change here, a preview made earlier must be made again before it can be sent for approval.'))
      : h('div', { class: 'mb-4' }, notice('info', 'Only administrators can change the packs.')),
    list, err,
    admin && h('div', { class: 'mt-4 flex flex-wrap justify-between gap-2' },
      h('button', { type: 'button', class: 'btn-secondary', id: 'pack-add', onclick: () => { if (!list.querySelector('[data-pack]')) mount(list); list.append(packCard({ tool: 'trivy', enabled: true, name: '' })); } }, icon('plus', 'size-4'), 'Add a pack'),
      save));
  return el;
}
