// Package trace records what agents, workflows and the assistant do as spans (design note 049), in the shape
// OpenTelemetry uses, without its SDK. Spans travel in the context: a span started under another is its child, and
// when the outermost span of a tree ends, the whole tree is handed to the sink (Forgeline's store, and an OTLP
// exporter when one is set). It has no dependency beyond the standard library and never blocks on the sink's work
// beyond the call itself.
package trace

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"sync"
	"time"
)

// Kinds, as OTLP names them.
const (
	Internal = "internal"
	Client   = "client"
)

// Span is one finished operation.
type Span struct {
	TraceID  string         `json:"trace_id"`
	SpanID   string         `json:"span_id"`
	ParentID string         `json:"parent_id,omitempty"`
	Name     string         `json:"name"`
	Kind     string         `json:"kind"`
	Start    time.Time      `json:"start"`
	End      time.Time      `json:"end"`
	Error    string         `json:"error,omitempty"` // empty: it went well
	Attrs    map[string]any `json:"attrs,omitempty"` // string, int64, float64 or bool
}

// Sink receives every span of a tree once its outermost span has ended, parents before children.
type Sink func(spans []Span)

// Tracer starts spans. The zero value records nothing.
type Tracer struct{ sink Sink }

// New makes a tracer that hands finished trees to sink.
func New(sink Sink) *Tracer { return &Tracer{sink: sink} }

type tree struct {
	mu    sync.Mutex
	spans []Span
	open  int
}

// Active is a span that has not ended. Its methods are safe on a nil Active (tracing off).
type Active struct {
	t    *Tracer
	tr   *tree
	mu   sync.Mutex
	span Span
	done bool
}

type ctxKey struct{}

// From is the span the context is in, or nil.
func From(ctx context.Context) *Active {
	a, _ := ctx.Value(ctxKey{}).(*Active)
	return a
}

func randomID(n int) string {
	b := make([]byte, n)
	rand.Read(b)
	return hex.EncodeToString(b)
}

// IDFor is a trace id made from a stable key (a workflow run's id), so work done in several stretches (a run that
// waited for a person) joins one trace.
func IDFor(key string) string {
	h := sha256.Sum256([]byte("forgeline-trace:" + key))
	return hex.EncodeToString(h[:16])
}

// Option changes a span as it starts.
type Option func(*Span)

// WithTraceID starts a new tree in a known trace instead of a random one. Ignored for a child span.
func WithTraceID(id string) Option { return func(s *Span) { s.TraceID = id } }

// WithAttrs sets attributes as the span starts.
func WithAttrs(attrs map[string]any) Option {
	return func(s *Span) {
		for k, v := range attrs {
			s.Attrs[k] = v
		}
	}
}

// Start begins a span: a child of the span in ctx, or the start of a new tree.
func (t *Tracer) Start(ctx context.Context, name, kind string, opts ...Option) (context.Context, *Active) {
	if t == nil || t.sink == nil {
		return ctx, nil
	}
	s := Span{SpanID: randomID(8), Name: name, Kind: kind, Start: time.Now().UTC(), Attrs: map[string]any{}}
	parent := From(ctx)
	var tr *tree
	if parent != nil && parent.t == t {
		s.TraceID, s.ParentID, tr = parent.span.TraceID, parent.span.SpanID, parent.tr
	} else {
		tr = &tree{}
	}
	inherited := s.TraceID
	for _, o := range opts {
		if o != nil {
			o(&s)
		}
	}
	if inherited != "" {
		s.TraceID = inherited // a child keeps its parent's trace
	}
	if s.TraceID == "" {
		s.TraceID = randomID(16)
	}
	tr.mu.Lock()
	tr.open++
	tr.mu.Unlock()
	a := &Active{t: t, tr: tr, span: s}
	return context.WithValue(ctx, ctxKey{}, a), a
}

// Set records an attribute.
func (a *Active) Set(key string, value any) {
	if a == nil {
		return
	}
	a.mu.Lock()
	a.span.Attrs[key] = value
	a.mu.Unlock()
}

// Fail marks the span as failed, with what went wrong (an error type, never a secret).
func (a *Active) Fail(why string) {
	if a == nil || why == "" {
		return
	}
	a.mu.Lock()
	a.span.Error = why
	a.mu.Unlock()
}

// TraceID is the span's trace, or "" when tracing is off.
func (a *Active) TraceID() string {
	if a == nil {
		return ""
	}
	return a.span.TraceID
}

// End finishes the span. When it is the last open span of its tree, the tree goes to the sink.
func (a *Active) End() {
	if a == nil {
		return
	}
	a.mu.Lock()
	if a.done {
		a.mu.Unlock()
		return
	}
	a.done = true
	a.span.End = time.Now().UTC()
	s := a.span
	a.mu.Unlock()
	a.tr.add(a.t, s, true)
}

func (tr *tree) add(t *Tracer, s Span, closes bool) {
	tr.mu.Lock()
	tr.spans = append(tr.spans, s)
	if closes {
		tr.open--
	}
	var out []Span
	if tr.open == 0 {
		out = tr.spans
		tr.spans = nil
	}
	tr.mu.Unlock()
	if out != nil {
		// Parents first: the order spans started in.
		sortByStart(out)
		t.sink(out)
	}
}

// Done records a span that already happened (a model call timed elsewhere) under the span in ctx. With no span in
// ctx it is a tree of its own, in s.TraceID when that is set.
func (t *Tracer) Done(ctx context.Context, s Span) {
	if t == nil || t.sink == nil {
		return
	}
	s.SpanID = randomID(8)
	if s.Attrs == nil {
		s.Attrs = map[string]any{}
	}
	if p := From(ctx); p != nil && p.t == t {
		s.TraceID, s.ParentID = p.span.TraceID, p.span.SpanID
		p.tr.add(t, s, false)
		return
	}
	if s.TraceID == "" {
		s.TraceID = randomID(16)
	}
	t.sink([]Span{s})
}

func sortByStart(spans []Span) {
	for i := 1; i < len(spans); i++ {
		for j := i; j > 0 && spans[j].Start.Before(spans[j-1].Start); j-- {
			spans[j], spans[j-1] = spans[j-1], spans[j]
		}
	}
}
