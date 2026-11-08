package server

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/forgelinehq/forgeline/internal/docstore"
)

// Onboarding (design note 038, step 1): the first-run steps that get a new server from "installed" to "working",
// without a model. Each step is done when what it sets up exists, read from the server's own state (nothing is
// ticked by hand), and an administrator may skip one. The console shows them on Home until all are done or skipped;
// `forgeline onboard` shows the same list in a terminal. Every step is set up on its own page, through the usual
// checks: onboarding adds no new way to change anything.

const idOnboarding = "onboarding"

// OnboardingStep is one thing to set up.
type OnboardingStep struct {
	ID      string `json:"id"`
	Title   string `json:"title"`
	Why     string `json:"why"`
	Link    string `json:"link"` // the console page that does it
	Action  string `json:"action"`
	Done    bool   `json:"done"`
	Skipped bool   `json:"skipped"`
	Detail  string `json:"detail,omitempty"` // what was found, when done
}

// OnboardingJSON is the list, and whether it is finished (every step done or skipped).
type OnboardingJSON struct {
	Steps    []OnboardingStep `json:"steps"`
	Done     int              `json:"done"`
	Finished bool             `json:"finished"`
}

type onboardingState struct {
	Skipped   map[string]bool `json:"skipped"`
	UpdatedBy string          `json:"updated_by"`
	UpdatedAt time.Time       `json:"updated_at"`
}

type onboardingDef struct {
	id, title, why, link, action string
	done                         func(ctx context.Context, s *Server) (bool, string)
}

func onboardingSteps() []onboardingDef {
	return []onboardingDef{
		{"admin", "Create the first administrator", "Someone has to own the server.", "#/people", "People and roles",
			func(ctx context.Context, s *Server) (bool, string) { return true, "done when the server was set up" }},
		{"people", "Bring in your team", "Invite people with roles, or let them sign in with your company login (single sign-on).", "#/sso", "Set up sign-in",
			func(ctx context.Context, s *Server) (bool, string) {
				if s.loadOIDC(ctx).Enabled {
					return true, "single sign-on is on"
				}
				if n, err := s.docs.Count(ctx, kindUser); err == nil && n > 1 {
					return true, "more than one person can sign in"
				}
				return false, ""
			}},
		{"github", "Connect GitHub", "So pushes start pipelines and results come back as commit statuses, and the assistant can read your repositories.", "#/integrations/github", "Connect GitHub",
			func(ctx context.Context, s *Server) (bool, string) {
				var st githubSettings
				if err := s.docs.Get(ctx, kindSettings, idGitHub, &st); err == nil && st.Enabled {
					if st.AppID != 0 {
						return true, "the GitHub App " + st.AppSlug + " is connected"
					}
					return true, "connected with a token"
				}
				if s.gh() != nil {
					return true, "connected by the server's settings"
				}
				return false, ""
			}},
		{"cloud", "Connect a cloud account", "So you can see what runs, design what should exist, and approve changes. Forgeline uses a login on the server; it never stores your cloud keys.", "#/integrations/aws", "Connect AWS",
			func(ctx context.Context, s *Server) (bool, string) {
				switch {
				case s.loadAWS(ctx).Enabled:
					return true, "AWS is connected"
				case s.loadGCP(ctx).Enabled:
					return true, "Google Cloud is connected"
				case s.loadAzure(ctx).Enabled:
					return true, "Azure is connected"
				}
				return false, ""
			}},
		{"assistant", "Turn on the assistant", "Claude explains failures, drafts pipelines and blueprints, and powers your agents. By an API key, the claude program on the server, or a Claude Code token.", "#/integrations/claude/settings", "Choose a model",
			func(ctx context.Context, s *Server) (bool, string) {
				if b := s.loadAssistant(ctx).Backend; b != assistOff {
					return true, map[string]string{assistAPI: "Claude, by an API key", assistCLI: "the claude program on the server", assistToken: "Claude, by a Claude Code token"}[b]
				}
				return false, ""
			}},
		{"ai-access", "Choose what the AI may touch", "Per cloud service: off, look only, or change with approval. Everything starts at change with approval; a different administrator always approves.", "#/integrations/aws", "Review AI access",
			func(ctx context.Context, s *Server) (bool, string) {
				if p, err := s.loadPolicy(ctx); err == nil && !p.UpdatedAt.IsZero() {
					return true, "reviewed by " + p.UpdatedBy
				}
				return false, ""
			}},
		{"first", "Make your first pipeline or blueprint", "A pipeline builds and tests your code; a blueprint draws what should exist in your cloud.", "#/pipelines", "Start something",
			func(ctx context.Context, s *Server) (bool, string) {
				if n, err := s.docs.Count(ctx, kindBlueprint); err == nil && n > 0 {
					return true, "a blueprint exists"
				}
				if ps, err := s.loadPipelines(ctx); err == nil && len(ps) > 0 {
					return true, "a pipeline exists"
				}
				return false, ""
			}},
	}
}

func (s *Server) loadOnboarding(ctx context.Context) onboardingState {
	var st onboardingState
	if err := s.docs.Get(ctx, kindSettings, idOnboarding, &st); err != nil && !errors.Is(err, docstore.ErrNotFound) {
		s.log.Warn("reading onboarding", "err", err)
	}
	if st.Skipped == nil {
		st.Skipped = map[string]bool{}
	}
	return st
}

func (s *Server) onboarding(ctx context.Context) OnboardingJSON {
	st := s.loadOnboarding(ctx)
	out := OnboardingJSON{Steps: []OnboardingStep{}}
	for _, d := range onboardingSteps() {
		done, detail := d.done(ctx, s)
		step := OnboardingStep{ID: d.id, Title: d.title, Why: d.why, Link: d.link, Action: d.action, Done: done, Detail: detail, Skipped: !done && st.Skipped[d.id]}
		if done {
			out.Done++
		}
		out.Steps = append(out.Steps, step)
	}
	out.Finished = true
	for _, step := range out.Steps {
		if !step.Done && !step.Skipped {
			out.Finished = false
		}
	}
	return out
}

func (s *Server) handleOnboarding(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.onboarding(r.Context()))
}

// handleSkipOnboarding skips a step (or, with "skip": false, brings it back). Administrators only; audited.
func (s *Server) handleSkipOnboarding(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Skip bool `json:"skip"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	id := r.PathValue("step")
	known := false
	for _, d := range onboardingSteps() {
		known = known || d.id == id
	}
	if !known {
		writeError(w, http.StatusNotFound, "no such step")
		return
	}
	p := principalFrom(r.Context())
	st := s.loadOnboarding(r.Context())
	if req.Skip {
		st.Skipped[id] = true
	} else {
		delete(st.Skipped, id)
	}
	st.UpdatedBy, st.UpdatedAt = p.Name, time.Now().UTC()
	if err := s.docs.Put(r.Context(), kindSettings, idOnboarding, st); err != nil {
		s.internalError(w, "saving onboarding", err)
		return
	}
	s.audit(r.Context(), p, clientIP(r), "onboarding.skip", "onboarding/"+id, map[bool]string{true: "skipped", false: "brought back"}[req.Skip], true)
	writeJSON(w, http.StatusOK, s.onboarding(r.Context()))
}
