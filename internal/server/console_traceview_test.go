package server

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// How a trace is drawn (design note 049): parents before children with their depth, a look per kind of span, and
// durations, tokens and cost in words.
const traceViewCases = `import { spanTree, spanLook, fmtMillis, fmtTokens, fmtCost } from './traceview.mjs';
let failed = 0;
const eq = (name, got, want) => { const g = JSON.stringify(got), w = JSON.stringify(want); if (g !== w) { failed++; console.error('FAIL ' + name + '\n  got  ' + g + '\n  want ' + w); } };
const t = (s) => '2026-11-20T10:00:0' + s + 'Z';
const spans = [
  { span_id: 'c', parent_id: 'a', name: 'tool', start: t(2), end: t(3), attrs: { 'gen_ai.operation.name': 'execute_tool' } },
  { span_id: 'a', name: 'root', start: t(0), end: t(5), attrs: { 'gen_ai.operation.name': 'invoke_agent' } },
  { span_id: 'b', parent_id: 'a', name: 'chat', start: t(1), end: t(2), attrs: { 'gen_ai.operation.name': 'chat' } },
  { span_id: 'd', parent_id: 'gone', name: 'orphan', start: t(4), end: t(4), attrs: {} },
];
eq('tree order and depth', spanTree(spans).map(r => r.span.name + ':' + r.depth), ['root:0', 'chat:1', 'tool:1', 'orphan:0']);
eq('looks', spans.map(s => spanLook(s).label), ['Tool call', 'Agent run', 'Model call', 'Agent run']);
eq('node look', spanLook({ attrs: { 'forgeline.node.kind': 'ask' } }).icon, 'ask');
eq('trace kinds', [spanLook({ kind: 'workflow' }).label, spanLook({ kind: 'assistant' }).label], ['Workflow', 'Assistant']);
eq('slice 3 kinds win over the operation', [spanLook({ attrs: { 'forgeline.trace.kind': 'mission', 'gen_ai.operation.name': 'invoke_workflow' } }).label, spanLook({ kind: 'team' }).label, spanLook({ kind: 'pipeline' }).label], ['Mission', 'Team run', 'Pipeline run']);
eq('steps and stages', [spanLook({ attrs: { 'forgeline.step.id': 'build' } }).label, spanLook({ attrs: { 'forgeline.mission.stage': 'propose' } }).label], ['Step', 'Stage (propose)']);
eq('millis', [5, 1500, 12000, 61000, 3720000].map(fmtMillis), ['5 ms', '1.5 s', '12 s', '1 min 1 s', '1 h 2 min']);
eq('tokens', [12, 1500, 2000000].map(fmtTokens), ['12', '1.5k', '2M']);
eq('cost', [0, 0.004, 1.234].map(fmtCost), ['—', '<$0.01', '$1.23']);
if (failed) process.exit(1);
`

func TestConsoleTraceView(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node is not installed")
	}
	tmp := t.TempDir()
	src, _ := uiFiles.ReadFile("ui/lib/traceview.js")
	os.WriteFile(filepath.Join(tmp, "traceview.mjs"), src, 0o600)
	test := filepath.Join(tmp, "cases.mjs")
	os.WriteFile(test, []byte(traceViewCases), 0o600)
	if out, err := exec.Command(node, test).CombinedOutput(); err != nil {
		t.Fatalf("trace view cases failed:\n%s", out)
	}
}
