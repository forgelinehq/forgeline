package oci

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeRegistry is enough of the distribution API for a push: token sign-in, blobs, uploads and manifests.
type fakeRegistry struct {
	mu        sync.Mutex
	blobs     map[string][]byte
	manifests map[string][]byte // repo:tag
	user, pw  string
	basicOnly bool
	tokenReqs int
	scopes    []string
	srv       *httptest.Server
}

func newFake(t *testing.T) *fakeRegistry {
	f := &fakeRegistry{blobs: map[string][]byte{}, manifests: map[string][]byte{}, user: "robot", pw: "s3cret"}
	f.srv = httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakeRegistry) host() string { return strings.TrimPrefix(f.srv.URL, "http://") }

func (f *fakeRegistry) serve(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if r.URL.Path == "/token" {
		f.tokenReqs++
		f.scopes = append(f.scopes, r.URL.Query().Get("scope"))
		u, p, ok := r.BasicAuth()
		if !ok || u != f.user || p != f.pw {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		json.NewEncoder(w).Encode(map[string]string{"token": "tok-123"})
		return
	}
	authed := r.Header.Get("Authorization") == "Bearer tok-123"
	if f.basicOnly {
		u, p, ok := r.BasicAuth()
		authed = ok && u == f.user && p == f.pw
	}
	if !authed {
		if f.basicOnly {
			w.Header().Set("WWW-Authenticate", `Basic realm="fake"`)
		} else {
			w.Header().Set("WWW-Authenticate", `Bearer realm="`+f.srv.URL+`/token",service="fake"`)
		}
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	p := strings.TrimPrefix(r.URL.Path, "/v2/")
	switch {
	case strings.Contains(p, "/blobs/uploads/"):
		if r.Method == http.MethodPost {
			w.Header().Set("Location", "/v2/"+strings.SplitN(p, "/blobs/", 2)[0]+"/blobs/uploads/u1?state=x")
			w.WriteHeader(http.StatusAccepted)
			return
		}
		body, _ := io.ReadAll(r.Body)
		d := r.URL.Query().Get("digest")
		if Digest(body) != d || r.URL.Query().Get("state") != "x" {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		f.blobs[d] = body
		w.WriteHeader(http.StatusCreated)
	case strings.Contains(p, "/blobs/"):
		d := p[strings.LastIndex(p, "/")+1:]
		if _, ok := f.blobs[d]; ok {
			w.WriteHeader(http.StatusOK)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	case strings.Contains(p, "/manifests/"):
		repo, tag, _ := strings.Cut(p, "/manifests/")
		body, _ := io.ReadAll(r.Body)
		var m Manifest
		if json.Unmarshal(body, &m) != nil || r.Header.Get("Content-Type") != ManifestType {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		for _, d := range append([]Descriptor{m.Config}, m.Layers...) {
			if _, ok := f.blobs[d.Digest]; !ok {
				w.WriteHeader(http.StatusBadRequest)
				json.NewEncoder(w).Encode(map[string]any{"errors": []map[string]string{{"code": "BLOB_UNKNOWN", "message": "blob unknown"}}})
				return
			}
		}
		f.manifests[repo+":"+tag] = body
		w.Header().Set("Docker-Content-Digest", Digest(body))
		w.WriteHeader(http.StatusCreated)
	case strings.HasSuffix(p, "/tags/list"):
		w.WriteHeader(http.StatusNotFound)
	default:
		w.WriteHeader(http.StatusNotFound)
	}
}

const sampleYAML = "version: \"16\"\nagents:\n  root:\n    model: anthropic/claude-sonnet-5-5\n    instruction: Explain failed runs.\n"

func TestAgentArtifactMatchesDockerAgent(t *testing.T) {
	at := time.Date(2026, 11, 20, 7, 0, 0, 0, time.UTC)
	a, err := AgentArtifact([]byte(sampleYAML), "forgeline test", at, map[string]string{"org.opencontainers.image.title": "Triage"})
	if err != nil {
		t.Fatal(err)
	}
	var m Manifest
	if err := json.Unmarshal(a.Manifest, &m); err != nil {
		t.Fatal(err)
	}
	if m.MediaType != ManifestType || m.ArtifactType != ArtifactType || m.SchemaVersion != 2 || m.Config.MediaType != ConfigType || len(m.Layers) != 1 || m.Layers[0].MediaType != LayerType {
		t.Fatalf("manifest: %s", a.Manifest)
	}
	if m.Annotations["org.opencontainers.image.created"] != "2026-11-20T07:00:00Z" || m.Annotations[AgentVersionAnnotation] != "forgeline test" || m.Annotations["org.opencontainers.image.title"] != "Triage" {
		t.Errorf("annotations: %v", m.Annotations)
	}
	if m.Config.Digest != Digest(a.Config) || m.Layers[0].Digest != Digest(a.Layer) || a.Digest != Digest(a.Manifest) {
		t.Error("digests do not match the blobs")
	}
	zr, err := gzip.NewReader(bytes.NewReader(a.Layer))
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := io.ReadAll(zr); string(got) != sampleYAML {
		t.Errorf("layer is not the gzipped YAML: %q", got)
	}
	var cfg struct {
		RootFS struct {
			DiffIDs []string `json:"diff_ids"`
		} `json:"rootfs"`
	}
	json.Unmarshal(a.Config, &cfg)
	if len(cfg.RootFS.DiffIDs) != 1 || cfg.RootFS.DiffIDs[0] != Digest([]byte(sampleYAML)) {
		t.Errorf("config diff_ids: %s", a.Config)
	}
	b, _ := AgentArtifact([]byte(sampleYAML), "forgeline test", at, map[string]string{"org.opencontainers.image.title": "Triage"})
	if b.Digest != a.Digest {
		t.Error("the same version must give the same digest")
	}
}

func TestPushWithTokenSignIn(t *testing.T) {
	f := newFake(t)
	a, _ := AgentArtifact([]byte(sampleYAML), "forgeline test", time.Now(), nil)
	reg := Registry{Host: f.host(), Username: "robot", Password: "s3cret"}
	d, err := Push(context.Background(), reg, "acme/triage", "v3", a)
	if err != nil {
		t.Fatal(err)
	}
	if d != a.Digest || string(f.manifests["acme/triage:v3"]) != string(a.Manifest) {
		t.Fatalf("pushed %s, stored %v", d, f.manifests)
	}
	if f.scopes[0] != "repository:acme/triage:pull,push" {
		t.Errorf("scope: %v", f.scopes)
	}
	// A second push of the same blobs skips the uploads.
	if _, err := Push(context.Background(), reg, "acme/triage", "v3", a); err != nil {
		t.Fatal(err)
	}
}

func TestPushWithBasicSignIn(t *testing.T) {
	f := newFake(t)
	f.basicOnly = true
	a, _ := AgentArtifact([]byte(sampleYAML), "forgeline test", time.Now(), nil)
	if _, err := Push(context.Background(), Registry{Host: f.host(), Username: "robot", Password: "s3cret"}, "acme/triage", "v1", a); err != nil {
		t.Fatal(err)
	}
	if f.tokenReqs != 0 {
		t.Error("basic sign-in must not ask for a token")
	}
}

func TestPushRefusals(t *testing.T) {
	f := newFake(t)
	a, _ := AgentArtifact([]byte(sampleYAML), "forgeline test", time.Now(), nil)
	ctx := context.Background()
	_, err := Push(ctx, Registry{Host: f.host(), Username: "robot", Password: "wrong"}, "acme/triage", "v1", a)
	if e, ok := err.(*Error); !ok || e.Status != http.StatusUnauthorized || !strings.Contains(err.Error(), "not accepted") {
		t.Errorf("wrong token: %v", err)
	}
	if len(f.manifests) != 0 {
		t.Error("nothing may be stored with a wrong token")
	}
	if _, err := Push(ctx, Registry{Host: f.host()}, "Acme/Bad Repo", "v1", a); err == nil {
		t.Error("a bad repository must be refused before any request")
	}
	if _, err := Push(ctx, Registry{Host: f.host()}, "acme/x", "v1/../x", a); err == nil {
		t.Error("a bad tag must be refused")
	}
	if err := Check(ctx, Registry{Host: f.host(), Username: "robot", Password: "s3cret"}, "acme/triage"); err != nil {
		t.Errorf("check: %v", err)
	}
	if err := Check(ctx, Registry{Host: f.host(), Username: "robot", Password: "nope"}, "acme/triage"); err == nil {
		t.Error("check with a wrong token must fail")
	}
}

// A token address that is not https would receive the user name and token in clear: refused.
func TestTokenRealmMustBeHTTPS(t *testing.T) {
	var asked bool
	evil := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { asked = true }))
	defer evil.Close()
	reg := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("WWW-Authenticate", `Bearer realm="http://tokens.example.com/token",service="x"`)
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer reg.Close()
	err := Check(context.Background(), Registry{Host: strings.TrimPrefix(reg.URL, "http://"), Username: "u", Password: "p"}, "a/b")
	if err == nil || !strings.Contains(err.Error(), "not https") || asked {
		t.Errorf("got %v", err)
	}
}

// An upload address on another host would get the token: refused.
func TestUploadMustStayOnTheRegistry(t *testing.T) {
	reg := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			w.Header().Set("Location", "https://elsewhere.example.com/upload")
			w.WriteHeader(http.StatusAccepted)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer reg.Close()
	a, _ := AgentArtifact([]byte(sampleYAML), "forgeline test", time.Now(), nil)
	_, err := Push(context.Background(), Registry{Host: strings.TrimPrefix(reg.URL, "http://")}, "a/b", "v1", a)
	if err == nil || !strings.Contains(err.Error(), "another host") {
		t.Errorf("got %v", err)
	}
}

func TestChecks(t *testing.T) {
	for _, h := range []string{"ghcr.io", "registry-1.docker.io", "127.0.0.1:5000", "localhost:5055", "123456789012.dkr.ecr.eu-west-1.amazonaws.com"} {
		if err := CheckHost(h); err != nil {
			t.Errorf("%s: %v", h, err)
		}
	}
	for _, h := range []string{"", "https://ghcr.io", "ghcr.io/x", "user@ghcr.io", "GHCR.IO", "a..b"} {
		if CheckHost(h) == nil {
			t.Errorf("%q should be refused", h)
		}
	}
	for _, r := range []string{"acme/triage", "a", "acme/agents/triage-bot", "a.b_c-d/e"} {
		if CheckRepository(r) != nil {
			t.Errorf("%s refused", r)
		}
	}
	for _, r := range []string{"", "Acme", "a//b", "/a", "a/", "a--", "a b"} {
		if CheckRepository(r) == nil {
			t.Errorf("%q should be refused", r)
		}
	}
	if !Loopback("127.0.0.1:5000") || !Loopback("localhost") || Loopback("ghcr.io") || Loopback("127.0.0.1.evil.com") {
		t.Error("loopback")
	}
	s, p := parseChallenge(`Bearer realm="https://auth.docker.io/token",service="registry.docker.io",scope="repository:a/b:pull"`)
	if s != "Bearer" || p["realm"] != "https://auth.docker.io/token" || p["service"] != "registry.docker.io" || p["scope"] != "repository:a/b:pull" {
		t.Errorf("challenge: %s %v", s, p)
	}
}

// TestPushLoadsInDockerAgent pushes to a real registry and runs the reference with the real Docker Agent CLI. It runs
// only when FORGELINE_DOCKER_AGENT names the binary and FORGELINE_TEST_REGISTRY a local registry (127.0.0.1:5055),
// for example `docker run -d -p 127.0.0.1:5055:5000 registry:3`.
func TestPushLoadsInDockerAgent(t *testing.T) {
	bin, host := os.Getenv("FORGELINE_DOCKER_AGENT"), os.Getenv("FORGELINE_TEST_REGISTRY")
	if bin == "" || host == "" {
		t.Skip("set FORGELINE_DOCKER_AGENT and FORGELINE_TEST_REGISTRY to run")
	}
	a, err := AgentArtifact([]byte(sampleYAML), "forgeline test", time.Now(), map[string]string{"org.opencontainers.image.title": "Triage"})
	if err != nil {
		t.Fatal(err)
	}
	tag := "v" + time.Now().Format("150405")
	if _, err := Push(context.Background(), Registry{Host: host}, "forgeline-test/triage", tag, a); err != nil {
		t.Fatal(err)
	}
	ref := host + "/forgeline-test/triage:" + tag
	dir := t.TempDir()
	env := append(os.Environ(), "TELEMETRY_ENABLED=false", "ANTHROPIC_API_KEY=unused", "HOME="+dir)
	run := exec.Command(bin, "run", "--dry-run", ref)
	run.Dir, run.Env = dir, env
	if out, err := run.CombinedOutput(); err != nil {
		t.Fatalf("docker agent run --dry-run %s: %v\n%s", ref, err, out)
	}
	pull := exec.Command(bin, "share", "pull", ref)
	pull.Dir, pull.Env = dir, env
	if out, err := pull.CombinedOutput(); err != nil {
		t.Fatalf("docker agent share pull: %v\n%s", err, out)
	}
	files, _ := filepath.Glob(filepath.Join(dir, "*.yaml"))
	found := false
	for _, f := range files {
		if b, _ := os.ReadFile(f); string(b) == sampleYAML {
			found = true
		}
	}
	if !found {
		t.Errorf("share pull did not write the pushed YAML into %s (files: %v)", dir, files)
	}
}
