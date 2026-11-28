package server

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// confirmedRun starts workflow id from an assistant card and returns how to read what the card shows of it.
func confirmedRun(m *mcpHarness, id string) (string, func() RunViewJSON) {
	m.t.Helper()
	cards := actionsIn(m.run(m.admin, map[string]any{"message": "run " + id}, lookupReply("prepare_action", map[string]any{"action": "run_workflow", "workflow": id})))
	if len(cards) != 1 {
		m.t.Fatalf("cards = %v", cards)
	}
	card := cards[0]["id"].(string)
	var a AssistAction
	json.Unmarshal(m.admin.expect(200, "POST", "/v1/assistant/actions/"+card+"/confirm", nil), &a)
	return runIDOf(a), func() RunViewJSON {
		var v RunViewJSON
		json.Unmarshal(m.admin.expect(200, "GET", "/v1/assistant/actions/"+card+"/run", nil), &v)
		return v
	}
}

// drawn is a run graph as lines: each node with its label and status, and the wires the run went along.
func drawn(g *RunViewGraph) (nodes, taken []string) {
	if g == nil {
		return nil, nil
	}
	for _, n := range g.Nodes {
		nodes = append(nodes, n.ID+" "+n.Label+" "+n.Status)
	}
	for _, e := range g.Edges {
		if e.Taken {
			taken = append(taken, e.From+">"+e.To)
		}
	}
	return nodes, taken
}

func sameLines(t *testing.T, what string, got, want []string) {
	t.Helper()
	if !slices.Equal(got, want) {
		t.Errorf("%s:\n got %q\nwant %q", what, got, want)
	}
}

// A graph workflow's run card draws the version it ran: each node's status, and only the branch the answer took.
func TestARunCardDrawsAGraphRun(t *testing.T) {
	m := newMCPHarness(t)
	m.turnOn()
	a := newAgent(t, m.admin, agentBody("Worker", nil, nil, "T0"))
	nodes := []WFNode{{ID: "start", Kind: nodeStart}, {ID: "ok", Kind: nodeAsk, Name: "Go ahead?", Question: "Go ahead?", Choices: []string{"go", "stop"}},
		agentNode("work", a.ID, "do it"), {ID: "done", Kind: nodeEnd, Name: "Done"}, {ID: "halt", Kind: nodeEnd, Name: "Stopped", Outcome: wfFailed}}
	edges := []WFEdge{wire("start", "ok"), branchWire("ok", "work", "go"), branchWire("ok", "halt", "stop"), wire("work", "done")}
	w := newWorkflow(t, m.admin, graphBody("Ask", nodes, edges))
	run, view := confirmedRun(m, w.ID)
	m.waitRun(run, wfWaiting)
	got, taken := drawn(view().Graph)
	sameLines(t, "waiting", got, []string{"start Start succeeded", "ok Go ahead? waiting", "work Worker pending", "done Done pending", "halt Stopped pending"})
	sameLines(t, "waiting wires", taken, []string{"start>ok"})

	m.llm.script("SECRET OUTPUT")
	m.admin.expect(200, "POST", "/v1/ai/workflow-runs/"+run+"/answer", map[string]any{"answer": "go"})
	m.waitRun(run, wfSucceeded)
	v := view()
	got, taken = drawn(v.Graph)
	sameLines(t, "done", got, []string{"start Start succeeded", "ok Go ahead? succeeded", "work Worker succeeded", "done Done succeeded", "halt Stopped pending"})
	sameLines(t, "done wires", taken, []string{"start>ok", "ok>work", "work>done"})
	if b, _ := json.Marshal(v.Graph); strings.Contains(string(b), "SECRET") || strings.Contains(string(b), "do it") {
		t.Errorf("the graph carries more than names and statuses: %s", b)
	}

	// A newer version does not change how this run is drawn; a workflow that is gone, or made again, is not drawn at all.
	m.admin.expect(201, "POST", "/v1/ai/workflows/"+w.ID+"/versions", graphBody("Ask", []WFNode{{ID: "start", Kind: nodeStart}, {ID: "end", Kind: nodeEnd}}, []WFEdge{wire("start", "end")}))
	if g := view().Graph; g == nil || len(g.Nodes) != 5 {
		t.Errorf("after a new version: %+v", g)
	}
	ctx := t.Context()
	stored, err := m.srv.loadRun(ctx, run)
	if err != nil {
		t.Fatal(err)
	}
	stored.Hash = "another"
	if g := m.srv.workflowRunGraph(ctx, stored); g != nil {
		t.Errorf("a run of another workflow with this id was drawn: %+v", g)
	}

	// A cancelled run leaves nothing looking as if it were still being waited for.
	w2 := newWorkflow(t, m.admin, graphBody("Cancel", nodes, edges))
	run, view = confirmedRun(m, w2.ID)
	m.waitRun(run, wfWaiting)
	m.admin.expect(200, "POST", "/v1/ai/workflow-runs/"+run+"/cancel", nil)
	m.waitRun(run, wfCancelled)
	got, _ = drawn(view().Graph)
	if !slices.Contains(got, "ok Go ahead? cancelled") {
		t.Errorf("cancelled: %q", got)
	}
}

// A staged workflow's run card draws a column per stage, with the approval before a gated one.
func TestARunCardDrawsAStagedRun(t *testing.T) {
	m := newMCPHarness(t)
	m.turnOn()
	a := newAgent(t, m.admin, agentBody("Worker", nil, nil, "T0"))
	w := newWorkflow(t, m.admin, wfBody("Staged", []WFStage{
		{Name: "Look", Steps: []WFStep{step(a.ID, "left"), step(a.ID, "right")}},
		{Name: "Report", Gate: true, Steps: []WFStep{step(a.ID, "report")}},
	}))
	m.llm.script("L", "R", "REPORT")
	run, view := confirmedRun(m, w.ID)
	m.waitRun(run, wfWaiting)
	got, taken := drawn(view().Graph)
	sameLines(t, "waiting", got, []string{"start Start succeeded", "step-0-0 Worker succeeded", "step-0-1 Worker succeeded", "gate-1 Approve Report waiting", "step-1-0 Worker pending"})
	sameLines(t, "waiting wires", taken, []string{"start>step-0-0", "start>step-0-1", "step-0-0>gate-1", "step-0-1>gate-1"})
	if g := view().Graph; g == nil || len(g.Edges) != 5 {
		t.Errorf("wires: %+v", g)
	}

	m.admin.expect(200, "POST", "/v1/ai/workflow-runs/"+run+"/release", map[string]any{})
	m.waitRun(run, wfSucceeded)
	got, taken = drawn(view().Graph)
	sameLines(t, "done", got, []string{"start Start succeeded", "step-0-0 Worker succeeded", "step-0-1 Worker succeeded", "gate-1 Approve Report succeeded", "step-1-0 Worker succeeded"})
	if len(taken) != 5 {
		t.Errorf("done wires: %q", taken)
	}
}

// A wire counts as taken only out of something that succeeded, and nothing in a stage that will not run looks pending.
func TestRunGraphStatuses(t *testing.T) {
	name := func(id string) string { return id }
	g := WFGraph{Nodes: []WFNode{{ID: "ask", Kind: nodeAsk}, {ID: "end", Kind: nodeEnd}}, Edges: []WFEdge{wire("ask", "end")}}
	for _, status := range []string{wfWaiting, wfRunning, wfFailed} {
		_, taken := drawn(graphRunGraph(WFRun{Status: status, Visits: []WFNodeRun{{Node: "ask", Status: status}}}, g, name))
		sameLines(t, "wire out of a "+status+" node", taken, nil)
	}
	stage := func(status string, gate bool, steps ...string) WFStageRun {
		st := WFStageRun{Name: "S", Status: status, Gate: gate}
		for _, s := range steps {
			st.Steps = append(st.Steps, WFStepRun{Agent: "a", Status: s})
		}
		return st
	}
	got, taken := drawn(stageRunGraph(WFRun{Stages: []WFStageRun{stage(wfRunning, false, wfSucceeded, wfRunning), stage(wfPending, false, wfPending)}}, name))
	sameLines(t, "running", got, []string{"start Start succeeded", "step-0-0 a succeeded", "step-0-1 a running", "step-1-0 a pending"})
	sameLines(t, "running wires", taken, []string{"start>step-0-0", "start>step-0-1"})
	got, taken = drawn(stageRunGraph(WFRun{Stages: []WFStageRun{stage(wfSkipped, false, wfSucceeded, wfRunning), stage(wfSkipped, true, wfPending)}}, name))
	sameLines(t, "cancelled", got, []string{"start Start succeeded", "step-0-0 a succeeded", "step-0-1 a skipped", "gate-1 Approve S skipped", "step-1-0 a skipped"})
	sameLines(t, "cancelled wires", taken, []string{"start>step-0-0"})
	// A step that did not succeed leads nowhere, even into something already marked as run.
	_, taken = drawn(stageRunGraph(WFRun{Stages: []WFStageRun{stage(wfFailed, false, wfFailed), stage(wfFailed, false, wfFailed)}}, name))
	sameLines(t, "failed wires", taken, []string{"start>step-0-0"})
}

// The card's graph runs downwards, side by side for what runs together, with loops drawn beside it.
const runGraphCases = `
import { layoutRun, NODE_W, NODE_H } from './rungraph.mjs';

let failed = 0;
const eq = (name, got, want) => {
  const g = JSON.stringify(got), w = JSON.stringify(want);
  if (g !== w) { failed++; console.error('FAIL ' + name + '\n  got  ' + g + '\n  want ' + w); }
};
const n = (id, kind = 'agent') => ({ id, kind, label: id });
const e = (from, to) => ({ from, to });
const rows = (l) => { const ys = [...new Set(l.nodes.map(n => n.y))].sort((a, b) => a - b); return ys.map(y => l.nodes.filter(n => n.y === y).map(n => n.id)); };
const inside = (l) => l.nodes.every(n => n.x >= 0 && n.y >= 0 && n.x + NODE_W <= l.w && n.y + NODE_H <= l.h);
const apart = (l) => l.nodes.every((a, i) => l.nodes.every((b, j) => i === j || a.y !== b.y || Math.abs(a.x - b.x) >= NODE_W));

// A merge sits below the longest of its branches; the order the nodes are listed in does not matter.
const fan = layoutRun({ nodes: [n('end', 'end'), n('join'), n('a'), n('a2'), n('b'), n('start', 'start')],
  edges: [e('start', 'a'), e('start', 'b'), e('a', 'a2'), e('a2', 'join'), e('b', 'join'), e('join', 'end')] });
eq('levels', rows(fan), [['start'], ['a', 'b'], ['a2'], ['join'], ['end']]);
eq('inside and apart', [inside(fan), apart(fan)], [true, true]);
eq('one node is centred over two', fan.nodes.find(n => n.id === 'start').x, fan.nodes.find(n => n.id === 'a').x + (NODE_W + 12) / 2);
eq('no wire leads back', fan.edges.filter(x => x.back).length, 0);

// The same, with the branches met in the other order.
eq('levels, other order', rows(layoutRun({ nodes: [n('start', 'start'), n('a'), n('a2'), n('b'), n('join')],
  edges: [e('start', 'b'), e('start', 'a'), e('a', 'a2'), e('b', 'join'), e('a2', 'join')] }))[3], ['join']);
eq('levels, longer branch first', rows(layoutRun({ nodes: [n('start', 'start'), n('b'), n('a'), n('a2'), n('join')],
  edges: [e('start', 'a'), e('start', 'b'), e('a', 'a2'), e('a2', 'join'), e('b', 'join')] }))[3], ['join']);
// A loop is cut where it comes back, wherever the nodes are listed.
eq('loop listed backwards', rows(layoutRun({ nodes: [n('check'), n('work'), n('start', 'start')], edges: [e('start', 'work'), e('work', 'check'), e('check', 'work')] })), [['start'], ['work'], ['check']]);

// A loop neither pushes its own start down nor hangs the layout.
const loop = layoutRun({ nodes: [n('start', 'start'), n('work'), n('check', 'decide'), n('end', 'end')],
  edges: [e('start', 'work'), e('work', 'check'), { from: 'check', to: 'work', when: 'again' }, e('check', 'end'), e('work', 'work')] });
eq('loop levels', rows(loop), [['start'], ['work'], ['check'], ['end']]);
eq('loop wires', loop.edges.filter(x => x.back).map(x => x.from + '>' + x.to), ['check>work', 'work>work']);
eq('the label comes along', loop.edges[2].when, 'again');
eq('room beside the graph for the loop and its label', [loop.w >= loop.edges[2].lx + 50, loop.edges[2].lx > NODE_W + 8], [true, true]);
// A loop under a wider level still runs outside that level.
const wide = layoutRun({ nodes: [n('start', 'start'), n('a'), n('b'), n('c'), n('check')], edges: [e('start', 'a'), e('a', 'b'), e('a', 'c'), e('b', 'check'), e('check', 'a')] });
const outside = Number(/H([\d.]+) V/.exec(wide.edges[4].d)[1]);
eq('beside every node', wide.nodes.every(x => x.x + NODE_W < outside), true);

eq('a node nothing leads to, and a wire to nowhere', rows(layoutRun({ nodes: [n('start', 'start'), n('lost')], edges: [e('start', 'gone')] })), [['start', 'lost']]);
eq('nothing to draw', layoutRun({}).nodes, []);
if (failed) process.exit(1);
`

func TestRunGraphLayout(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node is not installed")
	}
	src, err := uiFiles.ReadFile("ui/lib/rungraph.js")
	if err != nil {
		t.Fatal(err)
	}
	tmp := t.TempDir()
	if err := os.WriteFile(filepath.Join(tmp, "rungraph.mjs"), src, 0o600); err != nil {
		t.Fatal(err)
	}
	test := filepath.Join(tmp, "cases.mjs")
	if err := os.WriteFile(test, []byte(runGraphCases), 0o600); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command(node, test).CombinedOutput(); err != nil {
		t.Fatalf("run graph layout cases failed:\n%s", out)
	}
}
