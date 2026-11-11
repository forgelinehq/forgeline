// Team › Missions (design note 037, part 2): the board of missions by where they stand, and one mission live, stage
// by stage, with what it found, what it proposed, what it checked afterwards and the report Forgeline wrote.
import { h, icon, mount, pageHeader, relTime, spinner, errorBox, notice, toast, modal, field, confirmDialog, emptyState } from '../lib/dom.js';
import { get, post, atLeast } from '../lib/api.js';
import { navigate } from '../lib/router.js';
import { session } from '../app.js';
import { avatar } from './agents.js';
import { STAGES, COLUMNS, stageStates, columnOf, missionLine, findingsByRisk, outcome } from '../lib/missions.js';
import { tokens } from '../lib/usage.js';
import { workingBar, waitingDot } from '../lib/flow.js';

const SEV = { high: 'pill-bad', medium: 'pill-wait', low: 'pill-idle', info: 'pill-idle' };
const STEP = {
  done: 'border-emerald-300 bg-emerald-50 dark:border-emerald-500/40 dark:bg-emerald-500/10',
  now: 'border-sky-400 bg-sky-50 dark:border-sky-400/60 dark:bg-sky-500/10',
  wait: 'border-2 border-brand-500 bg-brand-50 dark:bg-brand-500/15',
  failed: 'border-rose-300 bg-rose-50 dark:border-rose-500/40 dark:bg-rose-500/10',
  skipped: 'border-dashed border-slate-200 text-slate-400 dark:border-slate-700',
  todo: 'border-slate-200 text-slate-500 dark:border-slate-700 dark:text-slate-400',
};
const STATUS = { running: 'pill-run', waiting: 'pill-wait', done: 'pill-ok', failed: 'pill-bad', cancelled: 'pill-idle', stopped: 'pill-bad' };

async function startMission(after) {
  let agents = [], accounts = [];
  try { agents = (await get('/v1/ai/agents')).agents.filter(a => !a.disabled); } catch { /* shown below */ }
  try { accounts = (await get('/v1/aws/accounts')).accounts; } catch { /* the primary is offered */ }
  if (!accounts.length) accounts = [{ id: 'primary', name: 'Primary' }];
  await modal('New mission', (close) => {
    const goal = h('textarea', { id: 'm-goal', class: 'input', rows: '3', maxlength: '2000', placeholder: 'Check the payments service for anything fragile or exposed, and fix what you can.' });
    const agent = h('select', { id: 'm-agent', class: 'input' }, agents.map(a => h('option', { value: a.id }, a.name)));
    const acct = h('select', { id: 'm-account', class: 'input' }, accounts.map(a => h('option', { value: a.id }, a.name)));
    const budget = h('input', { id: 'm-budget', class: 'input', type: 'number', min: '1000', step: '10000', placeholder: '200000' });
    const err = h('div', {});
    return h('div', { class: 'space-y-4' },
      !agents.length ? notice('warn', 'There is no agent that is turned on. Make one under Team › Agents first.') : null,
      field('Goal', goal, 'In your own words. The agent reads first, then proposes; nothing changes without an administrator’s approval.'),
      h('div', { class: 'grid gap-3 sm:grid-cols-2' }, field('Agent', agent), field('AWS account', acct, 'The only account it may look at')),
      field('Token budget', budget, 'It stops before its next model call once this is used. Empty means 200,000.'),
      err,
      h('div', { class: 'flex justify-end gap-2' }, h('button', { class: 'btn-secondary', type: 'button', onclick: () => close() }, 'Cancel'),
        h('button', { class: 'btn-primary', type: 'button', id: 'm-start', disabled: !agents.length, onclick: async () => {
          try {
            const m = await post('/v1/missions', { goal: goal.value, agent: agent.value, account: acct.value, budget_tokens: Number(budget.value) || 0 });
            close(); toast('Mission started', 'ok'); after(m);
          } catch (e) { mount(err, errorBox(e.message)); }
        } }, 'Start')));
  });
}

export async function missionsPage() {
  const el = h('div', {});
  const header = () => pageHeader('Missions', 'A goal you give one agent. It reads, proposes, waits for your OK, checks its work and reports.',
    h('button', { class: 'btn-primary', type: 'button', id: 'new-mission', onclick: () => startMission(m => navigate('/missions/' + m.id)) }, icon('plus', 'size-4'), 'New mission'));
  mount(el, header(), spinner());
  let list;
  try { list = (await get('/v1/missions')).missions; } catch (e) { mount(el, header(), errorBox(e.message)); return el; }
  if (!list.length) {
    mount(el, header(), h('div', { class: 'card' }, emptyState('flow', 'No missions yet', 'Give an agent a goal, such as “check the payments service for anything exposed”. It reports what it found and only changes things through proposals you approve.',
      h('button', { class: 'btn-primary', type: 'button', onclick: () => startMission(m => navigate('/missions/' + m.id)) }, 'Start the first one'))));
    return el;
  }
  const card = (m) => h('a', { href: '#/missions/' + m.id, class: 'card block space-y-2 p-3 hover:border-brand-400', 'data-mission': m.id },
    h('div', { class: 'flex items-start gap-2' }, avatar(m.agent_name, m.agent, 'size-7 text-[10px]'),
      h('b', { class: 'line-clamp-3 min-w-0 text-sm' }, m.goal)),
    h('p', { class: 'muted text-xs' }, missionLine(m)),
    h('div', { class: 'flex gap-1', 'aria-hidden': 'true' }, stageStates(m).map(s => h('span', { class: 'h-1.5 flex-1 rounded-full ' + ({ done: 'bg-emerald-500', now: 'bg-sky-500 animate-pulse', wait: 'bg-brand-500', failed: 'bg-rose-500' }[s.state] || 'bg-slate-200 dark:bg-slate-700') }))),
    h('p', { class: 'muted text-[11px]' }, 'started by ' + m.by + ' · ' + relTime(m.started_at)));
  mount(el, header(), h('div', { class: 'grid gap-4 lg:grid-cols-3', id: 'mission-board' }, COLUMNS.map(c => {
    const ms = list.filter(m => columnOf(m) === c.key);
    return h('section', { class: 'space-y-2', 'data-column': c.key, 'aria-label': c.label },
      h('div', { class: 'flex items-baseline justify-between' }, h('h2', { class: 'h-section' }, c.label), h('span', { class: 'muted text-xs tabular-nums' }, String(ms.length))),
      h('p', { class: 'hint' }, c.note),
      ms.length ? ms.map(card) : h('p', { class: 'muted rounded-xl border border-dashed border-slate-200 p-4 text-center text-xs dark:border-slate-800' }, 'None'));
  })));
  return el;
}

export async function missionPage({ params }) {
  const el = h('div', {});
  let timer = 0;
  async function load() {
    let m;
    try { m = await get('/v1/missions/' + encodeURIComponent(params.id)); } catch (e) { mount(el, pageHeader('Mission', ''), errorBox(e.message)); return; }
    draw(m);
    clearTimeout(timer);
    // Live while it works: a stage can take a minute, and waiting for approval can take days.
    if (m.status === 'running' || m.status === 'waiting') timer = setTimeout(() => { if (el.isConnected) load(); }, m.status === 'running' ? 2500 : 15000);
  }
  function draw(m) {
    const mayCancel = (m.by === session.name || atLeast(session.role, 'admin')) && (m.status === 'running' || m.status === 'waiting');
    const st = stageStates(m);
    const graph = h('ol', { class: 'grid grid-cols-2 gap-2 sm:grid-cols-5', id: 'mission-stages', 'aria-label': 'Stages' }, st.map((s, i) =>
      h('li', { class: 'relative rounded-xl border px-3 py-2.5 text-sm ' + STEP[s.state], 'data-stage': s.key, 'data-state': s.state },
        h('b', { class: 'block' }, (i + 1) + '. ' + s.label),
        h('span', { class: 'text-xs ' + (s.state === 'wait' ? 'text-brand-700 dark:text-brand-300' : 'muted') }, s.state === 'wait' ? waitingDot() : null,
          { now: 'working…', wait: 'waiting for you', done: 'done', failed: m.status === 'stopped' ? 'stopped' : m.status === 'cancelled' ? 'cancelled' : 'failed', skipped: 'not needed', todo: s.note }[s.state]),
        s.state === 'now' ? workingBar() : null)));
    const findings = findingsByRisk(m);
    const findingList = findings.length ? h('ul', { class: 'divide-y divide-slate-100 dark:divide-slate-800', id: 'mission-findings' }, findings.map(f => {
      const o = outcome(m, f.n);
      return h('li', { class: 'space-y-1 py-3', 'data-finding': String(f.n) },
        h('div', { class: 'flex flex-wrap items-center gap-2' }, h('span', { class: SEV[f.severity] }, f.severity), h('b', { class: 'text-sm' }, f.title),
          h('span', { class: 'muted text-xs', title: 'The lookup that showed it' }, 'seen with ' + f.source)),
        f.detail && h('p', { class: 'text-sm text-slate-600 dark:text-slate-300' }, f.detail),
        o.plan && h('p', { class: 'text-sm' }, o.plan.proposal ? h('a', { href: '#/proposals/' + o.plan.proposal, class: 'font-medium text-brand-700 underline dark:text-brand-300' }, 'Proposal ' + o.plan.proposal) : h('span', { class: 'muted' }, 'Recommended: '), o.plan.proposal ? ' · ' + o.plan.recommendation : o.plan.recommendation),
        o.evidence && h('p', { class: 'text-sm' }, h('span', { class: o.evidence.result === 'fixed' ? 'pill-ok' : o.evidence.result === 'still there' ? 'pill-bad' : 'pill-idle' }, o.evidence.result), ' ', o.evidence.detail));
    })) : h('p', { class: 'muted text-sm' }, m.stage === 'discover' && m.status === 'running' ? 'The agent is looking.' : 'Nothing it could show from a lookup.');
    const waiting = m.status === 'waiting' && h('div', { class: 'mb-4' }, notice('warn', h('span', {}, `${m.proposals.length === 1 ? 'Its proposal waits' : 'Its ' + m.proposals.length + ' proposals wait'} for a different administrator. `,
      m.proposals.map((id, i) => [i ? ', ' : '', h('a', { href: '#/proposals/' + id, class: 'underline' }, 'proposal ' + (i + 1))]), '. The mission goes on when each is approved or rejected.')));
    mount(el,
      pageHeader(m.goal, `${m.agent_name} (version ${m.agent_version}${m.may_propose ? ', may propose' : ', advises only'}) · account ${m.account} · started by ${m.by} ${relTime(m.started_at)}`,
        h('span', { class: STATUS[m.status] }, m.status),
        mayCancel && h('button', { class: 'btn-ghost btn-sm text-rose-600 dark:text-rose-400', type: 'button', id: 'mission-cancel', onclick: async () => {
          if (!await confirmDialog({ title: 'Cancel this mission?', body: 'It stops where it is. Proposals it already filed stay for an administrator to decide.', confirmLabel: 'Cancel mission', cancelLabel: 'Keep going', danger: true })) return;
          try { await post('/v1/missions/' + encodeURIComponent(m.id) + '/cancel', {}); toast('Cancelled', 'ok'); load(); } catch (e) { toast(e.message, 'error'); }
        } }, 'Cancel')),
      h('section', { class: 'card card-pad mb-4' }, graph,
        h('p', { class: 'muted mt-3 text-xs', id: 'mission-used' }, `Used ${tokens(m.used.input_tokens + m.used.output_tokens)} of ${tokens(m.budget_tokens)} tokens in ${m.used.calls} model calls.`)),
      waiting || null,
      m.report && h('section', { class: 'card card-pad mb-4', id: 'mission-report' }, h('h2', { class: 'h-section mb-2' }, 'Report'), h('p', { class: 'text-sm' }, m.report),
        h('p', { class: 'hint mt-2' }, 'Written by Forgeline from the record, not by the model.')),
      h('div', { class: 'grid gap-4 lg:grid-cols-[minmax(0,1fr)_20rem]' },
        h('section', { class: 'card card-pad' }, h('h2', { class: 'h-section mb-1' }, 'Findings'),
          m.unsupported ? h('p', { class: 'mb-2 text-xs text-amber-700 dark:text-amber-300', id: 'mission-unsupported' }, `${m.unsupported} ${m.unsupported === 1 ? 'claim was' : 'claims were'} left out: ${m.unsupported === 1 ? 'it' : 'they'} named no lookup the agent ran.`) : null,
          findingList),
        h('aside', { class: 'card card-pad self-start', 'aria-label': 'What the agent did' }, h('h2', { class: 'h-section mb-2' }, 'What the agent did'),
          m.steps.length ? h('ol', { class: 'space-y-3 text-sm', id: 'mission-steps' }, m.steps.map(s => h('li', {},
            h('div', { class: 'flex items-center justify-between gap-2' }, h('b', {}, STAGES.find(x => x.key === s.stage)?.label || s.stage), h('span', { class: STATUS[s.status === 'running' ? 'running' : s.status === 'failed' ? 'failed' : 'done'] }, s.status)),
            s.lookups?.length ? h('p', { class: 'muted text-xs' }, 'Looked at: ' + s.lookups.join(', ')) : h('p', { class: 'muted text-xs' }, 'No lookups'),
            s.error && h('p', { class: 'text-xs text-rose-700 dark:text-rose-300' }, s.error),
            s.output && h('details', { class: 'mt-1' }, h('summary', { class: 'cursor-pointer text-xs' }, 'What it said'), h('p', { class: 'mt-1 whitespace-pre-wrap text-xs' }, s.output))))) : h('p', { class: 'muted text-sm' }, 'Starting…'),
          m.error && m.status !== 'cancelled' ? h('div', { class: 'mt-3' }, errorBox(m.error)) : null)));
  }
  mount(el, spinner());
  await load();
  return el;
}
