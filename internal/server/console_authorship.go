package server

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/forgelinehq/forgeline/internal/authn"
)

// Developers write agents, skills, teams and workflows (design note 054). A model follows what those say, so one rule
// keeps writing them from becoming a way to borrow more rights: what a developer wrote runs with at most a developer's
// rights, whoever starts it. Every saved version records its author's role. An empty role means an administrator: before
// this, only administrators could write them.

// byDeveloper says whether a saved role is below an administrator's.
func byDeveloper(role string) bool { return role != "" && role != string(authn.Admin) }

// capped is the person with at most a developer's rights.
func capped(p Principal) Principal {
	if p.Role == authn.Admin {
		p.Role = authn.Developer
	}
	return p
}

// developerAgent says whether a developer wrote any of what this agent version follows: the version itself or a skill it
// pins. What cannot be read counts as a developer's.
func (s *Server) developerAgent(ctx context.Context, v AgentVersion) bool {
	if byDeveloper(v.ByRole) {
		return true
	}
	for _, pin := range v.Skills {
		sk, err := s.loadSkill(ctx, pin.ID)
		if err != nil {
			return true
		}
		sv, ok := sk.version(pin.N)
		if !ok || byDeveloper(sv.ByRole) {
			return true
		}
	}
	return false
}

// developerTeam says whether a developer wrote the team or any agent in it. A team is one conversation: what one member
// was told reaches the others, so the whole run is held to a developer's rights.
func (s *Server) developerTeam(ctx context.Context, t Team) bool {
	if byDeveloper(t.UpdatedByRole) {
		return true
	}
	for _, id := range append([]string{t.Lead}, t.Members...) {
		a, err := s.loadAgent(ctx, id)
		if err != nil {
			return true
		}
		v, ok := a.version(a.Current)
		if !ok || s.developerAgent(ctx, v) {
			return true
		}
	}
	return false
}

// adminOnlyTools refuses a tool list holding a tool the person's own role may not use: nobody gives an agent more than
// they have.
func adminOnlyTools(tools []string, role authn.Role) string {
	if role.AtLeast(authn.Admin) {
		return ""
	}
	var bad []string
	for _, t := range assistantLookups() {
		for _, name := range tools {
			if name == t.name && t.min == authn.Admin {
				bad = append(bad, name)
			}
		}
	}
	if len(bad) == 0 {
		return ""
	}
	return fmt.Sprintf("tools: only an administrator can give an agent %s", strings.Join(bad, " and "))
}

// gatingAgents names the agents whose publishing gate is this suite. Changing such a suite would change what the gate
// asks for, so it is an administrator's, like the gate.
func (s *Server) gatingAgents(ctx context.Context, suite string) ([]string, error) {
	all, err := s.listAgents(ctx)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, a := range all {
		if a.Gate != nil && a.Gate.Suite == suite {
			out = append(out, a.Name)
		}
	}
	return out, nil
}

// approvalWorkflow says whether the workflow's current version is an administrator's and holds a stage or question an
// administrator put there. Only an administrator changes one: otherwise the approval could simply be taken out.
func approvalWorkflow(wf Workflow) bool {
	v, ok := wf.version(wf.Current)
	if !ok || byDeveloper(v.ByRole) {
		return false
	}
	for _, st := range v.Stages {
		if st.Gate {
			return true
		}
	}
	if v.Graph != nil {
		for _, n := range v.Graph.Nodes {
			if n.Kind == nodeAsk {
				return true
			}
		}
	}
	return false
}

// mayChangeWorkflow answers 403 and returns false when the person may not change this workflow.
func (s *Server) mayChangeWorkflow(w http.ResponseWriter, r *http.Request, wf Workflow) bool {
	if principalFrom(r.Context()).Role.AtLeast(authn.Admin) || !approvalWorkflow(wf) {
		return true
	}
	s.auditReq(r, "ai.workflow.refused", "workflow/"+wf.ID, "holds an administrator's approval step", false)
	writeError(w, http.StatusForbidden, wf.Name+" has an approval step an administrator set up, so only an administrator changes it; copy it to make your own")
	return false
}

// mayChangeSuite answers 403 and returns false when the suite gates an agent and the person is not an administrator.
func (s *Server) mayChangeSuite(w http.ResponseWriter, r *http.Request, suite string) bool {
	if principalFrom(r.Context()).Role.AtLeast(authn.Admin) {
		return true
	}
	agents, err := s.gatingAgents(r.Context(), suite)
	if err != nil {
		s.internalError(w, "checking the gates", err)
		return false
	}
	if len(agents) == 0 {
		return true
	}
	s.auditReq(r, "ai.eval.refused", "eval/"+suite, "it is the publishing gate of "+strings.Join(agents, ", "), false)
	writeError(w, http.StatusForbidden, "this suite is the publishing gate of "+strings.Join(agents, ", ")+", so only an administrator changes it")
	return false
}

// Deleting (design note 054): a developer deletes only what they created; an administrator deletes anything. The creator
// is who saved the first version. A record that does not say who created it is an administrator's to delete.

func ownsIt(p Principal, creator string) bool {
	return p.Role.AtLeast(authn.Admin) || creator != "" && creator == p.Name
}

// mayDelete answers 403 and returns false when this person may not delete what creator made.
func (s *Server) mayDelete(w http.ResponseWriter, r *http.Request, action, target, name, creator string) bool {
	if ownsIt(principalFrom(r.Context()), creator) {
		return true
	}
	who := "an administrator"
	if creator != "" {
		who = creator + " or an administrator"
	}
	s.auditReq(r, action, target, "refused: not the creator", false)
	writeError(w, http.StatusForbidden, "only "+who+" can delete "+name)
	return false
}
