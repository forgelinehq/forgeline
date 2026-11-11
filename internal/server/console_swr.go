package server

import (
	"context"
	"sync"
	"time"
)

// swrCache keeps the last answer of a slow read (an AWS account's inventory takes tens of seconds on a large
// account) and answers from it at once:
//
//   - younger than fresh: the kept answer, nothing else happens;
//   - older, but younger than maxAge: the kept answer at once, and one refresh starts in the background;
//   - nothing kept, too old, or refresh asked for: the caller waits for a read.
//
// A read that fails is never kept, and it drops the answer kept before it.
//
// Only one read per key runs at a time; everyone who needs it waits for the same one. A read runs on bg, not on
// the request, so a person who navigates away does not throw away work the next visit needs.
type swrCache[T any] struct {
	mu      sync.Mutex
	entries map[string]*swrEntry[T]
	max     int // entries kept before the map is started afresh (memory bound)
}

type swrEntry[T any] struct {
	at     time.Time
	data   T
	have   bool
	flight *swrFlight[T]
}

type swrFlight[T any] struct {
	done chan struct{}
	data T
	ok   bool
}

type swrOpts struct {
	fresh, maxAge, timeout time.Duration
	bg                     context.Context // the server's life; nil means context.Background
	wg                     *sync.WaitGroup // counts background reads, so a stopping server waits for them
}

// get returns the answer for key, and when it was read. fetch reports whether its answer may be kept (a failed
// read is shown to whoever waited for it, never kept). err is set only when ctx ended while waiting.
func (c *swrCache[T]) get(ctx context.Context, key string, refresh bool, o swrOpts, fetch func(context.Context) (T, bool)) (T, time.Time, error) {
	c.mu.Lock()
	if c.entries == nil {
		c.entries = map[string]*swrEntry[T]{}
	}
	e := c.entries[key]
	if e == nil {
		if c.max > 0 && len(c.entries) >= c.max {
			c.entries = map[string]*swrEntry[T]{}
		}
		e = &swrEntry[T]{}
		c.entries[key] = e
	}
	age := time.Since(e.at)
	if e.have && !refresh && age < o.maxAge {
		data, at := e.data, e.at
		if age >= o.fresh && e.flight == nil {
			c.start(e, o, fetch)
		}
		c.mu.Unlock()
		return data, at, nil
	}
	f := e.flight
	if f == nil {
		f = c.start(e, o, fetch)
	}
	c.mu.Unlock()
	select {
	case <-f.done:
		return f.data, time.Now(), nil
	case <-ctx.Done():
		var zero T
		return zero, time.Time{}, ctx.Err()
	}
}

// peek returns the kept answer for key when there is one younger than maxAge, without starting a read.
func (c *swrCache[T]) peek(key string, maxAge time.Duration) (T, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e := c.entries[key]
	if e == nil || !e.have || time.Since(e.at) >= maxAge {
		var zero T
		return zero, false
	}
	return e.data, true
}

// clear forgets every kept answer (the AWS sign-in or settings changed). Reads in flight finish for their waiters
// but are not kept.
func (c *swrCache[T]) clear() {
	c.mu.Lock()
	c.entries = nil
	c.mu.Unlock()
}

// start begins a read for e. c.mu is held.
func (c *swrCache[T]) start(e *swrEntry[T], o swrOpts, fetch func(context.Context) (T, bool)) *swrFlight[T] {
	f := &swrFlight[T]{done: make(chan struct{})}
	e.flight = f
	bg := o.bg
	if bg == nil {
		bg = context.Background()
	}
	if o.wg != nil {
		o.wg.Add(1)
	}
	go func() {
		if o.wg != nil {
			defer o.wg.Done()
		}
		ctx, cancel := context.WithTimeout(bg, o.timeout)
		defer cancel()
		data, ok := fetch(ctx)
		c.mu.Lock()
		f.data, f.ok = data, ok
		if ok {
			e.data, e.at, e.have = data, time.Now(), true
		} else {
			// A failed read (an expired sign-in, a lost permission) must not leave an old answer on screen as if
			// all were well: the next visit waits for a read and shows what went wrong.
			e.have = false
		}
		e.flight = nil
		c.mu.Unlock()
		close(f.done)
	}()
	return f
}

// awsReadOpts: AWS reads stay fresh for a minute and are shown for up to 30 minutes while a newer one is read.
func (s *Server) awsReadOpts(timeout time.Duration) swrOpts {
	bg := s.baseCtx
	if bg == nil {
		bg = context.Background()
	}
	return swrOpts{fresh: inventoryTTL, maxAge: 30 * time.Minute, timeout: timeout + 15*time.Second, bg: bg, wg: &s.wg}
}

// forgetAWSReads drops every kept AWS read: the sign-in or the settings changed, so they may be of another account.
func (s *Server) forgetAWSReads() {
	s.awsCache.mu.Lock()
	s.awsCache.key, s.awsCache.cost = "", nil
	s.awsCache.mu.Unlock()
	s.awsSummary.clear()
	s.awsInv.clear()
	s.awsLinks.clear()
	s.awsAlarms.clear()
}
