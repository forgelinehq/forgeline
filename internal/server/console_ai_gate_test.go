package server

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func agentNow(t *testing.T, c *browser, id string) AIAgent {
	t.Helper()
	var a AIAgent
	json.Unmarshal(c.expect(200, "GET", "/v1/ai/agents/"+id, nil), &a)
	return a
}

// The gate (note 050, decision 5): with a gate, a saved version is a candidate; it becomes current only when its
// suite has passed on that very version since the suite last changed, and it is published.
func TestAGatedVersionNeedsAPassingRun(t *testing.T) {
	m := newMCPHarness(t)
	a := newAgent(t, m.admin, agentBody("Reviewer", nil, nil, "T0"))
	other := newAgent(t, m.admin, agentBody("Other", nil, nil, "T0"))
	var x EvalSuite
	json.Unmarshal(m.dev.expect(201, "POST", "/v1/ai/evals", map[string]any{"agent": a.ID, "name": "Gate", "cases": []map[string]any{
		{"name": "Says ok", "input": "go", "checks": []map[string]any{{"kind": "contains", "value": "ok"}}}}}), &x)
	var empty, theirs EvalSuite
	json.Unmarshal(m.dev.expect(201, "POST", "/v1/ai/evals", map[string]any{"agent": a.ID, "name": "Empty", "cases": []map[string]any{}}), &empty)
	json.Unmarshal(m.dev.expect(201, "POST", "/v1/ai/evals", map[string]any{"agent": other.ID, "name": "Theirs", "cases": x.Cases}), &theirs)

	m.dev.expect(403, "PUT", "/v1/ai/agents/"+a.ID+"/gate", map[string]any{"suite": x.ID})
	m.admin.expect(400, "PUT", "/v1/ai/agents/"+a.ID+"/gate", map[string]any{"suite": theirs.ID})
	m.admin.expect(400, "PUT", "/v1/ai/agents/"+a.ID+"/gate", map[string]any{"suite": empty.ID})
	m.admin.expect(200, "PUT", "/v1/ai/agents/"+a.ID+"/gate", map[string]any{"suite": x.ID})

	// v2 is saved as a candidate: v1 stays current.
	b := agentBody("Reviewer", nil, nil, "T0")
	b["instructions"] = "Review things more carefully."
	m.admin.expect(201, "POST", "/v1/ai/agents/"+a.ID+"/versions", b)
	if got := agentNow(t, m.admin, a.ID); got.Current != 1 || len(got.Versions) != 2 || got.Gate == nil {
		t.Fatalf("after saving: current %d, %d versions, gate %+v", got.Current, len(got.Versions), got.Gate)
	}
	m.dev.expect(403, "POST", "/v1/ai/agents/"+a.ID+"/publish", map[string]any{"version": 2})
	if out := string(m.admin.expect(409, "POST", "/v1/ai/agents/"+a.ID+"/publish", map[string]any{"version": 2})); !strings.Contains(out, "no passing run") {
		t.Errorf("publish without a run: %s", out)
	}

	// A failing run on v2 does not let it through; a passing run on v1 does not count for v2.
	run := func(version int, reply string) EvalRun {
		m.llm.script(reply)
		var r EvalRun
		json.Unmarshal(m.dev.expect(202, "POST", "/v1/ai/evals/"+x.ID+"/run", map[string]any{"version": version}), &r)
		return waitEvalRun(t, m.dev, r.ID)
	}
	run(2, "no")
	run(1, "ok")
	m.admin.expect(409, "POST", "/v1/ai/agents/"+a.ID+"/publish", map[string]any{"version": 2})

	// A passing run on v2: publish makes it current.
	run(2, "ok")
	m.admin.expect(200, "POST", "/v1/ai/agents/"+a.ID+"/publish", map[string]any{"version": 2})
	if got := agentNow(t, m.admin, a.ID); got.Current != 2 {
		t.Fatalf("published: current %d", got.Current)
	}
	m.admin.expect(409, "POST", "/v1/ai/agents/"+a.ID+"/publish", map[string]any{"version": 2})

	// A run older than the suite's last change does not count.
	b["instructions"] = "Review things even more carefully."
	m.admin.expect(201, "POST", "/v1/ai/agents/"+a.ID+"/versions", b)
	run(3, "ok")
	time.Sleep(5 * time.Millisecond)
	x.Cases[0].Name = "Says ok, renamed"
	m.dev.expect(200, "PUT", "/v1/ai/evals/"+x.ID, x)
	m.admin.expect(409, "POST", "/v1/ai/agents/"+a.ID+"/publish", map[string]any{"version": 3})

	// Run and publish if it passes: a failing run leaves it a candidate, a passing one publishes it.
	m.llm.script("no")
	var started struct {
		Run EvalRun `json:"run"`
	}
	json.Unmarshal(m.admin.expect(202, "POST", "/v1/ai/agents/"+a.ID+"/publish", map[string]any{"version": 3, "when_passes": true}), &started)
	waitEvalRun(t, m.dev, started.Run.ID)
	if got := agentNow(t, m.admin, a.ID); got.Current != 2 {
		t.Fatalf("a failing run published v3: current %d", got.Current)
	}
	m.llm.script("ok")
	json.Unmarshal(m.admin.expect(202, "POST", "/v1/ai/agents/"+a.ID+"/publish", map[string]any{"version": 3, "when_passes": true}), &started)
	waitEvalRun(t, m.dev, started.Run.ID)
	got := agentNow(t, m.admin, a.ID)
	for i := 0; i < 50 && got.Current != 3; i++ {
		time.Sleep(10 * time.Millisecond)
		got = agentNow(t, m.admin, a.ID)
	}
	if got.Current != 3 {
		t.Fatalf("a passing run did not publish v3: current %d", got.Current)
	}
	audit := string(m.admin.expect(200, "GET", "/v1/audit?limit=100", nil))
	if !strings.Contains(audit, "ai.agent.gate") || !strings.Contains(audit, "v2 is current (suite") || !strings.Contains(audit, "v3 is current (suite") {
		t.Error("gate and publishing are not audited")
	}

	// Gate off: saving makes the new version current again, as before.
	m.admin.expect(200, "PUT", "/v1/ai/agents/"+a.ID+"/gate", map[string]any{"suite": ""})
	b["instructions"] = "Review, once more."
	m.admin.expect(201, "POST", "/v1/ai/agents/"+a.ID+"/versions", b)
	if got := agentNow(t, m.admin, a.ID); got.Current != 4 {
		t.Errorf("without a gate: current %d", got.Current)
	}
}
