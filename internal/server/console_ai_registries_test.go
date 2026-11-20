package server

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/forgelinehq/forgeline/internal/authn"
	"github.com/forgelinehq/forgeline/internal/oci"
)

// basicRegistry is a registry that wants a user name and token, and keeps what was pushed.
type basicRegistry struct {
	mu        sync.Mutex
	blobs     map[string][]byte
	manifests map[string][]byte
	gate      chan struct{} // when set, a manifest waits for it, so a test can act while a push is under way
	reached   chan struct{}
	srv       *httptest.Server
}

func newBasicRegistry(t *testing.T) *basicRegistry {
	g := &basicRegistry{blobs: map[string][]byte{}, manifests: map[string][]byte{}}
	g.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if g.gate != nil && r.Method == http.MethodPut && strings.Contains(r.URL.Path, "/manifests/") {
			select {
			case g.reached <- struct{}{}:
			default:
			}
			select { // a bounded wait, so a broken check fails the test instead of hanging it
			case <-g.gate:
			case <-time.After(3 * time.Second):
			}
		}
		g.mu.Lock()
		defer g.mu.Unlock()
		if u, p, ok := r.BasicAuth(); !ok || u != "robot" || p != "tok-ok" {
			w.Header().Set("WWW-Authenticate", `Basic realm="test"`)
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		path := strings.TrimPrefix(r.URL.Path, "/v2/")
		switch {
		case strings.HasSuffix(path, "/tags/list"):
			w.WriteHeader(http.StatusNotFound)
		case r.Method == http.MethodPost && strings.HasSuffix(path, "/blobs/uploads/"):
			w.Header().Set("Location", r.URL.Path+"u")
			w.WriteHeader(http.StatusAccepted)
		case r.Method == http.MethodPut && strings.Contains(path, "/blobs/uploads/"):
			b, _ := io.ReadAll(r.Body)
			g.blobs[r.URL.Query().Get("digest")] = b
			w.WriteHeader(http.StatusCreated)
		case strings.Contains(path, "/blobs/"):
			w.WriteHeader(http.StatusNotFound)
		case r.Method == http.MethodPut && strings.Contains(path, "/manifests/"):
			b, _ := io.ReadAll(r.Body)
			repo, tag, _ := strings.Cut(path, "/manifests/")
			g.manifests[repo+":"+tag] = b
			w.Header().Set("Docker-Content-Digest", oci.Digest(b))
			w.WriteHeader(http.StatusCreated)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(g.srv.Close)
	return g
}

func (g *basicRegistry) host() string { return strings.TrimPrefix(g.srv.URL, "http://") }

// pushedYAML is the agent YAML in a pushed manifest's layer.
func (g *basicRegistry) pushedYAML(t *testing.T, key string) (oci.Manifest, string) {
	t.Helper()
	g.mu.Lock()
	defer g.mu.Unlock()
	var m oci.Manifest
	if err := json.Unmarshal(g.manifests[key], &m); err != nil {
		t.Fatalf("no manifest %s: %v", key, err)
	}
	zr, err := gzip.NewReader(bytes.NewReader(g.blobs[m.Layers[0].Digest]))
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(zr)
	return m, string(b)
}

func addRegistry(t *testing.T, c *browser, host, token string) AgentRegistry {
	t.Helper()
	var g AgentRegistry
	json.Unmarshal(c.expect(201, "POST", "/v1/ai/registries", map[string]any{"name": "Team registry", "host": host, "namespace": "acme/agents", "username": "robot", "token": token}), &g)
	return g
}

// Pushing an agent (note 046, step 3): an administrator proposes, a different one approves exactly that text, and the
// registry receives Docker Agent's artifact with the YAML that was shown.
func TestPushAnAgentThroughApproval(t *testing.T) {
	m := newMCPHarness(t)
	reg := newBasicRegistry(t)
	g := addRegistry(t, m.admin, reg.host(), "tok-ok")
	if !g.HasToken || g.Namespace != "acme/agents" {
		t.Fatalf("registry: %+v", g)
	}
	// The token is never shown again, to anyone.
	if out := string(m.admin.expect(200, "GET", "/v1/ai/registries", nil)); strings.Contains(out, "tok-ok") {
		t.Errorf("the token is listed: %s", out)
	}
	m.dev.expect(403, "GET", "/v1/ai/registries", nil)
	m.admin.expect(200, "POST", "/v1/ai/registries/"+g.ID+"/test", nil)

	a := newAgent(t, m.admin, agentBody("Triage", nil, []string{"list_runs"}, "T0"))
	reason := map[string]any{"registry": g.ID, "repository": "triage", "reason": "share the triage agent with the platform team"}
	m.dev.expect(403, "POST", "/v1/ai/agents/"+a.ID+"/push", reason)
	var p AgentPush
	raw := m.admin.expect(201, "POST", "/v1/ai/agents/"+a.ID+"/push", reason)
	json.Unmarshal(raw, &p)
	if !strings.Contains(string(raw), `"left_out":[{"tool":"list_runs","why":`) {
		t.Errorf("left_out as the console reads it: %s", raw)
	}
	wantRef := reg.host() + "/acme/agents/triage:v1"
	if p.Ref != wantRef || p.Status != apProposed || !strings.Contains(p.YAML, "agents:") || len(p.LeftOut) != 1 || p.LeftOut[0].Tool != "list_runs" {
		t.Fatalf("proposal: %+v", p)
	}
	if len(reg.manifests) != 0 {
		t.Fatal("a proposal pushed something")
	}

	// The proposer cannot approve; a wrong hash is refused; a developer cannot approve.
	m.admin.expect(403, "POST", "/v1/ai/pushes/"+p.ID+"/approve", map[string]any{"hash": p.Hash})
	m.admin2.expect(409, "POST", "/v1/ai/pushes/"+p.ID+"/approve", map[string]any{"hash": strings.Repeat("0", 64)})
	m.dev.expect(403, "POST", "/v1/ai/pushes/"+p.ID+"/approve", map[string]any{"hash": p.Hash})
	if len(reg.manifests) != 0 {
		t.Fatal("a refused approval pushed something")
	}

	var done AgentPush
	json.Unmarshal(m.admin2.expect(200, "POST", "/v1/ai/pushes/"+p.ID+"/approve", map[string]any{"hash": p.Hash, "note": "ok"}), &done)
	if done.Status != pushPushed || done.DecidedBy != "grace" || !strings.HasPrefix(done.Digest, "sha256:") {
		t.Fatalf("approved: %+v", done)
	}
	man, yaml := reg.pushedYAML(t, "acme/agents/triage:v1")
	if yaml != p.YAML || man.ArtifactType != oci.ArtifactType || man.Annotations[oci.AgentVersionAnnotation] == "" ||
		man.Annotations["io.forgeline.agent.id"] != a.ID || man.Annotations["io.forgeline.agent.fingerprint"] != a.Versions[0].Hash {
		t.Errorf("pushed: %+v\n%s", man, yaml)
	}
	m.admin2.expect(409, "POST", "/v1/ai/pushes/"+p.ID+"/approve", map[string]any{"hash": p.Hash})

	audit := string(m.admin.expect(200, "GET", "/v1/audit?limit=50", nil))
	for _, act := range []string{"ai.registry.create", "ai.agent.push.propose", "ai.agent.push.approve", `"ai.agent.push"`} {
		if !strings.Contains(audit, act) {
			t.Errorf("audit has no %s", act)
		}
	}
}

// A push is bound to the registry's address and token at approval time: a wrong token fails and says so, a changed
// address makes it stale, a removed registry refuses it.
func TestPushRefusalsAndFailures(t *testing.T) {
	m := newMCPHarness(t)
	reg := newBasicRegistry(t)
	g := addRegistry(t, m.admin, reg.host(), "wrong")
	a := newAgent(t, m.admin, agentBody("Triage", nil, nil, "T0"))
	body := map[string]any{"registry": g.ID, "reason": "share the triage agent with the platform team"}
	m.admin.expect(400, "POST", "/v1/ai/agents/"+a.ID+"/push", map[string]any{"registry": g.ID, "reason": "short"})
	m.admin.expect(400, "POST", "/v1/ai/agents/"+a.ID+"/push", map[string]any{"registry": "nope", "reason": "share the triage agent please"})
	m.admin.expect(400, "POST", "/v1/ai/agents/"+a.ID+"/push", map[string]any{"registry": g.ID, "repository": "Bad Name", "reason": "share the triage agent please"})
	m.admin.expect(404, "POST", "/v1/ai/agents/"+a.ID+"/push", map[string]any{"registry": g.ID, "version": 9, "reason": "share the triage agent please"})

	// A wrong token: approved, tried, failed with the registry's answer, nothing stored.
	var p AgentPush
	json.Unmarshal(m.admin.expect(201, "POST", "/v1/ai/agents/"+a.ID+"/push", body), &p)
	if p.Repository != "acme/agents/"+a.ID {
		t.Errorf("default repository: %s", p.Repository)
	}
	var failed AgentPush
	json.Unmarshal(m.admin2.expect(200, "POST", "/v1/ai/pushes/"+p.ID+"/approve", map[string]any{"hash": p.Hash}), &failed)
	if failed.Status != apFailed || !strings.Contains(failed.Result, "401") {
		t.Errorf("wrong token: %+v", failed)
	}

	// The address changed after the proposal: stale.
	json.Unmarshal(m.admin.expect(201, "POST", "/v1/ai/agents/"+a.ID+"/push", body), &p)
	m.admin.expect(200, "PUT", "/v1/ai/registries/"+g.ID, map[string]any{"name": "Team registry", "host": reg.host(), "namespace": "other", "username": "robot"})
	m.admin2.expect(409, "POST", "/v1/ai/pushes/"+p.ID+"/approve", map[string]any{"hash": p.Hash})

	// Removed: refused. The token left the vault with it.
	json.Unmarshal(m.admin.expect(201, "POST", "/v1/ai/agents/"+a.ID+"/push", body), &p)
	m.admin.expect(204, "DELETE", "/v1/ai/registries/"+g.ID, nil)
	if _, ok := m.srv.internalSecretValue(context.Background(), registryTokenName(g.ID)); ok {
		t.Error("the token stayed in the vault")
	}
	m.admin2.expect(409, "POST", "/v1/ai/pushes/"+p.ID+"/approve", map[string]any{"hash": p.Hash})

	// Cancel and reject.
	g2 := addRegistry(t, m.admin, reg.host(), "tok-ok")
	body["registry"] = g2.ID
	json.Unmarshal(m.admin.expect(201, "POST", "/v1/ai/agents/"+a.ID+"/push", body), &p)
	m.admin.expect(200, "POST", "/v1/ai/pushes/"+p.ID+"/cancel", nil)
	m.admin2.expect(409, "POST", "/v1/ai/pushes/"+p.ID+"/approve", map[string]any{"hash": p.Hash})
	json.Unmarshal(m.admin.expect(201, "POST", "/v1/ai/agents/"+a.ID+"/push", body), &p)
	m.admin2.expect(200, "POST", "/v1/ai/pushes/"+p.ID+"/reject", nil)
	m.admin2.expect(409, "POST", "/v1/ai/pushes/"+p.ID+"/approve", map[string]any{"hash": p.Hash})
	if len(reg.manifests) != 0 {
		t.Errorf("something was pushed: %v", reg.manifests)
	}
}

// Plain http only for this machine; a bad host or namespace is refused with where it is wrong.
func TestRegistryChecks(t *testing.T) {
	m := newMCPHarness(t)
	for _, b := range []map[string]any{
		{"name": "", "host": "ghcr.io"},
		{"name": "x", "host": "https://ghcr.io"},
		{"name": "x", "host": "ghcr.io/acme"},
		{"name": "x", "host": "ghcr.io", "namespace": "Acme Corp"},
		{"name": "x", "host": "ghcr.io", "username": "a:b"},
	} {
		m.admin.expect(400, "POST", "/v1/ai/registries", b)
	}
	m.dev.expect(403, "POST", "/v1/ai/registries", map[string]any{"name": "x", "host": "ghcr.io"})
}

// An approval rule for publishing asks for two approvals; a rule that names accounts does not cover pushes.
func TestPushFollowsApprovalRules(t *testing.T) {
	m := newMCPHarness(t)
	m.admin.createUser("hopper", authn.Admin)
	hopper := m.login("hopper")
	reg := newBasicRegistry(t)
	g := addRegistry(t, m.admin, reg.host(), "tok-ok")
	a := newAgent(t, m.admin, agentBody("Triage", nil, nil, "T0"))
	m.admin.expect(200, "PUT", "/v1/settings/approvals", map[string]any{"rules": []map[string]any{
		{"name": "two for publishing", "kinds": []string{"publish"}, "approvals": 2},
	}})
	var p AgentPush
	json.Unmarshal(m.admin.expect(201, "POST", "/v1/ai/agents/"+a.ID+"/push", map[string]any{"registry": g.ID, "reason": "share the triage agent please"}), &p)
	if p.Approval == nil || p.Approval.Needed != 2 {
		t.Fatalf("approval state: %+v", p.Approval)
	}
	var half AgentPush
	json.Unmarshal(m.admin2.expect(200, "POST", "/v1/ai/pushes/"+p.ID+"/approve", map[string]any{"hash": p.Hash}), &half)
	if half.Status != apProposed || len(reg.manifests) != 0 {
		t.Fatalf("one approval of two pushed: %+v", half)
	}
	m.admin2.expect(409, "POST", "/v1/ai/pushes/"+p.ID+"/approve", map[string]any{"hash": p.Hash})
	var done AgentPush
	json.Unmarshal(hopper.expect(200, "POST", "/v1/ai/pushes/"+p.ID+"/approve", map[string]any{"hash": p.Hash}), &done)
	if done.Status != pushPushed {
		t.Fatalf("second approval: %+v", done)
	}

	m.admin.expect(200, "PUT", "/v1/settings/approvals", map[string]any{"rules": []map[string]any{
		{"name": "two in primary", "accounts": []string{"primary"}, "approvals": 2},
	}})
	json.Unmarshal(m.admin.expect(201, "POST", "/v1/ai/agents/"+a.ID+"/push", map[string]any{"registry": g.ID, "reason": "share the triage agent again"}), &p)
	if p.Approval == nil || p.Approval.Needed != 1 {
		t.Errorf("an account rule covered a push: %+v", p.Approval)
	}
	m.admin.expect(400, "PUT", "/v1/settings/approvals", map[string]any{"rules": []map[string]any{{"name": "x", "kinds": []string{"deploy"}, "approvals": 1}}})
}

// While a push is under way, a second approval is refused: the push runs once.
func TestPushRunsOnce(t *testing.T) {
	m := newMCPHarness(t)
	m.admin.createUser("hopper", authn.Admin)
	hopper := m.login("hopper")
	reg := newBasicRegistry(t)
	reg.gate, reg.reached = make(chan struct{}), make(chan struct{}, 1)
	g := addRegistry(t, m.admin, reg.host(), "tok-ok")
	a := newAgent(t, m.admin, agentBody("Triage", nil, nil, "T0"))
	var p AgentPush
	json.Unmarshal(m.admin.expect(201, "POST", "/v1/ai/agents/"+a.ID+"/push", map[string]any{"registry": g.ID, "reason": "share the triage agent please"}), &p)
	done := make(chan []byte)
	go func() {
		_, b := m.admin2.do("POST", "/v1/ai/pushes/"+p.ID+"/approve", map[string]any{"hash": p.Hash})
		done <- b
	}()
	<-reg.reached
	var mid AgentPush
	json.Unmarshal(m.admin.expect(200, "GET", "/v1/ai/pushes/"+p.ID, nil), &mid)
	hopper.expect(409, "POST", "/v1/ai/pushes/"+p.ID+"/approve", map[string]any{"hash": p.Hash})
	close(reg.gate)
	var fin AgentPush
	json.Unmarshal(<-done, &fin)
	if mid.Status != pushPushing || fin.Status != pushPushed {
		t.Errorf("during: %s, after: %s", mid.Status, fin.Status)
	}
}
