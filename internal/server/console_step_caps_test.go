package server

import (
	"context"
	"io"
	"net/http"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/forgelinehq/forgeline/internal/executor"
)

const capsPipeline = `version: v1alpha1
name: caps
steps:
  - id: plain
    image: alpine:3.24
    run: echo
  - id: raw
    image: alpine:3.24
    run: ping -c1 host
    permissions:
      capabilities: [NET_RAW, CHOWN]
`

// The setting decides what every step gets back, a step's own list adds to it, and only what the mode did not already give is
// recorded as a grant. The default is relaxed; the setting is the administrator's.
func TestStepCapabilitiesSettingReachesTheContainer(t *testing.T) {
	var mu sync.Mutex
	seen := map[string][]string{}
	h := newHarness(t, &executor.Fake{Handler: func(_ context.Context, s executor.StepRun, _ io.Writer) (executor.Result, error) {
		mu.Lock()
		seen[s.ID] = s.Capabilities
		mu.Unlock()
		return executor.Result{}, nil
	}})
	runOnce := func() string {
		_, run := h.submit(capsPipeline, nil)
		h.waitStatus(run.ID, "succeeded")
		return run.ID
	}
	var v stepCapsJSON
	_, b := h.do("GET", "/v1/settings/step-capabilities", testToken, nil, "")
	jsonUnmarshal(b, &v)
	if v.Mode != executor.CapsRelaxed || !slices.Equal(v.Baseline, executor.RelaxedCapabilities) {
		t.Fatalf("default = %+v", v)
	}

	first := runOnce()
	if !slices.Equal(seen["plain"], executor.RelaxedCapabilities) {
		t.Errorf("relaxed: plain step has %v", seen["plain"])
	}
	if !slices.Contains(seen["raw"], "NET_RAW") || !slices.Contains(seen["raw"], "CHOWN") {
		t.Errorf("relaxed: raw step has %v", seen["raw"])
	}

	if r, _ := h.do("PUT", "/v1/settings/step-capabilities", testToken, strings.NewReader(`{"mode":"loose"}`), "application/json"); r.StatusCode != http.StatusBadRequest {
		t.Errorf("an unknown mode: status %d", r.StatusCode)
	}
	if r, _ := h.do("PUT", "/v1/settings/step-capabilities", testToken, strings.NewReader(`{"mode":"strict"}`), "application/json"); r.StatusCode != http.StatusOK {
		t.Fatalf("setting strict: status %d", r.StatusCode)
	}
	second := runOnce()
	if len(seen["plain"]) != 0 {
		t.Errorf("strict: plain step has %v, want none", seen["plain"])
	}
	if !slices.Equal(seen["raw"], []string{"NET_RAW", "CHOWN"}) {
		t.Errorf("strict: raw step has %v", seen["raw"])
	}

	audit := func(action string) []AuditEvent {
		var out struct {
			Events []AuditEvent `json:"events"`
		}
		_, b := h.do("GET", "/v1/audit?limit=200&action="+action, testToken, nil, "")
		jsonUnmarshal(b, &out)
		return out.Events
	}
	grants := map[string]string{}
	for _, e := range audit("run.capability.grant") {
		grants[e.Target] = e.Detail
	}
	// Relaxed already gives CHOWN, so only NET_RAW is a grant; strict gives nothing, so both are.
	if d := grants["run/"+first+"/raw"]; !strings.Contains(d, "capabilities NET_RAW on top of the relaxed") || strings.Contains(d, "CHOWN") {
		t.Errorf("relaxed grant = %q", d)
	}
	if d := grants["run/"+second+"/raw"]; !strings.Contains(d, "capabilities NET_RAW, CHOWN on top of the strict") {
		t.Errorf("strict grant = %q", d)
	}
	if len(grants) != 2 {
		t.Errorf("grants = %v, want one per run for the step that asked", grants)
	}
	if ev := audit("settings.step_capabilities"); len(ev) != 1 || !strings.Contains(ev[0].Detail, "relaxed to strict") {
		t.Errorf("the setting change: %+v", ev)
	}
}

// Only an administrator changes it; a developer can read it, so a step's author can see what a step gets.
func TestStepCapabilitiesSettingIsAdminOnly(t *testing.T) {
	m := newMCPHarness(t)
	m.dev.expect(403, "PUT", "/v1/settings/step-capabilities", map[string]any{"mode": "strict"})
	m.dev.expect(200, "GET", "/v1/settings/step-capabilities", nil)
	m.admin.expect(200, "PUT", "/v1/settings/step-capabilities", map[string]any{"mode": "strict"})
}
