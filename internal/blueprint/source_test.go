package blueprint

import (
	"encoding/json"
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
	if !strings.Contains(trust, `"repo:acme/shop:ref:refs/heads/release"`) || !strings.Contains(trust, "sts.amazonaws.com") || strings.Contains(trust, "*") ||
		!strings.Contains(trust, ":oidc-provider/token.actions.githubusercontent.com") {
		t.Errorf("trust must be this repository and branch only: %s", trust)
	}
	if !strings.Contains(res["aws_iam_role"]["support_push"]["assume_role_policy"].(string), "repo:acme/support-agent:ref:refs/heads/main") {
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
