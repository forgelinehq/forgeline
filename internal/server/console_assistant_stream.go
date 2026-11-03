package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"
)

// The assistant as an event stream (design note 024). POST /v1/assistant/runs takes the same body as
// /v1/assistant/chat and answers with server-sent events that follow the AG-UI vocabulary
// (RUN_STARTED, TOOL_CALL_*, TEXT_MESSAGE_*, CUSTOM, RUN_FINISHED, RUN_ERROR). It runs the same
// lookup loop with the same checks, limits and audit entries; it only shows the steps as they
// happen. What the assistant may do is unchanged. The backends return a whole answer, so the answer
// arrives as one TEXT_MESSAGE_CONTENT, not word by word.

const previewMax = 2048 // bytes of a lookup result shown in a step card
const argsMax = 400     // bytes of a lookup's arguments shown in a step card

const (
	evToolStart  = "tool_start"
	evToolResult = "tool_result"
)

// assistEvent is a step of the lookup loop, reported to whoever is watching it.
type assistEvent struct {
	Kind    string
	ID      string // identifies one lookup across its start and its result
	Tool    string
	Args    string // a short, readable form of the arguments
	Content string // the result, cut, or the reason it was refused
	OK      bool
}

type assistObserverKey struct{}
type assistObserver func(assistEvent)

// lookupObserver is the watcher the request carries, or nil.
func lookupObserver(ctx context.Context) assistObserver {
	f, _ := ctx.Value(assistObserverKey{}).(assistObserver)
	return f
}

// argsPreview is what a step card shows of a lookup's arguments. A call_aws command is shown the way
// the audit log records it; other arguments are shown as the compact JSON the model wrote, cut.
func argsPreview(c lookupCall) string {
	if c.Tool == "call_aws" {
		return truncate(lookupDetail(c), argsMax)
	}
	var buf strings.Builder
	var v any
	if json.Unmarshal(c.Args, &v) == nil {
		if b, err := json.Marshal(v); err == nil {
			buf.Write(b)
		}
	}
	if buf.String() == "{}" || buf.Len() == 0 {
		return ""
	}
	return truncate(buf.String(), argsMax)
}

// sseStream writes AG-UI style events. One goroutine keeps the connection alive while the model is
// thinking, so writes are serialised.
type sseStream struct {
	mu sync.Mutex
	w  http.ResponseWriter
	rc *http.ResponseController
}

func (e *sseStream) send(typ string, fields map[string]any) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if fields == nil {
		fields = map[string]any{}
	}
	fields["type"] = typ
	fields["timestamp"] = time.Now().UnixMilli()
	b, _ := json.Marshal(fields)
	fmt.Fprintf(e.w, "data: %s\n\n", b)
	_ = e.rc.Flush()
}

func (e *sseStream) keepAlive() {
	e.mu.Lock()
	defer e.mu.Unlock()
	fmt.Fprint(e.w, ": keep-alive\n\n")
	_ = e.rc.Flush()
}

func (s *Server) handleAssistantRun(w http.ResponseWriter, r *http.Request) {
	job, ok := s.prepareChat(w, r)
	if !ok {
		return
	}
	h := w.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("Cache-Control", "no-cache")
	h.Set("X-Accel-Buffering", "no") // ask reverse proxies not to buffer the stream
	w.WriteHeader(http.StatusOK)
	out := &sseStream{w: w, rc: http.NewResponseController(w)}
	_ = out.rc.Flush()

	done := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		t := time.NewTicker(keepAliveEvery)
		defer t.Stop()
		for {
			select {
			case <-t.C:
				out.keepAlive()
			case <-done:
				return
			}
		}
	}()
	defer func() { close(done); wg.Wait() }()

	runID := randomID()
	thread := "assistant"
	// A person with an account keeps a history. The conversation id is the one they sent, if it is theirs.
	conv := job.conv
	if job.p.UserID != "" {
		if _, mine := s.getConversation(r.Context(), job.p.UserID, conv); !mine {
			var other Conversation
			if conv == "" || !conversationIDRE.MatchString(conv) || s.docs.Get(r.Context(), kindChat, conv, &other) == nil {
				conv = randomID() // none, malformed, or someone else's: start a new one
			}
		}
		thread = conv
	}
	out.send("RUN_STARTED", map[string]any{"threadId": thread, "runId": runID})
	out.send("CUSTOM", map[string]any{"name": "forgeline.context", "value": map[string]any{"note": job.ctxNote, "role": string(job.p.Role)}})

	sink := &lookupSink{}
	sentP, sentE, sentN := 0, 0, 0
	obs := assistObserver(func(ev assistEvent) {
		switch ev.Kind {
		case evToolStart:
			out.send("TOOL_CALL_START", map[string]any{"toolCallId": ev.ID, "toolCallName": ev.Tool})
			out.send("TOOL_CALL_ARGS", map[string]any{"toolCallId": ev.ID, "delta": ev.Args})
			out.send("TOOL_CALL_END", map[string]any{"toolCallId": ev.ID})
		case evToolResult:
			out.send("TOOL_CALL_RESULT", map[string]any{"messageId": ev.ID + "-result", "toolCallId": ev.ID, "role": "tool", "content": ev.Content})
			out.send("CUSTOM", map[string]any{"name": "forgeline.tool_status", "value": map[string]any{"toolCallId": ev.ID, "ok": ev.OK}})
			// A lookup may have filed a proposal or asked for an environment.
			for ; sentP < len(sink.proposals); sentP++ {
				out.send("CUSTOM", map[string]any{"name": "forgeline.proposal", "value": map[string]any{"id": sink.proposals[sentP]}})
			}
			for ; sentE < len(sink.environments); sentE++ {
				out.send("CUSTOM", map[string]any{"name": "forgeline.environment", "value": map[string]any{"id": sink.environments[sentE]}})
			}
			for ; sentN < len(sink.notes); sentN++ {
				n := sink.notes[sentN]
				out.send("CUSTOM", map[string]any{"name": "forgeline.memory", "value": map[string]any{"id": n.ID, "text": n.Text, "about": n.About, "status": n.Status}})
			}
		}
	})
	ctx := context.WithValue(context.WithValue(r.Context(), lookupSinkKey{}, sink), assistObserverKey{}, obs)
	// A person is chatting: the assistant may use and add to memory. The default overview (run ids, pipeline names, counts) is clean; a run, a pipeline or the infrastructure attached as context is text they did not type.
	ctx = context.WithValue(ctx, memTurnKey{}, &memTurn{p: job.p, question: job.message, dirty: job.ctxNote != "overview"})
	// A person is present on this stream, so a tool that needs approval can ask them, here, and wait.
	ctx = context.WithValue(ctx, approverKey{}, approver(func(c lookupCall) error {
		return s.askToApprove(r.Context(), job.p, c, func(id string, wait time.Duration) {
			out.send("CUSTOM", map[string]any{"name": "forgeline.approval", "value": map[string]any{
				"id": id, "tool": c.Tool, "args": argsPreview(c), "integration": integrationOfTool(c.Tool), "expiresInSeconds": int(wait.Seconds())}})
		}, func(id, decision string) {
			out.send("CUSTOM", map[string]any{"name": "forgeline.approval_decided", "value": map[string]any{"id": id, "decision": decision}})
		})
	}))
	reply, lookups, err := s.askWithLookups(ctx, job.p, job.turns, job.ctxText, func(tool string, ok bool) {
		s.auditReq(r, "assistant.lookup", "assistant", tool, ok)
	})
	s.auditReq(r, "assistant.chat", "assistant", fmt.Sprintf("%s, %d chars asked, %d lookups, %v", job.ctxNote, job.asked, len(lookups), err == nil), err == nil)
	if err != nil {
		out.send("RUN_ERROR", map[string]any{"message": err.Error()})
		return
	}
	var refs []chatRef
	for _, id := range sink.proposals {
		refs = append(refs, chatRef{Kind: "proposal", ID: id})
	}
	for _, id := range sink.environments {
		refs = append(refs, chatRef{Kind: "environment", ID: id})
	}
	s.saveTurn(r.Context(), job.p, conv, job.message, reply, lookups, refs)
	msgID := randomID()
	out.send("TEXT_MESSAGE_START", map[string]any{"messageId": msgID, "role": "assistant"})
	out.send("TEXT_MESSAGE_CONTENT", map[string]any{"messageId": msgID, "delta": reply})
	out.send("TEXT_MESSAGE_END", map[string]any{"messageId": msgID})
	out.send("RUN_FINISHED", map[string]any{"threadId": thread, "runId": runID, "result": map[string]any{"context": job.ctxNote, "lookups": lookups, "conversation": conv}})
}
