package server

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// The Settings menu (ui/lib/settingsnav.js) is plain data and functions, so it runs under Node.
// Skipped where Node is not installed.
const settingsNavCases = `
import { SETTINGS, visibleGroups, isSettingsPath, activeItem, canOpenSettings } from './settingsnav.mjs';

let failed = 0;
const eq = (name, got, want) => {
  const g = JSON.stringify(got), w = JSON.stringify(want);
  if (g !== w) { failed++; console.error('FAIL ' + name + '\n  got  ' + g + '\n  want ' + w); }
};
const links = (role) => visibleGroups(role).flatMap(g => g.items.filter(i => !i.planned).map(i => i.path));

// Who sees what.
eq('an administrator sees every page', links('admin'), ['/people', '/sso', '/integrations', '/secrets', '/integrations/claude/settings', '/memory', '/audit', '/approval-rules', '/account']);
eq('a developer does not see People or the audit log', links('developer'), ['/integrations', '/secrets', '/integrations/claude/settings', '/memory', '/approval-rules', '/account']);
eq('a viewer sees only their own tokens', links('viewer'), ['/account']);
eq('an unknown role sees nothing', links('nobody'), []);
eq('a group with no pages for the role is left out', visibleGroups('developer').map(g => g.title), ['Connections', 'AI', 'Security', 'Developer']);
eq('settings open for developers and administrators only', [canOpenSettings('viewer'), canOpenSettings('developer'), canOpenSettings('admin')], [false, true, true]);

// Planned entries are shown but are never links.
const planned = SETTINGS.flatMap(g => g.items).filter(i => i.planned);
eq('planned entries have no path', planned.every(i => !i.path), true);
eq('planned entries are listed for administrators', visibleGroups('admin').flatMap(g => g.items).filter(i => i.planned).map(i => i.label), ['Budgets and limits']);
eq('a real page is never marked planned', SETTINGS.flatMap(g => g.items).filter(i => i.path).every(i => !i.planned && i.min), true);

// Which pages are part of Settings.
eq('the landing page', isSettingsPath('/settings'), true);
eq('integrations and a popup deep link', [isSettingsPath('/integrations'), isSettingsPath('/integrations/aws'), isSettingsPath('/integrations/github/settings')], [true, true, true]);
eq('secrets, people, audit', [isSettingsPath('/secrets'), isSettingsPath('/people'), isSettingsPath('/audit')], [true, true, true]);
eq('a query string is ignored', isSettingsPath('/integrations?x=1'), true);
eq('daily pages are not settings', ['/', '/pipelines', '/runs/abc', '/infrastructure', '/kubernetes', '/proposals', '/agents'].map(isSettingsPath), [false, false, false, false, false, false, false]);
eq('a path that only starts like a settings page is not one', isSettingsPath('/peoplex'), false);
eq('the personal account page does not make the gear active', isSettingsPath('/account'), false);

// The menu entry highlighted for a path: the most specific one.
eq('claude settings beats integrations', activeItem('/integrations/claude/settings').label, 'Models');
eq('memory is a settings page', activeItem('/memory').label, 'Memory');
eq('memory counts as settings', isSettingsPath('/memory'), true);
eq('a connection address is Connections', activeItem('/integrations/aws').label, 'Connections');
eq('no entry for a daily page', activeItem('/pipelines'), null);

if (failed) process.exit(1);
console.log('ok');
`

func TestConsoleSettingsNav(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node is not installed")
	}
	src, err := uiFiles.ReadFile("ui/lib/settingsnav.js")
	if err != nil {
		t.Fatal(err)
	}
	tmp := t.TempDir()
	if err := os.WriteFile(filepath.Join(tmp, "settingsnav.mjs"), src, 0o600); err != nil {
		t.Fatal(err)
	}
	test := filepath.Join(tmp, "cases.mjs")
	if err := os.WriteFile(test, []byte(settingsNavCases), 0o600); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command(node, test).CombinedOutput(); err != nil {
		t.Fatalf("settings nav cases failed:\n%s", out)
	}
}
