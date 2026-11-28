package server

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/forgelinehq/forgeline/internal/authn"
	"github.com/forgelinehq/forgeline/internal/docstore"
)

// Permissions for what the assistant does as the person (design note 052, slice 2). A read runs. A change asks the
// person first, with the exact command, and they answer once, always for that kind of command, or no. Rules say it
// ahead of time: allow, ask or deny, for the organisation (administrators) and for one person.
//
// What is not decided here, and cannot be changed by a rule: who may change anything at all (administrators), which
// accounts (only the ones an administrator marked "allow with my own approval"; the rest keep a proposal and a
// different administrator, note 006), and the AWS service switches. A rule can only narrow what those allow.

const (
	idPermissions  = "assistant-permissions"
	permAllow      = "allow"
	permAsk        = "ask"
	permDeny       = "deny"
	scopePerson    = "person"
	scopeOrg       = "org"
	permToolAWS    = "call_aws"
	maxPermRules   = 100 // per person, and for the organisation
	maxPermPattern = 200
	decisionOnce   = "approve"
	decisionAlways = "always"
	decisionDenied = "deny"
)

// PermRule is one rule. The pattern is matched against the command as the server will run it ("aws ecs update-service
// --cluster x"), where * stands for any characters.
type PermRule struct {
	ID        string    `json:"id"`
	Scope     string    `json:"scope"`           // person or org
	Owner     string    `json:"owner,omitempty"` // the person's user id, for a person rule
	OwnerName string    `json:"owner_name,omitempty"`
	Tool      string    `json:"tool"`
	Pattern   string    `json:"pattern"`
	Effect    string    `json:"effect"`
	Note      string    `json:"note,omitempty"`
	By        string    `json:"by"`
	At        time.Time `json:"at"`
}

type permissionsDoc struct {
	Rules []PermRule `json:"rules"`
	// OwnApproval holds the accounts an administrator marked "allow with my own approval", by account id. An account not
	// listed is protected: a change to it is a proposal that a different administrator approves.
	OwnApproval map[string]bool `json:"own_approval,omitempty"`
	// AutopilotOff is an administrator turning autopilot off for everyone (note 052).
	AutopilotOff bool `json:"autopilot_off,omitempty"`
}

// How long a change waits for the person; a variable so a test that fails early does not wait it out.
var permAskTimeout = 5 * time.Minute

var permMu sync.Mutex

func (s *Server) loadPermissions(ctx context.Context) (permissionsDoc, error) {
	var d permissionsDoc
	err := s.docs.Get(ctx, kindSettings, idPermissions, &d)
	if errors.Is(err, docstore.ErrNotFound) {
		return permissionsDoc{}, nil
	}
	return d, err
}

// globRE turns a pattern into an anchored expression where only * is special.
func globRE(pattern string) *regexp.Regexp {
	parts := strings.Split(pattern, "*")
	for i := range parts {
		parts[i] = regexp.QuoteMeta(parts[i])
	}
	return regexp.MustCompile("^" + strings.Join(parts, ".*") + "$")
}

// permAlwaysAsk are services whose changes are always asked about and never allowed ahead of time: they reach
// credentials and permissions, or run commands on machines (a runbook, note 051, is the reviewed way to do that).
var permAlwaysAsk = map[string]bool{"iam": true, "organizations": true, "account": true, "ssm": true, "sts": true, "sso-admin": true, "identitystore": true}

// ruleProblem says why a rule is not acceptable, or "".
func ruleProblem(r PermRule) string {
	switch {
	case r.Tool != permToolAWS:
		return "tool: only call_aws has rules so far"
	case r.Effect != permAllow && r.Effect != permAsk && r.Effect != permDeny:
		return "effect: allow, ask or deny"
	case r.Pattern == "" || len(r.Pattern) > maxPermPattern || strings.ContainsAny(r.Pattern, "\n\r\x00"):
		return fmt.Sprintf("pattern: 1 to %d characters on one line", maxPermPattern)
	case !strings.HasPrefix(r.Pattern, "aws "):
		return `pattern: starts with "aws ", for example "aws ecs update-service *"`
	}
	if r.Effect == permAllow {
		// An allow must name its service and operation: "aws *" would let the assistant change anything.
		toks := strings.Fields(r.Pattern)
		if len(toks) < 3 || strings.Contains(toks[1], "*") || strings.Contains(toks[2], "*") {
			return `pattern: an allow names the service and the operation ("aws ecs update-service *"); a wildcard there is only for ask and deny`
		}
		if permAlwaysAsk[toks[1]] {
			return fmt.Sprintf("pattern: %s changes are always asked about; they cannot be allowed ahead of time", toks[1])
		}
	}
	return ""
}

// permDecision is the answer to "may this run": allow, ask or deny, and why.
type permDecision struct {
	Effect string
	Why    string
	Rule   string // the rule that decided it, if any
}

// decide combines the rules for one command (design note 052): a deny from anyone wins; an ask from the organisation
// cannot be skipped by a person's allow; then an allow; then the default, which is to ask.
func (d permissionsDoc) decide(p Principal, tool, command string) permDecision {
	var matched []PermRule
	for _, r := range d.Rules {
		if r.Tool != tool || (r.Scope == scopePerson && r.Owner != p.UserID) {
			continue
		}
		if globRE(r.Pattern).MatchString(command) {
			matched = append(matched, r)
		}
	}
	for _, r := range matched {
		if r.Effect == permDeny {
			return permDecision{permDeny, fmt.Sprintf("a %s rule denies %q (%s)", r.Scope, r.Pattern, r.By), r.ID}
		}
	}
	for _, r := range matched {
		if r.Effect == permAsk && r.Scope == scopeOrg {
			return permDecision{permAsk, fmt.Sprintf("an organisation rule asks before %q", r.Pattern), r.ID}
		}
	}
	toks := strings.Fields(command)
	always := len(toks) >= 2 && permAlwaysAsk[toks[1]]
	for _, r := range matched {
		if r.Effect == permAllow && !always {
			return permDecision{permAllow, fmt.Sprintf("allowed by your rule %q", r.Pattern), r.ID}
		}
	}
	for _, r := range matched {
		if r.Effect == permAsk {
			return permDecision{permAsk, fmt.Sprintf("your rule asks before %q", r.Pattern), r.ID}
		}
	}
	return permDecision{permAsk, "a change asks first", ""}
}

// alwaysPattern is what "always allow this kind" would allow: the service and the operation, any options. "" when
// the kind may not be allowed ahead of time.
func alwaysPattern(command string) string {
	toks := strings.Fields(command)
	if len(toks) < 3 || toks[0] != "aws" || permAlwaysAsk[toks[1]] {
		return ""
	}
	return "aws " + toks[1] + " " + toks[2] + " *"
}

// ownApproval says whether an account lets a person change it with their own approval.
func (s *Server) ownApproval(ctx context.Context, account string) bool {
	if account == "" {
		account = primaryAccount
	}
	d, err := s.loadPermissions(ctx)
	return err == nil && d.OwnApproval[account]
}

// --- asking the person ---

// permAsk is what the person is shown when the assistant wants to change something.
type permRequest struct {
	Tool, Command, Account, Why, Pattern string       // Pattern is what "always" would allow; "" when it cannot be
	Actions                              []planAction // for a plan: everything it covers, shown in full
}

type permAsker func(permRequest) (decision string, err error)
type permAskerKey struct{}

// permAskerFrom is the way to ask the person about a change, or nil where nobody is present. An agent never has one.
func permAskerFrom(ctx context.Context) permAsker {
	if scopeFrom(ctx) != nil {
		return nil
	}
	a, _ := ctx.Value(permAskerKey{}).(permAsker)
	return a
}

// askPermission shows the change and waits. It returns approve (once), always or deny.
func (s *Server) askPermission(ctx context.Context, p Principal, req permRequest, shown func(id string, wait time.Duration), decided func(id, decision string)) (string, error) {
	pa := &pendingApproval{who: p.Name, tool: req.Tool, kind: "permission", answer: make(chan string, 1), always: req.Pattern != ""}
	id := s.approvals.add(pa)
	defer s.approvals.remove(id)
	shown(id, permAskTimeout)
	t := time.NewTimer(permAskTimeout)
	defer t.Stop()
	select {
	case d := <-pa.answer:
		decided(id, d)
		return d, nil
	case <-t.C:
		decided(id, "expired")
		return decisionDenied, errors.New("nobody answered in time, so it did not run")
	case <-ctx.Done():
		return decisionDenied, errors.New("the person left before answering, so it did not run")
	}
}

// --- the API ---

type permAccountJSON struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	OwnApproval bool   `json:"own_approval"`
}

func (s *Server) handleGetPermissions(w http.ResponseWriter, r *http.Request) {
	ctx, p := r.Context(), principalFrom(r.Context())
	d, err := s.loadPermissions(ctx)
	if err != nil {
		s.internalError(w, "reading the permissions", err)
		return
	}
	rules := []PermRule{}
	for _, x := range d.Rules {
		if x.Scope == scopeOrg || x.Owner == p.UserID {
			rules = append(rules, x)
		}
	}
	accts := []permAccountJSON{{ID: primaryAccount, Name: "Primary connection", OwnApproval: d.OwnApproval[primaryAccount]}}
	if as, err := s.listAWSAccounts(ctx); err == nil {
		for _, a := range as {
			accts = append(accts, permAccountJSON{ID: a.ID, Name: a.Name, OwnApproval: d.OwnApproval[a.ID]})
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"rules": rules, "accounts": accts, "role": p.Role})
}

type permRuleRequest struct {
	Scope   string `json:"scope"`
	Tool    string `json:"tool"`
	Pattern string `json:"pattern"`
	Effect  string `json:"effect"`
	Note    string `json:"note"`
}

func (s *Server) handleAddPermissionRule(w http.ResponseWriter, r *http.Request) {
	var q permRuleRequest
	if !readJSON(w, r, &q) {
		return
	}
	ctx, p := r.Context(), principalFrom(r.Context())
	if q.Tool == "" {
		q.Tool = permToolAWS
	}
	if q.Scope == "" {
		q.Scope = scopePerson
	}
	if q.Scope != scopePerson && q.Scope != scopeOrg {
		writeError(w, http.StatusBadRequest, "scope: person or org")
		return
	}
	if q.Scope == scopeOrg && !p.Role.AtLeast(authn.Admin) {
		writeError(w, http.StatusForbidden, "only an administrator makes an organisation rule")
		return
	}
	rule := PermRule{ID: authn.NewID(), Scope: q.Scope, Tool: q.Tool, Pattern: strings.TrimSpace(q.Pattern), Effect: q.Effect, Note: truncate(strings.TrimSpace(q.Note), 200), By: p.Name, At: time.Now().UTC()}
	if q.Scope == scopePerson {
		rule.Owner, rule.OwnerName = p.UserID, p.Name
	}
	if msg := ruleProblem(rule); msg != "" {
		writeError(w, http.StatusBadRequest, msg)
		return
	}
	permMu.Lock()
	defer permMu.Unlock()
	d, err := s.loadPermissions(ctx)
	if err != nil {
		s.internalError(w, "reading the permissions", err)
		return
	}
	n := 0
	for _, x := range d.Rules {
		if x.Scope == rule.Scope && x.Owner == rule.Owner {
			n++
		}
	}
	if n >= maxPermRules {
		writeError(w, http.StatusConflict, fmt.Sprintf("at most %d rules; remove one first", maxPermRules))
		return
	}
	d.Rules = append(d.Rules, rule)
	if err := s.docs.Put(ctx, kindSettings, idPermissions, d); err != nil {
		s.internalError(w, "saving the rule", err)
		return
	}
	s.auditReq(r, "assistant.permission.rule", "assistant", fmt.Sprintf("%s rule: %s %q", rule.Scope, rule.Effect, rule.Pattern), true)
	writeJSON(w, http.StatusCreated, rule)
}

func (s *Server) handleDeletePermissionRule(w http.ResponseWriter, r *http.Request) {
	ctx, p := r.Context(), principalFrom(r.Context())
	permMu.Lock()
	defer permMu.Unlock()
	d, err := s.loadPermissions(ctx)
	if err != nil {
		s.internalError(w, "reading the permissions", err)
		return
	}
	for i, x := range d.Rules {
		if x.ID != r.PathValue("id") {
			continue
		}
		if x.Scope == scopeOrg && !p.Role.AtLeast(authn.Admin) || x.Scope == scopePerson && x.Owner != p.UserID {
			writeError(w, http.StatusNotFound, "no such rule") // not yours: the same answer as for one that is not there
			return
		}
		d.Rules = append(d.Rules[:i], d.Rules[i+1:]...)
		if err := s.docs.Put(ctx, kindSettings, idPermissions, d); err != nil {
			s.internalError(w, "saving the rules", err)
			return
		}
		s.auditReq(r, "assistant.permission.rule.delete", "assistant", fmt.Sprintf("%s rule: %s %q", x.Scope, x.Effect, x.Pattern), true)
		writeJSON(w, http.StatusOK, map[string]bool{"deleted": true})
		return
	}
	writeError(w, http.StatusNotFound, "no such rule")
}

// handlePutPermissionAccount marks an account "allow with my own approval", or protected again.
func (s *Server) handlePutPermissionAccount(w http.ResponseWriter, r *http.Request) {
	var q struct {
		OwnApproval *bool `json:"own_approval"`
	}
	if !readJSON(w, r, &q) {
		return
	}
	if q.OwnApproval == nil {
		writeError(w, http.StatusBadRequest, "say own_approval: true or false")
		return
	}
	ctx, id := r.Context(), r.PathValue("id")
	name := "Primary connection"
	if id != primaryAccount {
		var a AWSAccount
		if err := s.docs.Get(ctx, kindAWSAccount, id, &a); err != nil {
			writeError(w, http.StatusNotFound, "no such account")
			return
		}
		name = a.Name
	}
	permMu.Lock()
	defer permMu.Unlock()
	d, err := s.loadPermissions(ctx)
	if err != nil {
		s.internalError(w, "reading the permissions", err)
		return
	}
	if d.OwnApproval == nil {
		d.OwnApproval = map[string]bool{}
	}
	if *q.OwnApproval {
		d.OwnApproval[id] = true
	} else {
		delete(d.OwnApproval, id)
	}
	if err := s.docs.Put(ctx, kindSettings, idPermissions, d); err != nil {
		s.internalError(w, "saving the account setting", err)
		return
	}
	s.auditReq(r, "assistant.permission.account", "integration/aws/"+id, fmt.Sprintf("%s: %s", name, map[bool]string{true: "the assistant may change it with a person's own approval", false: "protected: a change is a proposal a different administrator approves"}[*q.OwnApproval]), true)
	writeJSON(w, http.StatusOK, permAccountJSON{ID: id, Name: name, OwnApproval: *q.OwnApproval})
}
