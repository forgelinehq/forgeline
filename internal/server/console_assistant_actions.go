package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/forgelinehq/forgeline/internal/authn"
	"github.com/forgelinehq/forgeline/internal/docstore"
	"github.com/forgelinehq/forgeline/internal/pipeline"
)

// The assistant acting inside Forgeline (design note 028). The assistant never changes anything itself: its
// `prepare_action` tool only files a card. The person reads exactly what would happen and presses Confirm, and the
// server then does it as that person, with their role, through the same checks as the console, and audits it.
// Cloud changes are not here: they stay change proposals approved by a different administrator (note 006).

const (
	kindAssistAction = "assistaction"
	prepareTool      = "prepare_action"

	actSavePipeline = "save_pipeline"
	actRunPipeline  = "run_pipeline"
	actSaveSkill    = "save_skill"    // note 028, 2026-11-12: the assistant writes skills, agents and workflows
	actSaveAgent    = "save_agent"    // as cards an administrator confirms, through the console's own checks
	actSaveWorkflow = "save_workflow" //

	actPending   = "pending"
	actDone      = "done"
	actDismissed = "dismissed"
	actFailed    = "failed"

	assistActionTTL  = 30 * time.Minute
	maxPendingAction = 10
	maxActionYAML    = 64 << 10
)

// AssistAction is one prepared action waiting for its owner.
type AssistAction struct {
	ID        string          `json:"id"`
	Owner     string          `json:"owner"` // user id
	OwnerName string          `json:"owner_name"`
	Kind      string          `json:"kind"`
	Title     string          `json:"title"`
	Effects   []string        `json:"effects"`          // what confirming does, in plain words
	Detail    string          `json:"detail,omitempty"` // the pipeline file, shown in full
	Pipeline  string          `json:"pipeline"`         // pipeline id/name the action is about
	Spec      json.RawMessage `json:"spec,omitempty"`   // the skill, agent or workflow to save, exactly as checked
	Note      string          `json:"description,omitempty"`
	Status    string          `json:"status"`
	Result    string          `json:"result,omitempty"`
	Link      string          `json:"link,omitempty"` // console path to what was made
	Created   time.Time       `json:"created"`
	Expires   time.Time       `json:"expires"`
}

// actionMu serialises confirm/dismiss so one card cannot be confirmed twice.
var actionMu sync.Mutex

func (s *Server) lookupPrepareAction(ctx context.Context, raw json.RawMessage) (string, error) {
	t := memTurnFrom(ctx)
	if t == nil || scopeFrom(ctx) != nil || t.p.UserID == "" {
		return "", errors.New("prepare_action is only available when a person is chatting with the assistant")
	}
	var a struct {
		Action      string          `json:"action"`
		Title       string          `json:"title"`
		YAML        string          `json:"yaml"`
		Description string          `json:"description"`
		Pipeline    string          `json:"pipeline"`
		Spec        json.RawMessage `json:"spec"`
	}
	if err := decodeArgs(raw, &a); err != nil {
		return "", err
	}
	if !t.p.Role.AtLeast(authn.Developer) {
		return "", errors.New("your role cannot do this")
	}
	act := AssistAction{ID: randomID(), Owner: t.p.UserID, OwnerName: t.p.Name, Kind: a.Action, Status: actPending,
		Created: time.Now().UTC(), Title: truncate(strings.TrimSpace(a.Title), 120)}
	act.Expires = act.Created.Add(assistActionTTL)
	switch a.Action {
	case actSavePipeline:
		if len(a.YAML) > maxActionYAML {
			return "", errors.New("the pipeline file is too large")
		}
		name, apiErr := s.checkSavable(a.YAML, a.Description)
		if apiErr != nil {
			return "", errors.New("this pipeline is not valid, fix it first: " + apiErr.body.Error)
		}
		var existing SavedPipeline
		if s.docs.Get(ctx, kindPipeline, name, &existing) == nil {
			return "", fmt.Errorf("a pipeline named %q already exists; choose another name", name)
		}
		act.Pipeline, act.Detail, act.Note = name, a.YAML, a.Description
		act.Effects = append([]string{fmt.Sprintf("Saves a new pipeline %q. It does not run.", name)}, pipelineEffects(a.YAML)...)
		if act.Title == "" {
			act.Title = "Save pipeline " + name
		}
	case actRunPipeline:
		var sp SavedPipeline
		if err := s.docs.Get(ctx, kindPipeline, a.Pipeline, &sp); err != nil {
			return "", fmt.Errorf("no saved pipeline %q (see list_pipelines)", a.Pipeline)
		}
		act.Pipeline, act.Detail = sp.ID, sp.YAML
		act.Effects = append([]string{fmt.Sprintf("Starts a run of %q (version %d) now, on this server's Docker.", sp.ID, sp.Version)}, pipelineEffects(sp.YAML)...)
		if act.Title == "" {
			act.Title = "Run " + sp.ID
		}
	case actSaveSkill, actSaveAgent, actSaveWorkflow:
		// Creating these is for administrators in the console, so the card is too.
		if !t.p.Role.AtLeast(authn.Admin) {
			return "", errors.New("only an administrator can create skills, agents and workflows; tell the person what you would write instead")
		}
		if len(a.Spec) == 0 || len(a.Spec) > maxActionYAML {
			return "", errors.New("give the skill, agent or workflow as spec (see the tool's description)")
		}
		name, detail, effects, msg := s.checkDefinition(ctx, a.Action, a.Spec)
		if msg != "" {
			return "", errors.New("this is not valid, fix it first: " + msg)
		}
		act.Spec, act.Pipeline, act.Detail, act.Effects = a.Spec, name, detail, effects
		if act.Title == "" {
			act.Title = map[string]string{actSaveSkill: "Create skill ", actSaveAgent: "Create agent ", actSaveWorkflow: "Create workflow "}[a.Action] + name
		}
	default:
		return "", errors.New(`action must be "save_pipeline", "run_pipeline", "save_skill", "save_agent" or "save_workflow"`)
	}
	if n := s.pendingActionCount(ctx, t.p.UserID); n >= maxPendingAction {
		return "", errors.New("too many actions are waiting for the person; ask them to confirm or dismiss some")
	}
	if err := s.docs.Create(ctx, kindAssistAction, act.ID, act); err != nil {
		return "", errors.New("could not prepare the action")
	}
	ip, _ := ctx.Value(mcpIPKey{}).(string)
	s.audit(ctx, t.p, ip, "assistant.action.prepare", "action/"+act.ID, act.Kind+" "+act.Pipeline, true)
	if sink, ok := ctx.Value(lookupSinkKey{}).(*lookupSink); ok {
		sink.actions = append(sink.actions, act)
	}
	return "Prepared. NOTHING has happened yet: the person sees a card with the exact effects and must press Confirm. Tell them what you prepared and that it waits for them.", nil
}

// pipelineEffects says in plain words what a pipeline file can do, so the card does not rely on the model's summary.
func pipelineEffects(text string) []string {
	p, err := pipeline.Load(strings.NewReader(text))
	if err != nil {
		return nil
	}
	var out []string
	for _, st := range p.Steps {
		line := fmt.Sprintf("Step %q runs image %s", st.ID, st.Image)
		if st.Permissions.Network == "" || st.Permissions.Network == "egress" {
			line += ", with network access"
		} else {
			line += ", with no network"
		}
		if len(st.Permissions.Secrets) > 0 {
			line += ", and can read the secrets " + strings.Join(st.Permissions.Secrets, ", ")
		}
		out = append(out, line+".")
	}
	return out
}

func (s *Server) pendingActionCount(ctx context.Context, owner string) int {
	docs, err := s.docs.List(ctx, kindAssistAction, docstore.ListOptions{Desc: true, Limit: 200})
	if err != nil {
		return 0
	}
	all, _ := docstore.Decode[AssistAction](docs)
	n := 0
	for _, a := range all {
		if a.Owner == owner && a.Status == actPending && time.Now().Before(a.Expires) {
			n++
		}
	}
	return n
}

// actionFor returns the person's own action, or false (404 for anyone else, including administrators).
func (s *Server) actionFor(r *http.Request, p Principal) (AssistAction, bool) {
	var a AssistAction
	id := r.PathValue("id")
	if !conversationIDRE.MatchString(id) || s.docs.Get(r.Context(), kindAssistAction, id, &a) != nil || p.UserID == "" || a.Owner != p.UserID {
		return AssistAction{}, false
	}
	return a, true
}

func (s *Server) handleConfirmAction(w http.ResponseWriter, r *http.Request) {
	p := principalFrom(r.Context())
	actionMu.Lock()
	defer actionMu.Unlock()
	a, ok := s.actionFor(r, p)
	if !ok {
		writeError(w, http.StatusNotFound, "no such action")
		return
	}
	switch {
	case a.Status != actPending:
		writeError(w, http.StatusConflict, "this action was already "+a.Status)
		return
	case time.Now().After(a.Expires):
		writeError(w, http.StatusConflict, "this action expired; ask the assistant again")
		return
	}
	// Mark it first: if anything below fails, it cannot be replayed by pressing again.
	a.Status = actFailed
	var result, link string
	var apiErr *apiError
	switch a.Kind {
	case actSavePipeline:
		result, link, apiErr = s.doSavePipeline(r.Context(), p, a)
	case actRunPipeline:
		result, link, apiErr = s.doRunPipeline(r.Context(), p, a)
	case actSaveSkill, actSaveAgent, actSaveWorkflow:
		result, link, apiErr = s.doSaveDefinition(r.Context(), p, clientIP(r), a)
	default:
		apiErr = badRequest("unknown action")
	}
	if apiErr != nil {
		a.Result = apiErr.body.Error
		s.docs.Put(r.Context(), kindAssistAction, a.ID, a)
		s.auditReq(r, "assistant.action.confirm", "action/"+a.ID, a.Kind+" failed: "+truncate(apiErr.body.Error, 160), false)
		apiErr.write(w)
		return
	}
	a.Status, a.Result, a.Link = actDone, result, link
	s.docs.Put(r.Context(), kindAssistAction, a.ID, a)
	s.auditReq(r, "assistant.action.confirm", "action/"+a.ID, a.Kind+" "+a.Pipeline, true)
	writeJSON(w, http.StatusOK, a)
}

func (s *Server) handleDismissAction(w http.ResponseWriter, r *http.Request) {
	p := principalFrom(r.Context())
	actionMu.Lock()
	defer actionMu.Unlock()
	a, ok := s.actionFor(r, p)
	if !ok {
		writeError(w, http.StatusNotFound, "no such action")
		return
	}
	if a.Status != actPending {
		writeError(w, http.StatusConflict, "this action was already "+a.Status)
		return
	}
	a.Status = actDismissed
	s.docs.Put(r.Context(), kindAssistAction, a.ID, a)
	s.auditReq(r, "assistant.action.dismiss", "action/"+a.ID, a.Kind+" "+a.Pipeline, true)
	writeJSON(w, http.StatusOK, a)
}

// doSavePipeline does what the console's create does, after checking the file again.
func (s *Server) doSavePipeline(ctx context.Context, p Principal, a AssistAction) (string, string, *apiError) {
	name, apiErr := s.checkSavable(a.Detail, a.Note)
	if apiErr != nil {
		return "", "", apiErr
	}
	now := time.Now().UTC()
	sp := SavedPipeline{ID: name, Name: name, Description: a.Note, YAML: a.Detail, Version: 1,
		CreatedAt: now, CreatedBy: p.Name, UpdatedAt: now, UpdatedBy: p.Name}
	if err := s.docs.Create(ctx, kindPipeline, sp.ID, sp); errors.Is(err, docstore.ErrExists) {
		return "", "", &apiError{http.StatusConflict, ErrorJSON{Error: fmt.Sprintf("a pipeline named %q already exists", name)}}
	} else if err != nil {
		return "", "", &apiError{http.StatusInternalServerError, ErrorJSON{Error: "could not save the pipeline"}}
	}
	s.audit(ctx, p, "", "pipeline.create", "pipeline/"+sp.ID, "version 1, by the assistant, confirmed by the person", true)
	return fmt.Sprintf("Saved pipeline %q.", name), "/pipelines/" + name, nil
}

// doRunPipeline does what the console's Run button does.
func (s *Server) doRunPipeline(ctx context.Context, p Principal, a AssistAction) (string, string, *apiError) {
	var sp SavedPipeline
	if err := s.docs.Get(ctx, kindPipeline, a.Pipeline, &sp); err != nil {
		return "", "", &apiError{http.StatusNotFound, ErrorJSON{Error: "that pipeline no longer exists"}}
	}
	run, steps, apiErr := s.runSaved(ctx, sp)
	if apiErr != nil {
		return "", "", apiErr
	}
	s.recordRunMeta(ctx, run.ID, runMeta{Actor: p.Name, PipelineID: sp.ID, Version: sp.Version})
	s.audit(ctx, p, "", "run.start", "run/"+run.ID, fmt.Sprintf("%s v%d, by the assistant, confirmed by the person", sp.ID, sp.Version)+sourceNote(run), true)
	_ = steps
	return "Started run " + run.ID + ".", "/runs/" + run.ID, nil
}

// checkDefinition checks a skill, agent or workflow the assistant wrote, with the console's own rules, and says in plain
// words what saving it would do. It saves nothing. msg is set when it is not valid.
func (s *Server) checkDefinition(ctx context.Context, kind string, spec json.RawMessage) (name, detail string, effects []string, msg string) {
	strict := func(v any) string {
		dec := json.NewDecoder(strings.NewReader(string(spec)))
		dec.DisallowUnknownFields()
		if err := dec.Decode(v); err != nil {
			return "the spec does not match the expected fields: " + err.Error()
		}
		return ""
	}
	switch kind {
	case actSaveSkill:
		var req skillRequest
		if m := strict(&req); m != "" {
			return "", "", nil, m
		}
		if m := req.check(); m != "" {
			return "", "", nil, m
		}
		return strings.TrimSpace(req.Name), req.Body, []string{fmt.Sprintf("Creates a new skill %q, version 1. No agent uses it until one is edited to follow it.", strings.TrimSpace(req.Name))}, ""
	case actSaveAgent:
		var req agentRequest
		if m := strict(&req); m != "" {
			return "", "", nil, m
		}
		v, m := s.resolveAgent(ctx, req)
		if m != "" {
			return "", "", nil, m
		}
		name = strings.TrimSpace(req.Name)
		effects = []string{fmt.Sprintf("Creates a new agent %q, version 1, %s.", name, map[string]string{tierAdvisory: "that only reads and explains", tierReview: "that may also draft changes a person approves"}[v.Tier])}
		if len(v.Tools) == 0 {
			effects = append(effects, "It can use no tools: it answers only from what it is told.")
		} else {
			effects = append(effects, "It can use: "+strings.Join(v.Tools, ", ")+". Each runs as the person who asks, with their role.")
		}
		for _, pin := range v.Skills {
			effects = append(effects, fmt.Sprintf("It follows the skill %s at version %d.", pin.ID, pin.N))
		}
		return name, v.Instructions, effects, ""
	case actSaveWorkflow:
		var req wfRequest
		if m := strict(&req); m != "" {
			return "", "", nil, m
		}
		v, m := s.resolveWorkflow(ctx, req)
		if m != "" {
			return "", "", nil, m
		}
		name = strings.TrimSpace(req.Name)
		when := "only when someone starts it"
		if v.EveryMinutes > 0 {
			when = fmt.Sprintf("by itself every %d minutes, and when someone starts it", v.EveryMinutes)
		}
		effects = []string{fmt.Sprintf("Creates a new workflow %q, version 1, that runs %s, as a %s.", name, when, v.RunAs)}
		var b strings.Builder
		for i, st := range v.Stages {
			gate := ""
			if st.Gate {
				gate = " (waits for an administrator to release it)"
			}
			fmt.Fprintf(&b, "%d. %s%s\n", i+1, st.Name, gate)
			for _, sp := range st.Steps {
				fmt.Fprintf(&b, "   - %s@%d: %s\n", sp.Agent, sp.AgentN, sp.Task)
			}
			effects = append(effects, fmt.Sprintf("Stage %d, %s, runs %d step(s) together%s.", i+1, st.Name, len(st.Steps), gate))
		}
		return name, strings.TrimSpace(b.String()), effects, ""
	}
	return "", "", nil, "unknown kind"
}

// doSaveDefinition saves a confirmed skill, agent or workflow, checking it again: things may have changed since the
// card was prepared (an agent turned off, a name taken).
func (s *Server) doSaveDefinition(ctx context.Context, p Principal, ip string, a AssistAction) (string, string, *apiError) {
	if !p.Role.AtLeast(authn.Admin) {
		return "", "", &apiError{http.StatusForbidden, ErrorJSON{Error: "only an administrator can create skills, agents and workflows"}}
	}
	if _, _, _, msg := s.checkDefinition(ctx, a.Kind, a.Spec); msg != "" {
		return "", "", badRequest("this is no longer valid: " + msg)
	}
	const how = ", written by the assistant, confirmed by the person"
	fail := &apiError{http.StatusInternalServerError, ErrorJSON{Error: "could not save it"}}
	switch a.Kind {
	case actSaveSkill:
		var req skillRequest
		json.Unmarshal(a.Spec, &req)
		sk, err := s.saveNewSkill(ctx, p, ip, req, how)
		if err != nil {
			return "", "", fail
		}
		return fmt.Sprintf("Created skill %q.", sk.Name), "/skills/" + sk.ID, nil
	case actSaveAgent:
		var req agentRequest
		json.Unmarshal(a.Spec, &req)
		v, _ := s.resolveAgent(ctx, req)
		ag, err := s.saveNewAgent(ctx, p, ip, req, v, how)
		if err != nil {
			return "", "", fail
		}
		return fmt.Sprintf("Created agent %q.", ag.Name), "/agents/" + ag.ID, nil
	default:
		var req wfRequest
		json.Unmarshal(a.Spec, &req)
		v, _ := s.resolveWorkflow(ctx, req)
		wf, err := s.saveNewWorkflow(ctx, p, ip, req, v, how)
		if err != nil {
			return "", "", fail
		}
		return fmt.Sprintf("Created workflow %q.", wf.Name), "/workflows/" + wf.ID, nil
	}
}
