// Agents › Teams: teams of agents (note 045). Each team is a lead and its members; give it a goal and the lead plans a
// task for each member, the members work at the same time, and the lead writes the answer. Below the teams, every
// agent with what it is doing now and its budget. Adding or changing a team happens in a side panel.
import { h, icon, mount, pageHeader, relTime, spinner, errorBox, notice, toast, emptyState, drawer, field, confirmDialog } from '../lib/dom.js';
import { get, post, put, del, atLeast, mayDelete } from '../lib/api.js';
import { navigate } from '../lib/router.js';
import { session } from '../app.js';
import { avatar } from './agents.js';
import { stateLine, factsLine } from '../lib/team.js';
import { budgetLine, tokens } from '../lib/usage.js';
import { budgetBar } from './ai_use.js';
import { renderReply } from '../lib/mdview.js';
import { teamScheduleDrawer, nextLine } from './schedules.js';
import { describe } from '../lib/schedule.js';

const TONE = {
  work: 'text-sky-700 dark:text-sky-300', wait: 'text-amber-700 dark:text-amber-300',
  sleep: 'text-slate-600 dark:text-slate-300', idle: 'text-slate-500 dark:text-slate-400', off: 'text-slate-400 dark:text-slate-500',
};
const DOT = { work: 'bg-sky-500 animate-pulse', wait: 'bg-amber-500', sleep: 'bg-violet-400', idle: 'bg-slate-300 dark:bg-slate-600', off: 'bg-slate-300 dark:bg-slate-700' };
const RUN = {
  planning: ['pill-run', 'Lead is planning'], working: ['pill-run', 'Members working'], writing: ['pill-run', 'Lead is writing'],
  done: ['pill-ok', 'Done'], failed: ['pill-bad', 'Failed'],
};
const TASK = {
  waiting: ['border-slate-200 dark:border-slate-700', 'Waiting'], working: ['border-sky-400 dark:border-sky-500/60', 'Working'],
  done: ['border-emerald-300 dark:border-emerald-500/40', 'Done'], failed: ['border-rose-300 dark:border-rose-500/40', 'Failed'],
};
const active = (r) => r && !['done', 'failed'].includes(r.status);

export async function teamPage() {
  const el = h('div', {});
  const writer = atLeast(session.role, 'developer');
  const header = () => pageHeader('Teams', 'Give a team a goal: its lead splits the work, the members look into it at the same time with their own tools, and the lead writes one answer. Anything that would change your cloud still waits for your approval.',
    writer && h('button', { class: 'btn-primary', type: 'button', id: 'new-team', onclick: () => teamDrawer(null, agents, () => reload()) }, icon('plus', 'size-4'), 'New team'));
  let teams, roster, agents;
  async function reload() { el.replaceWith(await teamPage()); }
  mount(el, header(), spinner());
  try {
    [teams, roster, agents] = await Promise.all([get('/v1/ai/teams').then(r => r.teams), get('/v1/ai/team'), get('/v1/ai/agents').then(r => r.agents)]);
  } catch (e) { mount(el, header(), errorBox(e.message)); return el; }
  const byID = Object.fromEntries(agents.map(a => [a.id, a]));
  const name = (id) => byID[id]?.name || id;

  const teamCard = (t) => {
    const r = t.last_run;
    return h('article', { class: 'card card-pad flex flex-col gap-4', 'data-team': t.id },
      h('a', { href: '#/teams/' + t.id, class: 'group' },
        h('h3', { class: 'font-display text-lg font-bold group-hover:text-brand-700 dark:group-hover:text-brand-200' }, t.name),
        h('p', { class: 'muted mt-1 line-clamp-2 text-sm' }, t.purpose || 'No purpose written yet.')),
      h('div', { class: 'flex items-center gap-3' },
        h('span', { class: 'flex items-center gap-2 text-sm', title: 'Lead' }, avatar(name(t.lead), t.lead, 'size-8 text-[11px] ring-2 ring-brand-500'), h('span', { class: 'font-medium' }, name(t.lead))),
        h('span', { class: 'muted text-xs' }, 'leads'),
        h('span', { class: 'flex -space-x-2' }, t.members.map(m => h('span', { title: name(m) }, avatar(name(m), m, 'size-8 text-[11px] ring-2 ring-white dark:ring-slate-900')))),
        h('span', { class: 'muted text-xs' }, t.members.length + ' member' + (t.members.length === 1 ? '' : 's'))),
      h('div', { class: 'mt-auto flex items-center gap-2 border-t border-slate-100 pt-3 text-xs dark:border-slate-800' },
        r ? h('a', { href: '#/teams/' + t.id, class: 'flex min-w-0 items-center gap-2' }, h('span', { class: RUN[r.status]?.[0] || 'pill-idle' }, RUN[r.status]?.[1] || r.status), h('span', { class: 'muted truncate' }, r.goal), h('span', { class: 'muted flex-none' }, relTime(r.started)))
          : h('span', { class: 'muted' }, 'No goal given yet'),
        h('a', { class: 'btn-secondary btn-sm ml-auto flex-none', href: '#/teams/' + t.id }, icon('play', 'size-3.5'), 'Give a goal')));
  };

  const teamsBlock = teams.length
    ? h('div', { class: 'grid gap-4 md:grid-cols-2 xl:grid-cols-3', id: 'teams' }, teams.map(teamCard))
    : emptyState('users', 'No teams yet', 'A team is a lead and a few agents who work on one goal together. Make one from your agents, or install a ready-made team from the library (Incident triage, Platform review).',
        h('div', { class: 'mt-4 flex justify-center gap-2' }, writer && h('button', { class: 'btn-primary', type: 'button', onclick: () => teamDrawer(null, agents, () => reload()) }, 'Make a team'), h('a', { class: 'btn-secondary', href: '#/library' }, 'Open the library')));

  // Every agent, one row each: what it is doing and what it used this month.
  const inTeams = new Map();
  for (const t of teams) for (const id of [t.lead, ...t.members]) inTeams.set(id, [...(inTeams.get(id) || []), t.name]);
  const agentRow = (m) => {
    const st = stateLine(m);
    const line = budgetLine(m.budget);
    const used = (m.budget?.used?.input_tokens || 0) + (m.budget?.used?.output_tokens || 0);
    return h('li', { class: 'grid grid-cols-[2.25rem_minmax(0,1fr)] items-center gap-x-3 gap-y-1 px-4 py-3 sm:grid-cols-[2.25rem_minmax(0,1.3fr)_minmax(0,1fr)_minmax(0,1fr)]', 'data-member': m.id },
      avatar(m.name, m.id, 'size-9 text-xs'),
      h('a', { href: '#/agents/' + m.id, class: 'min-w-0 hover:text-brand-700 dark:hover:text-brand-300' }, h('span', { class: 'block truncate font-semibold' }, m.name),
        h('span', { class: 'muted block truncate text-xs' }, inTeams.has(m.id) ? 'In ' + inTeams.get(m.id).join(', ') : 'Not in a team')),
      h('span', { class: 'col-start-2 flex min-w-0 items-center gap-1.5 text-xs sm:col-start-auto ' + TONE[st.tone], 'data-state': m.state, title: st.text }, h('span', { class: 'size-2 shrink-0 rounded-full ' + DOT[st.tone] }), h('span', { class: 'truncate' }, st.text)),
      h('span', { class: 'col-start-2 min-w-0 text-xs sm:col-start-auto' }, line ? h('a', { href: '#/ai-use', class: 'block space-y-1', title: line.text }, budgetBar(line)) : h('span', { class: 'muted' }, (used ? tokens(used) + ' tokens this month · ' : '') + factsLine(m))));
  };
  const members = roster.members || [];

  mount(el, header(),
    roster.schedules_stopped && h('div', { class: 'mb-4' }, notice('warn', 'The assistant is off, so no agent wakes on a schedule. Agents with a model of their own still work when asked.')),
    teamsBlock,
    h('section', { class: 'mt-8' },
      h('div', { class: 'mb-2 flex items-baseline justify-between' }, h('h2', { class: 'h-section' }, `All agents (${members.length})`), writer && h('a', { class: 'text-sm text-brand-700 hover:underline dark:text-brand-300', href: '#/agents' }, 'Manage agents')),
      members.length ? h('ul', { class: 'card divide-y divide-slate-100 dark:divide-slate-800', id: 'team-agents' }, members.map(agentRow))
        : h('p', { class: 'muted text-sm' }, 'No agents yet.')));
  return el;
}

/** The side panel that makes or changes a team: name, purpose, a lead and members picked from the agents. */
export function teamDrawer(t, agents, after) {
  const usable = agents.filter(a => !a.disabled || (t && (t.lead === a.id || t.members.includes(a.id))));
  const name = h('input', { id: 'team-name', class: 'input', maxlength: '60', value: t?.name || '', placeholder: 'Platform health' });
  const purpose = h('textarea', { id: 'team-purpose', class: 'input', rows: '2', maxlength: '300', placeholder: 'What this team is for, in a sentence. The lead reads it.' }, t?.purpose || '');
  const lead = h('select', { id: 'team-lead', class: 'input' }, h('option', { value: '' }, 'Pick the lead'), usable.map(a => h('option', { value: a.id, selected: t?.lead === a.id }, a.name)));
  const chosen = new Set(t?.members || []);
  const search = h('input', { class: 'input', type: 'search', placeholder: 'Find an agent', 'aria-label': 'Find an agent' });
  const list = h('ul', { class: 'max-h-80 divide-y divide-slate-100 overflow-y-auto rounded-xl border border-slate-200 dark:divide-slate-800 dark:border-slate-700', id: 'team-members' });
  const count = h('span', { class: 'muted text-xs' });
  const err = h('div', {});
  const draw = () => {
    const q = search.value.trim().toLowerCase();
    count.textContent = chosen.size + ' of 8 chosen';
    mount(list, usable.filter(a => a.id !== lead.value && (!q || (a.name + ' ' + a.description).toLowerCase().includes(q))).map(a => {
      const cb = h('input', { type: 'checkbox', class: 'size-4', checked: chosen.has(a.id), 'aria-label': 'Add ' + a.name, onchange: () => { cb.checked ? chosen.add(a.id) : chosen.delete(a.id); draw(); } });
      return h('li', {}, h('label', { class: 'flex cursor-pointer items-center gap-3 px-3 py-2.5 hover:bg-slate-50 dark:hover:bg-slate-800/60', 'data-pick': a.id }, cb, avatar(a.name, a.id, 'size-8 text-[11px]'),
        h('span', { class: 'min-w-0' }, h('span', { class: 'block text-sm font-medium' }, a.name), h('span', { class: 'muted block truncate text-xs' }, a.description || (a.tools.length + ' tools')))));
    }));
  };
  lead.addEventListener('change', () => { chosen.delete(lead.value); draw(); });
  search.addEventListener('input', draw);
  draw();
  drawer(t ? 'Change ' + t.name : 'New team', () => h('div', { class: 'space-y-5' },
    field('Name', name), field('Purpose', purpose),
    field('Lead', lead, 'It plans who does what and writes the answer. Give it no tools; it only reads what the members found.'),
    h('section', { class: 'space-y-2' }, h('div', { class: 'flex items-baseline justify-between' }, h('h3', { class: 'text-sm font-semibold' }, 'Members'), count), search, list,
      h('p', { class: 'muted text-xs' }, 'Each member works with its own model, skills, tools and budget. An agent can be in several teams.')),
    err), {
    subtitle: 'A lead and up to eight members.',
    footer: (close) => [
      t && mayDelete(session, t.created_by) ? h('button', { class: 'btn-ghost mr-auto text-rose-600 dark:text-rose-400', type: 'button', onclick: async () => {
        if (!await confirmDialog({ title: 'Remove ' + t.name + '?', body: 'Its agents stay. Its schedules and past runs are deleted.', confirmLabel: 'Remove', danger: true })) return;
        try { await del('/v1/ai/teams/' + t.id); close(); toast('Removed', 'ok'); navigate('/team'); } catch (e) { mount(err, errorBox(e.message)); }
      } }, icon('trash', 'size-4'), 'Remove') : null,
      h('button', { class: 'btn-secondary', type: 'button', onclick: () => close() }, 'Cancel'),
      h('button', { class: 'btn-primary', type: 'button', id: 'team-save', onclick: async () => {
        const body = { name: name.value.trim(), purpose: purpose.value.trim(), lead: lead.value, members: [...chosen] };
        try { const out = t ? await put('/v1/ai/teams/' + t.id, body) : await post('/v1/ai/teams', body); close(); toast(t ? 'Saved' : 'Team made', 'ok'); after ? after(out) : navigate('/teams/' + out.id); }
        catch (e) { mount(err, errorBox(e.message)); }
      } }, t ? 'Save' : 'Make the team')],
  });
}

/** One team: its people, a box to give it a goal, and its runs with the plan, each member's work and the answer. */
export async function teamDetailPage({ params }) {
  const el = h('div', {});
  const writer = atLeast(session.role, 'developer');
  let data, agents, scheds = [], selected = null, timer = null;
  async function load() {
    try { [data, agents, scheds] = await Promise.all([get('/v1/ai/teams/' + encodeURIComponent(params.id)), get('/v1/ai/agents').then(r => r.agents), get('/v1/ai/schedules').then(r => r.schedules.filter(x => x.target === '/teams/' + params.id)).catch(() => [])]); }
    catch (e) { mount(el, errorBox(e.message)); return; }
    if (!selected || !data.runs.some(r => r.id === selected)) selected = data.runs[0]?.id || null;
    draw();
    clearTimeout(timer);
    if (data.runs.some(active)) timer = setTimeout(() => { if (el.isConnected) load(); }, 2000);
  }
  const byID = () => Object.fromEntries(agents.map(a => [a.id, a]));
  function draw() {
    const t = data.team, names = byID(), nm = (id) => names[id]?.name || id;
    const run = data.runs.find(r => r.id === selected);
    const goal = h('textarea', { id: 'team-goal', class: 'input', rows: '3', maxlength: '2000', placeholder: 'For example: Why did the last deploys fail, and is anything wrong underneath?' });
    const busy = data.runs.some(active);
    const go = h('button', { class: 'btn-primary', type: 'button', id: 'team-go', disabled: busy, onclick: async () => {
      if (!goal.value.trim()) { goal.focus(); return; }
      go.disabled = true;
      try { const r = await post('/v1/ai/teams/' + t.id + '/runs', { goal: goal.value.trim() }); selected = r.id; toast('The team is on it', 'ok'); load(); }
      catch (e) { toast(e.message, 'error'); go.disabled = false; }
    } }, icon('play', 'size-4'), busy ? 'Working…' : 'Give the goal');
    const person = (id, role) => h('a', { href: '#/agents/' + id, class: 'flex items-center gap-2 rounded-xl border border-slate-200 px-3 py-2 hover:border-brand-300 dark:border-slate-700', 'data-person': id },
      avatar(nm(id), id, 'size-8 text-[11px]'), h('span', { class: 'min-w-0' }, h('span', { class: 'block truncate text-sm font-medium' }, nm(id)), h('span', { class: 'muted block text-xs' }, role)));
    mount(el,
      h('a', { class: 'muted mb-4 inline-flex items-center gap-1 text-sm hover:text-slate-900 dark:hover:text-white', href: '#/team' }, icon('chevron', 'size-3 rotate-180'), 'All teams'),
      pageHeader(t.name, t.purpose, writer && h('button', { class: 'btn-secondary btn-sm', type: 'button', id: 'team-edit', onclick: () => teamDrawer(t, agents, () => load()) }, icon('edit', 'size-3.5'), 'Change')),
      h('div', { class: 'mb-6 flex flex-wrap gap-2', id: 'team-people' }, person(t.lead, 'Lead'), t.members.map(m => person(m, 'Member'))),
      h('section', { class: 'card card-pad mb-6' }, h('h2', { class: 'h-section mb-2' }, 'Give the team a goal'), goal,
        h('div', { class: 'mt-3 flex flex-wrap items-center gap-3' }, go, h('span', { class: 'muted text-xs' }, 'Every agent works as you, with its own tools and budget. Each model call is in the call log.'))),
      h('section', { class: 'card card-pad mb-6', id: 'team-schedules' },
        h('div', { class: 'mb-2 flex flex-wrap items-center justify-between gap-2' }, h('div', {}, h('h2', { class: 'h-section' }, 'On a schedule'), h('p', { class: 'muted text-xs' }, 'Goals the team gets by itself, at set times.')),
          writer && h('button', { class: 'btn-secondary btn-sm', type: 'button', id: 'team-schedule-add', onclick: () => teamScheduleDrawer(t, null, load) }, icon('clock', 'size-3.5'), 'Schedule a goal')),
        scheds.length ? h('ul', { class: 'divide-y divide-slate-100 dark:divide-slate-800' }, scheds.map(it => h('li', { class: 'flex flex-wrap items-center gap-x-4 gap-y-1 py-2 text-sm', 'data-schedule': it.id },
          h('span', { class: 'min-w-0 flex-1 truncate font-medium', title: it.goal }, it.goal),
          h('span', { class: 'muted' }, describe({ every_minutes: it.every_minutes, cron: it.cron, tz: it.tz }) + ', as ' + it.run_as),
          h('span', { class: 'text-xs' }, 'next: ', nextLine(it)),
          writer && h('button', { class: 'btn-ghost btn-sm', type: 'button', onclick: () => teamScheduleDrawer(t, it, load) }, 'Change'))))
          : h('p', { class: 'muted text-sm' }, writer ? 'None yet. Schedule a goal to have the team work on it every morning, every week or by a cron expression.' : 'None.')),
      h('div', { class: 'grid gap-6 lg:grid-cols-[18rem_minmax(0,1fr)]' },
        h('nav', { 'aria-label': 'Runs' }, h('h2', { class: 'h-section mb-2' }, 'Runs'),
          data.runs.length ? h('ul', { class: 'space-y-1', id: 'team-runs' }, data.runs.map(r => h('li', {}, h('button', { type: 'button', 'data-run': r.id,
            class: 'w-full rounded-lg px-3 py-2 text-left text-sm ' + (r.id === selected ? 'bg-brand-50 dark:bg-brand-950/40' : 'hover:bg-slate-50 dark:hover:bg-slate-800/60'), onclick: () => { selected = r.id; draw(); } },
            h('span', { class: 'block truncate font-medium' }, r.goal), h('span', { class: 'muted flex items-center gap-2 text-xs' }, h('span', { class: RUN[r.status]?.[0] }, RUN[r.status]?.[1]), relTime(r.started))))))
            : h('p', { class: 'muted text-sm' }, 'No runs yet.')),
        run ? runView(run, nm) : h('div', {})));
  }
  mount(el, spinner());
  await load();
  return el;
}

function runView(r, nm) {
  return h('section', { class: 'min-w-0 space-y-4', id: 'team-run', 'data-status': r.status },
    h('div', { class: 'card card-pad' },
      h('div', { class: 'flex flex-wrap items-center gap-2' }, h('span', { class: RUN[r.status]?.[0] }, RUN[r.status]?.[1]), h('span', { class: 'muted text-xs' }, r.trigger === 'schedule' ? `given by its schedule ${relTime(r.started)}, as ${r.run_as}` : `given by ${r.by} ${relTime(r.started)}`)),
      h('p', { class: 'mt-2 font-medium' }, r.goal),
      r.trace_id ? h('a', { class: 'muted mt-1 inline-flex items-center gap-1 text-xs hover:underline', href: '#/traces/' + r.trace_id, id: 'team-run-trace' }, icon('trace', 'size-3.5'), 'See its trace') : null,
      r.plan_note && h('p', { class: 'muted mt-2 text-sm' }, h('b', {}, r.lead_name + ' planned: '), r.plan_note),
      r.error && h('div', { class: 'mt-3' }, errorBox(r.error))),
    r.tasks.length ? h('div', { class: 'grid gap-3 xl:grid-cols-2', id: 'team-tasks' }, r.tasks.map(tt => h('article', { class: 'card border-l-4 p-4 ' + (TASK[tt.status]?.[0] || ''), 'data-task': tt.member, 'data-task-status': tt.status },
      h('div', { class: 'flex items-center gap-2' }, avatar(tt.member_name, tt.member, 'size-7 text-[10px]'), h('b', { class: 'text-sm' }, tt.member_name),
        h('span', { class: 'muted ml-auto text-xs' }, TASK[tt.status]?.[1] + (tt.millis ? ' · ' + (tt.millis / 1000).toFixed(1) + ' s' : ''))),
      h('p', { class: 'muted mt-2 text-xs' }, h('b', {}, 'Task: '), tt.task),
      tt.output && h('div', { class: 'prose-sm mt-3 max-h-72 overflow-y-auto text-sm' }, renderReply(tt.output)),
      tt.error && h('p', { class: 'mt-2 text-xs text-rose-700 dark:text-rose-400' }, tt.error),
      tt.lookups?.length ? h('p', { class: 'muted mt-2 font-mono text-[11px]' }, 'Looked up: ' + tt.lookups.join(', ')) : null))) : null,
    r.answer && h('section', { class: 'card card-pad border-2 border-brand-200 dark:border-brand-500/40', id: 'team-answer' },
      h('h2', { class: 'h-section mb-2' }, 'The team’s answer, by ' + r.lead_name), h('div', { class: 'text-sm' }, renderReply(r.answer))),
    r.proposals?.length ? notice('warn', `${r.proposals.length} change${r.proposals.length === 1 ? '' : 's'} proposed: `, h('a', { href: '#/proposals', class: 'underline' }, 'review them in Approvals')) : null);
}
