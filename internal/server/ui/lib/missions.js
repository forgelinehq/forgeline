// Forgeline web console: missions in words and stages (design note 037).
//
// No imports and no DOM, so it runs under Node in tests.

export const STAGES = [
  { key: 'discover', label: 'Discover', note: 'reads only' },
  { key: 'propose', label: 'Propose', note: 'files proposals' },
  { key: 'approval', label: 'Your OK', note: 'a different administrator' },
  { key: 'verify', label: 'Verify', note: 'looks again' },
  { key: 'report', label: 'Report', note: 'from the record' },
];

const ORDER = Object.fromEntries(STAGES.map((s, i) => [s.key, i]));

/**
 * Each stage's state for the live view: done, now, wait (waiting for people), failed, skipped or todo.
 * A stage the mission went past without working in it (no proposals, so no approval) is skipped.
 */
export function stageStates(m) {
  const at = ORDER[m.stage] ?? 0;
  const worked = new Set((m.steps || []).map(s => s.stage));
  const ended = ['done', 'failed', 'cancelled', 'stopped'].includes(m.status);
  return STAGES.map((st, i) => {
    let state = 'todo';
    if (i < at) state = worked.has(st.key) || (st.key === 'approval' && (m.proposals || []).length) ? 'done' : 'skipped';
    else if (i === at) {
      if (m.status === 'waiting') state = 'wait';
      else if (m.status === 'running') state = 'now';
      else if (m.status === 'done') state = 'done';
      else state = 'failed';
    } else if (ended) state = 'skipped';
    return { ...st, state };
  });
}

/** The board column a mission sits in. */
export function columnOf(m) {
  if (['done', 'failed', 'cancelled', 'stopped'].includes(m.status)) return 'ended';
  if (m.status === 'waiting') return 'waiting';
  return 'working';
}

export const COLUMNS = [
  { key: 'working', label: 'Working', note: 'discovering, proposing or verifying' },
  { key: 'waiting', label: 'Waiting for your OK', note: 'its proposals need an administrator' },
  { key: 'ended', label: 'Ended', note: 'done, stopped, failed or cancelled' },
];

/** The one line under a mission's goal on its card. */
export function missionLine(m) {
  const f = (m.findings || []).length, p = (m.proposals || []).length;
  const parts = [m.agent_name];
  if (m.status === 'running') parts.push(STAGES.find(s => s.key === m.stage)?.label.toLowerCase() + '…');
  else if (m.status === 'waiting') parts.push(p + (p === 1 ? ' proposal waits' : ' proposals wait'));
  else parts.push({ done: 'done', failed: 'failed', cancelled: 'cancelled', stopped: 'stopped: budget used up' }[m.status]);
  if (f) parts.push(f + (f === 1 ? ' finding' : ' findings'));
  return parts.join(' · ');
}

const SEV = { high: 0, medium: 1, low: 2, info: 3 };

/** Findings worst first, keeping their numbers (the plan and the evidence refer to them). */
export function findingsByRisk(m) {
  return (m.findings || []).map((f, i) => ({ ...f, n: i })).sort((a, b) => (SEV[a.severity] ?? 4) - (SEV[b.severity] ?? 4) || a.n - b.n);
}

/** What the mission did about a finding: the plan item and the evidence, if any. */
export function outcome(m, n) {
  return { plan: (m.plan || []).find(p => p.finding === n) || null, evidence: (m.evidence || []).filter(e => e.finding === n).pop() || null };
}
