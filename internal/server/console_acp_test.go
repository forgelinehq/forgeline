package server

import (
	"context"
	"encoding/json"
	"github.com/forgelinehq/forgeline/internal/authn"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// A stand-in ACP agent. It speaks the protocol on its standard input and output the way a real one does: it asks to
// be authenticated, makes a plan, runs a tool that needs permission, calls Forgeline's tools through the MCP server it was
// given (here straight over the socket), and answers.
const fakeACPAgent = `#!/usr/bin/env python3
import sys, json, os, glob, socket
log = open('acp.log', 'a')
def out(m): sys.stdout.write(json.dumps(m) + '\n'); sys.stdout.flush()
def rd(): 
    l = sys.stdin.readline()
    return json.loads(l) if l else None
authed = os.path.exists('authed')
copilot = os.path.exists('copilot-mode')  # behave like the real Copilot CLI (checked 2026-11-23): HTTP MCP only, asks before every tool
mcp = None
def rpc(method, params=None, i=[0]):
    i[0] += 1
    cfg = mcp[0]; env = {e['name']: e['value'] for e in cfg['env']}
    sock = glob.glob(os.getcwd() + '/engine/*/bridge/engine.sock')[0]
    body = json.dumps({'jsonrpc': '2.0', 'id': i[0], 'method': method, 'params': params or {}}).encode()
    s = socket.socket(socket.AF_UNIX); s.connect(sock)
    s.sendall(b'POST ' + env['FORGELINE_MCP_PATH'].encode() + b' HTTP/1.0\r\nHost: f\r\nContent-Type: application/json\r\nContent-Length: ' + str(len(body)).encode() + b'\r\n\r\n' + body)
    data = b''
    while True:
        c = s.recv(65536)
        if not c: break
        data += c
    return json.loads(data.split(b'\r\n\r\n', 1)[1])['result']
while True:
    m = rd()
    if m is None: break
    log.write(m.get('method', 'response') + '\n'); log.flush()
    meth = m.get('method'); i = m.get('id')
    if meth == 'initialize':
        if copilot:
            out({'jsonrpc': '2.0', 'id': i, 'result': {'protocolVersion': 1, 'agentCapabilities': {'loadSession': True, 'mcpCapabilities': {'http': True, 'sse': True}}, 'authMethods': [{'id': 'copilot-login'}]}}); continue
        out({'jsonrpc': '2.0', 'id': i, 'result': {'protocolVersion': 1, 'agentCapabilities': {'loadSession': True, 'sessionCapabilities': {'resume': {}}}, 'authMethods': [{'id': 'oauth-personal'}, {'id': 'gemini-api-key'}]}})
    elif meth == 'authenticate':
        open('authed', 'w').write(m['params']['methodId']); authed = True
        out({'jsonrpc': '2.0', 'id': i, 'result': {}})
    elif meth == 'session/new':
        if not authed and not copilot:
            out({'jsonrpc': '2.0', 'id': i, 'error': {'code': -32000, 'message': 'Authentication required'}}); continue
        mcp = m['params']['mcpServers']
        open('mcp.json', 'w').write(json.dumps(m['params']))
        out({'jsonrpc': '2.0', 'id': i, 'result': {'sessionId': 's-1'}})
    elif meth in ('session/resume', 'session/load'):
        mcp = m['params']['mcpServers']
        open('resumed.json', 'w').write(json.dumps({'method': meth, 'sessionId': m['params']['sessionId']}))
        out({'jsonrpc': '2.0', 'method': 'session/update', 'params': {'sessionId': 's-1', 'update': {'sessionUpdate': 'agent_message_chunk', 'content': {'type': 'text', 'text': 'OLD HISTORY'}}}})
        out({'jsonrpc': '2.0', 'id': i, 'result': {}})
    elif meth == 'session/prompt' and copilot:
        open('prompt.txt', 'w').write(json.dumps(m['params']['prompt']))
        picks = []
        for n, (cid, title, call) in enumerate([('t2', 'list_pipelines', 'forgeline-list_pipelines'), ('t3', 'notes', 'forgeline-notes')]):
            out({'jsonrpc': '2.0', 'method': 'session/update', 'params': {'sessionId': 's-1', 'update': {'sessionUpdate': 'tool_call', 'toolCallId': cid, 'title': call, 'kind': 'read', 'status': 'pending', 'rawInput': {}}}})
            out({'jsonrpc': '2.0', 'id': 901 + n, 'method': 'session/request_permission', 'params': {'sessionId': 's-1', 'toolCall': {'toolCallId': cid, 'title': title, 'kind': 'other', 'status': 'pending', 'rawInput': {}}, 'options': [{'optionId': 'allow_once', 'name': 'Allow once', 'kind': 'allow_once'}, {'optionId': 'allow_always', 'name': 'Always allow', 'kind': 'allow_always'}, {'optionId': 'reject_once', 'name': 'Reject', 'kind': 'reject_once'}]}})
            r = rd()
            while r is not None and r.get('id') != 901 + n: r = rd()
            picks.append(cid + '=' + r['result']['outcome'].get('optionId', r['result']['outcome']['outcome']))
        open('perm.txt', 'w').write(','.join(picks))
        out({'jsonrpc': '2.0', 'method': 'session/update', 'params': {'sessionId': 's-1', 'update': {'sessionUpdate': 'agent_message_chunk', 'content': {'type': 'text', 'text': 'Permissions: ' + ','.join(picks)}}}})
        out({'jsonrpc': '2.0', 'id': i, 'result': {'stopReason': 'end_turn'}})
    elif meth == 'session/prompt':
        open('prompt.txt', 'w').write(json.dumps(m['params']['prompt']))
        out({'jsonrpc': '2.0', 'method': 'session/update', 'params': {'sessionId': 's-1', 'update': {'sessionUpdate': 'plan', 'entries': [{'content': 'Look around', 'priority': 'high', 'status': 'in_progress'}, {'content': 'Report', 'priority': 'low', 'status': 'pending'}]}}})
        out({'jsonrpc': '2.0', 'method': 'session/update', 'params': {'sessionId': 's-1', 'update': {'sessionUpdate': 'tool_call', 'toolCallId': 't1', 'title': 'Run ls', 'kind': 'execute', 'status': 'pending', 'rawInput': {'command': 'ls -la'}}}})
        out({'jsonrpc': '2.0', 'id': 900, 'method': 'session/request_permission', 'params': {'sessionId': 's-1', 'toolCall': {'toolCallId': 't1', 'title': 'Run ls', 'kind': 'execute', 'rawInput': {'command': 'ls -la'}}, 'options': [{'optionId': 'a', 'name': 'Allow', 'kind': 'allow_once'}, {'optionId': 'r', 'name': 'Reject', 'kind': 'reject_once'}]}})
        r = rd()
        while r is not None and r.get('id') != 900: r = rd()
        outcome = r['result']['outcome']
        picked = outcome.get('optionId', outcome['outcome'])
        tools = len(rpc('tools/list')['tools'])
        out({'jsonrpc': '2.0', 'method': 'session/update', 'params': {'sessionId': 's-1', 'update': {'sessionUpdate': 'tool_call_update', 'toolCallId': 't1', 'status': 'completed', 'content': [{'type': 'content', 'content': {'type': 'text', 'text': 'file1 file2'}}]}}})
        out({'jsonrpc': '2.0', 'method': 'session/update', 'params': {'sessionId': 's-1', 'update': {'sessionUpdate': 'tool_call', 'toolCallId': 't2', 'title': 'forgeline list_pipelines', 'kind': 'other'}}})
        out({'jsonrpc': '2.0', 'method': 'session/update', 'params': {'sessionId': 's-1', 'update': {'sessionUpdate': 'agent_thought_chunk', 'content': {'type': 'text', 'text': 'thinking, not shown'}}}})
        out({'jsonrpc': '2.0', 'method': 'session/update', 'params': {'sessionId': 's-1', 'update': {'sessionUpdate': 'agent_message_chunk', 'content': {'type': 'text', 'text': 'Picked ' + picked + ' with ' + str(tools) + ' tools.'}}}})
        out({'jsonrpc': '2.0', 'id': i, 'result': {'stopReason': 'end_turn'}})
    elif meth == 'session/cancel':
        pass
`

// acpLastCmd is the most recent agent process the tests started (its environment is what the agent was given).
var acpLastCmd *exec.Cmd

func acpHarness(t *testing.T) (*mcpHarness, *dockerStub, *[]string, string) {
	t.Helper()
	bin := t.TempDir()
	if err := os.WriteFile(filepath.Join(bin, "agent"), []byte(fakeACPAgent), 0o755); err != nil {
		t.Fatal(err)
	}
	m := newMCPHarness(t)
	d := &dockerStub{runtimes: `{"runc":{"path":"runc"}}`}
	m.srv.run = d.run
	short, _ := os.MkdirTemp("/tmp", "flA")
	t.Cleanup(func() { os.RemoveAll(short) })
	m.srv.cfg.DataDir = short
	started := new([]string)
	m.srv.engineExec = func(ctx context.Context, name string, args ...string) *exec.Cmd {
		*started = append([]string{name}, args...)
		cmd := exec.CommandContext(ctx, filepath.Join(bin, "agent"))
		cmd.Dir = short
		acpLastCmd = cmd // askACP sets its environment after this returns, so a test reads it once the run is over
		return cmd
	}
	m.admin.expect(200, "PUT", "/v1/assistant/sandbox", map[string]any{"enabled": true})
	m.admin.expect(200, "PUT", "/v1/integrations/assistant", map[string]any{"backend": "acp-agent", "harness": "antigravity"})
	return m, d, started, short
}

func textOf(events []map[string]any) (text string, steps []string) {
	for _, f := range events {
		switch f["type"] {
		case "TEXT_MESSAGE_CONTENT":
			text += f["delta"].(string)
		case "TOOL_CALL_START":
			steps = append(steps, f["toolCallName"].(string))
		}
	}
	return
}

func TestAnACPAgentRunsInTheSandboxWithForgelineAsItsToolsAndPermissionPrompt(t *testing.T) {
	m, d, started, short := acpHarness(t)
	// Signed in by a Gemini API key from the vault: the key goes in the environment, and the agent is asked to authenticate.
	if _, _, err := m.srv.putSecret(t.Context(), "ada", geminiKeyName, "AIzaSyTestKey0123456789abcdefghij", "t"); err != nil {
		t.Fatal(err)
	}
	st := m.streamWith(m.dev, map[string]any{"message": "list the files", "conversationId": apConv}, "unused")
	id := m.waitPending(t)
	time.Sleep(30 * time.Millisecond)
	m.dev.expect(200, "POST", "/v1/assistant/approvals/"+id, map[string]any{"decision": "approve"})
	events, _ := answerOf(t, st)
	cards := eventsNamed(events, "forgeline.permission")
	if len(cards) != 1 || cards[0]["tool"] != "Bash" || cards[0]["command"] != "ls -la" {
		t.Errorf("permission card: %v", cards)
	}
	text, steps := textOf(events)
	if !strings.Contains(text, "Picked a with") || strings.Contains(text, "thinking") || strings.Contains(text, "OLD HISTORY") {
		t.Errorf("the answer: %q", text)
	}
	if strings.Join(steps, ",") != "execute" {
		t.Errorf("steps: %v (the agent's own tool once; Forgeline's own tool call is shown by Forgeline, not twice)", steps)
	}
	if plans := eventsNamed(events, "forgeline.plan"); len(plans) != 1 {
		t.Errorf("plan events: %v", plans)
	}
	line := strings.Join(*started, " ")
	for _, want := range []string{"docker exec -i -w /work", "-e HOME=/work/.agent", "-e " + geminiKeyName + " ", "sh -c export PATH=/work/.antigravity/bin", "agy acp"} {
		if !strings.Contains(line, want) {
			t.Errorf("the agent was not started with %q:\n%s", want, line)
		}
	}
	if strings.Contains(line, "AIzaSy") {
		t.Error("the API key is on the command line")
	}
	raw, _ := os.ReadFile(filepath.Join(short, "authed"))
	if string(raw) != "gemini-api-key" {
		t.Errorf("authenticated with %q", raw)
	}
	var mcp struct {
		Cwd        string `json:"cwd"`
		McpServers []struct {
			Name    string
			Command string
			Args    []string
			Env     []struct{ Name, Value string }
		} `json:"mcpServers"`
	}
	b, _ := os.ReadFile(filepath.Join(short, "mcp.json"))
	json.Unmarshal(b, &mcp)
	if mcp.Cwd != "/work" || len(mcp.McpServers) != 1 || mcp.McpServers[0].Command != "/opt/forgeline/forgeline" || mcp.McpServers[0].Args[0] != "mcp" || len(mcp.McpServers[0].Env) != 2 {
		t.Errorf("the MCP server given to the agent: %+v", mcp)
	}
	prompt, _ := os.ReadFile(filepath.Join(short, "prompt.txt"))
	if !strings.Contains(string(prompt), "forgeline_instructions") || !strings.Contains(string(prompt), "list the files") {
		t.Errorf("the prompt: %s", prompt)
	}
	_ = d

	// A refusal reaches the agent as a rejection; the next message resumes the session instead of starting a new one.
	st = m.streamWith(m.dev, map[string]any{"message": "again", "conversationId": apConv}, "unused")
	id = m.waitPending(t)
	m.dev.expect(200, "POST", "/v1/assistant/approvals/"+id, map[string]any{"decision": "deny"})
	events, _ = answerOf(t, st)
	if text, _ := textOf(events); !strings.Contains(text, "Picked r with") || strings.Contains(text, "OLD HISTORY") {
		t.Errorf("a refusal: %q", text)
	}
	raw, _ = os.ReadFile(filepath.Join(short, "resumed.json"))
	if !strings.Contains(string(raw), `"session/resume"`) || !strings.Contains(string(raw), "s-1") {
		t.Errorf("the second message did not resume the session: %s", raw)
	}

	// Autopilot answers it inside the sandbox, with no card.
	m.dev.expect(200, "POST", "/v1/assistant/autopilot/start", map[string]any{"conversation": apConv})
	st = m.streamWith(m.dev, map[string]any{"message": "once more", "conversationId": apConv, "autopilot": map[string]any{}}, "unused")
	events, _ = answerOf(t, st)
	if text, _ := textOf(events); !strings.Contains(text, "Picked a with") || len(eventsNamed(events, "forgeline.permission")) != 0 {
		t.Errorf("autopilot: %q", text)
	}
	m.dev.expect(200, "POST", "/v1/assistant/autopilot/stop", map[string]any{"conversation": apConv})
}

func TestAnACPAgentThatIsNotSignedInSaysHowToSignIn(t *testing.T) {
	m, _, _, _ := acpHarness(t)
	st := m.streamWith(m.dev, map[string]any{"message": "hi"}, "unused")
	select {
	case <-st.done:
	case <-time.After(10 * time.Second):
		t.Fatal("did not finish")
	}
	body := string(st.body)
	for _, want := range []string{"is not signed in", "docker exec -it", "GEMINI_API_KEY"} {
		if !strings.Contains(body, want) {
			t.Errorf("the message lacks %q: %s", want, body)
		}
	}
}

func TestChoosingAnACPAgentIsChecked(t *testing.T) {
	m := newMCPHarness(t)
	put := func(code int, body map[string]any) {
		m.admin.expect(code, "PUT", "/v1/integrations/assistant", body)
	}
	put(400, map[string]any{"backend": "acp-agent", "harness": "nonsense"})
	put(400, map[string]any{"backend": "acp-agent", "harness": ""})
	put(400, map[string]any{"backend": "acp-agent", "harness": "custom", "harness_cmd": ""})
	put(400, map[string]any{"backend": "acp-agent", "harness": "custom", "harness_cmd": "a\nb"})
	put(400, map[string]any{"backend": "acp-agent", "harness": "custom", "harness_cmd": strings.Repeat("x", 301)})
	put(200, map[string]any{"backend": "acp-agent", "harness": "copilot"})
	put(200, map[string]any{"backend": "acp-agent", "harness": "custom", "harness_cmd": "myagent acp --flag"})
	m.dev.expect(403, "PUT", "/v1/integrations/assistant", map[string]any{"backend": "acp-agent", "harness": "copilot"})
	// Without the sandbox the agent has nowhere safe to run.
	st := m.streamWith(m.dev, map[string]any{"message": "hi"}, "unused")
	select {
	case <-st.done:
	case <-time.After(5 * time.Second):
		t.Fatal("did not finish")
	}
	if !strings.Contains(string(st.body), "run in the sandbox") {
		t.Errorf("sandbox off: %s", st.body)
	}
}

func TestAnACPAgentNeedsTheSandboxNetworkAndRejectsWhenNobodyCanAnswer(t *testing.T) {
	m, _, _, _ := acpHarness(t)
	m.admin.expect(200, "PUT", "/v1/assistant/sandbox", map[string]any{"network": "none"})
	st := m.streamWith(m.dev, map[string]any{"message": "hi"}, "unused")
	select {
	case <-st.done:
	case <-time.After(5 * time.Second):
		t.Fatal("did not finish")
	}
	if !strings.Contains(string(st.body), "network cannot be set to none") {
		t.Errorf("network none: %s", st.body)
	}
	// With nobody to ask, a permission request is rejected, never allowed.
	run := &engineRun{ctx: t.Context(), p: Principal{UserID: "u", Name: "u", Role: authn.Developer}, set: buildNativeSet(nil)}
	out, _ := json.Marshal(m.srv.acpPermission(run, json.RawMessage(`{"toolCall":{"title":"rm","kind":"execute"},"options":[{"optionId":"a","kind":"allow_once"},{"optionId":"r","kind":"reject_once"}]}`), nil))
	if !strings.Contains(string(out), `"optionId":"r"`) {
		t.Errorf("nobody to ask: %s", out)
	}
}

// The preset commands are run by `sh -c` in the container, so each is run here for real with a stand-in program where the
// agent lives, to be sure the shell finds it.
func TestEachAgentPresetFindsItsProgramWhenRunByTheShell(t *testing.T) {
	home := t.TempDir()
	bin := filepath.Join(home, ".local", "bin")
	os.MkdirAll(bin, 0o755)
	for _, prog := range []string{"agy", "copilot", "codex-acp"} {
		os.WriteFile(filepath.Join(bin, prog), []byte("#!/bin/sh\necho \"ran "+prog+" $*\"\n"), 0o755)
	}
	for harness, want := range map[string]string{"antigravity": "ran agy acp", "copilot": "ran copilot --acp", "codex": "ran codex-acp"} {
		cmd := exec.Command("sh", "-c", harnessCommands[harness])
		cmd.Env = []string{"HOME=" + home, "PATH=" + bin + ":/usr/bin:/bin"}
		if harness == "antigravity" {
			cmd.Env = []string{"HOME=" + home, "PATH=/usr/bin:/bin"} // agy is found through the command's own PATH
		}
		out, err := cmd.CombinedOutput()
		if err != nil || !strings.Contains(string(out), want) {
			t.Errorf("%s: %q %v", harness, out, err)
		}
	}
	// The Antigravity installer's own folder is searched too.
	cmd := exec.Command("sh", "-c", strings.ReplaceAll(harnessCommands["antigravity"], "/work/.antigravity/bin", home+"/.local/bin"))
	cmd.Env = []string{"HOME=" + t.TempDir(), "PATH=/usr/bin:/bin"}
	if out, err := cmd.CombinedOutput(); err != nil || !strings.Contains(string(out), "ran agy acp") {
		t.Errorf("the installer's folder: %q %v", out, err)
	}
}

// Checked against the real Copilot CLI (1.0.107, 2026-11-23): it ignores a stdio MCP server and takes an HTTP one, and it asks
// permission before every tool, Forgeline's own included. So its tools are offered over HTTP on the container's loopback
// (forwarded to the engine socket), a request to use one of Forgeline's own tools is allowed without a card, and a tool of
// some other server that merely has a similar name still asks.
func TestACopilotAgentGetsForgelineOverHTTPAndAsksOnlyAboutItsOwnTools(t *testing.T) {
	m, _, started, short := acpHarness(t)
	if err := os.WriteFile(filepath.Join(short, "copilot-mode"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	m.admin.expect(200, "PUT", "/v1/integrations/assistant", map[string]any{"backend": "acp-agent", "harness": "copilot"})
	st := m.streamWith(m.dev, map[string]any{"message": "which pipelines exist?", "conversationId": apConv}, "unused")
	id := m.waitPending(t) // only the lookalike tool asks
	time.Sleep(30 * time.Millisecond)
	m.dev.expect(200, "POST", "/v1/assistant/approvals/"+id, map[string]any{"decision": "approve"})
	events, _ := answerOf(t, st)
	if cards := eventsNamed(events, "forgeline.permission"); len(cards) != 1 {
		t.Errorf("permission cards: %v (Forgeline's own tool must not ask; the other server's tool must)", cards)
	}
	if text, _ := textOf(events); !strings.Contains(text, "t2=allow_once,t3=allow_once") {
		t.Errorf("the answer: %q", text)
	}
	raw, _ := os.ReadFile(filepath.Join(short, "perm.txt"))
	if string(raw) != "t2=allow_once,t3=allow_once" {
		t.Errorf("answers given to the agent: %q", raw)
	}

	// The tools are an HTTP server on the container's loopback, not a stdio one, and the token stays out of the arguments.
	var mcp struct {
		McpServers []struct {
			Type, Name, URL, Command string
		} `json:"mcpServers"`
	}
	b, _ := os.ReadFile(filepath.Join(short, "mcp.json"))
	json.Unmarshal(b, &mcp)
	if len(mcp.McpServers) != 1 || mcp.McpServers[0].Type != "http" || mcp.McpServers[0].Command != "" || !strings.HasPrefix(mcp.McpServers[0].URL, "http://127.0.0.1:") || !strings.Contains(mcp.McpServers[0].URL, "/mcp/engine/") {
		t.Fatalf("the MCP server given to the agent: %+v", mcp)
	}
	line := strings.Join(*started, " ")
	port := strings.Split(strings.TrimPrefix(mcp.McpServers[0].URL, "http://127.0.0.1:"), "/")[0]
	for _, want := range []string{"-e FORGELINE_MCP_PATH ", "forgeline mcp --listen 127.0.0.1:" + port, "timeout ", "trap 'kill $P", "copilot --acp"} {
		if !strings.Contains(line, want) {
			t.Errorf("the agent was not started with %q:\n%s", want, line)
		}
	}
	if strings.Contains(line, "exec copilot") {
		t.Error("the shell must stay as the agent's parent so the listener is stopped when the agent ends")
	}
	if tok := mcp.McpServers[0].URL[strings.LastIndex(mcp.McpServers[0].URL, "/")+1:]; tok == "" || strings.Contains(line, tok) {
		t.Errorf("the engine token is on the command line (token %q)", tok)
	}
}

func TestOwnToolCallsAreRecognisedStrictly(t *testing.T) {
	set := buildNativeSet([]helpLine{{"list_pipelines", `{} lists pipelines`}, {"get_run", `{"id":"x"} one run`}})
	cases := []struct {
		title string
		own   bool
	}{
		{"forgeline-list_pipelines", true}, {"forgeline_get_run", true}, {"forgeline list_pipelines", true}, {"Forgeline-List_Pipelines", true},
		{"forgeline-notes", false}, {"forgeline-", false}, {"forgeline", false}, {"my-forgeline-list_pipelines", false},
		{"list_pipelines", false}, {"forgelinelist_pipelines", false}, {"forgeline-list_pipelines; rm -rf /", false},
	}
	for _, c := range cases {
		own := map[string]bool{}
		noteOwnCall(json.RawMessage(`{"update":{"sessionUpdate":"tool_call","toolCallId":"x","title":`+strconvQuote(c.title)+`}}`), set, own)
		if own["x"] != c.own {
			t.Errorf("%q: own = %v, want %v", c.title, own["x"], c.own)
		}
	}
	// Only a tool_call announces one.
	own := map[string]bool{}
	noteOwnCall(json.RawMessage(`{"update":{"sessionUpdate":"tool_call_update","toolCallId":"x","title":"forgeline-list_pipelines"}}`), set, own)
	if len(own) != 0 {
		t.Error("an update was taken as a call")
	}
}

func strconvQuote(s string) string { b, _ := json.Marshal(s); return string(b) }
