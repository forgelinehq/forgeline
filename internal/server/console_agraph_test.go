package server

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// The agent graph canvas helpers (ui/lib/agraph.js) are plain functions, run under Node. Skipped where Node is missing.
const agraphCases = `
import { emptyGraph, addNode, updateNode, removeNode, addWire, removeWire, wireProblem, nextLabel, branchLabels, stateKeys,
  autoPlace, tidy, wirePath, graphRunStatus, stagesToGraph, nodeSummary } from './agraph.mjs';
let failed = 0;
const eq = (name, got, want) => { const g = JSON.stringify(got), w = JSON.stringify(want); if (g !== w) { failed++; console.error('FAIL ' + name + '\n  got  ' + g + '\n  want ' + w); } };

let g = emptyGraph();
const before = JSON.stringify(g);
let r = addNode(g, 'agent', { x: 300, y: 100 }, { agent: 'worker', task: 'look' });
eq('adding leaves the original alone', JSON.stringify(g), before);
g = r.graph;
eq('new ids by kind', [r.id, addNode(g, 'agent').id], ['agent', 'agent-2']);
g = removeWire(g, 'start', 'end');
g = addWire(addWire(g, 'start', 'agent'), 'agent', 'end');
eq('wires', g.edges, [{ from: 'start', to: 'agent' }, { from: 'agent', to: 'end' }]);
eq('no wire into the start', wireProblem(g, 'agent', 'start'), 'nothing wires into the start');
eq('no wire out of an end', wireProblem(g, 'end', 'agent'), 'nothing wires out of an end');
eq('no duplicate', wireProblem(g, 'start', 'agent'), 'already wired');
eq('a plain node takes no label', wireProblem(g, 'agent', 'end', 'yes'), 'only a decision or a question with choices has branches');

// A decision: labels from its rules plus else; wires need a label; a new wire gets the next free one.
r = addNode(g, 'decide', null, { by: 'rule', rules: [{ key: 'agent', op: 'contains', value: 'bad', when: 'page' }] });
g = r.graph;
eq('decision labels', branchLabels(g.nodes.find(n => n.id === 'decide')), ['page', 'else']);
eq('a decision wire needs a label', wireProblem(g, 'decide', 'end'), 'say which branch this wire is for');
eq('next free label', nextLabel(g, 'decide'), 'page');
g = addWire(g, 'decide', 'end', 'page');
eq('then else', nextLabel(g, 'decide'), 'else');
g = addWire(g, 'decide', 'agent', 'else');
eq('a label may fan out', wireProblem(g, 'decide', 'agent-x', 'page'), 'pick two nodes');

// Turning a decision into a plain kind drops labels; removing a node drops its wires and reads of it.
let g2 = updateNode(g, 'decide', { kind: 'merge', by: null, rules: null });
eq('labels dropped', g2.edges.filter(e => e.from === 'decide').map(e => e.when || ''), ['', '']);
g2 = updateNode(g, 'agent', { reads: ['decide'] });
g2 = removeNode(g2, 'decide');
eq('removed with its wires', g2.edges.some(e => e.from === 'decide' || e.to === 'decide'), false);
eq('reads of it dropped', g2.nodes.find(n => n.id === 'agent').reads, undefined);
eq('the start stays', removeNode(g, 'start').nodes.some(n => n.id === 'start'), true);
eq('state keys', stateKeys(updateNode(g, 'agent', { fields: ['severity'] }), 'end'), ['agent', 'decide', 'severity']);

// A loop wire goes round below.
eq('forward path', wirePath({ x: 0, y: 0 }, { x: 400, y: 0 }).startsWith('M200 32 C300'), true);
eq('loop path goes under', wirePath({ x: 400, y: 0 }, { x: 0, y: 0 }).includes('L0 92'), true);

// Run status from visits: the last visit wins, visits counted, the branch taken marks its wire.
const run = { status: 'running', visits: [
  { node: 'start', status: 'succeeded' }, { node: 'agent', status: 'succeeded' }, { node: 'decide', status: 'succeeded', branch: 'else' },
  { node: 'agent', status: 'running' } ] };
const st = graphRunStatus(g, run);
eq('statuses', ['start', 'agent', 'decide', 'end'].map(id => st.nodes[id].status), ['succeeded', 'running', 'succeeded', 'pending']);
eq('visits', st.nodes.agent.visits, 1);
eq('taken wires', [...st.taken].sort(), ['decide>agent>else', 'start>agent>']);
eq('cancelled', graphRunStatus(g, { ...run, status: 'cancelled' }).nodes.agent.status, 'cancelled');
eq('summary', nodeSummary({ kind: 'decide', by: 'rule', rules: [{ key: 'sev', op: 'equals', value: 'high' }] }), 'sev equals high');

// A stages workflow as a graph (the server checks it in Go).
const conv = stagesToGraph({ stages: [
  { name: 'Look', steps: [{ agent: 'a', agent_n: 1, task: 'runs' }, { agent: 'b', agent_n: 1, task: 'platform' }] },
  { name: 'Report', gate: true, steps: [{ agent: 'a', agent_n: 1, task: 'write' }] } ] });
eq('converted nodes', conv.nodes.map(n => n.id), ['start', 's1-1', 's1-2', 'merge-1', 'release-2', 'stopped-2', 's2-1', 'end']);
eq('every node placed', conv.nodes.every(n => n.x >= 0 && n.y >= 0 && (n.x || n.y)), true);
const after = addNode(g, 'lookup', null, {}, 'agent');
const ag = g.nodes.find(n => n.id === 'agent'), lk = after.graph.nodes.find(n => n.id === after.id);
eq('a node added after another goes to its right', [lk.x - ag.x, lk.y >= ag.y], [260, true]);
const t = tidy(g);
eq('tidy: columns by distance from the start', ['start', 'agent', 'decide', 'end'].map(id => t.nodes.find(n => n.id === id).x), [24, 284, 24, 544]); // the decision is not reachable from the start in this graph
eq('auto place keeps placed graphs', autoPlace(g).nodes.map(n => [n.x, n.y]), g.nodes.map(n => [n.x, n.y]));
if (failed) process.exit(1);
console.log(JSON.stringify(conv));
`

func TestConsoleAgentGraph(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node is not installed")
	}
	tmp := t.TempDir()
	src, err := uiFiles.ReadFile("ui/lib/agraph.js")
	if err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(tmp, "agraph.mjs"), src, 0o600)
	os.WriteFile(filepath.Join(tmp, "cases.mjs"), []byte(agraphCases), 0o600)
	out, err := exec.Command(node, filepath.Join(tmp, "cases.mjs")).CombinedOutput()
	if err != nil {
		t.Fatalf("agent graph cases failed: %v\n%s", err, out)
	}
	// The converted stages workflow is a graph the server accepts, once its agent names are real agents.
	m := newMCPHarness(t)
	a := newAgent(t, m.admin, agentBody("A", nil, nil, "T0"))
	b := newAgent(t, m.admin, agentBody("B", nil, nil, "T0"))
	text := strings.ReplaceAll(strings.ReplaceAll(strings.TrimSpace(string(out)), `"agent":"a"`, `"agent":"`+a.ID+`"`), `"agent":"b"`, `"agent":"`+b.ID+`"`)
	var g WFGraph
	if err := json.Unmarshal([]byte(text), &g); err != nil {
		t.Fatalf("%v: %s", err, out)
	}
	newWorkflow(t, m.admin, graphBody("Converted", g.Nodes, g.Edges))
}
