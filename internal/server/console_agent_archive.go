package server

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/forgelinehq/forgeline/internal/docstore"
)

// Deleting an agent archives it first. An archived agent is turned off, hidden from the team and the agent list, and
// can be restored for agentCoolingOff; after that the hourly sweep deletes it for good. Its reporting line is cut and
// agents that reported to it report to people again. An agent that an enabled workflow uses, or that a mission is
// still working with, cannot be archived: the error names them, so nothing breaks without warning. Past runs and
// missions keep the agent's name, so the history still reads after it is gone.

const agentCoolingOff = 30 * 24 * time.Hour

// agentUsers lists what still needs an agent: enabled workflows whose current version uses it, and missions that
// have not ended.
func (s *Server) agentUsers(ctx context.Context, id string) ([]string, error) {
	var out []string
	wfs, err := s.listWorkflows(ctx)
	if err != nil {
		return nil, err
	}
	for _, w := range wfs {
		if w.Disabled {
			continue
		}
		v, ok := w.version(w.Current)
		if !ok {
			continue
		}
		uses := false
		for _, st := range v.Stages {
			for _, sp := range st.Steps {
				uses = uses || sp.Agent == id
			}
		}
		if v.Graph != nil {
			for _, n := range v.Graph.Nodes {
				uses = uses || n.Agent == id
			}
		}
		if uses {
			out = append(out, "workflow "+w.Name)
		}
	}
	out = append(out, s.teamUsers(ctx, id)...)
	ms, err := s.listMissions(ctx)
	if err != nil {
		return nil, err
	}
	for _, m := range ms {
		if m.Agent == id && !m.final() {
			out = append(out, "mission "+truncate(m.Goal, 60))
		}
	}
	return out, nil
}

func (a AIAgent) archived() bool { return a.ArchivedAt != nil }

// handleArchiveAgent is DELETE /v1/ai/agents/{id}.
func (s *Server) handleArchiveAgent(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	a, err := s.loadAgent(ctx, r.PathValue("id"))
	if err != nil {
		s.defError(w, "agent", err)
		return
	}
	if a.archived() {
		writeError(w, http.StatusConflict, a.Name+" is already archived")
		return
	}
	users, err := s.agentUsers(ctx, a.ID)
	if err != nil {
		s.internalError(w, "checking what uses the agent", err)
		return
	}
	if len(users) > 0 {
		writeError(w, http.StatusConflict, fmt.Sprintf("%s is still used by %s; turn those off or give them another agent first", a.Name, strings.Join(users, ", ")))
		return
	}
	now := time.Now().UTC()
	if !a.Disabled || byDeveloper(a.DisabledByRole) {
		a.DisabledByRole = string(principalFrom(ctx).Role)
	}
	a.ArchivedAt, a.ArchivedBy, a.Disabled, a.ReportsTo = &now, principalFrom(ctx).Name, true, ""
	if err := s.docs.Put(ctx, kindAIAgent, a.ID, a); err != nil {
		s.internalError(w, "archiving the agent", err)
		return
	}
	// Whoever reported to it reports to people now.
	all, _ := s.listAgents(ctx)
	for _, x := range all {
		if x.ReportsTo == a.ID {
			x.ReportsTo = ""
			if err := s.docs.Put(ctx, kindAIAgent, x.ID, x); err != nil {
				s.log.Error("cutting a reporting line", "agent", x.ID, "err", err)
			}
		}
	}
	s.auditReq(r, "ai.agent.archive", "agent/"+a.ID, fmt.Sprintf("%s; deleted for good after %s unless restored", a.Name, now.Add(agentCoolingOff).Format("2006-01-02")), true)
	writeJSON(w, http.StatusOK, a)
}

// handleRestoreAgent brings an archived agent back, turned off, before the sweep deletes it.
func (s *Server) handleRestoreAgent(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	a, err := s.loadAgent(ctx, r.PathValue("id"))
	if err != nil {
		s.defError(w, "agent", err)
		return
	}
	if !a.archived() {
		writeError(w, http.StatusConflict, a.Name+" is not archived")
		return
	}
	a.ArchivedAt, a.ArchivedBy = nil, ""
	if err := s.docs.Put(ctx, kindAIAgent, a.ID, a); err != nil {
		s.internalError(w, "restoring the agent", err)
		return
	}
	s.auditReq(r, "ai.agent.restore", "agent/"+a.ID, a.Name+" (still turned off)", true)
	writeJSON(w, http.StatusOK, a)
}

// purgeArchivedAgents deletes agents archived longer than the cooling-off period. It runs with the audit pruner.
func (s *Server) purgeArchivedAgents(ctx context.Context, now time.Time) (int, error) {
	all, err := s.listAgents(ctx)
	if err != nil {
		return 0, err
	}
	n := 0
	for _, a := range all {
		if a.ArchivedAt == nil || now.Sub(*a.ArchivedAt) < agentCoolingOff {
			continue
		}
		if err := s.docs.Delete(ctx, kindAIAgent, a.ID); err != nil {
			return n, err
		}
		s.audit(ctx, Principal{Name: "forgeline"}, "", "ai.agent.purge", "agent/"+a.ID, fmt.Sprintf("%s, archived by %s on %s", a.Name, a.ArchivedBy, a.ArchivedAt.Format("2006-01-02")), true)
		n++
	}
	return n, nil
}

// --- workflows are archived the same way ---

// lastWorkflowRuns is the newest run of each workflow.
func (s *Server) lastWorkflowRuns(ctx context.Context) map[string]WFRun {
	out := map[string]WFRun{}
	docs, err := s.docs.List(ctx, kindWorkflowRun, docstore.ListOptions{Desc: true, Limit: maxWFRuns})
	if err != nil {
		return out
	}
	runs, _ := docstore.Decode[WFRun](docs)
	newestFirst(runs)
	for _, r := range runs {
		if _, ok := out[r.Workflow]; !ok {
			out[r.Workflow] = r
		}
	}
	return out
}

// handleArchiveWorkflow is DELETE /v1/ai/workflows/{id}: off, hidden, restorable for agentCoolingOff, then removed.
// A workflow with a run that is going or waiting for a person cannot be archived.
func (s *Server) handleArchiveWorkflow(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	wf, err := s.loadWorkflow(ctx, r.PathValue("id"))
	if err != nil {
		s.defError(w, "workflow", err)
		return
	}
	if wf.ArchivedAt != nil {
		writeError(w, http.StatusConflict, wf.Name+" is already archived")
		return
	}
	if lr, ok := s.lastWorkflowRuns(ctx)[wf.ID]; ok && (lr.Status == wfRunning || lr.Status == wfWaiting) {
		writeError(w, http.StatusConflict, wf.Name+" has a run that is "+lr.Status+"; let it finish or cancel it first")
		return
	}
	now := time.Now().UTC()
	if !s.mayChangeWorkflow(w, r, wf) {
		return
	}
	if !wf.Disabled || byDeveloper(wf.DisabledByRole) {
		wf.DisabledByRole = string(principalFrom(ctx).Role)
	}
	wf.ArchivedAt, wf.ArchivedBy, wf.Disabled = &now, principalFrom(ctx).Name, true
	if err := s.docs.Put(ctx, kindWorkflow, wf.ID, wf); err != nil {
		s.internalError(w, "archiving the workflow", err)
		return
	}
	s.auditReq(r, "ai.workflow.archive", "workflow/"+wf.ID, fmt.Sprintf("%s; deleted for good after %s unless restored", wf.Name, now.Add(agentCoolingOff).Format("2006-01-02")), true)
	writeJSON(w, http.StatusOK, wf)
}

func (s *Server) handleRestoreWorkflow(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	wf, err := s.loadWorkflow(ctx, r.PathValue("id"))
	if err != nil {
		s.defError(w, "workflow", err)
		return
	}
	if wf.ArchivedAt == nil {
		writeError(w, http.StatusConflict, wf.Name+" is not archived")
		return
	}
	if !s.mayChangeWorkflow(w, r, wf) {
		return
	}
	wf.ArchivedAt, wf.ArchivedBy = nil, ""
	if err := s.docs.Put(ctx, kindWorkflow, wf.ID, wf); err != nil {
		s.internalError(w, "restoring the workflow", err)
		return
	}
	s.auditReq(r, "ai.workflow.restore", "workflow/"+wf.ID, wf.Name+" (still turned off)", true)
	writeJSON(w, http.StatusOK, wf)
}

// purgeArchivedWorkflows deletes workflows archived longer than the cooling-off period. Their past runs stay.
func (s *Server) purgeArchivedWorkflows(ctx context.Context, now time.Time) (int, error) {
	all, err := s.listWorkflows(ctx)
	if err != nil {
		return 0, err
	}
	n := 0
	for _, wf := range all {
		if wf.ArchivedAt == nil || now.Sub(*wf.ArchivedAt) < agentCoolingOff {
			continue
		}
		if err := s.docs.Delete(ctx, kindWorkflow, wf.ID); err != nil {
			return n, err
		}
		s.audit(ctx, Principal{Name: "forgeline"}, "", "ai.workflow.purge", "workflow/"+wf.ID, fmt.Sprintf("%s, archived by %s on %s", wf.Name, wf.ArchivedBy, wf.ArchivedAt.Format("2006-01-02")), true)
		n++
	}
	return n, nil
}
