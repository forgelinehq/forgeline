package blueprint

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/forgelinehq/forgeline/internal/guardrail"
	"github.com/forgelinehq/forgeline/internal/pipeline"
	"sigs.k8s.io/yaml"
)

func sourced() Blueprint {
	b := agentStack()
	b.Blocks = append(b.Blocks, Block{ID: "api", Kind: KindService, Name: "API", Image: "nginx:1.29", CPU: 256, Memory: 512, Count: 1, Port: 80,
		Source: &Source{Repo: "acme/shop", Branch: "release", Path: "services/api"}})
	b.Blocks[0].Source = &Source{Repo: "acme/support-agent"}
	return b
}

func TestSourceValidates(t *testing.T) {
	if err := sourced().Validate(); err != nil {
		t.Fatalf("valid: %v", err)
	}
	for name, src := range map[string]Source{
		"no owner":      {Repo: "shop"},
		"spaces":        {Repo: "acme/my shop"},
		"branch dots":   {Repo: "acme/shop", Branch: "a/../b"},
		"branch slash":  {Repo: "acme/shop", Branch: "/main"},
		"path escapes":  {Repo: "acme/shop", Path: "../etc"},
		"path absolute": {Repo: "acme/shop", Path: "/srv"},
		"path quotes":   {Repo: "acme/shop", Path: "a'b"},
	} {
		b := sourced()
		s := src
		b.Blocks[0].Source = &s
		if err := b.Validate(); err == nil || !strings.Contains(err.Error(), "source.") {
			t.Errorf("%s: got %v", name, err)
		}
	}
	b := sourced()
	b.Blocks[1].Source = &Source{Repo: "acme/data"} // a table has no source
	if err := b.Validate(); err == nil || !strings.Contains(err.Error(), "has no source") {
		t.Errorf("source on a table: %v", err)
	}
}

func TestSourceMakesARepositoryAndABranchOnlyPushRole(t *testing.T) {
	b := sourced()
	code, err := b.Tofu()
	if err != nil {
		t.Fatal(err)
	}
	res := resourcesOf(t, code)
	repo := res["aws_ecr_repository"]["api_src"]
	if repo["name"] != b.ImageRepoName("api") || repo["image_tag_mutability"] != "IMMUTABLE" {
		t.Errorf("repository = %v", repo)
	}
	trust := res["aws_iam_role"]["api_push"]["assume_role_policy"].(string)
	var td struct {
		Statement []struct {
			Condition map[string]map[string]any
		}
	}
	json.Unmarshal([]byte(trust), &td)
	subs := fmt.Sprint(td.Statement[0].Condition["StringLike"]["token.actions.githubusercontent.com:sub"])
	if subs != "[repo:acme/shop:ref:refs/heads/release repo:acme@*/shop@*:ref:refs/heads/release]" ||
		td.Statement[0].Condition["StringEquals"]["token.actions.githubusercontent.com:aud"] != "sts.amazonaws.com" ||
		!strings.Contains(trust, ":oidc-provider/token.actions.githubusercontent.com") {
		t.Errorf("trust must be this repository and branch only, in both subject forms: %s", trust)
	}
	// What the patterns accept and refuse, with IAM's StringLike rules (* matches any run of characters).
	match := func(pattern, s string) bool {
		re := "^" + strings.ReplaceAll(regexp.QuoteMeta(pattern), `\*`, ".*") + "$"
		return regexp.MustCompile(re).MatchString(s)
	}
	pats := githubSubjects("acme/shop", "release")
	matchesAny := func(s string) bool { return match(pats[0], s) || match(pats[1], s) }
	for s, want := range map[string]bool{
		"repo:acme/shop:ref:refs/heads/release":                              true,
		"repo:acme@1080213/shop@1455134797:ref:refs/heads/release":           true,
		"repo:acme@1/shop@2:ref:refs/heads/main":                             false, // another branch
		"repo:evil@1/shop@2:ref:refs/heads/release":                          false, // another owner
		"repo:acme@1/shop2@2:ref:refs/heads/release":                         false, // another repository
		"repo:acme-evil@1/shop@2:ref:refs/heads/release":                     false,
		"repo:acme@1/shop@2:pull_request":                                    false,
		"repo:acme@1/other@2:ref:refs/heads/x/shop@3:ref:refs/heads/release": true, // needs ":" in a branch name, which git refuses
	} {
		if matchesAny(s) != want {
			t.Errorf("subject %q: matched %v, want %v", s, !want, want)
		}
	}
	if !strings.Contains(res["aws_iam_role"]["support_push"]["assume_role_policy"].(string), "repo:acme@*/support-agent@*:ref:refs/heads/main") {
		t.Error("a source without a branch means main")
	}
	pol := res["aws_iam_role_policy"]["api_push"]["policy"].(string)
	if !strings.Contains(pol, "ecr:PutImage") || strings.Contains(pol, "ecr:DeleteRepository") || strings.Contains(pol, `"ecr:*"`) {
		t.Errorf("push policy = %s", pol)
	}
	known := strings.ReplaceAll(pol, "${aws_ecr_repository.api_src.arn}", "arn:aws:ecr:ap-south-1:111122223333:repository/x")
	show, _ := json.Marshal(map[string]any{"resource_changes": []any{map[string]any{"address": "aws_iam_role_policy.api_push", "mode": "managed", "type": "aws_iam_role_policy",
		"change": map[string]any{"actions": []string{"create"}, "after": map[string]any{"policy": known}, "after_unknown": map[string]any{}}}}})
	if fs, _ := guardrail.Check(show, nil); guardrail.Denied(fs) {
		t.Errorf("the push policy breaks a guardrail: %+v", fs)
	}
	// The provider is named, not looked up: a lookup needs iam:ListOpenIDConnectProviders, which builder roles lack.
	var doc struct {
		Data map[string]any `json:"data"`
	}
	json.Unmarshal(code, &doc)
	if _, ok := doc.Data["aws_iam_openid_connect_provider"]; ok {
		t.Error("the OIDC provider must not be read with a data source")
	}
	// Removing what the blueprint built may remove the images too: they are built again from the repository.
	if repo["force_delete"] != true {
		t.Error("the image repository should be removable")
	}
}

func TestDeliverySuggestion(t *testing.T) {
	b := sourced()
	d, err := b.SuggestDelivery("support", "111122223333")
	if err != nil {
		t.Fatal(err)
	}
	if d.Platform != "linux/arm64" || !strings.Contains(d.Workflow, "platforms: linux/arm64") || !strings.Contains(d.Workflow, "setup-qemu-action") {
		t.Errorf("an agent builds for arm64:\n%s", d.Workflow)
	}
	if !strings.Contains(d.Workflow, "role-to-assume: arn:aws:iam::111122223333:role/"+b.PushRoleName("support")) || !strings.Contains(d.Workflow, "id-token: write") {
		t.Errorf("workflow:\n%s", d.Workflow)
	}
	// Every action is pinned to a full commit SHA, never a tag.
	uses := regexp.MustCompile(`uses: (\S+)`).FindAllStringSubmatch(d.Workflow, -1)
	if len(uses) < 5 {
		t.Fatalf("uses = %v", uses)
	}
	for _, u := range uses {
		if !regexp.MustCompile(`^[a-z0-9-]+/[a-z0-9-]+@[0-9a-f]{40}$`).MatchString(u[1]) {
			t.Errorf("not pinned to a SHA: %s", u[1])
		}
	}
	a, _ := b.SuggestDelivery("api", "")
	if a.Platform != "linux/amd64" || strings.Contains(a.Workflow, "qemu") || !strings.Contains(a.Workflow, "context: services/api") ||
		!strings.Contains(a.Workflow, "branches: [release]") || !strings.Contains(a.Workflow, "paths: ['services/api/**']") || !strings.Contains(a.Next, "<account-id>") {
		t.Errorf("app delivery:\n%s\n%s", a.Workflow, a.Next)
	}
	if !strings.Contains(a.Pipeline, "repo: acme/shop") || !strings.Contains(a.Pipeline, "ref: release") || !strings.Contains(a.Pipeline, "workflow: forgeline-api.yml") {
		t.Errorf("pipeline:\n%s", a.Pipeline)
	}
	// Both files are what they claim: the workflow is YAML with the job, and the pipeline passes Forgeline's own loader.
	var wf struct {
		Jobs map[string]struct {
			Steps []map[string]any `json:"steps"`
		} `json:"jobs"`
	}
	if err := yaml.Unmarshal([]byte(a.Workflow), &wf); err != nil || len(wf.Jobs["image"].Steps) < 5 {
		t.Errorf("workflow is not the expected YAML: %v %+v", err, wf)
	}
	if _, err := pipeline.Load(strings.NewReader(a.Pipeline)); err != nil {
		t.Errorf("the suggested pipeline does not load: %v", err)
	}
	if _, err := b.SuggestDelivery("tickets", ""); err == nil {
		t.Error("a block without a source has no delivery")
	}

	// Built by Forgeline: a forgeline.yaml whose build step pushes to exactly the push rule's target.
	n, _ := b.SuggestDelivery("api", "111122223333")
	p, err := pipeline.Load(strings.NewReader(n.Native.Pipeline))
	if err != nil {
		t.Fatalf("the native pipeline does not load: %v\n%s", err, n.Native.Pipeline)
	}
	bs := p.Steps[0].Build
	if bs == nil || bs.Context != "services/api" || bs.Platform != "linux/amd64" || bs.Push != n.Native.RuleTarget || bs.Push != n.ImageRepo ||
		!strings.HasPrefix(bs.Push, "111122223333.dkr.ecr.") || len(bs.Tags) != 0 {
		t.Errorf("build step = %+v, rule target %q", bs, n.Native.RuleTarget)
	}
	if n.Native.RuleRepo != "acme/shop" || len(n.Native.RuleBranches) != 1 || n.Native.RuleBranches[0] != "release" || !regexp.MustCompile(`^[a-z][a-z0-9-]{0,39}$`).MatchString(n.Native.RuleID) {
		t.Errorf("rule = %+v", n.Native)
	}
	if !strings.Contains(n.Native.Next, "QEMU") || strings.Contains(n.Native.Pipeline, "github") {
		t.Errorf("native next/pipeline:\n%s\n%s", n.Native.Next, n.Native.Pipeline)
	}
	ag, _ := b.SuggestDelivery("support", "111122223333")
	if !strings.Contains(ag.Native.Pipeline, "platform: linux/arm64") || strings.Contains(ag.Native.Next, "QEMU") {
		t.Errorf("agent native:\n%s\n%s", ag.Native.Pipeline, ag.Native.Next)
	}
	// Without the account no rule can be made yet.
	if u, _ := b.SuggestDelivery("api", ""); u.Native.RuleTarget != "" || !strings.Contains(u.Native.Next, "<account-id>") {
		t.Errorf("unknown account: %+v", u.Native)
	}
}

func TestSourceTofuValidates(t *testing.T) {
	bin := os.Getenv("FORGELINE_TOFU")
	if bin == "" {
		t.Skip("set FORGELINE_TOFU to a tofu binary to check the code with tofu validate")
	}
	code, err := sourced().Tofu()
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "main.tf.json"), code, 0o600)
	for _, args := range [][]string{{"init", "-backend=false", "-input=false"}, {"validate", "-no-color"}} {
		cmd := exec.Command(bin, args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), "TF_PLUGIN_CACHE_DIR="+os.Getenv("TF_PLUGIN_CACHE_DIR"))
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("tofu %s: %v\n%s", args[0], err, out)
		}
	}
}
