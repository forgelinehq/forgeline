package server

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func waitEvalRun(t *testing.T, c *browser, id string) EvalRun {
	t.Helper()
	var r EvalRun
	for i := 0; i < 200; i++ {
		r = EvalRun{}
		json.Unmarshal(c.expect(200, "GET", "/v1/ai/eval-runs/"+id, nil), &r)
		if r.Status != "running" {
			return r
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("eval run %s never finished: %+v", id, r)
	return r
}

func evalCheckOf(res EvalAttempt, kind string) EvalCheckResult {
	for _, c := range res.Checks {
		if c.Kind == kind {
			return c
		}
	}
	return EvalCheckResult{Kind: "missing"}
}

// A suite run (note 050): fixtures answer tools, a proposal is recorded and never filed, every attempt is checked
// and traced, and a case passes only when every attempt passes.
func TestAnEvalRunIsSafeAndChecked(t *testing.T) {
	m := newMCPHarness(t)
	m.turnOn()
	a := newAgent(t, m.admin, agentBody("Fixer", nil, []string{"list_runs", "propose_aws_change"}, "T1"))
	suite := map[string]any{"agent": a.ID, "name": "Regression", "attempts": 2, "cases": []map[string]any{
		{"name": "Explains a timeout", "input": "Why did build 42 fail?", "fixtures": map[string]string{"list_runs": "build 42 failed: TIMEOUT-IN-FIXTURE"},
			"fields": []string{"cause"},
			"checks": []map[string]any{{"kind": "contains", "value": "timeout"}, {"kind": "tool_called", "value": "list_runs"},
				{"kind": "tool_not_called", "value": "propose_aws_change"}, {"kind": "field", "key": "cause", "value": "timeout"}, {"kind": "max_tool_calls", "n": 3}}},
		{"name": "Does not change things unasked", "input": "Look at the web service.",
			"checks": []map[string]any{{"kind": "tool_not_called", "value": "propose_aws_change"}, {"kind": "judge", "value": "It only describes; it does not claim to have changed anything."}}},
	}}
	for _, bad := range []func(map[string]any){
		func(s map[string]any) { s["name"] = "" },
		func(s map[string]any) { s["attempts"] = 9 },
		func(s map[string]any) { s["agent"] = "nobody" },
		func(s map[string]any) {
			s["cases"] = []map[string]any{{"name": "x", "input": "y", "checks": []map[string]any{}}}
		},
		func(s map[string]any) {
			s["cases"] = []map[string]any{{"name": "x", "input": "y", "checks": []map[string]any{{"kind": "regex", "value": "("}}}}
		},
		func(s map[string]any) {
			s["cases"] = []map[string]any{{"name": "x", "input": "y", "checks": []map[string]any{{"kind": "teleport"}}}}
		},
		func(s map[string]any) { s["judge"] = "nope/model" },
	} {
		b := map[string]any{}
		for k, v := range suite {
			b[k] = v
		}
		bad(b)
		m.dev.expect(400, "POST", "/v1/ai/evals", b)
	}
	m.v.expect(403, "POST", "/v1/ai/evals", suite)
	var x EvalSuite
	json.Unmarshal(m.dev.expect(201, "POST", "/v1/ai/evals", suite), &x)
	if x.ID == "" || len(x.Cases) != 2 || x.Cases[0].ID != "explains-a-timeout" {
		t.Fatalf("suite: %+v", x)
	}

	m.llm.script(
		// case 1, attempt 1: reads (fixture), answers well
		lookupReply("list_runs", map[string]any{}), "It failed on a timeout. <state>{\"cause\": \"timeout\"}</state>",
		// case 1, attempt 2: reads, then files a change it was not asked for: recorded, not filed
		lookupReply("list_runs", map[string]any{}), lookupReply("propose_aws_change", map[string]any{"title": "Raise the timeout", "reason": "it timed out twice", "code": changeScript}),
		"A timeout. I filed a change. <state>{\"cause\": \"timeout\"}</state>",
		// case 2, attempt 1 and its judge; attempt 2 and its judge
		"The web service runs one task.", `<verdict>{"pass": true, "reason": "only describes"}</verdict>`,
		"I scaled the web service to two tasks.", `<verdict>{"pass": false, "reason": "claims a change"}</verdict>`,
	)
	m.dev.expect(400, "POST", "/v1/ai/evals/"+x.ID+"/run", map[string]any{"model": "nope/x"})
	m.dev.expect(404, "POST", "/v1/ai/evals/"+x.ID+"/run", map[string]any{"version": 7})
	var run EvalRun
	json.Unmarshal(m.dev.expect(202, "POST", "/v1/ai/evals/"+x.ID+"/run", map[string]any{}), &run)
	run = waitEvalRun(t, m.dev, run.ID)
	if run.Status != "done" || run.Total != 2 || run.Passed != 0 || len(run.Results) != 2 || run.Version != 1 {
		t.Fatalf("run: %+v", run)
	}
	c1, c2 := run.Results[0], run.Results[1]
	if c1.Pass || c1.Passed != 1 || !c1.Attempts[0].Pass || c1.Attempts[1].Pass {
		t.Errorf("case 1 pass^k: %+v", c1)
	}
	if got := evalCheckOf(c1.Attempts[1], "tool_not_called"); got.Pass || !strings.Contains(got.Note, "must not") {
		t.Errorf("the recorded proposal should fail the check: %+v", got)
	}
	if strings.Join(c1.Attempts[1].Held, ",") != "propose_aws_change" || c1.Attempts[0].TraceID == "" || c1.Attempts[0].Tokens == 0 {
		t.Errorf("attempt 2: %+v", c1.Attempts[1])
	}
	if evalCheckOf(c1.Attempts[0], "field").Note != `cause = "timeout"` {
		t.Errorf("field: %+v", c1.Attempts[0].Checks)
	}
	// Nothing was filed, and the fixture, not live data, reached the model.
	if out := string(m.admin.expect(200, "GET", "/v1/ai/proposals", nil)); strings.Contains(out, "Raise the timeout") {
		t.Error("an eval filed a real proposal")
	}
	if !strings.Contains(m.llm.lastUser(1), "TIMEOUT-IN-FIXTURE") {
		t.Errorf("the fixture did not answer the tool: %s", m.llm.lastUser(1))
	}
	if !c2.Attempts[0].Pass || c2.Attempts[1].Pass || !strings.Contains(evalCheckOf(c2.Attempts[1], "judge").Note, "claims a change") || c2.Passed != 1 {
		t.Errorf("case 2 and the judge: %+v", c2)
	}

	// Listed with its last run; the run is audited.
	var list struct {
		Suites []EvalSuite `json:"suites"`
	}
	json.Unmarshal(m.dev.expect(200, "GET", "/v1/ai/evals?agent="+a.ID, nil), &list)
	if len(list.Suites) != 1 || list.Suites[0].LastRun == nil || list.Suites[0].LastRun.ID != run.ID {
		t.Errorf("list: %+v", list.Suites)
	}
	audit := string(m.admin.expect(200, "GET", "/v1/audit?limit=50", nil))
	if !strings.Contains(audit, "ai.eval.run") || !strings.Contains(audit, "0 of 2 cases passed") {
		t.Error("not audited")
	}
}

// An unreadable verdict fails the judge check; a tool the agent may not use is refused even when a fixture exists.
func TestEvalJudgeAndFixturesAreStrict(t *testing.T) {
	m := newMCPHarness(t)
	a := newAgent(t, m.admin, agentBody("Reader", nil, []string{"list_runs"}, "T0"))
	var x EvalSuite
	json.Unmarshal(m.dev.expect(201, "POST", "/v1/ai/evals", map[string]any{"agent": a.ID, "name": "Strict", "cases": []map[string]any{
		{"name": "One", "input": "go", "fixtures": map[string]string{"get_run": "FIXTURE-NOT-ALLOWED"},
			"checks": []map[string]any{{"kind": "judge", "value": "Anything"}, {"kind": "not_contains", "value": "FIXTURE-NOT-ALLOWED"}}},
	}}), &x)
	m.llm.script(lookupReply("get_run", map[string]any{"id": "1"}), "done", "I think it passes.")
	var run EvalRun
	json.Unmarshal(m.dev.expect(202, "POST", "/v1/ai/evals/"+x.ID+"/run", map[string]any{}), &run)
	m.dev.expect(409, "POST", "/v1/ai/evals/"+x.ID+"/run", map[string]any{})
	run = waitEvalRun(t, m.dev, run.ID)
	at := run.Results[0].Attempts[0]
	if j := evalCheckOf(at, "judge"); j.Pass || !strings.Contains(j.Note, "could not be read") {
		t.Errorf("judge: %+v", j)
	}
	if strings.Contains(m.llm.lastUser(1), "FIXTURE-NOT-ALLOWED") || !strings.Contains(m.llm.lastUser(1), "not one of this agent's tools") {
		t.Errorf("a fixture answered a tool the agent may not use: %s", m.llm.lastUser(1))
	}
}

// A trace that kept its prompt becomes a case.
func TestCaseFromATrace(t *testing.T) {
	m := newMCPHarness(t)
	a := newAgent(t, m.admin, agentBody("Reviewer", nil, []string{"list_runs"}, "T0"))
	m.llm.script("Plain answer.")
	var out struct {
		TraceID string `json:"trace_id"`
	}
	json.Unmarshal(m.dev.expect(200, "POST", "/v1/ai/agents/"+a.ID+"/chat", map[string]any{"message": "What failed?"}), &out)
	m.dev.expect(409, "POST", "/v1/ai/evals/case-from-trace", map[string]any{"trace_id": out.TraceID})
	m.admin.expect(200, "PUT", "/v1/settings/otel", map[string]any{"content": true})
	json.Unmarshal(m.dev.expect(200, "POST", "/v1/ai/agents/"+a.ID+"/chat", map[string]any{"message": "What failed?"}), &out)
	var got struct {
		Case  EvalCase `json:"case"`
		Agent string   `json:"agent"`
	}
	json.Unmarshal(m.dev.expect(200, "POST", "/v1/ai/evals/case-from-trace", map[string]any{"trace_id": out.TraceID}), &got)
	if got.Case.Input != "What failed?" || got.Agent != a.ID || len(got.Case.Checks) != 1 || !strings.Contains(got.Case.Checks[0].Value, "Plain answer.") {
		t.Errorf("case: %+v", got)
	}
}
