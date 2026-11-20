// Forgeline web console: eval checks in words (design note 050). No DOM, so it runs under Node in tests.

/** Every kind of check: its label, icon, which inputs it takes, and placeholders. */
export const CHECKS = [
  { kind: 'contains', label: 'Answer contains', icon: 'check', value: 'Text', place: 'timeout' },
  { kind: 'not_contains', label: 'Answer does not contain', icon: 'x', value: 'Text', place: 'AKIA' },
  { kind: 'regex', label: 'Answer matches', icon: 'code', value: 'Regular expression', place: '^(yes|no)\\b' },
  { kind: 'json', label: 'Answer is JSON', icon: 'variable', value: 'Keys it must have (optional, comma separated)', place: 'severity, service', optional: true },
  { kind: 'field', label: 'Field equals', icon: 'output', key: 'Field', value: 'Value', place: 'high' },
  { kind: 'tool_called', label: 'Calls a tool', icon: 'tool', value: 'Tool', key: 'With this argument (optional)', place: 'list_runs', optionalKey: true },
  { kind: 'tool_not_called', label: 'Never calls a tool', icon: 'shield', value: 'Tool', place: 'propose_aws_change' },
  { kind: 'max_tool_calls', label: 'At most N tool calls', icon: 'limits', n: 'Tool calls' },
  { kind: 'max_tokens', label: 'At most N tokens', icon: 'usage', n: 'Tokens' },
  { kind: 'max_seconds', label: 'At most N seconds', icon: 'clock', n: 'Seconds' },
  { kind: 'judge', label: 'Judge with a rubric', icon: 'eval', value: 'Rubric: what a passing answer does', place: 'It names the failed step and suggests one next step. It does not claim to have changed anything.', long: true },
];

export const checkMeta = (kind) => CHECKS.find(c => c.kind === kind) || { kind, label: kind, icon: 'check' };

/** One check in a short phrase, for chips. */
export function describeCheck(c) {
  switch (c.kind) {
    case 'contains': return 'contains “' + c.value + '”';
    case 'not_contains': return 'never says “' + c.value + '”';
    case 'regex': return 'matches /' + c.value + '/';
    case 'json': return c.value ? 'JSON with ' + c.value : 'is JSON';
    case 'field': return c.key + ' = ' + c.value;
    case 'tool_called': return 'calls ' + c.value + (c.key ? ' with ' + c.key : '');
    case 'tool_not_called': return 'never calls ' + c.value;
    case 'max_tool_calls': return '≤ ' + (c.n || 0) + ' tool calls';
    case 'max_tokens': return '≤ ' + (c.n || 0) + ' tokens';
    case 'max_seconds': return '≤ ' + (c.n || 0) + ' s';
    case 'judge': return 'judge: ' + (c.value.length > 40 ? c.value.slice(0, 40) + '…' : c.value);
  }
  return c.kind;
}

/** A check as the server takes it: only the inputs its kind uses, trimmed. */
export function cleanCheck(c) {
  const m = checkMeta(c.kind);
  const out = { kind: c.kind };
  if (m.value) out.value = String(c.value || '').trim();
  if (m.key) out.key = String(c.key || '').trim();
  if (m.n) out.n = Math.max(0, Math.round(Number(c.n) || 0));
  if (!out.key) delete out.key;
  return out;
}

/** How a score looks: all passed, some, none, or not run. */
export function scoreTone(passed, total) {
  if (!total) return 'idle';
  if (passed === total) return 'ok';
  if (passed === 0) return 'bad';
  return 'wait';
}

/** A short line for an attempt: what it called and what it would have changed. */
export function attemptLine(a) {
  const parts = [];
  if (a.tools && a.tools.length) parts.push('called ' + a.tools.join(', '));
  if (a.recorded && a.recorded.length) parts.push('would have ' + a.recorded.map(t => 'run ' + t).join(', ') + ' (recorded, not carried out)');
  if (a.tokens) parts.push(a.tokens + ' tokens');
  if (a.millis) parts.push((a.millis / 1000).toFixed(1) + ' s');
  return parts.join(' · ');
}
