package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/forgelinehq/forgeline/internal/authn"
	"github.com/forgelinehq/forgeline/internal/docstore"
)

// Approval rules (design note 032). With no rules a change needs one administrator who did not propose it, as
// before. A rule can ask for two approvals, and can name which administrators may approve, for the changes it
// matches. Every matching rule applies and the strictest wins. Rules only add requirements: nothing here can let
// a proposer approve their own change, let a non-administrator approve, or skip a check the approval runs.

const (
	idApprovalRules  = "approval-rules"
	approvalKindAI   = "ai"    // a change the assistant drafted (AI proposal)
	approvalKindCons = "cloud" // a cloud change made from the console
	maxApprovalRules = 20
)

type approvalRule struct {
	Name      string   `json:"name"`
	Accounts  []string `json:"accounts,omitempty"`  // account ids ("primary", an added account); empty is every account
	Kinds     []string `json:"kinds,omitempty"`     // "ai", "cloud"; empty is both
	HighRisk  bool     `json:"high_risk,omitempty"` // only changes rated high risk
	Approvals int      `json:"approvals"`           // 1 or 2
	Approvers []string `json:"approvers,omitempty"` // usernames of administrators; empty is any administrator
}

type approvalRules struct {
	Rules     []approvalRule `json:"rules"`
	UpdatedAt time.Time      `json:"updated_at"`
	UpdatedBy string         `json:"updated_by"`
}

// approvalVote is one approval given to a change that needs more than one.
type approvalVote struct {
	By   string    `json:"by"`
	At   time.Time `json:"at"`
	Note string    `json:"note,omitempty"`
}

// approvalNeed is what the rules ask of one change.
type approvalNeed struct {
	Needed    int      // approvals in all, at least 1
	Approvers []string // who may approve; nil means any administrator
	Rules     []string // the names of the rules that matched, for messages
}

func (n approvalNeed) mayApprove(name string) bool {
	return n.Approvers == nil || contains(n.Approvers, name)
}

func (s *Server) loadApprovalRules(ctx context.Context) (approvalRules, error) {
	var r approvalRules
	err := s.docs.Get(ctx, kindSettings, idApprovalRules, &r)
	if errors.Is(err, docstore.ErrNotFound) {
		return approvalRules{Rules: []approvalRule{}}, nil
	}
	if r.Rules == nil {
		r.Rules = []approvalRule{}
	}
	return r, err
}

func (r approvalRule) matches(kind, account string, high bool) bool {
	return (len(r.Accounts) == 0 || contains(r.Accounts, account)) &&
		(len(r.Kinds) == 0 || contains(r.Kinds, kind)) &&
		(!r.HighRisk || high)
}

// needFor combines the matching rules: the most approvals any asks, and the approvers every list allows.
func (rs approvalRules) needFor(kind, account string, high bool) approvalNeed {
	if account == "" {
		account = primaryAccount
	}
	n := approvalNeed{Needed: 1}
	for _, r := range rs.Rules {
		if !r.matches(kind, account, high) {
			continue
		}
		n.Rules = append(n.Rules, r.Name)
		if r.Approvals > n.Needed {
			n.Needed = r.Approvals
		}
		if len(r.Approvers) > 0 {
			if n.Approvers == nil {
				n.Approvers = append([]string{}, r.Approvers...)
			} else {
				var both []string
				for _, a := range n.Approvers {
					if contains(r.Approvers, a) {
						both = append(both, a)
					}
				}
				n.Approvers = append([]string{}, both...) // may be empty: then nobody may approve, and the message says so
			}
		}
	}
	sort.Strings(n.Approvers)
	return n
}

// approvalNeedFor reads the rules. An unreadable rule set is an error, and the approval is refused: a rule that
// cannot be read must not quietly turn into "one approval is enough".
func (s *Server) approvalNeedFor(ctx context.Context, kind, account string, high bool) (approvalNeed, error) {
	rs, err := s.loadApprovalRules(ctx)
	if err != nil {
		return approvalNeed{}, errors.New("the approval rules could not be read, so nothing is approved until they can")
	}
	return rs.needFor(kind, account, high), nil
}

// vote checks one approval against the rules and records it. done says whether this approval completes the count
// (the change should run now). A refusal comes back as status and message.
func (s *Server) vote(ctx context.Context, kind, account string, high bool, votes []approvalVote, who, note string) (out []approvalVote, need approvalNeed, done bool, status int, msg string) {
	need, err := s.approvalNeedFor(ctx, kind, account, high)
	if err != nil {
		return votes, need, false, http.StatusConflict, err.Error()
	}
	if !need.mayApprove(who) {
		if len(need.Approvers) == 0 {
			return votes, need, false, http.StatusForbidden, "the approval rules (" + strings.Join(need.Rules, ", ") + ") leave nobody who may approve this change; an administrator must fix the rules"
		}
		return votes, need, false, http.StatusForbidden, "the approval rule (" + strings.Join(need.Rules, ", ") + ") says only " + strings.Join(need.Approvers, ", ") + " may approve this change"
	}
	for _, v := range votes {
		if v.By == who {
			return votes, need, false, http.StatusConflict, fmt.Sprintf("you already approved this; it needs %d approvals from different administrators", need.Needed)
		}
	}
	out = append(append([]approvalVote{}, votes...), approvalVote{By: who, At: time.Now().UTC(), Note: truncate(strings.TrimSpace(note), 300)})
	return out, need, len(out) >= need.Needed, 0, ""
}

// --- what the console shows on a waiting change ---

// ApprovalStateJSON says how many approvals a change needs, who gave them, and who may give the next.
type ApprovalStateJSON struct {
	Needed    int            `json:"needed"`
	Given     []approvalVote `json:"given"`
	Approvers []string       `json:"approvers,omitempty"` // empty: any administrator, unless Nobody
	Nobody    bool           `json:"nobody,omitempty"`    // the matching rules name lists that share nobody
	Rules     []string       `json:"rules,omitempty"`
}

func (s *Server) approvalState(ctx context.Context, kind, account string, high bool, votes []approvalVote) *ApprovalStateJSON {
	rs, err := s.loadApprovalRules(ctx)
	if err != nil {
		return nil
	}
	return s.approvalStateFromNeed(rs.needFor(kind, account, high), votes)
}

func (s *Server) approvalStateFromNeed(n approvalNeed, votes []approvalVote) *ApprovalStateJSON {
	if votes == nil {
		votes = []approvalVote{}
	}
	return &ApprovalStateJSON{Needed: n.Needed, Given: votes, Approvers: n.Approvers, Nobody: n.Approvers != nil && len(n.Approvers) == 0, Rules: n.Rules}
}

// --- the API ---

func (s *Server) handleGetApprovalRules(w http.ResponseWriter, r *http.Request) {
	rs, err := s.loadApprovalRules(r.Context())
	if err != nil {
		writeError(w, http.StatusConflict, "the stored approval rules cannot be read")
		return
	}
	writeJSON(w, http.StatusOK, rs)
}

func (s *Server) handlePutApprovalRules(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Rules []approvalRule `json:"rules"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	ctx := r.Context()
	if len(req.Rules) > maxApprovalRules {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("at most %d rules", maxApprovalRules))
		return
	}
	accounts := map[string]bool{primaryAccount: true}
	if added, err := s.listAWSAccounts(ctx); err == nil {
		for _, a := range added {
			accounts[a.ID] = true
		}
	}
	admins := map[string]bool{}
	if users, err := s.loadUsers(ctx); err == nil {
		for _, u := range users {
			if u.Role == authn.Admin && !u.Disabled {
				admins[u.Username] = true
			}
		}
	}
	for i := range req.Rules {
		rl := &req.Rules[i]
		at := fmt.Sprintf("rules[%d]", i)
		rl.Name = strings.TrimSpace(rl.Name)
		switch {
		case rl.Name == "" || len(rl.Name) > 60:
			writeError(w, http.StatusBadRequest, at+": give it a name of up to 60 characters")
			return
		case rl.Approvals < 1 || rl.Approvals > 2:
			writeError(w, http.StatusBadRequest, at+": approvals must be 1 or 2")
			return
		}
		for j, a := range rl.Accounts {
			if !accounts[a] {
				writeError(w, http.StatusBadRequest, fmt.Sprintf("%s.accounts[%d]: %q is not a connected AWS account", at, j, a))
				return
			}
		}
		for j, k := range rl.Kinds {
			if k != approvalKindAI && k != approvalKindCons {
				writeError(w, http.StatusBadRequest, fmt.Sprintf("%s.kinds[%d]: %q must be ai or cloud", at, j, k))
				return
			}
		}
		for j, u := range rl.Approvers {
			if !admins[u] {
				writeError(w, http.StatusBadRequest, fmt.Sprintf("%s.approvers[%d]: %q is not an active administrator", at, j, u))
				return
			}
		}
		if rl.Approvals == 2 && len(rl.Approvers) == 1 {
			writeError(w, http.StatusBadRequest, at+": two approvals need at least two people who may approve")
			return
		}
	}
	cur, err := s.loadApprovalRules(ctx)
	if err != nil {
		writeError(w, http.StatusConflict, "the stored approval rules cannot be read, so they will not be changed over; fix or remove them first")
		return
	}
	before, _ := json.Marshal(cur.Rules)
	next := approvalRules{Rules: req.Rules, UpdatedAt: time.Now().UTC(), UpdatedBy: principalFrom(ctx).Name}
	if next.Rules == nil {
		next.Rules = []approvalRule{}
	}
	if err := s.docs.Put(ctx, kindSettings, idApprovalRules, next); err != nil {
		s.internalError(w, "saving the approval rules", err)
		return
	}
	after, _ := json.Marshal(next.Rules)
	s.auditReq(r, "approval.rules", "settings/approval-rules", truncate("now "+string(after)+"; was "+string(before), 1500), true)
	writeJSON(w, http.StatusOK, next)
}
