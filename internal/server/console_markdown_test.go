package server

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// The Markdown editor's parser and checks are plain functions with no DOM, so they run under
// Node. Skipped where Node is not installed. Each case states what it protects.
const markdownCases = `
import { parseBlocks, parseInline, checkInstructions, splitSections } from './markdown.mjs';

let failed = 0;
const eq = (name, got, want) => {
  const g = JSON.stringify(got), w = JSON.stringify(want);
  if (g !== w) { failed++; console.error('FAIL ' + name + '\n  got  ' + g + '\n  want ' + w); }
};
const levels = (r) => r.map(x => x.level);

// Text that looks like markup stays text: the renderer only ever makes text nodes from it.
eq('html stays text', parseBlocks('<img src=x onerror=alert(1)>'), [{ t: 'p', text: '<img src=x onerror=alert(1)>' }]);
eq('links are not parsed', parseInline('[a](javascript:alert(1))'), [{ t: 'text', text: '[a](javascript:alert(1))' }]);

eq('heading levels', parseBlocks('# A\n## B\n#### C'), [{ t: 'h', level: 1, text: 'A' }, { t: 'h', level: 2, text: 'B' }, { t: 'p', text: '#### C' }]);
eq('paragraph keeps line breaks', parseBlocks('one\ntwo\n\nthree'), [{ t: 'p', text: 'one\ntwo' }, { t: 'p', text: 'three' }]);
eq('lists and tasks', parseBlocks('1. a\n2. b\n\n- [ ] c\n- [x] d\n- e'), [
  { t: 'ol', items: [{ text: 'a', task: null }, { text: 'b', task: null }] },
  { t: 'ul', items: [{ text: 'c', task: false }, { text: 'd', task: true }, { text: 'e', task: null }] }]);
eq('bold at line start is not a list', parseBlocks('**Note** this'), [{ t: 'p', text: '**Note** this' }]);
eq('code fence keeps content raw', parseBlocks('` + "```" + `sh\n# not a heading\n- not a list\n` + "```" + `'), [{ t: 'code', lang: 'sh', text: '# not a heading\n- not a list' }]);
eq('unclosed fence runs to the end', parseBlocks('` + "```" + `\nx'), [{ t: 'code', lang: '', text: 'x' }]);
eq('quote', parseBlocks('> never delete\n> anything'), [{ t: 'quote', text: 'never delete\nanything' }]);
eq('inline spans', parseInline('a **b** and ` + "`c`" + `'), [{ t: 'text', text: 'a ' }, { t: 'b', text: 'b' }, { t: 'text', text: ' and ' }, { t: 'code', text: 'c' }]);
eq('CRLF', parseBlocks('a\r\nb'), [{ t: 'p', text: 'a\nb' }]);
eq('empty', parseBlocks(''), []);

// Tables, as an assistant writes them.
eq('table', parseBlocks('| Setting | Value |\n|---|---|\n| Tag | IMMUTABLE |\n| Scan | Off |'),
  [{ t: 'table', head: ['Setting', 'Value'], align: ['', ''], rows: [['Tag', 'IMMUTABLE'], ['Scan', 'Off']] }]);
eq('table after a heading with no blank line', parseBlocks('## Config\n| A | B |\n|---|---|\n| 1 | 2 |').map(b => b.t), ['h', 'table']);
eq('table alignment', parseBlocks('| a | b | c |\n|:---|:---:|---:|\n| 1 | 2 | 3 |')[0].align, ['', 'center', 'right']);
eq('short rows are padded and long rows cut', parseBlocks('| a | b |\n|---|---|\n| 1 |\n| 1 | 2 | 3 |')[0].rows, [['1', ''], ['1', '2']]);
eq('escaped pipe stays in the cell', parseBlocks('| a | b |\n|---|---|\n| x \\| y | z |')[0].rows, [['x | y', 'z']]);
eq('a table ends at the first line without pipes', parseBlocks('| a |\n|---|\n| 1 |\nafter').map(b => b.t), ['table', 'p']);
eq('a lone pipe line is a paragraph', parseBlocks('| not a table |').map(b => b.t), ['p']);
eq('a rule after a pipe line is not a table', parseBlocks('| a |\n---').map(b => b.t), ['p', 'hr']);
eq('pipes inside a code fence stay code', parseBlocks('` + "```" + `\n| a |\n|---|\n` + "```" + `').map(b => b.t), ['code']);

// Card sections.
const sec = splitSections(parseBlocks('Intro line\n\n## One\ntext\n\n### Deeper\nmore\n\n## Two\n- a'));
eq('intro before the first heading', sec.intro.map(b => b.t), ['p']);
eq('sections by level 1 and 2 headings', sec.sections.map(x => [x.title, x.blocks.map(b => b.t)]), [['One', ['p', 'h', 'p']], ['Two', ['ul']]]);
eq('deeper headings stay inside their section', sec.sections[0].blocks.map(b => b.level || 0), [0, 3, 0]);
eq('no headings means no sections', splitSections(parseBlocks('just text')).sections, []);

// Checks.
eq('empty text warns', levels(checkInstructions('  ')), ['warn']);
eq('clean text passes', levels(checkInstructions('Look at the logs. Ask before changing anything.', { tier: 'T0' })), ['ok']);
eq('T0 that says delete warns', levels(checkInstructions('Delete idle volumes. Ask first.', { tier: 'T0' })), ['warn']);
eq('T1 may mention delete', levels(checkInstructions('Propose to delete idle volumes. Ask first.', { tier: 'T1' })), ['ok']);
eq('a quoted rule is not an instruction', levels(checkInstructions('Ask first.\n\n> Never delete anything.', { tier: 'T0' })), ['ok']);
eq('a code sample is not an instruction', levels(checkInstructions('Ask first.\n\n` + "```" + `\naws ec2 delete-volume\n` + "```" + `', { tier: 'T0' })), ['ok']);
eq('no stop rule warns', levels(checkInstructions('Summarise the logs.')), ['warn']);
eq('credential is bad', levels(checkInstructions('Ask first. key AKIAABCDEFGHIJKLMNOP')), ['bad']);
eq('private key is bad', levels(checkInstructions('Ask first.\n-----BEGIN RSA PRIVATE KEY-----')), ['bad']);
eq('near the limit warns', levels(checkInstructions('Ask first. ' + 'x'.repeat(95), { max: 100 })), ['warn']);

if (failed) process.exit(1);
console.log('ok');
`

func TestConsoleMarkdown(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node is not installed")
	}
	src, err := uiFiles.ReadFile("ui/lib/markdown.js")
	if err != nil {
		t.Fatal(err)
	}
	tmp := t.TempDir()
	// .mjs so Node treats the file as a module, as the browser does.
	if err := os.WriteFile(filepath.Join(tmp, "markdown.mjs"), src, 0o600); err != nil {
		t.Fatal(err)
	}
	test := filepath.Join(tmp, "cases.mjs")
	if err := os.WriteFile(test, []byte(markdownCases), 0o600); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command(node, test).CombinedOutput(); err != nil {
		t.Fatalf("markdown cases failed:\n%s", out)
	}
}
