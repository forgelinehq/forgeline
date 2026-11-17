package server

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/forgelinehq/forgeline/internal/authn"
	"github.com/forgelinehq/forgeline/internal/docstore"
	"github.com/forgelinehq/forgeline/internal/store"
)

// Triggers for graph workflows (design note 044, part 4): a signed webhook, and a pipeline run that failed. Either
// starts the workflow's current version, never above its run-as role, as "workflow:<id>" like a schedule. What
// started it reaches the graph as the state value "trigger", which agents read only as data.

const (
	kindWFHook         = "aiwfhook"
	maxHookBody        = 64 << 10
	maxHookRunsPerHour = 30
	trigWebhook        = "webhook"
	trigRunFailed      = "run-failed"
)

// wfHook is a workflow's webhook secret, sealed by the vault.
type wfHook struct {
	ID        string    `json:"id"`
	Sealed    string    `json:"sealed"`
	CreatedBy string    `json:"created_by"`
	CreatedAt time.Time `json:"created_at"`
}

// handleWorkflowHookSecret makes a new webhook secret for a workflow and shows it once. The old one stops working.
func (s *Server) handleWorkflowHookSecret(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	wf, err := s.loadWorkflow(ctx, r.PathValue("id"))
	if err != nil {
		s.defError(w, "workflow", err)
		return
	}
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		s.internalError(w, "making a secret", err)
		return
	}
	secret := hex.EncodeToString(b)
	sealed, err := s.vault.Seal("workflow-hook/"+wf.ID, secret)
	if err != nil {
		s.internalError(w, "sealing the secret", err)
		return
	}
	if err := s.docs.Put(ctx, kindWFHook, wf.ID, wfHook{ID: wf.ID, Sealed: sealed, CreatedBy: principalFrom(ctx).Name, CreatedAt: time.Now().UTC()}); err != nil {
		s.internalError(w, "saving the secret", err)
		return
	}
	s.auditReq(r, "ai.workflow.hook-secret", "workflow/"+wf.ID, "a new webhook secret; the old one no longer works", true)
	writeJSON(w, http.StatusOK, map[string]any{"secret": secret, "url": "/hooks/workflows/" + wf.ID,
		"how": "POST JSON to the url with the header X-Forgeline-Signature-256: sha256=<HMAC-SHA256 of the body with this secret, in hex> (GitHub's X-Hub-Signature-256 works too)."})
}

// handleWorkflowHook starts a workflow from a signed request. Everything that is wrong answers the same way, so the
// endpoint says nothing about which workflows exist or take webhooks.
func (s *Server) handleWorkflowHook(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxHookBody))
	if err != nil {
		writeError(w, http.StatusRequestEntityTooLarge, fmt.Sprintf("the body is larger than %d KB", maxHookBody>>10))
		return
	}
	denied := func() { writeError(w, http.StatusUnauthorized, "the signature does not match") }
	id := r.PathValue("id")
	var hook wfHook
	if err := s.docs.Get(ctx, kindWFHook, id, &hook); err != nil {
		denied()
		return
	}
	secret, err := s.vault.Open("workflow-hook/"+id, hook.Sealed)
	if err != nil {
		denied()
		return
	}
	sig := r.Header.Get("X-Forgeline-Signature-256")
	if sig == "" {
		sig = r.Header.Get("X-Hub-Signature-256")
	}
	if !validSignature(secret, sig, body) {
		_ = s.audit(ctx, Principal{Name: "anonymous"}, clientIP(r), "ai.workflow.hook", "workflow/"+id, "a bad signature", false)
		denied()
		return
	}
	wf, err := s.loadWorkflow(ctx, id)
	if err != nil {
		denied()
		return
	}
	v, ok := wf.version(wf.Current)
	switch {
	case !ok || v.Graph == nil || !v.Webhook:
		writeError(w, http.StatusConflict, "the current version of this workflow does not start from a webhook")
		return
	case wf.Disabled:
		writeError(w, http.StatusConflict, "this workflow is turned off")
		return
	case s.recentRuns(ctx, id, trigWebhook, time.Hour) >= maxHookRunsPerHour:
		writeError(w, http.StatusTooManyRequests, fmt.Sprintf("this workflow already started %d times from its webhook in the last hour", maxHookRunsPerHour))
		return
	}
	payload := string(body)
	if !json.Valid(body) {
		payload = strings.ToValidUTF8(payload, "?")
	}
	if ev := r.Header.Get("X-GitHub-Event"); ev != "" {
		payload = "GitHub event: " + truncate(ev, 40) + "\n" + payload
	}
	run, err := s.startTriggered(ctx, wf, v, trigWebhook, payload)
	if errors.Is(err, errWFBusy) {
		writeError(w, http.StatusConflict, "a run of this workflow is already in progress")
		return
	}
	if err != nil {
		s.internalError(w, "starting the workflow", err)
		return
	}
	_ = s.audit(ctx, Principal{Name: "workflow:" + id}, clientIP(r), "ai.workflow.run", "workflow/"+id, fmt.Sprintf("v%d by webhook, run %s", v.N, run.ID), true)
	writeJSON(w, http.StatusAccepted, map[string]any{"run": run.ID})
}

// startTriggered starts a workflow with what started it in the state.
func (s *Server) startTriggered(ctx context.Context, wf Workflow, v WFVersion, trigger, payload string) (WFRun, error) {
	return s.startRunWith(ctx, wf, v, trigger, Principal{Name: "workflow:" + wf.ID, Role: authn.Role(v.RunAs)}, map[string]string{"trigger": truncate(payload, maxStateValue)})
}

// recentRuns counts a workflow's runs started by a trigger within a window.
func (s *Server) recentRuns(ctx context.Context, workflow, trigger string, within time.Duration) int {
	docs, err := s.docs.List(ctx, kindWorkflowRun, docstore.ListOptions{})
	if err != nil {
		return maxHookRunsPerHour // cannot tell: refuse rather than flood
	}
	runs, _ := docstore.Decode[WFRun](docs)
	n := 0
	for _, r := range runs {
		if r.Workflow == workflow && r.Trigger == trigger && time.Since(r.StartedAt) < within {
			n++
		}
	}
	return n
}

// onPipelineRunFailed starts every graph workflow whose current version asks to follow failed runs of that pipeline
// (or of any, "*"). A workflow already running skips this one; it is logged.
func (s *Server) onPipelineRunFailed(run store.Run, reason string) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	all, err := s.listWorkflows(ctx)
	if err != nil {
		return
	}
	payload, _ := json.Marshal(map[string]string{"run": run.ID, "pipeline": run.PipelineName, "reason": truncate(reason, 500)})
	for _, wf := range all {
		v, ok := wf.version(wf.Current)
		if wf.Disabled || !ok || v.Graph == nil || !followsPipeline(v.OnRunFailed, run.PipelineName) {
			continue
		}
		if s.recentRuns(ctx, wf.ID, trigRunFailed, time.Hour) >= maxHookRunsPerHour {
			s.log.Warn("workflow trigger skipped: too many runs this hour", "workflow", wf.ID)
			continue
		}
		r, err := s.startTriggered(ctx, wf, v, trigRunFailed, string(payload))
		if err != nil {
			s.log.Warn("workflow trigger skipped", "workflow", wf.ID, "run", run.ID, "err", err)
			continue
		}
		_ = s.audit(ctx, Principal{Name: "workflow:" + wf.ID, Role: authn.Role(v.RunAs)}, "", "ai.workflow.run", "workflow/"+wf.ID, fmt.Sprintf("v%d after pipeline run %s failed, run %s", v.N, run.ID, r.ID), true)
	}
}

func followsPipeline(names []string, pipeline string) bool {
	for _, n := range names {
		if n == "*" || n == pipeline {
			return true
		}
	}
	return false
}

// pipelineNameRE is the shape of a pipeline name (internal/pipeline's idPattern).
var pipelineNameRE = regexp.MustCompile(`^[a-z][a-z0-9-]{0,62}$`)
