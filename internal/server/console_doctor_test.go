package server

import (
	"context"
	"encoding/json"
	"github.com/forgelinehq/forgeline/internal/authn"
	"strings"
	"testing"
)

func (m *mcpHarness) confirm(c *browser, id string, code int) AssistAction {
	var a AssistAction
	json.Unmarshal(c.expect(code, "POST", "/v1/assistant/actions/"+id+"/confirm", nil), &a)
	return a
}

func TestTheDoctorSaysWhatIsHeldBackAndTheCardsTurnItOn(t *testing.T) {
	m := newMCPHarness(t)
	m.admin.expect(200, "PUT", "/v1/integrations/aws", map[string]any{"enabled": true, "profile": "dev", "region": "ap-south-1"})
	m.admin.expect(200, "PUT", "/v1/assistant/autopilot", map[string]any{"allowed": false})
	r := results(m.run(m.admin, map[string]any{"message": "why can't you do things?"}, lookupReply(doctorTool, map[string]string{})))
	for _, want := range []string{"the sandbox is off", "turn_on_sandbox", "primary AWS connection is protected", "allow_aws_changes", "autopilot is turned off", "no model provider is connected"} {
		if !strings.Contains(r[0], want) {
			t.Errorf("the doctor does not say %q:\n%s", want, r[0])
		}
	}
	m.admin.expect(200, "PUT", "/v1/assistant/autopilot", map[string]any{"allowed": true})

	// A developer is told to ask an administrator; an administrator gets the cards.
	dev := results(m.run(m.dev, map[string]any{"message": "turn it on"}, lookupReply("prepare_action", map[string]string{"action": "turn_on_sandbox"})))
	if !strings.Contains(dev[0], "only an administrator") {
		t.Errorf("a developer got a capability card: %s", dev[0])
	}
	cards := actionsIn(m.run(m.admin, map[string]any{"message": "turn on the shell and allow changes"}, lookupReply("prepare_action", map[string]string{"action": "turn_on_sandbox"}), lookupReply("prepare_action", map[string]string{"action": "allow_aws_changes", "account": "primary"})))
	if len(cards) != 2 {
		t.Fatalf("%d cards", len(cards))
	}
	if m.srv.loadSandbox(t.Context()).Enabled {
		t.Fatal("the sandbox was turned on before the person confirmed")
	}
	if d, _ := m.srv.loadPermissions(t.Context()); d.OwnApproval["primary"] {
		t.Fatal("changes were allowed before the person confirmed")
	}
	m.dev.expect(404, "POST", "/v1/assistant/actions/"+cards[0]["id"].(string)+"/confirm", nil) // not theirs
	for _, c := range cards {
		if a := m.confirm(m.admin, c["id"].(string), 200); a.Status != actDone {
			t.Errorf("%s: %s %s", a.Kind, a.Status, a.Result)
		}
	}
	if !m.srv.loadSandbox(t.Context()).Enabled {
		t.Error("confirming did not turn the sandbox on")
	}
	if d, _ := m.srv.loadPermissions(t.Context()); !d.OwnApproval["primary"] {
		t.Error("confirming did not allow changes")
	}
	// Both now read as fine, and asking again is refused.
	r = results(m.run(m.admin, map[string]any{"message": "check again"}, lookupReply(doctorTool, map[string]string{})))
	if !strings.Contains(r[0], "OK: the sandbox") || !strings.Contains(r[0], "OK: the primary AWS connection allows") {
		t.Errorf("after confirming:\n%s", r[0])
	}
	again := results(m.run(m.admin, map[string]any{"message": "again"}, lookupReply("prepare_action", map[string]string{"action": "turn_on_sandbox"}), lookupReply("prepare_action", map[string]string{"action": "allow_aws_changes", "account": "primary"}), lookupReply("prepare_action", map[string]string{"action": "allow_aws_changes", "account": "nope"})))
	if !strings.Contains(again[0], "already on") || !strings.Contains(again[1], "already allowed") || !strings.Contains(again[2], "no AWS account") {
		t.Errorf("repeat asks: %v", again)
	}
	aud := string(m.admin.expect(200, "GET", "/v1/audit?limit=60", nil))
	if !strings.Contains(aud, "assistant.action.confirm") || !strings.Contains(aud, "turn_on_sandbox") {
		t.Error("the audit log does not record it")
	}
}

func TestTheDoctorExplainsACommandLineBackend(t *testing.T) {
	m := newMCPHarness(t)
	m.admin.expect(200, "PUT", "/v1/integrations/assistant", map[string]any{"backend": "cli", "model": "claude-sonnet-5-5"})
	ctx := context.WithValue(t.Context(), principalKey{}, Principal{Name: "ada", UserID: "ada", Role: authn.Admin})
	out, err := m.srv.lookupDoctor(ctx, nil)
	if err != nil || !strings.Contains(out, "HELD BACK: the assistant runs through the cli backend (a command-line program with its tools turned off)") || !strings.Contains(out, "native tool calling") {
		t.Errorf("%v\n%s", err, out)
	}
}

func TestTheAssistantIsToldToUseTheDoctorAndKeepLessons(t *testing.T) {
	m := newMCPHarness(t)
	m.run(m.dev, map[string]any{"message": "hello"})
	sys := m.lastSystem()
	for _, want := range []string{"call doctor", "lessons.md", "- doctor "} {
		if !strings.Contains(sys, want) {
			t.Errorf("the prompt lacks %q", want)
		}
	}
}
