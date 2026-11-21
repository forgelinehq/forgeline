package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// Working like a colleague (design note 052, slice 1): the assistant can ask the person a question and wait, and
// keep a visible plan. Both exist only where a person is watching the run (the console's chat stream); an agent run,
// an MCP client or a plain request has nobody to ask, so the tools are not offered there.

const (
	askUserTool    = "ask_user"
	updatePlanTool = "update_plan"
	maxQuestionLen = 400
	maxOptionLen   = 80
	maxOptions     = 4
	maxAnswerLen   = 1000
	maxPlanItems   = 12
	maxPlanText    = 120
)

// The wall time one question may take (not counting the time spent waiting for the person); and how long it waits for an answer.
var assistantWall = 5 * time.Minute

// How long the assistant waits for an answer; time spent waiting does not count against its budget.
var questionTimeout = 10 * time.Minute

type asker func(question string, options []string) (string, error)
type askerKey struct{}

// askerFrom is the way to ask the person a question, or nil where nobody is present. An agent never has one.
func askerFrom(ctx context.Context) asker {
	if scopeFrom(ctx) != nil {
		return nil
	}
	a, _ := ctx.Value(askerKey{}).(asker)
	return a
}

// A question is shown and waited for through the same book as approvals, as a different kind.
func (s *Server) askPerson(ctx context.Context, p Principal, question string, options []string, shown func(id string, wait time.Duration), answered func(id, answer string)) (string, error) {
	pa := &pendingApproval{who: p.Name, tool: askUserTool, kind: "question", answer: make(chan string, 1)}
	id := s.approvals.add(pa)
	defer s.approvals.remove(id)
	shown(id, questionTimeout)
	t := time.NewTimer(questionTimeout)
	defer t.Stop()
	select {
	case a := <-pa.answer:
		answered(id, a)
		return a, nil
	case <-t.C:
		answered(id, "")
		return "", errors.New("nobody answered in time")
	case <-ctx.Done():
		return "", errors.New("the person left before answering")
	}
}

func (s *Server) handleAssistantQuestion(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Answer string `json:"answer"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	req.Answer = strings.TrimSpace(req.Answer)
	if req.Answer == "" || len(req.Answer) > maxAnswerLen {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("answer: 1 to %d characters", maxAnswerLen))
		return
	}
	if !s.approvals.resolveKind(r.PathValue("id"), principalFrom(r.Context()).Name, "question", req.Answer) {
		writeError(w, http.StatusNotFound, "no question of yours is waiting for that answer (it may have expired)")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"answer": req.Answer})
}

// lookupAskUser stops the run on a question and returns the answer to the model as data.
func (s *Server) lookupAskUser(ctx context.Context, raw json.RawMessage) (string, error) {
	var a struct {
		Question string   `json:"question"`
		Options  []string `json:"options"`
	}
	if err := decodeArgs(raw, &a); err != nil {
		return "", err
	}
	a.Question = strings.TrimSpace(a.Question)
	if a.Question == "" || len(a.Question) > maxQuestionLen {
		return "", fmt.Errorf("question: 1 to %d characters", maxQuestionLen)
	}
	if len(a.Options) > maxOptions {
		return "", fmt.Errorf("options: at most %d (the person can always type their own answer)", maxOptions)
	}
	for i, o := range a.Options {
		a.Options[i] = strings.TrimSpace(o)
		if a.Options[i] == "" || len(a.Options[i]) > maxOptionLen {
			return "", fmt.Errorf("options[%d]: 1 to %d characters", i, maxOptionLen)
		}
	}
	ask := askerFrom(ctx)
	if ask == nil {
		return "", errors.New("nobody is here to answer; make the most reasonable assumption, say that you assumed it, and carry on")
	}
	ans, err := ask(a.Question, a.Options)
	if err != nil {
		return "", fmt.Errorf("%v; carry on with the most reasonable assumption and say what you assumed, or stop and say what you need", err)
	}
	return "The person answered: " + ans, nil
}

type planItem struct {
	Text   string `json:"text"`
	Status string `json:"status"` // todo, doing or done
}

// lookupUpdatePlan replaces the visible to-do list. The model owns it; the person watches it.
func (s *Server) lookupUpdatePlan(ctx context.Context, raw json.RawMessage) (string, error) {
	var a struct {
		Items []planItem `json:"items"`
	}
	if err := decodeArgs(raw, &a); err != nil {
		return "", err
	}
	if len(a.Items) == 0 || len(a.Items) > maxPlanItems {
		return "", fmt.Errorf("items: 1 to %d", maxPlanItems)
	}
	for i := range a.Items {
		a.Items[i].Text = strings.TrimSpace(a.Items[i].Text)
		if a.Items[i].Text == "" || len(a.Items[i].Text) > maxPlanText {
			return "", fmt.Errorf("items[%d].text: 1 to %d characters", i, maxPlanText)
		}
		if st := a.Items[i].Status; st != "todo" && st != "doing" && st != "done" {
			return "", fmt.Errorf("items[%d].status: todo, doing or done", i)
		}
	}
	if obs := lookupObserver(ctx); obs != nil {
		b, _ := json.Marshal(a.Items)
		obs(assistEvent{Kind: evPlan, Content: string(b)})
	}
	return "The plan is updated and the person can see it. Carry on with the step that is doing.", nil
}

// workGuidance is how the assistant is told to behave (note 052): plan, find out before asking, ask when the answer
// changes the work, and try another way before saying it cannot.
func workGuidance(ctx context.Context) string {
	if scopeFrom(ctx) != nil {
		return ""
	}
	var b strings.Builder
	b.WriteString("\n\nHow to work: act like a careful colleague, not a menu. Find things out with lookups before you ask. When a lookup is cut off, fails or finds nothing, try another way (a narrower filter, another lookup) before you say you cannot, and say what you could not check.")
	if lookupObserver(ctx) != nil {
		b.WriteString(" For anything that takes several steps, call " + updatePlanTool + " first and keep it current as you go; it costs nothing.")
	}
	if askerFrom(ctx) != nil {
		b.WriteString(" When the answer changes what you would do and you cannot find it out (two clusters match, which account, which time range), call " + askUserTool + " with two to four options instead of guessing. Never ask permission to read.")
	}
	if a := autopilotFrom(ctx); a != nil && a.active() {
		b.WriteString(autopilotGuidance(a))
	}
	return b.String()
}
