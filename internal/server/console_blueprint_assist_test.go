package server

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/forgelinehq/forgeline/internal/authn"
)

// A draft the model sends back: the shop plus a queue the app reads.
const withQueue = `I added a queue for jobs and let the app read it.
<blueprint>{"blocks": [
 {"id": "web", "kind": "frontdoor", "name": "Front door"},
 {"id": "app", "kind": "service", "name": "App", "image": "nginx:1.29", "cpu": 256, "memory": 512, "count": 1, "port": 80, "env": {"GREETING": "hi ${x}"}},
 {"id": "data", "kind": "table", "name": "Data", "partition_key": "id", "partition_type": "S"},
 {"id": "jobs", "kind": "queue", "name": "Jobs", "retention_days": 4}],
 "links": [{"from": "web", "to": "app"}, {"from": "app", "to": "data", "access": "readwrite"}, {"from": "app", "to": "jobs", "access": "read"}]}</blueprint>`

func assistSetup(t *testing.T, replies ...string) (*scriptedLLM, *browser, *browser, *browser, string) {
	t.Helper()
	llm := newScriptedLLM(t, replies...)
	h, admin, dev, _ := assistantHarness(t, llm, "x")
	admin.createUser("viewer", authn.Viewer)
	h.srv.run = (&awsStub{}).run
	admin.expect(200, "PUT", "/v1/integrations/aws", map[string]any{"enabled": true, "profile": "dev", "region": "ap-south-1"})
	body := bpBody("Shop")
	blocks := body["blocks"].([]map[string]any)
	blocks[1]["x"], blocks[1]["y"] = 333, 44
	var made BlueprintJSON
	json.Unmarshal(dev.expect(201, "POST", "/v1/blueprints", body), &made)
	return llm, admin, dev, h.login("viewer"), made.ID
}

func draftOf(name string) map[string]any {
	b := bpBody(name)
	b["blocks"].([]map[string]any)[1]["x"] = 333
	b["blocks"].([]map[string]any)[1]["y"] = 44
	return b
}

func TestBlueprintAssistantSuggestsACheckedDraft(t *testing.T) {
	llm, _, dev, viewer, id := assistSetup(t, withQueue)
	viewer.expect(403, "POST", "/v1/blueprints/"+id+"/assist", map[string]any{"message": "add a queue"})
	d := draftOf("Shop")
	d["target"] = map[string]any{"cloud": "aws", "account": "elsewhere", "region": "us-east-1"} // ignored
	var r AssistReplyJSON
	json.Unmarshal(dev.expect(200, "POST", "/v1/blueprints/"+id+"/assist", map[string]any{"message": "add a queue the app reads", "draft": d}), &r)
	if r.Draft == nil || r.Problem != "" {
		t.Fatalf("reply = %+v", r)
	}
	if strings.Contains(r.Reply, "<blueprint>") || !strings.Contains(r.Reply, "added a queue") {
		t.Errorf("reply text: %q", r.Reply)
	}
	if r.Draft.Target.Account != "primary" || r.Draft.Target.Region != "ap-south-1" {
		t.Errorf("the target must stay the saved blueprint's: %+v", r.Draft.Target)
	}
	if got := strings.Join(r.Changes, "; "); got != "Adds Jobs (queue); Links App to Jobs (reads)" {
		t.Errorf("changes = %s", got)
	}
	for _, b := range r.Draft.Blocks {
		if b.ID == "app" && (b.X != 333 || b.Y != 44) {
			t.Errorf("the app lost its place: %+v", b)
		}
	}
	// Nothing was saved: the assistant only suggests.
	var saved BlueprintJSON
	json.Unmarshal(dev.expect(200, "GET", "/v1/blueprints/"+id, nil), &saved)
	if saved.Revision != 1 || len(saved.Blocks) != 3 {
		t.Errorf("the assistant saved something: %+v", saved)
	}
	// The model got its instructions, the draft as data, and no lookups.
	llm.mu.Lock()
	req := llm.reqs[0]
	llm.mu.Unlock()
	system := fmt.Sprint(req["system"])
	if !strings.Contains(system, "You help a person design a blueprint") || !strings.Contains(system, "<untrusted_context>") || !strings.Contains(system, `"partition_key": "id"`) || strings.Contains(system, "look things up") {
		t.Errorf("system prompt: %.600s", system)
	}
}

func TestBlueprintAssistantGetsOneRetry(t *testing.T) {
	bad := `Here it is. <blueprint>{"blocks": [{"id": "Web", "kind": "frontdoor", "name": "x"}], "links": []}</blueprint>`
	llm, _, dev, _, id := assistSetup(t, bad, withQueue)
	var r AssistReplyJSON
	json.Unmarshal(dev.expect(200, "POST", "/v1/blueprints/"+id+"/assist", map[string]any{"message": "add a queue", "draft": draftOf("Shop")}), &r)
	llm.mu.Lock()
	n, second := len(llm.reqs), fmt.Sprint(llm.reqs[len(llm.reqs)-1]["messages"])
	llm.mu.Unlock()
	if r.Draft == nil || n != 2 || !strings.Contains(second, "was not accepted: blocks[0].id") {
		t.Fatalf("retry: n=%d draft=%v msg=%.300s", n, r.Draft != nil, second)
	}

	// Twice wrong: no draft, and the reason.
	llm.mu.Lock()
	llm.replies, llm.reqs = []string{bad}, nil
	llm.mu.Unlock()
	r = AssistReplyJSON{}
	json.Unmarshal(dev.expect(200, "POST", "/v1/blueprints/"+id+"/assist", map[string]any{"message": "add a queue", "draft": draftOf("Shop")}), &r)
	if r.Draft != nil || !strings.Contains(r.Problem, "blocks[0].id") {
		t.Errorf("an invalid draft was offered: %+v", r)
	}
	// A plain answer, no draft and no retry.
	llm.mu.Lock()
	llm.replies, llm.reqs = []string{"A queue holds jobs until something reads them."}, nil
	llm.mu.Unlock()
	r = AssistReplyJSON{}
	json.Unmarshal(dev.expect(200, "POST", "/v1/blueprints/"+id+"/assist", map[string]any{"message": "what is a queue?", "draft": draftOf("Shop")}), &r)
	llm.mu.Lock()
	n = len(llm.reqs)
	llm.mu.Unlock()
	if r.Draft != nil || r.Problem != "" || n != 1 || !strings.Contains(r.Reply, "holds jobs") {
		t.Errorf("question: %+v (%d calls)", r, n)
	}
}

func TestBlueprintAssistantKeepsToTheServiceSwitches(t *testing.T) {
	llm, admin, dev, _, id := assistSetup(t, withQueue)
	admin.expect(200, "PUT", "/v1/integrations/aws/services", map[string]any{"off": []string{}, "read_only": []string{"sqs"}})
	var r AssistReplyJSON
	json.Unmarshal(dev.expect(200, "POST", "/v1/blueprints/"+id+"/assist", map[string]any{"message": "add a queue", "draft": draftOf("Shop")}), &r)
	if r.Draft != nil || !strings.Contains(r.Problem, "look only") {
		t.Errorf("a block on a look-only service was offered: %+v", r)
	}
	llm.mu.Lock()
	system := fmt.Sprint(llm.reqs[0]["system"])
	llm.mu.Unlock()
	if !strings.Contains(system, "sqs: look only") {
		t.Errorf("the model was not told: %.300s", system)
	}
	// Turned off: removing a block on that service is refused too.
	admin.expect(200, "PUT", "/v1/integrations/aws/services", map[string]any{"off": []string{"dynamodb"}, "read_only": []string{}})
	llm.mu.Lock()
	llm.replies = []string{`Removed the table. <blueprint>{"blocks": [{"id": "web", "kind": "frontdoor", "name": "Front door"}, {"id": "app", "kind": "service", "name": "App", "image": "nginx:1.29", "cpu": 256, "memory": 512, "count": 1, "port": 80, "env": {"GREETING": "hi ${x}"}}], "links": [{"from": "web", "to": "app"}]}</blueprint>`}
	llm.mu.Unlock()
	r = AssistReplyJSON{}
	json.Unmarshal(dev.expect(200, "POST", "/v1/blueprints/"+id+"/assist", map[string]any{"message": "remove the table", "draft": draftOf("Shop")}), &r)
	if r.Draft != nil || !strings.Contains(r.Problem, "may not remove Data") {
		t.Errorf("a block on a switched-off service was removed: %+v", r)
	}
	// With the assistant off, it says so.
	admin.expect(200, "PUT", "/v1/integrations/assistant", map[string]any{"backend": "off"})
	dev.expect(409, "POST", "/v1/blueprints/"+id+"/assist", map[string]any{"message": "add a queue", "draft": draftOf("Shop")})
}
