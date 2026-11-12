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
import { DEFAULTS, autoLayout, addBlock, removeBlock, updateBlock, moveBlock, linkTargets, addLink, removeLink, toBody, explainError, linkPath, linkPorts, bounds, fit, newID, planByBlock, planState, planWords, placeNew, flowOf } from './canvas.mjs';
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
eq('columns: doors, apps, then data', bp.blocks.map(b => b.x), [96, 346, 596, 596]);
eq('stacked in a column', bp.blocks.filter(b => b.x === 596).map(b => b.y), [40, 140]);
eq('a door may send traffic to the app', linkTargets(bp, door), [{ id: 'app', name: 'App', access: false, path: false }]);
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
eq('layout only when nothing has a place', autoLayout({ ...empty, blocks: [{ id: 'a', kind: 'table' }, { id: 'b', kind: 'frontdoor' }] }).blocks.map(b => [b.x, b.y]), [[596, 40], [96, 40]]);
eq('a placed blueprint keeps its places', autoLayout(bp).blocks.map(b => b.x), bp.blocks.map(b => b.x));
eq('messages use names', explainError(bp, 'links[1].access: say read; blocks[0]: front door'), 'the link from “App” to “Table”.access: say read; “Front door”: front door');
eq('a path leaves right and enters left', linkPath({ x: 0, y: 0 }, { x: 300, y: 0 }).startsWith('M190 33'), true);
{
  // Two apps each using three things: no two links share an end point on a block's edge, and the order on an edge
  // follows the other end, so lines leaving one block do not cross each other there.
  const web = { blocks: [{ id: 'a', x: 300, y: 40 }, { id: 'b', x: 300, y: 140 }, { id: 'q', x: 600, y: 40 }, { id: 'd', x: 600, y: 140 }, { id: 'r', x: 600, y: 240 }],
    links: [{ from: 'a', to: 'q' }, { from: 'a', to: 'd' }, { from: 'a', to: 'r' }, { from: 'b', to: 'q' }, { from: 'b', to: 'd' }, { from: 'b', to: 'r' }] };
  const p = linkPorts(web), at = (k) => p.get(k);
  eq('outs spread in order', [at('a>q').out, at('a>d').out, at('a>r').out], [-12, 0, 12]);
  eq('ins spread in order', [at('a>d').in, at('b>d').in], [-6, 6]);
  eq('bends differ', at('a>q').bend !== at('a>r').bend, true);
  const starts = new Set(web.links.map(l => { const [a2, b2] = [web.blocks.find(x => x.id === l.from), web.blocks.find(x => x.id === l.to)]; return linkPath(a2, b2, p.get(l.from + '>' + l.to)).split(' C')[0] + '|' + l.from; }));
  eq('every link leaves from its own point', starts.size, 6);
  eq('a lone link keeps the middle', linkPorts({ blocks: [{ id: 'x', x: 0, y: 0 }, { id: 'y', x: 300, y: 0 }], links: [{ from: 'x', to: 'y' }] }).get('x>y'), { out: 0, in: 0, bend: 0 });
  eq('one-column calls are left alone', linkPorts({ blocks: [{ id: 'x', x: 0, y: 0 }, { id: 'y', x: 0, y: 100 }], links: [{ from: 'x', to: 'y' }] }).has('x>y'), false);
}
eq('bounds grow with blocks', bounds(moveBlock(bp, door, 2000, 900)).w, 2230);
eq('fit', fit('abcdef', 4), 'abc…');
const plan = { status: 'ready', revision: 3, finished_at: new Date(1000e3).toISOString(), add: 2, change: 1, remove: 1, changes: [
  { block: 'app', action: 'add' }, { block: 'app', action: 'remove' }, { block: 'data', action: 'change' }, { block: '', action: 'add' }] };
eq('the worst action per block; shared pieces have none', planByBlock(plan), { app: 'remove', data: 'change' });
eq('ready', planState(plan, 3, false, 1001e3), 'ready');
eq('unsaved edits make it stale', planState(plan, 3, true, 1001e3), 'stale');
eq('a guardrail deny blocks it', planState({ ...plan, guardrails: [{ level: 'deny' }] }, 3, false, 1001e3), 'blocked');
eq('a warning does not', planState({ ...plan, guardrails: [{ level: 'warn' }] }, 3, false, 1001e3), 'ready');
eq('a newer revision makes it stale', planState(plan, 4, false, 1001e3), 'stale');
eq('an hour makes it stale', planState(plan, 3, false, 1000e3 + 3601e3), 'stale');
eq('nothing to do', planState({ ...plan, add: 0, change: 0, remove: 0 }, 3, false, 1001e3), 'nothing');
eq('built since the preview', planState({ ...plan, state_serial: 1 }, 3, false, 1001e3, 2), 'applied');
eq('the preview of the current state is not applied', planState({ ...plan, state_serial: 2 }, 3, false, 1001e3, 2), 'ready');
eq('planning and failed', [planState({ status: 'planning' }, 3, false), planState({ status: 'failed' }, 3, false), planState(null, 3, false)], ['planning', 'failed', 'none']);
eq('words', planWords(plan), '2 to add, 1 to change, 1 to remove');
eq('no words', planWords({ add: 0, change: 0, remove: 0 }), 'Nothing to change: AWS already matches the blueprint');
eq('removal words', planWords({ add: 0, change: 0, remove: 3, removal: 'keep-data' }), 'Remove what it built, keeping tables, buckets, databases and secrets: 3 to remove');
eq('removal nothing', planWords({ add: 0, change: 0, remove: 0, removal: 'all' }), 'Remove everything, step 2 of 2 (data included): nothing to remove');
const withNew = { ...bp, blocks: [...bp.blocks, { id: 'q', kind: 'queue', name: 'Q' }, { id: 'd2', kind: 'frontdoor', name: 'D2' }] };
eq('new blocks go below their column; placed ones stay', placeNew(withNew).blocks.map(b => [b.id, b.x, b.y]), [['door', 96, 40], ['app', 346, 40], ['table', 596, 40], ['logs', 596, 140], ['q', 596, 240], ['d2', 96, 140]]);
eq('nothing placed: lay it all out', placeNew({ ...empty, blocks: [{ id: 'a', kind: 'service' }] }).blocks[0].x, 346);
const fb = { blocks: [{ id: 'd', kind: 'frontdoor' }, { id: 'a', kind: 'service' }, { id: 't', kind: 'table' }, { id: 'l', kind: 'logs' }, { id: 'q', kind: 'queue' }] };
eq('traffic flows from the door to the app', flowOf(fb, { from: 'd', to: 'a' }), 'forward');
eq('logs flow out of the app', flowOf(fb, { from: 'a', to: 'l' }), 'forward');
eq('reading brings data back to the app', flowOf(fb, { from: 'a', to: 't', access: 'read' }), 'back');
eq('reading and writing goes both ways', flowOf(fb, { from: 'a', to: 'q', access: 'readwrite' }), 'both');
eq('a link to nothing has no flow', flowOf(fb, { from: 'a', to: 'gone' }), null);

// Note 039: databases, caches, secrets, service calls and paths.
{
  let w = { blocks: [], links: [] };
  const add = (k, f) => { const r = addBlock(w, k); w = updateBlock(r.bp, r.id, f || {}); return r.id; };
  const d = add('frontdoor'), fe = add('service', { name: 'Frontend' }), be = add('service', { name: 'Backend', port: 8080 }), wk = add('service', { name: 'Worker', port: null });
  const db = add('database'), rc = add('cache'), sc = add('secret'), old = add('existing', { existing: { type: 'database', name: 'legacy' } });
  eq('new kinds start valid', [db, rc, sc], ['db', 'cache', 'secret']);
  eq('the first front-door link is the default', linkTargets(w, d).map(t => [t.id, t.path]), [[fe, false], [be, false]]);
  w = addLink(w, d, fe);
  eq('then other services take a path', linkTargets(w, d).map(t => [t.id, t.path]), [[be, true]]);
  w = addLink(w, d, be, '', '/api/*');
  eq('the path is kept', w.links[1], { from: d, to: be, path: '/api/*' });
  const t = Object.fromEntries(linkTargets(w, fe).map(x => [x.id, x.access]));
  eq('a service may call one with a port, not itself or one without', [be in t, fe in t, wk in t], [true, false, false]);
  eq('databases, caches, secrets and existing ones need no access', [t[db], t[rc], t[sc], t[old]], [false, false, false, false]);
  eq('a call in one column runs from the bottom of one block to the top of the next', linkPath({ x: 346, y: 40 }, { x: 346, y: 140 }), 'M441 106 C441 126, 441 120, 441 140');
  eq('and upwards from the top', linkPath({ x: 346, y: 140 }, { x: 346, y: 40 }), 'M441 140 C441 120, 441 126, 441 106');
  eq('calls and databases flow both ways, secrets come back', [flowOf(w, { from: fe, to: be }), flowOf(w, { from: be, to: db }), flowOf(w, { from: be, to: sc }), flowOf(w, { from: be, to: old })], ['both', 'both', 'back', 'both']);
}
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
		if kind == blueprint.KindAgent {
			// An agent starts without an image: only the person knows their ECR image, and a made-up one would pass
			// the checks and fail only when applied. Everything else about it is valid.
			if err == nil || !strings.Contains(err.Error(), "ECR only") {
				t.Errorf("agent default: %v", err)
			}
			continue
		}
		if err != nil {
			t.Errorf("default %s is not valid: %v", kind, err)
		}
	}
}
