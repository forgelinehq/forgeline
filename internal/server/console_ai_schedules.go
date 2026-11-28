package server

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/forgelinehq/forgeline/internal/authn"
	"github.com/forgelinehq/forgeline/internal/docstore"
	"github.com/forgelinehq/forgeline/internal/schedule"
)

// Schedules (design note 047). A workflow's schedule is part of its version: every N minutes or a cron expression in a
// time zone. A team has schedules of its own, each a goal given to the team at its times, run as a viewer or a
// developer chosen by an administrator, as an unattended workflow is: never as an administrator, so anything it
// proposes waits for a person. A schedule can be paused without changing anything else, and every change is audited.
// One list shows all of them with their next times.

const (
	kindTeamSchedule   = "aiteamschedule"
	maxTeamSchedules   = 10 // per team
	maxSchedulesListed = 200
)

// TeamSchedule is a goal a team works on by itself.
type TeamSchedule struct {
	ID        string    `json:"id"`
	Team      string    `json:"team"`
	Goal      string    `json:"goal"`
	Every     int       `json:"every_minutes,omitempty"`
	Cron      string    `json:"cron,omitempty"`
	TZ        string    `json:"tz,omitempty"`
	RunAs     string    `json:"run_as"`
	Paused    bool      `json:"paused,omitempty"`
	By        string    `json:"by"` // who saved it last
	CreatedBy string    `json:"created_by,omitempty"`
	At        time.Time `json:"at"`
	LastAt    time.Time `json:"last_at,omitempty"` // when it last started a run by itself
	LastRun   string    `json:"last_run,omitempty"`
	LastNote  string    `json:"last_note,omitempty"` // why the last time did not start a run
}

func (ts TeamSchedule) spec() schedule.Spec {
	return schedule.Spec{Every: ts.Every, Cron: ts.Cron, TZ: ts.TZ}
}

type teamScheduleRequest struct {
	Goal   string `json:"goal"`
	Every  int    `json:"every_minutes"`
	Cron   string `json:"cron"`
	TZ     string `json:"tz"`
	RunAs  string `json:"run_as"`
	Paused bool   `json:"paused"`
}

func (req *teamScheduleRequest) check() string {
	req.Goal = strings.TrimSpace(req.Goal)
	req.Cron = strings.Join(strings.Fields(req.Cron), " ")
	switch {
	case req.Goal == "" || len(req.Goal) > maxTeamGoal:
		return fmt.Sprintf("goal: say what the team should do each time, in at most %d characters", maxTeamGoal)
	case req.RunAs != "viewer" && req.RunAs != "developer":
		return "run_as: viewer or developer: nobody is watching a scheduled run, so it never runs as an administrator"
	case req.Every == 0 && req.Cron == "":
		return "say when it runs: every_minutes, or cron with tz"
	}
	if _, err := schedule.Check(schedule.Spec{Every: req.Every, Cron: req.Cron, TZ: req.TZ}); err != nil {
		return err.Error()
	}
	return ""
}

func (s *Server) listTeamSchedules(ctx context.Context) ([]TeamSchedule, error) {
	docs, err := s.docs.List(ctx, kindTeamSchedule, docstore.ListOptions{})
	if err != nil {
		return nil, err
	}
	return docstore.Decode[TeamSchedule](docs)
}

func (s *Server) handleCreateTeamSchedule(w http.ResponseWriter, r *http.Request) {
	var req teamScheduleRequest
	if !readJSON(w, r, &req) {
		return
	}
	ctx := r.Context()
	t, err := s.loadTeam(ctx, r.PathValue("id"))
	if err != nil {
		s.defError(w, "team", err)
		return
	}
	if m := req.check(); m != "" {
		writeError(w, http.StatusBadRequest, m)
		return
	}
	all, err := s.listTeamSchedules(ctx)
	if err != nil {
		s.internalError(w, "reading the schedules", err)
		return
	}
	n := 0
	for _, ts := range all {
		if ts.Team == t.ID {
			n++
		}
	}
	if n >= maxTeamSchedules {
		writeError(w, http.StatusConflict, fmt.Sprintf("a team has at most %d schedules", maxTeamSchedules))
		return
	}
	p := principalFrom(ctx)
	ts := TeamSchedule{ID: t.ID + "-" + randomID()[:6], Team: t.ID, Goal: req.Goal, Every: req.Every, Cron: req.Cron, TZ: req.TZ, RunAs: req.RunAs,
		Paused: req.Paused, By: p.Name, CreatedBy: p.Name, At: time.Now().UTC()}
	ts.LastAt = ts.At // it waits for its first time instead of running at once
	if err := s.docs.Put(ctx, kindTeamSchedule, ts.ID, ts); err != nil {
		s.internalError(w, "saving the schedule", err)
		return
	}
	s.auditReq(r, "ai.schedule.create", "team/"+t.ID, fmt.Sprintf("%s, %s as %s: %s", ts.ID, schedule.Describe(ts.spec()), ts.RunAs, truncate(ts.Goal, 120)), true)
	writeJSON(w, http.StatusCreated, ts)
}

func (s *Server) handleUpdateTeamSchedule(w http.ResponseWriter, r *http.Request) {
	var req teamScheduleRequest
	if !readJSON(w, r, &req) {
		return
	}
	ctx := r.Context()
	var ts TeamSchedule
	if err := s.docs.Get(ctx, kindTeamSchedule, r.PathValue("sid"), &ts); err != nil {
		s.defError(w, "schedule", err)
		return
	}
	if m := req.check(); m != "" {
		writeError(w, http.StatusBadRequest, m)
		return
	}
	p := principalFrom(ctx)
	if ts.Cron != req.Cron || ts.TZ != req.TZ || ts.Every != req.Every || ts.Paused && !req.Paused {
		ts.LastAt = time.Now().UTC() // a new time, or a resumed one, counts from now: missed times are not run
	}
	ts.Goal, ts.Every, ts.Cron, ts.TZ, ts.RunAs, ts.Paused, ts.By, ts.At = req.Goal, req.Every, req.Cron, req.TZ, req.RunAs, req.Paused, p.Name, time.Now().UTC()
	if err := s.docs.Put(ctx, kindTeamSchedule, ts.ID, ts); err != nil {
		s.internalError(w, "saving the schedule", err)
		return
	}
	state := ""
	if ts.Paused {
		state = ", paused"
	}
	s.auditReq(r, "ai.schedule.update", "team/"+ts.Team, fmt.Sprintf("%s, %s as %s%s: %s", ts.ID, schedule.Describe(ts.spec()), ts.RunAs, state, truncate(ts.Goal, 120)), true)
	writeJSON(w, http.StatusOK, ts)
}

func (s *Server) handleDeleteTeamSchedule(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var ts TeamSchedule
	if err := s.docs.Get(ctx, kindTeamSchedule, r.PathValue("sid"), &ts); err != nil {
		s.defError(w, "schedule", err)
		return
	}
	if !s.mayDelete(w, r, "ai.schedule.delete", "team/"+ts.Team, "this schedule", ts.CreatedBy) {
		return
	}
	if err := s.docs.Delete(ctx, kindTeamSchedule, ts.ID); err != nil {
		s.internalError(w, "removing the schedule", err)
		return
	}
	s.auditReq(r, "ai.schedule.delete", "team/"+ts.Team, ts.ID, true)
	w.WriteHeader(http.StatusNoContent)
}

// handleWorkflowSchedulePause pauses or resumes a workflow's schedule without a new version: the workflow still runs
// by hand, from its webhook and after failed runs.
func (s *Server) handleWorkflowSchedulePause(paused bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		wf, err := s.loadWorkflow(ctx, r.PathValue("id"))
		if err != nil {
			s.defError(w, "workflow", err)
			return
		}
		if !s.mayChangeWorkflow(w, r, wf) {
			return
		}
		v, _ := wf.version(wf.Current)
		if !v.spec().On() {
			writeError(w, http.StatusConflict, "this workflow has no schedule; set one in its settings")
			return
		}
		p := principalFrom(ctx)
		s.wf.mu.Lock()
		var st wfState
		_ = s.docs.Get(ctx, kindWFState, wf.ID, &st)
		st.Paused, st.PausedBy, st.PausedAt = paused, "", nil
		if paused {
			now := time.Now().UTC()
			st.PausedBy, st.PausedAt = p.Name, &now
		} else if v.Cron != "" {
			st.LastScheduledAt = time.Now().UTC() // resuming waits for the next time; it does not run the missed ones
		}
		err = s.docs.Put(ctx, kindWFState, wf.ID, st)
		s.wf.mu.Unlock()
		if err != nil {
			s.internalError(w, "saving the schedule", err)
			return
		}
		action := "ai.schedule.resume"
		if paused {
			action = "ai.schedule.pause"
		}
		s.auditReq(r, action, "workflow/"+wf.ID, schedule.Describe(v.spec()), true)
		w.WriteHeader(http.StatusNoContent)
	}
}

// scheduleItem is one row of the schedules list.
type scheduleItem struct {
	Kind     string      `json:"kind"` // workflow or team
	ID       string      `json:"id"`   // the workflow, or the team schedule
	Name     string      `json:"name"` // the workflow's or the team's name
	Target   string      `json:"target"`
	Goal     string      `json:"goal,omitempty"`
	Every    int         `json:"every_minutes,omitempty"`
	Cron     string      `json:"cron,omitempty"`
	TZ       string      `json:"tz,omitempty"`
	Describe string      `json:"describe"`
	RunAs    string      `json:"run_as"`
	Paused   bool        `json:"paused"`
	Off      string      `json:"off,omitempty"` // why it does not run now (turned off, no model)
	Next     []time.Time `json:"next"`
	LastAt   *time.Time  `json:"last_at,omitempty"`
	LastRun  string      `json:"last_run,omitempty"`
	LastNote string      `json:"last_note,omitempty"`
	// CreatedBy is who made a team's schedule, so the console offers Delete only to them and to administrators.
	CreatedBy string `json:"created_by,omitempty"`
}

func (s *Server) handleListSchedules(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	now := time.Now().UTC()
	out := []scheduleItem{}
	off := ""
	if !s.aiReady(ctx) {
		off = "no model is set up, so nothing runs by itself"
	}
	wfs, err := s.listWorkflows(ctx)
	if err != nil {
		s.internalError(w, "reading the workflows", err)
		return
	}
	for _, wf := range wfs {
		v, ok := wf.version(wf.Current)
		if !ok || !v.spec().On() || wf.ArchivedAt != nil {
			continue
		}
		var st wfState
		_ = s.docs.Get(ctx, kindWFState, wf.ID, &st)
		it := scheduleItem{Kind: "workflow", ID: wf.ID, Name: wf.Name, Target: "/workflows/" + wf.ID, Every: v.EveryMinutes, Cron: v.Cron, TZ: v.TZ,
			Describe: schedule.Describe(v.spec()), RunAs: v.RunAs, Paused: st.Paused, Off: off}
		if wf.Disabled {
			it.Off = "the workflow is turned off"
		}
		if !st.LastScheduledAt.IsZero() {
			t := st.LastScheduledAt
			it.LastAt = &t
		}
		if !it.Paused && it.Off == "" {
			it.Next = schedule.NextTimes(v.spec(), st.LastScheduledAt, now, 3)
		}
		out = append(out, it)
	}
	tss, err := s.listTeamSchedules(ctx)
	if err != nil {
		s.internalError(w, "reading the schedules", err)
		return
	}
	for _, ts := range tss {
		t, err := s.loadTeam(ctx, ts.Team)
		if err != nil {
			continue
		}
		it := scheduleItem{Kind: "team", ID: ts.ID, Name: t.Name, Target: "/teams/" + t.ID, Goal: ts.Goal, Every: ts.Every, Cron: ts.Cron, TZ: ts.TZ,
			Describe: schedule.Describe(ts.spec()), RunAs: ts.RunAs, Paused: ts.Paused, Off: off, LastRun: ts.LastRun, LastNote: ts.LastNote, CreatedBy: ts.CreatedBy}
		if !ts.LastAt.IsZero() && ts.LastRun != "" {
			t := ts.LastAt
			it.LastAt = &t
		}
		if !it.Paused && it.Off == "" {
			it.Next = schedule.NextTimes(ts.spec(), ts.LastAt, now, 3)
		}
		out = append(out, it)
		if len(out) >= maxSchedulesListed {
			break
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		a, b := out[i].Next, out[j].Next
		switch {
		case len(a) == 0 || len(b) == 0:
			return len(a) > len(b)
		}
		return a[0].Before(b[0])
	})
	writeJSON(w, http.StatusOK, map[string]any{"schedules": out, "now": now})
}

// handlePreviewSchedule says what a schedule means before it is saved: in words and its next five times.
func (s *Server) handlePreviewSchedule(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Every int    `json:"every_minutes"`
		Cron  string `json:"cron"`
		TZ    string `json:"tz"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	spec := schedule.Spec{Every: req.Every, Cron: strings.Join(strings.Fields(req.Cron), " "), TZ: req.TZ}
	if _, err := schedule.Check(spec); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	now := time.Now().UTC()
	writeJSON(w, http.StatusOK, map[string]any{"describe": schedule.Describe(spec), "next": schedule.NextTimes(spec, now, now, 5)})
}

// teamScheduleTick starts each team schedule whose time has come. A team already working skips that time and says
// so on the schedule; the clock moves either way, so a busy team is not started late, twice.
func (s *Server) teamScheduleTick(ctx context.Context, now time.Time) {
	if !s.aiReady(ctx) {
		return
	}
	all, err := s.listTeamSchedules(ctx)
	if err != nil {
		return
	}
	for _, ts := range all {
		if ts.Paused || !schedule.Due(ts.spec(), ts.LastAt, now) {
			continue
		}
		t, err := s.loadTeam(ctx, ts.Team)
		if errors.Is(err, docstore.ErrNotFound) {
			_ = s.docs.Delete(ctx, kindTeamSchedule, ts.ID) // its team is gone
			continue
		} else if err != nil {
			continue
		}
		p := Principal{Name: "schedule:" + ts.ID, Role: authn.Role(ts.RunAs)}
		run, _, msg := s.startTeamRun(ctx, p, "", t, ts.Goal, "schedule")
		ts.LastAt, ts.LastNote = now, msg
		if msg == "" {
			ts.LastRun = run.ID
		}
		_ = s.docs.Put(ctx, kindTeamSchedule, ts.ID, ts)
	}
}
