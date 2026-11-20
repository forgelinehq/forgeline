package server

import (
	"context"
	"encoding/json"
	"github.com/forgelinehq/forgeline/internal/scheduler"
	"github.com/forgelinehq/forgeline/internal/store"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/forgelinehq/forgeline/internal/docstore"
	"github.com/forgelinehq/forgeline/internal/trace"
)

// Traces (design note 049): every assistant chat, agent run and workflow run as a tree of spans, kept here and shown
// under Agents › Runs and traces. A trace holds names, models, timings, tokens, cost and errors; no prompt, answer,
// instructions or tool arguments. The person who started it is kept here, as the audit log does.

const (
	kindTrace     = "trace"
	maxTraces     = 1000
	traceMaxAge   = 14 * 24 * time.Hour
	maxTraceSpans = 2000 // a trace that grows past this keeps its first spans and says how many were dropped
)

// TraceDoc is one trace as it is stored.
type TraceDoc struct {
	ID       string       `json:"id"`
	Name     string       `json:"name"` // the outermost span's
	Kind     string       `json:"kind"` // assistant, agent, workflow or model
	Subject  string       `json:"subject,omitempty"`
	Who      string       `json:"who,omitempty"`
	Started  time.Time    `json:"started"`
	Ended    time.Time    `json:"ended"`
	Millis   int64        `json:"millis"`
	Failed   bool         `json:"failed"`
	Input    int64        `json:"input_tokens"`
	Output   int64        `json:"output_tokens"`
	CostUSD  float64      `json:"cost_usd"`
	Models   int          `json:"model_calls"`
	Tools    int          `json:"tool_calls"`
	Dropped  int          `json:"dropped,omitempty"`
	Spans    []trace.Span `json:"spans,omitempty"`
	SpanSize int          `json:"span_count"`
}

type traceStore struct {
	mu    sync.Mutex
	saves int
}

// Attribute names Forgeline sets (the gen_ai.* ones follow the OpenTelemetry GenAI conventions).
const (
	attrOp          = "gen_ai.operation.name"
	attrProvider    = "gen_ai.provider.name"
	attrModel       = "gen_ai.request.model"
	attrIn          = "gen_ai.usage.input_tokens"
	attrOut         = "gen_ai.usage.output_tokens"
	attrAgentID     = "gen_ai.agent.id"
	attrAgentName   = "gen_ai.agent.name"
	attrAgentVer    = "gen_ai.agent.version"
	attrTool        = "gen_ai.tool.name"
	attrToolType    = "gen_ai.tool.type"
	attrErrorType   = "error.type"
	attrCost        = "forgeline.cost_usd"
	attrUser        = "forgeline.user" // kept here; never exported (note 049, decision 3)
	attrRole        = "forgeline.role"
	attrKind        = "forgeline.trace.kind"
	attrSubject     = "forgeline.subject"
	attrWorkflowID  = "forgeline.workflow.id"
	attrWorkflowRun = "forgeline.workflow.run"
	attrNodeID      = "forgeline.node.id"
	attrNodeKind    = "forgeline.node.kind"
	attrNodeVisit   = "forgeline.node.visit"
)

func num(v any) (int64, bool) {
	switch x := v.(type) {
	case int64:
		return x, true
	case int:
		return int64(x), true
	case float64:
		return int64(x), true
	}
	return 0, false
}

// saveTrace is the tracer's sink: it adds a finished tree to its trace (a workflow run that waited comes back in
// several trees with the same id) and keeps the store within its limits.
func (s *Server) saveTrace(spans []trace.Span) {
	if len(spans) == 0 {
		return
	}
	ctx := context.Background()
	s.traces.mu.Lock()
	defer s.traces.mu.Unlock()
	id := spans[0].TraceID
	var d TraceDoc
	if err := s.docs.Get(ctx, kindTrace, id, &d); err != nil {
		d = TraceDoc{ID: id}
	}
	for _, sp := range spans {
		if len(d.Spans) >= maxTraceSpans {
			d.Dropped++
			continue
		}
		d.Spans = append(d.Spans, sp)
	}
	summarize(&d)
	if err := s.docs.Put(ctx, kindTrace, id, d); err != nil {
		s.log.Error("saving a trace", "trace", id, "err", err)
		return
	}
	if s.traces.saves++; s.traces.saves%50 == 1 {
		s.pruneTraces(ctx)
	}
}

// summarize works out a trace's row from its spans: the outermost span names it, tokens and cost add up over model
// calls, and any failed span marks it failed.
func summarize(d *TraceDoc) {
	d.Input, d.Output, d.CostUSD, d.Models, d.Tools, d.Failed = 0, 0, 0, 0, 0, false
	ids := map[string]bool{}
	for _, sp := range d.Spans {
		ids[sp.SpanID] = true
	}
	var root *trace.Span
	for i := range d.Spans {
		sp := &d.Spans[i]
		if sp.Error != "" {
			d.Failed = true
		}
		switch sp.Attrs[attrOp] {
		case "chat":
			d.Models++
			if n, ok := num(sp.Attrs[attrIn]); ok {
				d.Input += n
			}
			if n, ok := num(sp.Attrs[attrOut]); ok {
				d.Output += n
			}
			if c, ok := sp.Attrs[attrCost].(float64); ok {
				d.CostUSD += c
			}
		case "execute_tool":
			d.Tools++
		}
		if (sp.ParentID == "" || !ids[sp.ParentID]) && (root == nil || sp.Start.Before(root.Start)) {
			root = sp
		}
		if d.Started.IsZero() || sp.Start.Before(d.Started) {
			d.Started = sp.Start
		}
		if sp.End.After(d.Ended) {
			d.Ended = sp.End
		}
	}
	if root != nil {
		d.Name = root.Name
		d.Kind, _ = root.Attrs[attrKind].(string)
		if d.Kind == "" {
			d.Kind = "model"
		}
		d.Subject, _ = root.Attrs[attrSubject].(string)
		d.Who, _ = root.Attrs[attrUser].(string)
	}
	d.Millis = d.Ended.Sub(d.Started).Milliseconds()
	d.SpanSize = len(d.Spans)
}

// pruneTraces keeps the newest maxTraces and none older than traceMaxAge.
func (s *Server) pruneTraces(ctx context.Context) {
	all, err := s.listTraces(ctx)
	if err != nil {
		return
	}
	cutoff := time.Now().Add(-traceMaxAge)
	for i, d := range all {
		if i >= maxTraces || d.Started.Before(cutoff) {
			s.docs.Delete(ctx, kindTrace, d.ID)
		}
	}
}

// listTraces is every trace, newest first, without its spans.
func (s *Server) listTraces(ctx context.Context) ([]TraceDoc, error) {
	docs, err := s.docs.List(ctx, kindTrace, docstore.ListOptions{})
	if err != nil {
		return nil, err
	}
	all, err := docstore.Decode[TraceDoc](docs)
	if err != nil {
		return nil, err
	}
	for i := range all {
		all[i].Spans = nil
	}
	sort.Slice(all, func(i, j int) bool { return all[i].Started.After(all[j].Started) })
	return all, nil
}

func (s *Server) handleListTraces(w http.ResponseWriter, r *http.Request) {
	all, err := s.listTraces(r.Context())
	if err != nil {
		s.internalError(w, "reading the traces", err)
		return
	}
	q := r.URL.Query()
	kind, subject, failed := q.Get("kind"), q.Get("subject"), q.Get("failed") == "1"
	limit, _ := strconv.Atoi(q.Get("limit"))
	if limit < 1 || limit > 500 {
		limit = 100
	}
	out := []TraceDoc{}
	for _, d := range all {
		if (kind != "" && d.Kind != kind) || (subject != "" && d.Subject != subject) || (failed && !d.Failed) {
			continue
		}
		out = append(out, d)
		if len(out) >= limit {
			break
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"traces": out, "total": len(all)})
}

func (s *Server) handleGetTrace(w http.ResponseWriter, r *http.Request) {
	id := strings.ToLower(r.PathValue("id"))
	var d TraceDoc
	if err := s.docs.Get(r.Context(), kindTrace, id, &d); err != nil {
		s.defError(w, "trace", err)
		return
	}
	writeJSON(w, http.StatusOK, d)
}

// --- where spans are started ---

// startAgentSpan begins the span of an assistant chat or an agent run: invoke_agent, with who asked.
func (s *Server) startAgentSpan(ctx context.Context, p Principal) (context.Context, *trace.Active) {
	sc := scopeFrom(ctx)
	name, kind, subject := "Assistant", "assistant", ""
	attrs := map[string]any{attrOp: "invoke_agent", attrRole: string(p.Role), attrUser: p.Name}
	if sc != nil {
		name, kind, subject = sc.agentName, "agent", sc.agent
		attrs[attrAgentID] = sc.agent
		if _, ver, ok := strings.Cut(sc.label, "@"); ok {
			attrs[attrAgentVer] = ver
		}
	}
	attrs[attrAgentName], attrs[attrKind], attrs[attrSubject] = name, kind, subject
	// An agent run inside a workflow or by the assistant is part of that trace, so it is not a trace of its own.
	return s.tracer.Start(ctx, "invoke_agent "+name, trace.Internal, trace.WithAttrs(attrs))
}

// providerName is the gen_ai.provider.name of a call: the provider's kind, as the conventions name them.
func (s *Server) providerName(ctx context.Context, provider string) string {
	if backendNames[provider] {
		if provider == assistCopilot {
			return "github_copilot"
		}
		return "anthropic"
	}
	p, err := s.loadProvider(ctx, provider)
	if err != nil {
		return provider
	}
	switch p.Kind {
	case provGemini:
		return "gcp.gemini"
	case provBedrock:
		return "aws.bedrock"
	case provAzureOpenAI:
		return "azure.ai.openai"
	case provOpenAICompat:
		return "openai"
	}
	return p.Kind
}

// traceModelCall records a model call that just finished as a chat span under the span in ctx.
func (s *Server) traceModelCall(ctx context.Context, c ModelCall) {
	end := c.At
	if end.IsZero() {
		end = time.Now().UTC()
	}
	attrs := map[string]any{attrOp: "chat", attrProvider: s.providerName(ctx, c.Provider), attrModel: c.Model,
		attrIn: c.Input, attrOut: c.Output, attrCost: c.CostUSD, attrKind: "model", attrSubject: c.Who}
	sp := trace.Span{Name: "chat " + c.Model, Kind: trace.Client, Start: end.Add(-time.Duration(c.Millis) * time.Millisecond), End: end, Attrs: attrs}
	if c.Error != "" {
		sp.Error = "model_error"
		attrs[attrErrorType] = "model_error"
	}
	s.tracer.Done(ctx, sp)
}

// --- prompts and answers, only when an administrator asked for them (note 049, decision 3) ---

const (
	attrInput      = "gen_ai.input.messages"
	attrOutput     = "gen_ai.output.messages"
	attrToolArgs   = "gen_ai.tool.call.arguments"
	attrToolResult = "gen_ai.tool.call.result"
	maxTraceText   = 4096
)

type traceContentKey struct{}

// traceContent says whether traces keep prompts, answers and tool arguments and results.
func (s *Server) traceContent(ctx context.Context) bool { return s.loadOTel(ctx).Content }

// contentText is text as a trace may keep it: anything shaped like a credential and every stored secret's value
// hidden, then cut to 4 KB.
func (s *Server) contentText(ctx context.Context, text string) string {
	out, _ := hideSecrets(text)
	for _, v := range s.stepSecrets(ctx) {
		if len(v) >= 6 {
			out = strings.ReplaceAll(out, v, "[hidden]")
		}
	}
	return truncate(out, maxTraceText)
}

// messagesAttr is one message in the shape the GenAI conventions give gen_ai.input.messages and
// gen_ai.output.messages, as JSON.
func (s *Server) messagesAttr(ctx context.Context, role, text string) string {
	b, _ := json.Marshal([]map[string]any{{"role": role, "parts": []map[string]string{{"type": "text", "content": s.contentText(ctx, text)}}}})
	return string(b)
}

// --- teams, missions and pipelines (slice 3) ---

// startTeamSpan begins a team run's trace: the lead and its members' agent runs nest under it.
func (s *Server) startTeamSpan(ctx context.Context, p Principal, t Team, runID string) (context.Context, *trace.Active) {
	return s.tracer.Start(ctx, "team "+t.Name, trace.Internal, trace.WithTraceID(trace.IDFor("team:"+runID)), trace.WithAttrs(map[string]any{
		attrOp: "invoke_workflow", attrKind: "team", attrSubject: t.ID, attrAgentName: t.Name, attrUser: p.Name, attrRole: string(p.Role), "forgeline.team.run": runID}))
}

// startMissionSpan begins one stretch of a mission (it waits for approvals between stretches), in the mission's trace.
func (s *Server) startMissionSpan(ctx context.Context, m Mission) (context.Context, *trace.Active) {
	return s.tracer.Start(ctx, "mission "+truncate(m.Goal, 60), trace.Internal, trace.WithTraceID(trace.IDFor("mission:"+m.ID)), trace.WithAttrs(map[string]any{
		attrOp: "invoke_workflow", attrKind: "mission", attrSubject: m.ID, attrAgentName: m.AgentName, attrAgentID: m.Agent, attrUser: m.By, "forgeline.mission.stage": m.Stage}))
}

// startPipelineSpan begins a pipeline run's trace; each step is recorded as it finishes.
func (s *Server) startPipelineSpan(ctx context.Context, run store.Run) (context.Context, *trace.Active) {
	return s.tracer.Start(ctx, "pipeline "+run.PipelineName, trace.Internal, trace.WithTraceID(trace.IDFor("run:"+run.ID)), trace.WithAttrs(map[string]any{
		attrKind: "pipeline", attrSubject: run.PipelineName, "forgeline.run.id": run.ID, "forgeline.trigger": string(run.Trigger), "forgeline.source.sha": run.SourceSHA}))
}

// traceStep records a finished pipeline step under the run's span: when it started (or, for a step that never
// started, such as a cached or skipped one, a moment at the end), its status and exit code.
func (s *Server) traceStep(run *trace.Active, started map[string]time.Time, mu *sync.Mutex, r scheduler.StepResult) {
	if run == nil {
		return
	}
	end := time.Now().UTC()
	mu.Lock()
	start, ok := started[r.ID]
	mu.Unlock()
	if !ok {
		start = end.Add(-r.Duration)
	}
	sp := trace.Span{Name: "step " + r.ID, Kind: trace.Internal, Start: start, End: end,
		Attrs: map[string]any{"forgeline.step.id": r.ID, "forgeline.step.status": string(r.Status), "forgeline.step.exit_code": int64(r.ExitCode)}}
	if r.Status != scheduler.StatusSucceeded && r.Status != scheduler.StatusCached && r.Status != scheduler.StatusSkipped {
		sp.Error = "step_" + string(r.Status)
		sp.Attrs[attrErrorType] = sp.Error
	}
	s.tracer.Done(trace.ContextWith(context.Background(), run), sp)
}
