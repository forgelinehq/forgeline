package server

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/forgelinehq/forgeline/internal/authn"
)

func TestWhatForThisChatWouldAllow(t *testing.T) {
	for cmd, want := range map[string]string{
		"npm test":                    "npm test *",
		"git log --oneline -3":        "git log *",
		"go test ./...":               "go test *",
		"python3 build.py":            "python3 build.py", // an interpreter: only exactly this
		"git -C /tmp status":          "git -C /tmp status",
		"ls":                          "ls",
		"npm run build":               "npm run build",
		"go run ./x":                  "go run ./x",
		"git log && rm -rf /work":     "git log && rm -rf /work",
		"git log > /work/out":         "git log > /work/out",
		"git log $(id)":               "git log $(id)",
		"A=1 make all":                "A=1 make all",
		"/usr/bin/git status":         "/usr/bin/git status",
		"docker ps":                   "docker ps",
		"curl example.com":            "curl example.com",
		"make build/linux":            "make build/linux",
		"cargo exec something":        "cargo exec something",
		"git log\nrm -rf /work/other": "",
	} {
		_, label := chatRule("Bash", map[string]any{"command": cmd})
		if cmd == "git log\nrm -rf /work/other" {
			want = cmd // two lines can only be allowed exactly
		}
		if label != want {
			t.Errorf("%q: %q, want %q", cmd, label, want)
		}
	}
	for name, tc := range map[string]struct {
		tool string
		in   map[string]any
	}{
		"hidden character": {"Bash", map[string]any{"command": "ls ‮ x"}},
		"too long":         {"Bash", map[string]any{"command": strings.Repeat("x", maxChatRuleText+1)}},
		"background":       {"Bash", map[string]any{"command": "sleep 1", "run_in_background": true}},
		"a hidden file":    {"Write", map[string]any{"file_path": "/work/.bashrc", "content": "x"}},
		"in a git folder":  {"Edit", map[string]any{"file_path": "/work/a/.git/config", "old_string": "a", "new_string": "b"}},
		"outside /work":    {"Write", map[string]any{"file_path": "/etc/hosts", "content": "x"}},
		"a hidden name":    {"Write", map[string]any{"file_path": "/work/a​b", "content": "x"}},
		"a web fetch":      {"WebFetch", map[string]any{"url": "https://example.com"}},
	} {
		if rule, _ := chatRule(tc.tool, tc.in); rule != "" {
			t.Errorf("%s may be allowed for the chat: %q", name, rule)
		}
	}

	c := &chatAllowances{}
	bash := func(s string) map[string]any { return map[string]any{"command": s} }
	rule, _ := chatRule("Bash", bash("npm test -- --watch=false"))
	c.add("k", rule)
	c.add("k", ruleEdits)
	for _, tc := range []struct {
		key, tool string
		in        map[string]any
		want      bool
	}{
		{"k", "Bash", bash("npm test"), true},
		{"k", "Bash", bash("npm test -v"), true},
		{"k", "Bash", bash("npm install x"), false},
		{"k", "Bash", bash("npm test; id"), false},
		{"k", "Bash", map[string]any{"command": "npm test", "run_in_background": true}, false},
		{"other", "Bash", bash("npm test"), false},
		{"", "Bash", bash("npm test"), false},
		{"k", "Write", map[string]any{"file_path": "/work/x", "content": "y"}, true},
		{"k", "Edit", map[string]any{"file_path": "/work/d/x", "old_string": "a", "new_string": "b"}, true},
		{"k", "Write", map[string]any{"file_path": "/work/.agent/x", "content": "y"}, false},
		{"k", "Write", map[string]any{"file_path": "/work/d/.env", "content": "y"}, false},
	} {
		if got := c.covers(tc.key, tc.tool, tc.in); got != tc.want {
			t.Errorf("covers(%q, %s, %v) = %v", tc.key, tc.tool, tc.in, got)
		}
	}
	// Without a person and a conversation nothing is covered, and a command allowed as written is not allowed in the background.
	c.add("", ruleEdits)
	if c.covers("", "Write", map[string]any{"file_path": "/work/x", "content": "y"}) {
		t.Error("an allowance with no chat covers a call")
	}
	c.add("k", "exact:python3 a.py")
	if !c.covers("k", "Bash", bash("python3 a.py")) || c.covers("k", "Bash", map[string]any{"command": "python3 a.py", "run_in_background": true}) {
		t.Error("the exact command")
	}
	for i := range maxChatRules + 5 {
		c.add("full", "exact:"+strings.Repeat("x", i+1))
	}
	if len(c.rules["full"]) != maxChatRules {
		t.Errorf("%d rules kept", len(c.rules["full"]))
	}
	for cmd, want := range map[string]bool{"ls -la": true, "cat a": true, "grep -r x .": true, "ls; id": false, "cat a > b": false, "rm a": false, "find . -delete": false, "env": false, "": false, "date -s x": false} {
		if readOnlyCommand(cmd) != want {
			t.Errorf("read only %q = %v", cmd, !want)
		}
	}
}

// answerCard answers the one card that is waiting.
func (m *mcpHarness) answerCard(t *testing.T, c *browser, body map[string]any) {
	t.Helper()
	id := m.waitPending(t)
	time.Sleep(20 * time.Millisecond)
	c.expect(200, "POST", "/v1/assistant/approvals/"+id, body)
}

// run_shell asks like the agent's own shell: once, for this chat, or no with a note; a command that only looks just runs.
func TestRunShellAsksAsTheAgentsOwnShellDoes(t *testing.T) {
	m, d := sandboxHarness(t)
	m.admin.expect(200, "PUT", "/v1/assistant/sandbox", map[string]any{"enabled": true})
	chat := func(conv, cmd string, answers ...map[string]any) (cards []map[string]any) {
		st := m.streamWith(m.dev, map[string]any{"conversationId": conv}, shell(map[string]any{"command": cmd}), "Done.")
		for _, a := range answers {
			m.answerCard(t, m.dev, a)
		}
		events, _ := answerOf(t, st)
		return eventsNamed(events, "forgeline.permission")
	}
	ran := func() int { return len(d.find("exec")) }
	yes, no, always := map[string]any{"decision": "approve"}, map[string]any{"decision": "deny"}, map[string]any{"decision": "always"}
	one, two := "0123456789abcdef0123456789abcde1", "0123456789abcdef0123456789abcde2"

	if cards := chat(one, "ls /work"); len(cards) != 0 || ran() != 1 {
		t.Fatalf("a command that only looks: %v, ran %d", cards, ran())
	}
	cards := chat(one, "npm test", no)
	if len(cards) != 1 || cards[0]["tool"] != shellCardTool || cards[0]["command"] != "npm test" || cards[0]["pattern"] != "npm test *" || cards[0]["scope"] != scopeChat || cards[0]["note"] != true || ran() != 1 {
		t.Fatalf("refused: %v, ran %d", cards, ran())
	}
	if res := m.llm.lastUser(1); !strings.Contains(res, "said no") {
		t.Errorf("the model was told: %s", res)
	}
	chat(one, "npm test", map[string]any{"decision": "deny", "feedback": "  use yarn instead  "})
	if res := m.llm.lastUser(1); !strings.Contains(res, "They wrote: use yarn instead") || ran() != 1 {
		t.Errorf("the note did not reach the model: %s", res)
	}
	if cards := chat(one, "npm test", yes); len(cards) != 1 || ran() != 2 {
		t.Fatalf("once: %v, ran %d", cards, ran())
	}
	if cards := chat(one, "npm test", always); len(cards) != 1 || ran() != 3 {
		t.Fatalf("for this chat: %v, ran %d", cards, ran())
	}
	if cards := chat(one, "npm test -v"); len(cards) != 0 || ran() != 4 {
		t.Errorf("the same kind in the same chat still asks: %v, ran %d", cards, ran())
	}
	if cards := chat(two, "npm test", no); len(cards) != 1 || ran() != 4 {
		t.Errorf("another chat: %v", cards)
	}
	// The whole command is on the card: a long one is not cut where its end would run unseen.
	longCmd := "make " + strings.Repeat("a", 2000) + " && rm -rf /work/out"
	if cards := chat(two, longCmd, no); len(cards) != 1 || cards[0]["command"] != longCmd {
		t.Error("the card does not show the whole command")
	}
	// The note belongs to a no, and has a limit.
	st := m.streamWith(m.dev, map[string]any{"conversationId": two}, shell(map[string]any{"command": "make all", "image": "node:22"}), "Done.")
	id := m.waitPending(t)
	m.dev.expect(400, "POST", "/v1/assistant/approvals/"+id, map[string]any{"decision": "approve", "feedback": "x"})
	m.dev.expect(400, "POST", "/v1/assistant/approvals/"+id, map[string]any{"decision": "deny", "feedback": strings.Repeat("x", maxAnswerLen+1)})
	m.dev.expect(200, "POST", "/v1/assistant/approvals/"+id, no)
	events, _ := answerOf(t, st)
	if cards := eventsNamed(events, "forgeline.permission"); cards[0]["command"] != "[in node:22] make all" {
		t.Errorf("the image is not on the card: %v", cards)
	}
	// Autopilot runs it without a card.
	st = m.streamWith(m.dev, map[string]any{"conversationId": two, "autopilot": map[string]any{"minutes": 5, "steps": 5}}, shell(map[string]any{"command": "make all"}), "Done.")
	if events, _ := answerOf(t, st); len(eventsNamed(events, "forgeline.permission")) != 0 || ran() != 5 {
		t.Errorf("autopilot: ran %d", ran())
	}
	// With nobody to ask, only what looks runs. The agent's own container is not one run_shell may use.
	m.ask(m.dev, shell(map[string]any{"command": "npm test"}))
	if res := m.llm.lastUser(1); !strings.Contains(res, "nobody is here") || ran() != 5 {
		t.Errorf("nobody present: %s", res)
	}
	m.ask(m.dev, shell(map[string]any{"command": "ls", "image": engineImage}))
	if res := m.llm.lastUser(1); !strings.Contains(res, "agent's own") || ran() != 5 {
		t.Errorf("the agent's container: %s", res)
	}
	aud := string(m.admin.expect(200, "GET", "/v1/audit?action=assistant.sandbox.command", nil))
	for _, want := range []string{"refused: npm test", "approved once: npm test", "approved, and allowed for this chat: npm test", "allowed for this chat: npm test -v"} {
		if !strings.Contains(aud, want) {
			t.Errorf("the audit log lacks %q", want)
		}
	}
}

// The engine's cards offer the same choices, show what a file will hold, and never offer them on the server itself.
func TestTheEnginesCardsShowTheFileAndCanAllowTheChat(t *testing.T) {
	m, _ := planHarness(t)
	p := Principal{UserID: "u1", Name: "uma", Role: authn.Developer}
	var asked []permRequest
	answer := decisionAlways
	ctx := context.WithValue(context.WithValue(t.Context(), autopilotConvKey{}, "c1"), permAskerKey{}, permAsker(func(r permRequest) (string, error) {
		asked = append(asked, r)
		return answer, nil
	}))
	call := func(c context.Context, tool string, in map[string]any) bool {
		raw, _ := json.Marshal(map[string]any{"tool_name": tool, "input": in})
		return strings.Contains(m.srv.enginePermission(&engineRun{ctx: c, p: p}, raw), `"allow"`)
	}
	write := map[string]any{"file_path": "/work/a.py", "content": "print(1)\n"}
	if !call(ctx, "Write", write) || len(asked) != 1 || asked[0].Pattern != "file changes under /work" || asked[0].Scope != scopeChat || !asked[0].Note ||
		!slices.Equal(asked[0].Actions, []planAction{{Tool: "Write", Path: "/work/a.py", Content: "print(1)\n"}}) {
		t.Fatalf("the write card: %+v", asked)
	}
	if !call(ctx, "Edit", map[string]any{"file_path": "/work/a.py", "old_string": "1", "new_string": "2"}) || len(asked) != 1 {
		t.Errorf("a later edit in the same chat asked: %d", len(asked))
	}
	// A hidden file asks, and "always" for it is not accepted.
	if call(ctx, "Write", map[string]any{"file_path": "/work/.bashrc", "content": "x"}) || len(asked) != 2 || asked[1].Pattern != "" {
		t.Errorf("a shell profile: %+v", asked)
	}
	long := strings.Repeat("y", maxShownText+10)
	answer = decisionOnce
	call(ctx, "Write", map[string]any{"file_path": "/work/.x", "content": long})
	if got := asked[2].Actions[0].Content; !strings.Contains(got, "cut here") || len(got) > maxShownText+100 {
		t.Errorf("a very long file: %d characters", len(got))
	}
	// On the server itself nothing is remembered or offered.
	m.admin.expect(200, "PUT", "/v1/assistant/sandbox", map[string]any{"enabled": false})
	answer = decisionAlways
	n := len(asked)
	if call(ctx, "Write", write) || len(asked) != n+1 || asked[n].Pattern != "" {
		t.Errorf("on the server: %+v", asked[n:])
	}
}
