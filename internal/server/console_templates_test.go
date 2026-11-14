package server

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/forgelinehq/forgeline/internal/template"
)

func jobsBlueprintBody() map[string]any {
	return map[string]any{
		"name": "Jobs", "target": map[string]any{"cloud": "aws", "account": "primary", "region": "ap-south-1"},
		"blocks": []map[string]any{
			{"id": "jobs", "kind": "template", "name": "Nightly jobs", "template": "aws-lambda-function@1.0.0",
				"inputs": map[string]any{"image": "111122223333.dkr.ecr.ap-south-1.amazonaws.com/fl-x-jobs:1", "schedule": "rate(1 day)"}},
			{"id": "inbox", "kind": "queue", "name": "Inbox", "retention_days": 4},
		},
		"links": []map[string]any{{"from": "inbox", "to": "jobs"}},
	}
}

func TestTemplatesAPI(t *testing.T) {
	m := newMCPHarness(t)
	var list struct{ Templates []TemplateJSON }
	json.Unmarshal(m.dev.expect(200, "GET", "/v1/templates", nil), &list)
	if len(list.Templates) == 0 || list.Templates[0].Ref != "aws-lambda-function@1.0.0" || list.Templates[0].Order[0] != "image" || len(list.Templates[0].Links) != 3 {
		t.Fatalf("templates = %+v", list.Templates)
	}
	m.v.expect(403, "GET", "/v1/templates", nil)
	code := string(m.dev.expect(200, "GET", "/v1/templates/aws-lambda-function/1.0.0/code", nil))
	if !strings.Contains(code, "aws_lambda_function") {
		t.Errorf("code = %.200s", code)
	}
	m.dev.expect(404, "GET", "/v1/templates/aws-lambda-function/9.9.9/code", nil)

	// The blueprint API refuses a template block that does not fit the template.
	bad := jobsBlueprintBody()
	bad["blocks"].([]map[string]any)[0]["inputs"] = map[string]any{"image": "nginx:latest"}
	if msg := string(m.dev.expect(400, "POST", "/v1/blueprints", bad)); !strings.Contains(msg, "blocks[0].inputs") {
		t.Errorf("a bad input: %s", msg)
	}
	m.dev.expect(201, "POST", "/v1/blueprints", jobsBlueprintBody())
}

func TestTemplateModulesTravelWithThePlan(t *testing.T) {
	m := newMCPHarness(t)
	m.srv.run = (&awsStub{}).run
	tf := &fakeTofu{}
	// Record whether the module file was in the work directory when tofu planned and applied.
	var mu sync.Mutex
	seen := map[string]string{}
	m.srv.tofuRun = func(ctx context.Context, dir string, env []string, args ...string) ([]byte, string, error) {
		if args[0] == "plan" || args[0] == "apply" {
			b, _ := os.ReadFile(filepath.Join(dir, "templates", "aws-lambda-function", "1.0.0", "main.tf.json"))
			mu.Lock()
			seen[args[0]] = string(b)
			mu.Unlock()
		}
		return tf.run(ctx, dir, env, args...)
	}
	m.srv.cloud.setInterval(10 * time.Millisecond)
	m.admin.expect(200, "PUT", "/v1/integrations/aws", map[string]any{"enabled": true, "profile": "dev", "region": "ap-south-1"})
	var made BlueprintJSON
	json.Unmarshal(m.dev.expect(201, "POST", "/v1/blueprints", jobsBlueprintBody()), &made)
	m.dev.expect(202, "POST", "/v1/blueprints/"+made.ID+"/plan", nil)
	var p BlueprintPlan
	waitFor(t, "the preview", func() bool {
		json.Unmarshal(m.dev.expect(200, "GET", "/v1/blueprints/"+made.ID+"/plan", nil), &p)
		return p.Status != planPlanning
	})
	lambda, _ := template.Get("aws-lambda-function@1.0.0")
	mu.Lock()
	planned := seen["plan"]
	mu.Unlock()
	if p.Status != planReady || planned != string(lambda.Code) {
		t.Fatalf("plan %s; module at plan time: %d bytes", p.Status, len(planned))
	}
	var stored storedPlan
	m.srv.docs.Get(t.Context(), kindBPPlan, made.ID, &stored)
	if stored.Modules["templates/aws-lambda-function/1.0.0/main.tf.json"] != string(lambda.Code) || !strings.Contains(stored.Code, `"module"`) {
		t.Fatalf("the plan does not keep its modules: %v", stored.Modules)
	}

	var c CloudChange
	json.Unmarshal(m.dev.expect(202, "POST", "/v1/blueprints/"+made.ID+"/propose", map[string]any{"reason": "nightly jobs"}), &c)
	waitFor(t, "the proposal", func() bool {
		json.Unmarshal(m.admin.expect(200, "GET", "/v1/cloud/changes/"+c.ID, nil), &c)
		return c.Status != stPreparing
	})
	m.admin.expect(202, "POST", "/v1/cloud/changes/"+c.ID+"/approve", map[string]any{})
	waitFor(t, "the apply", func() bool {
		json.Unmarshal(m.admin.expect(200, "GET", "/v1/cloud/changes/"+c.ID, nil), &c)
		return c.Status != stExecuting
	})
	mu.Lock()
	applied := seen["apply"]
	mu.Unlock()
	if c.Status != stSucceeded || applied != string(lambda.Code) {
		t.Fatalf("apply %s (%s); module at apply time: %d bytes", c.Status, c.Result, len(applied))
	}
}

func TestWorkdirRefusesModulePathsOutsideTemplates(t *testing.T) {
	m := newMCPHarness(t)
	for _, p := range []string{"../escape/main.tf.json", "templates/../../x/main.tf.json", "/etc/main.tf.json", "templates/a/b/other.tf.json", "templates/a/main.tf.json", "main.tf.json"} {
		if dir, err := m.srv.workdir("{}", "", "", map[string]string{p: "{}"}); err == nil {
			os.RemoveAll(dir)
			t.Errorf("%s: accepted", p)
		}
	}
}
