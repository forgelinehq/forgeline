package server

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// The agent engine in the sandbox (design note 053, slice 2). Claude Code runs inside the person's sandbox container, so the
// commands it runs and the files it writes are in the container's /work, not on the server. The container cannot reach the
// Forgeline server over the network, so Forgeline's tools come through a unix socket the server mounts into it, with the
// server's own binary (a copy, mounted read-only) as the stdio MCP bridge in front of it.

const (
	// engineImage is built on first use from the Dockerfile below. Bump the number when the Dockerfile changes.
	engineImage     = "forgeline/agent:1"
	engineClaudeVer = "2.1.330"
	engineNodeImage = "node:24-slim"
)

const engineDockerfile = `FROM ` + engineNodeImage + `
RUN apt-get update && apt-get install -y --no-install-recommends git curl ca-certificates python3 python3-pip jq unzip less ripgrep \
 && rm -rf /var/lib/apt/lists/* \
 && npm install -g @anthropic-ai/claude-code@` + engineClaudeVer + `
WORKDIR /work
`

type engineSocketKey struct{}

var (
	engineSockMu sync.Mutex
	engineSocks  = map[string]*http.Server{} // socket path -> server
	engineBuild  sync.Mutex
)

// engineBridgeDir is where one person's socket and the per-run files for the container live.
func engineBridgeDir(dataDir string, p Principal) string {
	work, _ := engineWorkspace(dataDir, p)
	return filepath.Join(filepath.Dir(work), "bridge")
}

// engineSocket makes sure the person's unix socket is being served, and returns its host directory. Only the engine's own
// endpoint is served on it, and the container it is mounted into belongs to the same person.
func (s *Server) engineSocket(dir string) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	sock := filepath.Join(dir, "engine.sock")
	engineSockMu.Lock()
	defer engineSockMu.Unlock()
	if _, ok := engineSocks[sock]; ok {
		return nil
	}
	_ = os.Remove(sock)
	ln, err := net.Listen("unix", sock)
	if err != nil {
		return err
	}
	_ = os.Chmod(sock, 0o666) // the container's user connects to it; the path carries a token for one message
	mux := http.NewServeMux()
	mux.HandleFunc("POST /mcp/engine/{token}", s.handleEngineMCP)
	srv := &http.Server{Handler: mux, ReadHeaderTimeout: 10 * time.Second,
		ConnContext: func(ctx context.Context, _ net.Conn) context.Context {
			return context.WithValue(ctx, engineSocketKey{}, true)
		}}
	engineSocks[sock] = srv
	go srv.Serve(ln)
	return nil
}

// engineBinary keeps a copy of the running server's binary where a container can mount it: the updater replaces the binary
// by name, and a container outlives a version.
func (s *Server) engineBinary() (dir string, err error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	dir = filepath.Join(s.cfg.DataDir, "engine", "bin")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	src, err := os.Open(exe)
	if err != nil {
		return "", err
	}
	defer src.Close()
	h := sha256.New()
	if _, err := io.Copy(h, src); err != nil {
		return "", err
	}
	sum := hex.EncodeToString(h.Sum(nil))[:16]
	stamp := filepath.Join(dir, "forgeline.sha")
	if b, _ := os.ReadFile(stamp); string(b) == sum {
		return dir, nil
	}
	if _, err := src.Seek(0, 0); err != nil {
		return "", err
	}
	tmp := filepath.Join(dir, "forgeline.new")
	out, err := os.OpenFile(tmp, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
	if err != nil {
		return "", err
	}
	if _, err := io.Copy(out, src); err != nil {
		out.Close()
		return "", err
	}
	out.Close()
	if err := os.Rename(tmp, filepath.Join(dir, "forgeline")); err != nil {
		return "", err
	}
	return dir, os.WriteFile(stamp, []byte(sum), 0o644)
}

// ensureEngineImage builds the agent image once. The first message after turning the engine on waits for it.
func (s *Server) ensureEngineImage(ctx context.Context) error {
	engineBuild.Lock()
	defer engineBuild.Unlock()
	if _, _, err := s.run(ctx, "docker", "image", "inspect", engineImage); err == nil {
		return nil
	}
	dir, err := os.MkdirTemp("", "forgeline-agent-image-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)
	if err := os.WriteFile(filepath.Join(dir, "Dockerfile"), []byte(engineDockerfile), 0o644); err != nil {
		return err
	}
	lctx := context.WithValue(ctx, cmdLimitKey{}, 20*time.Minute)
	if _, e, err := s.run(lctx, "docker", "build", "-t", engineImage, dir); err != nil {
		return fmt.Errorf("building the agent image: %s", dockerError(e, err))
	}
	return nil
}

// engineInContainer says whether the engine should run in the sandbox: when the sandbox is on, it must.
func (s *Server) engineInContainer(ctx context.Context) bool { return s.loadSandbox(ctx).Enabled }

// containerEngine returns the command that runs claude in the person's sandbox, and the host directory holding the files it
// reads (the system prompt, the socket).
func (s *Server) containerEngine(ctx context.Context, p Principal, token string, args []string, runFiles map[string]string) (*exec.Cmd, string, error) {
	st := s.loadSandbox(ctx)
	if st.Network == sandboxNetNone {
		return nil, "", errors.New("the agent must reach the model, so the sandbox network cannot be set to none while Claude Code runs as the agent")
	}
	if token == "" {
		return nil, "", errors.New("Claude Code in the sandbox needs a Claude Code token (Settings › Assistant model), because the server user's login stays on the server")
	}
	if err := s.ensureEngineImage(ctx); err != nil {
		return nil, "", err
	}
	bridge := engineBridgeDir(s.cfg.DataDir, p)
	if err := s.engineSocket(bridge); err != nil {
		return nil, "", fmt.Errorf("opening the agent's socket: %v", err)
	}
	bin, err := s.engineBinary()
	if err != nil {
		return nil, "", fmt.Errorf("preparing the bridge: %v", err)
	}
	for name, content := range runFiles {
		if err := os.WriteFile(filepath.Join(bridge, name), []byte(content), 0o644); err != nil {
			return nil, "", err
		}
	}
	ctr, err := s.ensureSandboxWith(ctx, p, engineImage, st, "-v", bridge+":/run/forgeline:ro", "-v", bin+":/opt/forgeline:ro")
	if err != nil {
		return nil, "", err
	}
	full := []string{"exec", "-i", "-w", "/work", "-e", "HOME=/work/.agent", "-e", engineEnv, "-e", claudeTokenName, ctr, "claude"}
	full = append(full, args...)
	cmd := s.engineCmd(ctx, "docker", full...)
	cmd.Env = []string{"PATH=" + os.Getenv("PATH"), "HOME=" + os.Getenv("HOME"), claudeTokenName + "=" + token}
	return cmd, bridge, nil
}

func (s *Server) engineCmd(ctx context.Context, name string, args ...string) *exec.Cmd {
	if s.engineExec != nil {
		return s.engineExec(ctx, name, args...)
	}
	return exec.CommandContext(ctx, name, args...)
}

// engineMCPConfig is the MCP server entry for the container: the bridge, pointed at the socket, with this message's token.
func engineMCPConfig(tok string) string {
	b, _ := json.Marshal(map[string]any{"mcpServers": map[string]any{"forgeline": map[string]any{
		"command": "/opt/forgeline/forgeline", "args": []string{"mcp"},
		"env": map[string]string{"FORGELINE_MCP_SOCKET": "/run/forgeline/engine.sock", "FORGELINE_MCP_PATH": "/mcp/engine/" + tok}}}})
	return string(b)
}

var _ = strings.TrimSpace
