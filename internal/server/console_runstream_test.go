package server

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// The console's reading of the assistant's event stream: the frame parser (ui/lib/sse.js) and the
// run state built from events (ui/lib/runstate.js). Both are plain functions and run under Node.
// Skipped where Node is not installed.
const runStreamCases = `
import { sseParser } from './sse.mjs';
import { newRun, applyEvent, progressLabel } from './runstate.mjs';

let failed = 0;
const eq = (name, got, want) => {
  const g = JSON.stringify(got), w = JSON.stringify(want);
  if (g !== w) { failed++; console.error('FAIL ' + name + '\n  got  ' + g + '\n  want ' + w); }
};
const parse = (...chunks) => { const out = []; const p = sseParser(e => out.push(e)); chunks.forEach(c => p.push(c)); return out; };

// Frames.
eq('one frame', parse('data: {"type":"A"}\n\n'), [{ type: 'A' }]);
eq('two frames in one chunk', parse('data: {"type":"A"}\n\ndata: {"type":"B"}\n\n').map(e => e.type), ['A', 'B']);
eq('a frame split across chunks', parse('data: {"type":', '"A"}\n', '\n').map(e => e.type), ['A']);
eq('split inside the blank line', parse('data: {"type":"A"}\n', '\ndata: {"type":"B"}\n\n').map(e => e.type), ['A', 'B']);
eq('an unfinished frame is held back', parse('data: {"type":"A"}\n\ndata: {"type":"B"}').map(e => e.type), ['A']);
eq('CRLF', parse('data: {"type":"A"}\r\n\r\n').map(e => e.type), ['A']);
eq('keep-alive comments are ignored', parse(': keep-alive\n\ndata: {"type":"A"}\n\n').map(e => e.type), ['A']);
eq('a frame that is not JSON does not stop the stream', parse('data: not json\n\ndata: {"type":"A"}\n\n').map(e => e.type), ['A']);
eq('data that is not an object is ignored', parse('data: 5\n\ndata: [1]\n\ndata: null\n\n'), []);
eq('data without a space after the colon', parse('data:{"type":"A"}\n\n').map(e => e.type), ['A']);
eq('multi-line data is joined', parse('data: {"type":\ndata: "A"}\n\n').map(e => e.type), ['A']);
eq('other fields are ignored', parse('id: 5\nevent: x\ndata: {"type":"A"}\n\n').map(e => e.type), ['A']);

// The run, as the events build it.
const run = (events) => events.reduce((r, e) => applyEvent(r, e), newRun());
const call = (id, name) => [{ type: 'TOOL_CALL_START', toolCallId: id, toolCallName: name }, { type: 'TOOL_CALL_ARGS', toolCallId: id, delta: '{"a":1}' }, { type: 'TOOL_CALL_END', toolCallId: id }];
const done = (id, content, ok = true) => [{ type: 'TOOL_CALL_RESULT', toolCallId: id, content }, { type: 'CUSTOM', name: 'forgeline.tool_status', value: { toolCallId: id, ok } }];

eq('a new run is running and empty', newRun(), { status: 'running', context: '', steps: [], approvals: [], text: '', proposals: [], environments: [], memories: [], actions: [], plan: [], questions: [], permissions: [], autopilot: null, autoActions: [], memFiles: [], conversation: '', error: '' });
let r = run([{ type: 'CUSTOM', name: 'forgeline.context', value: { note: 'infrastructure' } }, ...call('c1', 'aws_script')]);
eq('a lookup in progress', [r.context, r.steps.map(s => [s.id, s.name, s.args, s.status])], ['infrastructure', [['c1', 'aws_script', '{"a":1}', 'running']]]);
eq('progress names the lookup', progressLabel(r), 'Looking up aws_script…');
r = run([...call('c1', 'aws_script'), ...done('c1', 'ok data')]);
eq('a finished lookup keeps its preview', r.steps.map(s => [s.status, s.result]), [['done', 'ok data']]);
eq('between lookups and the answer', progressLabel(r), 'Writing the answer…');
eq('before anything', progressLabel(newRun()), 'Thinking…');
r = run([{ type: 'CUSTOM', name: 'forgeline.permission', value: { id: 'p1', tool: 'call_aws', command: 'aws ec2 stop-instances --instance-ids i-1', account: 'primary', why: 'a change asks first', pattern: 'aws ec2 stop-instances *', expiresInSeconds: 300 } }]);
eq('a change waits for the person, with the exact command', [r.permissions.map(x => [x.id, x.command, x.pattern, x.state]), progressLabel(r)], [[['p1', 'aws ec2 stop-instances --instance-ids i-1', 'aws ec2 stop-instances *', 'waiting']], 'Waiting for your answer…']);
r = run([{ type: 'CUSTOM', name: 'forgeline.permission', value: { id: 'p1', command: 'x' } }, { type: 'CUSTOM', name: 'forgeline.permission', value: { id: 'p1', command: 'again' } }, { type: 'CUSTOM', name: 'forgeline.permission_decided', value: { id: 'p1', decision: 'always' } }]);
eq('a repeated permission is not a second one, and the answer is kept', r.permissions.map(x => [x.command, x.state]), [['x', 'always']]);
eq('an unknown decision is a no', run([{ type: 'CUSTOM', name: 'forgeline.permission', value: { id: 'p2', command: 'x' } }, { type: 'CUSTOM', name: 'forgeline.permission_decided', value: { id: 'p2', decision: 'whatever' } }]).permissions.map(x => x.state), ['deny']);
r = run([{ type: 'CUSTOM', name: 'forgeline.plan', value: { items: [{ text: 'Find clusters', status: 'done' }, { text: 'Read findings', status: 'doing' }, { text: 'Odd', status: 'finished' }, { text: '' }] } }]);
eq('a plan keeps valid items and unknown statuses become todo', r.plan, [{ text: 'Find clusters', status: 'done' }, { text: 'Read findings', status: 'doing' }, { text: 'Odd', status: 'todo' }]);
r = run([{ type: 'CUSTOM', name: 'forgeline.plan', value: { items: [{ text: 'a', status: 'todo' }] } }, { type: 'CUSTOM', name: 'forgeline.plan', value: { items: [{ text: 'b', status: 'doing' }] } }]);
eq('a plan update replaces the list', r.plan.map(i => i.text), ['b']);
r = run([...call('c1', 'ask_user'), { type: 'CUSTOM', name: 'forgeline.question', value: { id: 'q1', question: 'Which one?', options: ['A', 'B', 'C', 'D', 'E'], expiresInSeconds: 600 } }]);
eq('a question waits, with at most four options', [r.questions.map(q => [q.id, q.question, q.options, q.state]), progressLabel(r)], [[['q1', 'Which one?', ['A', 'B', 'C', 'D'], 'waiting']], 'Waiting for your answer…']);
r = run([{ type: 'CUSTOM', name: 'forgeline.question', value: { id: 'q1', question: 'Which one?', options: ['A'] } }, { type: 'CUSTOM', name: 'forgeline.question', value: { id: 'q1', question: 'Again' } }, { type: 'CUSTOM', name: 'forgeline.question_answered', value: { id: 'q1', answer: 'A' } }]);
eq('a repeated question is not a second one, and the answer is kept', r.questions.map(q => [q.question, q.state, q.answer]), [['Which one?', 'answered', 'A']]);
eq('no answer is an expired question', run([{ type: 'CUSTOM', name: 'forgeline.question', value: { id: 'q2', question: 'x' } }, { type: 'CUSTOM', name: 'forgeline.question_answered', value: { id: 'q2', answer: '' } }]).questions.map(q => q.state), ['expired']);
r = run([{ type: 'CUSTOM', name: 'forgeline.autopilot', value: { state: 'on', used: 2, steps: 100, untilMs: 5 } }, { type: 'CUSTOM', name: 'forgeline.autopilot', value: { state: 'on', used: 3, steps: 100, untilMs: 5, action: 'aws ecs update-service --service api', account: 'primary' } }]);
eq('autopilot shows where the envelope stands and each change it ran', [r.autopilot, r.autoActions], [{ state: 'on', used: 3, steps: 100, untilMs: 5 }, [{ command: 'aws ecs update-service --service api', account: 'primary' }]]);
eq('autopilot ending is shown', run([{ type: 'CUSTOM', name: 'forgeline.autopilot', value: { state: 'off', used: 100, steps: 100 } }]).autopilot.state, 'off');
eq('a memory file is shown with what happened to it', run([{ type: 'CUSTOM', name: 'forgeline.memory_file', value: { path: '/memories/me/a.md', action: 'create', pending: true } }, { type: 'CUSTOM', name: 'forgeline.memory_file', value: { path: '/memories/me/b.md', action: 'weird' } }, { type: 'CUSTOM', name: 'forgeline.memory_file', value: {} }]).memFiles, [{ path: '/memories/me/a.md', action: 'create', pending: true }, { path: '/memories/me/b.md', action: 'edit', pending: false }]);
eq('no autopilot event, no autopilot', newRun().autopilot, null);
r = run([...call('c1', 'search_audit'), ...done('c1', 'The lookup failed: not available to your role', false)]);
eq('a refused lookup is failed', r.steps.map(s => s.status), ['failed']);
r = run([...call('c1', 'a'), ...done('c1', 'x'), ...call('c2', 'b'), ...done('c2', 'y')]);
eq('a failed marker is not undone by a later result', run([...call('c1', 'a'), { type: 'CUSTOM', name: 'forgeline.tool_status', value: { toolCallId: 'c1', ok: false } }, { type: 'TOOL_CALL_RESULT', toolCallId: 'c1', content: 'x' }]).steps.map(s => s.status), ['failed']);
eq('lookups keep their order', r.steps.map(s => s.id), ['c1', 'c2']);
eq('a repeated start is not a second step', run([...call('c1', 'a'), ...call('c1', 'a')]).steps.length, 1);
eq('a result for an unknown call is ignored', run([{ type: 'TOOL_CALL_RESULT', toolCallId: 'zz', content: 'x' }]).steps, []);
r = run([{ type: 'TEXT_MESSAGE_START', messageId: 'm' }, { type: 'TEXT_MESSAGE_CONTENT', messageId: 'm', delta: 'Hello ' }, { type: 'TEXT_MESSAGE_CONTENT', messageId: 'm', delta: 'world' }, { type: 'TEXT_MESSAGE_END', messageId: 'm' }, { type: 'RUN_FINISHED' }]);
eq('answer pieces are joined', [r.text, r.status, progressLabel(r)], ['Hello world', 'finished', '']);
r = run([{ type: 'RUN_ERROR', message: 'the assistant took too long' }]);
eq('an error ends the run with its message', [r.status, r.error], ['error', 'the assistant took too long']);
r = run([{ type: 'CUSTOM', name: 'forgeline.proposal', value: { id: 'p1' } }, { type: 'CUSTOM', name: 'forgeline.proposal', value: { id: 'p1' } }, { type: 'CUSTOM', name: 'forgeline.environment', value: { id: 'e1' } }]);
eq('proposals and environments are listed once', [r.proposals, r.environments], [['p1'], ['e1']]);
eq('unknown events and custom names are ignored', run([{ type: 'STATE_SNAPSHOT' }, { type: 'CUSTOM', name: 'other.thing', value: {} }, {}]), newRun());
eq('a custom event without a value is safe', run([{ type: 'CUSTOM', name: 'forgeline.proposal' }]).proposals, []);

// Approvals: a question belongs to the call that is open, and is waiting until it is answered.
const ask = { type: 'CUSTOM', name: 'forgeline.approval', value: { id: 'a1', tool: 'gcp_resources', args: '{"service":"compute"}', integration: 'gcp', expiresInSeconds: 120 } };
const waiting = run([...call('c1', 'list_runs'), { type: 'TOOL_CALL_RESULT', toolCallId: 'c1', content: 'x' }, ...call('c2', 'gcp_resources'), ask]);
eq('a question is waiting', waiting.approvals.map(a => [a.id, a.tool, a.integration, a.state, a.step, a.seconds]), [['a1', 'gcp_resources', 'gcp', 'waiting', 'c2', 120]]);
eq('the label says it is waiting for the person', progressLabel(waiting), 'Waiting for your answer…');
eq('a repeated question is not shown twice', run([...call('c1', 'x'), ask, ask]).approvals.length, 1);
for (const d of ['approve', 'deny', 'expired']) eq('answered: ' + d, run([...call('c1', 'x'), ask, { type: 'CUSTOM', name: 'forgeline.approval_decided', value: { id: 'a1', decision: d } }]).approvals[0].state, d);
eq('an unknown decision counts as a denial', run([...call('c1', 'x'), ask, { type: 'CUSTOM', name: 'forgeline.approval_decided', value: { id: 'a1', decision: 'maybe' } }]).approvals[0].state, 'deny');
eq('an answer for another question changes nothing', run([...call('c1', 'x'), ask, { type: 'CUSTOM', name: 'forgeline.approval_decided', value: { id: 'zz', decision: 'approve' } }]).approvals[0].state, 'waiting');
eq('after the answer the label goes back to the lookup', progressLabel(run([...call('c1', 'x'), ask, { type: 'CUSTOM', name: 'forgeline.approval_decided', value: { id: 'a1', decision: 'approve' } }])), 'Looking up x…');
eq('a note is kept once', run([{ type: 'CUSTOM', name: 'forgeline.memory', value: { id: 'm1', text: 'Works in eu-west-1', about: 'me', status: 'active' } }, { type: 'CUSTOM', name: 'forgeline.memory', value: { id: 'm1', text: 'again' } }]).memories, [{ id: 'm1', text: 'Works in eu-west-1', about: 'me', status: 'active' }]);
eq('an odd note status counts as active only when it says so', run([{ type: 'CUSTOM', name: 'forgeline.memory', value: { id: 'm2', text: 'x', about: 'weird', status: 'whatever' } }]).memories[0], { id: 'm2', text: 'x', about: 'me', status: 'active' });
eq('an action card is kept once, with its effects', run([{ type: 'CUSTOM', name: 'forgeline.action', value: { id: 'a1', kind: 'save_pipeline', title: 'Save demo', effects: ['Saves a new pipeline "demo". It does not run.'], detail: 'name: demo', pipeline: 'demo' } }, { type: 'CUSTOM', name: 'forgeline.action', value: { id: 'a1', title: 'again' } }]).actions, [{ id: 'a1', kind: 'save_pipeline', title: 'Save demo', effects: ['Saves a new pipeline "demo". It does not run.'], detail: 'name: demo', pipeline: 'demo', done: false, result: '', link: '' }]);
eq('a card autopilot already confirmed is shown as done', run([{ type: 'CUSTOM', name: 'forgeline.action', value: { id: 'a2', kind: 'save_agent', title: 'Create agent X', done: true, result: 'Created agent "X".', link: '/agents/x' } }]).actions.map(a => [a.done, a.result, a.link]), [[true, 'Created agent "X".', '/agents/x']]);
eq('an action without an id is ignored', run([{ type: 'CUSTOM', name: 'forgeline.action', value: { title: 'x' } }]).actions, []);
eq('a note without an id is ignored', run([{ type: 'CUSTOM', name: 'forgeline.memory', value: { text: 'x' } }]).memories, []);
eq('the conversation id comes from the start of the run', run([{ type: 'RUN_STARTED', threadId: 'c0ffee' }]).conversation, 'c0ffee');
eq('the old fixed thread name is not a conversation', run([{ type: 'RUN_STARTED', threadId: 'assistant' }]).conversation, '');
eq('a question without an id is ignored', run([{ type: 'CUSTOM', name: 'forgeline.approval', value: { tool: 'x' } }]).approvals, []);

if (failed) process.exit(1);
console.log('ok');
`

func TestConsoleRunStream(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node is not installed")
	}
	tmp := t.TempDir()
	for _, f := range []string{"sse", "runstate"} {
		src, err := uiFiles.ReadFile("ui/lib/" + f + ".js")
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(tmp, f+".mjs"), src, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	test := filepath.Join(tmp, "cases.mjs")
	if err := os.WriteFile(test, []byte(runStreamCases), 0o600); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command(node, test).CombinedOutput(); err != nil {
		t.Fatalf("run stream cases failed:\n%s", out)
	}
}
