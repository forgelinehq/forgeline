// Forgeline web console: the AI team as an org chart (note 029, frame 5).
//
// No imports and no DOM, so it runs under Node in tests. It turns GET /v1/ai/team into a tree, says each
// agent's state in words, and turns a workflow run into the steps of a mission strip.

/** members → the agents that report to people, each with `reports` (its own reports, sorted by name). */
export function buildTree(members) {
  const byId = new Map((members || []).map(m => [m.id, { ...m, reports: [] }]));
  const roots = [];
  for (const m of byId.values()) {
    const lead = m.reports_to && m.reports_to !== m.id ? byId.get(m.reports_to) : null;
    (lead ? lead.reports : roots).push(m);
  }
  // A loop the server did not catch would leave its agents out of every root's tree: cut the loop where it is
  // found, and hang that agent on top.
  const seen = new Set();
  const walk = m => { seen.add(m.id); m.reports = m.reports.filter(r => !seen.has(r.id)); m.reports.forEach(walk); };
  roots.forEach(walk);
  for (const m of byId.values()) {
    if (seen.has(m.id)) continue;
    const lead = byId.get(m.reports_to);
    if (lead) lead.reports = lead.reports.filter(r => r.id !== m.id);
    roots.push(m);
    walk(m);
  }
  const sort = list => { list.sort((a, b) => a.name.localeCompare(b.name)); list.forEach(m => sort(m.reports)); return list; };
  return sort(roots);
}

const pad = n => String(n).padStart(2, '0');
function when(iso, now) {
  const t = new Date(iso), n = new Date(now);
  const mins = Math.round((t - n) / 60000);
  if (mins <= 1) return 'any minute';
  if (mins < 60) return 'in ' + mins + ' min';
  const hm = pad(t.getHours()) + ':' + pad(t.getMinutes());
  return t.toDateString() === n.toDateString() ? 'at ' + hm : t.toLocaleDateString(undefined, { weekday: 'short' }) + ' ' + hm;
}

export function every(min) {
  if (!min) return '';
  if (min % 1440 === 0) return min === 1440 ? 'daily' : 'every ' + min / 1440 + ' days';
  if (min % 60 === 0) return min === 60 ? 'hourly' : 'every ' + min / 60 + ' hours';
  return 'every ' + min + ' min';
}

/** The state line on a card: { text, tone } where tone is work, wait, sleep, idle or off. */
export function stateLine(m, now = Date.now()) {
  // An agent that used up its budget answers nothing until next month or a higher budget (note 036).
  if (m.budget?.level === 'stop' && m.state !== 'off' && m.state !== 'working') return { text: 'Stopped: its monthly budget is used up', tone: 'off' };
  switch (m.state) {
    case 'working': return { text: 'Working on ' + (m.doing || 'a workflow'), tone: 'work' };
    case 'waiting': return { text: 'Waiting for your OK in ' + (m.doing || 'a workflow'), tone: 'wait' };
    case 'scheduled': return { text: 'Asleep · wakes ' + when(m.next_at, now) + (m.every_minutes ? ' (' + every(m.every_minutes) + ')' : ''), tone: 'sleep' };
    case 'off': return { text: 'Off', tone: 'off' };
    default: return { text: 'Waits to be asked', tone: 'idle' };
  }
}

/** The small print: what it may do and what it did this week. */
export function factsLine(m) {
  const parts = [m.mode === 'proposes' ? 'Proposes changes' : 'Advises'];
  if (m.steps_week) parts.push(m.steps_week + (m.steps_week === 1 ? ' task' : ' tasks') + ' this week' + (m.failed_week ? ', ' + m.failed_week + ' failed' : ''));
  return parts.join(' · ');
}

/** A run's stages as the steps of a mission: { name, state (done, now, wait, failed, todo, skipped), note }. */
export function missionSteps(run) {
  return (run?.stages || []).map(st => {
    const steps = st.steps || [];
    const failed = steps.filter(s => s.status === 'failed').length;
    if (st.gate && st.status === 'waiting') return { name: st.name, state: 'wait', note: 'waiting for you' };
    switch (st.status) {
      case 'succeeded': return { name: st.name, state: failed ? 'failed' : 'done', note: failed ? failed + ' of ' + steps.length + ' failed' : (steps.length === 1 ? 'done' : steps.length + ' done') };
      case 'running': return { name: st.name, state: 'now', note: 'working' };
      case 'failed': return { name: st.name, state: 'failed', note: 'failed' };
      case 'skipped': case 'cancelled': return { name: st.name, state: 'skipped', note: st.status };
      default: return { name: st.name, state: 'todo', note: st.gate ? 'needs your OK' : '' };
    }
  });
}
