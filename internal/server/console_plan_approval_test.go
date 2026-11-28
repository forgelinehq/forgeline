package server

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/forgelinehq/forgeline/internal/authn"
)

// planSteps makes the stand-in program in the sandbox make exactly these calls, and answer with what each got back.
func planSteps(t *testing.T, m *mcpHarness, bin string, steps ...[2]any) {
	t.Helper()
	list := []map[string]any{}
	for _, s := range steps {
		list = append(list, map[string]any{"name": s[0], "arguments": s[1]})
	}
	b, _ := json.Marshal(list)
	if err := os.WriteFile(filepath.Join(m.srv.cfg.DataDir, "steps.json"), b, 0o600); err != nil {
		t.Fatal(err)
	}
	head, _, ok := strings.Cut(fakeClaudeSocket, "sysfile = ")
	if !ok {
		t.Fatal("the stand-in program changed")
	}
	script := head + `print(json.dumps({'type': 'system', 'subtype': 'init', 'session_id': 'c-1'}), flush=True)
open('tools.json', 'w').write(json.dumps([t['name'] for t in rpc('tools/list')['tools']]))
out = []
for s in json.load(open('steps.json')):
    text = rpc('tools/call', s)['content'][0]['text']
    out.append(json.loads(text)['behavior'] if s['name'] == 'approve' else text)
open('out.json', 'w').write(json.dumps(out))
print(json.dumps({'type': 'result', 'subtype': 'success', 'is_error': False, 'result': 'done', 'session_id': 'c-1'}), flush=True)
`
	if err := os.WriteFile(filepath.Join(bin, "claude"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
}

func use(tool string, in map[string]any) [2]any {
	return [2]any{"approve", map[string]any{"tool_name": tool, "input": in}}
}

// planRun sends a message and answers each card in turn with the decisions given; it returns the cards and what the program got.
func planRun(t *testing.T, m *mcpHarness, extra map[string]any, decisions ...string) (cards []map[string]any, out []string) {
	t.Helper()
	_ = os.Remove(filepath.Join(m.srv.cfg.DataDir, "out.json"))
	st := m.streamWith(m.dev, extra, "unused")
	for _, d := range decisions {
		id := m.waitPending(t)
		time.Sleep(20 * time.Millisecond)
		m.dev.expect(200, "POST", "/v1/assistant/approvals/"+id, map[string]any{"decision": d})
	}
	events, _ := answerOf(t, st)
	b, err := os.ReadFile(filepath.Join(m.srv.cfg.DataDir, "out.json"))
	if err != nil {
		t.Fatalf("the program did not finish: %v %s", err, st.body)
	}
	json.Unmarshal(b, &out)
	return eventsNamed(events, "forgeline.permission"), out
}

func planHarness(t *testing.T) (*mcpHarness, string) {
	t.Helper()
	m, _, _ := containerEngineHarness(t)
	bin := t.TempDir()
	m.srv.engineExec = engineStandIn(m.srv.cfg.DataDir, bin, new([]string))
	return m, bin
}

// One answer covers exactly what was listed, once each, for that message only.
func TestAnApprovedPlanRunsExactlyWhatWasListedOnceInThatMessage(t *testing.T) {
	m, bin := planHarness(t)
	write := map[string]any{"file_path": "/work/a.py", "content": "print(1)\n"}
	edit := map[string]any{"file_path": "/work/a.py", "old_string": "1", "new_string": "2"}
	plan := map[string]any{"title": "Build  and\nrun it", "actions": []map[string]any{
		{"tool": "Write", "file_path": "/work/a.py", "content": "print(1)\n"},
		{"tool": "Edit", "file_path": "/work/a.py", "old_string": "1", "new_string": "2"},
		{"tool": "Bash", "command": "python3 /work/a.py"},
		{"tool": "Bash", "command": "date"},
		{"tool": "Bash", "command": "date"},
		{"tool": "Bash", "command": "never run in this message"},
	}}
	planSteps(t, m, bin,
		[2]any{approvePlanTool, plan},
		use("Write", write),
		use("Edit", edit),
		use("Bash", map[string]any{"command": "python3 /work/a.py", "description": "Run it", "timeout": 5000, "run_in_background": false}),
		use("Bash", map[string]any{"command": "date"}),
		use("Bash", map[string]any{"command": "date"}),
		// None of these is on the plan as written: a third time, a changed character, in the background, outside the sandbox's own
		// rules, other content, another file, every occurrence, and a tool a plan cannot hold.
		use("Bash", map[string]any{"command": "date"}),
		use("Bash", map[string]any{"command": "python3 /work/a.py "}),
		use("Bash", map[string]any{"command": "never run in this message", "run_in_background": true}),
		use("Bash", map[string]any{"command": "never run in this message", "dangerouslyDisableSandbox": true}),
		use("Write", map[string]any{"file_path": "/work/a.py", "content": "print(1)\n"}),
		use("Write", map[string]any{"file_path": "/work/b.py", "content": "print(1)\n"}),
		use("Edit", map[string]any{"file_path": "/work/a.py", "old_string": "1", "new_string": "2", "replace_all": true}),
		use("WebFetch", map[string]any{"url": "https://example.com"}),
	)
	cards, out := planRun(t, m, nil, "approve", "deny", "deny", "deny", "deny", "deny", "deny", "deny", "deny")
	want := []string{"allow", "allow", "allow", "allow", "allow", "deny", "deny", "deny", "deny", "deny", "deny", "deny", "deny"}
	if len(out) != len(want)+1 || !strings.HasPrefix(out[0], "Approved.") || !slices.Equal(out[1:], want) {
		t.Fatalf("what the program got: %q", out)
	}
	if len(cards) != 9 {
		t.Fatalf("%d cards, want the plan and the 8 calls that were not on it: %v", len(cards), cards)
	}
	first := cards[0]
	actions, _ := first["actions"].([]any)
	if first["tool"] != approvePlanTool || first["command"] != "Build and run it (6 actions)" || first["pattern"] != "" || len(actions) != 6 {
		t.Errorf("the plan card: %v", first)
	} else if a := actions[0].(map[string]any); a["file_path"] != "/work/a.py" || a["content"] != "print(1)\n" {
		t.Errorf("the card does not show the file in full: %v", a)
	}
	if !slices.Contains(toolsGiven(t, m), approvePlanTool) {
		t.Error("the program was not given approve_plan")
	}
	aud := string(m.admin.expect(200, "GET", "/v1/audit?limit=100", nil))
	if !strings.Contains(aud, "assistant.plan.approve") || !strings.Contains(aud, "approved on a plan: python3 /work/a.py") {
		t.Error("the plan and its use are not in the audit log")
	}

	// The next message starts with nothing approved, even what was listed and never used.
	planSteps(t, m, bin, use("Bash", map[string]any{"command": "never run in this message"}))
	cards, out = planRun(t, m, nil, "deny")
	if len(cards) != 1 || !slices.Equal(out, []string{"deny"}) {
		t.Errorf("the approval outlived its message: %v %v", cards, out)
	}
}

func TestARefusedPlanApprovesNothingAndAPlanNeedsAPersonTheSandboxAndNoAutopilot(t *testing.T) {
	m, bin := planHarness(t)
	plan := map[string]any{"title": "Look", "actions": []map[string]any{{"tool": "Bash", "command": "ls"}}}
	planSteps(t, m, bin, [2]any{approvePlanTool, plan}, use("Bash", map[string]any{"command": "ls"}))
	cards, out := planRun(t, m, nil, "deny", "deny")
	if len(cards) != 2 || !strings.Contains(out[0], "said no") || out[1] != "deny" {
		t.Fatalf("after a no: %d cards, %q", len(cards), out)
	}
	m.admin.expect(200, "GET", "/v1/audit?limit=1", nil)
	if aud := string(m.admin.expect(200, "GET", "/v1/audit?action=assistant.plan.approve", nil)); !strings.Contains(aud, "refused: Look") {
		t.Errorf("the refusal is not in the audit log: %s", aud)
	}
	// Under autopilot there is nothing to ask.
	planSteps(t, m, bin, [2]any{approvePlanTool, plan})
	cards, out = planRun(t, m, map[string]any{"autopilot": map[string]any{"minutes": 5, "steps": 20}})
	if len(cards) != 0 || !strings.Contains(out[0], "Autopilot is on") {
		t.Errorf("under autopilot: %v %q", cards, out)
	}
}

// A plan is taken only where a person can answer and the program's tools are in the sandbox; an approval is not used on the
// server itself; a title the card could not show is refused.
func TestWhereAPlanIsOffered(t *testing.T) {
	m, _ := planHarness(t)
	var asked []permRequest
	sink := &lookupSink{}
	p := Principal{UserID: "u1", Name: "uma", Role: authn.Developer}
	ctx := context.WithValue(context.WithValue(t.Context(), principalKey{}, p), lookupSinkKey{}, sink)
	withAsker := context.WithValue(ctx, permAskerKey{}, permAsker(func(r permRequest) (string, error) {
		asked = append(asked, r)
		return decisionOnce, nil
	}))
	plan := func(c context.Context, title string) (string, error) {
		raw, _ := json.Marshal(map[string]any{"title": title, "actions": []map[string]any{{"tool": "Bash", "command": "ls"}}})
		return m.srv.lookupApprovePlan(c, raw)
	}
	offered := func(c context.Context) bool {
		return slices.ContainsFunc(m.srv.lookupLines(c, authn.Developer), func(l helpLine) bool { return l.name == approvePlanTool })
	}
	if !offered(withAsker) || offered(ctx) {
		t.Error("offered with nobody to ask, or not offered with somebody")
	}
	for _, title := range []string{"", "  ", "safe \u202e plan"} {
		if _, err := plan(withAsker, title); err == nil {
			t.Errorf("title %q accepted", title)
		}
	}
	if _, err := plan(ctx, "Look"); err == nil || len(asked) != 0 {
		t.Errorf("with nobody to ask: %v", err)
	}
	if _, err := plan(withAsker, "Look"); err != nil || len(asked) != 1 {
		t.Fatalf("a plan: %v %v", err, asked)
	}
	answers := func(c context.Context) string {
		return m.srv.enginePermission(&engineRun{ctx: c, p: p}, json.RawMessage(`{"tool_name":"Bash","input":{"command":"ls"}}`))
	}
	m.admin.expect(200, "PUT", "/v1/assistant/sandbox", map[string]any{"enabled": false})
	if got := answers(ctx); !strings.Contains(got, `"deny"`) {
		t.Errorf("an approval for the sandbox ran on the server: %s", got)
	}
	if offered(withAsker) {
		t.Error("offered with the sandbox off")
	}
	if _, err := plan(withAsker, "Look"); err == nil {
		t.Error("taken with the sandbox off")
	}
	m.admin.expect(200, "PUT", "/v1/assistant/sandbox", map[string]any{"enabled": true})
	if got := answers(ctx); !strings.Contains(got, `"allow"`) {
		t.Errorf("the approval was not used in the sandbox: %s", got)
	}
	m.admin.expect(200, "PUT", "/v1/integrations/assistant", map[string]any{"backend": "cli", "model": ""})
	if offered(withAsker) {
		t.Error("offered to an agent that does not ask Forgeline about its tools this way")
	}
}

func toolsGiven(t *testing.T, m *mcpHarness) []string {
	t.Helper()
	var tools []string
	b, _ := os.ReadFile(filepath.Join(m.srv.cfg.DataDir, "tools.json"))
	json.Unmarshal(b, &tools)
	return tools
}

func TestOnlyWhatACardCanShowInFullGoesOnAPlan(t *testing.T) {
	bash := func(c string) map[string]any { return map[string]any{"tool": "Bash", "command": c} }
	file := func(p string) map[string]any { return map[string]any{"tool": "Write", "file_path": p, "content": "x"} }
	many := func(n, size int) (l []map[string]any) {
		for i := range n {
			l = append(l, map[string]any{"tool": "Write", "file_path": fmt.Sprintf("/work/%d", i), "content": strings.Repeat("x", size)})
		}
		return l
	}
	for name, l := range map[string][]map[string]any{
		"nothing":             {},
		"too many":            many(maxPlanActions+1, 1),
		"too much altogether": many(6, maxPlanActionText),
		"a long command":      {bash(strings.Repeat("x", maxPlanCommand+1))},
		"a long file":         many(1, maxPlanActionText+1),
		"a long old text":     {{"tool": "Edit", "file_path": "/work/a", "old_string": strings.Repeat("x", maxPlanActionText+1), "new_string": "y"}},
		"a long new text":     {{"tool": "Edit", "file_path": "/work/a", "old_string": "y", "new_string": strings.Repeat("x", maxPlanActionText+1)}},
		"another tool":        {{"tool": "WebFetch", "url": "https://example.com"}},
		"an empty command":    {bash("  ")},
		"a hidden character":  {bash("ls \u202e fr- mr")},
		"a carriage return":   {bash("rm -rf x\recho safe")},
		"a zero width one":    {bash("l\u200bs")},
		"a line separator":    {bash("ls\u2028rm x")},
		"outside /work":       {file("/etc/passwd")},
		"dot dot":             {file("/work/../etc/passwd")},
		"a relative path":     {file("a.txt")},
		"the agent's home":    {file("/work/.agent/.claude/settings.json")},
		"the home itself":     {file("/work/.agent")},
		"background":          {{"tool": "Bash", "command": "ls", "run_in_background": true}},
		"an unknown argument": {{"tool": "Write", "file_path": "/work/a", "content": "x", "mode": "777"}},
		"not text":            {{"tool": "Bash", "command": 3}},
		"content not text":    {{"tool": "Write", "file_path": "/work/a", "content": 3}},
		"a tool with nothing": {{"tool": "WebFetch"}},
		"replace_all as text": {{"tool": "Edit", "file_path": "/work/a", "old_string": "a", "new_string": "b", "replace_all": "yes"}},
	} {
		if _, err := readPlan(l); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	// What was approved for one occurrence does not cover all of them, nor the other way round.
	one := map[string]any{"file_path": "/work/a", "old_string": "a", "new_string": "b"}
	all := map[string]any{"file_path": "/work/a", "old_string": "a", "new_string": "b", "replace_all": true}
	for _, pair := range [][2]map[string]any{{one, all}, {all, one}} {
		g := &planGrant{}
		a, err := planActionOf("Edit", pair[0])
		if err != nil {
			t.Fatal(err)
		}
		g.add([]planAction{a})
		if g.take("Edit", pair[1]) || !g.take("Edit", pair[0]) || g.take("Edit", pair[0]) {
			t.Errorf("replace_all %v: the approval covered something else, or was not used once", pair[0]["replace_all"])
		}
	}
	ok := []map[string]any{bash("set -e\n\tls -la | grep é"), file("/work/a b.txt"), {"tool": "Edit", "file_path": "/work/a", "old_string": "a", "new_string": ""}}
	if got, err := readPlan(ok); err != nil || len(got) != 3 {
		t.Errorf("a good plan: %v %v", got, err)
	}
}

// The model is told about an argument only if the tool's schema has it, and the schema is read from the first example in the
// help: every field a plan item can have must be there.
func TestThePlanToolsSchemaNamesEveryFieldOfAnItem(t *testing.T) {
	var help string
	for _, l := range assistantLookups() {
		if l.name == approvePlanTool {
			help = l.help
		}
	}
	schema, _ := schemaFromHelp(help)
	actions, _ := schema["properties"].(map[string]any)["actions"].(map[string]any)
	items, _ := actions["items"].(map[string]any)
	props, _ := items["properties"].(map[string]any)
	typ := reflect.TypeOf(planAction{})
	for i := range typ.NumField() {
		name, _, _ := strings.Cut(typ.Field(i).Tag.Get("json"), ",")
		p, ok := props[name].(map[string]any)
		want := "string"
		if typ.Field(i).Type.Kind() == reflect.Bool {
			want = "boolean"
		}
		if !ok || p["type"] != want {
			t.Errorf("%s: %v, want a %s", name, props[name], want)
		}
	}
}
