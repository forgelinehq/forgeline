package server

import (
	"context"
	"fmt"
	"net/http"
	"sync"
	"time"
)

// Autopilot (design note 052, slice 4). A person switches it on for one conversation and gives it an envelope: how long
// and how many steps. Inside the envelope the assistant keeps working without stopping to ask, and a change that only
// asked because nothing said otherwise runs without a card. It never widens what is allowed: a protected account, a deny
// or an "ask" rule, the services that always ask, a secret handed to the sandbox, and agents all stay as they were.
// It lives in memory only, so a restart ends it, and it belongs to one conversation.

// There is no default limit: autopilot runs until the person stops it. A person may set a number of minutes or steps, and
// then it ends when either is used up. The caps only guard against a typo.
const (
	maxAutopilotMinutes = 7 * 24 * 60
	maxAutopilotSteps   = 100000
)

type autopilotKey struct{}

// autopilotRequest is what a chat message carries while the switch is on.
type autopilotRequest struct {
	Minutes int `json:"minutes"`
	Steps   int `json:"steps"`
}

type autopilot struct {
	mu      sync.Mutex
	key     string
	until   time.Time // zero: no time limit
	steps   int       // zero: no step limit
	used    int
	stopped bool
	notify  func(v map[string]any) // tells the person's screen; nil when nobody is watching
}

var (
	autopilotMu sync.Mutex
	autopilots  = map[string]*autopilot{}
)

func autopilotKeyFor(p Principal, conv string) string { return p.UserID + "|" + p.Name + "|" + conv }

// autopilotFrom is the autopilot this turn runs under, or nil. An exhausted or stopped one counts as none for changes.
func autopilotFrom(ctx context.Context) *autopilot {
	a, _ := ctx.Value(autopilotKey{}).(*autopilot)
	return a
}

func (a *autopilot) active() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.stopped == false && (a.until.IsZero() || time.Now().Before(a.until)) && (a.steps == 0 || a.used < a.steps)
}

// ended says why it is not active, or "".
func (a *autopilot) ended() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	switch {
	case a.stopped:
		return "stopped"
	case !a.until.IsZero() && !time.Now().Before(a.until):
		return "time"
	case a.steps > 0 && a.used >= a.steps:
		return "steps"
	}
	return ""
}

func (a *autopilot) spend() {
	a.mu.Lock()
	a.used++
	a.mu.Unlock()
	a.tell("on")
}

func (a *autopilot) tell(state string) { a.send(state, "", "") }

// announce shows the person a change that ran without a card, so nothing done for them is out of sight.
func (a *autopilot) announce(command, account string) { a.send("on", command, account) }

func (a *autopilot) send(state, command, account string) {
	a.mu.Lock()
	v := map[string]any{"state": state, "used": a.used, "steps": a.steps, "untilMs": untilMs(a.until)}
	f := a.notify
	a.mu.Unlock()
	if command != "" {
		v["action"], v["account"] = command, account
	}
	if f != nil {
		f(v)
	}
}

func (a *autopilot) stop() {
	a.mu.Lock()
	a.stopped = true
	a.mu.Unlock()
}

// startAutopilot returns the conversation's running autopilot, or starts one. started says which.
func startAutopilot(key string, req autopilotRequest) (a *autopilot, started bool) {
	autopilotMu.Lock()
	defer autopilotMu.Unlock()
	if cur := autopilots[key]; cur != nil && cur.active() {
		return cur, false
	}
	a = &autopilot{key: key}
	if req.Minutes > 0 {
		a.until = time.Now().Add(time.Duration(min(req.Minutes, maxAutopilotMinutes)) * time.Minute)
	}
	if req.Steps > 0 {
		a.steps = min(req.Steps, maxAutopilotSteps)
	}
	autopilots[key] = a
	return a, true
}

// endAutopilot stops and forgets the conversation's autopilot; it reports whether there was one.
func endAutopilot(key string) bool {
	autopilotMu.Lock()
	defer autopilotMu.Unlock()
	a := autopilots[key]
	if a == nil {
		return false
	}
	a.stop()
	delete(autopilots, key)
	return true
}

func untilMs(t time.Time) int64 {
	if t.IsZero() {
		return 0
	}
	return t.UnixMilli()
}

type autopilotConvKey struct{}

// liveAutopilot is the person's autopilot for this conversation as it is now, even if they switched it on while a message
// was already being worked on.
func liveAutopilot(ctx context.Context, p Principal) *autopilot {
	conv, _ := ctx.Value(autopilotConvKey{}).(string)
	if conv == "" {
		return nil
	}
	autopilotMu.Lock()
	defer autopilotMu.Unlock()
	if a := autopilots[autopilotKeyFor(p, conv)]; a != nil && a.active() {
		return a
	}
	return nil
}

// autopilotGuidance is what the model is told while it runs.
func autopilotGuidance(a *autopilot) string {
	return "\n\nAutopilot is on for this conversation, with no limit unless the person set one. Work on the task until it is done without stopping to ask: choose the sensible option, say in the answer what you chose, and keep going. Ask the person only when you are blocked on something only they know. A change the permission check allows runs without a card; a change to a protected account, to IAM, Organizations, accounts, SSM or STS, or one a rule forbids, still needs the person or a proposal, so do not try to get around that. When a tool is missing, install or write one in the sandbox."
}

func (s *Server) autopilotAllowed(ctx context.Context) bool {
	d, err := s.loadPermissions(ctx)
	return err == nil && !d.AutopilotOff
}

type autopilotJSON struct {
	Allowed bool `json:"allowed"`
}

func (s *Server) handleGetAutopilot(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, autopilotJSON{Allowed: s.autopilotAllowed(r.Context())})
}

// handleStartAutopilot switches autopilot on for a conversation that is already open, including while a message is being
// worked on: the next thing it would ask about runs, and what is waiting for an answer now is answered.
func (s *Server) handleStartAutopilot(w http.ResponseWriter, r *http.Request) {
	var q struct {
		Conversation string `json:"conversation"`
		Minutes      int    `json:"minutes"`
		Steps        int    `json:"steps"`
	}
	if !readJSON(w, r, &q) {
		return
	}
	if !s.autopilotAllowed(r.Context()) {
		writeError(w, http.StatusForbidden, "an administrator has turned autopilot off")
		return
	}
	p := principalFrom(r.Context())
	a, started := startAutopilot(autopilotKeyFor(p, q.Conversation), autopilotRequest{Minutes: q.Minutes, Steps: q.Steps})
	if started {
		s.auditReq(r, "assistant.autopilot.start", "assistant", fmt.Sprintf("%s, switched on in a running conversation", describeLimits(a)), true)
	}
	n := 0
	if s.engineInContainer(r.Context()) {
		n = s.approvals.approveWaitingEngine(p.Name)
	}
	if n > 0 {
		s.auditReq(r, "assistant.autopilot.answered", "assistant", fmt.Sprintf("%d waiting tool request(s) approved by switching autopilot on", n), true)
	}
	writeJSON(w, http.StatusOK, map[string]any{"started": started, "answered": n})
}

func describeLimits(a *autopilot) string {
	a.mu.Lock()
	defer a.mu.Unlock()
	switch {
	case a.until.IsZero() && a.steps == 0:
		return "no limit"
	case a.steps == 0:
		return "until " + a.until.UTC().Format(time.RFC3339)
	case a.until.IsZero():
		return fmt.Sprintf("%d steps", a.steps)
	}
	return fmt.Sprintf("%d steps until %s", a.steps, a.until.UTC().Format(time.RFC3339))
}

// handlePutAutopilot lets an administrator turn autopilot off for everyone, or back on.
func (s *Server) handlePutAutopilot(w http.ResponseWriter, r *http.Request) {
	var q struct {
		Allowed *bool `json:"allowed"`
	}
	if !readJSON(w, r, &q) {
		return
	}
	if q.Allowed == nil {
		writeError(w, http.StatusBadRequest, "say allowed: true or false")
		return
	}
	permMu.Lock()
	defer permMu.Unlock()
	d, err := s.loadPermissions(r.Context())
	if err != nil {
		s.internalError(w, "reading the permissions", err)
		return
	}
	d.AutopilotOff = !*q.Allowed
	if err := s.docs.Put(r.Context(), kindSettings, idPermissions, d); err != nil {
		s.internalError(w, "saving the autopilot setting", err)
		return
	}
	s.auditReq(r, "assistant.autopilot.settings", "assistant", fmt.Sprintf("autopilot allowed: %v", *q.Allowed), true)
	writeJSON(w, http.StatusOK, autopilotJSON{Allowed: *q.Allowed})
}

// handleStopAutopilot ends the person's autopilot at once, in a conversation that is running now or later.
func (s *Server) handleStopAutopilot(w http.ResponseWriter, r *http.Request) {
	var q struct {
		Conversation string `json:"conversation"`
	}
	if !readJSON(w, r, &q) {
		return
	}
	p := principalFrom(r.Context())
	if endAutopilot(autopilotKeyFor(p, q.Conversation)) {
		s.auditReq(r, "assistant.autopilot.stop", "assistant", "stopped by the person", true)
	}
	writeJSON(w, http.StatusOK, map[string]bool{"stopped": true})
}
