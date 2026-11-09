package server

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A fake copilot program that records how it was started and answers in its JSON lines.
func TestAssistantCopilotBackend(t *testing.T) {
	dir := t.TempDir()
	record := filepath.Join(dir, "record")
	script := `#!/bin/sh
{ echo "ARGS: $*"; echo "SERVER=$FORGELINE_TOKEN"; echo "GH=$GITHUB_TOKEN"; echo "PWD=$PWD"; cat; } > ` + record + `
usage=""
prev=""
for a in "$@"; do [ "$prev" = "--usage-output-file" ] && usage="$a"; prev="$a"; done
[ -n "$usage" ] && echo '{"tokenDetails":{"input":{"tokenCount":40},"cache_write":{"tokenCount":2},"output":{"tokenCount":7}}}' > "$usage"
echo '{"type":"session.tools_updated","data":{"model":"x"}}'
echo '{"type":"assistant.message","data":{"content":"Copilot says fine.","toolRequests":[]}}'
echo '{"type":"result","exitCode":0,"usage":{"premiumRequests":1}}'
`
	if err := os.WriteFile(filepath.Join(dir, "copilot"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+":"+os.Getenv("PATH"))
	t.Setenv("FORGELINE_TOKEN", "must-not-leak-0123456789")
	t.Setenv("GITHUB_TOKEN", "ghp-must-not-replace-the-login")

	h := newConsoleHarness(t)
	admin := h.adminBrowser("ada")
	admin.expect(200, "PUT", "/v1/integrations/assistant", map[string]any{"backend": "copilot"})
	var reply ChatReply
	json.Unmarshal(admin.expect(200, "POST", "/v1/assistant/chat", map[string]any{"message": "Summarise the failed run."}), &reply)
	if reply.Reply != "Copilot says fine." {
		t.Fatalf("reply = %q", reply.Reply)
	}
	raw, _ := os.ReadFile(record)
	got := string(raw)
	args := got[:strings.Index(got, "\n")]
	for _, want := range []string{"--available-tools", "--disable-builtin-mcps", "--no-custom-instructions", "--no-auto-update", "--no-ask-user", "--output-format json"} {
		if !strings.Contains(args, want) {
			t.Errorf("copilot was started without %s: %s", want, args)
		}
	}
	for _, bad := range []string{"--allow-all", "--yolo", "-p ", "Summarise", "--model"} {
		if strings.Contains(args, bad) {
			t.Errorf("copilot args contain %q: %s", bad, args)
		}
	}
	if strings.Contains(got, "must-not-leak") || strings.Contains(got, "ghp-must-not") {
		t.Errorf("copilot saw the server's environment:\n%s", got)
	}
	if !strings.Contains(got, "PWD="+os.TempDir()) || !strings.Contains(got, "forgeline-copilot-") {
		t.Errorf("copilot did not run in a scratch directory:\n%s", got)
	}
	if !strings.Contains(got, "<instructions>") || !strings.Contains(got, "Now the person asks:") && !strings.Contains(got, "Summarise the failed run.") {
		t.Errorf("the instructions and the question go on stdin:\n%s", got)
	}
	// A model name is passed on; a Claude alias left in the settings is not.
	admin.expect(200, "PUT", "/v1/integrations/assistant", map[string]any{"backend": "copilot", "model": "gpt-6-luna"})
	admin.expect(200, "POST", "/v1/assistant/chat", map[string]any{"message": "hi"})
	if raw, _ := os.ReadFile(record); !strings.Contains(string(raw), "--model gpt-6-luna") {
		t.Errorf("the model was not passed: %s", raw)
	}
	admin.expect(200, "PUT", "/v1/integrations/assistant", map[string]any{"backend": "copilot", "model": "sonnet"})
	admin.expect(200, "POST", "/v1/assistant/chat", map[string]any{"message": "hi"})
	if raw, _ := os.ReadFile(record); strings.Contains(string(raw), "--model") {
		t.Errorf("a Claude alias was passed to copilot: %s", raw)
	}
}

func TestReadCopilotOutput(t *testing.T) {
	cases := []struct{ raw, reply, failed string }{
		{`{"type":"assistant.message","data":{"content":"first"}}` + "\n" + `{"type":"assistant.message","data":{"content":"  last  "}}` + "\n" + `{"type":"result","exitCode":0}`, "last", ""},
		{`{"type":"session.error","data":{"message":"quota exceeded\nmore"}}` + "\n" + `{"type":"result","exitCode":1}`, "", "quota exceeded more"},
		{`{"type":"result","exitCode":2}`, "", "exit code 2"},
		{"not json\n" + `{"type":"assistant.message","data":{"content":"ok"}}`, "ok", ""},
	}
	for _, c := range cases {
		if r, f := readCopilotOutput([]byte(c.raw)); r != c.reply || f != c.failed {
			t.Errorf("%q: got %q %q", c.raw, r, f)
		}
	}
	dir := t.TempDir()
	p := filepath.Join(dir, "u.json")
	os.WriteFile(p, []byte(`{"tokenDetails":{"input":{"tokenCount":3},"cache_read":{"tokenCount":10},"cache_write":{"tokenCount":5},"output":{"tokenCount":4}}}`), 0o600)
	if u := readCopilotUsage(p); u.Input != 18 || u.Output != 4 || u.Unknown || u.Calls != 1 {
		t.Errorf("usage = %+v", u)
	}
	if u := readCopilotUsage(filepath.Join(dir, "missing")); !u.Unknown {
		t.Errorf("missing usage must be unknown: %+v", u)
	}
}

// TestCopilotReal makes one real call (one premium request) with the installed, logged-in copilot program.
func TestCopilotReal(t *testing.T) {
	if os.Getenv("FORGELINE_COPILOT_REAL") == "" {
		t.Skip("set FORGELINE_COPILOT_REAL=1 to make one real copilot call")
	}
	reply, use, err := askCopilot(context.Background(), "", "Answer in one word.", []chatTurn{{Role: "user", Content: "What colour is a clear daytime sky?"}})
	if err != nil || !strings.Contains(strings.ToLower(reply), "blue") {
		t.Fatalf("reply %q, err %v", reply, err)
	}
	t.Logf("reply %q, tokens in %d out %d", reply, use.Input, use.Output)
}
