package server

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/forgelinehq/forgeline/internal/authn"
)

func TestWhatPlanOnlyRefuses(t *testing.T) {
	call := func(tool, args string) lookupCall { return lookupCall{Tool: tool, Args: json.RawMessage(args)} }
	for name, tc := range map[string]struct {
		c    lookupCall
		want bool
	}{
		"a lookup":                     {call("list_pipelines", `{}`), false},
		"an API read":                  {call(forgelineAPITool, `{"method":" get ","path":"/v1/ai/teams"}`), false},
		"an API change":                {call(forgelineAPITool, `{"method":"POST","path":"/v1/ai/teams"}`), true},
		"an API call with no method":   {call(forgelineAPITool, `{}`), true},
		"a command that looks":         {call(runShellTool, `{"command":"ls /work"}`), false},
		"any other command":            {call(runShellTool, `{"command":"make"}`), true},
		"a look chained to a change":   {call(runShellTool, `{"command":"ls; rm -rf x"}`), true},
		"a command with a secret":      {call(runShellTool, `{"command":"ls","secrets":["K"]}`), true},
		"memory view":                  {call(memoryTool, `{"command":"view"}`), false},
		"memory search":                {call(memoryTool, `{"command":"search","query":"x"}`), false},
		"memory create":                {call(memoryTool, `{"command":"create"}`), true},
		"a note":                       {call(rememberTool, `{}`), true},
		"a card":                       {call(prepareTool, `{}`), true},
		"an AWS read":                  {call("call_aws", `{"cli_command":"aws sqs list-queues"}`), false},
		"an AWS change":                {call("call_aws", `{"cli_command":"aws sqs create-queue --queue-name q"}`), true},
		"an AWS proposal":              {call(toolProposeAWS, `{}`), true},
		"an environment":               {call(requestEnvTool, `{}`), true},
		"a runbook":                    {call(runRunbookTool, `{}`), true},
		"approving a plan of commands": {call(approvePlanTool, `{}`), true},
		"a plugin":                     {call(pluginToolPrefix+"x_y", `{}`), true},
	} {
		if got := changesInPlanMode(tc.c); got != tc.want {
			t.Errorf("%s: %v, want %v", name, got, tc.want)
		}
	}
}

// Plan only is enforced by the server: neither the model's tools nor the program's own change anything, whatever was
// allowed before; nothing can be approved on a card.
func TestPlanOnlyChangesNothing(t *testing.T) {
	m, d := sandboxHarness(t)
	m.admin.expect(200, "PUT", "/v1/assistant/sandbox", map[string]any{"enabled": true})
	conv := "0123456789abcdef0123456789abcde3"
	run := func(mode, cmd string) (res string, cards int, system string) {
		st := m.streamWith(m.dev, map[string]any{"conversationId": conv, "mode": mode}, shell(map[string]any{"command": cmd}), "Done.")
		events, _ := answerOf(t, st)
		return m.llm.lastUser(1), len(eventsNamed(events, "forgeline.permission")), m.lastSystem()
	}
	res, cards, system := run(modePlan, "make all")
	if !strings.Contains(res, "plan mode is on") || cards != 0 || len(d.find("exec")) != 0 {
		t.Fatalf("a command while planning: cards %d, ran %d, %s", cards, len(d.find("exec")), res)
	}
	if !strings.Contains(system, "Plan mode is on") {
		t.Error("the model is not told it is only planning")
	}
	if res, _, _ = run(modePlan, "ls /work"); len(d.find("exec")) != 1 {
		t.Errorf("a command that only looks did not run: %s", res)
	}
	if aud := string(m.admin.expect(200, "GET", "/v1/audit?action=assistant.mode.refused", nil)); !strings.Contains(aud, "plan only") {
		t.Errorf("the refusal is not in the audit log: %s", aud)
	}
	m.dev.expect(400, "POST", "/v1/assistant/runs", map[string]any{"message": "x", "mode": "yolo"})
	m.dev.expect(400, "POST", "/v1/assistant/runs", map[string]any{"message": "x", "mode": modePlan, "autopilot": map[string]any{"minutes": 5}})

	// The program's own tools.
	h, _ := planHarness(t)
	p := Principal{UserID: "u1", Name: "uma", Role: authn.Developer}
	asked := 0
	base := context.WithValue(context.WithValue(t.Context(), autopilotConvKey{}, "c1"), permAskerKey{}, permAsker(func(permRequest) (string, error) {
		asked++
		return decisionOnce, nil
	}))
	sink := &lookupSink{}
	base = context.WithValue(base, lookupSinkKey{}, sink)
	sink.plan.add([]planAction{{Tool: "Bash", Command: "make"}})
	h.srv.chatAllow.add(chatKey(base, p), ruleEdits)
	startAutopilot(autopilotKeyFor(p, "c1"), autopilotRequest{Steps: 5})
	defer endAutopilot(autopilotKeyFor(p, "c1"))
	allowed := func(mode, tool string, in map[string]any) bool {
		raw, _ := json.Marshal(map[string]any{"tool_name": tool, "input": in})
		return strings.Contains(h.srv.enginePermission(&engineRun{ctx: context.WithValue(base, modeKey{}, mode), p: p}, raw), `"allow"`)
	}
	file := map[string]any{"file_path": "/work/a", "content": "x"}
	for tool, in := range map[string]map[string]any{"Bash": {"command": "make"}, "Write": file, "Edit": {"file_path": "/work/a", "old_string": "a", "new_string": "b"}, "SomethingNew": {}} {
		if allowed(modePlan, tool, in) {
			t.Errorf("%s ran while planning", tool)
		}
	}
	endAutopilot(autopilotKeyFor(p, "c1"))
	if !allowed(modePlan, "WebFetch", map[string]any{"url": "https://example.com"}) || asked != 1 {
		t.Errorf("reading the web while planning did not ask: %d", asked)
	}
}

// With file changes accepted, a file under /work is written without a card; a hidden one, a command and the server's own
// shell still ask.
func TestAcceptEditsAcceptsOnlyFileChangesInTheSandbox(t *testing.T) {
	m, _ := planHarness(t)
	p := Principal{UserID: "u1", Name: "uma", Role: authn.Developer}
	asked := []string{}
	ctx := context.WithValue(context.WithValue(t.Context(), modeKey{}, modeEdits), permAskerKey{}, permAsker(func(r permRequest) (string, error) {
		asked = append(asked, r.Tool)
		return decisionDenied, nil
	}))
	call := func(tool string, in map[string]any) bool {
		raw, _ := json.Marshal(map[string]any{"tool_name": tool, "input": in})
		return strings.Contains(m.srv.enginePermission(&engineRun{ctx: ctx, p: p}, raw), `"allow"`)
	}
	write := map[string]any{"file_path": "/work/a", "content": "x"}
	if !call("Write", write) || !call("Edit", map[string]any{"file_path": "/work/d/a", "old_string": "a", "new_string": "b"}) || len(asked) != 0 {
		t.Errorf("a file change asked: %v", asked)
	}
	if call("Write", map[string]any{"file_path": "/work/.bashrc", "content": "x"}) || call("Write", map[string]any{"file_path": "/etc/x", "content": "x"}) || call("Bash", map[string]any{"command": "make"}) || !slices.Equal(asked, []string{"Write", "Write", "Bash"}) {
		t.Errorf("what must still ask: %v", asked)
	}
	if aud := string(m.admin.expect(200, "GET", "/v1/audit?action=assistant.engine.tool", nil)); !strings.Contains(aud, "accepted with file changes on") {
		t.Error("an accepted change is not in the audit log")
	}
	m.admin.expect(200, "PUT", "/v1/assistant/sandbox", map[string]any{"enabled": false})
	if call("Write", write) {
		t.Error("a file on the server itself was written without asking")
	}
}

const consoleModesCheck = `import { MODES, modeFields, carryOutChoices } from './modes.mjs';
let failed = 0;
const eq = (n, got, want) => { if (JSON.stringify(got) !== JSON.stringify(want)) { failed++; console.error(n + ' ' + JSON.stringify(got)); } };
eq('modes', MODES.map(m => m.id), ['ask', 'edits', 'plan']);
eq('ask sends nothing', modeFields('ask', false, {}), {});
eq('edits', modeFields('edits', false, {}), { mode: 'edits' });
eq('plan', modeFields('plan', false, {}), { mode: 'plan' });
eq('unknown', modeFields('x', false, {}), {});
eq('autopilot replaces the mode', modeFields('plan', true, { steps: 3 }), { autopilot: { steps: 3 } });
eq('a finished plan', carryOutChoices('plan', 'finished').map(c => c.mode), ['ask', 'edits']);
eq('not an ordinary answer', carryOutChoices('ask', 'finished'), []);
eq('not a failed plan', carryOutChoices('plan', 'error'), []);
if (failed) process.exit(1);
`

func TestConsoleModes(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node is not installed")
	}
	src, err := uiFiles.ReadFile("ui/lib/modes.js")
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	for name, body := range map[string][]byte{"modes.mjs": src, "check.mjs": []byte(consoleModesCheck)} {
		if err := os.WriteFile(filepath.Join(dir, name), body, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if out, err := exec.Command(node, filepath.Join(dir, "check.mjs")).CombinedOutput(); err != nil {
		t.Errorf("%s", out)
	}
	page, _ := uiFiles.ReadFile("ui/lib/assistant.js")
	for _, want := range []string{"Object.assign(body, modeFields(workMode, apOn, limitsBody()))", "carryOutChoices(sentMode, run.status)", "workMode = 'ask'; drawAutopilot(); empty()"} {
		if !strings.Contains(string(page), want) {
			t.Errorf("the panel lacks %q", want)
		}
	}
}

// Both kinds of agent are told they are only planning, and neither gets a card or runs its tool while they are.
func TestAnAgentInPlanModeIsToldAndRefused(t *testing.T) {
	plan := map[string]any{"message": "plan it", "conversationId": apConv, "mode": modePlan}

	m := engineHarness(t)
	events, _ := answerOf(t, m.streamWith(m.dev, plan, "unused"))
	if text, _ := textOf(events); !strings.Contains(text, "denied: plan mode is on") || len(eventsNamed(events, "forgeline.permission")) != 0 {
		t.Errorf("Claude Code while planning: %q", text)
	}
	found, _ := filepath.Glob(filepath.Join(m.srv.cfg.DataDir, "engine", "*", "work", "system.txt"))
	if len(found) != 1 {
		t.Fatalf("system prompt: %v", found)
	}
	if b, _ := os.ReadFile(found[0]); !strings.Contains(string(b), "Plan mode is on") {
		t.Error("Claude Code is not told it is only planning")
	}

	a, _, _, dir := acpHarness(t)
	if _, _, err := a.srv.putSecret(t.Context(), "ada", geminiKeyName, "AIzaSyTestKey0123456789abcdefghij", "t"); err != nil {
		t.Fatal(err)
	}
	events, _ = answerOf(t, a.streamWith(a.dev, plan, "unused"))
	if text, _ := textOf(events); !strings.Contains(text, "Picked r with") || len(eventsNamed(events, "forgeline.permission")) != 0 {
		t.Errorf("an ACP agent while planning: %q", text)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "prompt.txt")); !strings.Contains(string(b), "Plan mode is on") {
		t.Error("the ACP agent is not told it is only planning")
	}
}
