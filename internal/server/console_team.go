package server

import (
	"context"
	"net/http"
	"sort"
	"time"

	"github.com/forgelinehq/forgeline/internal/docstore"
)

// The AI team (note 029, frame 5). Agents can report to another agent, so the console can draw the team as an
// org chart, and each agent's state is read from what already exists: off, working on a workflow right now,
// waiting at a workflow's gate for a person, asleep until its next scheduled run, or waiting to be asked. A
// reporting line is a description in this slice: a lead does not hand work to its reports, and nothing here
// changes what an agent may do or who approves its proposals.

const (
	teamStateOff       = "off"
	teamStateWorking   = "working"
	teamStateWaiting   = "waiting" // a run it is in waits for a person at a gate
	teamStateScheduled = "scheduled"
	teamStateIdle      = "idle" // runs when someone asks or starts a workflow
)

type TeamMember struct {
	ID          string     `json:"id"`
	Name        string     `json:"name"`
	Description string     `json:"description"`
	ReportsTo   string     `json:"reports_to,omitempty"` // an agent id; empty reports to people
	Mode        string     `json:"mode"`                 // advises or proposes
	State       string     `json:"state"`
	Doing       string     `json:"doing,omitempty"`  // the workflow it is working on or waiting in
	RunID       string     `json:"run_id,omitempty"` // that run
	NextAt      *time.Time `json:"next_at,omitempty"`
	EveryMin    int        `json:"every_minutes,omitempty"`
	Workflows   []string   `json:"workflows,omitempty"` // enabled workflows it is a step of
	StepsWeek   int        `json:"steps_week"`
	FailedWeek  int        `json:"failed_week"`
	LastAt      *time.Time `json:"last_at,omitempty"`
}

type TeamJSON struct {
	Members       []TeamMember `json:"members"`
	Mission       *WFRun       `json:"mission,omitempty"` // the run in progress, else the latest
	AssistantOff  bool         `json:"assistant_off,omitempty"`
	SchedulesStop bool         `json:"schedules_stopped,omitempty"` // the assistant is off, so nothing runs on a schedule
}

func (s *Server) team(ctx context.Context, now time.Time) (TeamJSON, error) {
	agents, err := s.listAgents(ctx)
	if err != nil {
		return TeamJSON{}, err
	}
	wfs, err := s.listWorkflows(ctx)
	if err != nil {
		return TeamJSON{}, err
	}
	docs, err := s.docs.List(ctx, kindWorkflowRun, docstore.ListOptions{Desc: true, Limit: maxWFRuns})
	if err != nil {
		return TeamJSON{}, err
	}
	runs, err := docstore.Decode[WFRun](docs)
	if err != nil {
		return TeamJSON{}, err
	}
	out := TeamJSON{Members: []TeamMember{}, AssistantOff: s.loadAssistant(ctx).Backend == assistOff}
	out.SchedulesStop = out.AssistantOff

	byID := map[string]*TeamMember{}
	for _, a := range agents {
		m := TeamMember{ID: a.ID, Name: a.Name, Description: a.Description, ReportsTo: a.ReportsTo, Mode: "advises", State: teamStateIdle}
		if v, ok := a.version(a.Current); ok && v.Tier == tierReview {
			m.Mode = "proposes"
		}
		if a.Disabled {
			m.State = teamStateOff
		}
		out.Members = append(out.Members, m)
	}
	for i := range out.Members {
		byID[out.Members[i].ID] = &out.Members[i]
	}
	for _, m := range byID { // a lead that no longer exists is dropped
		if _, ok := byID[m.ReportsTo]; !ok {
			m.ReportsTo = ""
		}
	}
	for _, m := range out.Members { // so is a line that leads back to itself (saved lines never do; stored data might)
		for at, n := m.ReportsTo, 0; at != ""; at, n = byID[at].ReportsTo, n+1 {
			if at == m.ID || n > len(byID) {
				byID[m.ID].ReportsTo = ""
				break
			}
		}
	}

	// Schedules: the earliest next run among enabled scheduled workflows the agent is a step of.
	for _, wf := range wfs {
		v, ok := wf.version(wf.Current)
		if !ok || wf.Disabled {
			continue
		}
		var next time.Time
		if v.EveryMinutes > 0 {
			var st wfState
			_ = s.docs.Get(ctx, kindWFState, wf.ID, &st)
			next = st.LastScheduledAt.Add(time.Duration(v.EveryMinutes) * time.Minute)
			if next.Before(now) {
				next = now
			}
		}
		for _, stage := range v.Stages {
			for _, step := range stage.Steps {
				m := byID[step.Agent]
				if m == nil {
					continue
				}
				if len(m.Workflows) == 0 || m.Workflows[len(m.Workflows)-1] != wf.Name {
					m.Workflows = append(m.Workflows, wf.Name)
				}
				if !next.IsZero() && !out.SchedulesStop && (m.NextAt == nil || next.Before(*m.NextAt)) {
					n := next
					m.NextAt, m.EveryMin = &n, v.EveryMinutes
				}
			}
		}
	}

	// Runs: who is working now (newest run first), and what each did this week.
	weekAgo := now.AddDate(0, 0, -7)
	for _, run := range runs {
		for _, stage := range run.Stages {
			for _, step := range stage.Steps {
				m := byID[step.Agent]
				if m == nil {
					continue
				}
				if step.StartedAt != nil && (m.LastAt == nil || step.StartedAt.After(*m.LastAt)) {
					t := *step.StartedAt
					m.LastAt = &t
				}
				if step.StartedAt != nil && step.StartedAt.After(weekAgo) {
					m.StepsWeek++
					if step.Status == wfFailed {
						m.FailedWeek++
					}
				}
				if m.State == teamStateOff || m.Doing != "" {
					continue
				}
				switch {
				case step.Status == wfRunning:
					m.State, m.Doing, m.RunID = teamStateWorking, run.WorkflowName, run.ID
				case run.Status == wfWaiting && step.Status == wfPending:
					m.State, m.Doing, m.RunID = teamStateWaiting, run.WorkflowName, run.ID
				}
			}
		}
	}
	for i := range out.Members {
		m := &out.Members[i]
		if m.State == teamStateIdle && m.NextAt != nil {
			m.State = teamStateScheduled
		}
	}
	sort.SliceStable(out.Members, func(i, j int) bool { return out.Members[i].Name < out.Members[j].Name })

	for i := range runs { // the mission: a run in progress, else the latest
		if !runs[i].final() {
			out.Mission = &runs[i]
			break
		}
	}
	if out.Mission == nil && len(runs) > 0 {
		out.Mission = &runs[0]
	}
	return out, nil
}

func (s *Server) handleTeam(w http.ResponseWriter, r *http.Request) {
	t, err := s.team(r.Context(), time.Now().UTC())
	if err != nil {
		s.internalError(w, "reading the team", err)
		return
	}
	writeJSON(w, http.StatusOK, t)
}

// handleAgentReportsTo sets who an agent reports to: another agent, or nobody (it reports to people). A line
// that would make a loop is refused.
func (s *Server) handleAgentReportsTo(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ReportsTo string `json:"reports_to"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	a, err := s.loadAgent(r.Context(), r.PathValue("id"))
	if err != nil {
		s.defError(w, "agent", err)
		return
	}
	lead := ""
	if req.ReportsTo != "" {
		if req.ReportsTo == a.ID {
			writeError(w, http.StatusBadRequest, "an agent cannot report to itself")
			return
		}
		all, err := s.listAgents(r.Context())
		if err != nil {
			s.internalError(w, "reading the agents", err)
			return
		}
		up := map[string]string{}
		names := map[string]string{}
		for _, x := range all {
			up[x.ID], names[x.ID] = x.ReportsTo, x.Name
		}
		if _, ok := names[req.ReportsTo]; !ok {
			writeError(w, http.StatusBadRequest, "reports_to: no agent "+req.ReportsTo)
			return
		}
		for at, n := req.ReportsTo, 0; at != "" && n <= len(all); at, n = up[at], n+1 {
			if at == a.ID {
				writeError(w, http.StatusBadRequest, names[req.ReportsTo]+" already reports to "+a.Name+", so "+a.Name+" cannot report to it")
				return
			}
		}
		lead = names[req.ReportsTo]
	}
	a.ReportsTo = req.ReportsTo
	if err := s.docs.Put(r.Context(), kindAIAgent, a.ID, a); err != nil {
		s.internalError(w, "saving the agent", err)
		return
	}
	detail := a.Name + " reports to people"
	if lead != "" {
		detail = a.Name + " reports to " + lead
	}
	s.auditReq(r, "ai.agent.team", "agent/"+a.ID, detail, true)
	writeJSON(w, http.StatusOK, a)
}
