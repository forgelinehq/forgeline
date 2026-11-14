package server

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/forgelinehq/forgeline/internal/blueprint"
	"github.com/forgelinehq/forgeline/internal/docstore"
	"github.com/forgelinehq/forgeline/internal/guardrail"
)

// Building a blueprint (design note 035, slice 3). Preview runs `tofu plan` and keeps the saved plan, sealed by
// the vault. Sending it for approval files a cloud change that carries a frozen copy of that plan; when a
// different administrator approves, Forgeline applies exactly that plan file. The OpenTofu state is kept per
// blueprint, sealed, and saved after every apply, failed ones too, since a failed apply may have made things.

const (
	kindBlueprintApply = "blueprint-apply"

	kindBPPlan   = "blueprintplan"   // the latest preview of each blueprint (id = blueprint id)
	kindBPFrozen = "blueprintfrozen" // the plan a proposal carries (id = change id)
	kindBPState  = "blueprintstate"  // OpenTofu state (id = blueprint id)

	planTimeout = 10 * time.Minute
	planMaxAge  = time.Hour
	maxPlanFile = 16 << 20
	maxState    = 16 << 20

	planPlanning = "planning"
	planReady    = "ready"
	planFailed   = "failed"
)

// PlanChangeJSON is one resource a plan would add, change, replace or remove.
type PlanChangeJSON struct {
	Address string `json:"address"`
	Type    string `json:"type"`
	Block   string `json:"block,omitempty"` // the blueprint block it belongs to; empty for shared pieces
	Action  string `json:"action"`          // add, change, replace or remove
}

// BlueprintPlan is a preview: what applying the blueprint now would do.
type BlueprintPlan struct {
	ID          string           `json:"id"` // the blueprint's id
	Revision    int              `json:"revision"`
	Account     string           `json:"account"`     // the console's account id
	AWSAccount  string           `json:"aws_account"` // the 12-digit account it was planned against
	Region      string           `json:"region"`
	StateSerial int              `json:"state_serial"` // the state it was planned from
	By          string           `json:"by"`
	StartedAt   time.Time        `json:"started_at"`
	FinishedAt  *time.Time       `json:"finished_at,omitempty"`
	Status      string           `json:"status"`
	Error       string           `json:"error,omitempty"`
	Changes     []PlanChangeJSON `json:"changes"`
	Add         int              `json:"add"`
	Change      int              `json:"change"`
	Remove      int              `json:"remove"`
	// Guardrails are the rules the plan breaks (note 040): a deny stops it being sent for approval.
	Guardrails       []guardrail.Finding `json:"guardrails"`
	GuardrailVersion string              `json:"guardrail_version,omitempty"`
	// Removal is set when the preview removes what the blueprint built (note 041): keep-data, unprotect (step 1
	// of removing everything) or all (step 2). FinalSnapshot keeps a last snapshot of each database.
	Removal       string `json:"removal,omitempty"`
	FinalSnapshot bool   `json:"final_snapshot,omitempty"`
}

// storedPlan is the plan with the fields the API never shows.
type storedPlan struct {
	BlueprintPlan
	Profile string `json:"profile"`
	Code    string `json:"code"` // main.tf.json as planned
	// Modules are the template modules main.tf.json calls, by path, as planned: the apply uses exactly these.
	Modules map[string]string `json:"modules,omitempty"`
	Lock    string            `json:"lock"`   // .terraform.lock.hcl: the providers' versions and hashes
	Sealed  string            `json:"sealed"` // the plan file, base64, sealed by the vault
}

func (p storedPlan) view() BlueprintPlan {
	v := p.BlueprintPlan
	if v.Changes == nil {
		v.Changes = []PlanChangeJSON{}
	}
	if v.Guardrails == nil {
		v.Guardrails = []guardrail.Finding{}
	}
	return v
}

// blueprintState is OpenTofu's record of what a blueprint built, sealed.
type blueprintState struct {
	ID         string    `json:"id"`
	AWSAccount string    `json:"aws_account"`
	Region     string    `json:"region"`
	Serial     int       `json:"serial"` // Forgeline's count of applies, not OpenTofu's serial
	Sealed     string    `json:"sealed"`
	Lock       string    `json:"lock"`
	UpdatedAt  time.Time `json:"updated_at"`
	UpdatedBy  string    `json:"updated_by"`
	Empty      bool      `json:"empty,omitempty"` // nothing it built is left (all removed)
}

// bpBusy keeps one plan or apply per blueprint at a time.
type bpBusy struct {
	mu   sync.Mutex
	busy map[string]string
}

func (b *bpBusy) take(id, what string) (string, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.busy == nil {
		b.busy = map[string]string{}
	}
	if cur, ok := b.busy[id]; ok {
		return cur, false
	}
	b.busy[id] = what
	return "", true
}

func (b *bpBusy) done(id string) {
	b.mu.Lock()
	delete(b.busy, id)
	b.mu.Unlock()
}

func (s *Server) loadBPState(ctx context.Context, id string) (blueprintState, bool, error) {
	var st blueprintState
	err := s.docs.Get(ctx, kindBPState, id, &st)
	if errors.Is(err, docstore.ErrNotFound) {
		return st, false, nil
	}
	return st, err == nil, err
}

func consoleAccount(a string) string {
	if a == primaryAccount {
		return ""
	}
	return a
}

// ---------------------------------------------------------------- preview

// planRequest asks for a preview. Remove is empty for building, "keep-data" to remove everything but what holds
// data, or "everything" (removed in two approved steps when something is protected).
type planRequest struct {
	Remove        string `json:"remove"`
	FinalSnapshot *bool  `json:"final_snapshot"` // keep a final snapshot of each database; default yes
}

func (s *Server) handlePlanBlueprint(w http.ResponseWriter, r *http.Request) {
	ctx, p := r.Context(), principalFrom(r.Context())
	var req planRequest
	if r.ContentLength != 0 {
		if err := json.NewDecoder(io.LimitReader(r.Body, 4096)).Decode(&req); err != nil && !errors.Is(err, io.EOF) {
			writeError(w, http.StatusBadRequest, "the request is not valid JSON")
			return
		}
	}
	switch req.Remove {
	case "", "keep-data", "everything":
	default:
		writeError(w, http.StatusBadRequest, `remove must be "keep-data" or "everything"`)
		return
	}
	b, err := s.loadBlueprint(ctx, r.PathValue("id"))
	if err != nil {
		s.defError(w, "blueprint", err)
		return
	}
	if err := b.Validate(); err != nil {
		writeError(w, http.StatusConflict, "fix the blueprint first: "+err.Error())
		return
	}
	st, err := s.awsFor(ctx, b.Target.Account)
	if err != nil || !st.Enabled {
		writeError(w, http.StatusConflict, "the blueprint's AWS account is not connected")
		return
	}
	st.Region = b.Target.Region
	if req.Remove != "" {
		state, built, err := s.loadBPState(ctx, b.ID)
		if err != nil {
			s.internalError(w, "reading the blueprint's state", err)
			return
		}
		if !built || state.Empty {
			writeError(w, http.StatusConflict, "nothing this blueprint built is left to remove")
			return
		}
	}
	if what, ok := s.bp.take(b.ID, "a preview"); !ok {
		writeError(w, http.StatusConflict, "this blueprint is busy with "+what+"; try again when it ends")
		return
	}
	plan := storedPlan{BlueprintPlan: BlueprintPlan{ID: b.ID, Revision: b.Revision, Account: b.Target.Account, Region: st.Region, By: p.Name, StartedAt: time.Now().UTC(), Status: planPlanning, Changes: []PlanChangeJSON{},
		Removal: req.Remove, FinalSnapshot: req.FinalSnapshot == nil || *req.FinalSnapshot}, Profile: st.Profile}
	if err := s.docs.Put(ctx, kindBPPlan, b.ID, plan); err != nil {
		s.bp.done(b.ID)
		s.internalError(w, "saving the preview", err)
		return
	}
	what := b.Name
	if req.Remove != "" {
		what += " (remove " + req.Remove + ")"
	}
	s.audit(ctx, p, clientIP(r), "blueprint.preview", b.ID, what, true)
	answer := plan.view()
	s.wg.Add(1)
	go func(plan storedPlan) { // its own copy: the answer below must not see it change
		defer s.wg.Done()
		defer s.bp.done(b.ID)
		pctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), planTimeout)
		defer cancel()
		if err := s.runPlan(pctx, b, st, &plan); err != nil {
			plan.Status, plan.Error = planFailed, err.Error()
		} else {
			plan.Status = planReady
		}
		now := time.Now().UTC()
		plan.FinishedAt = &now
		if guardrail.Denied(plan.Guardrails) {
			var why []string
			for _, f := range plan.Guardrails {
				if f.Level == guardrail.Deny {
					why = append(why, f.Address+": "+f.Rule)
				}
			}
			s.audit(context.WithoutCancel(ctx), p, "", "blueprint.guardrail.deny", b.ID, strings.Join(why, "; "), false)
		}
		if err := s.docs.Put(context.WithoutCancel(ctx), kindBPPlan, b.ID, plan); err != nil {
			s.log.Error("saving a blueprint preview", "id", b.ID, "err", err)
		}
	}(plan)
	writeJSON(w, http.StatusAccepted, answer)
}

func (s *Server) handleGetBlueprintPlan(w http.ResponseWriter, r *http.Request) {
	var p storedPlan
	err := s.docs.Get(r.Context(), kindBPPlan, r.PathValue("id"), &p)
	if errors.Is(err, docstore.ErrNotFound) {
		writeJSON(w, http.StatusOK, nil) // no preview yet
		return
	} else if err != nil {
		s.internalError(w, "reading the preview", err)
		return
	}
	writeJSON(w, http.StatusOK, p.view())
}

// workdir makes a private directory for one tofu run, with the code, the provider lock and the state. The
// caller removes it: it holds the state unsealed.
func (s *Server) workdir(code, lock, state string, modules map[string]string) (string, error) {
	base := filepath.Join(s.cfg.DataDir, "tmp")
	if err := os.MkdirAll(base, 0o700); err != nil {
		return "", err
	}
	dir, err := os.MkdirTemp(base, "tofu-")
	if err != nil {
		return "", err
	}
	files := map[string]string{"main.tf.json": code, ".terraform.lock.hcl": lock, "terraform.tfstate": state}
	for p, body := range modules {
		// Only templates/<name>/<version>/main.tf.json, never a path that leaves the directory.
		if !filepath.IsLocal(p) || !strings.HasPrefix(p, "templates/") || filepath.Base(p) != "main.tf.json" || strings.Count(p, "/") != 3 {
			os.RemoveAll(dir)
			return "", fmt.Errorf("refused module path %q", p)
		}
		files[p] = body
	}
	for name, body := range files {
		if body == "" {
			continue
		}
		if err := os.MkdirAll(filepath.Join(dir, filepath.Dir(name)), 0o700); err != nil {
			os.RemoveAll(dir)
			return "", err
		}
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
			os.RemoveAll(dir)
			return "", err
		}
	}
	return dir, nil
}

func (s *Server) runPlan(ctx context.Context, b StoredBlueprint, st awsSettings, plan *storedPlan) error {
	idOut, err := s.aws(ctx, st, "sts", "get-caller-identity")
	if err != nil {
		return err
	}
	var id struct{ Account string }
	json.Unmarshal(idOut, &id)
	plan.AWSAccount = id.Account
	state, built, err := s.loadBPState(ctx, b.ID)
	if err != nil {
		return err
	}
	stateText := ""
	if built {
		if state.AWSAccount != id.Account || state.Region != st.Region {
			return fmt.Errorf("this blueprint was built in AWS account %s, %s; it cannot be moved to %s, %s", state.AWSAccount, state.Region, id.Account, st.Region)
		}
		if stateText, err = s.vault.Open("blueprint-state/"+b.ID, state.Sealed); err != nil {
			return errors.New("the saved state could not be opened")
		}
		plan.StateSerial = state.Serial
	}
	code, err := s.planCode(b, plan, stateText)
	if err != nil {
		return err
	}
	run, err := s.tofu(ctx)
	if err != nil {
		return err
	}
	modules, err := blueprint.ModulesFor(code)
	if err != nil {
		return err
	}
	dir, err := s.workdir(string(code), state.Lock, stateText, modules)
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)
	env, err := s.tofuEnv(ctx, st, dir)
	if err != nil {
		return err
	}
	steps := [][]string{
		{"init", "-input=false", "-no-color"},
		{"plan", "-input=false", "-no-color", "-lock=false", "-out=plan.bin"},
	}
	for _, args := range steps {
		if out, errText, err := run(ctx, dir, env, args...); err != nil {
			return fmt.Errorf("tofu %s failed: %s", args[0], tofuFailure(errText, out))
		}
	}
	show, errText, err := run(ctx, dir, env, "show", "-json", "-no-color", "plan.bin")
	if err != nil {
		return fmt.Errorf("tofu show failed: %s", tail(errText, 2000))
	}
	plan.Changes, plan.Add, plan.Change, plan.Remove, err = planChanges(show, b.Blueprint)
	if err != nil {
		return err
	}
	if plan.Guardrails, err = guardrail.Check(show, code); err != nil {
		return err
	}
	plan.GuardrailVersion = guardrail.Version
	plan.Guardrails = append(plan.Guardrails, s.quotaFindings(ctx, st, plan.Changes)...)
	file, err := os.ReadFile(filepath.Join(dir, "plan.bin"))
	if err != nil || len(file) > maxPlanFile {
		return errors.New("the plan file could not be kept")
	}
	if plan.Sealed, err = s.vault.Seal("blueprint-plan/"+b.ID, base64.StdEncoding.EncodeToString(file)); err != nil {
		return err
	}
	lock, _ := os.ReadFile(filepath.Join(dir, ".terraform.lock.hcl"))
	plan.Code, plan.Lock, plan.Modules = string(code), string(lock), modules
	return nil
}

// quotaFindings checks the account's AWS service quotas for what the plan adds, where a quota is known to stop an
// apply that the plan itself cannot see. Found on real AWS: an account whose "Total Agents per Account" for Bedrock
// AgentCore is 0 previews fine and fails at apply. A quota that cannot be read is a warning, never a pass.
func (s *Server) quotaFindings(ctx context.Context, st awsSettings, changes []PlanChangeJSON) []guardrail.Finding {
	agents := []string{}
	for _, c := range changes {
		if c.Type == "aws_bedrockagentcore_agent_runtime" && (c.Action == "add" || c.Action == "replace") {
			agents = append(agents, c.Address)
		}
	}
	if len(agents) == 0 {
		return nil
	}
	out, err := s.aws(ctx, st, "service-quotas", "list-service-quotas", "--service-code", "bedrock-agentcore")
	limit, found := -1.0, false
	if err == nil {
		var q struct {
			Quotas []struct {
				QuotaName string
				Value     float64
			}
		}
		if json.Unmarshal(out, &q) == nil {
			for _, x := range q.Quotas {
				if x.QuotaName == "Total Agents per Account" {
					limit, found = x.Value, true
				}
			}
		}
	}
	if !found {
		return []guardrail.Finding{{Rule: "service-quota", Level: guardrail.Warn, Address: agents[0],
			Text: "Forgeline could not read this account's AgentCore quota (Total Agents per Account), so it cannot tell whether AWS will allow these agents."}}
	}
	if limit < float64(len(agents)) {
		return []guardrail.Finding{{Rule: "service-quota", Level: guardrail.Deny, Address: agents[0],
			Text: fmt.Sprintf("This AWS account may have at most %d Bedrock AgentCore agents (Total Agents per Account), so AWS would refuse to create %d. Ask AWS to raise the quota in Service Quotas, then preview again.", int(limit), len(agents))}}
	}
	return nil
}

// planCode is the code to plan: the blueprint for a build, or the removal code for the stage the state calls for.
// Removing everything takes two steps when something is protected: plan.Removal says which one this is.
func (s *Server) planCode(b StoredBlueprint, plan *storedPlan, state string) ([]byte, error) {
	switch plan.Removal {
	case "":
		return b.Tofu()
	case "keep-data":
		plan.Removal = blueprint.RemoveKeepData
	case "everything":
		if state == "" {
			return nil, errors.New("nothing this blueprint built is left to remove")
		}
		need, err := blueprint.NeedsUnprotect([]byte(state), plan.FinalSnapshot)
		if err != nil {
			return nil, err
		}
		plan.Removal = blueprint.RemoveAll
		if need {
			plan.Removal = blueprint.RemoveUnprotect
		}
	}
	return b.RemovalTofu(plan.Removal, plan.FinalSnapshot)
}

// planChanges reads `tofu show -json` of a plan: the managed resources it would add, change, replace or remove.
func planChanges(show []byte, b blueprint.Blueprint) (out []PlanChangeJSON, add, change, remove int, err error) {
	var p struct {
		ResourceChanges []struct {
			Address, Mode, Type, Name string
			Change                    struct{ Actions []string }
		} `json:"resource_changes"`
	}
	if err := json.Unmarshal(show, &p); err != nil {
		return nil, 0, 0, 0, errors.New("could not read the plan: " + err.Error())
	}
	blocks := map[string]bool{}
	for _, bl := range b.Blocks {
		blocks[bl.ID] = true
	}
	out = []PlanChangeJSON{}
	for _, rc := range p.ResourceChanges {
		if rc.Mode != "managed" {
			continue
		}
		var action string
		switch strings.Join(rc.Change.Actions, ",") {
		case "create":
			action, add = "add", add+1
		case "update":
			action, change = "change", change+1
		case "delete":
			action, remove = "remove", remove+1
		case "delete,create", "create,delete":
			action, add, remove = "replace", add+1, remove+1
		default: // no-op and read
			continue
		}
		block, _, _ := strings.Cut(rc.Name, "_")
		if !blocks[block] {
			block = ""
		}
		out = append(out, PlanChangeJSON{Address: rc.Address, Type: rc.Type, Block: block, Action: action})
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Address < out[j].Address })
	return out, add, change, remove, nil
}

// ---------------------------------------------------------------- send for approval

// handleProposeBlueprint files the latest preview as a cloud change. The usual rules decide who approves it.
func (s *Server) handleProposeBlueprint(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Reason string `json:"reason"`
	}
	if !readJSON(w, r, &body) {
		return
	}
	ctx := r.Context()
	b, err := s.loadBlueprint(ctx, r.PathValue("id"))
	if err != nil {
		s.defError(w, "blueprint", err)
		return
	}
	c, herr := s.proposeChange(ctx, principalFrom(ctx), clientIP(r), proposeRequest{Kind: kindBlueprintApply, Reason: body.Reason, Account: consoleAccount(b.Target.Account), Blueprint: b.ID})
	if herr != nil {
		writeError(w, herr.status, herr.msg)
		return
	}
	writeJSON(w, http.StatusAccepted, c)
}

// readyPlan is the preview a proposal may carry: finished, of the blueprint as it is now, recent, from the
// state as it is now, and changing something.
func (s *Server) readyPlan(ctx context.Context, id string) (StoredBlueprint, storedPlan, *httpErr) {
	b, err := s.loadBlueprint(ctx, id)
	if err != nil {
		return b, storedPlan{}, refusal(http.StatusNotFound, "no such blueprint")
	}
	var p storedPlan
	if err := s.docs.Get(ctx, kindBPPlan, id, &p); err != nil {
		return b, p, refusal(http.StatusConflict, "preview the changes first")
	}
	state, _, err := s.loadBPState(ctx, id)
	switch {
	case err != nil:
		return b, p, refusal(http.StatusInternalServerError, "could not read the blueprint's state")
	case p.Status != planReady:
		return b, p, refusal(http.StatusConflict, "the preview is "+p.Status+"; preview again")
	case p.Revision != b.Revision:
		return b, p, refusal(http.StatusConflict, "the blueprint changed after the preview; preview again")
	case p.FinishedAt == nil || time.Since(*p.FinishedAt) > planMaxAge:
		return b, p, refusal(http.StatusConflict, "the preview is more than an hour old; preview again")
	case p.StateSerial != state.Serial:
		return b, p, refusal(http.StatusConflict, "the blueprint was built since the preview; preview again")
	case p.Add+p.Change+p.Remove == 0:
		return b, p, refusal(http.StatusConflict, "the preview changes nothing; there is nothing to approve")
	case p.GuardrailVersion == "":
		return b, p, refusal(http.StatusConflict, "the preview was made before guardrails were checked; preview again")
	}
	for _, f := range p.Guardrails {
		if f.Level == guardrail.Deny {
			return b, p, refusal(http.StatusConflict, "the preview breaks a guardrail ("+f.Address+"): "+f.Text)
		}
	}
	return b, p, nil
}

// proposeBlueprint fills in a blueprint change for proposeChange: its title and the preview rows now, and the
// frozen plan when the change is prepared.
func (s *Server) proposeBlueprint(ctx context.Context, c *CloudChange, id string) (func(context.Context, *CloudChange, awsSettings) error, *httpErr) {
	b, p, herr := s.readyPlan(ctx, id)
	if herr != nil {
		return nil, herr
	}
	if consoleAccount(p.Account) != c.Conn {
		return nil, refusal(http.StatusConflict, "the preview was made for another account")
	}
	c.Blueprint = b.ID
	c.Title = fmt.Sprintf("Build blueprint %s (%d to add, %d to change, %d to remove)", b.Name, p.Add, p.Change, p.Remove)
	switch p.Removal {
	case blueprint.RemoveKeepData:
		c.Title = fmt.Sprintf("Remove what blueprint %s built, keeping its data (%d to remove)", b.Name, p.Remove)
	case blueprint.RemoveUnprotect:
		c.Title = fmt.Sprintf("Remove blueprint %s, step 1 of 2: turn off deletion protection (%d to change)", b.Name, p.Change)
		if p.Remove > 0 {
			c.Title += fmt.Sprintf(" and remove the rest (%d)", p.Remove)
		}
	case blueprint.RemoveAll:
		c.Title = fmt.Sprintf("Remove everything blueprint %s built, data included (%d to remove)", b.Name, p.Remove)
	}
	names := map[string]string{}
	for _, bl := range b.Blocks {
		names[bl.ID] = bl.Name
	}
	for _, ch := range p.Changes {
		action := map[string]string{"add": "Add", "change": "Modify", "replace": "Remove and add", "remove": "Remove"}[ch.Action]
		c.Preview = append(c.Preview, ChangePreview{Action: action, Resource: ch.Address, Type: ch.Type, Detail: names[ch.Block]})
	}
	c.Guardrails = p.Guardrails
	c.PreviewNote = "Planned by OpenTofu " + tofuVersion + " at " + p.FinishedAt.Format(time.RFC3339) + "; exactly this plan is applied. " +
		fmt.Sprintf("Guardrails %s: no rule denied it; %d warning(s) to read.", p.GuardrailVersion, len(p.Guardrails))
	return func(ctx context.Context, c *CloudChange, _ awsSettings) error {
		if c.Account != p.AWSAccount {
			return fmt.Errorf("the preview was made in AWS account %s, but the connection is now %s", p.AWSAccount, c.Account)
		}
		return s.docs.Put(ctx, kindBPFrozen, c.ID, p)
	}, nil
}

// ---------------------------------------------------------------- apply

func (s *Server) executeBlueprint(ctx context.Context, c CloudChange, st awsSettings) (string, error) {
	var p storedPlan
	if err := s.docs.Get(ctx, kindBPFrozen, c.ID, &p); err != nil {
		return "", errors.New("the approved plan is missing")
	}
	if what, ok := s.bp.take(p.ID, "building"); !ok {
		return "", errors.New("the blueprint is busy with " + what)
	}
	defer s.bp.done(p.ID)
	state, built, err := s.loadBPState(ctx, p.ID)
	if err != nil {
		return "", err
	}
	if state.Serial != p.StateSerial {
		return "", errors.New("the blueprint was built again after this was previewed; nothing was applied. Preview and propose again")
	}
	stateText := ""
	if built {
		if stateText, err = s.vault.Open("blueprint-state/"+p.ID, state.Sealed); err != nil {
			return "", errors.New("the saved state could not be opened")
		}
	}
	planB64, err := s.vault.Open("blueprint-plan/"+p.ID, p.Sealed)
	if err != nil {
		return "", errors.New("the approved plan could not be opened")
	}
	planFile, err := base64.StdEncoding.DecodeString(planB64)
	if err != nil {
		return "", err
	}
	run, err := s.tofu(ctx)
	if err != nil {
		return "", err
	}
	dir, err := s.workdir(p.Code, p.Lock, stateText, p.Modules)
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(dir)
	if err := os.WriteFile(filepath.Join(dir, "plan.bin"), planFile, 0o600); err != nil {
		return "", err
	}
	st.Region = p.Region
	env, err := s.tofuEnv(ctx, st, dir)
	if err != nil {
		return "", err
	}
	// The providers must be the ones the plan was made with.
	if out, errText, err := run(ctx, dir, env, "init", "-input=false", "-no-color", "-lockfile=readonly"); err != nil {
		return "", fmt.Errorf("tofu init failed; nothing was applied: %s", tofuFailure(errText, out))
	}
	out, errText, applyErr := run(ctx, dir, env, "apply", "-input=false", "-no-color", "-lock=false", "plan.bin")
	// Keep the state whatever happened: a failed apply may still have made things.
	if serr := s.saveBPState(ctx, p, dir, c.DecidedBy); serr != nil {
		s.log.Error("saving blueprint state", "blueprint", p.ID, "err", serr)
		if applyErr == nil {
			return "", fmt.Errorf("applied, but the state could not be saved (%v); do not apply again before this is fixed", serr)
		}
	}
	if applyErr != nil {
		return "", fmt.Errorf("tofu apply failed: %s", tofuFailure(errText, out))
	}
	result := fmt.Sprintf("Applied: %d added, %d changed, %d removed.", p.Add, p.Change, p.Remove)
	if outs, _, err := run(ctx, dir, env, "output", "-json", "-no-color"); err == nil {
		var o map[string]struct{ Value any }
		if json.Unmarshal(outs, &o) == nil {
			keys := make([]string, 0, len(o))
			for k := range o {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			for _, k := range keys {
				result += fmt.Sprintf(" %s: %v.", k, o[k].Value)
			}
		}
	}
	return result, nil
}

func (s *Server) saveBPState(ctx context.Context, p storedPlan, dir, by string) error {
	raw, err := os.ReadFile(filepath.Join(dir, "terraform.tfstate"))
	if errors.Is(err, os.ErrNotExist) {
		return nil // nothing was made
	}
	if err != nil {
		return err
	}
	if len(raw) > maxState {
		return errors.New("the state is larger than Forgeline keeps")
	}
	sealed, err := s.vault.Seal("blueprint-state/"+p.ID, string(raw))
	if err != nil {
		return err
	}
	n, err := blueprint.Resources(raw)
	return s.docs.Put(context.WithoutCancel(ctx), kindBPState, p.ID, blueprintState{ID: p.ID, AWSAccount: p.AWSAccount, Region: p.Region,
		Serial: p.StateSerial + 1, Sealed: sealed, Lock: p.Lock, UpdatedAt: time.Now().UTC(), UpdatedBy: by, Empty: err == nil && n == 0})
}

// handleBlueprintBuilt says whether a blueprint has been built, and where.
func (s *Server) handleBlueprintBuilt(w http.ResponseWriter, r *http.Request) {
	st, built, err := s.loadBPState(r.Context(), r.PathValue("id"))
	if err != nil {
		s.internalError(w, "reading the blueprint's state", err)
		return
	}
	if !built {
		writeJSON(w, http.StatusOK, map[string]any{"built": false})
		return
	}
	if st.Empty {
		writeJSON(w, http.StatusOK, map[string]any{"built": false, "removed": true, "applies": st.Serial, "updated_at": st.UpdatedAt, "updated_by": st.UpdatedBy})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"built": true, "aws_account": st.AWSAccount, "region": st.Region, "applies": st.Serial, "updated_at": st.UpdatedAt, "updated_by": st.UpdatedBy})
}
