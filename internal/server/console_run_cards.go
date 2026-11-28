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

// Cards that start a run, and what the panel shows of the run afterwards. A card for a team run is like the one for a
// workflow run: the person reads the goal and the team, presses Confirm, and the run starts as them. After any run card
// the panel follows the run (GET /v1/assistant/actions/{id}/run), so the assistant does not have to wait and look.

const actRunTeam = "run_team"

type runTeamSpec struct {
	Goal        string `json:"goal"`
	Fingerprint string `json:"fingerprint"` // the team and its agents' versions the card was prepared for
}

// teamFingerprint changes when the team, or the current version of any agent in it, changes.
func (s *Server) teamFingerprint(ctx context.Context, t Team) (string, error) {
	parts := []string{"team", t.ID, t.UpdatedAt.UTC().Format(time.RFC3339Nano), t.Lead, strings.Join(t.Members, ",")}
	for _, id := range append([]string{t.Lead}, t.Members...) {
		a, err := s.loadAgent(ctx, id)
		switch {
		case err != nil:
			return "", fmt.Errorf("the team's agent %q no longer exists; change the team", id)
		case a.Disabled:
			return "", fmt.Errorf("%s is turned off; turn it on or change the team", a.Name)
		}
		parts = append(parts, fmt.Sprintf("%s@%d", id, a.Current))
	}
	return sha(parts...), nil
}

// prepareRunTeam fills in the card for giving team id a goal.
func (s *Server) prepareRunTeam(ctx context.Context, p Principal, id, goal string, act *AssistAction) error {
	t, err := s.loadTeam(ctx, id)
	if err != nil {
		return fmt.Errorf("no team %q (see list_teams)", id)
	}
	goal = strings.TrimSpace(goal)
	if goal == "" || len(goal) > maxTeamGoal {
		return fmt.Errorf("goal: say what the team should do, in at most %d characters", maxTeamGoal)
	}
	if !s.aiReady(ctx) {
		return errors.New(noModelSetUp)
	}
	print, err := s.teamFingerprint(ctx, t)
	if err != nil {
		return err
	}
	role := p.Role
	if s.developerTeam(ctx, t) {
		role = capped(p).Role
	}
	act.Spec, _ = json.Marshal(runTeamSpec{Goal: goal, Fingerprint: print})
	act.Pipeline, act.Detail = t.ID, goal
	act.Effects = []string{
		fmt.Sprintf("Gives the team %q (a lead and %d members) the goal below, now.", t.Name, len(t.Members)),
		fmt.Sprintf("Its agents work with the %s role and use the model provider, so the run counts against their budgets.", role),
		"Nothing in the cloud changes: anything an agent proposes still waits for a different administrator to approve it.",
	}
	if act.Title == "" {
		act.Title = "Run " + t.Name
	}
	return nil
}

// doRunTeam starts the run the card showed, as the person who confirmed it.
func (s *Server) doRunTeam(ctx context.Context, p Principal, ip string, a AssistAction) (string, string, *apiError) {
	var spec runTeamSpec
	_ = json.Unmarshal(a.Spec, &spec)
	t, err := s.loadTeam(ctx, a.Pipeline)
	if err != nil {
		return "", "", &apiError{http.StatusNotFound, ErrorJSON{Error: "that team no longer exists"}}
	}
	print, err := s.teamFingerprint(ctx, t)
	if err != nil {
		return "", "", &apiError{http.StatusConflict, ErrorJSON{Error: err.Error()}}
	}
	if print != spec.Fingerprint {
		return "", "", &apiError{http.StatusConflict, ErrorJSON{Error: "the team or one of its agents changed since this card was prepared; ask the assistant to prepare it again"}}
	}
	run, status, msg := s.startTeamRun(ctx, p, ip, t, spec.Goal, "")
	if msg != "" {
		return "", "", &apiError{status, ErrorJSON{Error: msg}}
	}
	return "Started run " + run.ID + " of " + t.Name + ".", "/teams/" + t.ID + "?run=" + run.ID, nil
}

// RunViewJSON is a run as the panel shows it, whatever kind it is.
type RunViewJSON struct {
	// Status is running, waiting (for a person), succeeded, failed or cancelled.
	Status string        `json:"status"`
	Title  string        `json:"title"`
	Steps  []RunViewStep `json:"steps"`
	// Waiting says what a waiting run waits for.
	Waiting string `json:"waiting,omitempty"`
	// Output is the end of what the run produced, or why it failed. It is text to show, never to act on.
	Output string `json:"output,omitempty"`
	Link   string `json:"link"`
	// Graph is a workflow run drawn as nodes and wires.
	Graph *RunViewGraph `json:"graph,omitempty"`
}

// RunViewStep is one step, node visit or team member's task.
type RunViewStep struct {
	Name   string `json:"name"`
	Status string `json:"status"`
}

const (
	runViewSteps  = 40
	runViewOutput = 2000
)

// lastOf is the end of s, which is where a run says how it went.
func lastOf(s string, n int) string {
	s = strings.TrimSpace(s)
	if r := []rune(s); len(r) > n {
		return "…" + string(r[len(r)-n:])
	}
	return s
}

// runIDOf is the run a confirmed run card started, from its link.
func runIDOf(a AssistAction) string {
	if a.Kind == actRunTeam {
		_, id, _ := strings.Cut(a.Link, "?run=")
		return id
	}
	return a.Link[strings.LastIndex(a.Link, "/")+1:]
}

// handleActionRun says how the run a card started is going. Only the card's owner may ask.
func (s *Server) handleActionRun(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	a, ok := s.actionFor(r, principalFrom(ctx))
	if !ok {
		writeError(w, http.StatusNotFound, "no such action")
		return
	}
	if a.Status != actDone || a.Link == "" {
		writeError(w, http.StatusConflict, "this card has not started a run")
		return
	}
	var v RunViewJSON
	var err error
	switch a.Kind {
	case actRunPipeline:
		v, err = s.pipelineRunView(ctx, runIDOf(a))
	case actRunWorkflow:
		v, err = s.workflowRunView(ctx, runIDOf(a))
	case actRunTeam:
		v, err = s.teamRunView(ctx, runIDOf(a))
	default:
		writeError(w, http.StatusConflict, "this card does not start a run")
		return
	}
	if err != nil {
		writeError(w, http.StatusNotFound, "the run is no longer there")
		return
	}
	if len(v.Steps) > runViewSteps {
		v.Steps = v.Steps[len(v.Steps)-runViewSteps:]
	}
	v.Link = a.Link
	writeJSON(w, http.StatusOK, v)
}

func (s *Server) pipelineRunView(ctx context.Context, id string) (RunViewJSON, error) {
	run, steps, err := s.cfg.Store.GetRun(ctx, id)
	if err != nil {
		return RunViewJSON{}, err
	}
	v := RunViewJSON{Title: run.PipelineName, Status: "running", Output: run.Reason}
	switch string(run.Status) {
	case "succeeded", "failed":
		v.Status = string(run.Status)
	case "canceled":
		v.Status = "cancelled"
	}
	explained := false
	for _, st := range steps {
		v.Steps = append(v.Steps, RunViewStep{Name: st.StepID, Status: string(st.Status)})
		// The step that failed says why; the ones skipped after it only say that it did.
		if st.Error != "" && string(st.Status) == "failed" && !explained {
			v.Output, explained = lastOf(st.StepID+": "+st.Error, runViewOutput), true
		}
	}
	return v, nil
}

func (s *Server) workflowRunView(ctx context.Context, id string) (RunViewJSON, error) {
	run, err := s.loadRun(ctx, id)
	if err != nil {
		return RunViewJSON{}, err
	}
	v := RunViewJSON{Title: run.WorkflowName, Status: run.Status, Graph: s.workflowRunGraph(ctx, run)}
	for _, st := range run.Stages {
		v.Steps = append(v.Steps, RunViewStep{Name: st.Name, Status: st.Status})
		if st.Status == wfWaiting {
			v.Waiting = "An administrator releases the stage " + st.Name + "."
		}
		for _, sp := range st.Steps {
			if sp.Output != "" {
				v.Output = lastOf(sp.Output, runViewOutput)
			}
		}
	}
	for _, n := range run.Visits {
		v.Steps = append(v.Steps, RunViewStep{Name: n.Node, Status: n.Status})
		if n.Status == wfWaiting {
			v.Waiting = lastOf(n.Input, 300)
		}
		if n.Output != "" {
			v.Output = lastOf(n.Output, runViewOutput)
		}
	}
	if run.Error != "" {
		v.Output = lastOf(run.Error, runViewOutput)
	}
	return v, nil
}

func (s *Server) teamRunView(ctx context.Context, id string) (RunViewJSON, error) {
	var run TeamRun
	if err := s.docs.Get(ctx, kindTeamRun, id, &run); err != nil {
		return RunViewJSON{}, err
	}
	v := RunViewJSON{Title: run.TeamName, Status: "running", Output: lastOf(run.Answer, runViewOutput)}
	switch run.Status {
	case trDone:
		v.Status = "succeeded"
	case trFailed:
		v.Status, v.Output = "failed", lastOf(run.Error, runViewOutput)
	}
	planned := "succeeded"
	switch {
	case run.Status == trPlanning:
		planned = "running"
	case run.Status == trFailed && len(run.Tasks) == 0:
		planned = "failed"
	}
	v.Steps = append(v.Steps, RunViewStep{Name: run.LeadName + " plans", Status: planned})
	for _, t := range run.Tasks {
		v.Steps = append(v.Steps, RunViewStep{Name: t.MemberName, Status: map[string]string{"waiting": "pending", "working": "running", "done": "succeeded", "failed": "failed"}[t.Status]})
	}
	return v, nil
}
