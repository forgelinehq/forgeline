// Settings › Approval rules (design note 032): which changes need two approvals, and who may approve them.
// Developers can read the rules (so they know who will approve); administrators change them.
import { h, icon, mount, pageHeader, spinner, errorBox, notice, toast, emptyState } from '../lib/dom.js';
import { get, put, atLeast } from '../lib/api.js';
import { session } from '../app.js';

export async function approvalRulesPage() {
  const el = h('div', {});
  const admin = atLeast(session.role, 'admin');
  const title = () => pageHeader('Approval rules', 'Every change already needs an administrator who did not propose it. A rule can ask for two approvals, or name who may approve, for the changes it matches. When several rules match, the strictest wins.');
  mount(el, title(), spinner());
  let rules, accounts = [], admins = [];
  try {
    const [r, a, u] = await Promise.all([get('/v1/settings/approvals'), get('/v1/aws/accounts').catch(() => ({ accounts: [] })), admin ? get('/v1/users').catch(() => ({ users: [] })) : { users: [] }]);
    rules = r.rules || [];
    accounts = (a.accounts || []).map(x => ({ id: x.id, name: x.name || x.id }));
    if (!accounts.some(x => x.id === 'primary')) accounts.unshift({ id: 'primary', name: 'Primary' });
    admins = (u.users || []).filter(x => x.role === 'admin' && !x.disabled).map(x => x.username);
  } catch (e) { mount(el, title(), errorBox(e.message)); return el; }

  const list = h('div', { class: 'space-y-4', id: 'approval-rules' });
  const err = h('div', { class: 'hidden' });

  function chips(options, picked, attr, disabled) {
    return h('div', { class: 'flex flex-wrap gap-2' }, options.map(([value, label]) => h('label', { class: 'flex items-center gap-1.5 rounded-lg border border-slate-200 px-2.5 py-1.5 text-sm has-[:checked]:border-brand-400 has-[:checked]:bg-brand-50 dark:border-slate-700 dark:has-[:checked]:bg-brand-500/10' },
      h('input', { type: 'checkbox', class: 'size-4 accent-brand-600', [attr]: value, checked: picked.includes(value), disabled }), label)));
  }

  function ruleCard(r) {
    const card = h('section', { class: 'card card-pad space-y-4', 'data-rule': '' });
    const name = h('input', { class: 'input', value: r.name || '', placeholder: 'Production needs two', maxlength: '60', 'data-name': '', disabled: !admin });
    const approvals = h('select', { class: 'input w-48', 'data-approvals': '', disabled: !admin }, [[1, 'One approval'], [2, 'Two approvals']].map(([v, l]) => h('option', { value: String(v), selected: (r.approvals || 1) === v }, l)));
    const high = h('input', { type: 'checkbox', class: 'size-4 accent-brand-600', 'data-high': '', checked: !!r.high_risk, disabled: !admin });
    card.append(
      h('div', { class: 'flex flex-wrap items-end gap-3' },
        h('label', { class: 'min-w-56 flex-1' }, h('span', { class: 'label' }, 'Name'), name),
        h('label', {}, h('span', { class: 'label' }, 'Needs'), approvals),
        admin && h('button', { type: 'button', class: 'btn-ghost btn-sm', 'aria-label': 'Remove this rule', onclick: () => card.remove() }, icon('trash', 'size-4'))),
      h('div', {}, h('div', { class: 'label' }, 'For changes to'), chips(accounts.map(a => [a.id, a.name]), r.accounts || [], 'data-account', !admin), h('p', { class: 'hint' }, 'None ticked: every account.')),
      h('div', {}, h('div', { class: 'label' }, 'Made by'), chips([['ai', 'The assistant (AI-drafted changes)'], ['cloud', 'People in the console']], r.kinds || [], 'data-kind', !admin), h('p', { class: 'hint' }, 'None ticked: both.')),
      h('label', { class: 'flex items-center gap-2 text-sm' }, high, 'Only changes rated high risk (for example deleting a stack)'),
      h('div', {}, h('div', { class: 'label' }, 'Who may approve'), admins.length || (r.approvers || []).length
        ? chips([...new Set([...admins, ...(r.approvers || [])])].map(u => [u, u]), r.approvers || [], 'data-approver', !admin)
        : h('p', { class: 'muted text-sm' }, (r.approvers || []).length ? r.approvers.join(', ') : 'Any administrator.'),
        h('p', { class: 'hint' }, 'None ticked: any administrator who did not propose the change.')));
    return card;
  }

  const draw = () => mount(list, rules.length ? rules.map(ruleCard) : emptyState('check', 'No rules', 'Every change needs one administrator who did not propose it. Add a rule to ask for more.'));
  draw();

  const read = () => [...list.querySelectorAll('[data-rule]')].map(card => ({
    name: card.querySelector('[data-name]').value.trim(),
    approvals: Number(card.querySelector('[data-approvals]').value),
    high_risk: card.querySelector('[data-high]').checked,
    accounts: [...card.querySelectorAll('[data-account]:checked')].map(b => b.dataset.account),
    kinds: [...card.querySelectorAll('[data-kind]:checked')].map(b => b.dataset.kind),
    approvers: [...card.querySelectorAll('[data-approver]:checked')].map(b => b.dataset.approver),
  }));

  const save = h('button', { type: 'button', class: 'btn-primary', id: 'rules-save', onclick: async () => {
    err.classList.add('hidden'); save.disabled = true;
    try { rules = (await put('/v1/settings/approvals', { rules: read() })).rules; toast('Approval rules saved', 'ok'); draw(); }
    catch (e) { mount(err, errorBox(e.message)); err.classList.remove('hidden'); }
    save.disabled = false;
  } }, 'Save rules');

  mount(el, title(),
    admin ? null : h('div', { class: 'mb-4' }, notice('info', 'Only administrators can change these rules.')),
    list, err,
    admin && h('div', { class: 'mt-4 flex flex-wrap justify-between gap-2' },
      h('button', { type: 'button', class: 'btn-secondary', id: 'rule-add', onclick: () => { if (!list.querySelector('[data-rule]')) mount(list); list.append(ruleCard({ approvals: 2 })); } }, icon('plus', 'size-4'), 'Add a rule'),
      save));
  return el;
}
