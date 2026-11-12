package server

import (
	"encoding/json"
	"strings"
	"testing"
)

// The assistant writes skills, agents and workflows as cards an administrator confirms (note 028). Nothing exists until
// then, the card says what it would do in words the server derives (not the model), and confirming goes through the
// console's own checks again.

func prepareDef(kind string, spec any) string {
	return lookupReply("prepare_action", map[string]any{"action": kind, "spec": spec})
}

func confirmCard(t *testing.T, m *mcpHarness, c *browser, evs []map[string]any, want int) AssistAction {
	t.Helper()
	cards := actionsIn(evs)
	if len(cards) != 1 {
		t.Fatalf("cards = %v (the model was told: %s)", cards, m.llm.lastUser(1))
	}
	var a AssistAction
	json.Unmarshal(c.expect(want, "POST", "/v1/assistant/actions/"+cards[0]["id"].(string)+"/confirm", nil), &a)
	return a
}

func TestAssistantWritesASkillAnAgentAndAWorkflow(t *testing.T) {
	m := newMCPHarness(t)

	// A skill.
	evs := m.run(m.admin, map[string]any{"message": "write a skill for cost reviews"}, prepareDef("save_skill",
		map[string]any{"name": "Cost review", "description": "Steps for a cost review", "body": "1. Read the summary.\n2. Report."}))
	if fx, _ := json.Marshal(actionsIn(evs)[0]["effects"]); !strings.Contains(string(fx), `Creates a new skill \"Cost review\"`) {
		t.Errorf("skill effects = %s", fx)
	}
	m.admin.expect(404, "GET", "/v1/ai/skills/cost-review", nil) // nothing until confirmed
	if a := confirmCard(t, m, m.admin, evs, 200); a.Status != actDone || a.Link != "/skills/cost-review" {
		t.Fatalf("skill action = %+v", a)
	}

	// An agent that follows it.
	evs = m.run(m.admin, map[string]any{"message": "an agent for it"}, prepareDef("save_agent", map[string]any{
		"name": "Cost watcher", "description": "Finds waste", "instructions": "## Goal\nFind waste.", "tools": []string{"aws_summary"},
		"skills": []map[string]any{{"id": "cost-review", "n": 0}}, "tier": "T0"}))
	fx, _ := json.Marshal(actionsIn(evs)[0]["effects"])
	if !strings.Contains(string(fx), "only reads and explains") || !strings.Contains(string(fx), "aws_summary") || !strings.Contains(string(fx), "cost-review at version 1") {
		t.Errorf("agent effects = %s", fx)
	}
	if a := confirmCard(t, m, m.admin, evs, 200); a.Link != "/agents/cost-watcher" {
		t.Fatalf("agent action = %+v", a)
	}
	var ag AIAgent
	json.Unmarshal(m.admin.expect(200, "GET", "/v1/ai/agents/cost-watcher", nil), &ag)
	if ag.Versions[0].Skills[0].N != 1 || ag.Versions[0].Tools[0] != "aws_summary" {
		t.Errorf("saved agent = %+v", ag.Versions[0])
	}

	// A workflow that uses the agent.
	evs = m.run(m.admin, map[string]any{"message": "a weekly workflow"}, prepareDef("save_workflow", map[string]any{
		"name": "Weekly cost review", "description": "d", "run_as": "viewer", "every_minutes": 10080,
		"stages": []map[string]any{{"name": "Look", "steps": []map[string]any{{"agent": "cost-watcher", "task": "Look for waste."}}},
			{"name": "Release", "gate": true, "steps": []map[string]any{{"agent": "cost-watcher", "task": "Summarise."}}}}}))
	fx, _ = json.Marshal(actionsIn(evs)[0]["effects"])
	if !strings.Contains(string(fx), "every 10080 minutes") || !strings.Contains(string(fx), "waits for an administrator") {
		t.Errorf("workflow effects = %s", fx)
	}
	if a := confirmCard(t, m, m.admin, evs, 200); a.Link != "/workflows/weekly-cost-review" {
		t.Fatalf("workflow action = %+v", a)
	}
	if !hasAudit(t, m.admin, "ai.workflow.create") {
		t.Error("the workflow's creation is not audited")
	}
}

func TestAssistantDefinitionsAreForAdministratorsAndChecked(t *testing.T) {
	m := newMCPHarness(t)
	skill := map[string]any{"name": "S", "description": "d", "body": "1. x"}

	// A developer cannot create these in the console, so the assistant cannot prepare them for one.
	if cards := actionsIn(m.run(m.dev, map[string]any{"message": "a"}, prepareDef("save_skill", skill))); len(cards) != 0 {
		t.Errorf("a developer got a card: %v", cards)
	}
	if !strings.Contains(m.llm.lastUser(1), "only an administrator") {
		t.Errorf("the model was not told why: %s", m.llm.lastUser(1))
	}
	for name, bad := range map[string]struct {
		kind string
		spec any
	}{
		"unknown field":         {"save_skill", map[string]any{"name": "S", "description": "d", "body": "1. x", "tier": "T1"}},
		"empty skill":           {"save_skill", map[string]any{"name": "S", "description": "d", "body": " "}},
		"agent with no purpose": {"save_agent", map[string]any{"name": "A", "description": "d", "instructions": "", "tier": "T0"}},
		"unknown tool":          {"save_agent", map[string]any{"name": "A", "description": "d", "instructions": "x", "tools": []string{"rm_rf"}, "tier": "T0"}},
		"proposal tool at T0":   {"save_agent", map[string]any{"name": "A", "description": "d", "instructions": "x", "tools": []string{"propose_aws_change"}, "tier": "T0"}},
		"workflow, no agent":    {"save_workflow", map[string]any{"name": "W", "description": "d", "run_as": "viewer", "stages": []map[string]any{{"name": "S", "steps": []map[string]any{{"agent": "ghost", "task": "t"}}}}}},
		"workflow as admin":     {"save_workflow", map[string]any{"name": "W", "description": "d", "run_as": "admin", "stages": []map[string]any{}}},
	} {
		if cards := actionsIn(m.run(m.admin, map[string]any{"message": name}, prepareDef(bad.kind, bad.spec))); len(cards) != 0 {
			t.Errorf("%s: got a card", name)
		}
	}

	// A card checked when prepared is checked again when confirmed: here the name was taken meanwhile is fine (a new id
	// is chosen), but an agent the workflow uses that was turned off makes it invalid.
	newAgent(t, m.admin, agentBody("Worker", nil, nil, "T0"))
	evs := m.run(m.admin, map[string]any{"message": "w"}, prepareDef("save_workflow", map[string]any{"name": "W", "description": "d", "run_as": "viewer",
		"stages": []map[string]any{{"name": "S", "steps": []map[string]any{{"agent": "worker", "task": "t"}}}}}))
	m.admin.expect(200, "POST", "/v1/ai/agents/worker/disable", nil)
	if a := confirmCard(t, m, m.admin, evs, 400); a.ID != "" && a.Status == actDone {
		t.Errorf("a workflow over a turned-off agent was saved: %+v", a)
	}
	m.admin.expect(404, "GET", "/v1/ai/workflows/w", nil)
}
