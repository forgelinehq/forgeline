// Settings › AI › Assistant permissions (design note 052, slice 2): what the assistant may do as you. A read runs. A
// change shows you the exact command and waits. Here you set rules ahead of time (allow, ask, deny), and an
// administrator says which AWS accounts the assistant may change with a person's own approval; every other account is
// protected, so a change to it is a proposal that a different administrator approves.
import { h, icon, mount, pageHeader, spinner, errorBox, notice, toast, field, relTime, pill, confirmDialog } from '../lib/dom.js';
import { get, post, put, del, atLeast } from '../lib/api.js';
import { session } from '../app.js';

const EFFECT = { allow: ['Allow', 'ok'], ask: ['Ask me', 'wait'], deny: ['Deny', 'bad'] };

export async function assistantPermissionsPage() {
  const el = h('div', {});
  async function load() {
    let d;
    try { d = await get('/v1/assistant/permissions'); } catch (e) { return mount(el, pageHeader('Assistant permissions', ''), errorBox(e.message)); }
    const admin = atLeast(session.role, 'admin');
    let sb = null;
    try { sb = await get('/v1/assistant/sandbox'); } catch { /* the page still works without it */ }
    mount(el,
      pageHeader('Assistant permissions', 'What the assistant may do as you. A read runs. A change shows you the exact command and waits for your answer.'),
      notice('info', 'Only an administrator can have the assistant change AWS, only in the chat (never in a background run or by an agent), and only on an account marked below. Everything else about a change stays as it was: the service switches under Connections still apply.'),
      sb ? sandboxCard(sb, admin, load) : null,
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

/** The sandbox: where the assistant runs commands and code. Off until an administrator turns it on. */
function sandboxCard(sb, admin, reload) {
  const set = async (body, msg) => { try { await put('/v1/assistant/sandbox', body); toast(msg, 'ok'); } catch (e) { toast(e.message, 'error'); } reload(); };
  const net = h('select', { id: 'sandbox-network', class: 'input w-full max-w-xs', 'aria-label': 'Network', disabled: !admin, onchange: (e) => set({ network: e.target.value }, 'Saved. A new sandbox uses it; reset yours to apply it now.') },
    h('option', { value: 'internet', selected: sb.network === 'internet' }, 'Internet (can install things)'), h('option', { value: 'none', selected: sb.network === 'none' }, 'No network'));
  return h('section', { class: 'card card-pad mt-4', id: 'perm-sandbox' },
    h('div', { class: 'mb-1 flex flex-wrap items-center justify-between gap-2' }, h('h2', { class: 'h-section' }, 'Sandbox'),
      h('div', { class: 'flex items-center gap-2' }, pill(sb.enabled ? 'ok' : 'idle', sb.enabled ? 'on' : 'off'),
        admin ? h('button', { type: 'button', class: 'btn-secondary btn-sm', id: 'sandbox-toggle', onclick: () => set({ enabled: !sb.enabled }, sb.enabled ? 'The sandbox is off.' : 'The sandbox is on.') }, sb.enabled ? 'Turn off' : 'Turn on') : null)),
    h('p', { class: 'muted mb-3 text-sm' }, 'A container Forgeline starts for you, where the assistant can run any command, install anything (apt, pip, npm, curl) and write and run code in any language. It keeps a /work folder between calls and is removed after ' + sb.idle_minutes + ' minutes unused. It cannot see this server\'s files or credentials, and gets a secret from your vault only when you approve a card that names it.'),
    h('dl', { class: 'mb-3 grid gap-x-4 gap-y-1 text-sm sm:grid-cols-[9rem_1fr]' },
      h('dt', { class: 'muted' }, 'Isolation'), h('dd', { id: 'sandbox-runtime', class: 'min-w-0 break-words' }, sb.runtime),
      h('dt', { class: 'muted' }, 'Network'), h('dd', { class: 'min-w-0' }, net, sb.network_note && sb.network_note !== sb.network ? h('span', { class: 'muted ml-2 text-xs' }, 'uses ' + sb.network_note) : null),
      h('dt', { class: 'muted' }, 'Docker'), h('dd', {}, sb.docker ? 'available' : 'not available on this server')),
    notice('warn', 'A container shares the host\'s kernel unless the host runs gVisor, and Forgeline cannot stop it from reaching other machines on the network without firewall rules on the host: a service that listens beyond this machine\'s loopback address may be reachable from it. Set FORGELINE_SANDBOX_NETWORK to a Docker network you have fenced, or choose No network.'),
    sb.containers.length ? h('ul', { class: 'mt-3 space-y-1 text-sm', id: 'sandbox-containers' }, sb.containers.map(c => h('li', {}, h('code', { class: 'font-mono text-xs' }, c.image), h('span', { class: 'muted ml-2' }, c.status)))) : null,
    h('div', { class: 'mt-3' }, h('button', { type: 'button', class: 'btn-ghost btn-sm', id: 'sandbox-reset', onclick: async () => {
      if (!await confirmDialog({ title: 'Reset your sandbox?', body: 'Your sandbox containers and everything in their /work folder are deleted.', confirmLabel: 'Reset' })) return;
      try { await post('/v1/assistant/sandbox/reset'); toast('Your sandbox was reset.', 'ok'); } catch (e) { toast(e.message, 'error'); }
      reload();
    } }, icon('trash', 'size-4'), 'Reset my sandbox')));
}
