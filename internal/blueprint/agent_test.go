package blueprint

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/forgelinehq/forgeline/internal/guardrail"
)

const agentImage = "111122223333.dkr.ecr.ap-south-1.amazonaws.com/support-agent:1.4"

func agentStack() Blueprint {
	return Blueprint{
		ID: "bp-agent", Name: "Support agent", Target: Target{Cloud: "aws", Account: "primary", Region: "ap-south-1"},
		Blocks: []Block{
			{ID: "support", Kind: KindAgent, Name: "Support agent", Image: agentImage, Env: map[string]string{"MODEL_ID": "anthropic.claude-sonnet"}},
			{ID: "tickets", Kind: KindTable, Name: "Tickets", PartitionKey: "id", PartitionType: "S"},
			{ID: "docs", Kind: KindBucket, Name: "Docs"},
			{ID: "api-key", Kind: KindSecret, Name: "Helpdesk API key"},
		},
		Links: []Link{
			{From: "support", To: "tickets", Access: AccessReadWrite},
			{From: "support", To: "docs", Access: AccessRead},
			{From: "support", To: "api-key"},
		},
	}
}

func TestAgentValidates(t *testing.T) {
	if err := agentStack().Validate(); err != nil {
		t.Fatalf("valid: %v", err)
	}
	for name, c := range map[string]struct {
		mut  func(*Blueprint)
		want string
	}{
		"docker hub image":   {func(b *Blueprint) { b.Blocks[0].Image = "python:3.13" }, "ECR only"},
		"other region":       {func(b *Blueprint) { b.Blocks[0].Image = strings.Replace(agentImage, "ap-south-1", "us-east-1", 1) }, "its own region"},
		"no access to table": {func(b *Blueprint) { b.Links[0].Access = "" }, "read or readwrite"},
		"access on a secret": {func(b *Blueprint) { b.Links[2].Access = AccessRead }, "leave access out"},
		"port on an agent":   {func(b *Blueprint) { b.Blocks[0].Port = 8080 }, "has no port"},
		"agent to a database": {func(b *Blueprint) {
			b.Blocks = append(b.Blocks, Block{ID: "db", Kind: KindDatabase, Name: "DB", Engine: "mysql", Size: "small", StorageGB: 20})
			b.Links = append(b.Links, Link{From: "support", To: "db"})
		}, "cannot link"},
		"bad env": {func(b *Blueprint) { b.Blocks[0].Env = map[string]string{"lower": "x"} }, "upper-case"},
	} {
		b := agentStack()
		c.mut(&b)
		if err := b.Validate(); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: got %v, want %q", name, err, c.want)
		}
	}
}

func TestAgentCode(t *testing.T) {
	code, err := agentStack().Tofu()
	if err != nil {
		t.Fatal(err)
	}
	res := resourcesOf(t, code)
	rt := res["aws_bedrockagentcore_agent_runtime"]["support"]
	if rt == nil {
		t.Fatalf("no runtime in %v", res)
	}
	if rt["agent_runtime_name"] != "fl_"+shortOf("bp-agent")+"_support" {
		t.Errorf("name = %v", rt["agent_runtime_name"])
	}
	art, _ := json.Marshal(rt["agent_runtime_artifact"])
	if !strings.Contains(string(art), agentImage) {
		t.Errorf("artifact = %s", art)
	}
	if nc, _ := json.Marshal(rt["network_configuration"]); !strings.Contains(string(nc), "PUBLIC") {
		t.Errorf("network = %s", nc)
	}
	env, _ := rt["environment_variables"].(map[string]any)
	if env["MODEL_ID"] == nil || env["TABLE_TICKETS"] == nil || env["BUCKET_DOCS"] == nil || env["API_KEY_SECRET_ARN"] == nil {
		t.Errorf("env = %v", env)
	}
	trust := res["aws_iam_role"]["support_agent"]["assume_role_policy"].(string)
	if !strings.Contains(trust, "bedrock-agentcore.amazonaws.com") || !strings.Contains(trust, "aws:SourceAccount") {
		t.Errorf("trust = %s", trust)
	}
	pol := res["aws_iam_role_policy"]["support_agent"]["policy"].(string)
	for _, want := range []string{"repository/support-agent", "bedrock:InvokeModel", "dynamodb:PutItem", "s3:GetObject", "secretsmanager:GetSecretValue", "GetWorkloadAccessTokenForJWT"} {
		if !strings.Contains(pol, want) {
			t.Errorf("policy misses %s", want)
		}
	}
	for _, never := range []string{"GetWorkloadAccessTokenForUserId", "s3:PutObject", "ecr:*", "\"Action\":\"*\""} {
		if strings.Contains(pol, never) {
			t.Errorf("policy must not grant %s", never)
		}
	}
	// The policy passes the guardrails once the account id is known (as it is at plan time).
	known := strings.ReplaceAll(pol, "${data.aws_caller_identity.current.account_id}", "111122223333")
	for _, r := range []string{"aws_dynamodb_table.tickets.arn", "aws_s3_bucket.docs.arn", "aws_secretsmanager_secret.api-key.arn"} {
		known = strings.ReplaceAll(known, "${"+r+"}", "arn:aws:x:ap-south-1:111122223333:"+r)
	}
	show, _ := json.Marshal(map[string]any{"resource_changes": []any{map[string]any{"address": "aws_iam_role_policy.support_agent", "mode": "managed", "type": "aws_iam_role_policy",
		"change": map[string]any{"actions": []string{"create"}, "after": map[string]any{"policy": known}, "after_unknown": map[string]any{}}}}})
	fs, err := guardrail.Check(show, nil)
	if err != nil {
		t.Fatal(err)
	}
	if guardrail.Denied(fs) {
		t.Errorf("the agent's own policy breaks a guardrail: %+v", fs)
	}
	if _, ok := resourcesOf(t, code)["aws_lb"]; ok {
		t.Error("an agent needs no load balancer")
	}
}

// shortOf is the six hex characters names carry for a blueprint id.
func shortOf(id string) string {
	sum := sha256.Sum256([]byte(id))
	return hex.EncodeToString(sum[:])[:6]
}

func TestAgentTofuValidates(t *testing.T) {
	bin := os.Getenv("FORGELINE_TOFU")
	if bin == "" {
		t.Skip("set FORGELINE_TOFU to a tofu binary to check the code with tofu validate")
	}
	code, err := agentStack().Tofu()
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
