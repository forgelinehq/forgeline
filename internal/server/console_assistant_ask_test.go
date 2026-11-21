package server

import (
	"context"
	"strings"
	"testing"
	"time"
)

// streamScript is stream with the model's whole script given.
func (m *mcpHarness) streamScript(c *browser, replies ...string) *streamed {
	m.llm.mu.Lock()
	m.llm.replies, m.llm.reqs = replies, nil
	m.llm.mu.Unlock()
	st := &streamed{done: make(chan struct{})}
	go func() {
		defer close(st.done)
		resp, body := c.do("POST", "/v1/assistant/runs", map[string]any{"message": "look at Dev01"})
		st.code, st.body = resp.StatusCode, body
	}()
	return st
}

func eventsNamed(events []map[string]any, name string) []map[string]any {
	var out []map[string]any
	for _, f := range events {
		if f["type"] == "CUSTOM" && f["name"] == name {
			out = append(out, f["value"].(map[string]any))
		}
	}
	return out
}

// The assistant stops on a question, shows it with its options, waits, and carries on with the answer; only the
// person asked can answer, and an approval cannot be answered as a question or the other way round.
func TestAssistantStopsToAskAndContinuesWithTheAnswer(t *testing.T) {
	m := newMCPHarness(t)
	st := m.stream(m.dev, lookupReply(askUserTool, map[string]any{"question": "Dev01 matches two ECS clusters. Which one?", "options": []string{"Cluster-bpdev01", "Snap-App-dev01"}}))
	id := m.waitPending(t)
	m.admin.expect(404, "POST", "/v1/assistant/questions/"+id, map[string]any{"answer": "Cluster-bpdev01"})                 // somebody else's
	m.v.expect(403, "POST", "/v1/assistant/questions/"+id, map[string]any{"answer": "Cluster-bpdev01"})                     // a viewer cannot reach it
	m.dev.expect(400, "POST", "/v1/assistant/questions/"+id, map[string]any{"answer": "  "})                                // nothing
	m.dev.expect(400, "POST", "/v1/assistant/questions/"+id, map[string]any{"answer": strings.Repeat("x", maxAnswerLen+1)}) // too long
	m.dev.expect(404, "POST", "/v1/assistant/approvals/"+id, map[string]any{"decision": "approve"})                         // a question is not an approval
	m.dev.expect(404, "POST", "/v1/assistant/questions/not-a-real-id", map[string]any{"answer": "x"})
	m.dev.expect(200, "POST", "/v1/assistant/questions/"+id, map[string]any{"answer": "Cluster-bpdev01"})
	m.dev.expect(404, "POST", "/v1/assistant/questions/"+id, map[string]any{"answer": "Cluster-bpdev01"}) // an answer counts once

	events, res := answerOf(t, st)
	if res == nil || !strings.Contains(res["content"].(string), "The person answered: Cluster-bpdev01") {
		t.Fatalf("the answer did not come back to the model: %v", res)
	}
	q := eventsNamed(events, "forgeline.question")
	a := eventsNamed(events, "forgeline.question_answered")
	if len(q) != 1 || q[0]["id"] != id || q[0]["question"] != "Dev01 matches two ECS clusters. Which one?" || len(q[0]["options"].([]any)) != 2 || q[0]["expiresInSeconds"].(float64) < 60 {
		t.Errorf("question card: %v", q)
	}
	if len(a) != 1 || a[0]["answer"] != "Cluster-bpdev01" {
		t.Errorf("answered: %v", a)
	}
	// The model is told how to work, and that it can ask and plan, only where a person is watching.
	if sys := m.llm.system(0); !strings.Contains(sys, askUserTool) || !strings.Contains(sys, updatePlanTool) || !strings.Contains(sys, "Never ask permission to read") {
		t.Errorf("stream prompt: %s", sys)
	}
	m.ask(m.dev, "hello")
	if sys := m.llm.system(0); strings.Contains(sys, askUserTool) || strings.Contains(sys, updatePlanTool) {
		t.Errorf("a plain request is offered tools that need someone watching: %s", sys)
	}
}

func TestAskUserChecksItsArgumentsAndNeedsAPerson(t *testing.T) {
	m := newMCPHarness(t)
	for _, c := range []struct {
		args map[string]any
		want string
	}{
		{map[string]any{"question": ""}, "question: 1 to"},
		{map[string]any{"question": strings.Repeat("q", maxQuestionLen+1)}, "question: 1 to"},
		{map[string]any{"question": "Which?", "options": []string{"a", "b", "c", "d", "e"}}, "options: at most 4"},
		{map[string]any{"question": "Which?", "options": []string{""}}, "options[0]: 1 to"},
		{map[string]any{"question": "Which?", "options": []string{strings.Repeat("o", maxOptionLen+1)}}, "options[0]: 1 to"},
	} {
		m.ask(m.dev, lookupReply(askUserTool, c.args))
		if res := m.llm.lastUser(1); !strings.Contains(res, c.want) {
			t.Errorf("%v: want %q in %s", c.args, c.want, res)
		}
	}
	// A plain request has nobody to answer, so the model is told to assume and carry on rather than hang.
	m.ask(m.dev, lookupReply(askUserTool, map[string]any{"question": "Which one?"}))
	if res := m.llm.lastUser(1); !strings.Contains(res, "nobody is here to answer") {
		t.Errorf("no person present: %s", res)
	}
	// An agent never has a person to ask, even when started from a chat.
	a := newAgent(t, m.admin, agentBody("Asker", nil, []string{askUserTool}, "T0"))
	m.llm.script(lookupReply(askUserTool, map[string]any{"question": "Which one?"}), "done")
	m.dev.expect(200, "POST", "/v1/ai/agents/"+a.ID+"/chat", map[string]any{"message": "go"})
	if res := m.llm.lastUser(1); strings.Contains(res, "The person answered") {
		t.Errorf("an agent asked a person: %s", res)
	}
}

// The plan is shown as a checklist and replaces the previous one; it is checked and costs nothing.
func TestAssistantPlanIsShownAndChecked(t *testing.T) {
	m := newMCPHarness(t)
	plan := []map[string]string{{"text": "Find the Dev01 clusters", "status": "done"}, {"text": "Read their findings", "status": "doing"}, {"text": "Write the report", "status": "todo"}}
	st := m.stream(m.dev, lookupReply(updatePlanTool, map[string]any{"items": plan}))
	events, _ := answerOf(t, st)
	got := eventsNamed(events, "forgeline.plan")
	if len(got) != 1 {
		t.Fatalf("plan events: %v", got)
	}
	items := got[0]["items"].([]any)
	if len(items) != 3 || items[1].(map[string]any)["status"] != "doing" {
		t.Errorf("plan: %v", items)
	}
	for _, bad := range []map[string]any{
		{"items": []map[string]string{}},
		{"items": []map[string]string{{"text": "x", "status": "finished"}}},
		{"items": []map[string]string{{"text": "", "status": "todo"}}},
		{"items": []map[string]string{{"text": strings.Repeat("t", maxPlanText+1), "status": "todo"}}},
	} {
		st := m.stream(m.dev, lookupReply(updatePlanTool, bad))
		events, res := answerOf(t, st)
		if len(eventsNamed(events, "forgeline.plan")) != 0 || res == nil || !strings.Contains(res["content"].(string), "The lookup failed") {
			t.Errorf("%v was accepted: %v", bad, res)
		}
	}
	many := make([]map[string]string, maxPlanItems+1)
	for i := range many {
		many[i] = map[string]string{"text": "step", "status": "todo"}
	}
	st = m.stream(m.dev, lookupReply(updatePlanTool, map[string]any{"items": many}))
	if events, _ := answerOf(t, st); len(eventsNamed(events, "forgeline.plan")) != 0 {
		t.Error("a plan over the limit was shown")
	}
}

// The wall-time budget stops a long question with a summary, and the time spent waiting for the person is not
// counted against it.
func TestAssistantTimeBudgetIgnoresTimeSpentWaitingForThePerson(t *testing.T) {
	old, oldQ := assistantWall, questionTimeout
	defer func() { assistantWall, questionTimeout = old, oldQ }()
	m := newMCPHarness(t)

	assistantWall = 200 * time.Millisecond
	st := m.streamScript(m.dev, lookupReply(askUserTool, map[string]any{"question": "Which?"}), lookupReply("list_runs", map[string]any{}), "Done.")
	id := m.waitPending(t)
	time.Sleep(400 * time.Millisecond) // longer than the budget, spent waiting for the person
	m.dev.expect(200, "POST", "/v1/assistant/questions/"+id, map[string]any{"answer": "the first"})
	events, _ := answerOf(t, st)
	ran := 0
	for _, f := range events {
		if f["type"] == "TOOL_CALL_START" && f["toolCallName"] == "list_runs" {
			ran++
		}
	}
	if ran != 1 {
		t.Errorf("the lookup after the answer was refused although the time was spent waiting: ran %d", ran)
	}

	assistantWall = -time.Second // used up before it starts
	st = m.streamScript(m.dev, lookupReply("list_runs", map[string]any{}), "I found nothing yet; say continue.")
	events, _ = answerOf(t, st)
	for _, f := range events {
		if f["type"] == "TOOL_CALL_START" {
			t.Errorf("a lookup ran after the time budget: %v", f)
		}
	}
	if sys := m.llm.lastUser(1); !strings.Contains(sys, `"continue"`) {
		t.Errorf("the model was not asked to summarise: %s", sys)
	}

	// Nobody answering is not a hang: the model is told and carries on.
	assistantWall, questionTimeout = 5*time.Minute, 150*time.Millisecond
	st = m.streamScript(m.dev, lookupReply(askUserTool, map[string]any{"question": "Which?"}), "I assumed the first.")
	_, res := answerOf(t, st)
	if res == nil || !strings.Contains(res["content"].(string), "nobody answered in time") {
		t.Errorf("timeout result: %v", res)
	}
}

func TestResolvingAQuestionAndAnApprovalAreDifferent(t *testing.T) {
	var b approvalBook
	q := &pendingApproval{who: "dev", kind: "question", answer: make(chan string, 1)}
	a := &pendingApproval{who: "dev", answer: make(chan string, 1)}
	qid, aid := b.add(q), b.add(a)
	if b.resolve(qid, "dev", "approve") {
		t.Error("a question was answered as an approval")
	}
	if b.resolveKind(aid, "dev", "question", "yes") {
		t.Error("an approval was answered as a question")
	}
	if !b.resolveKind(qid, "dev", "question", "the first") || <-q.answer != "the first" {
		t.Error("a question was not answered")
	}
	if !b.resolve(aid, "dev", "approve") || <-a.answer != "approve" {
		t.Error("an approval was not answered")
	}
}

// An agent run is never given a person to ask, even when the context carries one (started from a chat).
func TestAnAgentScopeNeverHasAPersonToAsk(t *testing.T) {
	ctx := context.WithValue(context.Background(), askerKey{}, asker(func(string, []string) (string, error) { return "yes", nil }))
	if askerFrom(ctx) == nil {
		t.Fatal("a chat has no asker")
	}
	scoped := context.WithValue(ctx, agentScopeKey{}, &agentScope{tools: map[string]bool{askUserTool: true}})
	if askerFrom(scoped) != nil {
		t.Error("an agent could ask the person")
	}
}

// A plan update, like a question or a card, still works after the step budget is used up, so a long job can show where
// it got to.
func TestPlanUpdatesStayFreeAfterTheStepBudget(t *testing.T) {
	m := newMCPHarness(t)
	replies := []string{}
	for i := 0; i < maxLookups; i++ {
		replies = append(replies, lookupReply("list_runs", map[string]any{}))
	}
	replies = append(replies, lookupReply(updatePlanTool, map[string]any{"items": []map[string]string{{"text": "Summarise", "status": "doing"}}}), "Where I got to.")
	st := m.streamScript(m.dev, replies...)
	if events, _ := answerOf(t, st); len(eventsNamed(events, "forgeline.plan")) != 1 {
		t.Error("the plan update after the budget did not run")
	}
}
