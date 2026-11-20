package trace

import (
	"context"
	"sync"
	"testing"
	"time"
)

type collect struct {
	mu    sync.Mutex
	trees [][]Span
}

func (c *collect) sink(s []Span) {
	c.mu.Lock()
	c.trees = append(c.trees, s)
	c.mu.Unlock()
}

func TestATreeIsHandedOverWhenItsLastSpanEnds(t *testing.T) {
	c := &collect{}
	tr := New(c.sink)
	ctx, root := tr.Start(context.Background(), "invoke_agent Triage", Internal, WithAttrs(map[string]any{"gen_ai.agent.id": "triage"}))
	cctx, tool := tr.Start(ctx, "execute_tool list_runs", Internal)
	tr.Done(cctx, Span{Name: "chat claude", Kind: Client, Start: time.Now(), End: time.Now(), Attrs: map[string]any{"gen_ai.usage.input_tokens": int64(10)}})
	tool.Fail("tool_error")
	tool.End()
	if len(c.trees) != 0 {
		t.Fatal("handed over before the root ended")
	}
	root.Set("gen_ai.usage.input_tokens", int64(10))
	root.End()
	root.End() // twice is harmless
	if len(c.trees) != 1 || len(c.trees[0]) != 3 {
		t.Fatalf("trees: %+v", c.trees)
	}
	spans := c.trees[0]
	r, tl, m := spans[0], spans[1], spans[2]
	if r.ParentID != "" || tl.ParentID != r.SpanID || m.ParentID != tl.SpanID {
		t.Errorf("parents: %+v", spans)
	}
	for _, s := range spans {
		if s.TraceID != r.TraceID || len(s.TraceID) != 32 || len(s.SpanID) != 16 {
			t.Errorf("ids: %+v", s)
		}
	}
	if tl.Error != "tool_error" || r.Attrs["gen_ai.agent.id"] != "triage" || r.End.Before(r.Start) {
		t.Errorf("fields: %+v %+v", r, tl)
	}
}

func TestChildrenThatOutliveTheirParent(t *testing.T) {
	c := &collect{}
	tr := New(c.sink)
	ctx, root := tr.Start(context.Background(), "invoke_workflow W", Internal)
	var wg sync.WaitGroup
	for i := 0; i < 5; i++ {
		wg.Add(1)
		_, n := tr.Start(ctx, "node", Internal)
		go func() { defer wg.Done(); time.Sleep(time.Millisecond); n.End() }()
	}
	root.End()
	wg.Wait()
	if len(c.trees) != 1 || len(c.trees[0]) != 6 {
		t.Fatalf("got %d trees, first of %d", len(c.trees), len(c.trees[0]))
	}
}

func TestAKnownTraceAndTracingOff(t *testing.T) {
	c := &collect{}
	tr := New(c.sink)
	ctx, a := tr.Start(context.Background(), "w", Internal, WithTraceID(IDFor("run-1")))
	_, b := tr.Start(ctx, "child", Internal, WithTraceID("ignored"))
	b.End()
	a.End()
	if c.trees[0][0].TraceID != IDFor("run-1") || c.trees[0][1].TraceID != IDFor("run-1") || IDFor("run-1") == IDFor("run-2") {
		t.Errorf("trace ids: %+v", c.trees[0])
	}
	tr.Done(context.Background(), Span{Name: "lone"})
	if len(c.trees) != 2 || c.trees[1][0].ParentID != "" {
		t.Errorf("a span with no parent is its own tree: %+v", c.trees)
	}

	var off *Tracer
	ctx2, x := off.Start(context.Background(), "x", Internal)
	x.Set("a", 1)
	x.Fail("e")
	x.End()
	off.Done(ctx2, Span{})
	if x.TraceID() != "" || From(ctx2) != nil {
		t.Error("a nil tracer records nothing")
	}
}
