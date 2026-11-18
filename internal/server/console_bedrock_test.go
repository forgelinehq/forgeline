package server

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/forgelinehq/forgeline/internal/executor"
)

// bedrockCLI stands in for the AWS CLI: it answers bedrock-runtime converse from the files it is given, and records
// the arguments and what the files held.
type bedrockCLI struct {
	mu       sync.Mutex
	args     [][]string
	messages []string
	system   []string
	fail     string // stderr to fail with, when set
}

func (b *bedrockCLI) run(_ context.Context, name string, args ...string) ([]byte, string, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.args = append(b.args, args)
	if len(args) < 2 || !contains(args, "converse") {
		return []byte(`{"Account":"111122223333","Arn":"arn:aws:sts::111122223333:assumed-role/Admin/ada"}`), "", nil
	}
	read := func(flag string) string {
		for i, a := range args {
			if a == flag && i+1 < len(args) {
				raw, _ := os.ReadFile(strings.TrimPrefix(args[i+1], "file://"))
				return string(raw)
			}
		}
		return ""
	}
	b.messages, b.system = append(b.messages, read("--messages")), append(b.system, read("--system"))
	if b.fail != "" {
		return nil, b.fail, errors.New("exit 254")
	}
	return []byte(`{"output":{"message":{"role":"assistant","content":[{"text":"Hello from Bedrock."}]}},"usage":{"inputTokens":1000,"outputTokens":200},"stopReason":"end_turn"}`), "", nil
}

func (b *bedrockCLI) last() []string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.args[len(b.args)-1]
}

func TestBedrockProviderUsesTheAWSConnection(t *testing.T) {
	h := newHarness(t, &executor.Fake{})
	cli := &bedrockCLI{}
	h.srv.run = cli.run
	admin := h.adminBrowser("ada")
	model := map[string]any{"id": "us.anthropic.claude-haiku-5-5-v1:0", "on": true, "input_usd_per_mtok": 0.1, "output_usd_per_mtok": 0.5}
	bedrock := func(change func(map[string]any)) map[string]any {
		b := map[string]any{"id": "bedrock", "kind": provBedrock, "name": "Bedrock", "region": "us-east-1", "models": []map[string]any{model}}
		if change != nil {
			change(b)
		}
		return b
	}
	for name, change := range map[string]func(map[string]any){
		"an address":          func(b map[string]any) { b["base_url"] = "https://bedrock.example.com" },
		"a key":               func(b map[string]any) { b["api_key"] = openAIKey },
		"private network":     func(b map[string]any) { b["allow_private"] = true },
		"bad region":          func(b map[string]any) { b["region"] = "--endpoint-url=http://evil" },
		"unknown account":     func(b map[string]any) { b["aws_account"] = "nowhere" },
		"model like a flag":   func(b map[string]any) { b["models"] = []map[string]any{{"id": "--endpoint-url", "on": true}} },
		"region on anthropic": nil,
	} {
		b := bedrock(change)
		if name == "region on anthropic" {
			b = anthropicProvider("anthropic", "https://api.anthropic.com", "claude-haiku-5-5")
			b["region"] = "us-east-1"
		}
		if resp, out := admin.do("POST", "/v1/providers", b); resp.StatusCode != 400 {
			t.Errorf("%s: %d %s, want 400", name, resp.StatusCode, out)
		}
	}
	admin.expect(201, "POST", "/v1/providers", bedrock(nil))

	// Not connected to AWS: a clear error, and the CLI is not asked for a model.
	resp, out := admin.do("POST", "/v1/providers/bedrock/test", nil)
	if resp.StatusCode != 502 || !strings.Contains(string(out), "not connected") || len(cli.messages) != 0 {
		t.Fatalf("not connected = %d %s", resp.StatusCode, out)
	}

	admin.expect(200, "PUT", "/v1/integrations/aws", map[string]any{"enabled": true, "profile": "dev", "region": "ap-south-1"})
	body := agentBody("Cloud", nil, nil, "T0")
	body["model"] = "bedrock/us.anthropic.claude-haiku-5-5-v1:0"
	a := newAgent(t, admin, body)
	var reply ChatReply
	json.Unmarshal(admin.expect(200, "POST", "/v1/ai/agents/"+a.ID+"/chat", map[string]any{"message": "secret-free question"}), &reply)
	args := strings.Join(cli.last(), " ")
	if !strings.Contains(reply.Reply, "Hello from Bedrock.") || !strings.Contains(args, "bedrock-runtime converse --model-id us.anthropic.claude-haiku-5-5-v1:0") ||
		!strings.Contains(args, "--profile dev") || !strings.Contains(args, "--region us-east-1") || strings.Contains(args, "secret-free question") {
		t.Fatalf("reply = %+v, args %s", reply, args)
	}
	// The prompt went in files, not on the command line, and the files are gone.
	if !strings.Contains(cli.messages[0], "secret-free question") || cli.system[0] == "" {
		t.Fatalf("messages = %q, system = %q", cli.messages[0], cli.system[0])
	}
	for _, a := range cli.last() {
		if f, ok := strings.CutPrefix(a, "file://"); ok {
			if _, err := os.Stat(f); err == nil {
				t.Errorf("%s was left behind", f)
			}
		}
	}
	var calls struct {
		Calls []ModelCall `json:"calls"`
	}
	json.Unmarshal(admin.expect(200, "GET", "/v1/model-calls?provider=bedrock", nil), &calls)
	if len(calls.Calls) == 0 || calls.Calls[0].CostUSD <= 0 || calls.Calls[0].Who != "Cloud" {
		t.Fatalf("calls = %+v", calls.Calls)
	}
	var list struct {
		Providers []ProviderJSON `json:"providers"`
	}
	json.Unmarshal(admin.expect(200, "GET", "/v1/providers", nil), &list)
	if p := list.Providers[0]; p.Status != "working" || p.HasKey {
		t.Fatalf("provider = %+v", p)
	}
}

func TestBedrockThrottlingFallsBackButADenialDoesNot(t *testing.T) {
	h := newHarness(t, &executor.Fake{})
	cli := &bedrockCLI{}
	h.srv.run = cli.run
	admin := h.adminBrowser("ada")
	admin.expect(200, "PUT", "/v1/integrations/aws", map[string]any{"enabled": true, "profile": "dev", "region": "ap-south-1"})
	backup := newFakeProvider(t, "from backup")
	admin.expect(201, "POST", "/v1/providers", anthropicProvider("backup", backup.srv.URL, "claude-haiku-5-5"))
	admin.expect(201, "POST", "/v1/providers", map[string]any{"id": "bedrock", "kind": provBedrock, "name": "Bedrock", "fallback": "backup/claude-haiku-5-5",
		"models": []map[string]any{{"id": "amazon.nova-lite-v1:0", "on": true}}})
	body := agentBody("Cloud", nil, nil, "T0")
	body["model"] = "bedrock/amazon.nova-lite-v1:0"
	a := newAgent(t, admin, body)

	cli.fail = "An error occurred (ThrottlingException) when calling the Converse operation: Too many requests"
	if resp, out := admin.do("POST", "/v1/ai/agents/"+a.ID+"/chat", map[string]any{"message": "go"}); resp.StatusCode != 200 || !strings.Contains(string(out), "from backup") {
		t.Fatalf("throttled = %d %s", resp.StatusCode, out)
	}
	cli.fail = "An error occurred (AccessDeniedException) when calling the Converse operation: not authorized to perform bedrock:InvokeModel"
	before := backup.calls()
	if resp, out := admin.do("POST", "/v1/ai/agents/"+a.ID+"/chat", map[string]any{"message": "go"}); resp.StatusCode == 200 || backup.calls() != before || !strings.Contains(string(out), "denied") {
		t.Fatalf("denied = %d %s", resp.StatusCode, out)
	}
}
