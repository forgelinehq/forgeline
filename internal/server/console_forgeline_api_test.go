package server

import (
	"context"
	"encoding/json"
	"github.com/forgelinehq/forgeline/internal/authn"
	"strings"
	"testing"
)

func apiCall(method, path string, body any) string {
	return lookupReply(forgelineAPITool, map[string]any{"method": method, "path": path, "body": body})
}

func newTestAgent(t *testing.T, m *mcpHarness, name string) string {
	t.Helper()
	var out struct{ ID string }
	json.Unmarshal(m.admin.expect(201, "POST", "/v1/ai/agents", map[string]any{"name": name, "description": "one line", "instructions": "You help.", "tools": []string{"list_runs"}, "tier": "T0"}), &out)
	if out.ID == "" {
		t.Fatal("no agent id")
	}
	return out.ID
}

func TestTheAssistantCanUpdateAndArchiveWhatExistsThroughTheConsoleAPIAsThePerson(t *testing.T) {
	m := newMCPHarness(t)
	id := newTestAgent(t, m, "Checker")
	// A read runs at once.
	r := results(m.run(m.admin, map[string]any{"message": "look"}, apiCall("GET", "/v1/ai/agents", nil)))
	if !strings.Contains(r[0], "Checker") {
		t.Errorf("a read: %s", r[0])
	}
	// A change asks first: a card with the exact request, and nothing changes until the answer.
	st := m.streamWith(m.admin, map[string]any{"message": "update it"}, apiCall("POST", "/v1/ai/agents/"+id+"/versions", map[string]any{"description": "one line", "instructions": "You are a careful reviewer. Be thorough.", "tools": []string{"list_runs"}, "tier": "T0", "note": "full instructions"}), "Done.")
	pid := m.waitPending(t)
	if !strings.Contains(agentText(t, m.admin, id), "You help.") {
		t.Error("the agent changed before the person answered")
	}
	m.admin.expect(200, "POST", "/v1/assistant/approvals/"+pid, map[string]any{"decision": "approve"})
	events, last := answerOf(t, st)
	t.Logf("update result: %v", last)
	card := eventsNamed(events, "forgeline.permission")
	if len(card) != 1 || card[0]["tool"] != forgelineAPITool || !strings.Contains(card[0]["command"].(string), "POST /v1/ai/agents/"+id+"/versions") {
		t.Errorf("the card: %v", card)
	}
	if !strings.Contains(agentText(t, m.admin, id), "careful reviewer") {
		t.Error("the update did not happen: it must be a new version of the same agent, not a copy")
	}
	var list struct{ Agents []struct{ ID string } }
	json.Unmarshal(m.admin.expect(200, "GET", "/v1/ai/agents", nil), &list)
	if len(list.Agents) != 1 {
		t.Errorf("%d agents: an update made a copy", len(list.Agents))
	}
	// Under autopilot a change runs without a card, as the person, and is audited as theirs.
	r = results(m.run(m.admin, map[string]any{"message": "archive it", "conversationId": apConv, "autopilot": map[string]any{}}, apiCall("DELETE", "/v1/ai/agents/"+id, nil)))
	if !strings.Contains(r[0], "DELETE /v1/ai/agents/"+id+": 200") {
		t.Errorf("autopilot: %s", r[0])
	}
	m.admin.expect(200, "POST", "/v1/assistant/autopilot/stop", map[string]any{"conversation": apConv})
	aud := string(m.admin.expect(200, "GET", "/v1/audit?limit=80", nil))
	for _, want := range []string{"assistant.api", "autopilot, no card", "ai.agent"} {
		if !strings.Contains(strings.ToLower(aud), strings.ToLower(want)) {
			t.Errorf("the audit log lacks %q", want)
		}
	}
	// The role is the person's: a developer cannot change an agent through the assistant any more than in the console.
	id2 := newTestAgent(t, m, "Other")
	st = m.streamWith(m.dev, map[string]any{"message": "update it"}, apiCall("POST", "/v1/ai/agents/"+id2+"/versions", map[string]any{"instructions": "x"}), "Done.")
	pid = m.waitPending(t)
	m.dev.expect(200, "POST", "/v1/assistant/approvals/"+pid, map[string]any{"decision": "approve"})
	events, res := answerOf(t, st)
	_ = events
	if res == nil || !strings.Contains(res["content"].(string), "403") {
		t.Errorf("a developer's change: %v", res)
	}
	// A refusal changes nothing.
	st = m.streamWith(m.admin, map[string]any{"message": "again"}, apiCall("DELETE", "/v1/ai/agents/"+id2, nil), "Done.")
	pid = m.waitPending(t)
	m.admin.expect(200, "POST", "/v1/assistant/approvals/"+pid, map[string]any{"decision": "deny"})
	_, res = answerOf(t, st)
	if res == nil || !strings.Contains(res["content"].(string), "you said no") {
		t.Errorf("a refusal: %v", res)
	}
	if agentText(t, m.admin, id2) == "" {
		t.Error("a refused delete deleted")
	}
}

func TestTheAssistantCannotReachWhatDecidesWhoMayDoWhat(t *testing.T) {
	m := newMCPHarness(t)
	never := [][2]string{
		{"GET", "/v1/users"}, {"POST", "/v1/users"}, {"POST", "/v1/tokens"}, {"GET", "/v1/secrets"}, {"PUT", "/v1/secrets/X"},
		{"PUT", "/v1/assistant/permissions/accounts/primary"}, {"PUT", "/v1/assistant/sandbox"}, {"PUT", "/v1/assistant/autopilot"},
		{"PUT", "/v1/integrations/assistant"}, {"POST", "/v1/ai/proposals/p1/approve"}, {"POST", "/v1/cloud/changes/c1/apply"},
		{"POST", "/v1/ai/agents/a/chat"}, {"PUT", "/v1/ai/agents/a/gate"}, {"PUT", "/v1/ai/agents/a/budget"}, {"POST", "/v1/ai/workflows/w/run"},
		{"POST", "/v1/runs"}, {"POST", "/v1/auth/login"}, {"POST", "/v1/assistant/approvals/x"}, {"POST", "/v1/assistant/actions/x/confirm"},
		{"PUT", "/v1/providers/x"}, {"POST", "/v1/market/install"},
		{"GET", "../v1/users"}, {"GET", "/v1/ai/agents/../users"}, {"GET", "/v1/ai//agents"}, {"GET", "/mcp"}, {"GET", "/healthz"},
	}
	var calls []string
	for _, c := range never {
		calls = append(calls, apiCall(c[0], c[1], nil))
	}
	for start := 0; start < len(calls); start += 20 { // a turn is at most 40 steps
		end := min(start+20, len(calls))
		rs := results(m.run(m.admin, map[string]any{"message": "try"}, calls[start:end]...))
		for i, r := range rs {
			if !strings.Contains(r, "lookup failed") {
				t.Errorf("%s %s was allowed: %s", never[start+i][0], never[start+i][1], truncate(r, 120))
			}
		}
	}
	// Writing outside what it may change is refused too, and GET of a secret's value is not a thing it can do.
	rs := results(m.run(m.admin, map[string]any{"message": "try"}, apiCall("POST", "/v1/blueprints", map[string]any{"name": "x"}), apiCall("PUT", "/v1/settings/approvals", map[string]any{})))
	if !strings.Contains(rs[0], "not one of them") {
		t.Errorf("a write outside the allowed parts: %s", rs[0])
	}
}

// agentText is the current instructions of an agent.
func agentText(t *testing.T, c *browser, id string) string {
	t.Helper()
	var a struct {
		Current  int
		Versions []struct {
			N            int
			Instructions string
		}
	}
	json.Unmarshal(c.expect(200, "GET", "/v1/ai/agents/"+id, nil), &a)
	for _, v := range a.Versions {
		if v.N == a.Current {
			return v.Instructions
		}
	}
	return ""
}

func TestAnAgentAndAMalformedPathCannotUseTheAPITool(t *testing.T) {
	m := newMCPHarness(t)
	ctx := context.WithValue(t.Context(), memTurnKey{}, &memTurn{p: Principal{UserID: "u1", Name: "u1", Role: authn.Admin}})
	ok := json.RawMessage(`{"method":"GET","path":"/v1/ai/agents"}`)
	if _, err := m.srv.lookupForgelineAPI(ctx, ok); err != nil {
		t.Fatalf("a person's own read failed: %v", err)
	}
	agent := context.WithValue(ctx, agentScopeKey{}, &agentScope{tools: map[string]bool{forgelineAPITool: true}})
	if _, err := m.srv.lookupForgelineAPI(agent, ok); err == nil {
		t.Error("an agent used the API tool")
	}
	for _, path := range []string{"/v1/ai/agents/../users", "/v1/ai//agents", "v1/ai/agents", "/v1/ai/agents #x", "/etc/passwd", ""} {
		_, err := m.srv.lookupForgelineAPI(ctx, json.RawMessage(`{"method":"GET","path":"`+path+`"}`))
		if err == nil || !strings.Contains(err.Error(), "API path") {
			t.Errorf("%q: %v", path, err)
		}
	}
}
