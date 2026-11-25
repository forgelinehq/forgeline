package server

import (
	"context"
	"encoding/json"
	"io"
	"strings"
	"sync"
	"testing"

	"github.com/forgelinehq/forgeline/internal/executor"
	"github.com/forgelinehq/forgeline/internal/store"
)

const wantsDeployKey = `version: v1alpha1
name: wants-secret
steps:
  - id: ship
    image: alpine
    run: echo
    permissions:
      secrets: [DEPLOY_KEY]
`

// How much a run is trusted follows from how it started (note 005, slice 3): a push to an allowed branch gets secrets only where
// that repository's rule says so; a pull request never does, whatever the rule says; and the audit log and the API say which.
func TestRunTrustDecidesWhoGetsSecrets(t *testing.T) {
	const value = "deploy-key-value-1234"
	type outcome struct {
		gotSecret bool
		runID     string
	}
	run := func(t *testing.T, secretsAllowed bool, send func(*harness, *fakeGitHub), sha string) (outcome, *harness) {
		t.Helper()
		gh := newFakeGitHub(t)
		gh.addCommit(ghRepo, sha, map[string]string{"forgeline.yaml": wantsDeployKey})
		var mu sync.Mutex
		got := false
		h := newHarness(t, &executor.Fake{Handler: func(_ context.Context, s executor.StepRun, _ io.Writer) (executor.Result, error) {
			mu.Lock()
			defer mu.Unlock()
			got = got || s.Secrets["DEPLOY_KEY"] == value
			return executor.Result{}, nil
		}}, func(c *Config) {
			c.Secrets = map[string]string{"DEPLOY_KEY": value}
			c.GitHub = &GitHubConfig{WebhookSecret: ghSecret, Token: ghToken, APIBase: gh.srv.URL,
				Allow: []RepoRule{{Repo: ghRepo, Branches: []string{"main"}, Secrets: secretsAllowed}}}
		})
		send(h, gh)
		var id string
		h.waitFor("the run to finish", func() bool {
			rs := h.runs()
			if len(rs) == 1 && (rs[0].Status == "succeeded" || rs[0].Status == "failed") {
				id = rs[0].ID
				return true
			}
			return false
		})
		mu.Lock()
		defer mu.Unlock()
		return outcome{gotSecret: got, runID: id}, h
	}
	push := func(sha string) func(*harness, *fakeGitHub) {
		return func(h *harness, _ *fakeGitHub) { h.webhook("push", "d1", pushEvent(ghRepo, "refs/heads/main", sha)) }
	}
	pr := func(sha string) func(*harness, *fakeGitHub) {
		return func(h *harness, _ *fakeGitHub) {
			h.webhook("pull_request", "pr1", prEvent("synchronize", ghRepo, "main", ghRepo, sha, 7))
		}
	}
	auditOf := func(h *harness, action string) []AuditEvent {
		var out struct {
			Events []AuditEvent `json:"events"`
		}
		_, b := h.do("GET", "/v1/audit?limit=100&action="+action, testToken, nil, "")
		json.Unmarshal(b, &out)
		return out.Events
	}
	trustOfRun := func(h *harness, id string) string {
		var r RunJSON
		_, b := h.do("GET", "/v1/runs/"+id, testToken, nil, "")
		json.Unmarshal(b, &r)
		return r.Trust
	}

	// A push where the repository's rule allows secrets: trusted, the step gets the secret, and the grant is in the log.
	o, h := run(t, true, push(sha1st), sha1st)
	if !o.gotSecret || trustOfRun(h, o.runID) != "trusted" {
		t.Errorf("a push to a repository that allows secrets: got secret %v, trust %q", o.gotSecret, trustOfRun(h, o.runID))
	}
	if g := auditOf(h, "run.secret.grant"); len(g) != 1 || !strings.Contains(g[0].Detail, "run started by github (trusted)") || strings.Contains(g[0].Detail, value) {
		t.Errorf("the grant in the audit log: %+v", g)
	}

	// A push where it does not: refused, with how to change that, and the refusal is in the log.
	o, h = run(t, false, push(sha1st), sha1st)
	if o.gotSecret {
		t.Error("a push got a secret although the repository's rule does not allow it")
	}
	_, steps, _ := h.store.GetRun(context.Background(), o.runID)
	if len(steps) != 1 || !strings.Contains(steps[0].Error, "Settings › GitHub") || strings.Contains(steps[0].Error, value) {
		t.Errorf("the refusal the step shows: %+v", steps)
	}
	if r := auditOf(h, "run.secret.refused"); len(r) != 1 || r[0].Allowed {
		t.Errorf("the refusal in the audit log: %+v", r)
	}

	// A same-repository pull request is untrusted: no secret, even where the rule allows them for pushes.
	o, h = run(t, true, pr(sha2nd), sha2nd)
	if o.gotSecret {
		t.Error("a pull request got a secret")
	}
	if trustOfRun(h, o.runID) != "untrusted" {
		t.Errorf("a pull request's trust = %q", trustOfRun(h, o.runID))
	}
	_, steps, _ = h.store.GetRun(context.Background(), o.runID)
	if len(steps) != 1 || !strings.Contains(steps[0].Error, "pull request") || !strings.Contains(steps[0].Error, "untrusted") {
		t.Errorf("the pull request's refusal: %+v", steps)
	}
	if r := auditOf(h, "run.secret.refused"); len(r) != 1 || !strings.Contains(r[0].Detail, "(untrusted)") {
		t.Errorf("the pull request's refusal in the audit log: %+v", r)
	}

	// secretsFor directly: the rule must be the repository's own, and the branch must still be one it allows.
	ctx := context.Background()
	h2 := newHarness(t, &executor.Fake{}, func(c *Config) {
		c.Secrets = map[string]string{"DEPLOY_KEY": value}
		c.GitHub = &GitHubConfig{WebhookSecret: ghSecret, Token: ghToken, Allow: []RepoRule{{Repo: ghRepo, Branches: []string{"main"}, Secrets: true}, {Repo: "acme/other", Branches: []string{"main"}}}}
	})
	for name, tc := range map[string]struct {
		run  store.Run
		want bool
	}{
		"a push to an allowed branch of a repository that allows secrets": {store.Run{Trigger: store.TriggerGitHub, SourceRepo: ghRepo, SourceRef: "refs/heads/main"}, true},
		"the repository's name in other letters":                          {store.Run{Trigger: store.TriggerGitHub, SourceRepo: strings.ToUpper(ghRepo), SourceRef: "refs/heads/main"}, true},
		"a branch the rule does not allow":                                {store.Run{Trigger: store.TriggerGitHub, SourceRepo: ghRepo, SourceRef: "refs/heads/feature"}, false},
		"another repository whose rule does not allow secrets":            {store.Run{Trigger: store.TriggerGitHub, SourceRepo: "acme/other", SourceRef: "refs/heads/main"}, false},
		"a repository with no rule":                                       {store.Run{Trigger: store.TriggerGitHub, SourceRepo: "acme/unknown", SourceRef: "refs/heads/main"}, false},
		"a pull request of the repository that allows secrets":            {store.Run{Trigger: store.TriggerGitHub, SourceRepo: ghRepo, SourceRef: "refs/pull/3/head"}, false},
		"a console run": {store.Run{Trigger: store.TriggerUI}, true},
	} {
		got, why := h2.srv.secretsFor(ctx, tc.run)
		if (len(got) > 0) != tc.want || (!tc.want && why == "") {
			t.Errorf("%s: secrets %v, reason %q, want secrets %v", name, len(got), why, tc.want)
		}
	}

	// The rule is the repository's own: another repository's setting does not carry over, and a console run is trusted as before.
	if got := trustOf(storeRun("api", "")); got != "trusted" {
		t.Errorf("an API run's trust = %q", got)
	}
}

func storeRun(trigger, ref string) store.Run {
	return store.Run{Trigger: store.Trigger(trigger), SourceRef: ref}
}

// The setting is per repository, off unless an administrator turns it on, and survives saving and reading back.
func TestTheSecretsRuleIsPerRepositoryAndOffByDefault(t *testing.T) {
	g := newActionsGH(t)
	h := newHarness(t, &executor.Fake{})
	admin := h.adminBrowser("ada")
	save := func(rules []map[string]any) {
		admin.expect(200, "PUT", "/v1/integrations/github", map[string]any{
			"enabled": true, "api_base": g.srv.URL, "public_url": "https://ci.example.com", "rules": rules,
			"token": ghToken, "generate_webhook_secret": true,
		})
	}
	read := func() map[string]bool {
		var out struct {
			Rules []githubRule `json:"rules"`
		}
		json.Unmarshal(admin.expect(200, "GET", "/v1/integrations/github", nil), &out)
		m := map[string]bool{}
		for _, r := range out.Rules {
			m[r.Repo] = r.Secrets
		}
		return m
	}
	save([]map[string]any{{"repo": ghRepo, "branches": []string{"main"}}, {"repo": "acme/deploys", "branches": []string{"main"}, "secrets": true}})
	if got := read(); got[ghRepo] || !got["acme/deploys"] || len(got) != 2 {
		t.Errorf("rules read back: %v", got)
	}
	// Saved again without the field, it is off again: it is never kept by accident.
	save([]map[string]any{{"repo": "acme/deploys", "branches": []string{"main"}}})
	if got := read(); got["acme/deploys"] {
		t.Errorf("the secrets rule was kept when it was not sent: %v", got)
	}
	// And the running configuration carries it.
	save([]map[string]any{{"repo": ghRepo, "branches": []string{"main"}, "secrets": true}})
	if cfg := h.srv.gh(); cfg == nil || len(cfg.Allow) != 1 || !cfg.Allow[0].Secrets {
		t.Errorf("the running rule: %+v", cfg)
	}
}
