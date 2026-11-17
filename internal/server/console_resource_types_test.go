package server

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

// fakeSchema answers `tofu providers schema -json` with real schemas of a few AWS resource types.
func fakeSchema(t *testing.T, calls *atomic.Int32) schemaRunner {
	raw, err := os.ReadFile("../blueprint/testdata/aws-schema-part.json")
	if err != nil {
		t.Fatal(err)
	}
	return func(context.Context) ([]byte, string, error) {
		calls.Add(1)
		return raw, "6.72.0", nil
	}
}

func resourceBP(attrs map[string]any) map[string]any {
	return map[string]any{"name": "Alerts", "target": map[string]any{"cloud": "aws", "account": "primary", "region": "ap-south-1"},
		"blocks": []map[string]any{
			{"id": "jobs", "kind": "queue", "name": "Jobs"},
			{"id": "alerts", "kind": "resource", "name": "Alerts", "resource_type": "aws_sns_topic"},
			{"id": "sub", "kind": "resource", "name": "Sub", "resource_type": "aws_sns_topic_subscription", "attributes": attrs},
		},
		"links": []map[string]any{
			{"from": "sub", "to": "alerts", "attribute": "topic_arn", "output": "arn"},
			{"from": "sub", "to": "jobs", "attribute": "endpoint", "output": "arn"},
		}}
}

func TestResourceTypeSettings(t *testing.T) {
	m := newMCPHarness(t)
	var calls atomic.Int32
	m.srv.schemaRun = fakeSchema(t, &calls)
	m.dev.expect(403, "PUT", "/v1/settings/resource-types", map[string]any{"accounts": map[string]any{"primary": []string{"aws_sns_topic"}}})
	for want, accts := range map[string]map[string]any{
		"accounts.staging: no such AWS account":             {"staging": []string{"aws_sns_topic"}},
		"accounts.primary[1]: aws_iam_access_key is never":  {"primary": []string{"aws_sns_topic", "aws_iam_access_key"}},
		"accounts.primary[0]: aws_iam_user_policy is never": {"primary": []string{"aws_iam_user_policy"}},
		`accounts.primary[0]: the AWS provider has no resource`: {"primary": []string{"aws_sns_topicz"}},
	} {
		if out := string(m.admin.expect(400, "PUT", "/v1/settings/resource-types", map[string]any{"accounts": accts})); !strings.Contains(out, want) {
			t.Errorf("want %q, got %s", want, out)
		}
	}
	m.admin.expect(200, "PUT", "/v1/settings/resource-types", map[string]any{"accounts": map[string]any{"primary": []string{"aws_sns_topic_subscription", "aws_sns_topic", "aws_sns_topic"}}})
	var rt resourceTypes
	json.Unmarshal(m.dev.expect(200, "GET", "/v1/settings/resource-types", nil), &rt)
	if strings.Join(rt.Accounts["primary"], ",") != "aws_sns_topic,aws_sns_topic_subscription" {
		t.Errorf("saved: %v", rt.Accounts)
	}
	m.admin.expect(200, "PUT", "/v1/settings/resource-types", map[string]any{"accounts": map[string]any{"primary": []string{"aws_sns_topic", "aws_ssm_parameter"}}})
	if a := string(m.admin.expect(200, "GET", "/v1/audit?action=blueprint.resource", nil)); !strings.Contains(a, "primary allows aws_ssm_parameter; primary no longer allows aws_sns_topic_subscription") {
		t.Errorf("audit: %s", a)
	}

	// The schema is made once and kept: a fresh server reads the file.
	if calls.Load() != 1 {
		t.Errorf("the schema was made %d times", calls.Load())
	}
	m.srv.schemas.val = nil
	m.dev.expect(200, "GET", "/v1/resource-types/aws_sns_topic", nil)
	if calls.Load() != 1 {
		t.Errorf("the kept schema was not read back: made %d times", calls.Load())
	}
	if _, err := os.Stat(filepath.Join(m.srv.cfg.DataDir, "tools", schemaFile)); err != nil {
		t.Error(err)
	}

	// Search and describe.
	var found struct{ Types []ResourceTypeJSON }
	json.Unmarshal(m.dev.expect(200, "GET", "/v1/resource-types/search?q=sns%20topic", nil), &found)
	if len(found.Types) < 2 || found.Types[0].Type != "aws_sns_topic" || found.Types[0].Label != "SNS topic" {
		t.Errorf("search: %+v", found.Types)
	}
	var desc struct {
		Fields  []ResourceFieldJSON
		Outputs []string
	}
	json.Unmarshal(m.dev.expect(200, "GET", "/v1/resource-types/aws_ssm_parameter", nil), &desc)
	by := map[string]ResourceFieldJSON{}
	for _, f := range desc.Fields {
		by[f.Name] = f
	}
	if !desc.Fields[0].Required || by["value"].Settable || by["value"].Why != "holds a secret" || by["value_wo"].Settable || by["version"].Why != "set by AWS" || by["region"].Settable || by["region"].Why != "the blueprint's region" ||
		!by["insecure_value"].Settable || by["tags"].Kind != "map" || by["name"].Kind != "string" {
		t.Errorf("fields: %+v", desc.Fields)
	}
	m.dev.expect(404, "GET", "/v1/resource-types/aws_nothing", nil)
}

// A blueprint with resource blocks is checked against the allow list and the schema when it is saved, checked and
// previewed; one without them never needs the schema.
func TestResourceBlocksInBlueprints(t *testing.T) {
	m := newMCPHarness(t)
	var calls atomic.Int32
	m.srv.schemaRun = fakeSchema(t, &calls)
	m.srv.run = (&awsStub{}).run
	m.srv.tofuRun = (&fakeTofu{}).run
	m.admin.expect(200, "PUT", "/v1/integrations/aws", map[string]any{"enabled": true, "profile": "dev", "region": "ap-south-1"})

	m.dev.expect(201, "POST", "/v1/blueprints", bpBody("Shop"))
	if calls.Load() != 0 {
		t.Errorf("a blueprint without resource blocks made the schema")
	}
	good := map[string]any{"protocol": "sqs"}
	if out := string(m.dev.expect(400, "POST", "/v1/blueprints", resourceBP(good))); !strings.Contains(out, "aws_sns_topic is not allowed in this account") {
		t.Errorf("a type nobody allowed was saved: %s", out)
	}
	m.admin.expect(200, "PUT", "/v1/settings/resource-types", map[string]any{"accounts": map[string]any{"primary": []string{"aws_sns_topic", "aws_sns_topic_subscription"}}})
	for want, attrs := range map[string]map[string]any{
		"protocol: it is required":             {},
		"subscription_role_arn: give a string": {"protocol": "sqs", "subscription_role_arn": 7},
		"arn: it cannot be set: AWS sets it":   {"protocol": "sqs", "arn": "x"},
	} {
		if out := string(m.dev.expect(400, "POST", "/v1/blueprints", resourceBP(attrs))); !strings.Contains(out, want) {
			t.Errorf("want %q, got %s", want, out)
		}
	}
	var chk map[string]any
	json.Unmarshal(m.dev.expect(200, "POST", "/v1/blueprints/check", resourceBP(map[string]any{})), &chk)
	if !strings.Contains(chk["error"].(string), "protocol: it is required") {
		t.Errorf("check: %v", chk)
	}
	var made BlueprintJSON
	json.Unmarshal(m.dev.expect(201, "POST", "/v1/blueprints", resourceBP(good)), &made)
	if code := string(m.dev.expect(200, "GET", "/v1/blueprints/"+made.ID+"/code", nil)); !strings.Contains(code, `"topic_arn": "${aws_sns_topic.alerts.arn}"`) {
		t.Errorf("code: %s", code)
	}

	// Taken off the allow list: the blueprint can no longer be previewed for building.
	m.admin.expect(200, "PUT", "/v1/settings/resource-types", map[string]any{"accounts": map[string]any{"primary": []string{"aws_sns_topic"}}})
	if out := string(m.dev.expect(409, "POST", "/v1/blueprints/"+made.ID+"/plan", nil)); !strings.Contains(out, "aws_sns_topic_subscription is not allowed") {
		t.Errorf("a type taken off the list was previewed: %s", out)
	}
}
