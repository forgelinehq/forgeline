package server

import (
	"context"
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
