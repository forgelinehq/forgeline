package server

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/forgelinehq/forgeline/internal/executor"
	"github.com/forgelinehq/forgeline/internal/pipeline"
	"github.com/forgelinehq/forgeline/internal/store"
)

// Linux capabilities of step containers (design note 005, T2). Every step container drops all of them. The administrator
// chooses what every step gets back: "relaxed" (the default while the effect on real pipelines is measured) or "strict"
// (nothing). A step can list more in permissions.capabilities, and each such grant is in the audit log.

const idStepCaps = "step-capabilities"

// StepCapsSettings is the administrator's choice.
type StepCapsSettings struct {
	Mode      string    `json:"mode"` // relaxed (default) or strict
	UpdatedBy string    `json:"updated_by,omitempty"`
	UpdatedAt time.Time `json:"updated_at,omitempty"`
}

func (s *Server) loadStepCaps(ctx context.Context) StepCapsSettings {
	var st StepCapsSettings
	_ = s.docs.Get(ctx, kindSettings, idStepCaps, &st)
	if st.Mode != executor.CapsStrict {
		st.Mode = executor.CapsRelaxed
	}
	return st
}

type stepCapsJSON struct {
	StepCapsSettings
	Baseline  []string `json:"baseline"`  // what every step gets in the current mode
	Dropped   []string `json:"dropped"`   // what relaxed mode still drops
	Grantable []string `json:"grantable"` // what a step may list
}

func stepCapsView(st StepCapsSettings) stepCapsJSON {
	v := stepCapsJSON{StepCapsSettings: st, Baseline: []string{}, Grantable: append([]string(nil), pipeline.GrantableCapabilities...)}
	if st.Mode == executor.CapsRelaxed {
		v.Baseline = append(v.Baseline, executor.RelaxedCapabilities...)
	}
	for _, c := range pipeline.GrantableCapabilities {
		if !contains(executor.RelaxedCapabilities, c) {
			v.Dropped = append(v.Dropped, c)
		}
	}
	return v
}

func (s *Server) handleGetStepCaps(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, stepCapsView(s.loadStepCaps(r.Context())))
}

func (s *Server) handlePutStepCaps(w http.ResponseWriter, r *http.Request) {
	var q struct {
		Mode string `json:"mode"`
	}
	if !readJSON(w, r, &q) {
		return
	}
	if q.Mode != executor.CapsRelaxed && q.Mode != executor.CapsStrict {
		writeError(w, http.StatusBadRequest, "mode: relaxed or strict")
		return
	}
	p := principalFrom(r.Context())
	cur := s.loadStepCaps(r.Context())
	old := cur.Mode
	cur.Mode, cur.UpdatedBy, cur.UpdatedAt = q.Mode, p.Name, time.Now().UTC()
	if err := s.docs.Put(r.Context(), kindSettings, idStepCaps, cur); err != nil {
		s.internalError(w, "saving the capability setting", err)
		return
	}
	s.auditReq(r, "settings.step_capabilities", "settings/step-capabilities", fmt.Sprintf("%s to %s", old, cur.Mode), true)
	writeJSON(w, http.StatusOK, stepCapsView(cur))
}

// auditCapabilityGrant records that a step asked for capabilities its mode did not give.
func (s *Server) auditCapabilityGrant(ctx context.Context, run store.Run, stepID string, names []string, mode string) {
	_ = s.audit(ctx, runActor(run), "", "run.capability.grant", "run/"+run.ID+"/"+stepID,
		fmt.Sprintf("capabilities %s on top of the %s baseline; run started by %s (%s)", strings.Join(names, ", "), mode, run.Trigger, trustOf(run)), true)
}
