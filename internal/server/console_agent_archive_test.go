package server

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/forgelinehq/forgeline/internal/authn"
	"github.com/forgelinehq/forgeline/internal/executor"
)

func TestDeletingAnAgentArchivesItThenTheSweepRemovesIt(t *testing.T) {
	h := newHarness(t, &executor.Fake{})
	admin := h.adminBrowser("ada")
	lead := newAgent(t, admin, agentBody("Lead", nil, nil, "T0"))
	member := newAgent(t, admin, agentBody("Member", nil, nil, "T0"))
	admin.expect(200, "PUT", "/v1/ai/agents/"+member.ID+"/reports-to", map[string]any{"reports_to": lead.ID})

	// A workflow that uses the lead keeps it from being archived, and the error says which.
	g := map[string]any{"nodes": []map[string]any{{"id": "start", "kind": "start"}, {"id": "ask", "kind": "agent", "agent": lead.ID, "task": "go"}, {"id": "end", "kind": "end"}},
		"edges": []map[string]any{{"from": "start", "to": "ask"}, {"from": "ask", "to": "end"}}}
	var wf Workflow
	json.Unmarshal(admin.expect(201, "POST", "/v1/ai/workflows", map[string]any{"name": "Uses the lead", "description": "d", "run_as": "viewer", "graph": g}), &wf)
	if resp, out := admin.do("DELETE", "/v1/ai/agents/"+lead.ID, nil); resp.StatusCode != 409 || !strings.Contains(string(out), "Uses the lead") {
		t.Fatalf("archive while used = %d %s", resp.StatusCode, out)
	}
	admin.expect(200, "POST", "/v1/ai/workflows/"+wf.ID+"/disable", map[string]any{})

	admin.createUser("dev", authn.Developer)
	dev := h.login("dev")
	dev.expect(403, "DELETE", "/v1/ai/agents/"+lead.ID, nil)
	admin.expect(200, "DELETE", "/v1/ai/agents/"+lead.ID, nil)
	admin.expect(409, "DELETE", "/v1/ai/agents/"+lead.ID, nil)

	// Hidden from the list and the team, listed with ?archived=1, cannot be turned on, and its report is cut.
	var list struct {
		Agents []map[string]any `json:"agents"`
	}
	json.Unmarshal(admin.expect(200, "GET", "/v1/ai/agents", nil), &list)
	if len(list.Agents) != 1 || list.Agents[0]["id"] != member.ID {
		t.Fatalf("agents = %+v", list.Agents)
	}
	json.Unmarshal(admin.expect(200, "GET", "/v1/ai/agents?archived=1", nil), &list)
	if len(list.Agents) != 1 || list.Agents[0]["id"] != lead.ID {
		t.Fatalf("archived = %+v", list.Agents)
	}
	if strings.Contains(string(admin.expect(200, "GET", "/v1/ai/team", nil)), `"`+lead.ID+`"`) {
		t.Error("the archived agent is still on the team")
	}
	admin.expect(409, "POST", "/v1/ai/agents/"+lead.ID+"/enable", map[string]any{})
	if m, _ := h.srv.loadAgent(t.Context(), member.ID); m.ReportsTo != "" {
		t.Errorf("member still reports to the archived lead")
	}

	// Restored: back in the list, still off; archived again, the sweep leaves it until the period has passed.
	admin.expect(200, "POST", "/v1/ai/agents/"+lead.ID+"/restore", map[string]any{})
	if a, _ := h.srv.loadAgent(t.Context(), lead.ID); a.archived() || !a.Disabled {
		t.Fatalf("restored = %+v", a)
	}
	admin.expect(200, "DELETE", "/v1/ai/agents/"+lead.ID, nil)
	now := time.Now().UTC()
	if n, err := h.srv.purgeArchivedAgents(t.Context(), now.Add(agentCoolingOff-time.Hour)); err != nil || n != 0 {
		t.Fatalf("early sweep removed %d (%v)", n, err)
	}
	if n, err := h.srv.purgeArchivedAgents(t.Context(), now.Add(agentCoolingOff+time.Hour)); err != nil || n != 1 {
		t.Fatalf("sweep removed %d (%v)", n, err)
	}
	admin.expect(404, "GET", "/v1/ai/agents/"+lead.ID, nil)
	if _, err := h.srv.loadAgent(t.Context(), member.ID); err != nil {
		t.Fatal("the sweep removed an agent that was not archived")
	}
	if !strings.Contains(string(admin.expect(200, "GET", "/v1/audit", nil)), "ai.agent.purge") {
		t.Error("the sweep left no audit entry")
	}
}

func TestDeletingAWorkflowArchivesItThenTheSweepRemovesIt(t *testing.T) {
	h := newHarness(t, &executor.Fake{})
	admin := h.adminBrowser("ada")
	a := newAgent(t, admin, agentBody("Worker", nil, nil, "T0"))
	g := map[string]any{"nodes": []map[string]any{{"id": "start", "kind": "start"}, {"id": "ask", "kind": "agent", "agent": a.ID, "task": "go"}, {"id": "end", "kind": "end"}},
		"edges": []map[string]any{{"from": "start", "to": "ask"}, {"from": "ask", "to": "end"}}}
	var wf Workflow
	json.Unmarshal(admin.expect(201, "POST", "/v1/ai/workflows", map[string]any{"name": "Old check", "description": "d", "run_as": "viewer", "graph": g}), &wf)
	var list struct {
		Workflows []map[string]any `json:"workflows"`
	}
	json.Unmarshal(admin.expect(200, "GET", "/v1/ai/workflows", nil), &list)
	if len(list.Workflows) != 1 || list.Workflows[0]["graph"] != true || list.Workflows[0]["agents"] != 1.0 {
		t.Fatalf("list = %+v", list.Workflows)
	}

	admin.expect(200, "DELETE", "/v1/ai/workflows/"+wf.ID, nil)
	admin.expect(409, "DELETE", "/v1/ai/workflows/"+wf.ID, nil)
	admin.expect(409, "POST", "/v1/ai/workflows/"+wf.ID+"/enable", map[string]any{})
	admin.expect(409, "POST", "/v1/ai/workflows/"+wf.ID+"/run", map[string]any{})
	json.Unmarshal(admin.expect(200, "GET", "/v1/ai/workflows", nil), &list)
	if len(list.Workflows) != 0 {
		t.Fatalf("archived still listed: %+v", list.Workflows)
	}
	json.Unmarshal(admin.expect(200, "GET", "/v1/ai/workflows?archived=1", nil), &list)
	if len(list.Workflows) != 1 {
		t.Fatalf("archived list = %+v", list.Workflows)
	}
	// Archived, it no longer keeps its agent from being archived.
	admin.expect(200, "POST", "/v1/ai/workflows/"+wf.ID+"/restore", map[string]any{})
	admin.expect(200, "DELETE", "/v1/ai/workflows/"+wf.ID, nil)
	if n, _ := h.srv.purgeArchivedWorkflows(t.Context(), time.Now().Add(agentCoolingOff-time.Hour)); n != 0 {
		t.Fatal("the sweep removed a workflow too early")
	}
	if n, _ := h.srv.purgeArchivedWorkflows(t.Context(), time.Now().Add(agentCoolingOff+time.Hour)); n != 1 {
		t.Fatal("the sweep did not remove the workflow")
	}
	admin.expect(404, "GET", "/v1/ai/workflows/"+wf.ID, nil)
}
