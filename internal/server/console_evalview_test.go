package server

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// Eval checks in words (design note 050): every kind the server knows has a label, chips read well, and a check is
// sent with only the inputs its kind uses.
const evalViewCases = `import { CHECKS, describeCheck, cleanCheck, scoreTone, attemptLine } from './evals.mjs';
let failed = 0;
const eq = (name, got, want) => { const g = JSON.stringify(got), w = JSON.stringify(want); if (g !== w) { failed++; console.error('FAIL ' + name + '\n  got  ' + g + '\n  want ' + w); } };
eq('kinds match the server', CHECKS.map(c => c.kind).sort(), ['contains', 'field', 'json', 'judge', 'max_seconds', 'max_tokens', 'max_tool_calls', 'not_contains', 'regex', 'tool_called', 'tool_not_called']);
eq('chips', [describeCheck({ kind: 'tool_not_called', value: 'propose_aws_change' }), describeCheck({ kind: 'field', key: 'cause', value: 'timeout' }), describeCheck({ kind: 'max_tool_calls', n: 3 })],
  ['never calls propose_aws_change', 'cause = timeout', '≤ 3 tool calls']);
eq('clean keeps only what the kind uses', [cleanCheck({ kind: 'contains', value: ' x ', key: 'k', n: 4 }), cleanCheck({ kind: 'max_tokens', value: 'v', n: '1500.4' }), cleanCheck({ kind: 'tool_called', value: 't', key: '' })],
  [{ kind: 'contains', value: 'x' }, { kind: 'max_tokens', n: 1500 }, { kind: 'tool_called', value: 't' }]);
eq('tones', [scoreTone(2, 2), scoreTone(1, 2), scoreTone(0, 2), scoreTone(0, 0)], ['ok', 'wait', 'bad', 'idle']);
eq('attempt line', attemptLine({ tools: ['list_runs', 'propose_aws_change'], recorded: ['propose_aws_change'], tokens: 1500, millis: 2300 }),
  'called list_runs, propose_aws_change · would have run propose_aws_change (recorded, not carried out) · 1500 tokens · 2.3 s');
if (failed) process.exit(1);
`

func TestConsoleEvalView(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node is not installed")
	}
	tmp := t.TempDir()
	src, _ := uiFiles.ReadFile("ui/lib/evals.js")
	os.WriteFile(filepath.Join(tmp, "evals.mjs"), src, 0o600)
	test := filepath.Join(tmp, "cases.mjs")
	os.WriteFile(test, []byte(evalViewCases), 0o600)
	if out, err := exec.Command(node, test).CombinedOutput(); err != nil {
		t.Fatalf("eval view cases failed:\n%s", out)
	}
}
