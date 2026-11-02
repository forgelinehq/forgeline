package server

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/forgelinehq/forgeline/internal/executor"
)

// integRun answers the commands the hub runs: `docker info`, `aws configure list-profiles`, and (through
// the explorer's stub) the AWS calls a test makes.
func integRun(stub *awsStub, dockerUp bool, profiles string) func(context.Context, string, ...string) ([]byte, string, error) {
	return func(ctx context.Context, name string, args ...string) ([]byte, string, error) {
		switch {
		case name == "docker":
			if !dockerUp {
				return nil, "Cannot connect to the Docker daemon", errors.New("exit 1")
			}
			return []byte(`{"ServerVersion":"29.3.0","Containers":3,"ContainersRunning":1,"Images":5}`), "", nil
		case name == "aws" && len(args) >= 2 && args[0] == "configure" && args[1] == "list-profiles":
			return []byte(profiles), "", nil
		}
		return stub.run(ctx, name, args...)
	}
}

func hub(t *testing.T, b *browser) map[string]IntegrationJSON {
	t.Helper()
	var out IntegrationsJSON
	if err := json.Unmarshal(b.expect(200, "GET", "/v1/integrations", nil), &out); err != nil {
		t.Fatal(err)
	}
	m := map[string]IntegrationJSON{}
	for _, it := range out.Integrations {
		if _, dup := m[it.ID]; dup {
			t.Errorf("integration %q is listed twice", it.ID)
		}
		m[it.ID] = it
	}
	return m
}

func TestHubListsEverySystemWithAStatusAndPlannedOnesHaveNoTest(t *testing.T) {
	m := newMCPHarness(t)
	m.srv.run = integRun(&awsStub{}, true, "default\nold\nforgeline-sso\n")
	m.v.expect(403, "GET", "/v1/integrations", nil) // a viewer is below the role
	h := hub(t, m.dev)

	for id, want := range map[string]string{"github": intOff, "aws": intOff, "aws-mcp": intOff, "docker": intConnected, "kubernetes": intOff, "claude": intConnected} {
		if got := h[id].Status; got != want {
			t.Errorf("%s status = %q, want %q (%+v)", id, got, want, h[id])
		}
	}
	valid := map[string]bool{intConnected: true, intAttention: true, intDetected: true, intOff: true, intNext: true, intLater: true}
	for id, it := range h {
		if it.Blurb == "" || len(it.Blurb) > 160 {
			t.Errorf("%s needs a short blurb for its card: %q", id, it.Blurb)
		}
		if (it.Status == intNext || it.Status == intLater) && it.Blurb != firstSentence(it.Plan) {
			t.Errorf("planned %s: the blurb should be the plan's first sentence, got %q", id, it.Blurb)
		}
		if !valid[it.Status] || it.Name == "" || it.Category == "" || it.Summary == "" {
			t.Errorf("%s is incomplete: %+v", id, it)
		}
		if (it.Status == intNext || it.Status == intLater) && (it.Testable || it.Plan == "" || it.Order == 0) {
			t.Errorf("planned %s should say what it will do, have an order and no test: %+v", id, it)
		}
	}
	// The order follows the maintainer's decision: GCP (now built), then Azure, then AI infrastructure, then Datadog.
	if g := h["gcp"]; g.Status != intOff || g.Order != 0 || !g.Testable || g.ReadOnlyBy != "provider" || g.Manage != "/infrastructure?tab=gcp" {
		t.Errorf("gcp is built now, so it is no longer planned: %+v", g)
	}
	if az := h["azure"]; az.Status != intOff || az.Order != 0 || !az.Testable || az.ReadOnlyBy != "provider" || az.Manage != "/infrastructure?tab=azure" {
		t.Errorf("azure is built now, so it is no longer planned: %+v", az)
	}
	if !(h["cloud-models"].Order < h["lambda"].Order && h["lambda"].Order < h["datadog"].Order) {
		t.Errorf("order = cloud models %d lambda %d datadog %d", h["cloud-models"].Order, h["lambda"].Order, h["datadog"].Order)
	}
	// Who enforces read-only is stated, and for Lambda it is Forgeline, because the provider has no read-only keys.
	if h["lambda"].ReadOnlyBy != "forgeline" || h["gcp"].ReadOnlyBy != "provider" || h["runpod"].ReadOnlyBy != "forgeline" {
		t.Errorf("read-only by: lambda %q gcp %q runpod %q", h["lambda"].ReadOnlyBy, h["gcp"].ReadOnlyBy, h["runpod"].ReadOnlyBy)
	}
	if !strings.Contains(h["runpod"].Plan, "2026-11-15") {
		t.Errorf("the RunPod plan does not mention that REST v1 retires: %q", h["runpod"].Plan)
	}
}

// The categories the server uses must be the ones the console lays out, or a card would not appear.
func TestHubCategoriesAreTheOnesTheConsoleKnows(t *testing.T) {
	m := newMCPHarness(t)
	m.srv.run = integRun(&awsStub{}, true, "old\n")
	js, err := uiFiles.ReadFile("ui/lib/hub.js")
	if err != nil {
		t.Fatal(err)
	}
	for _, it := range hub(t, m.dev) {
		if !strings.Contains(string(js), "'"+it.Category+"'") {
			t.Errorf("%s is in the category %q, which ui/lib/hub.js does not list", it.ID, it.Category)
		}
	}
}

func TestHubDetectsLoginsOnTheServerByNameOnly(t *testing.T) {
	m := newMCPHarness(t)
	stub := &awsStub{}
	m.srv.run = integRun(stub, true, "default\nold\nforgeline-sso\ndev\nstaging\nbad name!\n")
	m.admin.expect(200, "PUT", "/v1/integrations/aws", map[string]any{"enabled": true, "profile": "dev", "region": "ap-south-1"})
	// An account that was already added is not offered again.
	m.srv.docs.Put(context.Background(), kindAWSAccount, "a1", AWSAccount{ID: "a1", Name: "Staging", Profile: "staging", Region: "ap-south-1", AccountID: "444455556666"})

	before := len(stub.calls)
	h := hub(t, m.dev)
	var found []string
	for id, it := range h {
		if it.Status == intDetected {
			found = append(found, id)
			if !strings.HasPrefix(id, "aws-profile:") || it.Found == "" || it.Testable {
				t.Errorf("detected %s = %+v", id, it)
			}
			// Only the name is shown: nothing that looks like a key, a token or a URL.
			for _, bad := range []string{"AKIA", "token", "http", "secret"} {
				if strings.Contains(strings.ToLower(it.Found), strings.ToLower(bad)) && bad != "token" {
					t.Errorf("%s: found text mentions %q: %s", id, bad, it.Found)
				}
			}
		}
	}
	sort.Strings(found)
	got := strings.Join(found, ",")
	if got != "aws-profile:default,aws-profile:old" {
		t.Errorf("detected = %s; want default and old only (not the connected dev, the reserved forgeline-sso, the added staging, or a name that is not a valid profile)", got)
	}
	// Detection never ran anything but a list of names.
	for _, c := range stub.calls[before:] {
		t.Errorf("listing the hub made an AWS call: %q", c)
	}
}

func TestHubFlagsAnAwsSignInThatEndsSoonOrHasEnded(t *testing.T) {
	m := newMCPHarness(t)
	m.srv.run = integRun(&awsStub{}, true, "")
	ctx := context.Background()
	m.srv.docs.Put(ctx, kindSettings, idAWS, awsSettings{Enabled: true, Profile: ssoProfile, Region: "ap-south-1"})
	setExpiry := func(d time.Duration) {
		val, _ := json.Marshal(ssoToken{AccessToken: ssoAccessToken, ExpiresAt: time.Now().Add(d)})
		m.srv.putSecret(ctx, "test", ssoTokenName, string(val), "")
	}
	for _, c := range []struct {
		left    time.Duration
		status  string
		problem string
	}{
		{4 * time.Hour, intConnected, ""},
		{20 * time.Minute, intAttention, "ends in"},
		{-time.Minute, intAttention, "has expired"},
	} {
		setExpiry(c.left)
		it := hub(t, m.dev)["aws"]
		if it.Status != c.status || (c.problem == "") != (it.Problem == "") || !strings.Contains(it.Problem, c.problem) {
			t.Errorf("sign-in with %v left: status %q problem %q; want %q and %q", c.left, it.Status, it.Problem, c.status, c.problem)
		}
	}
}

func TestHubMarksAnUnreachableDockerAndAnMcpWithoutAws(t *testing.T) {
	m := newMCPHarness(t)
	m.srv.run = integRun(&awsStub{}, false, "")
	m.srv.docs.Put(context.Background(), kindSettings, idAWSMCP, awsMCPSettings{Enabled: true})
	h := hub(t, m.dev)
	if d := h["docker"]; d.Status != intAttention || !strings.Contains(d.Problem, "cannot reach Docker") {
		t.Errorf("docker = %+v", d)
	}
	if mc := h["aws-mcp"]; mc.Status != intAttention || !strings.Contains(mc.Problem, "AWS is not connected") {
		t.Errorf("aws-mcp = %+v", mc)
	}
}

func TestIntegrationTestOnlyReadsAndSaysWhatToFix(t *testing.T) {
	m := newMCPHarness(t)
	stub := &awsStub{}
	m.srv.run = integRun(stub, true, "")
	m.admin.expect(200, "PUT", "/v1/integrations/aws", map[string]any{"enabled": true, "profile": "dev", "region": "ap-south-1"})
	m.dev.expect(403, "POST", "/v1/integrations/aws/checks", nil) // a test is for administrators
	m.admin.expect(404, "POST", "/v1/integrations/nope/checks", nil)
	m.admin.expect(404, "POST", "/v1/integrations/lambda/checks", nil) // planned: nothing to test

	var res IntegrationTestJSON
	if err := json.Unmarshal(m.admin.expect(200, "POST", "/v1/integrations/aws/checks", nil), &res); err != nil {
		t.Fatal(err)
	}
	if res.Failed != 0 || res.Passed < 3 || res.Changed || len(res.Checks) != res.Passed+res.Warned+res.Failed {
		t.Errorf("result = %+v", res)
	}
	for _, c := range stub.calls {
		f := strings.Fields(c)
		if len(f) >= 2 && !(strings.HasPrefix(f[1], "describe-") || strings.HasPrefix(f[1], "list-") || f[1] == "get-caller-identity") {
			t.Errorf("the test ran %q, which is not a read", c)
		}
	}
	// The result is remembered and shown in the hub with who ran it, and the run is audited.
	if lt := hub(t, m.dev)["aws"].LastTest; lt == nil || lt.By == "" || lt.Passed != res.Passed {
		t.Errorf("last test = %+v", lt)
	}
	if !strings.Contains(string(m.admin.expect(200, "GET", "/v1/audit", nil)), "integration.test") {
		t.Error("the test was not audited")
	}

	// A role that may not read alarms is a warning with the permission named; one that may not read at all fails.
	m.srv.run = integRun(&awsStub{deny: map[string]bool{"cloudwatch describe-alarms": true}}, true, "")
	json.Unmarshal(m.admin.expect(200, "POST", "/v1/integrations/aws/checks", nil), &res)
	var warn *IntegrationCheckJSON
	for i, c := range res.Checks {
		if c.Result == "warn" {
			warn = &res.Checks[i]
		}
	}
	if warn == nil || !strings.Contains(warn.Detail+warn.Fix, "cloudwatch:DescribeAlarms") || res.Failed != 0 {
		t.Errorf("denied alarms: %+v", res)
	}
	m.srv.run = integRun(&awsStub{deny: map[string]bool{"ec2 describe-vpcs": true}}, true, "")
	json.Unmarshal(m.admin.expect(200, "POST", "/v1/integrations/aws/checks", nil), &res)
	if res.Failed != 1 {
		t.Errorf("denied reads should fail one check: %+v", res)
	}
}

func TestIntegrationTestsForDockerGitHubAndNotConnected(t *testing.T) {
	m := newMCPHarness(t)
	m.srv.run = integRun(&awsStub{}, true, "")
	var res IntegrationTestJSON
	json.Unmarshal(m.admin.expect(200, "POST", "/v1/integrations/docker/checks", nil), &res)
	if res.Passed != 1 || res.Failed != 0 {
		t.Errorf("docker = %+v", res)
	}
	m.srv.run = integRun(&awsStub{}, false, "")
	json.Unmarshal(m.admin.expect(200, "POST", "/v1/integrations/docker/checks", nil), &res)
	if res.Failed != 1 || res.Checks[0].Fix == "" {
		t.Errorf("docker down = %+v", res)
	}
	// Systems that are not connected fail with what to do, rather than pretending to pass.
	for _, id := range []string{"github", "aws", "gcp", "azure", "aws-mcp", "kubernetes"} {
		json.Unmarshal(m.admin.expect(200, "POST", "/v1/integrations/"+id+"/checks", nil), &res)
		if res.Failed == 0 || res.Passed != 0 {
			t.Errorf("%s not connected: %+v", id, res)
		}
		for _, c := range res.Checks {
			if c.Result == "fail" && c.Fix == "" {
				t.Errorf("%s: a failed check does not say what to do: %+v", id, c)
			}
		}
	}
}

func TestHubNeverShowsASecret(t *testing.T) {
	m := newMCPHarness(t)
	m.srv.run = integRun(&awsStub{}, true, "")
	// assistantHarness stored an API key for the assistant; it must not appear anywhere in the hub.
	body := string(m.dev.expect(200, "GET", "/v1/integrations", nil)) + string(m.admin.expect(200, "POST", "/v1/integrations/claude/checks", nil))
	if strings.Contains(body, assistKey) {
		t.Error("the hub shows the assistant's key")
	}
}

func TestHubAndTestForAConnectedGitHub(t *testing.T) {
	gh := newFakeGitHub(t)
	gh.addCommit(ghRepo, sha1st, map[string]string{"forgeline.yaml": ghPipeline})
	h := newHarness(t, &executor.Fake{})
	admin := h.adminBrowser("ada")
	h.srv.run = integRun(&awsStub{}, true, "")
	connectGitHub(t, admin, gh, map[string]any{"rules": []map[string]any{
		{"repo": ghRepo, "branches": []string{"main"}},
		{"repo": "acme/missing", "branches": []string{"main"}},
	}})

	it := hub(t, admin)["github"]
	if it.Status != intConnected || !strings.Contains(it.Summary, "Connected") || it.ReadOnlyBy != "provider" {
		t.Errorf("github = %+v", it)
	}
	var rules string
	for _, d := range it.Details {
		if d.Label == "Repositories allowed" {
			rules = d.Value
		}
	}
	if rules != "2" {
		t.Errorf("repositories allowed = %q", rules)
	}

	var res IntegrationTestJSON
	json.Unmarshal(admin.expect(200, "POST", "/v1/integrations/github/checks", nil), &res)
	var okRepo, badRepo *IntegrationCheckJSON
	for i, c := range res.Checks {
		switch {
		case strings.Contains(c.Name, ghRepo):
			okRepo = &res.Checks[i]
		case strings.Contains(c.Name, "acme/missing"):
			badRepo = &res.Checks[i]
		}
	}
	if okRepo == nil || okRepo.Result != "ok" || badRepo == nil || badRepo.Result != "fail" || badRepo.Fix == "" {
		t.Fatalf("checks = %+v", res.Checks)
	}
	if res.Failed != 1 || res.Changed {
		t.Errorf("result = %+v", res)
	}
	if lt := hub(t, admin)["github"].LastTest; lt == nil || lt.Failed != 1 {
		t.Errorf("last test = %+v", lt)
	}
}

func activityOf(t *testing.T, b *browser, id string) []IntegrationActivityJSON {
	t.Helper()
	var out struct{ Events []IntegrationActivityJSON }
	if err := json.Unmarshal(b.expect(200, "GET", "/v1/integrations/"+id+"/activity", nil), &out); err != nil {
		t.Fatal(err)
	}
	return out.Events
}

func TestIntegrationActivityShowsOnlyThatIntegrationsEntries(t *testing.T) {
	g := &gcloudStub{assets: gcpAssetsJSON}
	m := gcpHarness(t, g)
	m.dev.expect(403, "GET", "/v1/integrations/gcp/activity", nil) // activity is for administrators
	m.v.expect(403, "GET", "/v1/integrations/gcp/activity", nil)
	m.admin.expect(404, "GET", "/v1/integrations/nope/activity", nil)
	m.admin.expect(404, "GET", "/v1/integrations/lambda/activity", nil) // planned: nothing to show

	// A refused connection, a successful one and a test: three entries for Google Cloud.
	g.stderr = "PERMISSION_DENIED"
	m.admin.expect(422, "PUT", "/v1/integrations/gcp", map[string]any{"enabled": true, "scope": "projects/shop-prod"})
	g.stderr = ""
	m.admin.expect(200, "PUT", "/v1/integrations/gcp", map[string]any{"enabled": true, "scope": "projects/shop-prod"})
	m.admin.expect(200, "POST", "/v1/integrations/gcp/checks", nil)
	// AWS and the AWS MCP Server do their own things; neither may show up under Google Cloud, or under each other.
	m.admin.expect(200, "PUT", "/v1/integrations/aws", map[string]any{"enabled": true, "profile": "dev", "region": "ap-south-1"})
	m.admin.expect(200, "PUT", "/v1/integrations/aws/mcp", map[string]any{"enabled": true})
	m.admin.expect(200, "POST", "/v1/integrations/aws/checks", nil)

	gcp := activityOf(t, m.admin, "gcp")
	if len(gcp) != 3 {
		t.Fatalf("google cloud activity = %+v", gcp)
	}
	if gcp[0].Action != "integration.test" || gcp[1].Action != "integration.gcp.update" || !gcp[1].Allowed || gcp[2].Allowed {
		t.Errorf("newest first, with the refusal marked: %+v", gcp)
	}
	if !strings.Contains(gcp[2].Detail, "refused") || gcp[0].Actor == "" {
		t.Errorf("entries = %+v", gcp)
	}
	aws := activityOf(t, m.admin, "aws")
	mcp := activityOf(t, m.admin, "aws-mcp")
	for _, e := range aws {
		if strings.Contains(e.Action, "awsmcp") || strings.Contains(e.Action, "gcp") {
			t.Errorf("aws shows %+v", e)
		}
	}
	if len(aws) < 2 { // the connection and the test
		t.Errorf("aws activity = %+v", aws)
	}
	if len(mcp) != 1 || mcp[0].Action != "integration.awsmcp.update" {
		t.Errorf("aws-mcp activity = %+v (it must not include AWS's own entries)", mcp)
	}
	// Claude's entries are the assistant's settings and its tests; Kubernetes' are its cluster changes.
	m.admin.expect(200, "PUT", "/v1/integrations/assistant", map[string]any{"backend": "api", "api_key": assistKey, "api_base": m.llm.srv.URL})
	m.srv.audit(context.Background(), Principal{Name: "ada", Role: "admin"}, "", "k8s.cluster.add", "k8s/demo", "demo (token)", true)
	m.srv.audit(context.Background(), Principal{Name: "ada", Role: "admin"}, "", "kube.unrelated", "secret/x", "not a cluster change", true)
	if c := activityOf(t, m.admin, "claude"); len(c) == 0 || c[0].Action != "integration.assistant.update" {
		t.Errorf("claude activity = %+v", c)
	}
	if k := activityOf(t, m.admin, "kubernetes"); len(k) != 1 || k[0].Action != "k8s.cluster.add" {
		t.Errorf("kubernetes activity = %+v", k)
	}
	for _, e := range activityOf(t, m.admin, "gcp") {
		if strings.HasPrefix(e.Action, "k8s.") || strings.Contains(e.Action, "assistant") {
			t.Errorf("google cloud shows another integration's entry: %+v", e)
		}
	}
	// Docker records nothing; an empty list, not an error and not null.
	if d := activityOf(t, m.admin, "docker"); d == nil || len(d) != 0 {
		t.Errorf("docker = %+v", d)
	}
	// The address of the person is not part of it.
	raw := string(m.admin.expect(200, "GET", "/v1/integrations/gcp/activity", nil))
	if strings.Contains(raw, `"ip"`) {
		t.Errorf("the activity includes an address: %s", raw)
	}
}

func TestIntegrationActivityIsBounded(t *testing.T) {
	m := newMCPHarness(t)
	m.srv.run = integRun(&awsStub{}, true, "")
	for i := 0; i < integActivityMax+8; i++ {
		m.admin.expect(200, "POST", "/v1/integrations/github/checks", nil) // each test is recorded under GitHub
	}
	ev := activityOf(t, m.admin, "github")
	if len(ev) != integActivityMax {
		t.Errorf("%d entries, want the newest %d", len(ev), integActivityMax)
	}
	for i := 1; i < len(ev); i++ {
		if ev[i].Time.After(ev[i-1].Time) {
			t.Errorf("entries are not newest first at %d", i)
		}
	}
}
