package server

import (
	"context"
	"encoding/base64"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/forgelinehq/forgeline/internal/build"
	"github.com/forgelinehq/forgeline/internal/pipeline"
	"github.com/forgelinehq/forgeline/internal/store"
)

const (
	testTarget = "123456789012.dkr.ecr.ap-south-1.amazonaws.com/web"
	testSHA    = "0123456789abcdef0123456789abcdef01234567"
)

func pushRuleBody(over map[string]any) map[string]any {
	b := map[string]any{"repo": "acme/web", "branches": []string{"main", "release/*"}, "target": testTarget, "account": "primary"}
	for k, v := range over {
		b[k] = v
	}
	return b
}

func TestBuildPushRulesAPI(t *testing.T) {
	m := newMCPHarness(t)
	m.turnOn() // a connected primary AWS account
	for name, over := range map[string]map[string]any{
		"tagged target":     {"target": testTarget + ":latest"},
		"docker hub":        {"target": "docker.io/acme/web"},
		"every branch":      {"branches": []string{"*"}},
		"no branch":         {"branches": []string{}},
		"full ref":          {"branches": []string{"refs/heads/main"}},
		"repo without name": {"repo": "acme"},
		"unknown account":   {"account": "nope"},
	} {
		if msg := string(m.admin.expect(400, "PUT", "/v1/build-push-rules/web", pushRuleBody(over))); msg == "" {
			t.Errorf("%s: accepted", name)
		}
	}
	m.admin.expect(400, "PUT", "/v1/build-push-rules/Bad_Id", pushRuleBody(nil))
	m.dev.expect(403, "PUT", "/v1/build-push-rules/web", pushRuleBody(nil))
	m.dev.expect(403, "GET", "/v1/build-push-rules", nil)
	m.admin.expect(200, "PUT", "/v1/build-push-rules/web", pushRuleBody(nil))
	if !strings.Contains(string(m.admin.expect(200, "GET", "/v1/build-push-rules", nil)), testTarget) {
		t.Error("the rule is not listed")
	}
	if !hasAudit(t, m.admin, "build.pushrule.put") {
		t.Error("saving a rule is not audited")
	}
	m.dev.expect(403, "DELETE", "/v1/build-push-rules/web", nil)
	m.admin.expect(204, "DELETE", "/v1/build-push-rules/web", nil)
	m.admin.expect(404, "DELETE", "/v1/build-push-rules/web", nil)
}

func TestBuildStepPushesOnlyForATrustedPush(t *testing.T) {
	m := newMCPHarness(t)
	m.turnOn()
	m.admin.expect(200, "PUT", "/v1/build-push-rules/web", pushRuleBody(nil))
	srv := m.harness.srv

	var calls []build.Request
	oldRun, oldTok := buildRun, ecrToken
	defer func() { buildRun, ecrToken = oldRun, oldTok }()
	buildRun = func(_ context.Context, r build.Request, _ io.Writer) (build.Result, error) {
		calls = append(calls, r)
		return build.Result{Digest: "sha256:" + strings.Repeat("ab", 32)}, nil
	}
	tokenAuth := base64.StdEncoding.EncodeToString([]byte("AWS:topsecret"))
	var tokenFor []string
	tokenHost := "123456789012.dkr.ecr.ap-south-1.amazonaws.com"
	ecrToken = func(_ context.Context, _ *Server, _ awsSettings, registryID string) (string, string, error) {
		tokenFor = append(tokenFor, registryID)
		return tokenHost, tokenAuth, nil
	}

	push := func(run store.Run, b pipeline.Build) (error, string) {
		t.Helper()
		calls = nil
		var logs strings.Builder
		err := srv.buildStep(run)(context.Background(), &pipeline.Step{ID: "image", Build: &b}, &logs)
		return err, logs.String()
	}
	gh := func(ref string) store.Run {
		return store.Run{ID: "r1", Trigger: store.TriggerGitHub, SourceRepo: "Acme/Web", SourceRef: ref, SourceSHA: testSHA}
	}
	withPush := pipeline.Build{Context: "app", Push: testTarget}

	// Refused before anything is built or any credential is asked for.
	for name, c := range map[string]struct {
		run store.Run
		b   pipeline.Build
	}{
		"console run":           {store.Run{ID: "r1", Trigger: store.TriggerUI}, withPush},
		"api run":               {store.Run{ID: "r1", Trigger: store.TriggerAPI}, withPush},
		"api run claiming main": {store.Run{ID: "r1", Trigger: store.TriggerAPI, SourceRepo: "acme/web", SourceRef: "refs/heads/main", SourceSHA: testSHA}, withPush},
		"ui run claiming main":  {store.Run{ID: "r1", Trigger: store.TriggerUI, SourceRepo: "acme/web", SourceRef: "refs/heads/main", SourceSHA: testSHA}, withPush},
		"pull request":          {gh("refs/pull/7/head"), withPush},
		"branch not in rule":    {gh("refs/heads/feature/x"), withPush},
		"tag ref":               {gh("refs/tags/v1"), withPush},
		"other repository":      {store.Run{ID: "r1", Trigger: store.TriggerGitHub, SourceRepo: "acme/other", SourceRef: "refs/heads/main", SourceSHA: testSHA}, withPush},
		"target one char off":   {gh("refs/heads/main"), pipeline.Build{Context: "app", Push: testTarget + "s"}},
		"other registry":        {gh("refs/heads/main"), pipeline.Build{Context: "app", Push: "210987654321.dkr.ecr.ap-south-1.amazonaws.com/web"}},
	} {
		tokenFor = nil
		err, _ := push(c.run, c.b)
		if err == nil || !strings.Contains(err.Error(), "push refused") || len(calls) != 0 || len(tokenFor) != 0 {
			t.Errorf("%s: err=%v builds=%d tokens=%v", name, err, len(calls), tokenFor)
		}
	}
	// A rule whose odd pattern would match a pull request's ref still never lets a pull request push.
	m.admin.expect(200, "PUT", "/v1/build-push-rules/odd", pushRuleBody(map[string]any{"branches": []string{"r*/p*/*/h*"}}))
	if err, _ := push(gh("refs/pull/7/head"), withPush); err == nil || len(calls) != 0 {
		t.Errorf("a pull request pushed through an odd pattern: %v", err)
	}
	m.admin.expect(204, "DELETE", "/v1/build-push-rules/odd", nil)
	if !strings.Contains(string(m.admin.expect(200, "GET", "/v1/audit?action=build.push", nil)), "refused") {
		t.Error("refused pushes are not audited")
	}

	// A pull request still builds, without pushing and without any credential.
	tokenFor = nil
	if err, _ := push(gh("refs/pull/7/head"), pipeline.Build{Context: "app"}); err != nil || len(calls) != 1 || len(calls[0].Refs) != 0 || calls[0].Auth != "" || len(tokenFor) != 0 {
		t.Fatalf("build-only: err=%v calls=%+v tokens=%v", err, calls, tokenFor)
	}

	// A push to main (and a release branch) pushes, tagged with the commit, with the token for that registry only.
	for _, ref := range []string{"refs/heads/main", "refs/heads/release/2"} {
		tokenFor = nil
		err, logs := push(gh(ref), withPush)
		if err != nil || len(calls) != 1 {
			t.Fatalf("%s: err=%v calls=%d", ref, err, len(calls))
		}
		c := calls[0]
		if len(c.Refs) != 1 || c.Refs[0] != testTarget+":"+testSHA[:12] || c.AuthHost != tokenHost || c.Auth != tokenAuth || len(tokenFor) != 1 || tokenFor[0] != "123456789012" {
			t.Errorf("%s: request = %+v, tokens for %v", ref, c, tokenFor)
		}
		if strings.Contains(logs, tokenAuth) || strings.Contains(logs, "topsecret") {
			t.Errorf("%s: the credential is in the log", ref)
		}
		if !strings.Contains(logs, "pushed "+testTarget+"@sha256:") {
			t.Errorf("%s: logs = %s", ref, logs)
		}
	}
	if a := string(m.admin.expect(200, "GET", "/v1/audit?action=build.push", nil)); !strings.Contains(a, testTarget+":"+testSHA[:12]) || strings.Contains(a, "topsecret") {
		t.Errorf("the push is not audited with its tag, or the audit holds the credential: %s", a)
	}
	// Own tags.
	b := withPush
	b.Tags = []string{"latest", "v2"}
	if err, _ := push(gh("refs/heads/main"), b); err != nil || len(calls[0].Refs) != 2 || calls[0].Refs[1] != testTarget+":v2" {
		t.Errorf("tags: err=%v refs=%v", err, calls)
	}

	// A credential for another registry is not used.
	tokenHost = "999999999999.dkr.ecr.ap-south-1.amazonaws.com"
	if err, _ := push(gh("refs/heads/main"), withPush); err == nil || len(calls) != 0 {
		t.Errorf("a credential for another registry was used: %v", err)
	}
	tokenHost = "123456789012.dkr.ecr.ap-south-1.amazonaws.com"

	// The rule is removed: the same push is refused.
	m.admin.expect(204, "DELETE", "/v1/build-push-rules/web", nil)
	if err, _ := push(gh("refs/heads/main"), withPush); err == nil || len(calls) != 0 {
		t.Errorf("a push without a rule: %v", err)
	}

	// A failed build is audited as failed.
	buildRun = func(context.Context, build.Request, io.Writer) (build.Result, error) {
		return build.Result{}, errors.New("the build failed: exit 1")
	}
	if err, _ := push(gh("refs/pull/7/head"), pipeline.Build{Context: "app"}); err == nil {
		t.Error("a failed build succeeded")
	}
	if !strings.Contains(string(m.admin.expect(200, "GET", "/v1/audit?action=build.run", nil)), "exit 1") {
		t.Error("a failed build is not audited")
	}
}
