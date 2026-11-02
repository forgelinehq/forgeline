package server

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// The console shows the server version with one leading "v", whether the build reports "0.1.0" or
// "v0.1.0-4-gabc123" (a build from git). It once showed "vv0.1.0-4-g...".
const versionCases = `
import { versionLabel } from './version.mjs';
let failed = 0;
const eq = (name, got, want) => { if (got !== want) { failed++; console.error('FAIL ' + name + ' got ' + got + ' want ' + want); } };
eq('plain release', versionLabel('0.1.0'), 'v0.1.0');
eq('git describe', versionLabel('v0.1.0-4-gbdc1c0b'), 'v0.1.0-4-gbdc1c0b');
eq('dev build', versionLabel('0.0.0-dev'), 'v0.0.0-dev');
eq('empty', versionLabel(''), 'unknown');
eq('missing', versionLabel(undefined), 'unknown');
if (failed) process.exit(1);
console.log('ok');
`

func TestConsoleVersionLabel(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node is not installed")
	}
	src, err := uiFiles.ReadFile("ui/lib/version.js")
	if err != nil {
		t.Fatal(err)
	}
	tmp := t.TempDir()
	if err := os.WriteFile(filepath.Join(tmp, "version.mjs"), src, 0o600); err != nil {
		t.Fatal(err)
	}
	test := filepath.Join(tmp, "cases.mjs")
	if err := os.WriteFile(test, []byte(versionCases), 0o600); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command(node, test).CombinedOutput(); err != nil {
		t.Fatalf("version cases failed:\n%s", out)
	}
}
