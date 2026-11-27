package server

import (
	"encoding/json"
	"strings"
	"testing"
)

func runWorkflowCard(id string) string {
	return lookupReply("prepare_action", map[string]any{"action": "run_workflow", "workflow": id, "title": "Run it"})
}

// "Run it": the assistant prepares a workflow run as a card. Nothing starts until the person confirms, the run is theirs,
// and it is the version they were shown.
func TestARunWorkflowCardStartsTheShownVersionOnlyOnConfirm(t *testing.T) {
	m := newMCPHarness(t)
	m.turnOn()
	a := newAgent(t, m.admin, agentBody("Worker", nil, nil, "T0"))
	w := newWorkflow(t, m.admin, wfBody("Weekly report", []WFStage{{Name: "Look", Steps: []WFStep{step(a.ID, "Summarise.")}}}))

	for name, reply := range map[string]string{"unknown": runWorkflowCard("nope"), "no id": runWorkflowCard("")} {
		if cards := actionsIn(m.run(m.dev, map[string]any{"message": "run it"}, reply)); len(cards) != 0 {
			t.Errorf("%s: a card was prepared: %v", name, cards)
		}
	}
	m.v.expect(403, "POST", "/v1/assistant/runs", map[string]any{"message": "run it"})

	m.llm.script("Done.") // what the workflow's agent will answer
	cards := actionsIn(m.run(m.dev, map[string]any{"message": "run it"}, runWorkflowCard(w.ID)))
	if len(cards) != 1 || cards[0]["kind"] != "run_workflow" {
		t.Fatalf("cards = %v", cards)
	}
	id := cards[0]["id"].(string)
	fx, _ := json.Marshal(cards[0]["effects"])
	if !strings.Contains(string(fx), "Weekly report") || !strings.Contains(string(fx), "version 1, 1 stage") || !strings.Contains(string(fx), "different administrator") {
		t.Errorf("the card does not say what it does: %s", fx)
	}
	var runs struct {
		Runs []WFRun `json:"runs"`
	}
	json.Unmarshal(m.dev.expect(200, "GET", "/v1/ai/workflow-runs", nil), &runs)
	if len(runs.Runs) != 0 {
		t.Fatal("a run started before the person confirmed")
	}
	m.admin.expect(404, "POST", "/v1/assistant/actions/"+id+"/confirm", nil) // not theirs

	m.llm.script("The summary.")
	var a2 AssistAction
	json.Unmarshal(m.dev.expect(200, "POST", "/v1/assistant/actions/"+id+"/confirm", nil), &a2)
	if a2.Status != actDone || !strings.HasPrefix(a2.Link, "/workflow-runs/") {
		t.Fatalf("action = %+v", a2)
	}
	run := m.waitRun(strings.TrimPrefix(a2.Link, "/workflow-runs/"), "done", "succeeded", "failed")
	if run.Workflow != w.ID || run.By != "dev" || run.Trigger != "manual" || run.Version != 1 {
		t.Errorf("run = %+v", run)
	}
	m.dev.expect(409, "POST", "/v1/assistant/actions/"+id+"/confirm", nil)
	var view RunViewJSON
	json.Unmarshal(m.dev.expect(200, "GET", "/v1/assistant/actions/"+id+"/run", nil), &view)
	if view.Status != "succeeded" || view.Title != "Weekly report" || view.Output != "The summary." || len(view.Steps) != 1 || view.Steps[0] != (RunViewStep{Name: "Look", Status: "succeeded"}) {
		t.Errorf("view = %+v", view)
	}
	// A graph that stops at a question says what it waits for.
	g := newWorkflow(t, m.admin, graphBody("Asks first", []WFNode{{ID: "start", Kind: nodeStart}, {ID: "ok", Kind: nodeAsk, Question: "Go ahead?"}, {ID: "end", Kind: nodeEnd}},
		[]WFEdge{{From: "start", To: "ok"}, {From: "ok", To: "end"}}))
	asks := actionsIn(m.run(m.dev, map[string]any{"message": "run that"}, runWorkflowCard(g.ID)))[0]["id"].(string)
	var started AssistAction
	json.Unmarshal(m.dev.expect(200, "POST", "/v1/assistant/actions/"+asks+"/confirm", nil), &started)
	m.waitRun(strings.TrimPrefix(started.Link, "/workflow-runs/"), wfWaiting)
	view = RunViewJSON{}
	json.Unmarshal(m.dev.expect(200, "GET", "/v1/assistant/actions/"+asks+"/run", nil), &view)
	if view.Status != wfWaiting || view.Waiting != "Go ahead?" {
		t.Errorf("waiting view = %+v", view)
	}
	if aud := string(m.admin.expect(200, "GET", "/v1/audit?limit=60", nil)); !strings.Contains(aud, "confirmed by the person") || !strings.Contains(aud, "ai.workflow.run") {
		t.Error("the run is not in the audit log as confirmed by the person")
	}

	// A workflow that changes after the card was prepared is not started from that card.
	cards = actionsIn(m.run(m.dev, map[string]any{"message": "run it again"}, runWorkflowCard(w.ID)))
	id = cards[0]["id"].(string)
	body := wfBody("Weekly report", []WFStage{{Name: "Look", Steps: []WFStep{step(a.ID, "Summarise differently.")}}})
	m.admin.expect(201, "POST", "/v1/ai/workflows/"+w.ID+"/versions", body)
	if out := string(m.dev.expect(409, "POST", "/v1/assistant/actions/"+id+"/confirm", nil)); !strings.Contains(out, "changed since this card was prepared") {
		t.Errorf("a changed workflow was started: %s", out)
	}

	// A turned-off workflow gets no card.
	m.admin.expect(200, "POST", "/v1/ai/workflows/"+w.ID+"/disable", nil)
	if cards := actionsIn(m.run(m.dev, map[string]any{"message": "run it"}, runWorkflowCard(w.ID))); len(cards) != 0 {
		t.Errorf("a card for a turned-off workflow: %v", cards)
	}
}

// What is held back can change while the person talks, so the assistant is told to look again before saying so.
func TestTheAssistantIsToldToRecheckBeforeSayingSomethingIsHeldBack(t *testing.T) {
	m := newMCPHarness(t)
	m.run(m.dev, map[string]any{"message": "hello"})
	if sys := m.lastSystem(); !strings.Contains(sys, "never say something is held back from an earlier answer or from memory") {
		t.Errorf("the prompt does not tell it to re-check:\n%s", sys)
	}
}
