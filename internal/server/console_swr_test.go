package server

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestSWRCache(t *testing.T) {
	var c swrCache[int]
	var calls atomic.Int32
	gate := make(chan struct{})
	value, ok := atomic.Int32{}, atomic.Bool{}
	ok.Store(true)
	fetch := func(ctx context.Context) (int, bool) {
		calls.Add(1)
		select {
		case <-gate:
		case <-ctx.Done():
			return 0, false
		}
		return int(value.Load()), ok.Load()
	}
	var wg sync.WaitGroup
	o := swrOpts{fresh: time.Hour, maxAge: 2 * time.Hour, timeout: time.Minute, wg: &wg}

	// Three people ask at once: one read, and all three get it.
	value.Store(1)
	var got [3]int
	var asked sync.WaitGroup
	for i := range got {
		asked.Add(1)
		go func(i int) { defer asked.Done(); got[i], _, _ = c.get(context.Background(), "k", false, o, fetch) }(i)
	}
	time.Sleep(50 * time.Millisecond)
	close(gate)
	asked.Wait()
	if calls.Load() != 1 || got != [3]int{1, 1, 1} {
		t.Fatalf("calls = %d, got = %v", calls.Load(), got)
	}

	// Fresh: answered from what is kept, no read.
	if v, _, _ := c.get(context.Background(), "k", false, o, fetch); v != 1 || calls.Load() != 1 {
		t.Errorf("fresh: %d after %d reads", v, calls.Load())
	}

	// Stale: the old answer at once, and one read in the background that the next visit sees.
	o.fresh = 0
	value.Store(2)
	if v, _, _ := c.get(context.Background(), "k", false, o, fetch); v != 1 {
		t.Errorf("stale should answer with the kept value at once, got %d", v)
	}
	c.get(context.Background(), "k", false, o, fetch) // a read is already running: no second one
	wg.Wait()
	if calls.Load() != 2 {
		t.Errorf("one background read expected, got %d reads", calls.Load())
	}
	o.fresh = time.Hour
	if v, _, _ := c.get(context.Background(), "k", false, o, fetch); v != 2 {
		t.Errorf("after the background read: %d", v)
	}

	// Refresh waits for a new read.
	value.Store(3)
	if v, _, _ := c.get(context.Background(), "k", true, o, fetch); v != 3 {
		t.Errorf("refresh: %d", v)
	}

	// Someone who leaves stops waiting, but the read finishes and is kept for the next visit.
	gate = make(chan struct{})
	value.Store(4)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error)
	go func() { _, _, err := c.get(ctx, "k", true, o, fetch); done <- err }()
	time.Sleep(30 * time.Millisecond)
	cancel()
	if err := <-done; err == nil {
		t.Errorf("leaving should end the wait")
	}
	close(gate)
	wg.Wait()
	if v, _, _ := c.get(context.Background(), "k", false, o, fetch); v != 4 {
		t.Errorf("the read was not kept after the person left: %d", v)
	}

	// A failed read is shown to whoever waited and drops the old answer, so the next visit sees the failure.
	ok.Store(false)
	value.Store(-1)
	if v, _, _ := c.get(context.Background(), "k", true, o, fetch); v != -1 {
		t.Errorf("the failure was not shown: %d", v)
	}
	ok.Store(true)
	value.Store(5)
	if v, _, _ := c.get(context.Background(), "k", false, o, fetch); v != 5 {
		t.Errorf("an old answer survived a failed read: %d", v)
	}

	// Too old to show: wait for a read. clear forgets everything.
	o.maxAge = 0
	value.Store(6)
	if v, _, _ := c.get(context.Background(), "k", false, o, fetch); v != 6 {
		t.Errorf("too old: %d", v)
	}
	o.maxAge = time.Hour
	c.clear()
	value.Store(7)
	if v, _, _ := c.get(context.Background(), "k", false, o, fetch); v != 7 {
		t.Errorf("after clear: %d", v)
	}
}
