package server

import (
	"context"
	"encoding/json"
	"github.com/forgelinehq/forgeline/internal/authn"
	"strings"
	"testing"
	"time"
)

// actionOf returns the action cards in a streamed answer.
func actionsIn(evs []map[string]any) []map[string]any {
	var out []map[string]any
	for _, f := range evs {
		if f["type"] == "CUSTOM" && f["name"] == "forgeline.action" {
			out = append(out, f["value"].(map[string]any))
		}
	}
	return out
}

func prepareSave(yaml string) string {
	return lookupReply("prepare_action", map[string]string{"action": "save_pipeline", "title": "Save demo", "yaml": yaml})
}

func TestAnActionHappensOnlyWhenThePersonConfirms(t *testing.T) {
	m := newMCPHarness(t)
	evs := m.run(m.dev, map[string]any{"message": "make me a demo pipeline"}, prepareSave(twoSteps))
	cards := actionsIn(evs)
	if len(cards) != 1 {
		t.Fatalf("cards = %v", cards)
	}
	id := cards[0]["id"].(string)
	// The card's effects come from the file, not from the model.
	fx, _ := json.Marshal(cards[0]["effects"])
	if !strings.Contains(string(fx), "Saves a new pipeline") || !strings.Contains(string(fx), "image alpine") {
		t.Errorf("effects = %s", fx)
	}
	// Nothing exists yet.
	m.dev.expect(404, "GET", "/v1/pipelines/demo", nil)
	if !hasAudit(t, m.admin, "assistant.action.prepare") {
		t.Error("preparing is not audited")
	}

	// Someone else, an administrator included, cannot confirm or dismiss it.
	m.admin.expect(404, "POST", "/v1/assistant/actions/"+id+"/confirm", nil)
	m.admin.expect(404, "POST", "/v1/assistant/actions/"+id+"/dismiss", nil)
	m.dev.expect(404, "GET", "/v1/pipelines/demo", nil)

	// The owner confirms: it is saved as them, and it cannot be confirmed twice.
	var a AssistAction
	json.Unmarshal(m.dev.expect(200, "POST", "/v1/assistant/actions/"+id+"/confirm", nil), &a)
	if a.Status != actDone || a.Link != "/pipelines/demo" {
		t.Fatalf("action = %+v", a)
	}
	var sp SavedPipeline
	json.Unmarshal(m.dev.expect(200, "GET", "/v1/pipelines/demo", nil), &sp)
	if sp.CreatedBy == "" || sp.Version != 1 {
		t.Errorf("saved = %+v", sp)
	}
	m.dev.expect(409, "POST", "/v1/assistant/actions/"+id+"/confirm", nil)
	if !hasAudit(t, m.admin, "assistant.action.confirm") {
		t.Error("confirming is not audited")
	}
}

func TestADismissedOrExpiredActionCannotBeConfirmed(t *testing.T) {
	m := newMCPHarness(t)
	id := actionsIn(m.run(m.dev, map[string]any{"message": "a"}, prepareSave(twoSteps)))[0]["id"].(string)
	m.dev.expect(200, "POST", "/v1/assistant/actions/"+id+"/dismiss", nil)
	m.dev.expect(409, "POST", "/v1/assistant/actions/"+id+"/confirm", nil)
	m.dev.expect(404, "GET", "/v1/pipelines/demo", nil)

	// Expired.
	id = actionsIn(m.run(m.dev, map[string]any{"message": "b"}, prepareSave(twoSteps)))[0]["id"].(string)
	var a AssistAction
	if err := m.srv.docs.Get(t.Context(), kindAssistAction, id, &a); err != nil {
		t.Fatal(err)
	}
	a.Expires = time.Now().Add(-time.Minute)
	m.srv.docs.Put(t.Context(), kindAssistAction, id, a)
	m.dev.expect(409, "POST", "/v1/assistant/actions/"+id+"/confirm", nil)
	m.dev.expect(404, "GET", "/v1/pipelines/demo", nil)
}

func TestAnInvalidOrDuplicatePipelineIsNotOfferedAsAnAction(t *testing.T) {
	m := newMCPHarness(t)
	if cards := actionsIn(m.run(m.dev, map[string]any{"message": "a"}, prepareSave("name: [unclosed"))); len(cards) != 0 {
		t.Errorf("an invalid file got a card: %v", cards)
	}
	if !strings.Contains(m.llm.lastUser(1), "not valid") {
		t.Errorf("the model was not told: %s", m.llm.lastUser(1))
	}
	m.dev.expect(201, "POST", "/v1/pipelines", map[string]any{"yaml": twoSteps})
	if cards := actionsIn(m.run(m.dev, map[string]any{"message": "b"}, prepareSave(twoSteps))); len(cards) != 0 {
		t.Errorf("a duplicate got a card: %v", cards)
	}
	if !strings.Contains(m.llm.lastUser(1), "already exists") {
		t.Errorf("the model was not told: %s", m.llm.lastUser(1))
	}
}

func TestAConfirmedRunStartsARunOfTheSavedPipeline(t *testing.T) {
	m := newMCPHarness(t)
	m.dev.expect(201, "POST", "/v1/pipelines", map[string]any{"yaml": twoSteps})
	cards := actionsIn(m.run(m.dev, map[string]any{"message": "run it"}, lookupReply("prepare_action", map[string]string{"action": "run_pipeline", "pipeline": "demo"})))
	if len(cards) != 1 {
		t.Fatalf("cards = %v", cards)
	}
	var a AssistAction
	json.Unmarshal(m.dev.expect(200, "POST", "/v1/assistant/actions/"+cards[0]["id"].(string)+"/confirm", nil), &a)
	if !strings.HasPrefix(a.Link, "/runs/") {
		t.Fatalf("action = %+v", a)
	}
	m.dev.expect(200, "GET", "/v1/"+strings.TrimPrefix(a.Link, "/"), nil)
	// A pipeline that is not saved is refused at once.
	if cards := actionsIn(m.run(m.dev, map[string]any{"message": "run x"}, lookupReply("prepare_action", map[string]string{"action": "run_pipeline", "pipeline": "nope"}))); len(cards) != 0 {
		t.Errorf("a missing pipeline got a card: %v", cards)
	}
}

func TestPrepareActionIsOnlyForADeveloperChattingWithTheAssistant(t *testing.T) {
	m := newMCPHarness(t)
	// A viewer cannot use the assistant stream at all.
	m.v.expect(403, "POST", "/v1/assistant/runs", map[string]any{"message": "a"})
	// The plain /chat call has no person to confirm, so the tool is not offered or run.
	m.llm.mu.Lock()
	m.llm.replies, m.llm.reqs = []string{prepareSave(twoSteps), "ok"}, nil
	m.llm.mu.Unlock()
	m.dev.expect(200, "POST", "/v1/assistant/chat", map[string]any{"message": "make one"})
	if !strings.Contains(m.llm.lastUser(1), "only available when a person is chatting") {
		t.Errorf("plain chat ran the tool: %s", m.llm.lastUser(1))
	}
	if strings.Contains(m.lastSystem(), "prepare_action") {
		t.Error("plain chat is offered prepare_action")
	}
	m.dev.expect(404, "GET", "/v1/pipelines/demo", nil)
}

func TestTooManyWaitingActionsAreRefused(t *testing.T) {
	m := newMCPHarness(t)
	for i := 0; i < maxPendingAction; i++ {
		y := strings.Replace(twoSteps, "name: demo", "name: demo"+string(rune('a'+i)), 1)
		if len(actionsIn(m.run(m.dev, map[string]any{"message": "x"}, prepareSave(y)))) != 1 {
			t.Fatalf("action %d was refused", i)
		}
	}
	if cards := actionsIn(m.run(m.dev, map[string]any{"message": "x"}, prepareSave(strings.Replace(twoSteps, "name: demo", "name: extra", 1)))); len(cards) != 0 {
		t.Errorf("an eleventh waiting action was filed: %v", cards)
	}
}

func TestAnAgentCannotPrepareAnAction(t *testing.T) {
	m := newMCPHarness(t)
	raw, _ := json.Marshal(map[string]string{"action": "save_pipeline", "yaml": twoSteps})
	// Even if a person's turn is somehow in the context, a running agent has no one to press Confirm.
	ctx := context.WithValue(t.Context(), agentScopeKey{}, &agentScope{tools: map[string]bool{prepareTool: true}})
	ctx = context.WithValue(ctx, memTurnKey{}, &memTurn{p: Principal{Name: "dev", Role: authn.Developer, UserID: "u1"}})
	if _, err := m.srv.lookupPrepareAction(ctx, raw); err == nil {
		t.Error("an agent prepared an action")
	}
}

func TestAReplyThatMixesTextWithALookupIsSentBackOnceAndNeverRunWithTheText(t *testing.T) {
	m := newMCPHarness(t)
	m.dev.expect(201, "POST", "/v1/pipelines", map[string]any{"yaml": twoSteps})
	mixed := "I'll check the pipelines first.\n\n" + lookupReply("list_pipelines", map[string]string{})
	evs := m.run(m.dev, map[string]any{"message": "run demo"}, mixed, lookupReply("prepare_action", map[string]string{"action": "run_pipeline", "pipeline": "demo"}))
	// The mixed reply did not run anything itself; the model was told, and its next reply (a clean lookup) did.
	if len(actionsIn(evs)) != 1 {
		t.Fatalf("no card after the nudge: %v", evs)
	}
	if !strings.Contains(m.llm.lastUser(2), "mixed text with a lookup block") && !strings.Contains(m.llm.lastUser(1), "mixed text with a lookup block") {
		t.Error("the model was not told why nothing ran")
	}
	if tools := toolsRun(evs); len(tools) != 1 || tools[0] != "prepare_action" {
		t.Errorf("tools run = %v (the lookup inside the mixed reply must not run)", tools)
	}
}

func toolsRun(evs []map[string]any) []string {
	var out []string
	for _, f := range evs {
		if f["type"] == "TOOL_CALL_START" {
			out = append(out, f["toolCallName"].(string))
		}
	}
	return out
}
