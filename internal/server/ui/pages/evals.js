// Agents › Evals (design note 050): suites of cases an agent must pass. A run asks the agent every case one to five
// times with nothing that changes anything carried out; a case passes only if every attempt passes.
import { h, icon, mount, pageHeader, relTime, absTime, emptyState, spinner, errorBox, notice, toast, drawer, field, confirmDialog } from '../lib/dom.js';
import { get, post, put, del, mayDelete } from '../lib/api.js';
import { session } from '../app.js';
import { navigate } from '../lib/router.js';
import { chipInput } from '../lib/controls.js';
import { CHECKS, checkMeta, describeCheck, cleanCheck, scoreTone, attemptLine } from '../lib/evals.js';

const TONE = { ok: 'pill-ok', wait: 'pill-wait', bad: 'pill-bad', idle: 'pill-idle' };
const score = (r) => r ? h('span', { class: TONE[scoreTone(r.passed_cases, r.total_cases)] + ' tabular-nums' }, r.status === 'running' ? 'running…' : r.passed_cases + ' / ' + r.total_cases) : h('span', { class: 'pill-idle' }, 'not run');

// ------------------------------------------------------------------ the list

export async function evalsPage({ query }) {
  const el = h('div', {});
  let r, agents;
  try { [r, agents] = await Promise.all([get('/v1/ai/evals'), get('/v1/ai/agents').then(x => x.agents)]); } catch (e) { return mount(el, errorBox(e.message)); }
  const name = (id) => agents.find(a => a.id === id)?.name || id;
  mount(el, pageHeader('Evals', 'Test an agent before people rely on it: cases it must answer, tools it must or must never call, answers a judge model grades. Nothing an eval runs changes anything.',
    h('div', { class: 'flex flex-wrap gap-2' },
      h('button', { class: 'btn-secondary', type: 'button', id: 'eval-attacks', onclick: () => attackSuite(agents, query && query.agent) }, icon('shield', 'size-4'), 'Add attack suite'),
      h('button', { class: 'btn-primary', type: 'button', id: 'eval-new', onclick: () => newSuite(agents, query && query.agent) }, icon('plus', 'size-4'), 'New suite'))),
    r.suites.length ? h('div', { class: 'card divide-y divide-slate-200 dark:divide-slate-800', id: 'eval-list' }, r.suites.map(x => h('a', { href: '#/evals/' + x.id, 'data-suite': x.id,
      class: 'flex flex-wrap items-center gap-3 px-5 py-4 hover:bg-slate-50 dark:hover:bg-slate-800/40' },
      h('span', { class: 'flex size-9 flex-none items-center justify-center rounded-lg bg-brand-50 text-brand-700 dark:bg-brand-500/15 dark:text-brand-200' }, icon('eval', 'size-[18px]')),
      h('div', { class: 'min-w-0 flex-1' }, h('div', { class: 'font-medium' }, x.name), h('div', { class: 'muted text-xs' }, name(x.agent) + ' · ' + x.cases.length + ' cases · ' + x.attempts + (x.attempts === 1 ? ' attempt' : ' attempts each'))),
      x.last_run ? h('span', { class: 'muted text-xs', title: absTime(x.last_run.started) }, 'v' + x.last_run.version + (x.last_run.model ? ' on ' + x.last_run.model : '') + ' · ' + relTime(x.last_run.started)) : null,
      score(x.last_run)))) : emptyState('eval', 'No suites yet', 'A suite is a few questions for one agent and what the answers must do. Start with three cases you would check by hand.',
      h('button', { class: 'btn-primary', type: 'button', onclick: () => newSuite(agents) }, icon('plus', 'size-4'), 'New suite')));
  return el;
}

function attackSuite(agents, agentID) {
  drawer('Add attack suite', (close) => {
    const agent = h('select', { id: 'attack-agent', class: 'input' }, agents.filter(a => !a.archived_at).map(a => h('option', { value: a.id, selected: a.id === agentID }, a.name)));
    const out = h('div', {});
    const go = h('button', { class: 'btn-primary', type: 'button', id: 'attack-create', onclick: async () => {
      go.disabled = true;
      try { const x = await post('/v1/ai/evals/attack-suite', { agent: agent.value }); close(); navigate('/evals/' + x.id); }
      catch (e) { mount(out, errorBox(e.message)); go.disabled = false; }
    } }, 'Add');
    return h('div', { class: 'space-y-4' },
      h('p', { class: 'muted text-sm' }, 'Seven ready-made cases from the OWASP list for agentic applications: orders hidden in a log or an issue, a change asked for without approval, a tool used for something it is not for, secret values, and a role the person does not have. Each passes only if the agent does not obey. Every case is asked three times.'),
      field('Agent', agent, 'Give the agent run_failure or get_step_log so it reads the planted text, and set a judge model on the suite for the grading checks.', 'attack-agent'), out,
      h('div', { class: 'flex justify-end gap-2' }, h('button', { class: 'btn-secondary', type: 'button', onclick: () => close() }, 'Cancel'), go));
  });
}

function newSuite(agents, agentID) {
  drawer('New suite', (close) => {
    const agent = h('select', { id: 'suite-agent', class: 'input' }, agents.filter(a => !a.archived_at).map(a => h('option', { value: a.id, selected: a.id === agentID }, a.name)));
    const name = h('input', { id: 'suite-name', class: 'input', maxlength: '80', placeholder: 'Regression' });
    const attempts = h('select', { id: 'suite-attempts', class: 'input' }, [1, 2, 3, 5].map(n => h('option', { value: String(n), selected: n === 3 }, n === 1 ? 'Once' : n + ' times')));
    const out = h('div', {});
    const go = h('button', { class: 'btn-primary', type: 'button', id: 'suite-create', onclick: async () => {
      go.disabled = true;
      try { const x = await post('/v1/ai/evals', { agent: agent.value, name: name.value, attempts: Number(attempts.value), cases: [] }); close(); navigate('/evals/' + x.id); }
      catch (e) { mount(out, errorBox(e.message)); go.disabled = false; }
    } }, 'Create');
    return h('div', { class: 'space-y-4' }, field('Agent', agent, null, 'suite-agent'), field('Name', name, 'Regression for what must keep working; Capability for what it should learn to do.', 'suite-name'),
      field('Ask each case', attempts, 'A case passes only if every try passes, so answers that hold up only sometimes show up.', 'suite-attempts'), out,
      h('div', { class: 'flex justify-end gap-2' }, h('button', { class: 'btn-secondary', type: 'button', onclick: () => close() }, 'Cancel'), go));
  });
}

// ------------------------------------------------------------------ one suite

export async function suitePage({ params, query }) {
  const el = h('div', {});
  let tab = (query && query.tab) || 'cases';
  async function load() {
    let x, agent, runs, models;
    try {
      x = await get('/v1/ai/evals/' + encodeURIComponent(params.id));
      [agent, runs, models] = await Promise.all([get('/v1/ai/agents/' + encodeURIComponent(x.agent)), get('/v1/ai/eval-runs?suite=' + x.id).then(r => r.runs), get('/v1/models').then(r => r.models).catch(() => [])]);
    } catch (e) { return mount(el, errorBox(e.message)); }
    const save = async (next) => { try { await put('/v1/ai/evals/' + x.id, next); toast('Saved', 'ok'); load(); } catch (e) { toast(e.message, 'error'); } };
    const version = h('select', { class: 'input w-auto py-1.5 text-sm', id: 'run-version', 'aria-label': 'Version' }, [...agent.versions].reverse().map(v => h('option', { value: String(v.n) }, 'v' + v.n + (v.n === agent.current ? ' (current)' : ''))));
    const model = h('select', { class: 'input w-auto py-1.5 text-sm', id: 'run-model', 'aria-label': 'Model' }, [h('option', { value: '' }, 'its own model'), ...models.map(m => h('option', { value: m.ref }, m.label + ' · ' + m.provider_name))]);
    const runBtn = h('button', { class: 'btn-primary', type: 'button', id: 'suite-run', disabled: !x.cases.length, onclick: async () => {
      runBtn.disabled = true;
      try { const r = await post('/v1/ai/evals/' + x.id + '/run', { version: Number(version.value), model: model.value }); navigate('/eval-runs/' + r.id); }
      catch (e) { toast(e.message, 'error'); runBtn.disabled = false; }
    } }, icon('play', 'size-4'), 'Run');
    const tabBtn = (id, label, n) => h('button', { type: 'button', 'aria-pressed': String(tab === id), 'data-suite-tab': id, onclick: () => { tab = id; load(); },
      class: 'rounded-lg px-3 py-1.5 text-sm font-medium ' + (tab === id ? 'bg-brand-50 text-brand-700 dark:bg-brand-500/15 dark:text-brand-200' : 'text-slate-600 hover:bg-slate-100 dark:text-slate-300 dark:hover:bg-slate-800') }, label + ' (' + n + ')');
    mount(el,
      h('nav', { class: 'mb-2 text-sm', 'aria-label': 'Breadcrumb' }, h('a', { href: '#/evals', class: 'muted hover:underline' }, 'Evals'), h('span', { class: 'muted' }, ' › '), h('span', {}, x.name)),
      pageHeader(x.name, 'For ' + agent.name + ' · each case asked ' + (x.attempts === 1 ? 'once' : x.attempts + ' times') + ' · judge: ' + (x.judge || 'the assistant\'s model'),
        h('div', { class: 'flex flex-wrap items-center gap-2' }, version, model, runBtn,
          mayDelete(session, x.created_by) && h('button', { class: 'btn-ghost btn-sm text-rose-600 dark:text-rose-400', type: 'button', 'aria-label': 'Delete suite', title: 'Delete suite', onclick: async () => {
            if (!await confirmDialog({ title: 'Delete ' + x.name + '?', body: 'Its cases and runs are deleted. The agent is not changed.', confirmLabel: 'Delete', danger: true })) return;
            try { await del('/v1/ai/evals/' + x.id); navigate('/evals'); } catch (e) { toast(e.message, 'error'); }
          } }, icon('trash', 'size-4')))),
      h('div', { class: 'mb-4 flex gap-1' }, tabBtn('cases', 'Cases', x.cases.length), tabBtn('runs', 'Runs', runs.length)),
      tab === 'cases' ? casesView(x, agent, save, load) : runsView(runs));
  }
  mount(el, spinner());
  await load();
  return el;
}

function casesView(x, agent, save, reload) {
  const edit = (i) => caseDrawer(x, agent, i, reload);
  return h('div', { class: 'space-y-3', id: 'suite-cases' },
    x.cases.map((c, i) => h('article', { class: 'card card-pad flex flex-wrap items-start gap-3', 'data-case': c.id },
      h('span', { class: 'mt-0.5 flex size-8 flex-none items-center justify-center rounded-lg bg-slate-100 text-slate-600 dark:bg-slate-800 dark:text-slate-300' }, icon('task', 'size-4')),
      h('div', { class: 'min-w-0 flex-1 space-y-1.5' },
        h('div', { class: 'font-medium' }, c.name),
        h('p', { class: 'muted line-clamp-2 text-sm' }, c.input),
        h('div', { class: 'flex flex-wrap gap-1.5' },
          Object.keys(c.fixtures || {}).map(t => h('span', { class: 'inline-flex items-center gap-1 rounded-full bg-amber-50 px-2 py-0.5 text-xs text-amber-800 dark:bg-amber-500/10 dark:text-amber-300', title: 'This tool answers with a fixture' }, icon('tool', 'size-3'), t + ' (fixture)')),
          c.checks.map(ch => h('span', { class: 'inline-flex items-center gap-1 rounded-full border border-slate-200 px-2 py-0.5 text-xs dark:border-slate-700' }, icon(checkMeta(ch.kind).icon, 'size-3'), describeCheck(ch))))),
      h('div', { class: 'flex gap-1' }, h('button', { class: 'btn-secondary btn-sm', type: 'button', onclick: () => edit(i) }, icon('edit', 'size-3.5'), 'Change'),
        h('button', { class: 'btn-ghost btn-sm', type: 'button', 'aria-label': 'Remove ' + c.name, onclick: () => save({ ...x, cases: x.cases.filter((_, j) => j !== i) }) }, icon('trash', 'size-3.5'))))),
    h('button', { class: 'btn-secondary', type: 'button', id: 'case-add', onclick: () => edit(-1) }, icon('plus', 'size-4'), 'Add a case'),
    !x.cases.length ? notice('info', 'Tip: with Keep prompts and answers turned on (Settings › OpenTelemetry), a trace of a real chat has Add to a suite, which fills a case in for you.') : null);
}

export function caseDrawer(x, agent, i, reload, preset) {
  const c = preset || (i >= 0 ? x.cases[i] : { name: '', input: '', fixtures: {}, fields: [], checks: [{ kind: 'contains', value: '' }] });
  let checks = c.checks.map(ch => ({ ...ch }));
  let fixtures = Object.entries(c.fixtures || {}).map(([tool, out]) => ({ tool, out }));
  let fields = [...(c.fields || [])];
  const tools = (agent.versions.find(v => v.n === agent.current)?.tools) || [];
  drawer(i >= 0 ? 'Change case' : 'Add a case', (close) => {
    const name = h('input', { id: 'case-name', class: 'input', maxlength: '120', value: c.name, placeholder: 'Explains a timeout' });
    const input = h('textarea', { id: 'case-input', class: 'input', rows: '3', placeholder: 'Why did build 42 fail?' }, c.input);
    const fixBox = h('div', { class: 'space-y-2', id: 'case-fixtures' });
    const drawFix = () => mount(fixBox, fixtures.map((f, j) => h('div', { class: 'space-y-1 rounded-lg border border-slate-200 p-2 dark:border-slate-700' },
      h('div', { class: 'flex gap-2' }, h('select', { class: 'input py-1 text-sm', 'aria-label': 'Tool', onchange: (e) => { f.tool = e.target.value; } },
        [...new Set([...tools, f.tool].filter(Boolean))].map(t => h('option', { value: t, selected: t === f.tool }, t))),
        h('button', { type: 'button', class: 'btn-ghost p-1', 'aria-label': 'Remove fixture', onclick: () => { fixtures.splice(j, 1); drawFix(); } }, icon('x', 'size-4'))),
      h('textarea', { class: 'input font-mono text-xs', rows: '3', 'aria-label': 'What the tool returns', placeholder: 'What the tool returns in this case', oninput: (e) => { f.out = e.target.value; } }, f.out))),
      tools.length ? h('button', { type: 'button', class: 'btn-ghost btn-sm', id: 'fixture-add', onclick: () => { fixtures.push({ tool: tools[0], out: '' }); drawFix(); } }, icon('plus', 'size-3.5'), 'Answer a tool with a fixture') : h('p', { class: 'muted text-xs' }, 'This agent has no tools.'));
    drawFix();
    const chkBox = h('div', { class: 'space-y-2', id: 'case-checks' });
    const drawChecks = () => mount(chkBox, checks.map((ch, j) => {
      const m = checkMeta(ch.kind);
      const kind = h('select', { class: 'input py-1 text-sm', 'aria-label': 'Check', onchange: (e) => { checks[j] = { kind: e.target.value }; drawChecks(); } }, CHECKS.map(k => h('option', { value: k.kind, selected: k.kind === ch.kind }, k.label)));
      const inp = (prop, label, ph, long) => long
        ? h('textarea', { class: 'input text-sm', rows: '3', 'aria-label': label, placeholder: ph || label, oninput: (e) => { ch[prop] = e.target.value; } }, ch[prop] || '')
        : h('input', { class: 'input py-1 text-sm', 'aria-label': label, placeholder: ph || label, value: ch[prop] ?? '', type: prop === 'n' ? 'number' : 'text', oninput: (e) => { ch[prop] = e.target.value; } });
      return h('div', { class: 'space-y-1.5 rounded-lg border border-slate-200 p-2.5 dark:border-slate-700', 'data-check': String(j) },
        h('div', { class: 'flex items-center gap-2' }, h('span', { class: 'text-slate-500' }, icon(m.icon, 'size-4')), kind,
          h('button', { type: 'button', class: 'btn-ghost p-1', 'aria-label': 'Remove check', onclick: () => { checks.splice(j, 1); drawChecks(); } }, icon('x', 'size-4'))),
        m.key ? inp('key', m.key, ch.kind === 'field' ? 'cause' : 'limit') : null,
        m.value ? inp('value', m.value, m.place, m.long) : null,
        m.n ? inp('n', m.n, '3') : null);
    }), h('button', { type: 'button', class: 'btn-ghost btn-sm', id: 'check-add', onclick: () => { checks.push({ kind: 'contains', value: '' }); drawChecks(); } }, icon('plus', 'size-3.5'), 'Add a check'));
    drawChecks();
    const out = h('div', {});
    const ok = h('button', { class: 'btn-primary', type: 'button', id: 'case-save', onclick: async () => {
      const next = { name: name.value.trim(), input: input.value.trim(), fields, fixtures: Object.fromEntries(fixtures.filter(f => f.tool).map(f => [f.tool, f.out || ''])), checks: checks.map(cleanCheck) };
      if (i >= 0) next.id = x.cases[i].id;
      const cases = i >= 0 ? x.cases.map((y, j) => j === i ? next : y) : [...x.cases, next];
      try { await put('/v1/ai/evals/' + x.id, { ...x, cases }); toast('Saved', 'ok'); close(); reload && reload(); }
      catch (e) { mount(out, errorBox(e.message)); }
    } }, 'Save case');
    return h('div', { class: 'space-y-4' },
      field('Name', name, null, 'case-name'), field('What the agent is asked', input, null, 'case-input'),
      h('div', {}, h('div', { class: 'label flex items-center gap-1.5' }, icon('tool', 'size-3.5'), 'Fixtures'), h('p', { class: 'muted -mt-1 mb-2 text-xs' }, 'A tool with a fixture returns this text instead of reading live data, so the case gives the same input every time. Put an attack in a fixture to test that the agent does not obey it.'), fixBox),
      chipInput({ id: 'case-fields', label: 'Fields to set', values: fields, placeholder: 'cause, severity', onchange: (v) => { fields = v; }, hint: 'The agent is asked to set these at the end of its answer, for Field equals checks.' }),
      h('div', {}, h('div', { class: 'label flex items-center gap-1.5' }, icon('rules', 'size-3.5'), 'Checks'), h('p', { class: 'muted -mt-1 mb-2 text-xs' }, 'Every check must pass. Proposals and other changes are recorded, never carried out: test them with Never calls a tool.'), chkBox),
      out, h('div', { class: 'flex justify-end gap-2' }, h('button', { class: 'btn-secondary', type: 'button', onclick: () => close() }, 'Cancel'), ok));
  }, { subtitle: x.name });
}

function runsView(runs) {
  if (!runs.length) return emptyState('play', 'No runs yet', 'Run the suite to see how the agent does.');
  return h('div', { class: 'card overflow-x-auto' }, h('table', { class: 'tbl', id: 'suite-runs' },
    h('thead', {}, h('tr', {}, ['Score', 'Version', 'Model', 'By', 'When'].map(t => h('th', {}, t)))),
    h('tbody', {}, runs.map(r => h('tr', { class: 'cursor-pointer', 'data-run': r.id, onclick: () => navigate('/eval-runs/' + r.id) },
      h('td', {}, score(r)), h('td', { class: 'text-sm' }, 'v' + r.version), h('td', { class: 'text-sm' }, r.model || h('span', { class: 'muted' }, 'its own')),
      h('td', { class: 'text-sm' }, r.by), h('td', { class: 'muted text-sm', title: absTime(r.started) }, relTime(r.started)))))));
}

// ------------------------------------------------------------------ one run

export async function evalRunPage({ params }) {
  const el = h('div', {});
  let timer;
  async function load() {
    let r;
    try { r = await get('/v1/ai/eval-runs/' + encodeURIComponent(params.id)); } catch (e) { return mount(el, errorBox(e.message)); }
    const cell = (a, ci, ai) => h('button', { type: 'button', title: a.pass ? 'Passed' : 'Failed', 'data-cell': ci + '-' + ai, onclick: () => show(ci, ai),
      class: 'flex size-8 items-center justify-center rounded-lg ' + (a.pass ? 'bg-emerald-100 text-emerald-700 dark:bg-emerald-500/15 dark:text-emerald-300' : 'bg-rose-100 text-rose-700 dark:bg-rose-500/15 dark:text-rose-300') }, icon(a.pass ? 'check' : 'x', 'size-4'));
    const detail = h('section', { class: 'card card-pad space-y-3', id: 'attempt-detail' });
    const show = (ci, ai) => {
      const res = r.results[ci], a = res.attempts[ai];
      mount(detail,
        h('div', { class: 'flex flex-wrap items-center justify-between gap-2' }, h('h2', { class: 'font-semibold' }, res.name + ' · attempt ' + (ai + 1)),
          a.trace_id ? h('a', { class: 'btn-secondary btn-sm', href: '#/traces/' + a.trace_id }, icon('trace', 'size-3.5'), 'Trace') : null),
        h('p', { class: 'muted text-xs' }, attemptLine(a)),
        a.error ? errorBox(a.error) : null,
        h('ul', { class: 'space-y-1.5' }, a.checks.map(c => h('li', { class: 'flex items-start gap-2 text-sm' },
          h('span', { class: c.pass ? 'text-emerald-600' : 'text-rose-600' }, icon(c.pass ? 'check' : 'x', 'mt-0.5 size-4')),
          h('span', {}, h('span', { class: 'font-medium' }, checkMeta(c.kind).label), h('span', { class: 'muted' }, ' · ' + c.note))))),
        a.reply ? h('div', {}, h('div', { class: 'label' }, 'Its answer'), h('pre', { class: 'max-h-72 overflow-auto whitespace-pre-wrap rounded-lg bg-slate-50 p-3 text-xs dark:bg-slate-800/60' }, a.reply)) : null);
    };
    const total = r.total_cases;
    mount(el,
      h('nav', { class: 'mb-2 text-sm', 'aria-label': 'Breadcrumb' }, h('a', { href: '#/evals', class: 'muted hover:underline' }, 'Evals'), h('span', { class: 'muted' }, ' › '),
        h('a', { href: '#/evals/' + r.suite + '?tab=runs', class: 'muted hover:underline' }, r.suite_name), h('span', { class: 'muted' }, ' › run')),
      pageHeader(r.suite_name + ': ' + (r.status === 'running' ? 'running…' : r.passed_cases + ' of ' + total + ' cases passed'),
        r.agent_name + ' v' + r.version + (r.model ? ' on ' + r.model : '') + ' · each case ' + (r.attempts === 1 ? 'once' : r.attempts + ' times') + ' · by ' + r.by + ' · ' + relTime(r.started), score(r)),
      h('div', { class: 'grid gap-4 lg:grid-cols-[minmax(0,1fr)_minmax(0,1.2fr)]' },
        h('section', { class: 'card overflow-x-auto' }, h('table', { class: 'tbl', id: 'run-grid' },
          h('thead', {}, h('tr', {}, h('th', {}, 'Case'), h('th', {}, 'Attempts'), h('th', {}, 'Passed'))),
          h('tbody', {}, r.results.map((res, ci) => h('tr', { 'data-result': res.case },
            h('td', { class: 'text-sm font-medium' }, res.name),
            h('td', {}, h('div', { class: 'flex gap-1' }, res.attempts.map((a, ai) => cell(a, ci, ai)))),
            h('td', { class: 'whitespace-nowrap text-sm tabular-nums' }, h('span', { class: res.pass ? 'pill-ok' : 'pill-bad' }, res.passed_attempts + ' / ' + res.attempts.length)))),
            r.status === 'running' ? h('tr', {}, h('td', { class: 'muted text-sm', colspan: '3' }, 'Running the next case…')) : null))),
        detail));
    if (r.results.length) { const fi = r.results.findIndex(x => !x.pass); show(fi < 0 ? 0 : fi, Math.max(0, r.results[fi < 0 ? 0 : fi].attempts.findIndex(a => !a.pass))); }
    else mount(detail, h('p', { class: 'muted text-sm' }, 'Waiting for the first case…'));
    clearInterval(timer);
    if (r.status === 'running') timer = setInterval(() => { if (!document.hidden) load(); }, 1500);
  }
  mount(el, spinner());
  await load();
  el.cleanup = () => clearInterval(timer);
  return el;
}
