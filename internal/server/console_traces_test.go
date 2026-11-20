package server

import (
	"context"
	"encoding/json"
	"github.com/forgelinehq/forgeline/internal/executor"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/forgelinehq/forgeline/internal/trace"
)

func spanNamed(d TraceDoc, prefix string) *trace.Span {
	for i := range d.Spans {
		if strings.HasPrefix(d.Spans[i].Name, prefix) {
			return &d.Spans[i]
		}
	}
	return nil
}

// An agent chat is one trace (note 049): the agent's run, its model calls and its tool call, nested, with the GenAI
// attribute names; the reply says which trace; no prompt or answer is kept.
func TestAnAgentChatIsATrace(t *testing.T) {
	m := newMCPHarness(t)
	a := newAgent(t, m.admin, agentBody("Reviewer", nil, []string{"list_runs"}, "T0"))
	m.llm.script("```forgeline-lookup\n{\"tool\":\"list_runs\",\"args\":{}}\n```", "SECRET-ANSWER-TEXT")
	var out struct {
		TraceID string `json:"trace_id"`
	}
	json.Unmarshal(m.dev.expect(200, "POST", "/v1/ai/agents/"+a.ID+"/chat", map[string]any{"message": "PRIVATE-QUESTION"}), &out)
	if len(out.TraceID) != 32 {
		t.Fatalf("the reply names no trace: %+v", out)
	}
	var d TraceDoc
	raw := m.dev.expect(200, "GET", "/v1/traces/"+out.TraceID, nil)
	json.Unmarshal(raw, &d)
	root, tool, chat := spanNamed(d, "invoke_agent Reviewer"), spanNamed(d, "execute_tool list_runs"), spanNamed(d, "chat ")
	if root == nil || tool == nil || chat == nil {
		t.Fatalf("spans: %+v", d.Spans)
	}
	if root.ParentID != "" || tool.ParentID != root.SpanID || chat.ParentID != root.SpanID {
		t.Errorf("nesting: root %s, tool parent %s, chat parent %s", root.SpanID, tool.ParentID, chat.ParentID)
	}
	if root.Attrs[attrAgentID] != a.ID || root.Attrs[attrOp] != "invoke_agent" || root.Attrs[attrAgentVer] != "1" || tool.Attrs[attrTool] != "list_runs" || chat.Attrs[attrOp] != "chat" {
		t.Errorf("attributes: %+v / %+v / %+v", root.Attrs, tool.Attrs, chat.Attrs)
	}
	if d.Kind != "agent" || d.Subject != a.ID || d.Who != "dev" || d.Models != 2 || d.Tools != 1 || d.Failed {
		t.Errorf("summary: %+v", d)
	}
	if strings.Contains(string(raw), "PRIVATE-QUESTION") || strings.Contains(string(raw), "SECRET-ANSWER-TEXT") {
		t.Error("a trace keeps no prompt or answer")
	}

	// The list, filtered; viewers cannot read traces.
	var list struct {
		Traces []TraceDoc `json:"traces"`
	}
	json.Unmarshal(m.dev.expect(200, "GET", "/v1/traces?kind=agent&subject="+a.ID, nil), &list)
	if len(list.Traces) != 1 || list.Traces[0].ID != out.TraceID || list.Traces[0].Spans != nil {
		t.Errorf("list: %+v", list.Traces)
	}
	var failed struct {
		Traces []TraceDoc `json:"traces"`
	}
	json.Unmarshal(m.dev.expect(200, "GET", "/v1/traces?kind=agent&failed=1", nil), &failed)
	if len(failed.Traces) != 0 {
		t.Errorf("failed filter: %+v", failed.Traces)
	}
	m.v.expect(403, "GET", "/v1/traces", nil)
	m.v.expect(403, "GET", "/v1/traces/"+out.TraceID, nil)
}

// A tool that fails marks its span and the trace as failed.
func TestAFailedToolFailsTheTrace(t *testing.T) {
	m := newMCPHarness(t)
	a := newAgent(t, m.admin, agentBody("Reviewer", nil, []string{"get_run"}, "T0"))
	m.llm.script("```forgeline-lookup\n{\"tool\":\"get_run\",\"args\":{\"id\":\"nope\"}}\n```", "done")
	var out struct {
		TraceID string `json:"trace_id"`
	}
	json.Unmarshal(m.dev.expect(200, "POST", "/v1/ai/agents/"+a.ID+"/chat", map[string]any{"message": "go"}), &out)
	var d TraceDoc
	json.Unmarshal(m.dev.expect(200, "GET", "/v1/traces/"+out.TraceID, nil), &d)
	tool := spanNamed(d, "execute_tool get_run")
	if tool == nil || tool.Error != "tool_error" || tool.Attrs[attrErrorType] != "tool_error" || !d.Failed {
		t.Errorf("failed tool: %+v %+v", tool, d)
	}
}

// Old and extra traces are removed.
func TestTracesArePruned(t *testing.T) {
	m := newMCPHarness(t)
	s := m.srv
	for i := 0; i < 3; i++ {
		_, sp := s.tracer.Start(context.Background(), "invoke_agent X", trace.Internal)
		sp.End()
	}
	mine := func() []TraceDoc {
		all, _ := s.listTraces(context.Background())
		var out []TraceDoc
		for _, d := range all {
			if d.Name == "invoke_agent X" {
				out = append(out, d)
			}
		}
		return out
	}
	all := mine()
	if len(all) != 3 {
		t.Fatalf("got %d traces", len(all))
	}
	old := all[2]
	var d TraceDoc
	s.docs.Get(context.Background(), kindTrace, old.ID, &d)
	for i := range d.Spans {
		d.Spans[i].Start = d.Spans[i].Start.AddDate(0, 0, -15)
	}
	summarize(&d)
	s.docs.Put(context.Background(), kindTrace, d.ID, d)
	s.pruneTraces(context.Background())
	if all = mine(); len(all) != 2 {
		t.Errorf("a trace older than 14 days was kept: %d", len(all))
	}
}

// A workflow run is one trace across the time it waited for a person: a span per stretch, a span per node visit,
// and the agent's run, model call inside its node.
func TestAWorkflowRunIsOneTrace(t *testing.T) {
	m := newMCPHarness(t)
	a := newAgent(t, m.admin, agentBody("Worker", nil, nil, "T0"))
	w := newWorkflow(t, m.admin, graphBody("Ask then work", []WFNode{{ID: "start", Kind: nodeStart},
		{ID: "ok", Kind: nodeAsk, Question: "Go ahead?", Choices: []string{"go", "stop"}},
		agentNode("work", a.ID, "do it"), {ID: "done", Kind: nodeEnd}, {ID: "halt", Kind: nodeEnd, Outcome: wfFailed}},
		[]WFEdge{wire("start", "ok"), branchWire("ok", "work", "go"), branchWire("ok", "halt", "stop"), wire("work", "done")}))
	m.llm.script("WORKED")
	id := m.startWF(m.dev, w.ID, 202).ID
	m.waitRun(id, wfWaiting)
	m.admin.expect(200, "POST", "/v1/ai/workflow-runs/"+id+"/answer", map[string]any{"answer": "go"})
	m.waitRun(id, wfSucceeded, wfFailed)
	var run WFRun
	json.Unmarshal(m.dev.expect(200, "GET", "/v1/ai/workflow-runs/"+id, nil), &run)
	if run.TraceID != trace.IDFor(id) {
		t.Fatalf("trace id: %q", run.TraceID)
	}
	// The second stretch's span ends just after the run is saved as finished.
	var d TraceDoc
	for i := 0; i < 100; i++ {
		d = TraceDoc{}
		json.Unmarshal(m.dev.expect(200, "GET", "/v1/traces/"+run.TraceID, nil), &d)
		if d.Kind == "workflow" && strings.Count(namesOf(d), "invoke_workflow ") == 2 && spanNamed(d, "chat ") != nil {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	names := namesOf(d)
	work, agent, chat := spanNamed(d, "agent work"), spanNamed(d, "invoke_agent Worker"), spanNamed(d, "chat ")
	if strings.Count(names, "invoke_workflow Ask then work") != 2 || spanNamed(d, "ask ok") == nil || work == nil || agent == nil || chat == nil {
		t.Fatalf("spans: %s", names)
	}
	if agent.ParentID != work.SpanID || chat.ParentID != agent.SpanID || work.Attrs[attrNodeID] != "work" {
		t.Errorf("nesting or attributes: work %+v, agent %+v, chat %+v", work, agent, chat)
	}
	if d.Subject != w.ID || d.Failed || d.Who != "dev" {
		t.Errorf("summary: %+v", d)
	}
}

func namesOf(d TraceDoc) string {
	var b strings.Builder
	for _, s := range d.Spans {
		b.WriteString(s.Name + "; ")
	}
	return b.String()
}

// waitTrace reads a trace until check accepts it (a run's outermost span ends just after the run is saved).
func waitTrace(t *testing.T, s *Server, id string, check func(TraceDoc) bool) TraceDoc {
	t.Helper()
	var d TraceDoc
	for i := 0; i < 150; i++ {
		d = TraceDoc{}
		if err := s.docs.Get(context.Background(), kindTrace, id, &d); err == nil && check(d) {
			return d
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("trace %s never became ready: %s", id, namesOf(d))
	return d
}

// A pipeline run is a trace: the run and a span per step, failed steps marked (note 049, slice 3).
func TestAPipelineRunIsATrace(t *testing.T) {
	fake := &executor.Fake{Handler: func(_ context.Context, s executor.StepRun, logs io.Writer) (executor.Result, error) {
		if s.ID == "test" {
			return executor.Result{ExitCode: 3}, nil
		}
		return executor.Result{}, nil
	}}
	h := newHarness(t, fake)
	_, run := h.submit(twoSteps, nil)
	h.waitStatus(run.ID, "failed")
	d := waitTrace(t, h.srv, trace.IDFor("run:"+run.ID), func(d TraceDoc) bool { return spanNamed(d, "pipeline ") != nil })
	root, build, test := spanNamed(d, "pipeline "), spanNamed(d, "step build"), spanNamed(d, "step test")
	if root == nil || build == nil || test == nil || build.ParentID != root.SpanID || test.ParentID != root.SpanID {
		t.Fatalf("spans: %s", namesOf(d))
	}
	if build.Error != "" || test.Error == "" || test.Attrs["forgeline.step.exit_code"] != float64(3) || root.Error != "run_failed" || d.Kind != "pipeline" || !d.Failed {
		t.Errorf("statuses: build %+v test %+v root %+v", build, test, root)
	}
}

// With "keep prompts and answers" on, a trace has the question, the answer and the tool's arguments and result,
// with anything shaped like a credential and every stored secret hidden. Off (the default) it has none of them.
func TestTracesKeepContentOnlyWhenAsked(t *testing.T) {
	m := newMCPHarness(t)
	m.admin.expect(201, "PUT", "/v1/secrets/DB_PASSWORD", map[string]any{"value": "hunter2-very-secret"})
	a := newAgent(t, m.admin, agentBody("Reviewer", nil, []string{"list_runs"}, "T0"))
	ask := func() TraceDoc {
		m.llm.script("```forgeline-lookup\n{\"tool\":\"list_runs\",\"args\":{\"limit\":3}}\n```", "The answer mentions hunter2-very-secret.")
		var out struct {
			TraceID string `json:"trace_id"`
		}
		json.Unmarshal(m.dev.expect(200, "POST", "/v1/ai/agents/"+a.ID+"/chat", map[string]any{"message": "QUESTION with AKIAABCDEFGHIJKLMNOP"}), &out)
		var d TraceDoc
		json.Unmarshal(m.dev.expect(200, "GET", "/v1/traces/"+out.TraceID, nil), &d)
		return d
	}
	if d := ask(); spanNamed(d, "invoke_agent").Attrs[attrInput] != nil || spanNamed(d, "execute_tool").Attrs[attrToolArgs] != nil {
		t.Fatal("content was kept with the setting off")
	}
	m.admin.expect(200, "PUT", "/v1/settings/otel", map[string]any{"content": true})
	d := ask()
	root, tool := spanNamed(d, "invoke_agent"), spanNamed(d, "execute_tool")
	in, _ := root.Attrs[attrInput].(string)
	out, _ := root.Attrs[attrOutput].(string)
	args, _ := tool.Attrs[attrToolArgs].(string)
	if !strings.Contains(in, "QUESTION") || !strings.Contains(in, `"role":"user"`) || !strings.Contains(out, "The answer mentions") || !strings.Contains(args, `"limit":3`) || tool.Attrs[attrToolResult] == nil {
		t.Errorf("content: in %q out %q args %q", in, out, args)
	}
	if strings.Contains(in, "AKIAABCDEFGHIJKLMNOP") || strings.Contains(out, "hunter2-very-secret") || !strings.Contains(out, "[hidden]") {
		t.Errorf("a credential or secret was kept: in %q out %q", in, out)
	}
}
