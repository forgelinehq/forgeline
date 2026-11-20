// Pushing an agent to a registry (design note 046, step 3). An administrator proposes a push of one version; a
// different administrator reads the exact Docker Agent file and reference here and approves it. Only then does
// anything leave Forgeline.
import { h, icon, mount, pageHeader, relTime, absTime, pill, spinner, errorBox, notice, toast, confirmDialog, drawer, field, copyButton } from '../lib/dom.js';
import { approvalView } from '../lib/approval.js';
import { get, post, atLeast } from '../lib/api.js';
import { navigate } from '../lib/router.js';
import { session } from '../app.js';

const STATUS_LABEL = { proposed: 'waiting', pushing: 'pushing', pushed: 'pushed', failed: 'failed', rejected: 'rejected', cancelled: 'withdrawn', expired: 'expired' };

/** The drawer behind an agent's "Push to a registry" button. */
export async function pushDrawer(ag, v) {
  let regs;
  try { regs = (await get('/v1/ai/registries')).registries; } catch (e) { toast(e.message, 'error'); return; }
  await drawer('Push ' + ag.name + ' v' + v.n, (close) => {
    if (!regs.length) {
      return h('div', { class: 'space-y-3', id: 'push-no-registry' },
        notice('info', 'No registry is set up yet. Add one under Settings › Agent registries (GitHub, Docker Hub or any OCI registry), then come back.'),
        h('a', { class: 'btn-secondary', href: '#/registries', onclick: () => close() }, icon('plus', 'size-4'), 'Add a registry'));
    }
    const sel = h('select', { id: 'push-registry', class: 'input' }, regs.map(g => h('option', { value: g.id }, g.name + ' · ' + g.host + (g.namespace ? '/' + g.namespace : ''))));
    const repo = h('input', { id: 'push-repo', class: 'input font-mono', placeholder: ag.id, maxlength: '200', spellcheck: 'false' });
    const reason = h('textarea', { id: 'push-reason', class: 'input', rows: '3', maxlength: '800', placeholder: 'Who is it for, and why now? The approver reads this.' });
    const ref = h('code', { class: 'block break-all rounded-lg bg-slate-100 px-3 py-2 text-xs dark:bg-slate-800', id: 'push-ref' });
    const out = h('div', {});
    const show = () => {
      const g = regs.find(x => x.id === sel.value);
      const name = (repo.value.trim() || ag.id).replace(/^\/+|\/+$/g, '');
      ref.textContent = g.host + '/' + (g.namespace ? g.namespace + '/' : '') + name + ':v' + v.n;
    };
    sel.addEventListener('change', show); repo.addEventListener('input', show); show();
    const go = h('button', { class: 'btn-primary', type: 'button', id: 'push-propose', onclick: async () => {
      go.disabled = true;
      try {
        const p = await post('/v1/ai/agents/' + encodeURIComponent(ag.id) + '/push', { registry: sel.value, repository: repo.value.trim(), version: v.n, reason: reason.value.trim() });
        toast('Proposed. Another administrator approves it under Approvals.', 'ok');
        close(); navigate('/pushes/' + p.id);
      } catch (e) { mount(out, errorBox(e.message)); go.disabled = false; }
    } }, icon('upload', 'size-4'), 'Propose the push');
    return h('div', { class: 'space-y-4' },
      h('p', { class: 'muted text-sm' }, 'This sends the Docker Agent file of version ' + v.n + ' to a registry, the way docker agent share push does, so anyone with access can run it with docker agent run. Nothing is sent until a different administrator approves the exact file and address.'),
      field('Registry', sel, null, 'push-registry'),
      field('Repository', repo, 'Lower-case letters, digits, and . _ - separated by /. Empty uses the agent\'s id.', 'push-repo'),
      h('div', {}, h('div', { class: 'label' }, 'It will be pushed as'), ref),
      field('Why', reason, '10 to 800 characters.', 'push-reason'),
      out,
      h('div', { class: 'flex justify-end gap-2' }, h('button', { class: 'btn-secondary', type: 'button', onclick: () => close() }, 'Cancel'), go));
  }, { subtitle: 'A proposal: a different administrator approves it.' });
}

/** The pushes card on the Approvals list. Viewers cannot read agents, so it is left out for them. */
export async function pushesCard() {
  if (!atLeast(session.role, 'developer')) return null;
  let ps;
  try { ps = (await get('/v1/ai/pushes')).pushes; } catch { return null; }
  if (!ps.length) return null;
  return h('section', { class: 'card mt-6 overflow-x-auto', id: 'pushes' },
    h('div', { class: 'border-b border-slate-200 px-5 py-3 dark:border-slate-800' }, h('h2', { class: 'h-section' }, 'Agent pushes'),
      h('p', { class: 'muted text-xs' }, 'An agent version sent to a registry. Nothing is sent until a different administrator approves the exact file.')),
    h('table', { class: 'tbl' },
      h('thead', {}, h('tr', {}, ['Agent', 'To', 'Status', 'Proposed by', 'When'].map(x => h('th', {}, x)))),
      h('tbody', {}, ps.map(p => h('tr', { 'data-push': p.id },
        h('td', {}, h('a', { href: '#/pushes/' + p.id, class: 'font-medium text-brand-700 hover:underline dark:text-brand-200' }, p.agent_name + ' v' + p.version)),
        h('td', { class: 'break-all font-mono text-xs' }, p.ref),
        h('td', {}, pill(p.status, STATUS_LABEL[p.status])),
        h('td', { class: 'text-sm' }, p.proposed_by),
        h('td', { class: 'muted text-sm', title: absTime(p.proposed_at) }, relTime(p.proposed_at)))))));
}

/** One push: what will be sent, where, and the approval. */
export async function pushPage({ params }) {
  const el = h('div', {});
  async function load() {
    let p;
    try { p = await get('/v1/ai/pushes/' + encodeURIComponent(params.id)); } catch (e) { return mount(el, errorBox(e.message)); }
    const admin = atLeast(session.role, 'admin');
    const mine = p.proposed_by === session.name;
    const waiting = p.status === 'proposed';
    const av = approvalView(p.approval, session.name, p.proposed_by, 'push');
    const note = h('input', { id: 'push-note', class: 'input', placeholder: 'Optional note for the record', 'aria-label': 'Note' });
    const act = async (what, body) => {
      try { await post('/v1/ai/pushes/' + p.id + '/' + what, body); toast(what === 'approve' ? (av.last ? 'Approved' : 'Your approval is recorded. It needs another administrator.') : 'Done', 'ok'); } catch (e) { toast(e.message, 'error'); }
      load();
    };
    const approve = h('button', { class: 'btn-primary', type: 'button', id: 'approve-push', disabled: !av.may, title: av.why || null, onclick: async () => {
      const ok = await confirmDialog(av.last
        ? { title: 'Approve and push?', body: 'Forgeline sends exactly the file shown to ' + p.ref + '. Anyone who can read that repository can run the agent.', confirmLabel: 'Approve and push' }
        : { title: 'Approve this push?', body: 'Your approval is recorded. Nothing is sent until another administrator approves the same file.', confirmLabel: 'Approve' });
      if (ok) act('approve', { hash: p.hash, note: note.value });
    } }, icon('upload', 'size-4'), av.label);
    const row = (label, ...kids) => h('div', { class: 'grid gap-1 py-2 text-sm sm:grid-cols-[9rem_1fr] sm:gap-3' }, h('div', { class: 'muted' }, label), h('div', { class: 'min-w-0 break-words' }, ...kids));
    mount(el,
      h('nav', { class: 'mb-2 text-sm', 'aria-label': 'Breadcrumb' }, h('a', { href: '#/proposals', class: 'muted hover:underline' }, 'Approvals'), h('span', { class: 'muted' }, ' › '), h('span', {}, 'Push ' + p.id)),
      pageHeader('Push ' + p.agent_name + ' v' + p.version, 'Proposed by ' + p.proposed_by + ' · ' + relTime(p.proposed_at), pill(p.status, STATUS_LABEL[p.status])),
      p.status === 'failed' && h('div', { class: 'mb-4', id: 'push-failed' }, errorBox('The push did not complete: ' + p.result)),
      p.status === 'pushed' && h('div', { class: 'mb-4 space-y-2', id: 'push-done' }, notice('ok', 'Pushed. Run it with: ', h('code', { class: 'text-xs' }, 'docker agent run ' + p.ref)),
        h('div', { class: 'flex flex-wrap gap-2' }, copyButton('docker agent run ' + p.ref, 'Copy the command'))),
      h('section', { class: 'card card-pad mb-4' },
        row('Agent', h('a', { class: 'font-medium text-brand-700 hover:underline dark:text-brand-200', href: '#/agents/' + p.agent }, p.agent_name), h('span', { class: 'muted' }, ' version ' + p.version)),
        row('To', h('code', { class: 'text-xs', id: 'push-target' }, p.ref), h('span', { class: 'muted block text-xs' }, 'Registry ' + p.registry_name)),
        row('Why', h('span', { class: 'whitespace-pre-wrap' }, p.reason)),
        p.left_out.length ? row('Stays behind', h('ul', { class: 'space-y-1' }, p.left_out.map(l => h('li', {}, h('code', { class: 'text-xs' }, l.tool), h('span', { class: 'muted block text-xs' }, l.why))))) : null,
        p.digest ? row('Digest', h('code', { class: 'break-all text-xs' }, p.digest)) : null),
      h('section', { class: 'card card-pad mb-4' }, h('h2', { class: 'h-section mb-2' }, 'The exact file'),
        h('p', { class: 'muted mb-2 text-xs' }, 'No key or secret is in it: it names the environment variables to set.'),
        h('pre', { class: 'max-h-[28rem] overflow-auto rounded-lg bg-slate-950 p-3 font-mono text-xs text-slate-100', id: 'push-yaml' }, p.yaml),
        h('p', { class: 'muted mt-2 break-all font-mono text-[11px]' }, 'sha256 ' + p.hash)),
      waiting && av.line ? h('div', { class: 'mb-4', id: 'approval-state' }, notice('info', av.line + '.')) : null,
      waiting && admin && !mine && !av.may ? h('div', { class: 'mb-4' }, notice('warn', av.why)) : null,
      waiting && (admin
        ? (mine ? h('div', { class: 'mb-4' }, notice('info', 'You proposed this, so someone else must approve it.'))
          : h('section', { class: 'card card-pad mb-4' }, h('div', { class: 'flex flex-wrap items-end gap-3' }, h('div', { class: 'min-w-64 flex-1' }, note), approve,
            h('button', { class: 'btn-secondary', type: 'button', id: 'reject-push', onclick: () => act('reject', {}) }, 'Reject'))))
        : h('div', { class: 'mb-4' }, notice('info', 'An administrator will review this.'))),
      waiting && (mine || admin) && h('button', { class: 'btn-ghost btn-sm', type: 'button', id: 'cancel-push', onclick: () => act('cancel', {}) }, 'Withdraw this push'),
      p.decided_by && h('p', { class: 'muted mb-4 text-sm' }, (p.status === 'rejected' ? 'Rejected by ' : p.status === 'cancelled' ? 'Withdrawn by ' : 'Approved by ') + p.decided_by + (p.note ? ' — ' + p.note : '')));
  }
  mount(el, spinner());
  await load();
  return el;
}
