package server

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/forgelinehq/forgeline/internal/authn"
)

// The agent programs in the sandbox (design note 053). An agent that runs over ACP is a program inside the person's
// sandbox container: Forgeline does not ship it, so it can be missing, out of date, or not signed in. This file knows, for
// each ready-made agent, how to tell whether it is there, how to install the latest version, and what makes it work (a token
// in the vault, or a sign-in inside the sandbox). The Settings page, the chat and the assistant's own cards all use it, so a
// missing program is found and offered for installation instead of failing with "command not found".
//
// Sources, checked 2026-11-23: @github/copilot 1.0.114 on npm; @agentclientprotocol/codex-acp 2.10.0 (Apache-2.0, depends on
// @openai/codex ^0.171.0; the older @zed-industries/codex-acp is at 0.16.0); the Antigravity installer at
// https://antigravity.google/cli/install.sh, which puts agy in ~/.local/bin and is safe to run again.

type engineSpec struct {
	Harness string
	Label   string
	Binary  string // the program the preset starts
	// Install is run by `sh -c` as root in the person's sandbox, with HOME=/work/.agent, to put the latest version there.
	Install string
	Source  string // where it comes from, in plain words, for the card
	// Version is a shell command that prints the installed version; empty means "installed" is all that can be said.
	Version string
	// PathPrefix is added to PATH for the check, when the program is not installed where PATH looks.
	PathPrefix string
	// TokenName is a vault secret that signs the agent in without a person at a terminal. It is passed in the environment as
	// TokenEnv. Empty when the agent has no such way.
	TokenName, TokenEnv, TokenHelp string
	// Login is the command that signs the agent in by hand inside the sandbox, and what to know about it.
	Login, LoginHelp string
	// Checked says what was verified against the real program, so the page does not claim more.
	Checked string
}

var engineSpecs = map[string]engineSpec{
	"copilot": {
		Harness: "copilot", Label: "GitHub Copilot CLI", Binary: "copilot",
		Install: "npm install -g @github/copilot@latest", Source: "npm (@github/copilot)",
		Version:   "copilot --version | head -n 1",
		TokenName: "COPILOT_GITHUB_TOKEN", TokenEnv: "COPILOT_GITHUB_TOKEN",
		TokenHelp: "a fine-grained personal access token with the \"Copilot Requests\" permission (a classic ghp_ token is not supported), or an OAuth token from the GitHub CLI",
		Login:     "copilot login --device-code",
		LoginHelp: "Run it in a terminal (docker exec -it), approve the code at github.com/login/device, and answer y to \"store the token in a plaintext config file\": the sandbox has no keychain and without it the token is dropped.",
		Checked:   "installed, signed in and run over ACP inside the sandbox with the real program (2026-11-23); the token from the vault was not yet tried against a real account",
	},
	"codex": {
		Harness: "codex", Label: "Codex (codex-acp adapter)", Binary: "codex-acp",
		Install: "npm install -g @agentclientprotocol/codex-acp@latest", Source: "npm (@agentclientprotocol/codex-acp, which brings @openai/codex)",
		Version:   "codex-acp --version | head -n 1",
		TokenName: "CODEX_API_KEY", TokenEnv: "CODEX_API_KEY",
		TokenHelp: "an OpenAI API key (the adapter also reads OPENAI_API_KEY)",
		Login:     "codex login --device-auth",
		LoginHelp: "Sign in with a ChatGPT account inside the sandbox, or store an API key in the vault instead.",
		Checked:   "nothing: package names and versions were read from npm, the adapter has not been run here",
	},
	"antigravity": {
		Harness: "antigravity", Label: "Antigravity CLI (agy)", Binary: "agy",
		Install: "curl -fsSL https://antigravity.google/cli/install.sh | bash", Source: "Google's installer script (it downloads the agy binary into ~/.local/bin)",
		Version:    "agy --version | head -n 1",
		PathPrefix: "/work/.antigravity/bin:$HOME/.local/bin",
		TokenName:  geminiKeyName, TokenEnv: geminiKeyName,
		TokenHelp: "a Gemini API key; Forgeline passes it in and asks agy to sign in with it",
		Login:     "agy",
		LoginHelp: "Start it in a terminal inside the sandbox (docker exec -it) and follow its sign-in.",
		Checked:   "initialize and session/new against the real agy on the server host; not signed in or run inside the sandbox",
	},
}

// engineOrder is the order the Settings page lists them in.
var engineOrder = []string{"antigravity", "copilot", "codex"}

func engineSpecFor(harness string) (engineSpec, bool) {
	sp, ok := engineSpecs[harness]
	return sp, ok
}

// EngineStatus is what Settings shows for one agent.
type EngineStatus struct {
	Harness   string `json:"harness"`
	Label     string `json:"label"`
	Binary    string `json:"binary"`
	Installed bool   `json:"installed"`
	Version   string `json:"version,omitempty"`
	Install   string `json:"install"`
	Source    string `json:"source"`
	TokenName string `json:"token_name,omitempty"`
	TokenSet  bool   `json:"token_set"`
	TokenHelp string `json:"token_help,omitempty"`
	Login     string `json:"login,omitempty"`
	LoginHelp string `json:"login_help,omitempty"`
	Checked   string `json:"checked"`
	Error     string `json:"error,omitempty"`
}

// engineContainer makes sure the person's sandbox exists with the engine image and the mounts every agent run uses, and
// returns its name. An agent is installed into this container, so it must be the one the agent will run in.
func (s *Server) engineContainer(ctx context.Context, p Principal) (ctr, bridge string, err error) {
	st := s.loadSandbox(ctx)
	if !st.Enabled {
		return "", "", errors.New("the sandbox is off: an administrator turns it on under Settings › Assistant permissions")
	}
	if err := s.ensureEngineImage(ctx); err != nil {
		return "", "", err
	}
	bridge = engineBridgeDir(s.cfg.DataDir, p)
	if err := s.engineSocket(bridge); err != nil {
		return "", "", fmt.Errorf("opening the agent's socket: %v", err)
	}
	bin, err := s.engineBinary()
	if err != nil {
		return "", "", fmt.Errorf("preparing the bridge: %v", err)
	}
	ctr, err = s.ensureSandboxWith(ctx, p, engineImage, st, "-v", bridge+":/run/forgeline:ro", "-v", bin+":/opt/forgeline:ro")
	return ctr, bridge, err
}

const engineMissing = "__forgeline_missing__"

// probeEngine asks the container whether the program is there, and which version.
func (s *Server) probeEngine(ctx context.Context, ctr string, sp engineSpec) (installed bool, version string, err error) {
	path := ""
	if sp.PathPrefix != "" {
		path = "export PATH=" + sp.PathPrefix + ":$PATH; "
	}
	show := "echo installed"
	if sp.Version != "" {
		show = sp.Version
	}
	script := path + "if command -v " + sp.Binary + " >/dev/null 2>&1; then " + show + "; else echo " + engineMissing + "; fi"
	out, stderr, err := s.run(ctx, "docker", "exec", "-e", "HOME=/work/.agent", ctr, "sh", "-c", script)
	if err != nil {
		return false, "", fmt.Errorf("asking the sandbox: %s", dockerError(stderr, err))
	}
	line := strings.TrimSpace(strings.SplitN(strings.TrimSpace(string(out)), "\n", 2)[0])
	if line == engineMissing || line == "" {
		return false, "", nil
	}
	if line == "installed" {
		line = ""
	}
	return true, truncate(line, 120), nil
}

func (s *Server) engineStatusFor(ctx context.Context, ctr string, sp engineSpec) EngineStatus {
	st := EngineStatus{Harness: sp.Harness, Label: sp.Label, Binary: sp.Binary, Install: sp.Install, Source: sp.Source,
		TokenName: sp.TokenName, TokenHelp: sp.TokenHelp, Login: sp.Login, LoginHelp: sp.LoginHelp, Checked: sp.Checked}
	if sp.TokenName != "" {
		_, st.TokenSet = s.internalSecretValue(ctx, sp.TokenName)
	}
	if ctr == "" {
		return st
	}
	installed, version, err := s.probeEngine(ctx, ctr, sp)
	if err != nil {
		st.Error = err.Error()
		return st
	}
	st.Installed, st.Version = installed, version
	return st
}

// installEngine installs the latest version of an agent in the person's sandbox and says what it found.
func (s *Server) installEngine(ctx context.Context, p Principal, harness string) (EngineStatus, error) {
	sp, ok := engineSpecFor(harness)
	if !ok {
		return EngineStatus{}, fmt.Errorf("no agent called %q: %s", harness, strings.Join(engineOrder, ", "))
	}
	sbx := s.loadSandbox(ctx)
	if sbx.Network == sandboxNetNone {
		return EngineStatus{}, errors.New("installing needs the network, and the sandbox network is set to none")
	}
	ctr, _, err := s.engineContainer(ctx, p)
	if err != nil {
		return EngineStatus{}, err
	}
	lctx := context.WithValue(ctx, cmdLimitKey{}, 15*time.Minute)
	_, stderr, err := s.run(lctx, "docker", "exec", "-u", "root", "-e", "HOME=/work/.agent", ctr, "sh", "-c", sp.Install)
	if err != nil {
		_ = s.audit(ctx, p, "", "assistant.engine.install", sp.Harness, "failed: "+truncate(dockerError(stderr, err), 200), false)
		return s.engineStatusFor(ctx, ctr, sp), fmt.Errorf("the install did not finish: %s", dockerError(stderr, err))
	}
	st := s.engineStatusFor(ctx, ctr, sp)
	if st.Error == "" && !st.Installed {
		_ = s.audit(ctx, p, "", "assistant.engine.install", sp.Harness, "ran, but "+sp.Binary+" is still not found", false)
		return st, fmt.Errorf("the install ran, but %s is still not found in the sandbox", sp.Binary)
	}
	_ = s.audit(ctx, p, "", "assistant.engine.install", sp.Harness, "installed "+sp.Label+versionNote(st.Version), true)
	return st, nil
}

// versionNote is " (<what the program printed>)", or nothing.
func versionNote(v string) string {
	if v == "" {
		return ""
	}
	return " (" + v + ")"
}

// howToMakeItWork says, in plain words, what is left to do once an agent is installed.
func howToMakeItWork(sp engineSpec, tokenSet bool) string {
	var b strings.Builder
	if sp.TokenName != "" {
		if tokenSet {
			b.WriteString("A token is stored in the vault as " + sp.TokenName + ", so it signs in by itself. ")
		} else {
			fmt.Fprintf(&b, "To make it work: store a token in the vault (Settings › Secrets) as %s and Forgeline signs it in by itself; use %s. ", sp.TokenName, sp.TokenHelp)
		}
	}
	if sp.Login != "" {
		fmt.Fprintf(&b, "Or sign in once by hand inside the sandbox: `%s`. %s", sp.Login, sp.LoginHelp)
	}
	return strings.TrimSpace(b.String())
}

// --- the API ---

func (s *Server) handleListEngines(w http.ResponseWriter, r *http.Request) {
	ctx, p := r.Context(), principalFrom(r.Context())
	out := make([]EngineStatus, 0, len(engineOrder))
	if !s.loadSandbox(ctx).Enabled {
		for _, h := range engineOrder {
			st := s.engineStatusFor(ctx, "", engineSpecs[h])
			st.Error = "the sandbox is off, so nothing can be checked or installed: an administrator turns it on under Settings › Assistant permissions"
			out = append(out, st)
		}
		writeJSON(w, http.StatusOK, map[string]any{"sandbox": false, "engines": out})
		return
	}
	ctr, _, err := s.engineContainer(ctx, p)
	for _, h := range engineOrder {
		st := s.engineStatusFor(ctx, ctr, engineSpecs[h])
		if err != nil {
			st.Error = err.Error()
		}
		out = append(out, st)
	}
	writeJSON(w, http.StatusOK, map[string]any{"sandbox": true, "engines": out})
}

func (s *Server) handleInstallEngine(w http.ResponseWriter, r *http.Request) {
	ctx, p := r.Context(), principalFrom(r.Context())
	if _, ok := engineSpecFor(r.PathValue("harness")); !ok {
		writeError(w, http.StatusNotFound, fmt.Sprintf("no agent called %q: %s", r.PathValue("harness"), strings.Join(engineOrder, ", ")))
		return
	}
	sbx := s.loadSandbox(ctx)
	switch {
	case !sbx.Enabled:
		writeError(w, http.StatusConflict, "the sandbox is off: an administrator turns it on under Settings › Assistant permissions")
		return
	case sbx.Network == sandboxNetNone:
		writeError(w, http.StatusConflict, "installing needs the network, and the sandbox network is set to none")
		return
	}
	st, err := s.installEngine(ctx, p, r.PathValue("harness"))
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]any{"error": err.Error(), "engine": st})
		return
	}
	sp, _ := engineSpecFor(st.Harness)
	s.auditReq(r, "assistant.engine.install.request", "engine/"+st.Harness, "installed "+st.Label+versionNote(st.Version), true)
	writeJSON(w, http.StatusOK, map[string]any{"engine": st, "next": howToMakeItWork(sp, st.TokenSet)})
}

// --- "install it" as a card, for the assistant (and for the chat when the program is missing) ---

const actInstallEngine = "install_engine"

// prepareInstallEngine fills in the card that installs an agent. Installing software is an administrator's, and it always
// waits for the person, autopilot included (it is a capability card).
func (s *Server) prepareInstallEngine(ctx context.Context, p Principal, harness string, act *AssistAction) error {
	if !p.Role.AtLeast(authn.Admin) {
		return errors.New("only an administrator can install an agent in the sandbox; tell the person to ask one")
	}
	sp, ok := engineSpecFor(harness)
	if !ok {
		return fmt.Errorf("harness is one of %s", strings.Join(engineOrder, ", "))
	}
	if !s.loadSandbox(ctx).Enabled {
		return errors.New("the sandbox is off: prepare turn_on_sandbox first, then this")
	}
	act.Pipeline = sp.Harness
	act.Effects = []string{
		fmt.Sprintf("Installs the latest %s in the sandbox, from %s, by running `%s` as root inside the container.", sp.Label, sp.Source, sp.Install),
		"It downloads from the internet through the sandbox's network. Nothing on the server itself changes; the program stays in this sandbox.",
		howToMakeItWork(sp, tokenIsSet(s, ctx, sp)),
	}
	if act.Title == "" {
		act.Title = "Install " + sp.Label
	}
	return nil
}

func tokenIsSet(s *Server, ctx context.Context, sp engineSpec) bool {
	if sp.TokenName == "" {
		return false
	}
	_, ok := s.internalSecretValue(ctx, sp.TokenName)
	return ok
}

func (s *Server) doInstallEngine(ctx context.Context, p Principal, a AssistAction) (string, string, *apiError) {
	if !p.Role.AtLeast(authn.Admin) {
		return "", "", &apiError{http.StatusForbidden, ErrorJSON{Error: "only an administrator can do this"}}
	}
	st, err := s.installEngine(ctx, p, a.Pipeline)
	if err != nil {
		return "", "", &apiError{http.StatusBadGateway, ErrorJSON{Error: err.Error()}}
	}
	sp, _ := engineSpecFor(st.Harness)
	return strings.TrimSpace(fmt.Sprintf("Installed %s%s. %s", st.Label, versionNote(st.Version), howToMakeItWork(sp, st.TokenSet))), "/settings/assistant", nil
}
