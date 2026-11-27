// Runbooks (design note 051): scripts a developer or administrator writes and a different administrator reviews, run on servers
// through SSM. A check (it only reads) runs when asked; a fix waits for a different administrator each time.
import { h, icon, mount, pageHeader, relTime, absTime, pill, spinner, errorBox, notice, toast, confirmDialog, drawer, field, emptyState } from '../lib/dom.js';
import { chipInput, choiceCards } from '../lib/controls.js';
import { approvalView } from '../lib/approval.js';
import { get, post, atLeast } from '../lib/api.js';
import { navigate } from '../lib/router.js';
import { session } from '../app.js';

const STATUS_LABEL = { proposed: 'waiting', running: 'running', succeeded: 'succeeded', failed: 'failed', rejected: 'rejected', cancelled: 'withdrawn', expired: 'expired' };
const KIND = {
  check: { label: 'Check', icon: 'health', note: 'Only reads: disk use, folder sizes, backup ages. Runs when asked, by people and agents.' },
  fix: { label: 'Fix', icon: 'wand', note: 'Changes something: prune Docker, delete old backups. A different administrator approves every run.' },
};

const kindPill = (k) => h('span', { class: 'inline-flex items-center gap-1 rounded-full px-2 py-0.5 text-xs font-medium ' +
  (k === 'fix' ? 'bg-amber-100 text-amber-800 dark:bg-amber-500/15 dark:text-amber-200' : 'bg-sky-100 text-sky-800 dark:bg-sky-500/15 dark:text-sky-200') },
icon(KIND[k]?.icon || 'tool', 'size-3'), KIND[k]?.label || k);

const targetText = (t) => t.instances?.length ? t.instances.join(', ') : 'running instances tagged ' + t.tag_key + '=' + t.tag_value;

/** The list: every runbook, and the examples to start from. */
export async function runbooksPage() {
  const el = h('div', {});
  let d;
  try { d = await get('/v1/runbooks'); } catch (e) { mount(el, errorBox(e.message)); return el; }
  const admin = atLeast(session.role, 'admin');
  const writer = atLeast(session.role, 'developer');
  const add = writer && h('button', { class: 'btn-primary', type: 'button', id: 'new-runbook', onclick: () => editDrawer(null, null) }, icon('plus', 'size-4'), 'New runbook');
  const list = d.runbooks.length
    ? h('section', { class: 'card overflow-x-auto', id: 'runbooks' }, h('table', { class: 'tbl' },
      h('thead', {}, h('tr', {}, ['Runbook', 'Kind', 'Runs on', 'Version', ''].map(x => h('th', {}, x)))),
      h('tbody', {}, d.runbooks.map(rb => {
        const v = rb.versions.find(x => x.n === rb.current) || rb.versions[rb.versions.length - 1];
        const waiting = rb.versions.some(x => !x.review && x.n > rb.current);
        return h('tr', { 'data-runbook': rb.id },
          h('td', {}, h('a', { href: '#/runbooks/' + rb.id, class: 'font-medium text-brand-700 hover:underline dark:text-brand-200' }, rb.name), h('div', { class: 'muted text-xs' }, rb.description)),
          h('td', {}, kindPill(rb.kind)),
          h('td', { class: 'font-mono text-xs' }, targetText(v.target)),
          h('td', { class: 'text-sm' }, rb.current ? 'v' + rb.current : h('span', { class: 'muted' }, 'none reviewed')),
          h('td', {}, waiting ? pill('proposed', 'waits for review') : null));
      }))))
    : emptyState('steps', 'No runbooks yet', 'A runbook is a script that fixes or checks something on your servers, run through AWS Systems Manager. Start from an example below.', add || null);
  const examples = h('section', { class: 'mt-6', id: 'runbook-examples' },
    h('h2', { class: 'h-section mb-1' }, 'Examples'),
    h('p', { class: 'muted mb-3 text-sm' }, 'For a build server whose disk keeps filling up. Read them, change them, and have another administrator review them; none is installed by itself.'),
    h('div', { class: 'grid gap-3 md:grid-cols-3' }, d.examples.map(ex => h('div', { class: 'card card-pad flex flex-col gap-2', 'data-example': ex.name },
      h('div', { class: 'flex items-center justify-between gap-2' }, h('span', { class: 'font-medium' }, ex.name), kindPill(ex.kind)),
      h('p', { class: 'muted flex-1 text-sm' }, ex.description),
      writer ? h('button', { class: 'btn-secondary btn-sm self-start', type: 'button', onclick: () => editDrawer(null, ex) }, icon('copy', 'size-4'), 'Start from this') : null))));
  mount(el,
    pageHeader('Runbooks', 'Scripts that check or fix things on your servers through AWS Systems Manager. Agents choose a runbook and its values; they never write commands.', add || null),
    notice('info', 'A developer or an administrator writes a runbook and a different administrator reviews it. Checks then run when asked; every fix waits for a different administrator to approve it.'),
    h('div', { class: 'mt-4' }, list), examples);
  return el;
}

/** Make a runbook, or a new version of one. `from` is an example or the current version to start from. */
async function editDrawer(rb, from) {
  const v = rb ? (rb.versions.find(x => x.n === rb.current) || rb.versions[rb.versions.length - 1]) : null;
  const src = from || v || {};
  let kind = rb?.kind || from?.kind || 'check';
  let params = JSON.parse(JSON.stringify(src.params || []));
  let instances = [...(src.target?.instances || [])];
  await drawer(rb ? 'New version of ' + rb.name : 'New runbook', (close) => {
    const name = h('input', { id: 'rb-name', class: 'input', maxlength: '80', value: from?.name || '' });
    const desc = h('input', { id: 'rb-desc', class: 'input', maxlength: '300', value: from?.description || '' });
    const script = h('textarea', { id: 'rb-script', class: 'input font-mono text-xs', rows: '14', maxlength: '8000', spellcheck: 'false' });
    script.value = src.script || '#!/bin/bash\nset -eu\n';
    const tagKey = h('input', { id: 'rb-tag-key', class: 'input font-mono', placeholder: 'forgeline-runbooks', value: src.target?.tag_key || (instances.length ? '' : 'forgeline-runbooks') });
    const tagVal = h('input', { id: 'rb-tag-value', class: 'input font-mono', placeholder: 'jenkins', value: src.target?.tag_value || '' });
    const timeout = h('input', { id: 'rb-timeout', class: 'input', type: 'number', min: '30', max: '3600', value: String(src.timeout_seconds || 300) });
    const note = h('input', { id: 'rb-note', class: 'input', maxlength: '300', placeholder: 'What changed, for the reviewer' });
    const paramsBox = h('div', { class: 'space-y-2', id: 'rb-params' });
    const drawParams = () => mount(paramsBox, params.length ? params.map((p, i) => paramRow(p, () => { params.splice(i, 1); drawParams(); })) : h('p', { class: 'muted text-sm' }, 'No parameters: the script always runs the same way.'),
      params.length < 8 ? h('button', { class: 'btn-ghost btn-sm', type: 'button', id: 'rb-add-param', onclick: () => { params.push({ name: '', type: 'choice', choices: [] }); drawParams(); } }, icon('plus', 'size-4'), 'Add a parameter') : null);
    drawParams();
    const out = h('div', {});
    const save = h('button', { class: 'btn-primary', type: 'button', id: 'rb-save', onclick: async () => {
      save.disabled = true;
      const body = { script: script.value, params: params.map(cleanParam), timeout_seconds: Number(timeout.value) || 300, note: note.value.trim(),
        target: instances.length ? { instances } : { tag_key: tagKey.value.trim(), tag_value: tagVal.value.trim() } };
      try {
        const saved = rb
          ? await post('/v1/runbooks/' + encodeURIComponent(rb.id) + '/versions', body)
          : await post('/v1/runbooks', { ...body, name: name.value.trim(), description: desc.value.trim(), kind });
        toast('Saved. A different administrator reviews it before it runs.', 'ok');
        close(); navigate('/runbooks/' + saved.id);
      } catch (e) { mount(out, errorBox(e.message)); save.disabled = false; }
    } }, icon('check', 'size-4'), 'Save for review');
    return h('div', { class: 'space-y-4' },
      rb ? null : field('Name', name, null, 'rb-name'),
      rb ? null : field('What it does', desc, 'Agents read this to choose a runbook.', 'rb-desc'),
      rb ? null : h('div', {}, h('div', { class: 'label' }, 'Kind'), choiceCards({ id: 'rb-kind', value: kind, options: ['check', 'fix'].map(k => ({ value: k, ...KIND[k] })), onchange: (k) => { kind = k; } })),
      field('Script', script, 'Runs as root through AWS-RunShellScript. Parameters arrive as environment variables; quote them ("$NAME").', 'rb-script'),
      h('div', {}, h('div', { class: 'label' }, 'Parameters'), h('p', { class: 'hint mb-2' }, 'A choice from a list, or a whole number in a range. Never free text, so nothing an agent writes reaches the shell as code.'), paramsBox),
      h('div', {}, h('div', { class: 'label' }, 'Runs on'),
        chipInput({ id: 'rb-instances', values: instances, placeholder: 'i-0123456789abcdef0', hint: 'Instance ids, or leave empty and use a tag. The instances need the SSM agent.', max: 20, onchange: (vs) => { instances = vs; } }),
        h('div', { class: 'mt-2 grid gap-2 sm:grid-cols-2' }, field('Tag key', tagKey, null, 'rb-tag-key'), field('Tag value', tagVal, null, 'rb-tag-value'))),
      field('Time limit (seconds)', timeout, '30 to 3600.', 'rb-timeout'),
      field('Note', note, null, 'rb-note'),
      out,
      h('div', { class: 'flex justify-end gap-2' }, h('button', { class: 'btn-secondary', type: 'button', onclick: () => close() }, 'Cancel'), save));
  }, { subtitle: 'Saved versions wait for a different administrator to review them.' });
}

function cleanParam(p) {
  const o = { name: String(p.name || '').trim().toUpperCase(), label: String(p.label || '').trim(), type: p.type, default: String(p.default ?? '').trim() };
  if (p.type === 'choice') o.choices = p.choices || [];
  else { o.min = Number(p.min) || 0; o.max = Number(p.max) || 0; }
  return o;
}

function paramRow(p, remove) {
  const name = h('input', { class: 'input font-mono', placeholder: 'BACKUP_DIR', 'aria-label': 'Variable name', value: p.name || '', oninput: (e) => { p.name = e.target.value; } });
  const label = h('input', { class: 'input', placeholder: 'Backup folder', 'aria-label': 'Label', value: p.label || '', oninput: (e) => { p.label = e.target.value; } });
  const type = h('select', { class: 'input', 'aria-label': 'Type', onchange: (e) => { p.type = e.target.value; draw(); } },
    h('option', { value: 'choice', selected: p.type === 'choice' }, 'Choice'), h('option', { value: 'int', selected: p.type === 'int' }, 'Number'));
  const def = h('input', { class: 'input', placeholder: 'Default', 'aria-label': 'Default', value: p.default || '', oninput: (e) => { p.default = e.target.value; } });
  const body = h('div', {});
  const draw = () => mount(body, p.type === 'choice'
    ? chipInput({ id: 'rb-choices-' + Math.random().toString(36).slice(2, 8), values: p.choices || [], placeholder: '/var/backups/db', hint: 'The values a run may pick. Letters, digits, spaces and . _ / : @ + - only.', onchange: (vs) => { p.choices = vs; } })
    : h('div', { class: 'grid grid-cols-2 gap-2' },
      h('input', { class: 'input', type: 'number', min: '0', placeholder: 'From', 'aria-label': 'From', value: String(p.min ?? 0), oninput: (e) => { p.min = e.target.value; } }),
      h('input', { class: 'input', type: 'number', min: '0', placeholder: 'To', 'aria-label': 'To', value: String(p.max ?? 10), oninput: (e) => { p.max = e.target.value; } })));
  draw();
  return h('div', { class: 'space-y-2 rounded-xl border border-slate-200 p-3 dark:border-slate-700', 'data-param': p.name || 'new' },
    h('div', { class: 'grid gap-2 sm:grid-cols-[1fr_1fr_7rem_6rem_auto]' }, name, label, type, def,
      h('button', { class: 'btn-ghost btn-sm', type: 'button', 'aria-label': 'Remove parameter', onclick: remove }, icon('trash', 'size-4'))),
    body);
}

/** One runbook: its versions and reviews, a form to run it, and its runs. */
export async function runbookPage({ params }) {
  const el = h('div', {});
  async function load() {
    let rb, runs;
    try { [rb, runs] = await Promise.all([get('/v1/runbooks/' + encodeURIComponent(params.id)), get('/v1/runbook-runs?runbook=' + encodeURIComponent(params.id))]); } catch (e) { return mount(el, errorBox(e.message)); }
    const admin = atLeast(session.role, 'admin');
    const cur = rb.versions.find(x => x.n === rb.current);
    const pending = rb.versions.filter(x => !x.review && x.n > rb.current);
    mount(el,
      h('nav', { class: 'mb-2 text-sm', 'aria-label': 'Breadcrumb' }, h('a', { href: '#/runbooks', class: 'muted hover:underline' }, 'Runbooks'), h('span', { class: 'muted' }, ' › '), h('span', {}, rb.name)),
      pageHeader(rb.name, rb.description, kindPill(rb.kind), atLeast(session.role, 'developer') ? h('button', { class: 'btn-secondary', type: 'button', id: 'rb-new-version', onclick: () => editDrawer(rb, null) }, icon('edit', 'size-4'), 'New version') : null),
      pending.map(v => reviewCard(rb, v, admin, load)),
      cur ? runCard(rb, cur, load) : h('div', { class: 'mb-4' }, notice('warn', 'No version has been reviewed yet, so this runbook cannot run.')),
      cur ? versionCard(cur, 'What runs now: v' + cur.n) : null,
      runsTable(runs.runs));
  }
  mount(el, spinner());
  await load();
  return el;
}

function versionCard(v, title) {
  return h('section', { class: 'card card-pad mb-4', 'data-version': String(v.n) },
    h('div', { class: 'mb-2 flex flex-wrap items-baseline justify-between gap-2' }, h('h2', { class: 'h-section' }, title),
      h('span', { class: 'muted text-xs' }, 'by ' + v.by + ' · ' + relTime(v.at) + (v.review ? ' · reviewed by ' + v.review.by : ''))),
    v.note ? h('p', { class: 'mb-2 text-sm' }, v.note) : null,
    h('dl', { class: 'mb-3 grid gap-x-4 gap-y-1 text-sm sm:grid-cols-[9rem_1fr]' },
      h('dt', { class: 'muted' }, 'Runs on'), h('dd', { class: 'font-mono text-xs' }, targetText(v.target)),
      h('dt', { class: 'muted' }, 'Time limit'), h('dd', {}, v.timeout_seconds + ' seconds'),
      v.params.length ? [h('dt', { class: 'muted' }, 'Parameters'), h('dd', {}, h('ul', { class: 'space-y-0.5' }, v.params.map(p => h('li', {}, h('code', { class: 'text-xs' }, p.name), ' ',
        h('span', { class: 'muted text-xs' }, p.type === 'choice' ? 'one of ' + p.choices.join(', ') : p.min + ' to ' + p.max, p.default ? ' · default ' + p.default : '')))))] : null),
    h('pre', { class: 'max-h-[24rem] overflow-auto rounded-lg bg-slate-950 p-3 font-mono text-xs text-slate-100' }, v.script),
    h('p', { class: 'muted mt-2 break-all font-mono text-[11px]' }, 'sha256 ' + v.hash));
}

function reviewCard(rb, v, admin, reload) {
  const mine = v.by === session.name;
  const note = h('input', { class: 'input', id: 'rb-review-note', placeholder: 'Optional note for the record', 'aria-label': 'Review note' });
  const btn = h('button', { class: 'btn-primary', type: 'button', id: 'rb-review', onclick: async () => {
    const ok = await confirmDialog({ title: 'Approve v' + v.n + '?', body: rb.kind === 'check'
      ? 'You confirm this script only reads. Once approved, people and agents can run it without asking each time.'
      : 'Once approved, this script is what runs when a fix is approved. Every run still needs a different administrator.', confirmLabel: 'Approve this version' });
    if (!ok) return;
    try { await post('/v1/runbooks/' + encodeURIComponent(rb.id) + '/review', { version: v.n, hash: v.hash, note: note.value }); toast('Reviewed. v' + v.n + ' is what runs now.', 'ok'); } catch (e) { toast(e.message, 'error'); }
    reload();
  } }, icon('check', 'size-4'), 'Approve v' + v.n);
  return h('div', { class: 'mb-4 rounded-2xl border border-amber-300 bg-amber-50/60 p-1 dark:border-amber-500/40 dark:bg-amber-500/5', id: 'rb-pending-' + v.n },
    versionCard(v, 'v' + v.n + ' waits for review'),
    h('div', { class: 'px-4 pb-3' }, !admin ? notice('info', 'An administrator other than ' + v.by + ' reviews it.')
      : mine ? notice('info', 'You wrote this version, so a different administrator reviews it.')
        : h('div', { class: 'flex flex-wrap items-end gap-3' }, h('div', { class: 'min-w-64 flex-1' }, note), btn)));
}

function runCard(rb, v, reload) {
  const vals = {};
  const inputs = v.params.map(p => {
    vals[p.name] = p.default || (p.type === 'choice' ? p.choices[0] : String(p.min));
    const ctl = p.type === 'choice'
      ? h('select', { class: 'input', id: 'rb-run-' + p.name, onchange: (e) => { vals[p.name] = e.target.value; } }, p.choices.map(c => h('option', { value: c, selected: c === vals[p.name] }, c)))
      : h('input', { class: 'input', type: 'number', id: 'rb-run-' + p.name, min: String(p.min), max: String(p.max), value: vals[p.name], oninput: (e) => { vals[p.name] = e.target.value; } });
    return field(p.label || p.name, ctl, p.type === 'int' ? p.min + ' to ' + p.max : null, 'rb-run-' + p.name);
  });
  const reason = rb.kind === 'fix' ? h('textarea', { class: 'input', id: 'rb-run-reason', rows: '2', maxlength: '800', placeholder: 'Why now? The approver reads this.' }) : null;
  const out = h('div', {});
  const go = h('button', { class: 'btn-primary', type: 'button', id: 'rb-run', onclick: async () => {
    go.disabled = true;
    try {
      const run = await post('/v1/runbooks/' + encodeURIComponent(rb.id) + '/run', { params: vals, reason: reason?.value.trim() || '' });
      toast(rb.kind === 'fix' ? 'Filed. A different administrator approves it under Approvals.' : 'Started.', 'ok');
      navigate('/runbook-runs/' + run.id);
    } catch (e) { mount(out, errorBox(e.message)); go.disabled = false; }
  } }, icon(rb.kind === 'fix' ? 'send' : 'play', 'size-4'), rb.kind === 'fix' ? 'Ask for this fix' : 'Run the check');
  return h('section', { class: 'card card-pad mb-4', id: 'rb-run-card' },
    h('h2', { class: 'h-section mb-1' }, rb.kind === 'fix' ? 'Ask for a fix' : 'Run it'),
    h('p', { class: 'muted mb-3 text-sm' }, rb.kind === 'fix' ? 'Nothing runs until a different administrator approves the exact script, values and instances.' : 'Runs now on ' + targetText(v.target) + '.'),
    inputs.length ? h('div', { class: 'mb-3 grid gap-3 sm:grid-cols-2' }, inputs) : null,
    reason ? field('Why', reason, '10 to 800 characters.', 'rb-run-reason') : null,
    out, h('div', { class: 'mt-3 flex justify-end' }, go));
}

function runsTable(runs, title = 'Runs') {
  if (!runs.length) return null;
  return h('section', { class: 'card overflow-x-auto', id: 'rb-runs' },
    h('div', { class: 'border-b border-slate-200 px-5 py-3 dark:border-slate-800' }, h('h2', { class: 'h-section' }, title)),
    h('table', { class: 'tbl' },
      h('thead', {}, h('tr', {}, ['Run', 'Status', 'Values', 'Asked by', 'When'].map(x => h('th', {}, x)))),
      h('tbody', {}, runs.map(r => h('tr', { 'data-run': r.id },
        h('td', {}, h('a', { href: '#/runbook-runs/' + r.id, class: 'font-medium text-brand-700 hover:underline dark:text-brand-200' }, r.runbook_name + ' v' + r.version), r.via_ai ? h('span', { class: 'muted ml-1 text-xs' }, 'by an agent') : null),
        h('td', {}, pill(r.status, STATUS_LABEL[r.status])),
        h('td', { class: 'font-mono text-xs' }, Object.entries(r.params || {}).map(([k, v]) => k + '=' + v).join(' ')),
        h('td', { class: 'text-sm' }, r.by),
        h('td', { class: 'muted text-sm', title: absTime(r.at) }, relTime(r.at)))))));
}

/** The fixes waiting, on the Approvals list. */
export async function runbookFixesCard() {
  if (!atLeast(session.role, 'developer')) return null;
  let rs;
  try { rs = (await get('/v1/runbook-runs')).runs.filter(r => r.kind === 'fix'); } catch { return null; }
  if (!rs.length) return null;
  const card = runsTable(rs.slice(0, 20), 'Runbook fixes');
  card.id = 'runbook-fixes';
  return h('div', { class: 'mt-6' }, card);
}

/** One run: the exact script, the instances, each one's output, and for a fix the approval. */
export async function runbookRunPage({ params }) {
  const el = h('div', {});
  let timer = null;
  async function load() {
    let r;
    try { r = await get('/v1/runbook-runs/' + encodeURIComponent(params.id)); } catch (e) { return mount(el, errorBox(e.message)); }
    const admin = atLeast(session.role, 'admin');
    const mine = r.by === session.name;
    const waiting = r.status === 'proposed';
    const av = approvalView(r.approval, session.name, r.by, 'run');
    const note = h('input', { id: 'rbr-note', class: 'input', placeholder: 'Optional note for the record', 'aria-label': 'Note' });
    const act = async (what, body) => {
      try { await post('/v1/runbook-runs/' + r.id + '/' + what, body); toast(what === 'approve' ? (av.last ? 'Approved. It is running.' : 'Your approval is recorded. It needs another administrator.') : 'Done', 'ok'); } catch (e) { toast(e.message, 'error'); }
      load();
    };
    const approve = h('button', { class: 'btn-primary', type: 'button', id: 'approve-runbook', disabled: !av.may, title: av.why || null, onclick: async () => {
      const ok = await confirmDialog({ title: av.last ? 'Approve and run?' : 'Approve this fix?', body: av.last
        ? 'The script shown runs as root on ' + r.targets.join(', ') + '.' : 'Your approval is recorded. Nothing runs until another administrator approves it too.', confirmLabel: av.label });
      if (ok) act('approve', { hash: r.hash, note: note.value });
    } }, icon('play', 'size-4'), av.label);
    const row = (label, ...kids) => h('div', { class: 'grid gap-1 py-2 text-sm sm:grid-cols-[9rem_1fr] sm:gap-3' }, h('div', { class: 'muted' }, label), h('div', { class: 'min-w-0 break-words' }, ...kids));
    mount(el,
      h('nav', { class: 'mb-2 text-sm', 'aria-label': 'Breadcrumb' }, h('a', { href: '#/runbooks/' + r.runbook, class: 'muted hover:underline' }, r.runbook_name), h('span', { class: 'muted' }, ' › '), h('span', {}, 'Run ' + r.id)),
      pageHeader(r.runbook_name + ' v' + r.version, 'Asked by ' + r.by + (r.via_ai ? ' through an agent' : '') + ' · ' + relTime(r.at), kindPill(r.kind), pill(r.status, STATUS_LABEL[r.status])),
      r.status === 'failed' && r.error ? h('div', { class: 'mb-4', id: 'rbr-failed' }, errorBox(r.error)) : null,
      h('section', { class: 'card card-pad mb-4' },
        row('Instances', h('span', { class: 'font-mono text-xs', id: 'rbr-targets' }, r.targets.join(', '))),
        Object.keys(r.params || {}).length ? row('Values', h('span', { class: 'font-mono text-xs' }, Object.entries(r.params).map(([k, v]) => k + '=' + v).join('  '))) : null,
        r.reason ? row('Why', h('span', { class: 'whitespace-pre-wrap' }, r.reason)) : null,
        r.command_id ? row('SSM command', h('code', { class: 'text-xs' }, r.command_id)) : null,
        r.trace_id ? row('Trace', h('a', { class: 'text-brand-700 hover:underline dark:text-brand-200', href: '#/traces/' + r.trace_id }, 'Open the trace')) : null),
      waiting && av.line ? h('div', { class: 'mb-4', id: 'approval-state' }, notice('info', av.line + '.')) : null,
      waiting && (admin
        ? (mine ? h('div', { class: 'mb-4' }, notice('info', 'You asked for this fix, so someone else must approve it.'))
          : h('section', { class: 'card card-pad mb-4' }, !av.may && av.why ? h('div', { class: 'mb-3' }, notice('warn', av.why)) : null,
            h('div', { class: 'flex flex-wrap items-end gap-3' }, h('div', { class: 'min-w-64 flex-1' }, note), approve,
              h('button', { class: 'btn-secondary', type: 'button', id: 'reject-runbook', onclick: () => act('reject', {}) }, 'Reject'))))
        : h('div', { class: 'mb-4' }, notice('info', 'A different administrator will review this fix.'))),
      waiting && (mine || admin) ? h('button', { class: 'btn-ghost btn-sm mb-4', type: 'button', id: 'cancel-runbook', onclick: () => act('cancel', {}) }, 'Withdraw this fix') : null,
      r.decided_by && !waiting ? h('p', { class: 'muted mb-4 text-sm' }, (r.status === 'rejected' ? 'Rejected by ' : r.status === 'cancelled' ? 'Withdrawn by ' : 'Approved by ') + r.decided_by) : null,
      r.instances.length ? h('section', { class: 'mb-4 space-y-3', id: 'rbr-output' }, r.instances.map(i => h('div', { class: 'card card-pad', 'data-instance': i.id },
        h('div', { class: 'mb-2 flex flex-wrap items-center justify-between gap-2' }, h('span', { class: 'font-mono text-sm' }, i.id),
          h('span', { class: 'text-sm' }, pill(i.status === 'Success' ? 'succeeded' : ['Pending', 'InProgress', 'Delayed'].includes(i.status) ? 'running' : 'failed', i.status), h('span', { class: 'muted ml-2 text-xs' }, 'exit ' + i.exit_code))),
        i.error ? h('p', { class: 'mb-2 text-sm text-red-700 dark:text-red-300' }, i.error) : null,
        h('pre', { class: 'max-h-[28rem] overflow-auto rounded-lg bg-slate-950 p-3 font-mono text-xs text-slate-100' }, i.output || '(no output yet)')))) : null,
      h('details', { class: 'card card-pad mb-4', open: waiting },
        h('summary', { class: 'cursor-pointer text-sm font-medium' }, 'The exact script sent'),
        h('pre', { class: 'mt-2 max-h-[28rem] overflow-auto rounded-lg bg-slate-950 p-3 font-mono text-xs text-slate-100', id: 'rbr-script' }, r.script),
        h('p', { class: 'muted mt-2 break-all font-mono text-[11px]' }, 'sha256 ' + r.hash)));
    clearTimeout(timer);
    if (r.status === 'running') timer = setTimeout(() => { if (el.isConnected) load(); }, 2000);
  }
  mount(el, spinner());
  await load();
  return el;
}
