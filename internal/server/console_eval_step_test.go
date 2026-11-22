package server

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/forgelinehq/forgeline/internal/store"
)

func evalPipeline(suite string) string {
	return "version: v1alpha1\nname: ship-agent\nsteps:\n  - id: gate\n    eval:\n      suite: " + suite + "\n  - id: after\n    needs: [gate]\n    image: alpine:3.20\n    run: echo shipped\n"
}

// An eval step runs the suite on the server as the run, passes only when every case does, and stops what needs it. A run
// started by GitHub cannot run one, and never reaches the model.
func TestEvalStepGatesAPipelineOnAnAgentsSuite(t *testing.T) {
	m := newMCPHarness(t)
	m.turnOn()
	a := newAgent(t, m.admin, agentBody("Fixer", nil, []string{"list_runs", "propose_aws_change"}, "T1"))
	var x EvalSuite
	json.Unmarshal(m.dev.expect(201, "POST", "/v1/ai/evals", map[string]any{"agent": a.ID, "name": "Gate", "attempts": 1, "cases": []map[string]any{
		{"name": "Names the cause", "input": "Why did build 42 fail?", "checks": []map[string]any{{"kind": "contains", "value": "timeout"}, {"kind": "tool_not_called", "value": "propose_aws_change"}}},
	}}), &x)

	// A passing suite lets the pipeline carry on. (This harness's fake executor exits 1, so the step after the gate runs
	// and fails: that it ran at all, instead of being skipped, is what shows the gate let it through.)
	m.llm.script("It was a timeout.")
	_, run := m.submit(evalPipeline(x.ID), nil)
	got := m.waitStatus(run.ID, "failed")
	if s := stepStatuses(got); s["gate"] != "succeeded" || s["after"] != "failed" {
		t.Fatalf("steps = %v", s)
	}
	log := m.stepLog(run.ID, "gate")
	for _, want := range []string{"Suite \"" + x.ID + "\": 1 cases", "pass  Names the cause", "1 of 1 cases passed"} {
		if !strings.Contains(log, want) {
			t.Errorf("log lacks %q:\n%s", want, log)
		}
	}
	var runs struct {
		Runs []EvalRun `json:"runs"`
	}
	json.Unmarshal(m.dev.expect(200, "GET", "/v1/ai/eval-runs?suite="+x.ID, nil), &runs)
	if len(runs.Runs) != 1 || runs.Runs[0].By != "run "+run.ID || runs.Runs[0].Passed != 1 {
		t.Fatalf("eval runs: %+v", runs.Runs)
	}
	if !strings.Contains(string(m.admin.expect(200, "GET", "/v1/audit?limit=50", nil)), "ai.eval.run") {
		t.Error("the eval is not in the audit log")
	}

	// A failing case fails the step and skips what needs it.
	m.llm.script("It broke somewhere.")
	_, run = m.submit(evalPipeline(x.ID), nil)
	got = m.waitStatus(run.ID, "failed")
	if s := stepStatuses(got); s["gate"] != "failed" || s["after"] != "skipped" {
		t.Fatalf("steps = %v", s)
	}
	if e := stepError(got, "gate"); !strings.Contains(e, "1 of 1 cases failed: Names the cause") {
		t.Errorf("error = %q", e)
	}
	if !strings.Contains(m.stepLog(run.ID, "gate"), "FAIL  Names the cause") {
		t.Error("the log does not mark the failing case")
	}

	// An unknown suite fails by name, before any model call.
	m.llm.script()
	_, run = m.submit(evalPipeline("nope"), nil)
	got = m.waitStatus(run.ID, "failed")
	if e := stepError(got, "gate"); !strings.Contains(e, `no eval suite "nope"`) {
		t.Errorf("error = %q", e)
	}

	// A run GitHub started may not run it, and the model is never asked.
	r, err := m.store.CreateRun(context.Background(), store.Run{PipelineName: "ship-agent", PipelineYAML: evalPipeline(x.ID), Trigger: store.TriggerGitHub}, []string{"gate", "after"})
	if err != nil {
		t.Fatal(err)
	}
	got = m.waitStatus(r.ID, "failed")
	if e := stepError(got, "gate"); !strings.Contains(e, "started by GitHub may not run evals") {
		t.Errorf("error = %q", e)
	}
	if n := m.llm.count(); n != 0 {
		t.Errorf("the model was asked %d times for a refused eval", n)
	}
}
