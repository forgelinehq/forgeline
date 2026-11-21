package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/forgelinehq/forgeline/internal/authn"
	"github.com/forgelinehq/forgeline/internal/docstore"
	"github.com/forgelinehq/forgeline/internal/trace"
)

// Runbooks (design note 051): shell scripts an administrator writes and a different administrator reviews, with typed
// parameters, run through SSM Run Command on chosen instances. A check (it only reads) runs when asked; a fix is a
// proposal a different administrator approves each time. Agents choose a runbook and its parameters; they never write
// commands.

const (
	kindRunbook      = "airunbook"
	kindRunbookRun   = "airunbookrun"
	rbCheck          = "check"
	rbFix            = "fix"
	maxRunbookScript = 8000
	maxRunbookParams = 8
	maxRunbookTarget = 20 // instances one run may reach
	maxRunbookOutput = 8000
	runbookPoll      = 3 * time.Second
	runbookLost      = 3600*time.Second + 10*time.Minute // the longest time limit, the wait after it, and a margin
	approvalKindRB   = "runbook"                         // approval rules (note 032): a fix runbook
	runRunbookTool   = "run_runbook"
)

// RunbookParam is one value a run gives the script, as an environment variable: a choice from a list, or a whole
// number in a range. Never free text, so nothing a model writes reaches the shell as code.
type RunbookParam struct {
	Name    string   `json:"name"` // ENV_NAME
	Label   string   `json:"label"`
	Type    string   `json:"type"` // choice or int
	Choices []string `json:"choices,omitempty"`
	Min     int      `json:"min,omitempty"`
	Max     int      `json:"max,omitempty"`
	Default string   `json:"default,omitempty"`
}

// RunbookTarget is where a runbook may run: instance ids, or the running instances with a tag.
type RunbookTarget struct {
	Account   string   `json:"account,omitempty"` // a connected account id; empty is the primary
	Instances []string `json:"instances,omitempty"`
	TagKey    string   `json:"tag_key,omitempty"`
	TagValue  string   `json:"tag_value,omitempty"`
}

// RunbookReview is a different administrator's approval of one version's text.
type RunbookReview struct {
	By   string    `json:"by"`
	At   time.Time `json:"at"`
	Note string    `json:"note,omitempty"`
}

// RunbookVersion is one fixed text of a runbook.
type RunbookVersion struct {
	N       int            `json:"n"`
	Script  string         `json:"script"`
	Params  []RunbookParam `json:"params"`
	Target  RunbookTarget  `json:"target"`
	Timeout int            `json:"timeout_seconds"`
	Note    string         `json:"note,omitempty"`
	By      string         `json:"by"`
	At      time.Time      `json:"at"`
	Hash    string         `json:"hash"`
	Review  *RunbookReview `json:"review,omitempty"`
}

// Runbook is a named script with its versions; Current is the newest reviewed one, which is what runs (0: none yet).
type Runbook struct {
	ID          string           `json:"id"`
	Name        string           `json:"name"`
	Description string           `json:"description"`
	Kind        string           `json:"kind"` // check or fix
	Current     int              `json:"current"`
	Versions    []RunbookVersion `json:"versions"`
	CreatedBy   string           `json:"created_by"`
	CreatedAt   time.Time        `json:"created_at"`
}

func (r Runbook) version(n int) (RunbookVersion, bool) {
	for _, v := range r.Versions {
		if v.N == n {
			return v, true
		}
	}
	return RunbookVersion{}, false
}

func (v RunbookVersion) hash(id, kind string) string {
	p, _ := json.Marshal(v.Params)
	t, _ := json.Marshal(v.Target)
	return sha("runbook", id, kind, fmt.Sprint(v.N), v.Script, string(p), string(t), fmt.Sprint(v.Timeout))
}

// RunbookInstance is what one instance did.
type RunbookInstance struct {
	ID       string `json:"id"`
	Status   string `json:"status"` // as SSM says: Success, Failed, TimedOut, Cancelled, InProgress…
	ExitCode int    `json:"exit_code"`
	Output   string `json:"output"`
	Error    string `json:"error,omitempty"`
}

// RunbookRun is one run: of a check it starts at once; of a fix it is a proposal first.
type RunbookRun struct {
	ID          string             `json:"id"`
	Runbook     string             `json:"runbook"`
	RunbookName string             `json:"runbook_name"`
	Kind        string             `json:"kind"`
	Version     int                `json:"version"`
	VersionHash string             `json:"version_hash"`
	Params      map[string]string  `json:"params"`
	Reason      string             `json:"reason,omitempty"`
	Script      string             `json:"script"`  // exactly what runs, with its environment lines
	Targets     []string           `json:"targets"` // instance ids, resolved when the run was asked for
	Account     string             `json:"account"`
	Hash        string             `json:"hash"`
	Status      string             `json:"status"` // proposed, running, succeeded, failed, rejected, cancelled, expired
	By          string             `json:"by"`
	ViaAI       bool               `json:"via_ai"`
	At          time.Time          `json:"at"`
	DecidedBy   string             `json:"decided_by,omitempty"`
	Approvals   []approvalVote     `json:"approvals,omitempty"`
	Approval    *ApprovalStateJSON `json:"approval,omitempty"`
	CommandID   string             `json:"command_id,omitempty"`
	Instances   []RunbookInstance  `json:"instances"`
	Error       string             `json:"error,omitempty"`
	Finished    *time.Time         `json:"finished,omitempty"`
	TraceID     string             `json:"trace_id,omitempty"`
}

var (
	rbEnvRE      = regexp.MustCompile(`^[A-Z][A-Z0-9_]{0,39}$`)
	rbChoiceRE   = regexp.MustCompile(`^[A-Za-z0-9 ._/:@+-]{1,120}$`) // no quote, no $, no backslash: safe inside single quotes anyway
	rbInstanceRE = regexp.MustCompile(`^i-[0-9a-f]{8,17}$`)
	rbTagRE      = regexp.MustCompile(`^[A-Za-z0-9 ._:/=+@-]{1,128}$`)
	rbReserved   = map[string]bool{"PATH": true, "HOME": true, "IFS": true, "LD_PRELOAD": true, "LD_LIBRARY_PATH": true, "SHELL": true, "BASH_ENV": true, "ENV": true}
)

func (s *Server) checkRunbookVersion(ctx context.Context, v *RunbookVersion) string {
	v.Script = strings.ReplaceAll(v.Script, "\r\n", "\n")
	switch {
	case strings.TrimSpace(v.Script) == "" || len(v.Script) > maxRunbookScript:
		return fmt.Sprintf("script: 1 to %d characters", maxRunbookScript)
	case len(v.Params) > maxRunbookParams:
		return fmt.Sprintf("params: at most %d", maxRunbookParams)
	case v.Timeout == 0:
		v.Timeout = 300
	}
	if v.Timeout < 30 || v.Timeout > 3600 {
		return "timeout_seconds: 30 to 3600"
	}
	seen := map[string]bool{}
	for i := range v.Params {
		p := &v.Params[i]
		at := fmt.Sprintf("params[%d]", i)
		switch {
		case !rbEnvRE.MatchString(p.Name) || rbReserved[p.Name] || seen[p.Name]:
			return at + ".name: an environment variable name such as BACKUP_DIR, once, and not one the shell uses"
		case p.Type == "choice" && (len(p.Choices) == 0 || len(p.Choices) > 20):
			return at + ".choices: 1 to 20"
		case p.Type == "int" && (p.Min > p.Max || p.Min < 0 || p.Max > 1_000_000):
			return at + ": min and max from 0 to 1000000, min not above max"
		case p.Type != "choice" && p.Type != "int":
			return at + ".type: choice or int"
		}
		for _, c := range p.Choices {
			if !rbChoiceRE.MatchString(c) {
				return fmt.Sprintf("%s.choices: %q may hold letters, digits, spaces and . _ / : @ + - only", at, c)
			}
		}
		if p.Default != "" {
			if _, msg := p.value(p.Default); msg != "" {
				return at + ".default: " + msg
			}
		}
		if p.Label == "" {
			p.Label = p.Name
		}
		seen[p.Name] = true
	}
	t := &v.Target
	switch {
	case len(t.Instances) == 0 && t.TagKey == "":
		return "target: instance ids, or a tag"
	case len(t.Instances) > maxRunbookTarget:
		return fmt.Sprintf("target.instances: at most %d", maxRunbookTarget)
	case t.TagKey != "" && (!rbTagRE.MatchString(t.TagKey) || !rbTagRE.MatchString(t.TagValue)):
		return "target: a tag key and value of letters, digits, spaces and . _ : / = + @ -"
	}
	for _, id := range t.Instances {
		if !rbInstanceRE.MatchString(id) {
			return fmt.Sprintf("target.instances: %q is not an instance id", id)
		}
	}
	if t.Account == "" {
		t.Account = primaryAccount
	}
	if _, err := s.awsFor(ctx, t.Account); err != nil {
		return "target.account: " + err.Error()
	}
	return ""
}

// value checks one parameter's value against its type.
func (p RunbookParam) value(v string) (string, string) {
	v = strings.TrimSpace(v)
	switch p.Type {
	case "choice":
		if contains(p.Choices, v) {
			return v, ""
		}
		return "", fmt.Sprintf("%s: one of %s", p.Name, strings.Join(p.Choices, ", "))
	case "int":
		n, err := strconv.Atoi(v)
		if err != nil || n < p.Min || n > p.Max {
			return "", fmt.Sprintf("%s: a whole number from %d to %d", p.Name, p.Min, p.Max)
		}
		return strconv.Itoa(n), ""
	}
	return "", p.Name + ": unknown type"
}

// render is the script that runs: the parameters as environment lines, single-quoted, then the reviewed text.
func renderRunbook(v RunbookVersion, params map[string]string) (string, map[string]string, string) {
	var b strings.Builder
	b.WriteString("#!/bin/bash\n# Forgeline runbook: parameters, then the reviewed script.\n")
	clean := map[string]string{}
	for _, p := range v.Params {
		raw, ok := params[p.Name]
		if !ok || raw == "" {
			raw = p.Default
		}
		val, msg := p.value(raw)
		if msg != "" {
			return "", nil, "params." + msg
		}
		clean[p.Name] = val
		fmt.Fprintf(&b, "%s='%s'\nexport %s\n", p.Name, val, p.Name)
	}
	for k := range params {
		if _, ok := clean[k]; !ok {
			return "", nil, fmt.Sprintf("params.%s: this runbook has no such parameter", k)
		}
	}
	b.WriteString("\n" + v.Script + "\n")
	return b.String(), clean, ""
}

func runHash(r RunbookRun) string {
	p, _ := json.Marshal(r.Params)
	return sha("runbookrun", r.Runbook, fmt.Sprint(r.Version), r.VersionHash, string(p), strings.Join(r.Targets, ","), r.Account, r.Script)
}

// --- storage ---

func (s *Server) loadRunbook(ctx context.Context, id string) (Runbook, error) {
	var r Runbook
	err := s.docs.Get(ctx, kindRunbook, id, &r)
	return r, err
}

func (s *Server) listRunbooks(ctx context.Context) ([]Runbook, error) {
	docs, err := s.docs.List(ctx, kindRunbook, docstore.ListOptions{})
	if err != nil {
		return nil, err
	}
	all, err := docstore.Decode[Runbook](docs)
	sort.Slice(all, func(i, j int) bool { return all[i].Name < all[j].Name })
	return all, err
}

func (s *Server) loadRunbookRun(ctx context.Context, id string) (RunbookRun, error) {
	var r RunbookRun
	if err := s.docs.Get(ctx, kindRunbookRun, id, &r); err != nil {
		return r, err
	}
	return settleRun(r), nil
}

// settleRun says what a stored run is now: a fix nobody approved in 24 hours has expired, and a run still "running"
// long after the longest time limit was lost when the server stopped.
func settleRun(r RunbookRun) RunbookRun {
	if r.Status == apProposed && time.Since(r.At) > aiProposalTTL {
		r.Status = apExpired
	}
	if r.Status == "running" && time.Since(r.At) > runbookLost {
		r.Status = "failed"
		r.Error = "Forgeline stopped while this ran, so how it ended is not known; check SSM command " + r.CommandID + " on the instances"
	}
	return r
}

// --- the API ---

type runbookRequest struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Kind        string         `json:"kind"`
	Script      string         `json:"script"`
	Params      []RunbookParam `json:"params"`
	Target      RunbookTarget  `json:"target"`
	Timeout     int            `json:"timeout_seconds"`
	Note        string         `json:"note"`
}

func (s *Server) handleListRunbooks(w http.ResponseWriter, r *http.Request) {
	all, err := s.listRunbooks(r.Context())
	if err != nil {
		s.internalError(w, "reading the runbooks", err)
		return
	}
	if all == nil {
		all = []Runbook{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"runbooks": all, "examples": runbookExamples})
}

func (s *Server) handleGetRunbook(w http.ResponseWriter, r *http.Request) {
	rb, err := s.loadRunbook(r.Context(), r.PathValue("id"))
	if err != nil {
		s.defError(w, "runbook", err)
		return
	}
	writeJSON(w, http.StatusOK, rb)
}

// handleSaveRunbook creates a runbook, or adds a version to one. A new version waits for review before it runs.
func (s *Server) handleSaveRunbook(w http.ResponseWriter, r *http.Request) {
	var q runbookRequest
	if !readJSON(w, r, &q) {
		return
	}
	ctx, who, id := r.Context(), principalFrom(r.Context()), r.PathValue("id")
	var rb Runbook
	if id == "" {
		q.Name, q.Description = strings.TrimSpace(q.Name), strings.TrimSpace(q.Description)
		if m := checkNameDesc(q.Name, q.Description); m != "" {
			writeError(w, http.StatusBadRequest, m)
			return
		}
		if q.Kind != rbCheck && q.Kind != rbFix {
			writeError(w, http.StatusBadRequest, "kind: check (it only reads) or fix (it changes something)")
			return
		}
		rb = Runbook{ID: evalSlug(q.Name), Name: q.Name, Description: q.Description, Kind: q.Kind, CreatedBy: who.Name, CreatedAt: time.Now().UTC()}
		if rb.ID == "" {
			rb.ID = authn.NewID()
		}
		if _, err := s.loadRunbook(ctx, rb.ID); err == nil {
			writeError(w, http.StatusConflict, "a runbook with that name exists")
			return
		}
	} else {
		var err error
		if rb, err = s.loadRunbook(ctx, id); err != nil {
			s.defError(w, "runbook", err)
			return
		}
		if len(rb.Versions) >= maxVersions {
			writeError(w, http.StatusConflict, fmt.Sprintf("a runbook keeps at most %d versions", maxVersions))
			return
		}
	}
	v := RunbookVersion{Script: q.Script, Params: q.Params, Target: q.Target, Timeout: q.Timeout, Note: truncate(strings.TrimSpace(q.Note), maxNote), By: who.Name, At: time.Now().UTC()}
	if v.Params == nil {
		v.Params = []RunbookParam{}
	}
	if m := s.checkRunbookVersion(ctx, &v); m != "" {
		writeError(w, http.StatusBadRequest, m)
		return
	}
	v.N = len(rb.Versions) + 1
	v.Hash = v.hash(rb.ID, rb.Kind)
	rb.Versions = append(rb.Versions, v)
	if err := s.docs.Put(ctx, kindRunbook, rb.ID, rb); err != nil {
		s.internalError(w, "saving the runbook", err)
		return
	}
	s.auditReq(r, "runbook.version", "runbook/"+rb.ID, fmt.Sprintf("%s (%s) v%d %.12s, waits for review by another administrator", rb.Name, rb.Kind, v.N, v.Hash), true)
	status := http.StatusOK
	if id == "" {
		status = http.StatusCreated
	}
	writeJSON(w, status, rb)
}

// handleReviewRunbook is a different administrator approving one version's exact text; it becomes what runs.
func (s *Server) handleReviewRunbook(w http.ResponseWriter, r *http.Request) {
	var q struct {
		Version int    `json:"version"`
		Hash    string `json:"hash"`
		Note    string `json:"note"`
	}
	if !readJSON(w, r, &q) {
		return
	}
	ctx, who := r.Context(), principalFrom(r.Context())
	s.runbookMu.Lock()
	defer s.runbookMu.Unlock()
	rb, err := s.loadRunbook(ctx, r.PathValue("id"))
	if err != nil {
		s.defError(w, "runbook", err)
		return
	}
	i := q.Version - 1
	switch {
	case i < 0 || i >= len(rb.Versions):
		writeError(w, http.StatusNotFound, "no such version")
		return
	case rb.Versions[i].By == who.Name:
		s.auditReq(r, "runbook.review", "runbook/"+rb.ID, fmt.Sprintf("refused: v%d was written by the reviewer", q.Version), false)
		writeError(w, http.StatusForbidden, "you wrote this version; a different administrator reviews it")
		return
	case q.Hash != rb.Versions[i].Hash || rb.Versions[i].hash(rb.ID, rb.Kind) != q.Hash:
		writeError(w, http.StatusConflict, "the text you reviewed is not this version's; reload and read it again")
		return
	case rb.Versions[i].Review != nil:
		writeError(w, http.StatusConflict, "this version is already reviewed")
		return
	case q.Version < rb.Current:
		writeError(w, http.StatusConflict, fmt.Sprintf("v%d is already in use; review a newer version", rb.Current))
		return
	}
	rb.Versions[i].Review = &RunbookReview{By: who.Name, At: time.Now().UTC(), Note: truncate(strings.TrimSpace(q.Note), 300)}
	rb.Current = q.Version
	if err := s.docs.Put(ctx, kindRunbook, rb.ID, rb); err != nil {
		s.internalError(w, "saving the review", err)
		return
	}
	s.auditReq(r, "runbook.review", "runbook/"+rb.ID, fmt.Sprintf("%s v%d reviewed; it is what runs now", rb.Name, q.Version), true)
	writeJSON(w, http.StatusOK, rb)
}

type runbookRunRequest struct {
	Params map[string]string `json:"params"`
	Reason string            `json:"reason"`
}

func (s *Server) handleRunRunbook(w http.ResponseWriter, r *http.Request) {
	var q runbookRunRequest
	if !readJSON(w, r, &q) {
		return
	}
	run, status, msg := s.askRunbook(r.Context(), principalFrom(r.Context()), clientIP(r), r.PathValue("id"), q.Params, q.Reason, false, false)
	if msg != "" {
		writeError(w, status, msg)
		return
	}
	writeJSON(w, status, run)
}

// askRunbook starts a check, or files a fix for approval. It resolves the targets now, so the approver sees the
// instances a fix would reach.
// wait says whether to wait for a check's output (an agent needs it) or to start it and return (a page polls).
func (s *Server) askRunbook(ctx context.Context, p Principal, ip, id string, params map[string]string, reason string, viaAI, wait bool) (RunbookRun, int, string) {
	rb, err := s.loadRunbook(ctx, id)
	if err != nil {
		return RunbookRun{}, http.StatusNotFound, fmt.Sprintf("there is no runbook %q", id)
	}
	if rb.Current == 0 {
		return RunbookRun{}, http.StatusConflict, "no version of this runbook has been reviewed yet, so it cannot run"
	}
	v, _ := rb.version(rb.Current)
	reason = strings.TrimSpace(reason)
	if rb.Kind == rbFix && (len(reason) < 10 || len(reason) > 800) {
		return RunbookRun{}, http.StatusBadRequest, "reason: a fix needs a reason of 10 to 800 characters; the approver reads it"
	}
	if params == nil {
		params = map[string]string{}
	}
	script, clean, msg := renderRunbook(v, params)
	if msg != "" {
		return RunbookRun{}, http.StatusBadRequest, msg
	}
	st, err := s.awsFor(ctx, v.Target.Account)
	if err != nil || !st.Enabled {
		return RunbookRun{}, http.StatusConflict, "the runbook's AWS account is not connected"
	}
	if viaAI {
		// Runbooks reach virtual machines through SSM, which the service switches count as "other services".
		if off, err := s.awsServicesOff(ctx); err != nil || off["ec2"] || off[svcOther] {
			return RunbookRun{}, http.StatusForbidden, "an administrator turned off the AI's access to virtual machines or other AWS services, so it may not run runbooks"
		}
	}
	targets, err := s.runbookTargets(ctx, st, v.Target)
	if err != nil {
		return RunbookRun{}, http.StatusBadGateway, "finding the instances: " + err.Error()
	}
	run := RunbookRun{ID: authn.NewID(), Runbook: rb.ID, RunbookName: rb.Name, Kind: rb.Kind, Version: v.N, VersionHash: v.Hash, Params: clean, Reason: reason,
		Script: script, Targets: targets, Account: v.Target.Account, By: p.Name, ViaAI: viaAI, At: time.Now().UTC(), Instances: []RunbookInstance{}}
	run.Hash = runHash(run)
	if sp := trace.From(ctx); sp != nil {
		run.TraceID = sp.TraceID()
	}
	if rb.Kind == rbFix {
		run.Status = apProposed
		if err := s.docs.Put(ctx, kindRunbookRun, run.ID, run); err != nil {
			return RunbookRun{}, http.StatusInternalServerError, "internal error: saving the run"
		}
		s.audit(ctx, p, ip, "runbook.propose", "runbook/"+rb.ID, fmt.Sprintf("%s v%d on %s, params %v (run %s, waits for a different administrator)", rb.Name, v.N, strings.Join(targets, ","), clean, run.ID), true)
		run.Approval = s.approvalState(ctx, approvalKindRB, run.Account, false, nil)
		return run, http.StatusAccepted, ""
	}
	run.Status = "running"
	if err := s.docs.Put(ctx, kindRunbookRun, run.ID, run); err != nil {
		return RunbookRun{}, http.StatusInternalServerError, "internal error: saving the run"
	}
	s.audit(ctx, p, ip, "runbook.run", "runbook/"+rb.ID, fmt.Sprintf("check %s v%d on %s, params %v (run %s)", rb.Name, v.N, strings.Join(targets, ","), clean, run.ID), true)
	if !wait {
		s.startRunbook(ctx, p, st, run, v.Timeout)
		return run, http.StatusAccepted, ""
	}
	run = s.executeRunbook(ctx, p, st, run, v.Timeout)
	return run, http.StatusOK, ""
}

// startRunbook runs it in the background; the run's page shows how it went.
func (s *Server) startRunbook(ctx context.Context, p Principal, st awsSettings, run RunbookRun, timeout int) {
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		s.executeRunbook(context.WithoutCancel(ctx), p, st, run, timeout)
	}()
}

// runbookTargets is the instance ids a run reaches: the listed ones, or the running instances with the tag.
func (s *Server) runbookTargets(ctx context.Context, st awsSettings, t RunbookTarget) ([]string, error) {
	if len(t.Instances) > 0 {
		return append([]string(nil), t.Instances...), nil
	}
	out, err := s.aws(ctx, st, "ec2", "describe-instances", "--filters", "Name=tag:"+t.TagKey+",Values="+t.TagValue, "Name=instance-state-name,Values=running",
		"--query", "Reservations[].Instances[].InstanceId")
	if err != nil {
		return nil, err
	}
	var ids []string
	if err := json.Unmarshal(out, &ids); err != nil {
		return nil, errors.New("could not read the instances")
	}
	if len(ids) == 0 {
		return nil, fmt.Errorf("no running instance has the tag %s=%s", t.TagKey, t.TagValue)
	}
	if len(ids) > maxRunbookTarget {
		return nil, fmt.Errorf("%d instances have the tag; a run reaches at most %d", len(ids), maxRunbookTarget)
	}
	sort.Strings(ids)
	return ids, nil
}

// executeRunbook sends the script through SSM Run Command and waits for every instance, then records what each did.
func (s *Server) executeRunbook(ctx context.Context, p Principal, st awsSettings, run RunbookRun, timeout int) RunbookRun {
	ctx, span := s.tracer.Start(ctx, "runbook "+run.RunbookName, trace.Internal, trace.WithAttrs(map[string]any{attrKind: "runbook", attrSubject: run.Runbook,
		"forgeline.runbook.kind": run.Kind, "forgeline.runbook.version": int64(run.Version), "forgeline.runbook.targets": strings.Join(run.Targets, ","), attrUser: p.Name, attrRole: string(p.Role)}))
	defer span.End()
	if run.TraceID == "" {
		run.TraceID = span.TraceID()
	}
	finish := func(status, errText string) RunbookRun {
		now := time.Now().UTC()
		run.Status, run.Error, run.Finished = status, errText, &now
		if status != "succeeded" {
			span.Set(attrErrorType, "runbook_"+status)
			span.Fail("runbook_" + status)
		}
		if err := s.docs.Put(context.WithoutCancel(ctx), kindRunbookRun, run.ID, run); err != nil {
			s.log.Error("saving a runbook run", "run", run.ID, "err", err)
		}
		s.audit(context.WithoutCancel(ctx), p, "", "runbook.done", "runbook/"+run.Runbook, fmt.Sprintf("run %s: %s %s", run.ID, status, truncate(errText, 200)), status == "succeeded")
		return run
	}
	params, _ := json.Marshal(map[string][]string{"commands": {run.Script}, "executionTimeout": {strconv.Itoa(timeout)}})
	out, err := s.aws(ctx, st, append([]string{"ssm", "send-command", "--document-name", "AWS-RunShellScript", "--comment", truncate("Forgeline runbook "+run.Runbook+" run "+run.ID, 100),
		"--timeout-seconds", "60", "--parameters", string(params), "--instance-ids"}, run.Targets...)...)
	if err != nil {
		return finish("failed", "sending the command: "+err.Error())
	}
	var sent struct {
		Command struct{ CommandId string } `json:"Command"`
	}
	if json.Unmarshal(out, &sent) != nil || sent.Command.CommandId == "" {
		return finish("failed", "SSM did not say which command it started")
	}
	run.CommandID = sent.Command.CommandId
	s.docs.Put(ctx, kindRunbookRun, run.ID, run)
	deadline := time.Now().Add(time.Duration(timeout+90) * time.Second)
	failures := 0
	for {
		out, err := s.aws(ctx, st, "ssm", "list-command-invocations", "--command-id", run.CommandID, "--details")
		if err == nil {
			failures = 0
			insts, done := readInvocations(out, run.Targets)
			run.Instances = insts
			if done {
				break
			}
		} else if failures++; failures >= 5 {
			// The command may still run on the instances; say so rather than guess.
			return finish("failed", "could not read how the command went (it may still be running; SSM command "+run.CommandID+"): "+err.Error())
		}
		if time.Now().After(deadline) {
			return finish("failed", "the instances did not finish in time")
		}
		select {
		case <-ctx.Done():
			return finish("failed", "stopped before the instances finished")
		case <-time.After(s.runbookPollEvery()):
		}
	}
	for _, in := range run.Instances {
		if in.Status != "Success" {
			return finish("failed", fmt.Sprintf("%s: %s", in.ID, in.Status))
		}
	}
	return finish("succeeded", "")
}

func (s *Server) runbookPollEvery() time.Duration {
	if s.runbookPoll > 0 {
		return s.runbookPoll
	}
	return runbookPoll
}

// readInvocations reads SSM's list-command-invocations: each instance's status, exit code and output, cut and with
// credentials hidden. done is true when every target has finished.
func readInvocations(raw []byte, targets []string) ([]RunbookInstance, bool) {
	var resp struct {
		CommandInvocations []struct {
			InstanceId     string
			Status         string
			CommandPlugins []struct {
				ResponseCode  int
				Output        string
				StatusDetails string
			}
		}
	}
	if json.Unmarshal(raw, &resp) != nil {
		return nil, false
	}
	byID := map[string]RunbookInstance{}
	for _, inv := range resp.CommandInvocations {
		in := RunbookInstance{ID: inv.InstanceId, Status: inv.Status}
		for _, pl := range inv.CommandPlugins {
			in.ExitCode = pl.ResponseCode
			o, _ := hideSecrets(pl.Output)
			in.Output = truncate(o, maxRunbookOutput)
			if inv.Status != "Success" && pl.StatusDetails != "" {
				in.Error = pl.StatusDetails
			}
		}
		byID[in.ID] = in
	}
	out := make([]RunbookInstance, 0, len(targets))
	done := true
	for _, id := range targets {
		in, ok := byID[id]
		if !ok {
			in = RunbookInstance{ID: id, Status: "Pending"}
		}
		switch in.Status {
		case "Success", "Failed", "TimedOut", "Cancelled", "Undeliverable", "Terminated", "DeliveryTimedOut", "ExecutionTimedOut":
		default:
			done = false
		}
		out = append(out, in)
	}
	return out, done
}

func (s *Server) handleListRunbookRuns(w http.ResponseWriter, r *http.Request) {
	docs, err := s.docs.List(r.Context(), kindRunbookRun, docstore.ListOptions{})
	if err != nil {
		s.internalError(w, "reading the runs", err)
		return
	}
	all, _ := docstore.Decode[RunbookRun](docs)
	sort.Slice(all, func(i, j int) bool { return all[i].At.After(all[j].At) })
	rb := r.URL.Query().Get("runbook")
	out := []RunbookRun{}
	for _, run := range all {
		if rb != "" && run.Runbook != rb {
			continue
		}
		run = settleRun(run)
		if run.Status == apProposed {
			run.Approval = s.approvalState(r.Context(), approvalKindRB, run.Account, false, run.Approvals)
		}
		run.Script = ""
		for i := range run.Instances {
			run.Instances[i].Output = ""
		}
		out = append(out, run)
		if len(out) >= 100 {
			break
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"runs": out})
}

func (s *Server) handleGetRunbookRun(w http.ResponseWriter, r *http.Request) {
	run, err := s.loadRunbookRun(r.Context(), r.PathValue("id"))
	if err != nil {
		s.defError(w, "run", err)
		return
	}
	if run.Status == apProposed {
		run.Approval = s.approvalState(r.Context(), approvalKindRB, run.Account, false, run.Approvals)
	}
	writeJSON(w, http.StatusOK, run)
}

// handleApproveRunbookRun is a different administrator approving a fix exactly as proposed; the approval that
// completes the count runs it.
func (s *Server) handleApproveRunbookRun(w http.ResponseWriter, r *http.Request) {
	var q struct {
		Hash string `json:"hash"`
		Note string `json:"note"`
	}
	if !readJSON(w, r, &q) {
		return
	}
	ctx, who := r.Context(), principalFrom(r.Context())
	s.runbookMu.Lock()
	run, err := s.loadRunbookRun(ctx, r.PathValue("id"))
	if err != nil {
		s.runbookMu.Unlock()
		s.defError(w, "run", err)
		return
	}
	refuse := func(status int, msg string) {
		s.runbookMu.Unlock()
		s.auditReq(r, "runbook.approve", "runbook-run/"+run.ID, "refused: "+msg, false)
		writeError(w, status, msg)
	}
	rb, rerr := s.loadRunbook(ctx, run.Runbook)
	switch {
	case run.Status == apExpired:
		refuse(http.StatusConflict, "this fix was proposed more than 24 hours ago; ask for it again")
		return
	case run.Status != apProposed:
		refuse(http.StatusConflict, "this run is "+run.Status+", not waiting for approval")
		return
	case run.By == who.Name:
		refuse(http.StatusForbidden, "you asked for this fix; a different administrator approves it")
		return
	case q.Hash != run.Hash || runHash(run) != run.Hash:
		refuse(http.StatusConflict, "what you approved is not this run; reload and read it again")
		return
	case rerr != nil || rb.Current != run.Version:
		refuse(http.StatusConflict, "the runbook changed since this was proposed; ask for it again")
		return
	}
	votes, need, done, vstatus, vmsg := s.vote(ctx, approvalKindRB, run.Account, false, run.Approvals, who.Name, q.Note)
	if vmsg != "" {
		refuse(vstatus, vmsg)
		return
	}
	run.Approvals = votes
	if !done {
		s.docs.Put(ctx, kindRunbookRun, run.ID, run)
		s.runbookMu.Unlock()
		s.auditReq(r, "runbook.approve", "runbook-run/"+run.ID, fmt.Sprintf("approval %d of %d", len(votes), need.Needed), true)
		writeJSON(w, http.StatusOK, run)
		return
	}
	run.Status, run.DecidedBy = "running", who.Name
	if err := s.docs.Put(ctx, kindRunbookRun, run.ID, run); err != nil {
		s.runbookMu.Unlock()
		s.internalError(w, "saving the approval", err)
		return
	}
	s.runbookMu.Unlock()
	s.auditReq(r, "runbook.approve", "runbook-run/"+run.ID, fmt.Sprintf("%s v%d on %s", run.RunbookName, run.Version, strings.Join(run.Targets, ",")), true)
	st, err := s.awsFor(ctx, run.Account)
	if err != nil {
		now := time.Now().UTC()
		run.Status, run.Error, run.Finished = "failed", "the runbook's AWS account is not connected", &now
		s.docs.Put(ctx, kindRunbookRun, run.ID, run)
		writeError(w, http.StatusConflict, run.Error)
		return
	}
	v, _ := rb.version(run.Version)
	s.startRunbook(ctx, who, st, run, v.Timeout)
	writeJSON(w, http.StatusAccepted, run)
}

func (s *Server) handleDecideRunbookRun(reject bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx, who := r.Context(), principalFrom(r.Context())
		s.runbookMu.Lock()
		defer s.runbookMu.Unlock()
		run, err := s.loadRunbookRun(ctx, r.PathValue("id"))
		if err != nil {
			s.defError(w, "run", err)
			return
		}
		if run.Status != apProposed && run.Status != apExpired {
			writeError(w, http.StatusConflict, "this run is "+run.Status)
			return
		}
		status, action := apCancelled, "runbook.cancel"
		if reject {
			status, action = apRejected, "runbook.reject"
		} else if who.Name != run.By && !who.Role.AtLeast(authn.Admin) {
			writeError(w, http.StatusForbidden, "only the person who asked, or an administrator, can withdraw it")
			return
		}
		run.Status, run.DecidedBy = status, who.Name
		s.docs.Put(ctx, kindRunbookRun, run.ID, run)
		s.auditReq(r, action, "runbook-run/"+run.ID, run.RunbookName, true)
		writeJSON(w, http.StatusOK, run)
	}
}

// --- the tool agents use ---

// lookupRunRunbook lets an agent run a check or propose a fix: a runbook id and parameter values, never commands.
func (s *Server) lookupRunRunbook(ctx context.Context, raw json.RawMessage) (string, error) {
	var a struct {
		Runbook string            `json:"runbook"`
		Params  map[string]string `json:"params"`
		Reason  string            `json:"reason"`
	}
	// Strict: an agent that adds a command or a script of its own is told no, not quietly given the reviewed script.
	dec := json.NewDecoder(strings.NewReader(string(raw)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&a); err != nil {
		return "", errors.New("run_runbook takes only runbook, params and reason; you cannot send commands or scripts")
	}
	p := principalFrom(ctx)
	if a.Runbook == "" {
		return s.runbookMenu(ctx), nil
	}
	run, _, msg := s.askRunbook(ctx, p, "", a.Runbook, a.Params, a.Reason, true, true)
	if msg != "" {
		return "", errors.New(msg)
	}
	if run.Kind == rbFix {
		return fmt.Sprintf("Fix %s filed as run %s for %s, with %v. Nothing has run: a different administrator must approve it under Approvals; tell the person that.",
			run.RunbookName, run.ID, strings.Join(run.Targets, ", "), run.Params), nil
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Check %s (run %s) %s on %d instance(s).\n", run.RunbookName, run.ID, run.Status, len(run.Instances))
	for _, in := range run.Instances {
		fmt.Fprintf(&b, "\n== %s: %s (exit %d)\n%s\n", in.ID, in.Status, in.ExitCode, truncate(in.Output, maxLookupResult/max(1, len(run.Instances))-200))
	}
	return b.String(), nil
}

// runbookMenu lists the reviewed runbooks with their parameters, for an agent that asks without an id.
func (s *Server) runbookMenu(ctx context.Context) string {
	all, _ := s.listRunbooks(ctx)
	var b strings.Builder
	for _, rb := range all {
		if rb.Current == 0 {
			continue
		}
		v, _ := rb.version(rb.Current)
		fmt.Fprintf(&b, "- %q (%s): %s.", rb.ID, rb.Kind, rb.Description)
		for _, p := range v.Params {
			if p.Type == "choice" {
				fmt.Fprintf(&b, " %s: one of %s.", p.Name, strings.Join(p.Choices, "|"))
			} else {
				fmt.Fprintf(&b, " %s: %d to %d.", p.Name, p.Min, p.Max)
			}
		}
		b.WriteString("\n")
	}
	if b.Len() == 0 {
		return "There are no reviewed runbooks yet."
	}
	return "Runbooks (a check runs now; a fix is filed for a different administrator to approve):\n" + b.String()
}
