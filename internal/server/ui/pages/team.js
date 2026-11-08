// Team › Team: the AI team as an org chart (note 029, frame 5). Who reports to whom, what each agent is doing
// now, when it next wakes, and the mission in progress. People approve anything that changes the cloud.
import { h, icon, mount, pageHeader, relTime, spinner, errorBox, notice, toast, emptyState } from '../lib/dom.js';
import { get, put, atLeast } from '../lib/api.js';
import { session } from '../app.js';
import { avatar } from './agents.js';
import { buildTree, stateLine, factsLine, missionSteps } from '../lib/team.js';

const TONE = {
  work: 'text-sky-700 dark:text-sky-300', wait: 'text-amber-700 dark:text-amber-300',
  sleep: 'text-slate-600 dark:text-slate-300', idle: 'text-slate-500 dark:text-slate-400', off: 'text-slate-400 dark:text-slate-500',
};
const DOT = { work: 'bg-sky-500 animate-pulse', wait: 'bg-amber-500', sleep: 'bg-violet-400', idle: 'bg-slate-300 dark:bg-slate-600', off: 'bg-slate-300 dark:bg-slate-700' };
const STEP = {
  done: 'border-emerald-300 bg-emerald-50 dark:border-emerald-500/40 dark:bg-emerald-500/10',
  failed: 'border-rose-300 bg-rose-50 dark:border-rose-500/40 dark:bg-rose-500/10',
  now: 'border-sky-400 bg-sky-50 dark:border-sky-400/60 dark:bg-sky-500/10',
  wait: 'border-2 border-brand-500 bg-brand-50 dark:bg-brand-500/15',
  todo: 'border-slate-200 text-slate-500 dark:border-slate-700 dark:text-slate-400',
  skipped: 'border-dashed border-slate-200 text-slate-400 dark:border-slate-700',
};

const COLS = { 1: 'sm:grid-cols-1', 2: 'sm:grid-cols-2', 3: 'sm:grid-cols-3', 4: 'sm:grid-cols-4', 5: 'sm:grid-cols-5', 6: 'sm:grid-cols-6' };

export async function teamPage() {
  const el = h('div', {});
  const admin = atLeast(session.role, 'admin');
  const header = () => pageHeader('Your AI team', 'Who reports to whom, what each agent is doing, and when it wakes next. You approve anything that changes your cloud.',
    admin && h('a', { class: 'btn-primary', href: '#/agents' }, icon('plus', 'size-4'), 'New agent'));
  mount(el, header(), spinner());
  let team;
  try { team = await get('/v1/ai/team'); } catch (e) { mount(el, header(), errorBox(e.message)); return el; }
  const members = team.members || [];
  const names = Object.fromEntries(members.map(m => [m.id, m.name]));

  function leadPicker(m) {
    const sel = h('select', { class: 'input mt-1 h-8 py-0 text-xs', 'aria-label': 'Who ' + m.name + ' reports to', 'data-reports': m.id },
      h('option', { value: '' }, 'Reports to you'),
      members.filter(x => x.id !== m.id).map(x => h('option', { value: x.id, selected: x.id === m.reports_to }, 'Reports to ' + x.name)));
    sel.addEventListener('change', async () => {
      try { await put('/v1/ai/agents/' + m.id + '/reports-to', { reports_to: sel.value }); toast(m.name + (sel.value ? ' now reports to ' + names[sel.value] : ' now reports to you'), 'ok'); el.replaceWith(await teamPage()); }
      catch (e) { toast(e.message, 'error'); sel.value = m.reports_to || ''; }
    });
    return sel;
  }

  function card(m) {
    const st = stateLine(m);
    return h('div', { class: 'card flex w-60 flex-col gap-1.5 p-3 text-left' + (m.state === 'off' ? ' opacity-70' : ''), 'data-member': m.id },
      h('a', { href: '#/agents/' + m.id, class: 'flex items-center gap-2.5 hover:text-brand-700 dark:hover:text-brand-300' }, avatar(m.name, m.id, 'size-9 text-xs'),
        h('span', { class: 'min-w-0' }, h('span', { class: 'block truncate font-semibold' }, m.name), m.description && h('span', { class: 'muted block truncate text-xs' }, m.description))),
      h('span', { class: 'flex items-center gap-1.5 text-xs ' + TONE[st.tone], 'data-state': m.state, title: st.text }, h('span', { class: 'size-2 shrink-0 rounded-full ' + DOT[st.tone] }),
        m.run_id ? h('a', { href: '#/workflow-runs/' + m.run_id, class: 'truncate underline-offset-2 hover:underline' }, st.text) : h('span', { class: 'truncate' }, st.text)),
      h('span', { class: 'muted text-xs' }, factsLine(m)),
      admin && leadPicker(m));
  }

  // A node: its card, then a line down to a row of its reports.
  function node(m) {
    return h('div', { class: 'flex flex-col items-center' }, card(m),
      m.reports.length > 0 && h('div', { class: 'h-5 w-0.5 bg-slate-300 dark:bg-slate-700', 'aria-hidden': 'true' }),
      m.reports.length > 0 && row(m.reports));
  }
  function row(list) {
    return h('ul', { class: 'flex flex-wrap justify-center gap-x-4 gap-y-6 border-t-2 border-slate-300 pt-5 dark:border-slate-700', 'aria-label': 'Reports' },
      list.map(m => h('li', { class: 'flex flex-col items-center' }, node(m))));
  }

  const you = h('div', { class: 'flex w-56 items-center gap-2.5 rounded-xl border-2 border-dashed border-brand-500 p-3' },
    h('span', { class: 'flex size-9 items-center justify-center rounded-xl bg-brand-600 text-sm font-bold text-white', 'aria-hidden': 'true' }, (session.name || '?').slice(0, 1).toUpperCase()),
    h('span', {}, h('b', {}, 'You and your team'), h('span', { class: 'muted block text-xs' }, 'approve every change')));
  const hire = h('a', { href: '#/marketplace', class: 'flex w-60 items-center justify-center gap-2 rounded-xl border-2 border-dashed border-slate-300 p-6 text-sm text-slate-500 hover:border-brand-400 hover:text-brand-700 dark:border-slate-700 dark:text-slate-400' }, icon('plus', 'size-4'), 'Hire an agent');

  const tree = buildTree(members);
  const chart = members.length
    ? h('div', { class: 'card overflow-x-auto p-6', id: 'team-chart' }, h('div', { class: 'flex min-w-fit flex-col items-center' }, you,
        h('div', { class: 'h-5 w-0.5 bg-slate-300 dark:bg-slate-700', 'aria-hidden': 'true' }),
        h('ul', { class: 'flex flex-wrap justify-center gap-x-4 gap-y-6 border-t-2 border-slate-300 pt-5 dark:border-slate-700' },
          tree.map(m => h('li', { class: 'flex flex-col items-center' }, node(m))), admin && h('li', {}, hire))))
    : emptyState('sparkle', 'No agents yet', 'An agent is an assistant with one job, its own instructions and tools. Make one, or hire one from the marketplace.',
        h('div', { class: 'mt-4 flex justify-center gap-2' }, admin && h('a', { class: 'btn-primary', href: '#/agents' }, 'Make an agent'), h('a', { class: 'btn-secondary', href: '#/marketplace' }, 'Browse the marketplace')));

  const run = team.mission;
  const steps = missionSteps(run);
  const mission = run && h('section', { class: 'card card-pad mt-6', id: 'team-mission' },
    h('div', { class: 'flex flex-wrap items-baseline justify-between gap-2' },
      h('h2', { class: 'font-display text-lg font-semibold' }, h('a', { href: '#/workflow-runs/' + run.id, class: 'hover:text-brand-700 dark:hover:text-brand-300' }, (run.finished_at ? 'Last mission: ' : 'Mission: ') + run.workflow_name)),
      h('span', { class: 'muted text-xs' }, (run.trigger === 'schedule' ? 'on schedule' : 'started by ' + run.by) + ' · ' + relTime(run.started_at))),
    h('ol', { class: 'mt-3 grid grid-cols-2 gap-2 text-center text-xs ' + (COLS[steps.length] || 'sm:grid-cols-6'), 'data-steps': String(steps.length) },
      steps.map(s => h('li', { class: 'rounded-lg border px-2 py-2.5 ' + STEP[s.state], 'data-step': s.state },
        h('b', { class: 'block text-sm' }, s.name), s.note && h('span', { class: s.state === 'wait' ? 'text-brand-700 dark:text-brand-300' : 'muted' }, s.note)))),
    steps.some(s => s.state === 'wait') && h('p', { class: 'mt-3 text-sm' }, 'It is waiting for an administrator. ', h('a', { href: '#/workflow-runs/' + run.id, class: 'font-medium text-brand-700 underline dark:text-brand-300' }, 'Review it')));

  mount(el, header(),
    team.schedules_stopped && h('div', { class: 'mb-4' }, notice('warn', 'The assistant is off, so no agent wakes on a schedule. ', h('a', { href: '#/integrations/claude/settings', class: 'underline' }, 'Turn it on'))),
    chart, mission,
    h('p', { class: 'hint mt-4' }, 'A reporting line shows how your team is organised; a lead does not hand out work yet. Spending limits per agent are coming: Forgeline does not yet measure what each agent spends.'));
  return el;
}
