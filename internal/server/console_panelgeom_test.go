package server

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// The assistant panel's sizing rules are plain functions (ui/lib/panelgeom.js), so they run under
// Node. Skipped where Node is not installed. The rule that matters: whatever was saved or dragged,
// the panel stays on screen and big enough to use.
const panelGeomCases = `
import { dockWidth, wideDockWidth, floatRect, readPrefs, MIN_DOCK } from './panelgeom.mjs';

let failed = 0;
const eq = (name, got, want) => {
  const g = JSON.stringify(got), w = JSON.stringify(want);
  if (g !== w) { failed++; console.error('FAIL ' + name + '\n  got  ' + g + '\n  want ' + w); }
};

eq('default dock width', dockWidth(undefined, 1600), 520);
eq('dock cannot be narrower than the minimum', dockWidth(50, 1600), MIN_DOCK);
eq('dock is at most 70% of the window', dockWidth(5000, 1000), 700);
eq('dock is at most 1100 px on a huge screen', dockWidth(5000, 4000), 1100);
eq('a small window still gets the minimum', dockWidth(900, 360), MIN_DOCK);
eq('not a number falls back', dockWidth('wide', 1600), 520);
eq('NaN falls back', dockWidth(NaN, 1600), 520);
eq('wide is half the window', wideDockWidth(2000), 1000);
eq('wide stays within the limits', wideDockWidth(500), MIN_DOCK);

eq('float defaults sit at the top right', floatRect({}, 1600, 900), { x: 1016, y: 64, w: 560, h: 680 });
eq('float pulled back from the right and bottom edges', floatRect({ x: 1590, y: 890, w: 560, h: 680 }, 1600, 900), { x: 1032, y: 212, w: 560, h: 680 });
eq('float cannot leave the top left', floatRect({ x: -400, y: -50, w: 560, h: 400 }, 1600, 900), { x: 8, y: 8, w: 560, h: 400 });
eq('float cannot be tiny', floatRect({ x: 10, y: 10, w: 10, h: 10 }, 1600, 900), { x: 10, y: 10, w: 320, h: 280 });
eq('float cannot be bigger than the window', floatRect({ x: 0, y: 0, w: 5000, h: 5000 }, 800, 600), { x: 8, y: 8, w: 784, h: 584 });
eq('float in a window smaller than the minimum', floatRect({}, 300, 250).w >= 320, true);
eq('null position falls back', floatRect({ x: null, y: null }, 1600, 900).y, 64);

eq('prefs from nothing', readPrefs(''), { mode: 'dock', dockW: 520, float: {} });
eq('prefs from garbage', readPrefs('{not json'), { mode: 'dock', dockW: 520, float: {} });
eq('prefs keep float mode and sizes', readPrefs('{"mode":"float","dockW":640,"float":{"x":5,"w":400}}'), { mode: 'float', dockW: 640, float: { x: 5, w: 400 } });
eq('prefs ignore wrong types and unknown fields', readPrefs('{"mode":"evil","dockW":"9","float":{"x":"1","z":3},"extra":1}'), { mode: 'dock', dockW: 520, float: {} });

if (failed) process.exit(1);
console.log('ok');
`

func TestConsolePanelGeometry(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node is not installed")
	}
	src, err := uiFiles.ReadFile("ui/lib/panelgeom.js")
	if err != nil {
		t.Fatal(err)
	}
	tmp := t.TempDir()
	if err := os.WriteFile(filepath.Join(tmp, "panelgeom.mjs"), src, 0o600); err != nil {
		t.Fatal(err)
	}
	test := filepath.Join(tmp, "cases.mjs")
	if err := os.WriteFile(test, []byte(panelGeomCases), 0o600); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command(node, test).CombinedOutput(); err != nil {
		t.Fatalf("panel geometry cases failed:\n%s", out)
	}
}
