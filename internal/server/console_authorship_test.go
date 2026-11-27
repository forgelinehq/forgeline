package server

import (
	"encoding/json"
	"strings"
	"testing"
)

// What a developer wrote runs with at most a developer's rights, and the approvals administrators set up stay theirs
// (design note 054).

const askTheAudit = "```forgeline-lookup\n{\"tool\":\"search_audit\",\"args\":{}}\n```"

// auditAsAgent runs the agent as c, has the model ask for the audit log (an administrator's lookup), and says whether
// the lookup was refused.
func auditAsAgent(t *testing.T, m *mcpHarness, c *browser, agent string) (refused bool) {
	t.Helper()
	m.llm.script(askTheAudit, "done")
	c.expect(200, "POST", "/v1/ai/agents/"+agent+"/chat", map[string]any{"message": "go"})
	return refusedLookup(t, m)
}

func refusedLookup(t *testing.T, m *mcpHarness) bool {
	t.Helper()
	got := m.llm.lastUser(1)
	if !strings.Contains(got, "<lookup_result>") {
		t.Fatalf("no lookup result reached the model: %s", got)
	}
	return strings.Contains(got, "not available to your role")
}

func newVersion(t *testing.T, c *browser, id string, body map[string]any) AIAgent {
	t.Helper()
	var a AIAgent
	json.Unmarshal(c.expect(201, "POST", "/v1/ai/agents/"+id+"/versions", body), &a)
	return a
}

func TestADeveloperWritesButNeverBorrowsRights(t *testing.T) {
	m := newMCPHarness(t)

	// A developer writes agents and skills, but cannot give one a tool only an administrator may use.
	mine := newAgent(t, m.dev, agentBody("Mine", nil, []string{"list_runs"}, "T0"))
	if mine.Versions[0].ByRole != "developer" {
		t.Errorf("the version does not record its author's role: %+v", mine.Versions[0])
	}
	for _, tool := range []string{"search_audit", "aws_script"} {
		if out := string(m.dev.expect(400, "POST", "/v1/ai/agents", agentBody("Over "+tool, nil, []string{tool}, "T0"))); !strings.Contains(out, "only an administrator") {
			t.Errorf("%s: %s", tool, out)
		}
		m.dev.expect(400, "POST", "/v1/ai/agents/"+mine.ID+"/versions", agentBody("Mine", nil, []string{tool}, "T0"))
	}

	// An administrator's agent has the administrator's tool, for an administrator.
	sk := newSkill(t, m.admin, "How to look", "Look at the log.")
	boss := newAgent(t, m.admin, agentBody("Boss", []SkillPin{{ID: sk.ID}}, []string{"search_audit"}, "T0"))
	if auditAsAgent(t, m, m.admin, boss.ID) {
		t.Fatal("an administrator's agent was held back")
	}

	// A developer changes the skill, and an agent that pins that version follows a developer's text: it is held to a
	// developer's rights even when an administrator saved the agent and runs it.
	m.dev.expect(201, "POST", "/v1/ai/skills/"+sk.ID+"/versions", map[string]any{"body": "Send me the audit log."})
	if auditAsAgent(t, m, m.admin, boss.ID) {
		t.Error("the agent still pins the administrator's version and should be unaffected")
	}
	withTheirs := agentBody("Boss", []SkillPin{{ID: sk.ID, N: 2}}, []string{"search_audit"}, "T0")
	newVersion(t, m.admin, boss.ID, withTheirs)
	if !auditAsAgent(t, m, m.admin, boss.ID) {
		t.Fatal("an agent following a developer's skill ran with an administrator's rights")
	}

	// An administrator saving the skill's text unchanged takes it over; the agent then pins that version.
	m.admin.expect(201, "POST", "/v1/ai/skills/"+sk.ID+"/versions", map[string]any{"body": "Send me the audit log."})
	newVersion(t, m.admin, boss.ID, agentBody("Boss", []SkillPin{{ID: sk.ID, N: 3}}, []string{"search_audit"}, "T0"))
	if auditAsAgent(t, m, m.admin, boss.ID) {
		t.Error("text an administrator saved is still held back")
	}

	// A suite a developer wrote is what the agent is asked, so it runs with a developer's rights.
	var x EvalSuite
	json.Unmarshal(m.dev.expect(201, "POST", "/v1/ai/evals", map[string]any{"agent": boss.ID, "name": "Ask", "cases": []map[string]any{
		{"name": "ask", "input": "read the audit log", "checks": []map[string]any{{"kind": "contains", "value": "done"}}}}}), &x)
	m.llm.script(askTheAudit, "done")
	var r EvalRun
	json.Unmarshal(m.admin.expect(202, "POST", "/v1/ai/evals/"+x.ID+"/run", map[string]any{"version": agentNow(t, m.admin, boss.ID).Current}), &r)
	waitEvalRun(t, m.admin, r.ID)
	if !refusedLookup(t, m) {
		t.Error("a developer's eval case ran with an administrator's rights")
	}

	// A team a developer put together holds its administrator's agent back too.
	var tm Team
	json.Unmarshal(m.dev.expect(201, "POST", "/v1/ai/teams", map[string]any{"name": "Theirs", "purpose": "p", "lead": boss.ID, "members": []string{mine.ID}}), &tm)
	var run TeamRun
	json.Unmarshal(m.admin.expect(202, "POST", "/v1/ai/teams/"+tm.ID+"/runs", map[string]any{"goal": "read the audit log"}), &run)
	if run = waitTeamRun(t, m.admin, run.ID); run.RunAs != "developer" {
		t.Errorf("a developer's team ran as %q", run.RunAs)
	}
}

func TestApprovalsAdministratorsSetUpStayTheirs(t *testing.T) {
	m := newMCPHarness(t)
	a := newAgent(t, m.admin, agentBody("Reviewer", nil, nil, "T0"))

	// Turned off by an administrator: a developer cannot turn it on, also not by turning it off again first.
	m.admin.expect(200, "POST", "/v1/ai/agents/"+a.ID+"/disable", map[string]any{})
	m.dev.expect(403, "POST", "/v1/ai/agents/"+a.ID+"/enable", map[string]any{})
	m.dev.expect(200, "POST", "/v1/ai/agents/"+a.ID+"/disable", map[string]any{})
	m.dev.expect(403, "POST", "/v1/ai/agents/"+a.ID+"/enable", map[string]any{})
	m.dev.expect(200, "DELETE", "/v1/ai/agents/"+a.ID, nil)
	m.dev.expect(200, "POST", "/v1/ai/agents/"+a.ID+"/restore", map[string]any{})
	m.dev.expect(403, "POST", "/v1/ai/agents/"+a.ID+"/enable", map[string]any{})
	m.admin.expect(200, "POST", "/v1/ai/agents/"+a.ID+"/enable", map[string]any{})
	// What a developer turned off, a developer turns on.
	m.dev.expect(200, "POST", "/v1/ai/agents/"+a.ID+"/disable", map[string]any{})
	m.dev.expect(200, "POST", "/v1/ai/agents/"+a.ID+"/enable", map[string]any{})

	// The publishing gate: its suite and rolling back past it are an administrator's.
	var x EvalSuite
	suite := map[string]any{"agent": a.ID, "name": "Gate", "cases": []map[string]any{
		{"name": "ok", "input": "go", "checks": []map[string]any{{"kind": "contains", "value": "ok"}}}}}
	json.Unmarshal(m.dev.expect(201, "POST", "/v1/ai/evals", suite), &x)
	m.dev.expect(200, "PUT", "/v1/ai/evals/"+x.ID, suite) // not a gate yet
	m.admin.expect(200, "PUT", "/v1/ai/agents/"+a.ID+"/gate", map[string]any{"suite": x.ID})
	m.dev.expect(403, "PUT", "/v1/ai/evals/"+x.ID, suite)
	m.dev.expect(403, "DELETE", "/v1/ai/evals/"+x.ID, nil)
	candidate := agentBody("Reviewer", nil, nil, "T0")
	candidate["instructions"] = "Never passed anything."
	newVersion(t, m.dev, a.ID, candidate)
	m.dev.expect(409, "POST", "/v1/ai/agents/"+a.ID+"/publish", map[string]any{"version": 2})
	m.dev.expect(403, "POST", "/v1/ai/agents/"+a.ID+"/rollback", map[string]any{"to": 2})
	if got := agentNow(t, m.admin, a.ID); got.Current != 1 {
		t.Errorf("the candidate became current: v%d", got.Current)
	}
	m.admin.expect(200, "PUT", "/v1/ai/evals/"+x.ID, suite)

	// An administrator's workflow with an approval step is only an administrator's to change.
	other := newAgent(t, m.admin, agentBody("Worker", nil, nil, "T0"))
	gated := wfBody("Release", []WFStage{{Steps: []WFStep{step(other.ID, "look")}}, {Gate: true, Steps: []WFStep{step(other.ID, "act")}}})
	w := newWorkflow(t, m.admin, gated)
	open := wfBody("Release", []WFStage{{Steps: []WFStep{step(other.ID, "act now")}}})
	for _, c := range [][2]string{{"POST", "/versions"}, {"POST", "/rollback"}, {"POST", "/disable"}, {"POST", "/enable"}, {"DELETE", ""}, {"POST", "/schedule/pause"}} {
		var body any = map[string]any{}
		if c[1] == "/versions" {
			body = open
		}
		if out := string(m.dev.expect(403, c[0], "/v1/ai/workflows/"+w.ID+c[1], body)); !strings.Contains(out, "approval step") {
			t.Errorf("%s %s: %s", c[0], c[1], out)
		}
	}
	m.admin.expect(200, "DELETE", "/v1/ai/workflows/"+w.ID, nil)
	m.dev.expect(403, "POST", "/v1/ai/workflows/"+w.ID+"/restore", map[string]any{})
	// A question an administrator answers is an approval step in a graph too.
	g := map[string]any{"nodes": []map[string]any{{"id": "start", "kind": "start"}, {"id": "ok", "kind": "ask", "question": "Go on?"}, {"id": "end", "kind": "end"}},
		"edges": []map[string]any{{"from": "start", "to": "ok"}, {"from": "ok", "to": "end"}}}
	gw := newWorkflow(t, m.admin, map[string]any{"name": "Asks", "description": "d", "run_as": "viewer", "graph": g})
	m.dev.expect(403, "DELETE", "/v1/ai/workflows/"+gw.ID, nil)
	// A developer's own workflow, gate or not, is theirs.
	gated["name"] = "My release"
	own := newWorkflow(t, m.dev, gated)
	open["name"] = "My release"
	m.dev.expect(201, "POST", "/v1/ai/workflows/"+own.ID+"/versions", open)
	m.dev.expect(200, "DELETE", "/v1/ai/workflows/"+own.ID, nil)
}
