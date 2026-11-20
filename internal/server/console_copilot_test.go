package server

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// A fake copilot program that records how it was started and answers in its JSON lines.
// modernCopilotHelp and oldCopilotHelp stand for what `copilot --help` lists in 1.0.107 and 1.0.34.
const modernCopilotHelp = "--fleet  combine with -i, -p, or piped stdin\n-p, --prompt <text>\n--disable-client-telemetry\n--usage-output-file <file>\n--available-tools [<tools>...]\n"
const oldCopilotHelp = "-p, --prompt <text>\n--available-tools [<tools>...]\n"

// fakeCopilot puts a copilot program on PATH that answers --help with help, records how it was started and what it
// read, and answers as Copilot does; toolCount is what it says the model was offered.
func fakeCopilot(t *testing.T, help string, toolCount int) (record string) {
	dir := t.TempDir()
	record = filepath.Join(dir, "record")
	os.WriteFile(filepath.Join(dir, "help"), []byte(help), 0o600)
	script := `#!/bin/sh
if [ "$1" = "--help" ]; then cat ` + filepath.Join(dir, "help") + `; exit 0; fi
{ echo "ARGS: $*" | tr '\n' ' '; echo; echo "SERVER=$FORGELINE_TOKEN"; echo "GH=$GITHUB_TOKEN"; echo "PROXY=$HTTPS_PROXY"; echo "CA=$NODE_EXTRA_CA_CERTS"; echo "PWD=$PWD"; echo "STDIN:"; cat; } > ` + record + `
usage=""
prev=""
for a in "$@"; do [ "$prev" = "--usage-output-file" ] && usage="$a"; prev="$a"; done
[ -n "$usage" ] && echo '{"tokenDetails":{"input":{"tokenCount":40},"cache_write":{"tokenCount":2},"output":{"tokenCount":7}}}' > "$usage"
echo '{"type":"session.usage_checkpoint","data":{"promptCacheBreakState":[{"models":{"m":{"tool_count":` + strconv.Itoa(toolCount) + `}}}]}}'
echo '{"type":"assistant.message","data":{"content":"Copilot says fine.","toolRequests":[]}}'
echo '{"type":"result","exitCode":0,"usage":{"premiumRequests":1}}'
`
	if err := os.WriteFile(filepath.Join(dir, "copilot"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+":"+os.Getenv("PATH"))
	return record
}

func TestAssistantCopilotBackend(t *testing.T) {
	t.Setenv("FORGELINE_TOKEN", "must-not-leak-0123456789")
	t.Setenv("GITHUB_TOKEN", "ghp-must-not-replace-the-login")
	t.Setenv("HTTPS_PROXY", "http://proxy.corp:8080")
	t.Setenv("NODE_EXTRA_CA_CERTS", "/etc/corp-ca.pem")
	h := newConsoleHarness(t)
	admin := h.adminBrowser("ada")
	admin.expect(200, "PUT", "/v1/integrations/assistant", map[string]any{"backend": "copilot"})

	for _, tc := range []struct {
		name      string
		help      string
		stdin     bool
		telemetry bool
	}{{"a CLI that reads a piped prompt", modernCopilotHelp, true, true}, {"an older CLI (1.0.34)", oldCopilotHelp, false, false}} {
		record := fakeCopilot(t, tc.help, 0)
		var reply ChatReply
		json.Unmarshal(admin.expect(200, "POST", "/v1/assistant/chat", map[string]any{"message": "Summarise the failed run."}), &reply)
		if reply.Reply != "Copilot says fine." {
			t.Fatalf("%s: reply = %q", tc.name, reply.Reply)
		}
		raw, _ := os.ReadFile(record)
		got := string(raw)
		args := got[:strings.Index(got, "\nSERVER=")]
		stdin := got[strings.Index(got, "STDIN:"):]
		for _, want := range []string{"--available-tools " + noCopilotTool, "--disable-builtin-mcps", "--no-custom-instructions", "--no-auto-update", "--no-ask-user", "--output-format json"} {
			if !strings.Contains(args, want) {
				t.Errorf("%s: copilot was started without %s: %s", tc.name, want, args)
			}
		}
		for _, bad := range []string{"--allow-all", "--yolo", "--model", "--excluded-tools"} {
			if strings.Contains(args, bad) {
				t.Errorf("%s: copilot args contain %q: %s", tc.name, bad, args)
			}
		}
		if tc.stdin != strings.Contains(stdin, "Summarise the failed run.") || tc.stdin == strings.Contains(args, "--prompt") || tc.stdin == strings.Contains(args, "Summarise") {
			t.Errorf("%s: the prompt should go %s:\nargs %s\n%s", tc.name, map[bool]string{true: "on stdin", false: "in --prompt"}[tc.stdin], args, stdin)
		}
		if tc.telemetry != strings.Contains(args, "--disable-client-telemetry") || tc.telemetry != strings.Contains(args, "--usage-output-file") {
			t.Errorf("%s: flags the CLI does not list must not be sent: %s", tc.name, args)
		}
		if strings.Contains(got, "must-not-leak") || strings.Contains(got, "ghp-must-not") {
			t.Errorf("%s: copilot saw the server's environment:\n%s", tc.name, got)
		}
		if !strings.Contains(got, "PROXY=http://proxy.corp:8080") || !strings.Contains(got, "CA=/etc/corp-ca.pem") {
			t.Errorf("%s: proxy and CA settings must reach copilot:\n%s", tc.name, got)
		}
		if !strings.Contains(got, "PWD="+os.TempDir()) || !strings.Contains(got, "forgeline-copilot-") {
			t.Errorf("%s: copilot did not run in a scratch directory:\n%s", tc.name, got)
		}
		if !strings.Contains(got, "<instructions>") {
			t.Errorf("%s: the instructions lead the prompt:\n%s", tc.name, got)
		}
	}

	// A model name is passed on; a Claude alias left in the settings is not.
	record := fakeCopilot(t, modernCopilotHelp, 0)
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

	// A CLI that offered the model tools anyway: its reply is refused.
	fakeCopilot(t, modernCopilotHelp, 25)
	resp, body := admin.do("POST", "/v1/assistant/chat", map[string]any{"message": "hi"})
	if resp.StatusCode == 200 || !strings.Contains(string(body), "offered the model 25 tools") {
		t.Errorf("a reply from a model with tools was accepted: %d %s", resp.StatusCode, body)
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
	// askCopilot refuses a reply if the CLI reports any tool offered to the model, so a pass here also checks that
	// --available-tools with a name that does not exist leaves none.
	reply, use, err := askCopilot(context.Background(), "", "Answer in one word.", []chatTurn{{Role: "user", Content: "What colour is a clear daytime sky?"}})
	if err != nil || !strings.Contains(strings.ToLower(reply), "blue") {
		t.Fatalf("reply %q, err %v", reply, err)
	}
	t.Logf("reply %q, tokens in %d out %d", reply, use.Input, use.Output)
}
