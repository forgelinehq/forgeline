package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path"
	"strings"
	"sync"
	"unicode"
)

// One approval for a plan (design note 053). A job that needs several commands would otherwise stop on a card for each. The
// agent may list the exact commands and file writes first; the person reads them all and answers once. What they approved
// then runs without another card, each as often as it was listed, only in the reply that asked. Anything that differs by a
// character, or is not on the list, asks as before. Only the engine's own Bash, Write and Edit in the sandbox can be on a
// plan: AWS changes, changes to Forgeline, fetches and secrets keep their own cards.

const (
	approvePlanTool   = "approve_plan"
	maxPlanActions    = 20
	maxPlanCommand    = 4000
	maxPlanActionText = 8000
	maxPlanTotalText  = 40000
)

// planAction is one step of a plan, and also the shape a tool call is compared in.
type planAction struct {
	Tool       string `json:"tool"`
	Command    string `json:"command,omitempty"`
	Path       string `json:"file_path,omitempty"`
	Content    string `json:"content,omitempty"`
	Old        string `json:"old_string,omitempty"`
	New        string `json:"new_string,omitempty"`
	ReplaceAll bool   `json:"replace_all,omitempty"`
}

// planActionOf reads a tool call into the part a plan approves. Every argument that changes what happens must be in it, so a
// call with any other argument (a command in the background, say) is not something a plan can cover. A command's
// description and time limit change nothing it does.
func planActionOf(tool string, in map[string]any) (planAction, error) {
	a := planAction{Tool: tool}
	var fields map[string]*string
	switch tool {
	case "Bash":
		fields = map[string]*string{"command": &a.Command}
	case "Write":
		fields = map[string]*string{"file_path": &a.Path, "content": &a.Content}
	case "Edit":
		fields = map[string]*string{"file_path": &a.Path, "old_string": &a.Old, "new_string": &a.New}
	default:
		return a, fmt.Errorf("only Bash, Write and Edit can be on a plan, not %q", tool)
	}
	for name, v := range in {
		if p := fields[name]; p != nil {
			s, ok := v.(string)
			if !ok {
				return a, fmt.Errorf("%s: %s must be text", tool, name)
			}
			*p = s
			continue
		}
		switch {
		case tool == "Bash" && (name == "description" || name == "timeout"):
		case tool == "Edit" && name == "replace_all":
			b, ok := v.(bool)
			if !ok {
				return a, errors.New("Edit: replace_all must be true or false")
			}
			a.ReplaceAll = b
		case v == nil || v == false:
		default:
			return a, fmt.Errorf("%s: %s cannot be part of a plan", tool, name)
		}
	}
	if tool == "Bash" && strings.TrimSpace(a.Command) == "" {
		return a, errors.New("Bash: give the command")
	}
	if tool != "Bash" && (path.Clean(a.Path) != a.Path || !strings.HasPrefix(a.Path, "/work/") || a.Path == "/work/.agent" || strings.HasPrefix(a.Path, "/work/.agent/")) {
		return a, fmt.Errorf("%s: file_path is a full path under /work, outside /work/.agent", tool)
	}
	return a, nil
}

// hiddenRune is a character a card would not show for what it is: a control character, or one that reorders or hides the
// text around it.
func hiddenRune(r rune) bool {
	return r != '\n' && r != '\t' && (unicode.IsControl(r) || unicode.Is(unicode.Cf, r) || r == '\u2028' || r == '\u2029' || r == unicode.ReplacementChar)
}

// readPlan checks the list the agent sent. All of it has to fit on the card in full.
func readPlan(list []map[string]any) ([]planAction, error) {
	if len(list) == 0 || len(list) > maxPlanActions {
		return nil, fmt.Errorf("actions: 1 to %d", maxPlanActions)
	}
	out := make([]planAction, 0, len(list))
	total := 0
	for i, m := range list {
		tool, _ := m["tool"].(string)
		in := map[string]any{}
		for k, v := range m {
			if k != "tool" {
				in[k] = v
			}
		}
		a, err := planActionOf(tool, in)
		if err != nil {
			return nil, fmt.Errorf("actions[%d]: %v", i, err)
		}
		if len(a.Command) > maxPlanCommand || len(a.Content) > maxPlanActionText || len(a.Old) > maxPlanActionText || len(a.New) > maxPlanActionText {
			return nil, fmt.Errorf("actions[%d]: too long to show in full (a command up to %d characters, a text up to %d); leave it out and it asks by itself", i, maxPlanCommand, maxPlanActionText)
		}
		for _, s := range []string{a.Command, a.Path, a.Content, a.Old, a.New} {
			total += len(s)
			if strings.IndexFunc(s, hiddenRune) >= 0 {
				return nil, fmt.Errorf("actions[%d]: it holds a character the card cannot show; leave it out and it asks by itself", i)
			}
		}
		out = append(out, a)
	}
	if total > maxPlanTotalText {
		return nil, fmt.Errorf("the plan is over %d characters; split it", maxPlanTotalText)
	}
	return out, nil
}

// planGrant is what the person has approved in this reply and has not yet run.
type planGrant struct {
	mu   sync.Mutex
	left map[planAction]int
}

func (g *planGrant) add(actions []planAction) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.left == nil {
		g.left = map[planAction]int{}
	}
	for _, a := range actions {
		g.left[a]++
	}
}

// take uses up one approval of exactly this call, if there is one.
func (g *planGrant) take(tool string, in map[string]any) bool {
	a, err := planActionOf(tool, in)
	if err != nil {
		return false
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.left[a] == 0 {
		return false
	}
	g.left[a]--
	return true
}

// planOffered says whether a plan can be approved here: a person is watching, and the agent's own tools run in the sandbox.
func (s *Server) planOffered(ctx context.Context) bool {
	return permAskerFrom(ctx) != nil && s.loadAssistant(ctx).Backend == assistEngine && s.engineInContainer(ctx)
}

// lookupApprovePlan is the tool: it shows the whole plan and waits for one answer.
func (s *Server) lookupApprovePlan(ctx context.Context, raw json.RawMessage) (string, error) {
	var q struct {
		Title   string           `json:"title"`
		Actions []map[string]any `json:"actions"`
	}
	if err := decodeArgs(raw, &q); err != nil {
		return "", err
	}
	sink, _ := ctx.Value(lookupSinkKey{}).(*lookupSink)
	if sink == nil || !s.planOffered(ctx) {
		return "", errors.New("a plan can only be approved by a person in the chat, for Claude Code's own tools in the sandbox")
	}
	p := principalFrom(ctx)
	if liveAutopilot(ctx, p) != nil {
		return "Autopilot is on, so your commands and file writes run without asking. No plan is needed.", nil
	}
	actions, err := readPlan(q.Actions)
	if err != nil {
		return "", err
	}
	title := truncate(strings.Join(strings.Fields(q.Title), " "), 120)
	if title == "" || strings.IndexFunc(title, hiddenRune) >= 0 {
		return "", errors.New("title: say in a few words what the plan does")
	}
	summary := fmt.Sprintf("%s (%d actions)", title, len(actions))
	choice, err := permAskerFrom(ctx)(permRequest{Tool: approvePlanTool, Command: summary, Actions: actions,
		Why: "each of these runs once, exactly as shown, in this reply, without asking again. Anything else asks as usual"})
	if err != nil || choice != decisionOnce {
		_ = s.audit(ctx, p, "", "assistant.plan.approve", approvePlanTool, "refused: "+summary, false)
		if err != nil {
			return "", err
		}
		return "The person said no, so nothing on the plan is approved. Ask what they would like instead.", nil
	}
	sink.plan.add(actions)
	detail, _ := json.Marshal(actions)
	_ = s.audit(ctx, p, "", "assistant.plan.approve", approvePlanTool, summary+": "+truncate(string(detail), 600), true)
	return "Approved. Run these now with your own tools, each exactly as listed, character for character. Anything different asks the person again.", nil
}
