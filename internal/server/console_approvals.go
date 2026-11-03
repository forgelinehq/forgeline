package server

import (
	"context"
	"errors"
	"net/http"
	"sync"
	"time"
)

// Needs approval for read tools (design note 026, slice 2). When the access policy says a read tool needs
// approval, a run that has a person on its stream stops before the tool, shows them the call as a card, and
// waits. Only the person who asked can answer, only over their own signed-in session, and only for the
// question that is waiting; no answer in two minutes is a denial. A run that has nobody present (an agent, an
// MCP client, a plain chat request) cannot ask, so the tool is refused there.

var approvalTimeout = 2 * time.Minute

type approver func(c lookupCall) error
type approverKey struct{}

// approverFrom is the way to ask a person, or nil where nobody is present. An agent run never has one, even
// when it was started from a chat that does: an agent acts on its own tool list and cannot borrow the
// person's presence for a tool they did not see.
func approverFrom(ctx context.Context) approver {
	if scopeFrom(ctx) != nil {
		return nil
	}
	a, _ := ctx.Value(approverKey{}).(approver)
	return a
}

type pendingApproval struct {
	who    string
	tool   string
	answer chan string // approve or deny
}

type approvalBook struct {
	mu      sync.Mutex
	pending map[string]*pendingApproval
}

func (b *approvalBook) add(p *pendingApproval) string {
	id := randomID()
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.pending == nil {
		b.pending = map[string]*pendingApproval{}
	}
	b.pending[id] = p
	return id
}

func (b *approvalBook) remove(id string) {
	b.mu.Lock()
	delete(b.pending, id)
	b.mu.Unlock()
}

// resolve hands an answer to the waiting call. It reports whether there was one for this person.
func (b *approvalBook) resolve(id, who, decision string) bool {
	b.mu.Lock()
	p, ok := b.pending[id]
	if ok && p.who == who {
		delete(b.pending, id)
	} else {
		ok = false
	}
	b.mu.Unlock()
	if ok {
		p.answer <- decision // buffered: the waiter may already have given up
	}
	return ok
}

// askToApprove shows the call to the person and waits for their answer. It returns nil only for an approval.
func (s *Server) askToApprove(ctx context.Context, p Principal, c lookupCall, shown func(id string, wait time.Duration), decided func(id, decision string)) error {
	pa := &pendingApproval{who: p.Name, tool: c.Tool, answer: make(chan string, 1)}
	id := s.approvals.add(pa)
	defer s.approvals.remove(id)
	shown(id, approvalTimeout)
	t := time.NewTimer(approvalTimeout)
	defer t.Stop()
	select {
	case d := <-pa.answer:
		decided(id, d)
		if d == "approve" {
			return nil
		}
		return errors.New(c.Tool + " was denied by " + p.Name + " when asked")
	case <-t.C:
		decided(id, "expired")
		return errors.New("nobody approved " + c.Tool + " in time, so it did not run")
	case <-ctx.Done():
		return errors.New("the person left before approving " + c.Tool + ", so it did not run")
	}
}

func (s *Server) handleAssistantApproval(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Decision string `json:"decision"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	if req.Decision != "approve" && req.Decision != "deny" {
		writeError(w, http.StatusBadRequest, "decision must be approve or deny")
		return
	}
	p := principalFrom(r.Context())
	if !s.approvals.resolve(r.PathValue("id"), p.Name, req.Decision) {
		writeError(w, http.StatusNotFound, "no question of yours is waiting for that answer (it may have expired)")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"decision": req.Decision})
}
