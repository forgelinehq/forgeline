package server

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// The workflow graph's layout (ui/lib/wfgraph.js) is plain functions, run under Node. Skipped where Node is missing.
const wfGraphCases = `
import { layoutWorkflow, edgePath, runStatuses, edgeLive, NODE_W, NODE_H } from './wfgraph.mjs';
let failed = 0;
const eq = (name, got, want) => { const g = JSON.stringify(got), w = JSON.stringify(want); if (g !== w) { failed++; console.error('FAIL ' + name + '\n  got  ' + g + '\n  want ' + w); } };

const v = { every_minutes: 1440, stages: [
  { name: 'Look', steps: [{ agent: 'a', task: 'read runs' }, { agent: 'b', task: 'check platform' }] },
  { name: 'Report', gate: true, steps: [{ agent: 'lead', task: 'write it' }] },
] };
const L = layoutWorkflow(v, (id) => id.toUpperCase());
eq('columns: trigger, two steps, approval, one step', L.nodes.map(n => [n.id, n.kind, n.col]),
  [['trigger', 'trigger', 0], ['step-0-0', 'step', 1], ['step-0-1', 'step', 1], ['gate-1', 'gate', 2], ['step-1-0', 'step', 3]]);
eq('a schedule says so', [L.nodes[0].label, L.nodes[0].sub], ['On a schedule', 'every day']);
eq('steps are labelled by agent', L.nodes[1].label, 'A');
eq('every node of a column joins every node of the next', L.edges.map(e => e.from + '>' + e.to),
  ['trigger>step-0-0', 'trigger>step-0-1', 'step-0-0>gate-1', 'step-0-1>gate-1', 'gate-1>step-1-0']);
eq('a column is centred on the tallest', L.nodes[0].y + NODE_H / 2, (L.nodes[1].y + L.nodes[2].y + NODE_H) / 2);
eq('nothing overlaps in a column', L.nodes[2].y >= L.nodes[1].y + NODE_H, true);
eq('the drawing holds every node', L.nodes.every(n => n.x + NODE_W <= L.w && n.y + NODE_H <= L.h), true);
eq('an edge runs right to left middles', edgePath({ x: 0, y: 0 }, { x: 260, y: 0 }), 'M196 32 C228 32, 228 32, 260 32');
eq('by hand', layoutWorkflow({ stages: [{ name: 's', steps: [{ agent: 'x', task: '' }] }] }).nodes[0].label, 'Started by hand');

// A run waiting at the gate.
const run = { stages: [
  { status: 'succeeded', steps: [{ status: 'succeeded' }, { status: 'failed' }] },
  { status: 'waiting', gate: true, steps: [{ status: 'pending' }] },
] };
const s = runStatuses(L, run);
eq('statuses', [s.trigger, s['step-0-0'], s['step-0-1'], s['gate-1'], s['step-1-0']], ['succeeded', 'succeeded', 'failed', 'waiting', 'pending']);
eq('the edge into the waiting approval moves', edgeLive(s, { from: 'step-0-0', to: 'gate-1' }), true);
eq('not from a failed step', edgeLive(s, { from: 'step-0-1', to: 'gate-1' }), false);
const released = runStatuses(L, { stages: [run.stages[0], { status: 'running', gate: true, gate_by: 'ada', steps: [{ status: 'running' }] }] });
eq('once released the approval is done and the step works', [released['gate-1'], released['step-1-0']], ['succeeded', 'running']);
eq('a stage the run has not reached is pending', runStatuses(L, { stages: [] })['step-1-0'], 'pending');
if (failed) process.exit(1);
`

func TestConsoleWorkflowGraph(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node is not installed")
	}
	tmp := t.TempDir()
	src, err := uiFiles.ReadFile("ui/lib/wfgraph.js")
	if err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(tmp, "wfgraph.mjs"), src, 0o600)
	test := filepath.Join(tmp, "cases.mjs")
	os.WriteFile(test, []byte(wfGraphCases), 0o600)
	if out, err := exec.Command(node, test).CombinedOutput(); err != nil {
		t.Fatalf("workflow graph cases failed: %v\n%s", err, out)
	}
}
