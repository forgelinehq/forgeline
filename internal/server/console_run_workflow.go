package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
)

// A card that starts a workflow run (design note 052). The assistant can build a workflow and then, instead of telling the
// person to go and press Run, prepare the run: they read what it does and press Confirm, and the run starts as them. The
// card names the version it was prepared for; if the workflow has changed since, confirming it is refused, so the person
// only ever starts what they were shown.

const actRunWorkflow = "run_workflow"

type runWorkflowSpec struct {
	Version int `json:"version"`
}

func workflowShape(v WFVersion) string {
	switch {
	case v.Graph != nil:
		return fmt.Sprintf("a graph of %d nodes", len(v.Graph.Nodes))
	case len(v.Stages) == 1:
		return "1 stage"
	default:
		return fmt.Sprintf("%d stages", len(v.Stages))
	}
}

// prepareRunWorkflow fills in the card for starting workflow id.
func (s *Server) prepareRunWorkflow(ctx context.Context, id string, act *AssistAction) error {
	wf, err := s.loadWorkflow(ctx, id)
	if err != nil || wf.ArchivedAt != nil {
		return fmt.Errorf("no workflow %q (see list_workflows)", id)
	}
	if wf.Disabled {
		return fmt.Errorf("the workflow %q is turned off", wf.ID)
	}
	if !s.aiReady(ctx) {
		return errors.New(noModelSetUp)
	}
	v, ok := wf.version(wf.Current)
	if !ok {
		return fmt.Errorf("the workflow %q has no current version", wf.ID)
	}
	act.Spec, _ = json.Marshal(runWorkflowSpec{Version: v.N})
	act.Pipeline = wf.ID
	act.Effects = []string{
		fmt.Sprintf("Starts a run of the workflow %q (version %d, %s) now.", wf.Name, v.N, workflowShape(v)),
		fmt.Sprintf("Its agents run with the %s role and use the model provider, so the run counts against their budgets.", v.RunAs),
		"Nothing in the cloud changes: anything an agent proposes still waits for a different administrator to approve it.",
	}
	if act.Title == "" {
		act.Title = "Run " + wf.Name
	}
	return nil
}

// doRunWorkflow starts the run the card showed, as the person who confirmed it.
func (s *Server) doRunWorkflow(ctx context.Context, p Principal, a AssistAction) (string, string, *apiError) {
	var spec runWorkflowSpec
	_ = json.Unmarshal(a.Spec, &spec)
	wf, err := s.loadWorkflow(ctx, a.Pipeline)
	if err != nil || wf.ArchivedAt != nil {
		return "", "", &apiError{http.StatusNotFound, ErrorJSON{Error: "that workflow no longer exists"}}
	}
	switch {
	case wf.Disabled:
		return "", "", &apiError{http.StatusConflict, ErrorJSON{Error: "this workflow is turned off"}}
	case wf.Current != spec.Version:
		return "", "", &apiError{http.StatusConflict, ErrorJSON{Error: fmt.Sprintf("the workflow changed since this card was prepared (version %d, now %d); ask the assistant to prepare it again", spec.Version, wf.Current)}}
	case !s.aiReady(ctx):
		return "", "", &apiError{http.StatusConflict, ErrorJSON{Error: noModelSetUp}}
	}
	v, ok := wf.version(spec.Version)
	if !ok {
		return "", "", &apiError{http.StatusNotFound, ErrorJSON{Error: "that version no longer exists"}}
	}
	run, err := s.startRun(ctx, wf, v, "manual", p)
	if errors.Is(err, errWFBusy) {
		return "", "", &apiError{http.StatusConflict, ErrorJSON{Error: err.Error()}}
	} else if err != nil {
		return "", "", &apiError{http.StatusInternalServerError, ErrorJSON{Error: "could not start the run"}}
	}
	_ = s.audit(ctx, p, "", "ai.workflow.run", "workflow/"+wf.ID, fmt.Sprintf("run %s v%d, by the assistant, confirmed by the person", run.ID, v.N), true)
	return "Started run " + run.ID + " of " + wf.Name + ".", "/workflow-runs/" + run.ID, nil
}
