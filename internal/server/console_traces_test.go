package server

import (
	"context"
	"encoding/json"
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
	json.Unmarshal(m.dev.expect(200, "GET", "/v1/traces?failed=1", nil), &list)
	if len(list.Traces) != 0 {
		t.Errorf("failed filter: %+v", list.Traces)
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
	all, _ := s.listTraces(context.Background())
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
	all, _ = s.listTraces(context.Background())
	if len(all) != 2 {
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
