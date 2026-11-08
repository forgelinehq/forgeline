package server

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/forgelinehq/forgeline/internal/blueprint"
)

const canvasCases = `
import { DEFAULTS, autoLayout, addBlock, removeBlock, updateBlock, moveBlock, linkTargets, addLink, removeLink, toBody, explainError, linkPath, bounds, fit, newID, planByBlock, planState, planWords } from './canvas.mjs';
let failed = 0;
const eq = (name, got, want) => { const g = JSON.stringify(got), w = JSON.stringify(want); if (g !== w) { failed++; console.error('FAIL ' + name + '\n  got  ' + g + '\n  want ' + w); } };
const empty = { name: 'T', target: { cloud: 'aws', account: 'primary', region: 'ap-south-1' }, blocks: [], links: [] };

// Building the small web app with the canvas's own operations.
let r = addBlock(empty, 'frontdoor'); let bp = r.bp; const door = r.id;
r = addBlock(bp, 'service'); bp = r.bp; const app = r.id;
r = addBlock(bp, 'table'); bp = r.bp; const table = r.id;
r = addBlock(bp, 'logs'); bp = r.bp; const logs = r.id;
eq('ids', [door, app, table, logs], ['door', 'app', 'table', 'logs']);
eq('the original is not changed', empty.blocks.length, 0);
eq('columns: doors, apps, then data', bp.blocks.map(b => b.x), [40, 290, 540, 540]);
eq('stacked in a column', bp.blocks.filter(b => b.x === 540).map(b => b.y), [40, 140]);
eq('a door may send traffic to the app', linkTargets(bp, door), [{ id: 'app', name: 'App', access: false }]);
eq('an app may use data and logs', linkTargets(bp, app).map(t => [t.id, t.access]), [['table', true], ['logs', false]]);
eq('a table links to nothing', linkTargets(bp, table), []);
bp = addLink(bp, door, app);
bp = addLink(bp, app, table, 'readwrite');
bp = addLink(bp, app, logs);
eq('a door with a link may not add another', linkTargets(bp, door), []);
const both = addBlock(bp, 'logs');
eq('one logs block per app', linkTargets(both.bp, app).map(t => t.id), []);
r = addBlock(bp, 'service'); const portless = updateBlock(r.bp, r.id, { port: '' });
const door2 = addBlock(portless, 'frontdoor');
eq('a second door: not to a fronted app, not to an app without a port', linkTargets(door2.bp, door2.id), []);
eq('second ids', [r.id, door2.id, newID(door2.bp, 'service')], ['app-2', 'door-2', 'app-3']);
eq('second names', r.bp.blocks.find(b => b.id === 'app-2').name, 'App 2');
eq('unset fields are removed', 'port' in portless.blocks.find(b => b.id === 'app-2'), false);
eq('remove a block takes its links', removeBlock(bp, app).links, []);
eq('remove a link', removeLink(bp, app, logs).links.length, 2);
eq('moving keeps to the canvas', moveBlock(bp, door, -50, 12.6).blocks[0], { ...bp.blocks[0], x: 0, y: 13 });
eq('layout only when nothing has a place', autoLayout({ ...empty, blocks: [{ id: 'a', kind: 'table' }, { id: 'b', kind: 'frontdoor' }] }).blocks.map(b => [b.x, b.y]), [[540, 40], [40, 40]]);
eq('a placed blueprint keeps its places', autoLayout(bp).blocks.map(b => b.x), bp.blocks.map(b => b.x));
eq('messages use names', explainError(bp, 'links[1].access: say read; blocks[0]: front door'), 'the link from “App” to “Table”.access: say read; “Front door”: front door');
eq('a path leaves right and enters left', linkPath({ x: 0, y: 0 }, { x: 300, y: 0 }).startsWith('M190 33'), true);
eq('bounds grow with blocks', bounds(moveBlock(bp, door, 2000, 900)).w, 2230);
eq('fit', fit('abcdef', 4), 'abc…');
const plan = { status: 'ready', revision: 3, finished_at: new Date(1000e3).toISOString(), add: 2, change: 1, remove: 1, changes: [
  { block: 'app', action: 'add' }, { block: 'app', action: 'remove' }, { block: 'data', action: 'change' }, { block: '', action: 'add' }] };
eq('the worst action per block; shared pieces have none', planByBlock(plan), { app: 'remove', data: 'change' });
eq('ready', planState(plan, 3, false, 1001e3), 'ready');
eq('unsaved edits make it stale', planState(plan, 3, true, 1001e3), 'stale');
eq('a newer revision makes it stale', planState(plan, 4, false, 1001e3), 'stale');
eq('an hour makes it stale', planState(plan, 3, false, 1000e3 + 3601e3), 'stale');
eq('nothing to do', planState({ ...plan, add: 0, change: 0, remove: 0 }, 3, false, 1001e3), 'nothing');
eq('planning and failed', [planState({ status: 'planning' }, 3, false), planState({ status: 'failed' }, 3, false), planState(null, 3, false)], ['planning', 'failed', 'none']);
eq('words', planWords(plan), '2 to add, 1 to change, 1 to remove');
eq('no words', planWords({ add: 0, change: 0, remove: 0 }), 'Nothing to change: AWS already matches the blueprint');
console.log(JSON.stringify({ built: toBody(bp, 3), defaults: DEFAULTS }));
if (failed) process.exit(1);
`

func TestConsoleCanvas(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node is not installed")
	}
	tmp := t.TempDir()
	src, err := uiFiles.ReadFile("ui/lib/canvas.js")
	if err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(tmp, "canvas.mjs"), src, 0o600)
	test := filepath.Join(tmp, "cases.mjs")
	os.WriteFile(test, []byte(canvasCases), 0o600)
	out, err := exec.Command(node, test).CombinedOutput()
	if err != nil {
		t.Fatalf("canvas cases failed: %v\n%s", err, out)
	}
	var res struct {
		Built struct {
			Name     string            `json:"name"`
			Target   blueprint.Target  `json:"target"`
			Blocks   []blueprint.Block `json:"blocks"`
			Links    []blueprint.Link  `json:"links"`
			Revision int               `json:"revision"`
		} `json:"built"`
		Defaults map[string]blueprint.Block `json:"defaults"`
	}
	if err := json.Unmarshal(out[strings.LastIndex(string(out[:len(out)-1]), "\n")+1:], &res); err != nil {
		t.Fatalf("output: %v\n%s", err, out)
	}
	// What the canvas builds is valid by the server's rules, and so is every block it adds.
	b := blueprint.Blueprint{ID: "x", Name: res.Built.Name, Target: res.Built.Target, Blocks: res.Built.Blocks, Links: res.Built.Links}
	if err := b.Validate(); err != nil || res.Built.Revision != 3 {
		t.Errorf("built on the canvas: %v (revision %d)", err, res.Built.Revision)
	}
	if len(res.Defaults) != len(blueprint.Kinds) {
		t.Errorf("defaults for %d kinds, want %d", len(res.Defaults), len(blueprint.Kinds))
	}
	for kind, d := range res.Defaults {
		d.ID, d.Kind = "a", kind
		one := blueprint.Blueprint{ID: "x", Name: "x", Target: res.Built.Target, Blocks: []blueprint.Block{d}}
		err := one.Validate()
		if kind == blueprint.KindFrontDoor {
			// A front door alone is incomplete (it needs a service), but its fields are valid.
			if err == nil || !strings.Contains(err.Error(), "exactly one service") {
				t.Errorf("front door default: %v", err)
			}
			continue
		}
		if err != nil {
			t.Errorf("default %s is not valid: %v", kind, err)
		}
	}
}
