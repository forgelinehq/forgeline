package server

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"testing"
)

// The plain-words labels for assistant steps (ui/lib/steplabel.js) and agent tools (ui/lib/tools.js)
// are DOM-free, so they run under Node. Skipped where Node is not installed.
const labelCases = `
import { stepLabel, stepsSummary } from './steplabel.mjs';
import { toolInfo, groupTools, plainHelp, TOOL_GROUPS, TIERS, tierTitle } from './tools.mjs';
import { NAMES } from './names.mjs';

let failed = 0;
const eq = (name, got, want) => {
  const g = JSON.stringify(got), w = JSON.stringify(want);
  if (g !== w) { failed++; console.error('FAIL ' + name + '\n  got  ' + g + '\n  want ' + w); }
};

// Steps read as sentences, with the useful arguments as a short note.
eq('done with details', stepLabel('aws_resources', '{"account":"primary","service":"sqs"}', 'done'), { text: 'Listed AWS resources', detail: 'primary · sqs' });
eq('running', stepLabel('aws_resources', '', 'running').text, 'Listing AWS resources');
eq('failed', stepLabel('aws_accounts', '{}', 'failed').text, 'Could not finish: checked which AWS accounts are connected');
eq('unknown tool', stepLabel('new_tool', '', 'done').text, 'Used new tool');
eq('bad args are ignored', stepLabel('get_run', 'not json', 'done').detail, '');
eq('long values are cut', stepLabel('github_files', JSON.stringify({ repo: 'x'.repeat(80) }), 'done').detail.length, 40);
eq('summary', [stepsSummary([{ status: 'done' }]), stepsSummary([{ status: 'done' }, { status: 'failed' }]), stepsSummary([{ status: 'running' }])],
  ['Looked at 1 thing', 'Looked at 2 things (1 could not finish)', 'Working']);

// Every tool the server offers has a plain title and a real group (not Other).
for (const n of NAMES) {
  const i = toolInfo(n);
  if (i.group === 'other') { failed++; console.error('FAIL tool ' + n + ' has no group'); }
  const s = stepLabel(n, '', 'done').text;
  if (s.startsWith('Used ')) { failed++; console.error('FAIL tool ' + n + ' has no step sentence'); }
}
eq('tools that ask a person', ['propose_aws_change', 'request_environment', 'aws_resources'].map(n => toolInfo(n).asks), [true, true, false]);
const g = groupTools([{ name: 'aws_resources' }, { name: 'list_runs' }, { name: 'mystery' }, { name: 'call_aws' }]);
eq('groups in catalogue order, empty ones left out', g.map(x => x.id), ['delivery', 'aws', 'other']);
eq('server order kept inside a group', g[1].tools.map(t => t.name), ['aws_resources', 'call_aws']);
eq('fields kept', g[0].tools[0].title, 'See recent runs');
eq('every group has a title', TOOL_GROUPS.every(x => x.title), true);
eq('tiers in words', [tierTitle('T0'), tierTitle('T1'), tierTitle('nope')], ['Advises', 'Proposes', 'Advises']);
eq('two tiers', TIERS.map(t => t.id), ['T0', 'T1']);
eq('schema dropped', plainHelp('{"account":"optional"} the AWS summary'), 'the AWS summary');
eq('nested braces in a schema', plainHelp('{"code":"params={})\\nresult"} read-only calls'), 'read-only calls');
eq('plain help kept', plainHelp('see recent runs'), 'see recent runs');
eq('long help cut', plainHelp('x'.repeat(200)).length, 140);

if (failed) process.exit(1);
console.log('ok');
`

func TestConsoleStepAndToolLabels(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node is not installed")
	}
	tmp := t.TempDir()
	for _, f := range []string{"steplabel", "tools"} {
		src, err := uiFiles.ReadFile("ui/lib/" + f + ".js")
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(tmp, f+".mjs"), src, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	// Every tool name the server can offer, so a new tool without words fails here.
	names := []string{}
	for _, l := range assistantLookups() {
		names = append(names, l.name)
	}
	names = append(names, toolProposeCloud)
	var b []byte
	b = append(b, "export const NAMES = ["...)
	for i, n := range names {
		if !regexp.MustCompile(`^[a-z0-9_]+$`).MatchString(n) {
			t.Fatalf("odd tool name %q", n)
		}
		if i > 0 {
			b = append(b, ',')
		}
		b = append(b, '"')
		b = append(b, n...)
		b = append(b, '"')
	}
	b = append(b, "];\n"...)
	if err := os.WriteFile(filepath.Join(tmp, "names.mjs"), b, 0o600); err != nil {
		t.Fatal(err)
	}
	test := filepath.Join(tmp, "cases.mjs")
	if err := os.WriteFile(test, []byte(labelCases), 0o600); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command(node, test).CombinedOutput(); err != nil {
		t.Fatalf("label cases failed:\n%s", out)
	}
}

// The words for a waiting change under approval rules (ui/lib/approval.js).
const approvalCases = `
import { approvalView } from './approval.mjs';
let failed = 0;
const eq = (name, got, want) => { const g = JSON.stringify(got), w = JSON.stringify(want); if (g !== w) { failed++; console.error('FAIL ' + name + '\n  got  ' + g + '\n  want ' + w); } };
const one = approvalView({ needed: 1, given: [] }, 'grace', 'dev');
eq('one approval: no extra line, approve and apply', [one.line, one.may, one.label, one.last], ['', true, 'Approve and apply', true]);
eq('no state is one approval', approvalView(null, 'grace', 'dev').label, 'Approve and apply');
const two = { needed: 2, given: [], rules: ['Prod'] };
eq('first of two', [approvalView(two, 'grace', 'dev').label, approvalView(two, 'grace', 'dev').last, approvalView(two, 'grace', 'dev').line], ['Approve (1 of 2)', false, 'Needs 2 approvals (Prod)']);
const half = { needed: 2, given: [{ by: 'grace' }], rules: ['Prod'] };
eq('second of two', [approvalView(half, 'lin', 'dev').label, approvalView(half, 'lin', 'dev').last], ['Approve and apply', true]);
eq('the same person again', [approvalView(half, 'grace', 'dev').may, approvalView(half, 'grace', 'dev').why], [false, 'You approved this. It needs another administrator.']);
eq('the proposer', approvalView(half, 'dev', 'dev').may, false);
eq('given line', approvalView(half, 'lin', 'dev').line, 'Needs 2 approvals · 1 given, by grace (Prod)');
const named = { needed: 1, given: [], approvers: ['lin'], rules: ['Leads'] };
eq('not named', [approvalView(named, 'grace', 'dev').may, approvalView(named, 'grace', 'dev').why], [false, 'Only lin may approve this change.']);
eq('named', approvalView(named, 'lin', 'dev').may, true);
eq('the rules leave nobody', [approvalView({ needed: 1, given: [], nobody: true }, 'lin', 'dev').may, approvalView({ needed: 1, given: [], nobody: true }, 'lin', 'dev').why], [false, 'The approval rules leave nobody who may approve this change.']);
eq('run word', approvalView(null, 'a', 'b', 'run').label, 'Approve and run');
if (failed) process.exit(1);
console.log('ok');
`

func TestConsoleApprovalWords(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node is not installed")
	}
	tmp := t.TempDir()
	src, err := uiFiles.ReadFile("ui/lib/approval.js")
	if err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(tmp, "approval.mjs"), src, 0o600)
	test := filepath.Join(tmp, "cases.mjs")
	os.WriteFile(test, []byte(approvalCases), 0o600)
	if out, err := exec.Command(node, test).CombinedOutput(); err != nil {
		t.Fatalf("approval cases failed:\n%s", out)
	}
}
