package server

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// An agent's instructions as sections (design note 048): reading Markdown into sections and writing it back gives the
// same text, so switching between sections and Markdown never changes what the model receives.
const instructionCases = `import { parse, compose, withStandard, addSection, addCustom, listItems, listBody } from './instructions.mjs';
let f = 0;
const eq = (n, g, w) => { const a = JSON.stringify(g), b = JSON.stringify(w); if (a !== b) { f++; console.log('FAIL', n, '\n got ', a, '\n want', b); } };
const samples = [
  'You explain failed pipeline runs in plain words.\n\n- Say what failed.\n- Say the likely cause.',
  '## Goal\nFind idle resources.\n\n## Rules\n- Look first.\n- Never delete.',
  'Intro\n\n## Notes\nfree text\n\n~~~\n## not a heading\n~~~\n\n## Output format\nJSON',
  '## Steps\n1. a\n2. b\n\n## Steps\nagain',
  '',
];
for (const s of samples) eq('round trip: ' + s.slice(0, 20), compose(parse(s)), s.replace(/\n{3,}/g, '\n\n').trim());
const p = parse(samples[2]);
eq('ids', p.map(s => s.id), ['role', 'custom', 'output']);
eq('heading kept as written', p[2].title, 'Output format');
eq('fence is not a heading', p[1].body.includes('## not a heading'), true);
eq('duplicate becomes custom', parse(samples[3]).map(s => s.id), ['steps', 'custom']);
eq('standard sections added empty, in order', withStandard(parse(samples[1])).map(s => s.id), ['role', 'goal', 'steps', 'rules', 'output', 'escalate']);
eq('empty ones are left out', compose(withStandard(parse(samples[1]))), samples[1]);
eq('list items', listItems('1. a\n2. b', 'ordered'), ['a', 'b']);
eq('not a plain list', listItems('1. a\ntext', 'ordered'), null);
eq('misnumbered is not editable as items', listItems('1. a\n3. b', 'ordered'), null);
eq('list body', listBody(['a', ' ', 'b'], 'ordered'), '1. a\n2. b');
eq('add examples after the rest', addSection(withStandard([]), 'examples').map(s => s.id).slice(-1), ['examples']);
eq('add goal before rules', addSection(parse(samples[1]).filter(s => s.id !== 'goal'), 'goal').map(s => s.id), ['goal', 'rules']);
eq('custom', compose(addCustom([], '## Tone').map(s => ({ ...s, body: 'Kind.' }))), '## Tone\n\nKind.');
if (f) process.exit(1);
`

func TestConsoleInstructionSections(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node is not installed")
	}
	tmp := t.TempDir()
	src, _ := uiFiles.ReadFile("ui/lib/instructions.js")
	os.WriteFile(filepath.Join(tmp, "instructions.mjs"), src, 0o600)
	test := filepath.Join(tmp, "cases.mjs")
	os.WriteFile(test, []byte(instructionCases), 0o600)
	if out, err := exec.Command(node, test).CombinedOutput(); err != nil {
		t.Fatalf("instruction sections failed:\n%s", out)
	}
}
