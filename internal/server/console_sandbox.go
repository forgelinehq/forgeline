package server

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/forgelinehq/forgeline/internal/authn"
)

// The sandbox (design note 052, slice 3): a container Forgeline starts for a person, where the assistant can run any
// command, install anything and write and run code in any language, with the network on. It is where it does
// things, not a limit on what it can do. What it is for is keeping the server's own credentials out of reach of a
// command the model wrote after reading something it should not have trusted: no host mounts, no Docker socket, no
// privileged mode, dropped capabilities, limits on memory, processes and time, and no credential inside unless the
// person names a vault secret on a card.
//
// The honest limits: a container shares the host's kernel (a gVisor runtime is used when the host has one, and the
// status says which), and Forgeline cannot close the container's network to the local network or a cloud metadata
// address without firewall rules on the host. FORGELINE_SANDBOX_NETWORK names a Docker network an operator has
// fenced that way; the page says so.

const (
	runShellTool        = "run_shell"
	idSandbox           = "assistant-sandbox"
	defaultSandboxImage = "ubuntu:24.04"
	maxShellCommand     = 8000
	defaultShellSeconds = 120
	maxShellSeconds     = 900
	maxShellSecrets     = 8
	maxShellOutput      = 12 << 10
	sandboxIdle         = 30 * time.Minute
	sandboxReapEvery    = 2 * time.Minute
	sandboxNetInternet  = "internet"
	sandboxNetNone      = "none"
)

// SandboxSettings is what an administrator decides. The sandbox is off until they turn it on, because it runs
// anything the model writes, with the network, on the server's machine.
type SandboxSettings struct {
	Enabled   bool      `json:"enabled"`
	Network   string    `json:"network"` // internet (default) or none
	UpdatedBy string    `json:"updated_by,omitempty"`
	UpdatedAt time.Time `json:"updated_at,omitempty"`
}

func (s *Server) loadSandbox(ctx context.Context) SandboxSettings {
	var st SandboxSettings
	_ = s.docs.Get(ctx, kindSettings, idSandbox, &st)
	if st.Network == "" {
		st.Network = sandboxNetInternet
	}
	return st
}

// cmdLimitKey lets a caller give a host command more than the usual 25 seconds (a long install in the sandbox).
type cmdLimitKey struct{}

var sandboxImageRE = regexp.MustCompile(`^[a-z0-9][a-z0-9._/-]{0,100}(:[A-Za-z0-9._-]{1,128})?(@sha256:[a-f0-9]{64})?$`)
var sandboxSecretRE = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]{0,63}$`)

func sandboxNames(p Principal, image string) (container, volume, owner string) {
	id := p.UserID
	if id == "" {
		id = p.Name
	}
	o := sha256.Sum256([]byte("owner:" + id))
	im := sha256.Sum256([]byte("image:" + image))
	owner = hex.EncodeToString(o[:])[:12]
	return "forgeline-sbx-" + owner + "-" + hex.EncodeToString(im[:])[:6], "forgeline-sbx-" + owner + "-work", owner
}

var sandboxMu sync.Mutex
var sandboxUsed = map[string]time.Time{} // container name -> last use, for the reaper

func sandboxEnv() string { return strings.TrimSpace(os.Getenv("FORGELINE_SANDBOX_NETWORK")) }

// sandboxRuntime says what isolates the container from the host kernel: gVisor when Docker has it, else nothing
// beyond the container itself.
func (s *Server) sandboxRuntime(ctx context.Context) (flag, label string) {
	out, _, err := s.run(ctx, "docker", "info", "--format", "{{json .Runtimes}}")
	if err == nil && strings.Contains(string(out), `"runsc"`) {
		return "runsc", "gVisor (runsc)"
	}
	return "", "hardened container (shares the host kernel)"
}

// sandboxRunArgs is the whole `docker run`. Everything that makes it a sandbox is here, so a test can read it.
func sandboxRunArgs(name, volume, owner, image, network, runtime string, extra ...string) []string {
	args := []string{"run", "-d", "--name", name, "--label", "forgeline.sandbox=1", "--label", "forgeline.owner=" + owner,
		"-v", volume + ":/work", "-w", "/work", "--hostname", "sandbox",
		"--memory", "2g", "--memory-swap", "2g", "--cpus", "2", "--pids-limit", "512",
		"--cap-drop", "ALL"}
	for _, c := range []string{"CHOWN", "DAC_OVERRIDE", "FOWNER", "SETGID", "SETUID", "KILL"} {
		args = append(args, "--cap-add", c)
	}
	args = append(args, "--security-opt", "no-new-privileges", "--network", network)
	if runtime != "" {
		args = append(args, "--runtime", runtime)
	}
	args = append(args, extra...) // read-only mounts for the agent engine (note 053)
	return append(args, image, "sleep", "infinity")
}

func (s *Server) ensureSandbox(ctx context.Context, p Principal, image string, st SandboxSettings) (string, error) {
	// The agent's own folder (its sign-in and sessions) is in /work: this container sees an empty, read-only one over it.
	return s.ensureSandboxWith(ctx, p, image, st, "--tmpfs", "/work/.agent:ro,size=1m")
}

func (s *Server) ensureSandboxWith(ctx context.Context, p Principal, image string, st SandboxSettings, extra ...string) (string, error) {
	name, volume, owner := sandboxNames(p, image)
	sandboxMu.Lock()
	defer sandboxMu.Unlock()
	sandboxUsed[name] = time.Now()
	if out, _, err := s.run(ctx, "docker", "inspect", "-f", "{{.State.Running}}", name); err == nil {
		if strings.TrimSpace(string(out)) == "true" {
			return name, nil
		}
		if _, e, err := s.run(ctx, "docker", "start", name); err != nil {
			return "", fmt.Errorf("starting the sandbox: %s", dockerError(e, err))
		}
		return name, nil
	}
	network := "bridge"
	switch {
	case st.Network == sandboxNetNone:
		network = "none"
	case sandboxEnv() != "":
		network = sandboxEnv()
	}
	runtime, _ := s.sandboxRuntime(ctx)
	lctx := context.WithValue(ctx, cmdLimitKey{}, 10*time.Minute) // pulling an image can take a while
	if _, e, err := s.run(lctx, "docker", sandboxRunArgs(name, volume, owner, image, network, runtime, extra...)...); err != nil {
		return "", fmt.Errorf("starting the sandbox: %s", dockerError(e, err))
	}
	return name, nil
}

func dockerError(stderr string, err error) string {
	if stderr = strings.TrimSpace(stderr); stderr != "" {
		if i := strings.IndexByte(stderr, '\n'); i > 0 {
			stderr = stderr[:i]
		}
		return truncate(stderr, 300)
	}
	if err != nil {
		return err.Error()
	}
	return "unknown error"
}

// shellOutput keeps the start and the end of what a command printed, with credentials hidden.
func shellOutput(raw string, secrets []string) string {
	for _, v := range secrets {
		if len(v) >= 4 {
			raw = strings.ReplaceAll(raw, v, "[hidden]")
		}
	}
	raw, _ = hideSecrets(raw)
	if len(raw) > maxShellOutput {
		half := maxShellOutput / 2
		raw = raw[:half] + fmt.Sprintf("\n[… %d bytes left out …]\n", len(raw)-maxShellOutput) + raw[len(raw)-half:]
	}
	return raw
}

// lookupRunShell runs a command in the person's sandbox.
func (s *Server) lookupRunShell(ctx context.Context, raw json.RawMessage) (string, error) {
	var a struct {
		Command string   `json:"command"`
		Seconds int      `json:"timeout_seconds"`
		Image   string   `json:"image"`
		Secrets []string `json:"secrets"`
	}
	if err := decodeArgs(raw, &a); err != nil {
		return "", err
	}
	p := principalFrom(ctx)
	if scopeFrom(ctx) != nil {
		return "", errors.New("an agent cannot use the sandbox yet; ask in the chat")
	}
	st := s.loadSandbox(ctx)
	if !st.Enabled {
		return "", errors.New("the sandbox is turned off. An administrator turns it on under Settings › Assistant permissions; until then, say what you would have run")
	}
	switch {
	case strings.TrimSpace(a.Command) == "" || len(a.Command) > maxShellCommand || strings.ContainsRune(a.Command, 0):
		return "", fmt.Errorf("command: 1 to %d characters", maxShellCommand)
	case a.Seconds < 0 || a.Seconds > maxShellSeconds:
		return "", fmt.Errorf("timeout_seconds: up to %d", maxShellSeconds)
	case len(a.Secrets) > maxShellSecrets:
		return "", fmt.Errorf("secrets: at most %d", maxShellSecrets)
	}
	if a.Seconds == 0 {
		a.Seconds = defaultShellSeconds
	}
	if a.Image == "" {
		a.Image = defaultSandboxImage
	}
	if a.Image == engineImage {
		return "", errors.New("image: that container is the agent's own; pick another image")
	}
	if !sandboxImageRE.MatchString(a.Image) {
		return "", errors.New("image: a Docker image name such as ubuntu:24.04, python:3.13 or node:22")
	}
	var env []string
	var values []string
	if len(a.Secrets) > 0 {
		var err error
		if env, values, err = s.sandboxSecrets(ctx, p, a.Secrets, a.Command); err != nil {
			return "", err
		}
	}
	if len(a.Secrets) == 0 { // a command with secrets was shown on the card that gave them
		if err := s.askShell(ctx, p, a.Command, a.Image); err != nil {
			return "", err
		}
	}
	name, err := s.ensureSandbox(ctx, p, a.Image, st)
	if err != nil {
		return "", err
	}
	args := []string{"exec", "-i", "-w", "/work"}
	for _, e := range env {
		args = append(args, "-e", e[:strings.IndexByte(e, '=')]) // the name only: the value reaches docker through its environment, not its arguments
	}
	args = append(args, name, "timeout", "-k", "5", strconv.Itoa(a.Seconds), "bash", "-c", a.Command)
	rctx := context.WithValue(ctx, cmdLimitKey{}, time.Duration(a.Seconds+30)*time.Second)
	if len(env) > 0 {
		rctx = context.WithValue(rctx, extraEnvKey{}, env)
	}
	out, stderr, runErr := s.run(rctx, "docker", args...)
	text := string(out)
	if stderr != "" {
		text += "\n" + stderr
	}
	code := 0
	var ee *exec.ExitError
	if errors.As(runErr, &ee) {
		code = ee.ExitCode()
	} else if runErr != nil {
		code = -1
		if text == "" {
			text = runErr.Error()
		}
	}
	text = strings.TrimSpace(shellOutput(text, values))
	switch code {
	case 0:
		if text == "" {
			return "Exit 0, no output.", nil
		}
		return "Exit 0.\n" + text, nil
	case 124, 137:
		return fmt.Sprintf("Timed out after %d seconds (exit %d).\n%s", a.Seconds, code, text), nil
	}
	return fmt.Sprintf("Exit %d.\n%s", code, text), nil
}

// sandboxSecrets resolves the vault secrets a command may be given. Giving a secret to a command is the one thing the
// sandbox is not allowed to do by itself: only an administrator, in the chat, on a card that names the secrets and
// shows the command, once. The model sees the names, never the values.
func (s *Server) sandboxSecrets(ctx context.Context, p Principal, names []string, command string) (env, values []string, err error) {
	have, err := s.secretNames(ctx)
	if err != nil {
		return nil, nil, errors.New("the vault could not be read, so no secret is given to the sandbox")
	}
	seen := map[string]bool{}
	for _, n := range names {
		switch {
		case !sandboxSecretRE.MatchString(n):
			return nil, nil, fmt.Errorf("secrets: %q is not a secret name (letters, digits and _)", n)
		case !have[n]:
			return nil, nil, fmt.Errorf("there is no secret %q in the vault; the person adds it under Settings › Secrets", n)
		case seen[n]:
			return nil, nil, fmt.Errorf("secrets: %q is listed twice", n)
		}
		seen[n] = true
	}
	refuse := func(msg string) error {
		_ = s.audit(ctx, p, "", "assistant.sandbox.secrets", "sandbox", "refused: "+strings.Join(names, ",")+": "+msg, false)
		return errors.New(msg)
	}
	switch {
	case !p.Role.AtLeast(authn.Admin):
		return nil, nil, refuse("giving a secret to the sandbox needs an administrator")
	}
	ask := permAskerFrom(ctx)
	if ask == nil {
		return nil, nil, refuse("giving a secret to the sandbox needs the person's approval and nobody is here to give it; ask in the chat")
	}
	sort.Strings(names)
	choice, aerr := ask(permRequest{Tool: runShellTool, Command: cutShown(command), Note: true, Why: "this gives the secrets " + strings.Join(names, ", ") + " to the command, which can use them for anything they allow"})
	if aerr != nil || choice != decisionOnce {
		msg := "you said no, so it did not run"
		if aerr != nil {
			msg = aerr.Error()
		}
		return nil, nil, refuse(msg)
	}
	all := s.stepSecrets(ctx)
	for _, n := range names {
		v := all[n]
		if v == "" || strings.ContainsAny(v, "\n\r\x00") {
			return nil, nil, refuse(fmt.Sprintf("the secret %q cannot be passed as an environment variable (empty, or it has a line break)", n))
		}
		env = append(env, n+"="+v)
		values = append(values, v)
	}
	_ = s.audit(ctx, p, "", "assistant.sandbox.secrets", "sandbox", "approved once: "+strings.Join(names, ",")+": "+truncate(command, 200), true)
	return env, values, nil
}

// --- the API ---

type sandboxContainerJSON struct {
	Name   string `json:"name"`
	Image  string `json:"image"`
	Status string `json:"status"`
}

func (s *Server) handleGetSandbox(w http.ResponseWriter, r *http.Request) {
	ctx, p := r.Context(), principalFrom(r.Context())
	st := s.loadSandbox(ctx)
	_, runtimeLabel := s.sandboxRuntime(ctx)
	_, _, owner := sandboxNames(p, "")
	docker := true
	mine := []sandboxContainerJSON{}
	out, _, err := s.run(ctx, "docker", "ps", "-a", "--filter", "label=forgeline.owner="+owner, "--format", "{{.Names}}\t{{.Image}}\t{{.Status}}")
	if err != nil {
		docker = false
	}
	for _, l := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if f := strings.Split(l, "\t"); len(f) == 3 {
			mine = append(mine, sandboxContainerJSON{f[0], f[1], f[2]})
		}
	}
	net := st.Network
	if sandboxEnv() != "" && net == sandboxNetInternet {
		net = "the Docker network " + sandboxEnv()
	}
	writeJSON(w, http.StatusOK, map[string]any{"enabled": st.Enabled, "network": st.Network, "network_note": net, "runtime": runtimeLabel, "docker": docker, "image": defaultSandboxImage,
		"containers": mine, "idle_minutes": int(sandboxIdle.Minutes()), "updated_by": st.UpdatedBy})
}

func (s *Server) handlePutSandbox(w http.ResponseWriter, r *http.Request) {
	var q struct {
		Enabled *bool  `json:"enabled"`
		Network string `json:"network"`
	}
	if !readJSON(w, r, &q) {
		return
	}
	cur := s.loadSandbox(r.Context())
	if q.Enabled != nil {
		cur.Enabled = *q.Enabled
	}
	if q.Network != "" {
		if q.Network != sandboxNetInternet && q.Network != sandboxNetNone {
			writeError(w, http.StatusBadRequest, "network: internet or none")
			return
		}
		cur.Network = q.Network
	}
	p := principalFrom(r.Context())
	cur.UpdatedBy, cur.UpdatedAt = p.Name, time.Now().UTC()
	if err := s.docs.Put(r.Context(), kindSettings, idSandbox, cur); err != nil {
		s.internalError(w, "saving the sandbox settings", err)
		return
	}
	s.auditReq(r, "assistant.sandbox.settings", "sandbox", fmt.Sprintf("enabled %v, network %s", cur.Enabled, cur.Network), true)
	writeJSON(w, http.StatusOK, cur)
}

// handleResetSandbox removes the person's own sandbox containers and their workspace.
func (s *Server) handleResetSandbox(w http.ResponseWriter, r *http.Request) {
	ctx, p := r.Context(), principalFrom(r.Context())
	_, volume, owner := sandboxNames(p, "")
	out, _, _ := s.run(ctx, "docker", "ps", "-a", "-q", "--filter", "label=forgeline.owner="+owner)
	removed := 0
	for _, id := range strings.Fields(string(out)) {
		if _, _, err := s.run(ctx, "docker", "rm", "-f", id); err == nil {
			removed++
		}
	}
	sandboxMu.Lock()
	for k := range sandboxUsed {
		if strings.HasPrefix(k, "forgeline-sbx-"+owner+"-") {
			delete(sandboxUsed, k)
		}
	}
	sandboxMu.Unlock()
	_, _, verr := s.run(ctx, "docker", "volume", "rm", "-f", volume)
	s.auditReq(r, "assistant.sandbox.reset", "sandbox", fmt.Sprintf("%d containers removed, workspace removed %v", removed, verr == nil), true)
	writeJSON(w, http.StatusOK, map[string]any{"containers_removed": removed, "workspace_removed": verr == nil})
}

// startSandboxReaper removes sandbox containers nobody has used for half an hour. Their workspace volume stays, so
// files survive; a container Forgeline did not start in this run is adopted with the clock starting now.
func (s *Server) startSandboxReaper(ctx context.Context) {
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		t := time.NewTicker(sandboxReapEvery)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				s.reapSandboxes(ctx, time.Now())
			}
		}
	}()
}

func (s *Server) reapSandboxes(ctx context.Context, now time.Time) int {
	if !s.loadSandbox(ctx).Enabled {
		return 0
	}
	out, _, err := s.run(ctx, "docker", "ps", "-a", "--filter", "label=forgeline.sandbox=1", "--format", "{{.Names}}")
	if err != nil {
		return 0
	}
	sandboxMu.Lock()
	defer sandboxMu.Unlock()
	n := 0
	for _, name := range strings.Fields(string(out)) {
		last, ok := sandboxUsed[name]
		if !ok {
			sandboxUsed[name] = now
			continue
		}
		if now.Sub(last) > sandboxIdle {
			if _, _, err := s.run(ctx, "docker", "rm", "-f", name); err == nil {
				delete(sandboxUsed, name)
				n++
			}
		}
	}
	return n
}
