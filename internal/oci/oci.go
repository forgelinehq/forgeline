// Package oci pushes an agent to a container registry (design note 046, step 3), the way Docker Agent's
// `docker agent share push` stores it, so `docker agent run <ref>` and `docker agent share pull <ref>` work on it.
// It speaks the OCI distribution API directly (check, upload two blobs, put the manifest) with token or basic
// authentication, and has no dependency beyond the standard library.
package oci

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

// The media types Docker Agent (v1.162.0) writes: an OCI image manifest whose artifact type marks it as an agent,
// a Docker image config naming one layer, and that layer, which is the agent's YAML gzipped (not a tar).
const (
	ManifestType = "application/vnd.oci.image.manifest.v1+json"
	ArtifactType = "application/vnd.docker.agent.config.v1+json"
	ConfigType   = "application/vnd.docker.container.image.v1+json"
	LayerType    = "application/vnd.docker.image.rootfs.diff.tar.gzip"
)

// Descriptor points at a blob.
type Descriptor struct {
	MediaType string `json:"mediaType"`
	Size      int64  `json:"size"`
	Digest    string `json:"digest"`
}

// Manifest is the image manifest of an agent.
type Manifest struct {
	Annotations   map[string]string `json:"annotations,omitempty"`
	ArtifactType  string            `json:"artifactType"`
	Config        Descriptor        `json:"config"`
	Layers        []Descriptor      `json:"layers"`
	MediaType     string            `json:"mediaType"`
	SchemaVersion int               `json:"schemaVersion"`
}

// Artifact is everything a push uploads, with the manifest's own digest.
type Artifact struct {
	Config, Layer, Manifest []byte
	Digest                  string // of the manifest: what the registry calls the pushed reference
}

// Digest is the sha256 digest of b, as the distribution API writes it.
func Digest(b []byte) string {
	h := sha256.Sum256(b)
	return "sha256:" + hex.EncodeToString(h[:])
}

func gz(b []byte) ([]byte, error) {
	var buf bytes.Buffer
	w, err := gzip.NewWriterLevel(&buf, gzip.BestSpeed) // no name and no time in the header: the same YAML gives the same layer
	if err != nil {
		return nil, err
	}
	if _, err := w.Write(b); err != nil {
		return nil, err
	}
	if err := w.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// AgentVersionAnnotation is the manifest annotation Docker Agent looks for before it accepts an artifact as an agent
// ("wasn't created by `docker agent share push`" otherwise). It checks only that it is there; its value names the
// tool that wrote the artifact, so Forgeline writes its own name and version, not Docker Agent's.
const AgentVersionAnnotation = "io.docker.agent.version"

// AgentArtifact builds the artifact for one agent YAML. creator names what wrote it ("forgeline v0.9.0"); created
// is when the agent version was made, so pushing the same version twice gives the same digest; annotations are added
// to the manifest's (titles, Forgeline's ids).
func AgentArtifact(yaml []byte, creator string, created time.Time, annotations map[string]string) (Artifact, error) {
	layer, err := gz(yaml)
	if err != nil {
		return Artifact{}, err
	}
	zero := "0001-01-01T00:00:00Z"
	config, err := json.Marshal(map[string]any{
		"architecture": "", "os": "", "created": zero, "history": []map[string]string{{"created": zero}},
		"rootfs": map[string]any{"type": "layers", "diff_ids": []string{Digest(yaml)}}, "config": map[string]any{},
	})
	if err != nil {
		return Artifact{}, err
	}
	ann := map[string]string{
		"org.opencontainers.image.created":     created.UTC().Format(time.RFC3339),
		"org.opencontainers.image.description": "OCI artifact containing agent.yaml",
		AgentVersionAnnotation:                 creator,
	}
	for k, v := range annotations {
		ann[k] = v
	}
	m := Manifest{Annotations: ann, ArtifactType: ArtifactType, MediaType: ManifestType, SchemaVersion: 2,
		Config: Descriptor{MediaType: ConfigType, Size: int64(len(config)), Digest: Digest(config)},
		Layers: []Descriptor{{MediaType: LayerType, Size: int64(len(layer)), Digest: Digest(layer)}}}
	manifest, err := json.Marshal(m)
	if err != nil {
		return Artifact{}, err
	}
	return Artifact{Config: config, Layer: layer, Manifest: manifest, Digest: Digest(manifest)}, nil
}

// --- references ---

var (
	hostRE = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]*[a-z0-9])?(\.[a-z0-9]([a-z0-9-]*[a-z0-9])?)*(:[0-9]{1,5})?$`)
	// A repository is path components of lower-case letters and digits, joined by single separators (the distribution
	// spec's grammar, without the rarer double underscore).
	repoRE = regexp.MustCompile(`^[a-z0-9]+([._-][a-z0-9]+)*(/[a-z0-9]+([._-][a-z0-9]+)*)*$`)
	tagRE  = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9_.-]{0,127}$`)
)

// Loopback says whether a registry host is this machine, where plain http is allowed (a local test registry).
func Loopback(host string) bool {
	h := host
	if i := strings.LastIndex(h, ":"); i >= 0 && !strings.Contains(h[i:], "]") {
		h = h[:i]
	}
	return h == "localhost" || h == "127.0.0.1"
}

// CheckHost says what is wrong with a registry host such as ghcr.io or 127.0.0.1:5000.
func CheckHost(host string) error {
	if !hostRE.MatchString(host) || len(host) > 253 {
		return fmt.Errorf("host: %q is not a registry host such as ghcr.io or registry.example.com:5000", host)
	}
	return nil
}

// CheckRepository says what is wrong with a repository path such as acme/agents/triage.
func CheckRepository(repo string) error {
	if !repoRE.MatchString(repo) || len(repo) > 255 {
		return fmt.Errorf("repository: %q must be lower-case letters, digits and . _ - separated by /", repo)
	}
	return nil
}

// CheckTag says what is wrong with a tag.
func CheckTag(tag string) error {
	if !tagRE.MatchString(tag) {
		return fmt.Errorf("tag: %q is not a tag (letters, digits, _ . -, at most 128)", tag)
	}
	return nil
}

// --- the client ---

// Registry is where to push and how to sign in. An empty Username pushes without signing in.
type Registry struct {
	Host               string
	Username, Password string
	HTTP               *http.Client // nil: a client with a 60-second timeout
}

func (r Registry) scheme() string {
	if Loopback(r.Host) {
		return "http"
	}
	return "https"
}

func (r Registry) base() string { return r.scheme() + "://" + r.Host }

func (r Registry) client() *http.Client {
	if r.HTTP != nil {
		return r.HTTP
	}
	return &http.Client{Timeout: 60 * time.Second}
}

// session keeps the token a registry gave for one repository.
type session struct {
	reg    Registry
	repo   string
	bearer string
	basic  bool
}

// Error is a registry's refusal, with its status and its own words.
type Error struct {
	Status int
	Op     string
	Detail string
}

func (e *Error) Error() string {
	msg := fmt.Sprintf("%s: the registry answered %d", e.Op, e.Status)
	switch e.Status {
	case http.StatusUnauthorized:
		msg += " (the user name or token was not accepted)"
	case http.StatusForbidden:
		msg += " (the token may not push to this repository)"
	}
	if e.Detail != "" {
		msg += ": " + e.Detail
	}
	return msg
}

func readError(op string, resp *http.Response) error {
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	var body struct {
		Errors []struct{ Code, Message string } `json:"errors"`
	}
	detail := ""
	if json.Unmarshal(b, &body) == nil && len(body.Errors) > 0 {
		detail = strings.TrimSpace(body.Errors[0].Code + " " + body.Errors[0].Message)
	}
	if len(detail) > 300 {
		detail = detail[:300]
	}
	return &Error{Status: resp.StatusCode, Op: op, Detail: detail}
}

// do sends a request, signing in when the registry asks to (once), and returns the response for the caller to close.
func (s *session) do(ctx context.Context, method, rawURL string, body []byte, header http.Header) (*http.Response, error) {
	for attempt := 0; ; attempt++ {
		var rd io.Reader
		if body != nil {
			rd = bytes.NewReader(body)
		}
		req, err := http.NewRequestWithContext(ctx, method, rawURL, rd)
		if err != nil {
			return nil, err
		}
		for k, v := range header {
			req.Header[k] = v
		}
		if body != nil {
			req.ContentLength = int64(len(body))
		}
		switch {
		case s.bearer != "":
			req.Header.Set("Authorization", "Bearer "+s.bearer)
		case s.basic:
			req.SetBasicAuth(s.reg.Username, s.reg.Password)
		}
		resp, err := s.reg.client().Do(req)
		if err != nil {
			return nil, err
		}
		if resp.StatusCode != http.StatusUnauthorized || attempt > 0 {
			return resp, nil
		}
		challenge := resp.Header.Get("WWW-Authenticate")
		io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
		resp.Body.Close()
		if err := s.signIn(ctx, challenge); err != nil {
			return nil, err
		}
	}
}

// signIn answers a WWW-Authenticate challenge: Basic sends the user name and token; Bearer exchanges them (or
// nothing, for an anonymous pull token) for a token from the realm the registry names.
func (s *session) signIn(ctx context.Context, challenge string) error {
	scheme, params := parseChallenge(challenge)
	switch strings.ToLower(scheme) {
	case "basic":
		if s.reg.Username == "" {
			return &Error{Status: http.StatusUnauthorized, Op: "sign in", Detail: "the registry wants a user name and token"}
		}
		s.basic = true
		return nil
	case "bearer":
	default:
		return &Error{Status: http.StatusUnauthorized, Op: "sign in", Detail: "the registry asked for an unknown kind of sign-in"}
	}
	realm, err := url.Parse(params["realm"])
	if err != nil || realm.Host == "" {
		return errors.New("sign in: the registry named no token address")
	}
	// The token address gets the user name and token, so it must be https, or http on this machine only.
	if realm.Scheme != "https" && !(realm.Scheme == "http" && Loopback(realm.Host)) {
		return fmt.Errorf("sign in: the registry's token address %s is not https", realm.Redacted())
	}
	q := realm.Query()
	if params["service"] != "" {
		q.Set("service", params["service"])
	}
	q.Set("scope", "repository:"+s.repo+":pull,push")
	realm.RawQuery = q.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, realm.String(), nil)
	if err != nil {
		return err
	}
	if s.reg.Username != "" {
		req.SetBasicAuth(s.reg.Username, s.reg.Password)
	}
	resp, err := s.reg.client().Do(req)
	if err != nil {
		return fmt.Errorf("sign in: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return readError("sign in", resp)
	}
	var tok struct {
		Token       string `json:"token"`
		AccessToken string `json:"access_token"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&tok); err != nil {
		return errors.New("sign in: the token answer was not JSON")
	}
	s.bearer = tok.Token
	if s.bearer == "" {
		s.bearer = tok.AccessToken
	}
	if s.bearer == "" {
		return errors.New("sign in: the registry gave no token")
	}
	return nil
}

// parseChallenge reads `Bearer realm="...",service="...",scope="..."`.
func parseChallenge(h string) (string, map[string]string) {
	scheme, rest, _ := strings.Cut(strings.TrimSpace(h), " ")
	params := map[string]string{}
	for rest = strings.TrimSpace(rest); rest != ""; {
		k, v, ok := strings.Cut(rest, "=")
		if !ok {
			break
		}
		k = strings.ToLower(strings.TrimSpace(k))
		v = strings.TrimSpace(v)
		if strings.HasPrefix(v, `"`) {
			end := strings.Index(v[1:], `"`)
			if end < 0 {
				params[k] = v[1:]
				break
			}
			params[k], rest = v[1:end+1], strings.TrimPrefix(strings.TrimSpace(v[end+2:]), ",")
		} else {
			val, after, _ := strings.Cut(v, ",")
			params[k], rest = val, after
		}
		rest = strings.TrimSpace(rest)
	}
	return scheme, params
}

// Check signs in for a repository and reads its tag list, so a wrong host, user name or token shows before anyone
// approves a push. A repository that does not exist yet is fine (registries create it on the first push).
func Check(ctx context.Context, reg Registry, repo string) error {
	s := &session{reg: reg, repo: repo}
	resp, err := s.do(ctx, http.MethodGet, reg.base()+"/v2/"+repo+"/tags/list", nil, nil)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusNotFound {
		return nil
	}
	return readError("check", resp)
}

// Push uploads an artifact to repo and tags it. It returns the manifest digest the registry confirmed.
func Push(ctx context.Context, reg Registry, repo, tag string, a Artifact) (string, error) {
	if err := CheckRepository(repo); err != nil {
		return "", err
	}
	if err := CheckTag(tag); err != nil {
		return "", err
	}
	s := &session{reg: reg, repo: repo}
	for _, blob := range [][]byte{a.Layer, a.Config} {
		if err := s.putBlob(ctx, blob); err != nil {
			return "", err
		}
	}
	resp, err := s.do(ctx, http.MethodPut, reg.base()+"/v2/"+repo+"/manifests/"+tag, a.Manifest, http.Header{"Content-Type": {ManifestType}})
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK {
		return "", readError("put manifest", resp)
	}
	if got := resp.Header.Get("Docker-Content-Digest"); got != "" && got != a.Digest {
		return "", fmt.Errorf("put manifest: the registry stored %s, not %s", got, a.Digest)
	}
	return a.Digest, nil
}

// putBlob uploads one blob in a single request unless the registry already has it.
func (s *session) putBlob(ctx context.Context, blob []byte) error {
	d := Digest(blob)
	base := s.reg.base() + "/v2/" + s.repo
	resp, err := s.do(ctx, http.MethodHead, base+"/blobs/"+d, nil, nil)
	if err != nil {
		return err
	}
	resp.Body.Close()
	if resp.StatusCode == http.StatusOK {
		return nil
	}
	resp, err = s.do(ctx, http.MethodPost, base+"/blobs/uploads/", nil, nil)
	if err != nil {
		return err
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusAccepted {
		return readError("start upload", resp)
	}
	loc, err := url.Parse(resp.Header.Get("Location"))
	if err != nil || resp.Header.Get("Location") == "" {
		return errors.New("start upload: the registry gave no upload address")
	}
	up := resp.Request.URL.ResolveReference(loc)
	if up.Host != resp.Request.URL.Host {
		return fmt.Errorf("start upload: the registry sent the upload to another host (%s)", up.Host)
	}
	q := up.Query()
	q.Set("digest", d)
	up.RawQuery = q.Encode()
	resp, err = s.do(ctx, http.MethodPut, up.String(), blob, http.Header{"Content-Type": {"application/octet-stream"}})
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		return readError("upload", resp)
	}
	return nil
}
