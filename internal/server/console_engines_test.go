package server

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/forgelinehq/forgeline/internal/authn"
)

// installingMakesItAppear makes the Docker stand-in behave like a sandbox where the install works: after the install command
// has run as root, asking for the program answers with a version.
func installingMakesItAppear(d *dockerStub, install, version string) {
	d.onExec = func(args []string) ([]byte, string, error, bool) {
		if len(args) > 3 && args[1] == "-u" && args[2] == "root" && args[len(args)-1] == install {
			d.probe = version // the stub's lock is already held: it is calling this
			return nil, "", nil, true
		}
		return nil, "", nil, false
	}
}

func execCalls(d *dockerStub, contains string) int {
	n := 0
	for _, c := range d.find("exec") {
		if strings.Contains(strings.Join(c, " "), contains) {
			n++
		}
	}
	return n
}

// An agent that is not installed in the sandbox is found when it is chosen or used, offered for installation as a card an
// administrator confirms, and its token (from the vault) reaches only that agent.
func TestAMissingAgentIsOfferedForInstallAndThenRuns(t *testing.T) {
	m, d, started, short := acpHarness(t)
	if err := os.WriteFile(filepath.Join(short, "copilot-mode"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	m.admin.expect(200, "PUT", "/v1/integrations/assistant", map[string]any{"backend": "acp-agent", "harness": "copilot"})
	d.probe = engineMissing

	// An administrator is told, and gets the card; nothing is started.
	evs := m.run(m.admin, map[string]any{"message": "hello"})
	text, _ := textOf(evs)
	cards := actionsIn(evs)
	if !strings.Contains(text, "GitHub Copilot CLI is not installed") || strings.Contains(text, "command not found") || !strings.Contains(text, "COPILOT_GITHUB_TOKEN") || !strings.Contains(text, "copilot login --device-code") {
		t.Errorf("the reply: %q", text)
	}
	if len(cards) != 1 || cards[0]["kind"] != "install_engine" {
		t.Fatalf("cards = %v", cards)
	}
	fx, _ := json.Marshal(cards[0]["effects"])
	if !strings.Contains(string(fx), "npm install -g @github/copilot@latest") || !strings.Contains(string(fx), "as root inside the container") || !strings.Contains(string(fx), "npm (@github/copilot)") {
		t.Errorf("the card does not say what it does: %s", fx)
	}
	if len(*started) != 0 {
		t.Errorf("the agent was started although it is not installed: %v", *started)
	}
	if n := execCalls(d, "npm install"); n != 0 {
		t.Error("something was installed before the person confirmed")
	}

	// Anyone else is told who to ask, and gets no card.
	evs = m.run(m.dev, map[string]any{"message": "hello"})
	if text, _ := textOf(evs); !strings.Contains(text, "An administrator can install") || len(actionsIn(evs)) != 0 {
		t.Errorf("a developer: %q, %v", text, actionsIn(evs))
	}

	// Confirming installs it, as root, and says what is left to do.
	installingMakesItAppear(d, "npm install -g @github/copilot@latest", "GitHub Copilot CLI 1.0.114.")
	id := cards[0]["id"].(string)
	m.dev.expect(404, "POST", "/v1/assistant/actions/"+id+"/confirm", nil)
	var a AssistAction
	json.Unmarshal(m.admin.expect(200, "POST", "/v1/assistant/actions/"+id+"/confirm", nil), &a)
	if a.Status != actDone || !strings.Contains(a.Result, "Installed GitHub Copilot CLI (GitHub Copilot CLI 1.0.114.)") || !strings.Contains(a.Result, "COPILOT_GITHUB_TOKEN") {
		t.Fatalf("action = %+v", a)
	}
	if execCalls(d, "-u root -e HOME=/work/.agent") != 1 || execCalls(d, "npm install -g @github/copilot@latest") != 1 {
		t.Errorf("the install was not run once as root in the sandbox: %v", d.find("exec"))
	}
	if aud := string(m.admin.expect(200, "GET", "/v1/audit?limit=40", nil)); !strings.Contains(aud, "assistant.engine.install") || !strings.Contains(aud, "installed GitHub Copilot CLI") {
		t.Error("the install is not in the audit log")
	}

	// With a token in the vault it reaches the chosen agent, in the environment and not on the command line, and another
	// agent's key does not.
	const token = "github_pat_11TESTTOKEN0123456789abcdefghij"
	if _, _, err := m.srv.putSecret(t.Context(), "ada", "COPILOT_GITHUB_TOKEN", token, "t"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := m.srv.putSecret(t.Context(), "ada", geminiKeyName, "AIzaSyTestKey0123456789abcdefghij", "t"); err != nil {
		t.Fatal(err)
	}
	st := m.streamWith(m.admin, map[string]any{"message": "which pipelines exist?", "conversationId": apConv}, "unused")
	pid := m.waitPending(t)
	time.Sleep(30 * time.Millisecond)
	m.admin.expect(200, "POST", "/v1/assistant/approvals/"+pid, map[string]any{"decision": "approve"})
	answerOf(t, st)
	line := strings.Join(*started, " ")
	if !strings.Contains(line, "-e COPILOT_GITHUB_TOKEN ") || strings.Contains(line, token) || strings.Contains(line, geminiKeyName) {
		t.Errorf("the token is not passed by name only, or another agent's key is:\n%s", line)
	}
	env := strings.Join(acpLastCmd.Env, "\n")
	if !strings.Contains(env, "COPILOT_GITHUB_TOKEN="+token) || strings.Contains(env, "GEMINI_API_KEY") {
		t.Errorf("the agent's environment: %s", env)
	}
}

func TestTheEnginesApiListsChecksAndInstalls(t *testing.T) {
	// Sandbox off: nothing is checked or installed, and it says why.
	off := newMCPHarness(t)
	var list struct {
		Sandbox bool           `json:"sandbox"`
		Engines []EngineStatus `json:"engines"`
	}
	json.Unmarshal(off.admin.expect(200, "GET", "/v1/assistant/engines", nil), &list)
	if list.Sandbox || len(list.Engines) != 3 || !strings.Contains(list.Engines[0].Error, "sandbox is off") {
		t.Errorf("sandbox off: %+v", list)
	}
	off.dev.expect(403, "GET", "/v1/assistant/engines", nil)
	off.admin.expect(409, "POST", "/v1/assistant/engines/copilot/install", nil)

	m, d, _, _ := acpHarness(t)
	m.dev.expect(403, "GET", "/v1/assistant/engines", nil)
	m.dev.expect(403, "POST", "/v1/assistant/engines/copilot/install", nil)
	m.admin.expect(404, "POST", "/v1/assistant/engines/nope/install", nil)

	d.probe = engineMissing
	if _, _, err := m.srv.putSecret(t.Context(), "ada", "COPILOT_GITHUB_TOKEN", "github_pat_11TESTTOKEN0123456789abcdefghij", "t"); err != nil {
		t.Fatal(err)
	}
	raw := m.admin.expect(200, "GET", "/v1/assistant/engines", nil)
	if strings.Contains(string(raw), "github_pat_") {
		t.Error("the token is in the answer")
	}
	json.Unmarshal(raw, &list)
	byName := map[string]EngineStatus{}
	for _, e := range list.Engines {
		byName[e.Harness] = e
	}
	if !list.Sandbox || len(list.Engines) != 3 {
		t.Fatalf("list: %+v", list)
	}
	for _, h := range []string{"antigravity", "copilot", "codex"} {
		if byName[h].Installed || byName[h].Install == "" || byName[h].Source == "" || byName[h].Checked == "" {
			t.Errorf("%s: %+v", h, byName[h])
		}
	}
	if !byName["copilot"].TokenSet || byName["antigravity"].TokenSet || byName["codex"].TokenSet {
		t.Errorf("token_set: copilot %v, antigravity %v, codex %v", byName["copilot"].TokenSet, byName["antigravity"].TokenSet, byName["codex"].TokenSet)
	}
	if byName["codex"].Install != "npm install -g @agentclientprotocol/codex-acp@latest" || byName["antigravity"].Install != "curl -fsSL https://antigravity.google/cli/install.sh | bash" {
		t.Errorf("install commands: %q, %q", byName["codex"].Install, byName["antigravity"].Install)
	}

	// Installing as an administrator: the version comes back with what to do next.
	installingMakesItAppear(d, "npm install -g @agentclientprotocol/codex-acp@latest", "@agentclientprotocol/codex-acp@2.10.0")
	var res struct {
		Engine EngineStatus `json:"engine"`
		Next   string       `json:"next"`
	}
	json.Unmarshal(m.admin.expect(200, "POST", "/v1/assistant/engines/codex/install", nil), &res)
	if !res.Engine.Installed || res.Engine.Version != "@agentclientprotocol/codex-acp@2.10.0" || !strings.Contains(res.Next, "CODEX_API_KEY") {
		t.Errorf("install: %+v", res)
	}

	// An install that fails says so and is audited as failed; so does one that "worked" but left the program missing.
	d.onExec = func(args []string) ([]byte, string, error, bool) {
		if len(args) > 3 && args[1] == "-u" {
			return nil, "npm ERR! network request failed", os.ErrInvalid, true
		}
		return nil, "", nil, false
	}
	d.probe = engineMissing
	if out := string(m.admin.expect(502, "POST", "/v1/assistant/engines/copilot/install", nil)); !strings.Contains(out, "did not finish") || !strings.Contains(out, "network request failed") {
		t.Errorf("a failed install: %s", out)
	}
	d.onExec = func(args []string) ([]byte, string, error, bool) {
		if len(args) > 3 && args[1] == "-u" {
			return nil, "", nil, true // ran, and installed nothing
		}
		return nil, "", nil, false
	}
	if out := string(m.admin.expect(502, "POST", "/v1/assistant/engines/copilot/install", nil)); !strings.Contains(out, "still not found") {
		t.Errorf("an install that left nothing: %s", out)
	}

	// With the sandbox network off an install is refused before anything runs.
	d.onExec = nil
	before := execCalls(d, "-u root")
	m.admin.expect(200, "PUT", "/v1/assistant/sandbox", map[string]any{"enabled": true, "network": "none"})
	if out := string(m.admin.expect(409, "POST", "/v1/assistant/engines/copilot/install", nil)); !strings.Contains(out, "needs the network") || execCalls(d, "-u root") != before {
		t.Errorf("network none: %s", out)
	}
}

// Installing software is a capability card: it always waits for the person, autopilot included, and only an administrator
// can have one prepared.
func TestAnInstallCardAlwaysWaitsAndIsForAdministrators(t *testing.T) {
	m := newMCPHarness(t)
	d := &dockerStub{runtimes: `{"runc":{"path":"runc"}}`, probe: engineMissing}
	m.srv.run = d.run
	m.admin.expect(200, "PUT", "/v1/assistant/sandbox", map[string]any{"enabled": true})
	card := lookupReply("prepare_action", map[string]any{"action": "install_engine", "harness": "codex"})
	if cards := actionsIn(m.run(m.dev, map[string]any{"message": "install codex"}, card)); len(cards) != 0 {
		t.Errorf("a developer got an install card: %v", cards)
	}
	if cards := actionsIn(m.run(m.admin, map[string]any{"message": "install nonsense"}, lookupReply("prepare_action", map[string]any{"action": "install_engine", "harness": "emacs"}))); len(cards) != 0 {
		t.Errorf("a card for an unknown agent: %v", cards)
	}
	evs := m.run(m.admin, map[string]any{"message": "install codex", "autopilot": map[string]any{"minutes": 5, "steps": 20}}, card)
	cards := actionsIn(evs)
	if len(cards) != 1 || cards[0]["done"] != false {
		t.Fatalf("under autopilot the install card should wait: %v", cards)
	}
	if n := execCalls(d, "npm install"); n != 0 {
		t.Error("autopilot installed software without the person")
	}
}

// The doctor describes an ACP agent as what it is (it was described as a text-protocol command-line backend), and the Claude
// Code engine on the server says what to do when `claude` is missing, without installing anything on the server itself.
func TestTheDoctorAndClaudeSayWhatToDoForAnAgentThatIsNotThere(t *testing.T) {
	m := newMCPHarness(t)
	m.admin.expect(200, "PUT", "/v1/integrations/assistant", map[string]any{"backend": "acp-agent", "harness": "copilot"})
	out, err := m.srv.lookupDoctor(t.Context(), nil)
	if err != nil || !strings.Contains(out, "GitHub Copilot CLI runs as the agent over ACP and needs the sandbox") || strings.Contains(out, "text") {
		t.Errorf("sandbox off: %v %s", err, out)
	}
	m.admin.expect(200, "PUT", "/v1/assistant/sandbox", map[string]any{"enabled": true})
	if out, _ := m.srv.lookupDoctor(t.Context(), nil); !strings.Contains(out, "runs as the agent over ACP, inside your sandbox") || !strings.Contains(out, "offers to install the latest version") {
		t.Errorf("sandbox on: %s", out)
	}
	// With the sandbox off the Claude Code engine needs `claude` on the server: Forgeline says so and installs nothing there.
	m.admin.expect(200, "PUT", "/v1/assistant/sandbox", map[string]any{"enabled": false})
	t.Setenv("PATH", t.TempDir())
	m.srv.engineExec = nil
	_, err = m.srv.askEngine(t.Context(), Principal{Name: "ada", UserID: "ada", Role: authn.Admin}, "c1", "hi", "", nil, nil)
	if err == nil || !strings.Contains(err.Error(), "npm install -g @anthropic-ai/claude-code") || !strings.Contains(err.Error(), "does not install software on the server") || !strings.Contains(err.Error(), engineClaudeVer) {
		t.Errorf("claude missing: %v", err)
	}
}
