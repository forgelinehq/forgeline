package server

import (
	"encoding/json"
	"github.com/forgelinehq/forgeline/internal/authn"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

// A stand-in for the claude program. It does what the real one does with Forgeline: reads the MCP address from its
// arguments, lists the tools, calls one, asks the permission prompt before its own Bash, and prints stream-json.
const fakeClaude = `#!/usr/bin/env python3
import sys, json, urllib.request, os
args = sys.argv[1:]
open('args.json', 'a').write(json.dumps(args) + '\n')
cfg = json.loads(args[args.index('--mcp-config') + 1])
url = cfg['mcpServers']['forgeline']['url']
def rpc(method, params=None, i=[0]):
    i[0] += 1
    req = urllib.request.Request(url, data=json.dumps({'jsonrpc': '2.0', 'id': i[0], 'method': method, 'params': params or {}}).encode(), headers={'Content-Type': 'application/json'})
    return json.load(urllib.request.urlopen(req))['result']
prompt = sys.stdin.read()
sid = 'sess-1'
if '--resume' in args: sid = args[args.index('--resume') + 1] + '+'
print(json.dumps({'type': 'system', 'subtype': 'init', 'session_id': sid}), flush=True)
streaming = '--include-partial-messages' in args
def ev(inner):  # what the real program prints for each partial piece (checked 2026-11-25)
    if streaming: print(json.dumps({'type': 'stream_event', 'event': inner, 'session_id': sid}), flush=True)
def text(t): ev({'type': 'content_block_delta', 'index': 0, 'delta': {'type': 'text_delta', 'text': t}})
ev({'type': 'message_start', 'message': {'role': 'assistant'}})
ev({'type': 'content_block_delta', 'index': 0, 'delta': {'type': 'thinking_delta', 'thinking': 'hmm'}})
ev({'type': 'content_block_delta', 'index': 0, 'delta': {'type': 'future_delta', 'text': 'NOT-AN-ANSWER'}})
text('Let me look '); text('at them.')
rpc('initialize')
tools = [t['name'] for t in rpc('tools/list')['tools']]
res = rpc('tools/call', {'name': 'list_pipelines', 'arguments': {}})['content'][0]['text']
print(json.dumps({'type': 'assistant', 'message': {'content': [{'type': 'tool_use', 'id': 'mcp1', 'name': 'mcp__forgeline__list_pipelines', 'input': {}}]}}), flush=True)
dec = json.loads(rpc('tools/call', {'name': 'approve', 'arguments': {'tool_name': 'Bash', 'input': {'command': 'echo hello'}, 'tool_use_id': 'tu1'}})['content'][0]['text'])
print(json.dumps({'type': 'assistant', 'message': {'content': [{'type': 'tool_use', 'id': 'tu1', 'name': 'Bash', 'input': {'command': 'echo hello'}}]}}), flush=True)
out = 'hello' if dec['behavior'] == 'allow' else 'denied: ' + dec.get('message', '')
ev({'type': 'message_start', 'message': {'role': 'assistant'}})
text('Done: '); text(out)
print(json.dumps({'type': 'user', 'message': {'content': [{'type': 'tool_result', 'tool_use_id': 'tu1', 'content': out}]}}), flush=True)
open('seen.json', 'w').write(json.dumps({'tools': tools, 'list': res, 'decision': dec, 'prompt': prompt, 'cwd': os.getcwd(), 'home': os.environ.get('HOME'), 'env_keys': sorted(os.environ.keys())}))
print(json.dumps({'type': 'result', 'subtype': 'success', 'is_error': False, 'result': 'Done: ' + out, 'session_id': sid}), flush=True)
`

func engineHarness(t *testing.T) *mcpHarness {
	t.Helper()
	bin := t.TempDir()
	if err := os.WriteFile(filepath.Join(bin, "claude"), []byte(fakeClaude), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	m := newMCPHarness(t)
	m.srv.engineBase = m.http.URL
	if _, _, err := m.srv.putSecret(t.Context(), "ada", claudeTokenName, "sk-ant-oat01-test-token-0123456789", "test"); err != nil {
		t.Fatal(err)
	}
	m.admin.expect(200, "PUT", "/v1/integrations/assistant", map[string]any{"backend": "claude-agent", "model": ""})
	return m
}

func TestTheAgentEngineRunsClaudeWithForgelineAsItsToolsAndItsPermissionPrompt(t *testing.T) {
	m := engineHarness(t)
	conv := "0123456789abcdef0123456789abcdef"
	st := m.streamWith(m.dev, map[string]any{"conversationId": conv, "message": "look at my pipelines and run echo hello"}, "unused")
	id := m.waitPending(t) // the permission card for Bash
	time.Sleep(30 * time.Millisecond)
	m.dev.expect(200, "POST", "/v1/assistant/approvals/"+id, map[string]any{"decision": "approve"})
	events, _ := answerOf(t, st)

	cards := eventsNamed(events, "forgeline.permission")
	if len(cards) != 1 || cards[0]["tool"] != "Bash" || cards[0]["command"] != "echo hello" {
		t.Errorf("permission card: %v", cards)
	}
	var text string
	var steps []string
	for _, f := range events {
		switch f["type"] {
		case "TEXT_MESSAGE_CONTENT":
			text += f["delta"].(string)
		case "TOOL_CALL_START":
			steps = append(steps, f["toolCallName"].(string))
		}
	}
	// The text arrives as the program writes it, a second message on its own line, and nothing that is not answer text (a delta of
	// another kind carries the words NOT-AN-ANSWER). The history keeps all of it, not only the program's last message.
	if text != "Let me look at them.\n\nDone: hello" {
		t.Errorf("the streamed answer: %q", text)
	}
	var conversation Conversation
	json.Unmarshal(m.dev.expect(200, "GET", "/v1/assistant/conversations/"+conv, nil), &conversation)
	if len(conversation.Messages) != 2 || conversation.Messages[1].Text != "Let me look at them.\n\nDone: hello" {
		t.Errorf("history: %+v", conversation.Messages)
	}
	if strings.Join(steps, ",") != "list_pipelines,Bash" {
		t.Errorf("steps: %v (the Forgeline tool once, from the MCP call, and the program's own Bash)", steps)
	}

	// What the program saw: Forgeline's tools and the prompt tool, a workspace of its own, a clean environment.
	var seen struct {
		Tools    []string       `json:"tools"`
		Decision map[string]any `json:"decision"`
		Prompt   string         `json:"prompt"`
		Cwd      string         `json:"cwd"`
		Home     string         `json:"home"`
		EnvKeys  []string       `json:"env_keys"`
		List     string         `json:"list"`
	}
	b, err := os.ReadFile(filepath.Join(m.srv.cfg.DataDir, "engine"))
	_ = b
	_ = err
	work, home := engineWorkspace(m.srv.cfg.DataDir, Principal{UserID: "dev"})
	_ = home
	raw, rerr := os.ReadFile(filepath.Join(filepath.Dir(work), "work", "seen.json"))
	if rerr != nil {
		// the dev principal's id is not "dev": find the file
		matches, _ := filepath.Glob(filepath.Join(m.srv.cfg.DataDir, "engine", "*", "work", "seen.json"))
		if len(matches) != 1 {
			t.Fatalf("the program did not run: %v", rerr)
		}
		raw, _ = os.ReadFile(matches[0])
	}
	json.Unmarshal(raw, &seen)
	has := func(n string) bool {
		for _, x := range seen.Tools {
			if x == n {
				return true
			}
		}
		return false
	}
	if !has("list_pipelines") || !has("get_run") || !has("approve") || !has("memory") {
		t.Errorf("tools it was given: %v", seen.Tools)
	}
	if seen.Decision["behavior"] != "allow" || !strings.Contains(seen.Prompt, "look at my pipelines") {
		t.Errorf("decision/prompt: %v %q", seen.Decision, seen.Prompt)
	}
	for _, k := range seen.EnvKeys {
		if strings.Contains(strings.ToUpper(k), "FORGELINE") || strings.Contains(k, "AWS_") {
			t.Errorf("the program was given %s", k)
		}
	}
	if !slices.Contains(seen.EnvKeys, "ENABLE_TOOL_SEARCH") {
		t.Errorf("the program holds Forgeline's tools back: %v", seen.EnvKeys)
	}
	if !strings.Contains(seen.Cwd, "engine") || !strings.Contains(seen.Home, "engine") {
		t.Errorf("workspace: cwd %s home %s", seen.Cwd, seen.Home)
	}

	// The arguments: the permission prompt is Forgeline's, other MCP servers are ignored, its own writes are not pre-allowed.
	matches, _ := filepath.Glob(filepath.Join(m.srv.cfg.DataDir, "engine", "*", "work", "args.json"))
	argl, _ := os.ReadFile(matches[0])
	line := strings.Split(strings.TrimSpace(string(argl)), "\n")[0]
	for _, want := range []string{"--permission-prompt-tool", "mcp__forgeline__approve", "--strict-mcp-config", "--output-format", "stream-json", "--include-partial-messages", "--disallowedTools", "AskUserQuestion", "ScheduleWakeup", "Monitor"} {
		if !strings.Contains(line, want) {
			t.Errorf("the program was not started with %s: %s", want, line)
		}
	}
	for _, bad := range []string{"dangerously-skip-permissions", `"Bash"`, `"Write"`} {
		if strings.Contains(line, bad) {
			t.Errorf("the program was started with %s", bad)
		}
	}

	// A second message in the same conversation resumes the session.
	st2 := m.streamWith(m.dev, map[string]any{"conversationId": conv, "message": "and again"}, "unused")
	id2 := m.waitPending(t)
	m.dev.expect(200, "POST", "/v1/assistant/approvals/"+id2, map[string]any{"decision": "deny"})
	events2, _ := answerOf(t, st2)
	text = ""
	for _, f := range events2 {
		if f["type"] == "TEXT_MESSAGE_CONTENT" {
			text += f["delta"].(string)
		}
	}
	if !strings.Contains(text, "denied") {
		t.Errorf("a refused tool was not refused to the program: %q", text)
	}
	argl, _ = os.ReadFile(matches[0])
	if lines := strings.Split(strings.TrimSpace(string(argl)), "\n"); len(lines) != 2 || !strings.Contains(lines[1], "--resume") || !strings.Contains(lines[1], "sess-1") {
		t.Errorf("the second message did not resume the session: %v", lines)
	}
	// The endpoint is closed once the run is over, and it is not for other machines.
	resp, _ := m.http.Client().Post(m.http.URL+"/mcp/engine/nothing", "application/json", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"ping"}`))
	if resp.StatusCode != 404 {
		t.Errorf("an unknown run token: %d", resp.StatusCode)
	}
}

func TestTheEngineEndpointIsForTheLocalProgramOnlyAndOnlyWhileItRuns(t *testing.T) {
	m := newMCPHarness(t)
	run := &engineRun{ctx: t.Context(), p: Principal{UserID: "u", Name: "u", Role: authn.Developer}, set: buildNativeSet(nil)}
	engineRuns.mu.Lock()
	engineRuns.m["tok123"] = run
	engineRuns.mu.Unlock()
	defer func() { engineRuns.mu.Lock(); delete(engineRuns.m, "tok123"); engineRuns.mu.Unlock() }()
	call := func(remote string) int {
		req := httptest.NewRequest("POST", "/mcp/engine/tok123", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"ping"}`))
		req.SetPathValue("token", "tok123")
		req.RemoteAddr = remote
		w := httptest.NewRecorder()
		m.srv.handleEngineMCP(w, req)
		return w.Code
	}
	if c := call("127.0.0.1:5555"); c != 200 {
		t.Errorf("the local program: %d", c)
	}
	if c := call("203.0.113.9:5555"); c != 403 {
		t.Errorf("another machine got %d", c)
	}
	// With nobody to ask, the program's own tool is refused rather than run.
	if out := m.srv.enginePermission(run, json.RawMessage(`{"tool_name":"Bash","input":{"command":"id"}}`)); !strings.Contains(out, `"deny"`) {
		t.Errorf("a tool ran with nobody to approve it: %s", out)
	}
}

func TestEngineRunsAreForgottenWhenTheyEnd(t *testing.T) {
	m := engineHarness(t)
	st := m.streamWith(m.dev, map[string]any{"message": "go"}, "unused")
	id := m.waitPending(t)
	m.dev.expect(200, "POST", "/v1/assistant/approvals/"+id, map[string]any{"decision": "approve"})
	answerOf(t, st)
	engineRuns.mu.Lock()
	n := len(engineRuns.m)
	engineRuns.mu.Unlock()
	if n != 0 {
		t.Errorf("%d engine runs are still reachable after the answer", n)
	}
}
