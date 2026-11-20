package server

import (
	"context"
	"fmt"
	"net/http"
	"time"
)

// The gate on agent versions (design note 050, decision 5, accepted by the maintainer on 2026-11-20: "if passes and
// published it should be the current version"). With a gate, saving makes a candidate; publishing makes it current,
// and needs a passing run of the gate suite on that very version, made after the suite last changed. A rollback is
// not gated: it restores a version that was current before.

// handleAgentGate turns the gate on with a suite of this agent's, or off with an empty suite.
func (s *Server) handleAgentGate(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Suite string `json:"suite"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	ctx, who := r.Context(), principalFrom(r.Context())
	a, err := s.loadAgent(ctx, r.PathValue("id"))
	if err != nil {
		s.defError(w, "agent", err)
		return
	}
	if req.Suite == "" {
		a.Gate = nil
	} else {
		x, err := s.loadSuite(ctx, req.Suite)
		if err != nil || x.Agent != a.ID {
			writeError(w, http.StatusBadRequest, "suite: not one of this agent's suites")
			return
		}
		if len(x.Cases) == 0 {
			writeError(w, http.StatusBadRequest, "suite: it has no cases yet, so nothing could pass it")
			return
		}
		a.Gate = &AgentGate{Suite: x.ID, By: who.Name, At: time.Now().UTC()}
	}
	if err := s.docs.Put(ctx, kindAIAgent, a.ID, a); err != nil {
		s.internalError(w, "saving the agent", err)
		return
	}
	detail := "off"
	if a.Gate != nil {
		detail = "new versions must pass " + a.Gate.Suite
	}
	s.auditReq(r, "ai.agent.gate", "agent/"+a.ID, detail, true)
	writeJSON(w, http.StatusOK, a)
}

// passingRun is a run of the gate suite on version n with the version's own model, every case passed, made after the
// suite last changed; or nil.
func (s *Server) passingRun(ctx context.Context, x EvalSuite, n int) *EvalRun {
	runs, err := s.evalRuns(ctx, x.ID)
	if err != nil {
		return nil
	}
	for i := range runs {
		r := runs[i]
		if r.Version == n && r.Model == "" && r.Status == "done" && r.Total > 0 && r.Passed == r.Total && !r.Started.Before(x.UpdatedAt) {
			return &r
		}
	}
	return nil
}

// handlePublish makes a version current. With a gate it needs a passing run of the gate suite on that version, or,
// with when_passes, starts one and publishes when every case passes.
func (s *Server) handlePublish(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Version    int  `json:"version"`
		WhenPasses bool `json:"when_passes"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	ctx, p := r.Context(), principalFrom(r.Context())
	a, err := s.loadAgent(ctx, r.PathValue("id"))
	if err != nil {
		s.defError(w, "agent", err)
		return
	}
	v, ok := a.version(req.Version)
	switch {
	case !ok:
		writeError(w, http.StatusNotFound, fmt.Sprintf("version: %s has no version %d", a.ID, req.Version))
		return
	case v.N == a.Current:
		writeError(w, http.StatusConflict, fmt.Sprintf("v%d is already current", v.N))
		return
	case a.archived():
		writeError(w, http.StatusConflict, "this agent is archived; restore it first")
		return
	}
	if a.Gate == nil {
		s.publish(ctx, p, clientIP(r), a.ID, v.N, "no gate")
		a, _ = s.loadAgent(ctx, a.ID)
		writeJSON(w, http.StatusOK, a)
		return
	}
	x, err := s.loadSuite(ctx, a.Gate.Suite)
	if err != nil {
		writeError(w, http.StatusConflict, "the gate suite "+a.Gate.Suite+" no longer exists; choose another, or turn the gate off")
		return
	}
	if run := s.passingRun(ctx, x, v.N); run != nil {
		s.publish(ctx, p, clientIP(r), a.ID, v.N, "suite "+x.ID+" passed in run "+run.ID)
		a, _ = s.loadAgent(ctx, a.ID)
		writeJSON(w, http.StatusOK, a)
		return
	}
	if !req.WhenPasses {
		writeError(w, http.StatusConflict, fmt.Sprintf("v%d has no passing run of %s since the suite last changed; run it (or publish when it passes)", v.N, x.Name))
		return
	}
	run, status, msg := s.startSuiteRun(ctx, p, x, a, v, "", v.N)
	if msg != "" {
		writeError(w, status, msg)
		return
	}
	s.auditReq(r, "ai.agent.publish", "agent/"+a.ID, fmt.Sprintf("v%d will be published if run %s of %s passes", v.N, run.ID, x.ID), true)
	writeJSON(w, http.StatusAccepted, map[string]any{"run": run})
}

// publish makes version n current, if it still exists and is not already current.
func (s *Server) publish(ctx context.Context, p Principal, ip, agentID string, n int, why string) bool {
	s.agentMu.Lock()
	defer s.agentMu.Unlock()
	a, err := s.loadAgent(ctx, agentID)
	if err != nil || a.archived() || a.Current == n {
		return false
	}
	if _, ok := a.version(n); !ok {
		return false
	}
	a.Current = n
	if err := s.docs.Put(ctx, kindAIAgent, a.ID, a); err != nil {
		s.log.Error("publishing an agent version", "agent", a.ID, "err", err)
		return false
	}
	s.audit(ctx, p, ip, "ai.agent.publish", "agent/"+a.ID, fmt.Sprintf("v%d is current (%s)", n, why), true)
	return true
}
