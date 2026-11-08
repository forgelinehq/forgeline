// Forgeline web console: what the approval rules mean for one waiting change (design note 032).
//
// No imports and no DOM, so it runs under Node in tests. It only words what the server says in a change's
// `approval` field; the server decides who may approve and refuses everyone else.

/**
 * state: the change's `approval` ({ needed, given: [{by, at, note}], approvers, rules }) or null.
 * me: the person looking; proposer: who proposed the change.
 * Returns { line, may, why, label, last }:
 *   line  what to show ("Needs 2 approvals · 1 given, by grace"), or '' when one approval is the usual
 *   may   whether this person's approval would count (the server checks again)
 *   why   when may is false, why not, in words
 *   label the Approve button's words
 *   last  whether this approval would make the change run
 */
export function approvalView(state, me, proposer, runWord = 'apply') {
  const needed = state?.needed || 1;
  const given = state?.given || [];
  const approvers = state?.nobody ? [] : (state?.approvers || null);
  const rules = state?.rules || [];
  const left = Math.max(needed - given.length, 0);
  const last = left <= 1;
  let line = '';
  if (needed > 1 || approvers) {
    line = 'Needs ' + needed + (needed === 1 ? ' approval' : ' approvals');
    if (given.length) line += ' · ' + given.length + ' given, by ' + given.map(v => v.by).join(', ');
    if (approvers) line += approvers.length ? ' · only ' + approvers.join(', ') + ' may approve' : ' · the rules leave nobody who may approve';
    if (rules.length) line += ' (' + rules.join(', ') + ')';
  }
  let may = true, why = '';
  if (me === proposer) { may = false; why = 'You proposed this, so someone else must approve it.'; }
  else if (given.some(v => v.by === me)) { may = false; why = 'You approved this. It needs another administrator.'; }
  else if (approvers && !approvers.includes(me)) {
    may = false;
    why = approvers.length ? 'Only ' + approvers.join(', ') + ' may approve this change.' : 'The approval rules leave nobody who may approve this change.';
  }
  const label = last ? 'Approve and ' + runWord : 'Approve (' + (given.length + 1) + ' of ' + needed + ')';
  return { line, may, why, label, last };
}
