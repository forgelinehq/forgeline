package server

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func teamOf(t *testing.T, b *browser) (TeamJSON, map[string]TeamMember) {
	t.Helper()
	var tj TeamJSON
	json.Unmarshal(b.expect(200, "GET", "/v1/ai/team", nil), &tj)
	by := map[string]TeamMember{}
	for _, m := range tj.Members {
		by[m.Name] = m
	}
	return tj, by
}

func TestTeamReportingLines(t *testing.T) {
	m := newMCPHarness(t)
	lead := newAgent(t, m.admin, agentBody("Platform lead", nil, nil, "T0"))
	cost := newAgent(t, m.admin, agentBody("Cost watcher", nil, nil, "T1"))
	guard := newAgent(t, m.admin, agentBody("Security guard", nil, nil, "T0"))

	m.dev.expect(403, "PUT", "/v1/ai/agents/"+cost.ID+"/reports-to", map[string]any{"reports_to": lead.ID})
	if out := string(m.admin.expect(400, "PUT", "/v1/ai/agents/"+cost.ID+"/reports-to", map[string]any{"reports_to": cost.ID})); !strings.Contains(out, "cannot report to itself") {
		t.Errorf("self message: %s", out)
	}
	m.admin.expect(400, "PUT", "/v1/ai/agents/"+cost.ID+"/reports-to", map[string]any{"reports_to": "nope"})
	m.admin.expect(404, "PUT", "/v1/ai/agents/nope/reports-to", map[string]any{"reports_to": lead.ID})
	m.admin.expect(200, "PUT", "/v1/ai/agents/"+cost.ID+"/reports-to", map[string]any{"reports_to": lead.ID})
	m.admin.expect(200, "PUT", "/v1/ai/agents/"+guard.ID+"/reports-to", map[string]any{"reports_to": cost.ID})
	// A loop, directly or further up, is refused.
	if out := string(m.admin.expect(400, "PUT", "/v1/ai/agents/"+lead.ID+"/reports-to", map[string]any{"reports_to": guard.ID})); !strings.Contains(out, "already reports to") {
		t.Errorf("loop message: %s", out)
	}
	m.admin.expect(400, "PUT", "/v1/ai/agents/"+lead.ID+"/reports-to", map[string]any{"reports_to": cost.ID})

	// A new version of the agent keeps its line.
	m.admin.expect(201, "POST", "/v1/ai/agents/"+cost.ID+"/versions", map[string]any{"instructions": "Find waste.", "tier": "T1", "note": "n"})
	_, by := teamOf(t, m.dev)
	if by["Cost watcher"].ReportsTo != lead.ID || by["Security guard"].ReportsTo != cost.ID || by["Platform lead"].ReportsTo != "" {
		t.Fatalf("lines: %+v", by)
	}
	if by["Cost watcher"].Mode != "proposes" || by["Platform lead"].Mode != "advises" {
		t.Errorf("modes: %+v", by)
	}
	if a := string(m.admin.expect(200, "GET", "/v1/audit?action=ai.agent.team", nil)); !strings.Contains(a, "Cost watcher reports to Platform lead") {
		t.Errorf("not audited: %s", a)
	}
	// Back to people.
	m.admin.expect(200, "PUT", "/v1/ai/agents/"+guard.ID+"/reports-to", map[string]any{"reports_to": ""})
	if _, by = teamOf(t, m.dev); by["Security guard"].ReportsTo != "" {
		t.Errorf("not cleared: %+v", by["Security guard"])
	}
	// A loop already in storage (not something the API saves) is shown as reporting to people, not followed forever.
	a1, _ := m.srv.loadAgent(t.Context(), lead.ID)
	a1.ReportsTo = guard.ID
	m.srv.docs.Put(t.Context(), kindAIAgent, a1.ID, a1)
	a2, _ := m.srv.loadAgent(t.Context(), guard.ID)
	a2.ReportsTo = lead.ID
	m.srv.docs.Put(t.Context(), kindAIAgent, a2.ID, a2)
	if _, by = teamOf(t, m.dev); by["Platform lead"].ReportsTo != "" && by["Security guard"].ReportsTo != "" {
		t.Errorf("a stored loop is passed on: %+v", by)
	}
	m.v.expect(403, "GET", "/v1/ai/team", nil)
}

func TestTeamStatesComeFromWorkflowsAndRuns(t *testing.T) {
	m := newMCPHarness(t)
	worker := newAgent(t, m.admin, agentBody("Worker", nil, nil, "T0"))
	gated := newAgent(t, m.admin, agentBody("Gated", nil, nil, "T0"))
	sleeper := newAgent(t, m.admin, agentBody("Sleeper", nil, nil, "T0"))
	off := newAgent(t, m.admin, agentBody("Off", nil, nil, "T0"))
	newAgent(t, m.admin, agentBody("Idle", nil, nil, "T0"))
	body := wfBody("Nightly scan", []WFStage{{Name: "scan", Steps: []WFStep{step(sleeper.ID, "scan"), step(off.ID, "scan")}}})
	body["every_minutes"] = 60
	wf := newWorkflow(t, m.admin, body)
	m.admin.expect(200, "POST", "/v1/ai/agents/"+off.ID+"/disable", nil)
	last := time.Now().UTC().Add(-20 * time.Minute)
	m.srv.docs.Put(t.Context(), kindWFState, wf.ID, wfState{LastScheduledAt: last})

	now := time.Now().UTC()
	started := now.Add(-time.Minute)
	old := now.AddDate(0, 0, -9)
	runs := []WFRun{
		{ID: "r1", WorkflowName: "Last week", Status: wfFailed, StartedAt: old, Stages: []WFStageRun{{Name: "a", Status: wfFailed, Steps: []WFStepRun{{Agent: worker.ID, Status: wfFailed, StartedAt: &old}}}}},
		{ID: "r2", WorkflowName: "Clean tables", Status: wfRunning, StartedAt: started, Stages: []WFStageRun{{Name: "look", Status: wfRunning, Steps: []WFStepRun{{Agent: worker.ID, Status: wfRunning, StartedAt: &started}}}}},
		{ID: "r3", WorkflowName: "Release", Status: wfWaiting, StartedAt: started, Stages: []WFStageRun{
			{Name: "build", Status: wfSucceeded, Steps: []WFStepRun{{Agent: worker.ID, Status: wfFailed, StartedAt: &started}}},
			{Name: "ok", Gate: true, Status: wfWaiting, Steps: []WFStepRun{{Agent: gated.ID, Status: wfPending}}}}},
		// Newer but finished: not the mission. An agent turned off is off, whatever an old run says.
		{ID: "r4", WorkflowName: "Done", Status: wfSucceeded, StartedAt: started, Stages: []WFStageRun{{Name: "a", Status: wfSucceeded, Steps: []WFStepRun{{Agent: off.ID, Status: wfRunning, StartedAt: &started}}}}},
	}
	for _, r := range runs {
		m.srv.docs.Put(t.Context(), kindWorkflowRun, r.ID, r)
	}
	tj, by := teamOf(t, m.dev)
	if w := by["Worker"]; w.State != teamStateWorking || w.Doing != "Clean tables" || w.RunID != "r2" || w.StepsWeek != 2 || w.FailedWeek != 1 || w.LastAt == nil {
		t.Errorf("worker: %+v", w)
	}
	if g := by["Gated"]; g.State != teamStateWaiting || g.Doing != "Release" {
		t.Errorf("gated: %+v", g)
	}
	if s := by["Sleeper"]; s.State != teamStateScheduled || s.EveryMin != 60 || s.NextAt == nil || s.NextAt.Sub(last.Add(time.Hour)).Abs() > time.Second || len(s.Workflows) != 1 {
		t.Errorf("sleeper: %+v", s)
	}
	if o := by["Off"]; o.State != teamStateOff || o.Doing != "" {
		t.Errorf("off: %+v", o)
	}
	if i := by["Idle"]; i.State != teamStateIdle || i.NextAt != nil {
		t.Errorf("idle: %+v", i)
	}
	// The mission is the newest run still going.
	if tj.Mission == nil || tj.Mission.ID != "r3" || len(tj.Mission.Stages) != 2 {
		t.Errorf("mission: %+v", tj.Mission)
	}
	// With the assistant off nothing runs on a schedule, and the team says so instead of promising a time.
	m.admin.expect(200, "PUT", "/v1/integrations/assistant", map[string]any{"backend": "off"})
	tj, by = teamOf(t, m.dev)
	if !tj.SchedulesStop || by["Sleeper"].NextAt != nil || by["Sleeper"].State != teamStateIdle {
		t.Errorf("assistant off: %+v %+v", tj.SchedulesStop, by["Sleeper"])
	}
}
