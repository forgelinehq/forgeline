// Forgeline web console: what an assistant run looks like so far, built from its events.
// No imports and no DOM, so it runs under Node in tests. The events follow the AG-UI vocabulary
// (design note 024); anything not listed here is ignored, so a newer server cannot break the panel.

export function newRun() {
  return { status: 'running', context: '', steps: [], text: '', proposals: [], environments: [], error: '' };
}

const step = (run, id) => run.steps.find(s => s.id === id);

/** Returns the run after one more event. The run is changed in place and also returned. */
export function applyEvent(run, ev) {
  switch (ev.type) {
    case 'CUSTOM': {
      const v = ev.value || {};
      if (ev.name === 'forgeline.context') run.context = String(v.note || '');
      else if (ev.name === 'forgeline.tool_status') { const s = step(run, v.toolCallId); if (s) s.status = v.ok === false ? 'failed' : 'done'; }
      else if (ev.name === 'forgeline.proposal' && v.id && !run.proposals.includes(v.id)) run.proposals.push(String(v.id));
      else if (ev.name === 'forgeline.environment' && v.id && !run.environments.includes(v.id)) run.environments.push(String(v.id));
      break;
    }
    case 'TOOL_CALL_START':
      if (!step(run, ev.toolCallId)) run.steps.push({ id: ev.toolCallId, name: String(ev.toolCallName || ''), args: '', result: '', status: 'running' });
      break;
    case 'TOOL_CALL_ARGS': { const s = step(run, ev.toolCallId); if (s) s.args += String(ev.delta || ''); break; }
    case 'TOOL_CALL_RESULT': { const s = step(run, ev.toolCallId); if (s) { s.result = String(ev.content || ''); if (s.status === 'running') s.status = 'done'; } break; }
    case 'TEXT_MESSAGE_CONTENT': run.text += String(ev.delta || ''); break;
    case 'RUN_FINISHED': run.status = 'finished'; break;
    case 'RUN_ERROR': run.status = 'error'; run.error = String(ev.message || 'The assistant failed.'); break;
  }
  return run;
}

/** What to tell the person while nothing has arrived yet. */
export function progressLabel(run) {
  if (run.status !== 'running') return '';
  const open = run.steps.filter(s => s.status === 'running');
  if (open.length) return 'Looking up ' + open[open.length - 1].name + '…';
  return run.steps.length ? 'Writing the answer…' : 'Thinking…';
}
