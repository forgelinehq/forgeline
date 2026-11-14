package blueprint

import (
	"fmt"
	"strings"

	"github.com/forgelinehq/forgeline/internal/template"
)

// Delivery is what Forgeline suggests for a block with a source (note 040, step 4): a GitHub Actions workflow the
// person adds to their repository (Forgeline never pushes to it), and a Forgeline pipeline that starts it.
type Delivery struct {
	Block        string `json:"block"`
	Repo         string `json:"repo"`
	Branch       string `json:"branch"`
	WorkflowPath string `json:"workflow_path"`
	Workflow     string `json:"workflow"`
	Pipeline     string `json:"pipeline"`
	ImageRepo    string `json:"image_repository"` // where images go; the account is a placeholder until it is known
	Platform     string `json:"platform"`
	Next         string `json:"next"`
	// Native is the other way (design note 043, slice 3): Forgeline builds and pushes the image itself.
	Native NativeDelivery `json:"native"`
}

// NativeDelivery is a forgeline.yaml with a build step for the repository, and the push rule that lets it push. No
// GitHub Actions, no role for GitHub: Forgeline's own AWS connection pushes, for runs GitHub starts for a push.
type NativeDelivery struct {
	PipelinePath string   `json:"pipeline_path"`
	Pipeline     string   `json:"pipeline"`
	RuleID       string   `json:"rule_id"`
	RuleRepo     string   `json:"rule_repo"`
	RuleBranches []string `json:"rule_branches"`
	RuleTarget   string   `json:"rule_target"` // empty until the account is known
	Next         string   `json:"next"`
}

// Actions pinned to full commit SHAs, each checked against its repository (2026-11-12).
var deliveryActions = map[string]string{
	"checkout":  "actions/checkout@3d3c42e5aac5ba805825da76410c181273ba90b1 # v7.0.1",
	"awscreds":  "aws-actions/configure-aws-credentials@e1253824e5c10ff9df46874f81ed3ec929e19cfd # v6.3.0",
	"ecrlogin":  "aws-actions/amazon-ecr-login@03f1aad4c6c7ffd436567f42f9384779290529bd # v2.1.7",
	"qemu":      "docker/setup-qemu-action@99012661954931238ded8c8b007157a8430204e1 # v4.4.0",
	"buildx":    "docker/setup-buildx-action@f87e5991a6d7451dcb8d9637bfbc97413f497069 # v4.4.1",
	"buildpush": "docker/build-push-action@c3c9e263c25d99ce0380d002d59b67737d91b0dc # v7.4.0",
}

// SuggestDelivery writes the workflow and pipeline for one block. account is the 12-digit AWS account the
// blueprint builds in, or "" when it is not known yet (then a placeholder is written and said so).
func (b Blueprint) SuggestDelivery(blockID, account string) (Delivery, error) {
	var bl *Block
	for i := range b.Blocks {
		if b.Blocks[i].ID == blockID {
			bl = &b.Blocks[i]
		}
	}
	if bl == nil {
		return Delivery{}, fmt.Errorf("no block %q", blockID)
	}
	if bl.Source == nil {
		return Delivery{}, fmt.Errorf("%s has no source repository; set one first", bl.Name)
	}
	acct := account
	if acct == "" {
		acct = "<account-id>"
	}
	region := b.Target.Region
	repo := b.ImageRepoName(bl.ID)
	registry := acct + ".dkr.ecr." + region + ".amazonaws.com"
	role := "arn:aws:iam::" + acct + ":role/" + b.PushRoleName(bl.ID)
	platform := "linux/amd64" // Fargate's default
	if bl.Kind == KindAgent {
		platform = "linux/arm64" // AgentCore runs arm64 images only
	}
	if bl.Kind == KindTemplate {
		// A template that runs an image says which processor through its architecture input (Lambda: arm64 unless set).
		if t, err := template.Get(bl.Template); err == nil {
			if in, err := t.Inputs.Resolve(bl.Inputs); err == nil && in["architecture"] == "arm64" {
				platform = "linux/arm64"
			}
		}
	}
	d := Delivery{Block: bl.ID, Repo: bl.Source.Repo, Branch: bl.Source.BranchOr(), WorkflowPath: ".github/workflows/forgeline-" + bl.ID + ".yml",
		ImageRepo: registry + "/" + repo, Platform: platform}
	path := bl.Source.PathOr()
	watch := "'" + strings.TrimSuffix(path, "/") + "/**'"
	if path == "." {
		watch = "'**'"
	}
	var w strings.Builder
	fmt.Fprintf(&w, `# Builds %s's image and pushes it to its private ECR repository (made by the Forgeline blueprint %q).
# It only pushes: running the new image is a blueprint change that is previewed and approved in Forgeline.
name: forgeline-%s
on:
  workflow_dispatch:
  push:
    branches: [%s]
    paths: [%s]
permissions:
  contents: read
  id-token: write # to sign in to AWS with GitHub's OIDC token; no AWS keys are stored
concurrency: forgeline-%s
jobs:
  image:
    runs-on: ubuntu-latest
    steps:
      - uses: %s
        with:
          persist-credentials: false
      - uses: %s
        with:
          role-to-assume: %s
          aws-region: %s
      - uses: %s
`, yamlText(bl.Name), b.Name, bl.ID, d.Branch, watch, bl.ID, deliveryActions["checkout"], deliveryActions["awscreds"], role, region, deliveryActions["ecrlogin"])
	if platform == "linux/arm64" {
		fmt.Fprintf(&w, "      - uses: %s\n", deliveryActions["qemu"])
	}
	fmt.Fprintf(&w, `      - uses: %s
      - uses: %s
        with:
          context: %s
          platforms: %s
          push: true
          provenance: false
          tags: %s/%s:${{ github.sha }}
`, deliveryActions["buildx"], deliveryActions["buildpush"], path, platform, registry, repo)
	d.Workflow = w.String()
	d.Pipeline = fmt.Sprintf(`version: v1alpha1
name: build-%s
steps:
  - id: build-image
    workflow:
      repo: %s
      workflow: forgeline-%s.yml
      ref: %s
`, bl.ID, bl.Source.Repo, bl.ID, d.Branch)
	d.Next = "Add the workflow to " + bl.Source.Repo + " (a pull request someone merges). Build the blueprint so the image repository and the push role exist; " +
		"GitHub's OIDC provider (token.actions.githubusercontent.com) must already be set up in the account. After a push, set " + bl.Name +
		"'s image to " + registry + "/" + repo + ":<commit> and send the preview for approval."
	if account == "" {
		d.Next = "The AWS account is not known until the blueprint is previewed, so the workflow says <account-id>. " + d.Next
	}
	d.Native = nativeDelivery(b, *bl, registry+"/"+repo, d.Branch, platform, account != "")
	return d, nil
}

// nativeDelivery writes the forgeline.yaml that builds the block's image in Forgeline, and the push rule it needs.
func nativeDelivery(b Blueprint, bl Block, target, branch, platform string, known bool) NativeDelivery {
	n := NativeDelivery{PipelinePath: "forgeline.yaml", RuleID: ruleID(b.ID, bl.ID), RuleRepo: bl.Source.Repo, RuleBranches: []string{branch}}
	ctx := bl.Source.PathOr()
	ctxLine := ""
	if ctx != "." {
		ctxLine = "      context: " + ctx + "\n"
	}
	n.Pipeline = fmt.Sprintf(`# Builds %s's image and pushes it to its private ECR repository (made by the Forgeline blueprint %q).
# Forgeline runs it when GitHub reports a push to %s; it pushes only through the push rule %s. Pull requests build
# it without pushing. Running the new image is a blueprint change that is previewed and approved in Forgeline.
version: v1alpha1
name: build-%s
steps:
  - id: image
    build:
%s      platform: %s
      push: %s
    timeout: 30m
`, yamlText(bl.Name), b.Name, branch, n.RuleID, bl.ID, ctxLine, platform, target)
	if known {
		n.RuleTarget = target
	}
	n.Next = "Add forgeline.yaml to " + bl.Source.Repo + " (or this step to the one there), through a pull request someone merges. " +
		"An administrator allows the push (the rule below, under Settings › Image builds); GitHub must be able to reach Forgeline's webhook. " +
		"After a push to " + branch + ", set " + bl.Name + "'s image to the new tag (the commit) and send the preview for approval."
	if platform != "linux/arm64" {
		n.Next += " Building " + platform + " on an ARM server (a Raspberry Pi) needs QEMU emulation in its Docker."
	}
	if !known {
		n.Next = "The AWS account is not known until the blueprint is previewed, so the file says <account-id> and the push rule cannot be made yet. " + n.Next
	}
	return n
}

// ruleID is the push rule a blueprint block suggests: short, lower case, stable.
func ruleID(bpID, blockID string) string {
	id := strings.ToLower("bp-" + blockID + "-" + bpID)
	clean := make([]rune, 0, len(id))
	for _, r := range id {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' {
			clean = append(clean, r)
		} else {
			clean = append(clean, '-')
		}
	}
	id = strings.Trim(string(clean), "-")
	if len(id) > 40 {
		id = strings.Trim(id[:40], "-")
	}
	return id
}

// yamlText keeps a name safe inside a YAML comment line.
func yamlText(s string) string { return strings.NewReplacer("\n", " ", "\r", " ").Replace(s) }
