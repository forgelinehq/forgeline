package server

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

const apConv = "0123456789abcdef0123456789abcdef"

// streamWith runs a chat turn with the given extra request fields and the scripted replies.
func (m *mcpHarness) streamWith(c *browser, extra map[string]any, replies ...string) *streamed {
	m.llm.mu.Lock()
	m.llm.replies, m.llm.reqs = replies, nil
	m.llm.mu.Unlock()
	body := map[string]any{"message": "look at Dev01", "conversationId": apConv}
	for k, v := range extra {
		body[k] = v
	}
	st := &streamed{done: make(chan struct{})}
	go func() {
		defer close(st.done)
		resp, b := c.do("POST", "/v1/assistant/runs", body)
		st.code, st.body = resp.StatusCode, b
	}()
	return st
}

func TestAutopilotRunsAllowedChangesWithoutACardAndNothingElse(t *testing.T) {
	m, st := runbookHarness(t)
	change := lookupReply("call_aws", map[string]any{"cli_command": "aws ecs update-service --cluster web --service api --desired-count 3"})
	on := map[string]any{"autopilot": map[string]any{"minutes": 5, "steps": 20}}
	m.admin.expect(200, "PUT", "/v1/assistant/permissions/accounts/primary", map[string]any{"own_approval": true})

	// Without autopilot the change asks; with it, it runs, the person is shown, and the audit says why.
	s0 := m.streamWith(m.admin, nil, change, "Done.")
	id := m.waitPending(t)
	m.admin.expect(200, "POST", "/v1/assistant/approvals/"+id, map[string]any{"decision": "deny"})
	answerOf(t, s0)
	s1 := m.streamWith(m.admin, on, change, "Done.")
	events, res := answerOf(t, s1)
	if st.count("ecs update-service") != 1 || res == nil || strings.Contains(res["content"].(string), "lookup failed") {
		t.Fatalf("autopilot did not run the change: %d runs, %v", st.count("ecs update-service"), res)
	}
	if len(eventsNamed(events, "forgeline.permission")) != 0 {
		t.Error("a card was shown under autopilot")
	}
	var acted bool
	for _, e := range eventsNamed(events, "forgeline.autopilot") {
		if a, _ := e["action"].(string); strings.Contains(a, "ecs update-service") {
			acted = true
		}
	}
	if !acted {
		t.Error("the person was not shown the change that ran")
	}
	if aud := string(m.admin.expect(200, "GET", "/v1/audit?limit=60", nil)); !strings.Contains(aud, "autopilot, no card") || !strings.Contains(aud, "assistant.autopilot.start") {
		t.Errorf("the audit log does not say autopilot ran it: %s", aud)
	}

	// The next message without the switch is back to asking: it does not carry on by itself.
	s2 := m.streamWith(m.admin, nil, change, "Done.")
	id = m.waitPending(t)
	m.admin.expect(200, "POST", "/v1/assistant/approvals/"+id, map[string]any{"decision": "deny"})
	answerOf(t, s2)
	if st.count("ecs update-service") != 1 {
		t.Errorf("autopilot carried over to a message without the switch: %d runs", st.count("ecs update-service"))
	}

	// What it never covers: a service that always asks, an organisation "ask" rule, a deny, and a protected account.
	iam := lookupReply("call_aws", map[string]any{"cli_command": "aws iam create-user --user-name x"})
	s3 := m.streamWith(m.admin, on, iam, "Done.")
	id = m.waitPending(t)
	m.admin.expect(200, "POST", "/v1/assistant/approvals/"+id, map[string]any{"decision": "deny"})
	answerOf(t, s3)
	m.admin2.expect(201, "POST", "/v1/assistant/permissions/rules", map[string]any{"scope": "org", "effect": "ask", "pattern": "aws ecs update-service *"})
	s4 := m.streamWith(m.admin, on, change, "Done.")
	id = m.waitPending(t)
	m.admin.expect(200, "POST", "/v1/assistant/approvals/"+id, map[string]any{"decision": "deny"})
	answerOf(t, s4)
	m.admin2.expect(201, "POST", "/v1/assistant/permissions/rules", map[string]any{"scope": "org", "effect": "deny", "pattern": "aws ecs update-service *"})
	_, res = answerOf(t, m.streamWith(m.admin, on, change, "Done."))
	if res == nil || !strings.Contains(res["content"].(string), "a org rule denies") {
		t.Errorf("a deny did not hold under autopilot: %v", res)
	}
	m.admin.expect(200, "PUT", "/v1/assistant/permissions/accounts/primary", map[string]any{"own_approval": false})
	_, res = answerOf(t, m.streamWith(m.admin, on, lookupReply("call_aws", map[string]any{"cli_command": "aws s3api create-bucket --bucket b"}), "Done."))
	if res == nil || !strings.Contains(res["content"].(string), "this account is protected") {
		t.Errorf("a protected account was changed under autopilot: %v", res)
	}
	if st.count("ecs update-service") != 1 || st.count("iam create-user") != 0 || st.count("create-bucket") != 0 {
		t.Errorf("something ran that must not: ecs %d, iam %d, s3 %d", st.count("ecs update-service"), st.count("iam create-user"), st.count("create-bucket"))
	}
	// A developer's autopilot changes nothing about what a developer may do.
	m.dev.expect(200, "GET", "/v1/assistant/autopilot", nil)
	_, res = answerOf(t, m.streamWith(m.dev, on, change, "Done."))
	if res == nil || !strings.Contains(res["content"].(string), "needs an administrator") {
		t.Errorf("a developer changed AWS under autopilot: %v", res)
	}
}

func TestAutopilotHasAnEnvelopeAStopAndAnAdministratorSwitch(t *testing.T) {
	m := newMCPHarness(t)
	look := lookupReply("get_overview", map[string]any{})

	// Steps: the default budget is 40; inside an envelope it is the person's number, and it ends when used.
	var many []string
	for i := 0; i < 50; i++ {
		many = append(many, look)
	}
	many = append(many, "Finished.")
	_, res := answerOf(t, m.streamWith(m.dev, nil, many...))
	if res == nil {
		t.Fatal("no result")
	}
	m.llm.mu.Lock()
	plain := len(m.llm.reqs)
	m.llm.mu.Unlock()
	if plain > maxLookups+3 {
		t.Fatalf("the default budget did not hold: %d model calls", plain)
	}
	answerOf(t, m.streamWith(m.dev, map[string]any{"autopilot": map[string]any{"minutes": 5, "steps": 3}}, many...))
	m.llm.mu.Lock()
	small := len(m.llm.reqs)
	m.llm.mu.Unlock()
	if small > 6 || !strings.Contains(m.llm.lastUser(small-1), "autopilot steps are used up") {
		t.Errorf("the envelope did not end the work: %d model calls", small)
	}
	answerOf(t, m.streamWith(m.dev, map[string]any{"autopilot": map[string]any{"minutes": 5, "steps": 60}}, many...))
	m.llm.mu.Lock()
	big := len(m.llm.reqs)
	m.llm.mu.Unlock()
	if big <= plain {
		t.Errorf("autopilot did not allow more steps: %d against %d", big, plain)
	}

	// The model is told it is on autopilot, and not when it is off.
	answerOf(t, m.streamWith(m.dev, map[string]any{"autopilot": map[string]any{}}, "Hi."))
	if !strings.Contains(m.lastSystem(), "Autopilot is on") {
		t.Error("the model is not told about autopilot")
	}
	m.dev.expect(200, "POST", "/v1/assistant/autopilot/stop", map[string]any{"conversation": apConv})
	answerOf(t, m.streamWith(m.dev, nil, "Hi."))
	if strings.Contains(m.lastSystem(), "Autopilot is on") {
		t.Error("the model is told about autopilot while it is off")
	}

	// Stop ends it at once.
	a, started := startAutopilot("someone|else|c", autopilotRequest{})
	if !started || !a.active() {
		t.Fatal("not started")
	}
	if b, again := startAutopilot("someone|else|c", autopilotRequest{}); again || b != a {
		t.Error("a second start made a second autopilot")
	}
	endAutopilot("someone|else|c")
	if a.active() || a.ended() != "stopped" {
		t.Error("stop did not end it")
	}
	a2, _ := startAutopilot("t|t|t", autopilotRequest{Minutes: 1, Steps: 1})
	a2.until = time.Now().Add(-time.Second)
	if a2.active() || a2.ended() != "time" {
		t.Error("an out-of-time autopilot is still active")
	}
	endAutopilot("t|t|t")
	m.dev.expect(200, "POST", "/v1/assistant/autopilot/stop", map[string]any{"conversation": apConv})
	m.v.expect(403, "POST", "/v1/assistant/autopilot/stop", map[string]any{"conversation": apConv})

	// An administrator can turn it off for everyone.
	m.dev.expect(403, "PUT", "/v1/assistant/autopilot", map[string]any{"allowed": false})
	m.admin.expect(400, "PUT", "/v1/assistant/autopilot", map[string]any{})
	m.admin.expect(200, "PUT", "/v1/assistant/autopilot", map[string]any{"allowed": false})
	if resp, _ := m.dev.do("POST", "/v1/assistant/runs", map[string]any{"message": "hi", "autopilot": map[string]any{}}); resp.StatusCode != 403 {
		t.Errorf("autopilot started although an administrator turned it off: %d", resp.StatusCode)
	}
	m.admin.expect(200, "PUT", "/v1/assistant/autopilot", map[string]any{"allowed": true})
}

func TestAnEndedAutopilotAllowsNothingAndAStoppedOneIsNotResumed(t *testing.T) {
	ctx := func(a *autopilot) context.Context { return context.WithValue(context.Background(), autopilotKey{}, a) }
	dec := permDecision{Effect: permAsk, Why: "a change asks first"}
	cmd := "aws ecs update-service --cluster web --service api"
	a, _ := startAutopilot("k1", autopilotRequest{Minutes: 5, Steps: 2})
	if !autopilotAllowsChange(ctx(a), dec, cmd) {
		t.Error("a running autopilot does not allow a plain change")
	}
	a.spend()
	if !autopilotAllowsChange(ctx(a), dec, cmd) {
		t.Error("the step before the last was refused")
	}
	a.spend()
	if autopilotAllowsChange(ctx(a), dec, cmd) {
		t.Error("an autopilot with no steps left allowed a change")
	}
	b, _ := startAutopilot("k2", autopilotRequest{})
	b.stop()
	if autopilotAllowsChange(ctx(b), dec, cmd) {
		t.Error("a stopped autopilot allowed a change")
	}
	if autopilotAllowsChange(context.Background(), dec, cmd) {
		t.Error("no autopilot allowed a change")
	}
	endAutopilot("k1")
	endAutopilot("k2")

	// A message without the switch ends it for good: switching it on again starts a new envelope.
	autopilotMu.Lock()
	clear(autopilots) // other tests leave theirs standing, and the conversation id is shared
	autopilotMu.Unlock()
	m := newMCPHarness(t)
	answerOf(t, m.streamWith(m.dev, map[string]any{"autopilot": map[string]any{"steps": 7}}, "Hi."))
	answerOf(t, m.streamWith(m.dev, nil, "Hi."))
	autopilotMu.Lock()
	left := 0
	for k := range autopilots {
		if strings.HasSuffix(k, "|"+apConv) {
			left++
		}
	}
	autopilotMu.Unlock()
	if left != 0 {
		t.Errorf("a message without the switch left the autopilot standing: %v", autopilots)
	}
}

func TestAutopilotHasNoLimitUnlessOneIsSet(t *testing.T) {
	a, _ := startAutopilot("free|x|c", autopilotRequest{})
	for i := 0; i < 5000; i++ {
		a.spend()
	}
	if !a.active() || a.ended() != "" {
		t.Error("autopilot with no limit ended")
	}
	b, _ := startAutopilot("steps|x|c", autopilotRequest{Steps: 3})
	for i := 0; i < 3; i++ {
		b.spend()
	}
	if b.active() || b.ended() != "steps" {
		t.Error("a step limit did not end it")
	}
	c, _ := startAutopilot("time|x|c", autopilotRequest{Minutes: 1})
	c.until = time.Now().Add(-time.Second)
	if c.active() || c.ended() != "time" {
		t.Error("a time limit did not end it")
	}
	endAutopilot("free|x|c")
	endAutopilot("steps|x|c")
	endAutopilot("time|x|c")
	m := newMCPHarness(t)
	var g struct {
		Allowed bool `json:"allowed"`
	}
	json.Unmarshal(m.dev.expect(200, "GET", "/v1/assistant/autopilot", nil), &g)
	if !g.Allowed || strings.Contains(string(m.dev.expect(200, "GET", "/v1/assistant/autopilot", nil)), "default_") {
		t.Error("the page still talks about default limits")
	}
}

func TestAutopilotRunsTheAgentsOwnToolsInTheContainerWithoutACardAndNotOnTheServer(t *testing.T) {
	m, _, _ := containerEngineHarness(t)
	start := func(c *browser) {
		c.expect(200, "POST", "/v1/assistant/autopilot/start", map[string]any{"conversation": apConv})
	}
	answerText := func(st *streamed) string {
		events, _ := answerOf(t, st)
		var text string
		for _, f := range events {
			if f["type"] == "TEXT_MESSAGE_CONTENT" {
				text += f["delta"].(string)
			}
		}
		return text
	}
	// Switched on before the message: no card, the tool is allowed, and it is audited.
	start(m.dev)
	st := m.streamWith(m.dev, map[string]any{"message": "deploy", "conversationId": apConv, "autopilot": map[string]any{}}, "unused")
	if text := answerText(st); !strings.Contains(text, "in container: allow") {
		t.Errorf("autopilot: %q", text)
	}
	if aud := string(m.admin.expect(200, "GET", "/v1/audit?limit=60", nil)); !strings.Contains(aud, "autopilot, no card") {
		t.Error("the audit log does not say autopilot allowed it")
	}
	endAutopilot(autopilotKeyFor(Principal{UserID: "dev", Name: "dev"}, apConv))
	m.dev.expect(200, "POST", "/v1/assistant/autopilot/stop", map[string]any{"conversation": apConv})

	// Switched on while the agent is waiting for an answer: the waiting request is answered yes.
	st = m.streamWith(m.dev, map[string]any{"message": "deploy again", "conversationId": apConv}, "unused")
	m.waitPending(t)
	var out struct{ Answered int }
	json.Unmarshal(m.dev.expect(200, "POST", "/v1/assistant/autopilot/start", map[string]any{"conversation": apConv}), &out)
	if out.Answered != 1 {
		t.Errorf("answered %d waiting requests", out.Answered)
	}
	if text := answerText(st); !strings.Contains(text, "in container: allow") {
		t.Errorf("switched on while waiting: %q", text)
	}
	m.dev.expect(200, "POST", "/v1/assistant/autopilot/stop", map[string]any{"conversation": apConv})

	// An administrator's switch-off holds, and autopilot does not touch another person's waiting request.
	m.admin.expect(200, "PUT", "/v1/assistant/autopilot", map[string]any{"allowed": false})
	m.dev.expect(403, "POST", "/v1/assistant/autopilot/start", map[string]any{"conversation": apConv})
	m.admin.expect(200, "PUT", "/v1/assistant/autopilot", map[string]any{"allowed": true})
	st = m.streamWith(m.dev, map[string]any{"message": "again", "conversationId": apConv}, "unused")
	id := m.waitPending(t)
	m.admin.expect(200, "POST", "/v1/assistant/autopilot/start", map[string]any{"conversation": apConv})
	time.Sleep(50 * time.Millisecond)
	if !m.srv.approvals.stillWaiting(id) {
		t.Error("another person's autopilot answered my waiting request")
	}
	m.dev.expect(200, "POST", "/v1/assistant/approvals/"+id, map[string]any{"decision": "deny"})
	if text := answerText(st); !strings.Contains(text, "deny") {
		t.Errorf("a refused request: %q", text)
	}
	m.admin.expect(200, "POST", "/v1/assistant/autopilot/stop", map[string]any{"conversation": apConv})
}

func TestAutopilotDoesNotCoverTheAgentsToolsOnTheServerItself(t *testing.T) {
	m := engineHarness(t) // the sandbox is off: the agent runs on the server
	m.dev.expect(200, "POST", "/v1/assistant/autopilot/start", map[string]any{"conversation": apConv})
	st := m.streamWith(m.dev, map[string]any{"message": "run it", "conversationId": apConv, "autopilot": map[string]any{}}, "unused")
	id := m.waitPending(t) // still asks
	m.dev.expect(200, "POST", "/v1/assistant/approvals/"+id, map[string]any{"decision": "approve"})
	answerOf(t, st)
	m.dev.expect(200, "POST", "/v1/assistant/autopilot/stop", map[string]any{"conversation": apConv})
}

func TestAutopilotWithoutALimitDoesNotStopAtTheUsualBudget(t *testing.T) {
	m := newMCPHarness(t)
	look := lookupReply("list_pipelines", map[string]any{})
	var many []string
	for i := 0; i < 70; i++ {
		many = append(many, look)
	}
	many = append(many, "Finished.")
	answerOf(t, m.streamWith(m.dev, map[string]any{"autopilot": map[string]any{}}, many...))
	if n := m.llm.count(); n < 71 {
		t.Errorf("autopilot with no limit stopped after %d model calls", n)
	}
	m.dev.expect(200, "POST", "/v1/assistant/autopilot/stop", map[string]any{"conversation": apConv})
}
