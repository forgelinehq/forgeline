// Workflows › Schedules (design note 047): every workflow and team schedule in one list, with when each runs next, how its
// last time went, and pause, resume, edit and run now. A team's own page shows its schedules with the same drawer.

import { h, icon, mount, pageHeader, relTime, absTime, spinner, errorBox, toast, emptyState, drawer, field, confirmDialog } from '../lib/dom.js';
import { get, post, put, del, atLeast } from '../lib/api.js';
import { session } from '../app.js';
import { scheduleField } from '../lib/schedulefield.js';
import { describe } from '../lib/schedule.js';

const ROLE = { viewer: 'Viewer: reads only', developer: 'Developer: may propose changes, which an administrator approves' };
const when = (iso) => new Date(iso).toLocaleString(undefined, { weekday: 'short', day: 'numeric', month: 'short', hour: '2-digit', minute: '2-digit' });
const specOf = (it) => ({ every_minutes: it.every_minutes || 0, cron: it.cron || '', tz: it.tz || '' });

/** Adds or changes one of a team's schedules: a goal, when, and the role it runs as. */
export function teamScheduleDrawer(team, it, after) {
  drawer(it ? 'Change the schedule' : 'Schedule a goal', (close) => {
    let spec = it ? specOf(it) : { cron: '0 9 * * 1-5', tz: Intl.DateTimeFormat().resolvedOptions().timeZone || 'UTC' };
    const goal = h('textarea', { id: 'ts-goal', class: 'input', rows: '3', maxlength: '2000', placeholder: 'For example: Check last night\'s failed runs and say what they have in common.' }, it?.goal || '');
    const runAs = h('select', { id: 'ts-runas', class: 'input' }, Object.entries(ROLE).map(([v, l]) => h('option', { value: v, selected: (it?.run_as || 'viewer') === v }, l)));
    const paused = h('input', { type: 'checkbox', id: 'ts-paused', checked: !!it?.paused });
    const err = h('div', {});
    const save = h('button', { class: 'btn-primary', type: 'button', id: 'ts-save', onclick: async () => {
      save.disabled = true;
      const body = { goal: goal.value, ...spec, run_as: runAs.value, paused: paused.checked };
      try {
        await (it ? put('/v1/ai/team-schedules/' + it.id, body) : post('/v1/ai/teams/' + team.id + '/schedules', body));
        toast(it ? 'Schedule saved' : 'Scheduled', 'ok'); close(); after();
      } catch (e) { mount(err, errorBox(e.message)); save.disabled = false; }
    } }, it ? 'Save' : 'Schedule it');
    return h('div', { class: 'space-y-4' },
      h('p', { class: 'muted text-sm' }, 'At each time, ' + team.name + ' gets this goal, as if someone gave it: the lead plans, the members work, and the answer is kept with the team\'s runs.'),
      field('Goal', goal, null, 'ts-goal'),
      h('div', {}, h('div', { class: 'label' }, 'When'), scheduleField({ id: 'ts-when', spec, onChange: (s) => { spec = { every_minutes: s.every_minutes || 0, cron: s.cron || '', tz: s.tz || '' }; } })),
      field('Runs as', runAs, 'Nobody watches a scheduled run, so it never runs as an administrator. A change it proposes waits for an administrator to approve.', 'ts-runas'),
      it ? h('label', { class: 'flex items-center gap-2 text-sm' }, paused, 'Paused: skip its times until resumed') : null,
      err,
      h('div', { class: 'flex flex-wrap justify-between gap-2' },
        it ? h('button', { class: 'btn-ghost btn-sm text-rose-600 dark:text-rose-400', type: 'button', id: 'ts-delete', onclick: async () => {
          if (!await confirmDialog({ title: 'Delete this schedule?', body: 'The team stops getting this goal. Its past runs stay.', confirmLabel: 'Delete', danger: true })) return;
          try { await del('/v1/ai/team-schedules/' + it.id); toast('Deleted', 'ok'); close(); after(); } catch (e) { toast(e.message, 'error'); }
        } }, icon('trash', 'size-3.5'), 'Delete') : h('span', {}),
        h('div', { class: 'flex gap-2' }, h('button', { class: 'btn-secondary', type: 'button', onclick: () => close() }, 'Cancel'), save)));
  }, { subtitle: team.name });
}

/** One schedule's state in a few words: next time, paused, or why it does not run. */
export function nextLine(it) {
  if (it.off) return h('span', { class: 'text-amber-700 dark:text-amber-300' }, 'Not running: ' + it.off);
  if (it.paused) return h('span', { class: 'pill-idle' }, 'Paused');
  return it.next?.length ? h('span', { title: it.next.map(when).join('\n') }, when(it.next[0])) : h('span', { class: 'muted' }, '—');
}

export async function schedulesPage() {
  const admin = atLeast(session.role, 'admin');
  const el = h('div', {});
  async function load() {
    let data, teams = [];
    try { [data, teams] = await Promise.all([get('/v1/ai/schedules'), get('/v1/ai/teams').then(r => r.teams || [])]); } catch (e) { return mount(el, errorBox(e.message)); }
    const teamOf = (it) => teams.find(t => '/teams/' + t.id === it.target);
    const act = async (fn, ok) => { try { await fn(); toast(ok, 'ok'); load(); } catch (e) { toast(e.message, 'error'); } };
    const actions = (it) => {
      if (!admin) return null;
      const out = [];
      if (it.kind === 'workflow') {
        out.push(h('button', { class: 'btn-ghost btn-sm', type: 'button', 'data-pause': it.id, onclick: () => act(() => post('/v1/ai/workflows/' + it.id + '/schedule/' + (it.paused ? 'resume' : 'pause')), it.paused ? 'Resumed' : 'Paused') }, it.paused ? 'Resume' : 'Pause'),
          h('a', { class: 'btn-ghost btn-sm', href: '#/workflows/' + it.id + '/canvas' }, 'Change'),
          h('button', { class: 'btn-ghost btn-sm', type: 'button', onclick: () => act(() => post('/v1/ai/workflows/' + it.id + '/run'), 'Started') }, 'Run now'));
      } else {
        const t = teamOf(it);
        out.push(h('button', { class: 'btn-ghost btn-sm', type: 'button', 'data-pause': it.id, onclick: () => act(() => put('/v1/ai/team-schedules/' + it.id, { goal: it.goal, ...specOf(it), run_as: it.run_as, paused: !it.paused }), it.paused ? 'Resumed' : 'Paused') }, it.paused ? 'Resume' : 'Pause'),
          t ? h('button', { class: 'btn-ghost btn-sm', type: 'button', onclick: () => teamScheduleDrawer(t, it, load) }, 'Change') : null,
          h('button', { class: 'btn-ghost btn-sm', type: 'button', onclick: () => act(() => post('/v1/ai/teams/' + (t?.id || '') + '/runs', { goal: it.goal }), 'The team is on it') }, 'Run now'));
      }
      return h('div', { class: 'flex flex-wrap justify-end gap-1' }, out);
    };
    mount(el, pageHeader('Schedules', 'Everything that runs by itself: workflows on their schedule, and goals given to teams at set times. Times are shown in your time zone.'),
      data.schedules.length ? h('div', { class: 'card overflow-x-auto' }, h('table', { class: 'tbl', id: 'schedules' },
        h('thead', {}, h('tr', {}, ['What', 'When', 'Next', 'Last time', 'Runs as', ''].map(x => h('th', {}, x)))),
        h('tbody', {}, data.schedules.map(it => h('tr', { 'data-schedule': it.id, 'data-kind': it.kind },
          h('td', { class: 'max-w-sm' }, h('a', { class: 'font-medium hover:underline', href: '#' + it.target }, it.name), h('span', { class: 'muted ml-1.5 text-xs' }, it.kind === 'team' ? 'team' : 'workflow'),
            it.goal ? h('p', { class: 'muted truncate text-xs', title: it.goal }, it.goal) : null),
          h('td', { class: 'text-sm' }, describe(specOf(it))),
          h('td', { class: 'whitespace-nowrap text-sm' }, nextLine(it)),
          h('td', { class: 'text-sm' }, it.last_note ? h('span', { class: 'text-amber-700 dark:text-amber-300', title: it.last_note }, 'Skipped: ' + it.last_note)
            : it.last_at ? h('span', { title: absTime(it.last_at) }, relTime(it.last_at)) : h('span', { class: 'muted' }, 'not yet')),
          h('td', { class: 'text-sm capitalize' }, it.run_as),
          h('td', {}, actions(it)))))))
        : emptyState('clock', 'Nothing runs by itself yet', 'Give a workflow a schedule in its Settings, or schedule a goal on a team\'s page. Either can run every few hours, on weekdays at nine, or by a cron expression, in your time zone.'));
  }
  mount(el, spinner());
  await load();
  return el;
}
