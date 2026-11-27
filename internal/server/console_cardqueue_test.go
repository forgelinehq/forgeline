package server

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// The assistant's cards come one at a time and the assistant hears back once, after the last (ui/lib/cardqueue.js).
// The rules are plain functions, so they run under Node. Skipped where Node is not installed.
const cardQueueCases = `
import { newQueue, queueView, decide, takeReply } from './cardqueue.mjs';

let failed = 0;
const eq = (name, got, want) => {
  const g = JSON.stringify(got), w = JSON.stringify(want);
  if (g !== w) { failed++; console.error('FAIL ' + name + '\n  got  ' + g + '\n  want ' + w); }
};
const ids = (v) => v.shown.map(a => a.id);
const a = { id: 'a', title: 'Create agent one' }, b = { id: 'b', title: 'Create agent two' }, c = { id: 'c', title: 'Run it' };

let q = newQueue();
let v = queueView([a, b, c], q);
eq('only the first card is shown', ids(v), ['a']);
eq('the others are named', v.next, ['Create agent two', 'Run it']);
eq('position', [v.position, v.total], [1, 3]);

decide(q, a, 'confirm', 'Created agent "one".');
eq('nothing is said while cards wait', takeReply(q, [a, b, c], false), '');
v = queueView([a, b, c], q);
eq('the next card appears under the decided one', ids(v), ['a', 'b']);
eq('position moves on', [v.position, v.total], [2, 3]);

decide(q, b, 'dismiss');
decide(q, b, 'confirm', 'again');
decide(q, c, 'failed', 'no such workflow');
eq('nothing is said while the assistant is still answering', takeReply(q, [a, b, c], true), '');
eq('one message tells every decision once', takeReply(q, [a, b, c], false),
  'I confirmed "Create agent one". Created agent "one". I dismissed "Create agent two", so go on without it. Confirming "Run it" failed: no such workflow Carry on.');
eq('and not again', takeReply(q, [a, b, c], false), '');

q = newQueue();
decide(q, a, 'confirm', 'Done.');
eq('a single card answers at once', takeReply(q, [a], false), 'I confirmed "Create agent one". Done. Carry on.');
const d = { id: 'd', title: 'Later' };
eq('a card that arrives later still shows', ids(queueView([a, d], q)), ['a', 'd']);

q = newQueue();
const auto = { id: 'x', title: 'Autopilot did it', done: true };
v = queueView([auto, a], q);
eq('a card autopilot ran is shown and not counted', [ids(v), v.total], [['x', 'a'], 1]);
eq('autopilot cards send nothing', takeReply(q, [auto], false), '');

q = newQueue();
const many = [a, b, c];
for (const x of many) decide(q, x, 'confirm', 'r'.repeat(5000));
const long = takeReply(q, many, false);
eq('the message fits', long.length <= 3900, true);
eq('every card is still named', many.every(x => long.includes('"' + x.title + '"')), true);
eq('and it still says to carry on', long.endsWith(' Carry on.'), true);

if (failed) process.exit(1);
console.log('ok');
`

func TestAssistantCardsComeOneAtATime(t *testing.T) {
	// The panel must use the queue: no card may answer the assistant by itself.
	panel, err := uiFiles.ReadFile("ui/lib/assistant.js")
	if err != nil {
		t.Fatal(err)
	}
	if n := strings.Count(string(panel), "follow("); n != 2 {
		t.Errorf("assistant.js should call follow only from the card queue (and define it): found %d", n)
	}
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node is not installed")
	}
	src, err := uiFiles.ReadFile("ui/lib/cardqueue.js")
	if err != nil {
		t.Fatal(err)
	}
	tmp := t.TempDir()
	if err := os.WriteFile(filepath.Join(tmp, "cardqueue.mjs"), src, 0o600); err != nil {
		t.Fatal(err)
	}
	test := filepath.Join(tmp, "cases.mjs")
	if err := os.WriteFile(test, []byte(cardQueueCases), 0o600); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command(node, test).CombinedOutput(); err != nil {
		t.Fatalf("card queue cases failed:\n%s", out)
	}
}
