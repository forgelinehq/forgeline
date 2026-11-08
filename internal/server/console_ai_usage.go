package server

import (
	"context"
	"errors"
	"fmt"
	"math"
	"net/http"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/forgelinehq/forgeline/internal/docstore"
)

// What the AI uses, and budgets with hard stops (design note 036). Every model call goes through askModel,
// which records the tokens the backend reports, the time the call took, and the cost when the claude program
// reports one. Use is added up per agent per calendar month (UTC); people's own questions are added up
// together. An administrator can give an agent a monthly budget in tokens and in minutes: at 80% the console
// warns, and once it is used up the agent's next model call is refused. Cloud spend an agent causes is not
// counted: Forgeline cannot attribute it yet.

const (
	kindAIUsage  = "aiusage"
	peopleBucket = "_people" // questions people ask the assistant themselves
	budgetWarnAt = 0.8
	maxBudgetTok = 1_000_000_000
	maxBudgetMin = 100_000
)

// tokenCounts is the usage object both the API and the claude program send.
type tokenCounts struct {
	InputTokens         int64 `json:"input_tokens"`
	CacheCreationTokens int64 `json:"cache_creation_input_tokens"`
	CacheReadTokens     int64 `json:"cache_read_input_tokens"`
	OutputTokens        int64 `json:"output_tokens"`
}

func (t tokenCounts) input() int64 { return t.InputTokens + t.CacheCreationTokens + t.CacheReadTokens }

// modelUsage is what one or more calls used.
type modelUsage struct {
	Calls   int64   `json:"calls"`
	Input   int64   `json:"input_tokens"`
	Output  int64   `json:"output_tokens"`
	Millis  int64   `json:"millis"`
	CostUSD float64 `json:"cost_usd"`
	Costed  int64   `json:"costed_calls"` // calls whose cost the backend reported
	Unknown bool    `json:"-"`            // this call's tokens were not reported
	Blind   int64   `json:"blind_calls"`  // calls whose tokens were not reported
}

func (u modelUsage) tokens() int64 { return u.Input + u.Output }

// UsageJSON is one agent's (or people's) use in one month.
type UsageJSON struct {
	Who    string    `json:"who"`
	Month  string    `json:"month"`
	LastAt time.Time `json:"last_at"`
	modelUsage
}

// AgentBudget is an agent's monthly limit. Zero means no limit of that kind.
type AgentBudget struct {
	Tokens    int64     `json:"tokens,omitempty"`
	Minutes   int64     `json:"minutes,omitempty"`
	UpdatedBy string    `json:"updated_by,omitempty"`
	UpdatedAt time.Time `json:"updated_at,omitempty"`
}

// BudgetStateJSON says how far through its budget an agent is.
type BudgetStateJSON struct {
	Budget   *AgentBudget `json:"budget,omitempty"`
	Used     modelUsage   `json:"used"`
	Fraction float64      `json:"fraction"` // the larger of tokens and minutes, 0 with no budget
	Level    string       `json:"level"`    // ok, warn (80% or more) or stop (used up)
}

var monthRE = regexp.MustCompile(`^\d{4}-(0[1-9]|1[0-2])$`)

func monthOf(t time.Time) string { return t.UTC().Format("2006-01") }

type usageState struct{ mu sync.Mutex }

func usageID(who, month string) string { return who + "|" + month }

func (s *Server) usageFor(ctx context.Context, who, month string) (UsageJSON, error) {
	var u UsageJSON
	err := s.docs.Get(ctx, kindAIUsage, usageID(who, month), &u)
	if errors.Is(err, docstore.ErrNotFound) {
		return UsageJSON{Who: who, Month: month}, nil
	}
	return u, err
}

// who is billed for a call: the running agent, or people.
func usageWho(ctx context.Context) (string, string) {
	if sc := scopeFrom(ctx); sc != nil && sc.agent != "" {
		return sc.agent, sc.agentName
	}
	return peopleBucket, ""
}

// budgetState is where an agent stands this month.
func budgetState(b *AgentBudget, used modelUsage) BudgetStateJSON {
	st := BudgetStateJSON{Budget: b, Used: used, Level: "ok"}
	if b == nil {
		return st
	}
	if b.Tokens > 0 {
		st.Fraction = math.Max(st.Fraction, float64(used.tokens())/float64(b.Tokens))
	}
	if b.Minutes > 0 {
		st.Fraction = math.Max(st.Fraction, float64(used.Millis)/60000/float64(b.Minutes))
	}
	switch {
	case st.Fraction >= 1:
		st.Level = "stop"
	case st.Fraction >= budgetWarnAt:
		st.Level = "warn"
	}
	return st
}

var errOverBudget = errors.New("over budget")

// checkBudget refuses a call by an agent that has used up its monthly budget.
func (s *Server) checkBudget(ctx context.Context) error {
	who, name := usageWho(ctx)
	if who == peopleBucket {
		return nil
	}
	a, err := s.loadAgent(ctx, who)
	if err != nil || a.Budget == nil {
		return nil // an agent that cannot be read is refused elsewhere
	}
	u, err := s.usageFor(ctx, who, monthOf(time.Now()))
	if err != nil {
		return fmt.Errorf("the agent's use could not be read, so it is not run: %w", err)
	}
	if st := budgetState(a.Budget, u.modelUsage); st.Level == "stop" {
		return fmt.Errorf("%w: %s has used its budget for %s (%s); an administrator can raise it under Team", errOverBudget, name, monthOf(time.Now()), usedWords(a.Budget, u.modelUsage))
	}
	return nil
}

func budgetWords(b *AgentBudget) string {
	var parts []string
	if b.Tokens > 0 {
		parts = append(parts, fmt.Sprintf("%d tokens", b.Tokens))
	}
	if b.Minutes > 0 {
		parts = append(parts, fmt.Sprintf("%d minutes", b.Minutes))
	}
	if len(parts) == 0 {
		return "no limit"
	}
	return strings.Join(parts, " and ")
}

func usedWords(b *AgentBudget, u modelUsage) string {
	var parts []string
	if b.Tokens > 0 {
		parts = append(parts, fmt.Sprintf("%d of %d tokens", u.tokens(), b.Tokens))
	}
	if b.Minutes > 0 {
		parts = append(parts, fmt.Sprintf("%.1f of %d minutes", float64(u.Millis)/60000, b.Minutes))
	}
	if len(parts) == 2 {
		return parts[0] + ", " + parts[1]
	}
	if len(parts) == 1 {
		return parts[0]
	}
	return "no limit"
}

// recordUsage adds a call to this month's total for its agent (or people), and audits an agent crossing 80%
// and 100% of its budget.
func (s *Server) recordUsage(ctx context.Context, use modelUsage) {
	if use.Calls == 0 {
		use.Calls = 1
	}
	if use.Unknown {
		use.Blind = 1
	}
	ctx = context.WithoutCancel(ctx)
	who, name := usageWho(ctx)
	month := monthOf(time.Now())
	s.usage.mu.Lock()
	u, err := s.usageFor(ctx, who, month)
	if err != nil {
		s.usage.mu.Unlock()
		s.log.Error("reading AI use", "who", who, "err", err)
		return
	}
	before := u.modelUsage
	u.Calls += use.Calls
	u.Input += use.Input
	u.Output += use.Output
	u.Millis += use.Millis
	u.CostUSD += use.CostUSD
	u.Costed += use.Costed
	u.Blind += use.Blind
	u.LastAt = time.Now().UTC()
	err = s.docs.Put(ctx, kindAIUsage, usageID(who, month), u)
	s.usage.mu.Unlock()
	if err != nil {
		s.log.Error("saving AI use", "who", who, "err", err)
		return
	}
	if who == peopleBucket {
		return
	}
	a, err := s.loadAgent(ctx, who)
	if err != nil || a.Budget == nil {
		return
	}
	was, now := budgetState(a.Budget, before), budgetState(a.Budget, u.modelUsage)
	if was.Level != now.Level && now.Level != "ok" {
		action := map[string]string{"warn": "ai.agent.budget.warn", "stop": "ai.agent.budget.stop"}[now.Level]
		s.audit(ctx, Principal{Name: "forgeline"}, "", action, "agent/"+who, fmt.Sprintf("%s has used %s this month", name, usedWords(a.Budget, u.modelUsage)), true)
	}
}

// handleAIUsage lists this month's (or ?month=YYYY-MM) use per agent and for people.
func (s *Server) handleAIUsage(w http.ResponseWriter, r *http.Request) {
	month := r.URL.Query().Get("month")
	if month == "" {
		month = monthOf(time.Now())
	}
	if !monthRE.MatchString(month) {
		writeError(w, http.StatusBadRequest, "month: give it as YYYY-MM")
		return
	}
	docs, err := s.docs.List(r.Context(), kindAIUsage, docstore.ListOptions{})
	if err != nil {
		s.internalError(w, "reading AI use", err)
		return
	}
	all, err := docstore.Decode[UsageJSON](docs)
	if err != nil {
		s.internalError(w, "reading AI use", err)
		return
	}
	out := []UsageJSON{}
	for _, u := range all {
		if u.Month == month {
			out = append(out, u)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].tokens() > out[j].tokens() })
	writeJSON(w, http.StatusOK, map[string]any{"month": month, "usage": out})
}

// handleAgentBudget sets or clears an agent's monthly budget (administrators).
func (s *Server) handleAgentBudget(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Tokens  int64 `json:"tokens"`
		Minutes int64 `json:"minutes"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	if req.Tokens < 0 || req.Tokens > maxBudgetTok {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("tokens: 0 (no limit) to %d", int64(maxBudgetTok)))
		return
	}
	if req.Minutes < 0 || req.Minutes > maxBudgetMin {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("minutes: 0 (no limit) to %d", maxBudgetMin))
		return
	}
	p := principalFrom(r.Context())
	a, err := s.loadAgent(r.Context(), r.PathValue("id"))
	if err != nil {
		s.defError(w, "agent", err)
		return
	}
	detail := a.Name + ": no budget"
	if req.Tokens == 0 && req.Minutes == 0 {
		a.Budget = nil
	} else {
		a.Budget = &AgentBudget{Tokens: req.Tokens, Minutes: req.Minutes, UpdatedBy: p.Name, UpdatedAt: time.Now().UTC()}
		detail = a.Name + ": " + budgetWords(a.Budget) + " a month"
	}
	if err := s.docs.Put(r.Context(), kindAIAgent, a.ID, a); err != nil {
		s.internalError(w, "saving the agent", err)
		return
	}
	s.audit(r.Context(), p, clientIP(r), "ai.agent.budget", "agent/"+a.ID, detail, true)
	u, _ := s.usageFor(r.Context(), a.ID, monthOf(time.Now()))
	writeJSON(w, http.StatusOK, budgetState(a.Budget, u.modelUsage))
}
