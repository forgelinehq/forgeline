// Settings › Image builds (design note 043): push rules say which repository's branches may push built images to
// which ECR repository, as which connected AWS account. Any run may build; only a run GitHub started for a push to an
// allowed branch may push, and only to its rule's repository.
import { h, icon, mount, pageHeader, spinner, errorBox, notice, toast, modal, field, emptyState, relTime, confirmDialog } from '../lib/dom.js';
import { get, put, del } from '../lib/api.js';

export async function buildRulesPage() {
  const el = h('div', {});
  let accounts = [];
  const intro = 'Build steps make images from a run’s files. Any run may build; a push needs a rule here, and only runs GitHub starts for a push to an allowed branch use it. Pull requests and console runs never push.';
  async function load() {
    mount(el, pageHeader('Image builds', intro), spinner());
    let rules;
    try {
      [rules, accounts] = await Promise.all([get('/v1/build-push-rules').then(r => r.rules), get('/v1/aws/accounts').then(r => r.accounts || []).catch(() => [])]);
    } catch (e) { mount(el, pageHeader('Image builds', ''), errorBox(e.message)); return; }
    mount(el, pageHeader('Image builds', intro,
      h('button', { class: 'btn-primary', type: 'button', id: 'rule-add', onclick: () => form(null) }, icon('plus', 'size-4'), 'Add push rule')),
      notice('info', 'Pushing an image changes nothing that runs: deploying it still needs an approval. The push credential is a fresh ECR token that never enters a build container or a log.'),
      rules.length ? h('div', { class: 'card mt-4 divide-y divide-slate-100 dark:divide-slate-800', id: 'rule-list' }, rules.map(row))
        : emptyState('box', 'No push rules yet', 'Without a rule, build steps build but do not push. Add one for each repository and branch that should publish an image.'));
  }

  function accountName(id) {
    const a = accounts.find(x => x.id === id);
    return a ? a.name + (a.account_id ? ' (' + a.account_id + ')' : '') : id;
  }

  function row(r) {
    return h('div', { class: 'flex flex-wrap items-start justify-between gap-3 px-5 py-3', 'data-rule': r.id },
      h('div', { class: 'min-w-0' },
        h('div', { class: 'flex flex-wrap items-center gap-2' }, h('span', { class: 'font-medium' }, r.repo), h('span', { class: 'muted text-sm' }, 'branches'),
          r.branches.map(b => h('code', { class: 'pill-idle font-mono' }, b))),
        h('p', { class: 'mt-1 break-all font-mono text-xs' }, '→ ' + r.target),
        h('p', { class: 'muted mt-0.5 text-xs' }, `pushes as ${accountName(r.account)} · rule ${r.id} · changed by ${r.updated_by} ${relTime(r.updated_at)}`)),
      h('div', { class: 'flex gap-2' },
        h('button', { class: 'btn-secondary btn-sm', type: 'button', onclick: () => form(r) }, icon('edit', 'size-3.5'), 'Change'),
        h('button', { class: 'btn-ghost btn-sm text-rose-600 dark:text-rose-400', type: 'button', 'aria-label': 'Remove rule ' + r.id, title: 'Remove', onclick: () => remove(r) }, icon('trash', 'size-3.5'))));
  }

  async function remove(r) {
    if (!await confirmDialog({ title: 'Remove push rule ' + r.id + '?', body: 'Builds from ' + r.repo + ' stop pushing to ' + r.target + '. Images already there stay.', confirmLabel: 'Remove', danger: true })) return;
    try { await del('/v1/build-push-rules/' + encodeURIComponent(r.id)); toast('Removed', 'ok'); } catch (e) { toast(e.message, 'error'); }
    load();
  }

  function form(r) {
    modal(r ? 'Change push rule ' + r.id : 'Add a push rule', (close) => {
      const v = (id, val, attrs = {}) => h('input', { id, class: 'input', value: val ?? '', ...attrs });
      const id = v('rule-id', r?.id, { placeholder: 'web-main', disabled: !!r });
      const repo = v('rule-repo', r?.repo, { placeholder: 'owner/name', class: 'input font-mono' });
      const branches = v('rule-branches', (r?.branches || ['main']).join(', '), { class: 'input font-mono' });
      const target = v('rule-target', r?.target, { placeholder: '123456789012.dkr.ecr.ap-south-1.amazonaws.com/web', class: 'input font-mono' });
      const acct = h('select', { id: 'rule-account', class: 'input' }, accounts.length ? accounts.map(a => h('option', { value: a.id, selected: (r?.account || 'primary') === a.id }, accountName(a.id)))
        : h('option', { value: 'primary' }, 'Primary'));
      const err = h('div', {});
      const save = h('button', { class: 'btn-primary', type: 'button', id: 'rule-save', onclick: async () => {
        const body = { repo: repo.value.trim(), branches: branches.value.split(',').map(b => b.trim()).filter(Boolean), target: target.value.trim(), account: acct.value };
        try {
          await put('/v1/build-push-rules/' + encodeURIComponent(id.value.trim()), body);
          close(); toast('Saved', 'ok'); load();
        } catch (e) { mount(err, errorBox(e.message)); }
      } }, r ? 'Save' : 'Add');
      return h('div', { class: 'space-y-4' },
        h('div', { class: 'grid grid-cols-2 gap-2' }, field('Id', id, 'Lower-case letters, digits and dashes'), field('Repository', repo, 'The GitHub repository whose pushes build it')),
        field('Branches', branches, 'Comma separated, such as main or release/*. A bare * is not allowed.'),
        field('Push to', target, 'The ECR repository, without a tag. Build steps must name exactly this.'),
        field('As AWS account', acct, 'Forgeline asks this account for a short-lived ECR token for each push.'),
        err,
        h('div', { class: 'flex justify-end gap-2' }, h('button', { class: 'btn-secondary', type: 'button', onclick: () => close() }, 'Cancel'), save));
    }, { wide: true });
  }

  await load();
  return el;
}
