// Settings › AI › Assistant permissions (design note 052, slice 2): what the assistant may do as you. A read runs. A
// change shows you the exact command and waits. Here you set rules ahead of time (allow, ask, deny), and an
// administrator says which AWS accounts the assistant may change with a person's own approval; every other account is
// protected, so a change to it is a proposal that a different administrator approves.
import { h, icon, mount, pageHeader, spinner, errorBox, notice, toast, field, relTime, pill } from '../lib/dom.js';
import { get, post, put, del, atLeast } from '../lib/api.js';
import { session } from '../app.js';

const EFFECT = { allow: ['Allow', 'ok'], ask: ['Ask me', 'wait'], deny: ['Deny', 'bad'] };

export async function assistantPermissionsPage() {
  const el = h('div', {});
  async function load() {
    let d;
    try { d = await get('/v1/assistant/permissions'); } catch (e) { return mount(el, pageHeader('Assistant permissions', ''), errorBox(e.message)); }
    const admin = atLeast(session.role, 'admin');
    mount(el,
      pageHeader('Assistant permissions', 'What the assistant may do as you. A read runs. A change shows you the exact command and waits for your answer.'),
      notice('info', 'Only an administrator can have the assistant change AWS, only in the chat (never in a background run or by an agent), and only on an account marked below. Everything else about a change stays as it was: the service switches under Connections still apply.'),
      accountsCard(d.accounts, admin, load),
      rulesCard(d.rules, admin, load));
  }
  mount(el, spinner());
  await load();
  return el;
}

function accountsCard(accounts, admin, reload) {
  return h('section', { class: 'card card-pad mt-4', id: 'perm-accounts' },
    h('h2', { class: 'h-section mb-1' }, 'AWS accounts'),
    h('p', { class: 'muted mb-3 text-sm' }, 'Protected is the default: a change to the account is a proposal that a different administrator approves. Mark an account for your own approval when it is yours to change (a sandbox, a personal account), and an administrator can then approve each change in the chat.'),
    h('ul', { class: 'divide-y divide-slate-200 dark:divide-slate-800' }, accounts.map(a => h('li', { class: 'flex flex-wrap items-center justify-between gap-3 py-2.5', 'data-account': a.id },
      h('div', {}, h('div', { class: 'font-medium' }, a.name), h('div', { class: 'muted text-xs' }, a.own_approval ? 'The assistant may change it after a person approves the exact command in the chat.' : 'Protected: changes are proposals.')),
      h('div', { class: 'flex items-center gap-2' }, pill(a.own_approval ? 'ok' : 'idle', a.own_approval ? 'own approval' : 'protected'),
        admin ? h('button', { type: 'button', class: 'btn-secondary btn-sm', 'data-toggle-account': a.id, onclick: async () => {
          try { await put('/v1/assistant/permissions/accounts/' + encodeURIComponent(a.id), { own_approval: !a.own_approval }); toast(a.own_approval ? 'Protected again.' : 'The assistant may now change it with your approval.', 'ok'); } catch (e) { toast(e.message, 'error'); }
          reload();
        } }, a.own_approval ? 'Protect it' : 'Allow my own approval') : null)))));
}

function rulesCard(rules, admin, reload) {
  const scope = h('select', { id: 'perm-scope', class: 'input w-40', 'aria-label': 'Applies to' }, h('option', { value: 'person' }, 'Only me'), admin ? h('option', { value: 'org' }, 'Everyone') : null);
  const effect = h('select', { id: 'perm-effect', class: 'input w-32', 'aria-label': 'Effect' }, Object.entries(EFFECT).map(([v, [l]]) => h('option', { value: v }, l)));
  const pattern = h('input', { id: 'perm-pattern', class: 'input min-w-0 flex-1 font-mono text-sm', placeholder: 'aws ecs update-service *', spellcheck: 'false', maxlength: '200', 'aria-label': 'Command pattern' });
  const note = h('input', { id: 'perm-note', class: 'input min-w-0 flex-1 text-sm', placeholder: 'Why (optional)', maxlength: '200', 'aria-label': 'Note' });
  const out = h('div', {});
  const add = h('button', { type: 'button', class: 'btn-primary btn-sm', id: 'perm-add', onclick: async () => {
    add.disabled = true;
    try { await post('/v1/assistant/permissions/rules', { scope: scope.value, effect: effect.value, pattern: pattern.value.trim(), note: note.value.trim() }); toast('Rule added.', 'ok'); reload(); }
    catch (e) { mount(out, errorBox(e.message)); add.disabled = false; }
  } }, icon('plus', 'size-4'), 'Add rule');
  const row = (r) => h('tr', { 'data-rule': r.id },
    h('td', {}, pill(EFFECT[r.effect][1], EFFECT[r.effect][0])),
    h('td', { class: 'font-mono text-xs break-all' }, r.pattern, r.note ? h('div', { class: 'muted font-sans' }, r.note) : null),
    h('td', { class: 'text-sm' }, r.scope === 'org' ? 'Everyone' : 'Only you'),
    h('td', { class: 'muted text-sm' }, r.by),
    h('td', {}, (r.scope === 'person' || admin) ? h('button', { type: 'button', class: 'btn-ghost btn-sm', 'aria-label': 'Remove this rule', 'data-remove-rule': r.id, onclick: async () => {
      try { await del('/v1/assistant/permissions/rules/' + encodeURIComponent(r.id)); } catch (e) { toast(e.message, 'error'); }
      reload();
    } }, icon('trash', 'size-4')) : null));
  return h('section', { class: 'card mt-4 overflow-x-auto', id: 'perm-rules' },
    h('div', { class: 'border-b border-slate-200 px-5 py-3 dark:border-slate-800' }, h('h2', { class: 'h-section' }, 'Rules'),
      h('p', { class: 'muted text-xs' }, 'Matched against the command as Forgeline will run it; * stands for any characters. A deny always wins. An organisation "ask" cannot be skipped by your own allow. An allow names its service and operation, and never covers iam, organizations, account, ssm or sts: those always ask.')),
    rules.length ? h('table', { class: 'tbl' }, h('thead', {}, h('tr', {}, ['', 'Command', 'Applies to', 'By', ''].map(x => h('th', {}, x)))), h('tbody', {}, rules.map(row)))
      : h('p', { class: 'muted px-5 py-4 text-sm' }, 'No rules yet. Choosing "Always allow" on a change in the chat adds one here.'),
    h('div', { class: 'border-t border-slate-200 p-4 dark:border-slate-800' },
      h('div', { class: 'flex flex-wrap items-center gap-2' }, scope, effect, pattern),
      h('div', { class: 'mt-2 flex flex-wrap items-center gap-2' }, note, add), h('div', { class: 'mt-2' }, out)));
}
