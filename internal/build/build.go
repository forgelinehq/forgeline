// Package build runs a pipeline's build step (design note 043): BuildKit through the host's Docker
// (docker buildx build) on a directory of the workspace. The Dockerfile's RUN lines get no secrets, no host mounts
// and no entitlements; the push credential, when there is one, lives in a private Docker client configuration that
// only the docker client reads and that is removed when the build ends.
package build

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/forgelinehq/forgeline/internal/pipeline"
)

// Request is one build.
type Request struct {
	// Docker is the docker CLI. Empty means "docker".
	Docker string
	// Workspace is the host directory the context and Dockerfile must stay inside.
	Workspace string
	Build     pipeline.Build
	// Refs are the full references to push (repository:tag). Empty means build only.
	Refs []string
	// Auth, when pushing, is the registry host and its base64 "user:password" for the private client configuration.
	AuthHost, Auth string
	// Env is added to the docker client's environment (for example DOCKER_HOST). The server's own environment is not
	// passed on, so its registry logins and cloud credentials stay out of the build.
	Env []string
}

// Result is what a build produced.
type Result struct {
	Digest string // the pushed image's digest, when pushing
}

var digestRE = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)

// Paths resolves the context and Dockerfile and checks that both stay inside the workspace after following links,
// so a repository cannot point the build at the server's own files.
func Paths(workspace string, b pipeline.Build) (contextDir, dockerfile string, err error) {
	root, err := filepath.EvalSymlinks(workspace)
	if err != nil {
		return "", "", fmt.Errorf("the run has no workspace to build from: %w", err)
	}
	inside := func(p string) bool {
		rel, err := filepath.Rel(root, p)
		return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
	}
	ctxRel := b.Context
	if ctxRel == "" {
		ctxRel = "."
	}
	if !filepath.IsLocal(ctxRel) {
		return "", "", fmt.Errorf("build.context %q is not inside the workspace", b.Context)
	}
	contextDir, err = filepath.EvalSymlinks(filepath.Join(root, ctxRel))
	if err != nil {
		return "", "", fmt.Errorf("build.context %q does not exist in the workspace", ctxRel)
	}
	if !inside(contextDir) {
		return "", "", fmt.Errorf("build.context %q leads outside the workspace", ctxRel)
	}
	if st, err := os.Stat(contextDir); err != nil || !st.IsDir() {
		return "", "", fmt.Errorf("build.context %q is not a directory", ctxRel)
	}
	dfRel := b.Dockerfile
	if dfRel == "" {
		dfRel = "Dockerfile"
	}
	if !filepath.IsLocal(dfRel) {
		return "", "", fmt.Errorf("build.dockerfile %q is not inside the context", b.Dockerfile)
	}
	dockerfile, err = filepath.EvalSymlinks(filepath.Join(contextDir, dfRel))
	if err != nil {
		return "", "", fmt.Errorf("there is no %s in %s", dfRel, ctxRel)
	}
	if !inside(dockerfile) {
		return "", "", fmt.Errorf("build.dockerfile %q leads outside the workspace", dfRel)
	}
	if st, err := os.Stat(dockerfile); err != nil || !st.Mode().IsRegular() {
		return "", "", fmt.Errorf("build.dockerfile %q is not a file", dfRel)
	}
	return contextDir, dockerfile, nil
}

// Args is the docker command line for a build. It never carries --allow (no entitlements), a mount, a secret or the
// push credential.
func Args(r Request, contextDir, dockerfile, metadataFile string) []string {
	b := r.Build
	a := []string{"buildx", "build", "--progress", "plain", "--file", dockerfile, "--metadata-file", metadataFile, "--provenance", "false"}
	if b.Platform != "" {
		a = append(a, "--platform", b.Platform)
	}
	if b.Network == pipeline.NetworkNone {
		a = append(a, "--network", "none")
	}
	names := make([]string, 0, len(b.Args))
	for k := range b.Args {
		names = append(names, k)
	}
	sort.Strings(names)
	for _, k := range names {
		a = append(a, "--build-arg", k+"="+b.Args[k])
	}
	if len(r.Refs) > 0 {
		for _, ref := range r.Refs {
			a = append(a, "--tag", ref)
		}
		// A pushed image is built from scratch: layers cached by another build (a pull request's, say) are not reused.
		a = append(a, "--push", "--no-cache")
	} else {
		a = append(a, "--output", "type=cacheonly")
	}
	return append(a, contextDir)
}

// Run builds, and pushes when r.Refs is set.
func Run(ctx context.Context, r Request, logs io.Writer) (Result, error) {
	if len(r.Refs) > 0 && (r.AuthHost == "" || r.Auth == "") {
		return Result{}, errors.New("a push needs a registry credential")
	}
	contextDir, dockerfile, err := Paths(r.Workspace, r.Build)
	if err != nil {
		return Result{}, err
	}
	cfg, err := os.MkdirTemp("", "forgeline-build-")
	if err != nil {
		return Result{}, fmt.Errorf("could not prepare the build: %w", err)
	}
	defer os.RemoveAll(cfg) // the credential goes with it
	if err := writeClientConfig(cfg, r.AuthHost, r.Auth); err != nil {
		return Result{}, err
	}
	meta := filepath.Join(cfg, "metadata.json")
	docker := r.Docker
	if docker == "" {
		docker = "docker"
	}
	cmd := exec.CommandContext(ctx, docker, Args(r, contextDir, dockerfile, meta)...)
	cmd.Env = append(clientEnv(cfg), r.Env...)
	cmd.Stdout, cmd.Stderr = logs, logs
	cmd.WaitDelay = 0
	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return Result{}, ctx.Err()
		}
		return Result{}, fmt.Errorf("the build failed: %w", err)
	}
	var res Result
	if len(r.Refs) > 0 {
		raw, err := os.ReadFile(meta)
		var m map[string]any
		if err == nil {
			err = json.Unmarshal(raw, &m)
		}
		d, _ := m["containerimage.digest"].(string)
		if err != nil || !digestRE.MatchString(d) {
			return Result{}, errors.New("the image was pushed but its digest could not be read")
		}
		res.Digest = d
	}
	return res, nil
}

// writeClientConfig writes a Docker client configuration readable only by this process's user: the one registry
// login a push needs, or none. A user's own CLI plugins (such as a buildx in ~/.docker/cli-plugins) stay reachable.
func writeClientConfig(dir, host, auth string) error {
	conf := map[string]any{"auths": map[string]any{}}
	if host != "" {
		conf["auths"] = map[string]any{host: map[string]string{"auth": auth}}
	}
	raw, _ := json.Marshal(conf)
	if err := os.WriteFile(filepath.Join(dir, "config.json"), raw, 0o600); err != nil {
		return fmt.Errorf("could not prepare the build: %w", err)
	}
	if home, err := os.UserHomeDir(); err == nil {
		plugins := filepath.Join(home, ".docker", "cli-plugins")
		if st, err := os.Stat(plugins); err == nil && st.IsDir() {
			_ = os.Symlink(plugins, filepath.Join(dir, "cli-plugins"))
		}
	}
	return nil
}

// clientEnv is the docker client's whole environment: enough to find programs and the daemon, and the private
// configuration. Nothing else of the server's environment is passed on.
func clientEnv(cfg string) []string {
	env := []string{"DOCKER_CONFIG=" + cfg, "PATH=" + os.Getenv("PATH"), "HOME=" + cfg}
	for _, k := range []string{"DOCKER_HOST", "DOCKER_CONTEXT", "DOCKER_TLS_VERIFY", "DOCKER_CERT_PATH", "XDG_RUNTIME_DIR"} {
		if v, ok := os.LookupEnv(k); ok {
			env = append(env, k+"="+v)
		}
	}
	return env
}
