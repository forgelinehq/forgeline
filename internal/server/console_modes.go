package server

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
)

// How the assistant works on a message, as other coding agents offer it (design note 053): it asks before it changes
// anything; or file changes in the sandbox are accepted without asking; or it only plans, and nothing is changed until the
// person says to carry the plan out. Autopilot is the fourth way and has its own switch. Plan only is enforced here, at the
// places every tool call passes, so it does not depend on the model doing as it is told.

const (
	modeAsk   = ""
	modeEdits = "edits"
	modePlan  = "plan"
)

type modeKey struct{}

func modeFrom(ctx context.Context) string {
	m, _ := ctx.Value(modeKey{}).(string)
	return m
}

var errPlanOnly = errors.New("plan mode is on: nothing is changed. Look things up as you need to, then answer with the plan, step by step; the person chooses to have it carried out")

// changesInPlanMode says whether a tool call would change something and so does not run while only planning. What is not
// known to only look is refused: a plugin's tool, whatever it does.
func changesInPlanMode(c lookupCall) bool {
	var a struct {
		Command string   `json:"command"`
		Method  string   `json:"method"`
		Secrets []string `json:"secrets"`
	}
	_ = json.Unmarshal(c.Args, &a)
	switch {
	case strings.HasPrefix(c.Tool, pluginToolPrefix), c.Tool == approvePlanTool:
		return true
	case c.Tool == forgelineAPITool:
		return !strings.EqualFold(strings.TrimSpace(a.Method), "GET")
	case c.Tool == runShellTool:
		return len(a.Secrets) > 0 || !readOnlyCommand(a.Command)
	case c.Tool == memoryTool:
		return a.Command != "view" && a.Command != "search"
	}
	return changesSomething(c.Tool) || isCLIChange(c)
}

// planModeReads are the program's own tools that still go to their usual card while planning: they reach out to read.
var planModeReads = map[string]bool{"WebFetch": true, "WebSearch": true}

func modeGuidance(ctx context.Context) string {
	if modeFrom(ctx) != modePlan {
		return ""
	}
	return "\n\nPlan mode is on. Change nothing: no file writes, no commands beyond ones that only look, no cards, no proposals, no changes to Forgeline or any cloud; such calls are refused. Find out what you need by reading, then answer with a short numbered plan of what you would do, naming the files, commands and changes, and what you could not check. Do not ask for approval: the person gets a button to have the plan carried out."
}
