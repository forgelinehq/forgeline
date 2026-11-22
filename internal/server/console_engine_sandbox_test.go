package server

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/forgelinehq/forgeline/internal/authn"
)

// The stand-in program for the container: like fakeClaude, but it reaches Forgeline the way the real one does in a
// container, through the bridge's socket, here reached directly.
const fakeClaudeSocket = `#!/usr/bin/env python3
import sys, json, socket, os
args = sys.argv[1:]
open('args.json', 'a').write(json.dumps(args) + '\n')
cfg = json.loads(args[args.index('--mcp-config') + 1])['mcpServers']['forgeline']
path = cfg['env']['FORGELINE_MCP_PATH']
import glob
sock = glob.glob(os.getcwd() + '/engine/*/bridge/engine.sock')[0]
def rpc(method, params=None, i=[0]):
    i[0] += 1
    body = json.dumps({'jsonrpc': '2.0', 'id': i[0], 'method': method, 'params': params or {}}).encode()
    s = socket.socket(socket.AF_UNIX); s.connect(sock)
    s.sendall(b'POST ' + path.encode() + b' HTTP/1.0\r\nHost: forgeline\r\nContent-Type: application/json\r\nContent-Length: ' + str(len(body)).encode() + b'\r\n\r\n' + body)
    data = b''
    while True:
        c = s.recv(65536)
        if not c: break
        data += c
    return json.loads(data.split(b'\r\n\r\n', 1)[1])['result']
sysfile = args[args.index('--append-system-prompt-file') + 1]
print(json.dumps({'type': 'system', 'subtype': 'init', 'session_id': 'c-1'}), flush=True)
tools = [t['name'] for t in rpc('tools/list')['tools']]
dec = json.loads(rpc('tools/call', {'name': 'approve', 'arguments': {'tool_name': 'Bash', 'input': {'command': 'make deploy'}}})['content'][0]['text'])
print(json.dumps({'type': 'result', 'subtype': 'success', 'is_error': False, 'result': 'in container: ' + dec['behavior'] + ' ' + str(len(tools)) + ' tools; token=' + os.environ.get('CLAUDE_CODE_OAUTH_TOKEN', '')[:6], 'session_id': 'c-1'}), flush=True)
`

func TestTheAgentRunsInTheSandboxContainerAndReachesForgelineThroughASocket(t *testing.T) {
	bin := t.TempDir()
	if err := os.WriteFile(filepath.Join(bin, "claude"), []byte(fakeClaudeSocket), 0o755); err != nil {
		t.Fatal(err)
	}
	m := newMCPHarness(t)
	d := &dockerStub{runtimes: `{"runc":{"path":"runc"}}`, noImage: true}
	m.srv.run = d.run
	short, _ := os.MkdirTemp("/tmp", "flE")
	t.Cleanup(func() { os.RemoveAll(short) })
	m.srv.cfg.DataDir = short
	var started []string
	m.srv.engineExec = func(ctx context.Context, name string, args ...string) *exec.Cmd {
		started = append([]string{name}, args...)
		i := 0
		for j, a := range args {
			if a == "claude" && j > 0 && args[j-1] != "-e" {
				i = j
			}
		}
		cmd := exec.CommandContext(ctx, filepath.Join(bin, "claude"), args[i+1:]...)
		cmd.Dir = short
		return cmd
	}
	if _, _, err := m.srv.putSecret(t.Context(), "ada", claudeTokenName, "sk-ant-oat01-test-token-0123456789", "t"); err != nil {
		t.Fatal(err)
	}
	m.admin.expect(200, "PUT", "/v1/assistant/sandbox", map[string]any{"enabled": true})
	m.admin.expect(200, "PUT", "/v1/integrations/assistant", map[string]any{"backend": "claude-agent", "model": ""})

	st := m.streamWith(m.dev, map[string]any{"message": "deploy it"}, "unused")
	id := m.waitPending(t)
	time.Sleep(30 * time.Millisecond)
	m.dev.expect(200, "POST", "/v1/assistant/approvals/"+id, map[string]any{"decision": "deny"})
	events, _ := answerOf(t, st)
	var text string
	for _, f := range events {
		if f["type"] == "TEXT_MESSAGE_CONTENT" {
			text += f["delta"].(string)
		}
	}
	if !strings.Contains(text, "in container: deny") || !strings.Contains(text, "token=sk-ant") {
		t.Fatalf("the agent's answer: %q (events %v)", text, events)
	}

	// The image was built once, the container is the person's sandbox with the socket and the bridge mounted read-only,
	// and the program was started inside it with the token in the environment, not on the command line.
	if len(d.find("build")) != 1 {
		t.Errorf("the agent image was built %d times", len(d.find("build")))
	}
	runs := d.find("run")
	if len(runs) != 1 {
		t.Fatalf("docker run calls: %v", runs)
	}
	run := strings.Join(runs[0], " ")
	for _, want := range []string{":/run/forgeline:ro", ":/opt/forgeline:ro", "forgeline/agent:1 sleep infinity", "--cap-drop ALL", "--network bridge"} {
		if !strings.Contains(run, want) {
			t.Errorf("docker run lacks %q:\n%s", want, run)
		}
	}
	line := strings.Join(started, " ")
	for _, want := range []string{"docker exec -i -w /work", "-e HOME=/work/.agent", "-e " + claudeTokenName + " ", "--permission-prompt-tool mcp__forgeline__approve", "--append-system-prompt-file /run/forgeline/system-", "/opt/forgeline/forgeline", "FORGELINE_MCP_SOCKET", "/run/forgeline/engine.sock"} {
		if !strings.Contains(line, want) {
			t.Errorf("the program was not started with %q:\n%s", want, line)
		}
	}
	if strings.Contains(line, "sk-ant-oat01") {
		t.Error("the token is on the command line")
	}
	if left, _ := filepath.Glob(filepath.Join(short, "engine", "*", "bridge", "system-*.md")); len(left) != 0 {
		t.Errorf("the system prompt stays on disk after the run: %v", left)
	}
	// With the network off the agent cannot reach its model, and with no token the server user's login is not used.
	m.admin.expect(200, "PUT", "/v1/assistant/sandbox", map[string]any{"network": "none"})
	st = m.streamWith(m.dev, map[string]any{"message": "again"}, "unused")
	_, _ = answerOf(t, st)
	if !strings.Contains(string(st.body), "network cannot be set to none") {
		t.Errorf("network none: %s", st.body)
	}
}

func TestTheEngineSocketServesOnlyTheEngineEndpointAndNeedsALiveRunToken(t *testing.T) {
	m := newMCPHarness(t)
	short, _ := os.MkdirTemp("/tmp", "flS")
	t.Cleanup(func() { os.RemoveAll(short) })
	if err := m.srv.engineSocket(short); err != nil {
		t.Fatal(err)
	}
	engineRuns.mu.Lock()
	engineRuns.m["live-token"] = &engineRun{ctx: t.Context(), p: Principal{UserID: "u", Name: "u", Role: authn.Developer}, set: buildNativeSet(nil)}
	engineRuns.mu.Unlock()
	defer func() { engineRuns.mu.Lock(); delete(engineRuns.m, "live-token"); engineRuns.mu.Unlock() }()
	client := &http.Client{Transport: &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		var d net.Dialer
		return d.DialContext(ctx, "unix", filepath.Join(short, "engine.sock"))
	}}}
	post := func(path string) int {
		resp, err := client.Post("http://x"+path, "application/json", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`))
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		return resp.StatusCode
	}
	if c := post("/mcp/engine/live-token"); c != 200 {
		t.Errorf("a live token on the socket: %d", c)
	}
	if c := post("/mcp/engine/dead-token"); c != 404 {
		t.Errorf("an unknown token: %d", c)
	}
	for _, p := range []string{"/v1/users", "/mcp", "/healthz", "/v1/assistant/sandbox"} {
		if c := post(p); c != 404 && c != 405 {
			t.Errorf("%s is reachable on the engine socket: %d", p, c)
		}
	}
	var raw json.RawMessage
	_ = raw
}

func TestTheContainerAgentNeedsATokenAndOnlyBuildsTheImageWhenItIsMissing(t *testing.T) {
	m := newMCPHarness(t)
	d := &dockerStub{runtimes: `{"runc":{"path":"runc"}}`}
	m.srv.run = d.run
	short, _ := os.MkdirTemp("/tmp", "flT")
	t.Cleanup(func() { os.RemoveAll(short) })
	m.srv.cfg.DataDir = short
	m.srv.engineExec = func(ctx context.Context, name string, args ...string) *exec.Cmd {
		return exec.CommandContext(ctx, "true")
	}
	m.admin.expect(200, "PUT", "/v1/assistant/sandbox", map[string]any{"enabled": true})
	m.admin.expect(200, "PUT", "/v1/integrations/assistant", map[string]any{"backend": "claude-agent", "model": ""})
	// No Claude Code token: the server user's login is not carried into a container.
	run := func() string {
		st := m.streamWith(m.dev, map[string]any{"message": "go"}, "unused")
		select {
		case <-st.done:
		case <-time.After(5 * time.Second):
			t.Fatal("did not finish")
		}
		return string(st.body)
	}
	if out := run(); !strings.Contains(out, "needs a Claude Code token") || len(d.find("run")) != 0 {
		t.Errorf("no token: %s", out)
	}
	// With a token and the image present, nothing is built.
	if _, _, err := m.srv.putSecret(t.Context(), "ada", claudeTokenName, "sk-ant-oat01-test-token-0123456789", "t"); err != nil {
		t.Fatal(err)
	}
	run()
	if len(d.find("build")) != 0 || len(d.find("run")) != 1 {
		t.Errorf("image present: %d builds, %d containers started", len(d.find("build")), len(d.find("run")))
	}
}
