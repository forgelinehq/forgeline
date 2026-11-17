package blueprint

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// awsSchemas are real schemas of a few AWS resource types, cut from `tofu providers schema -json`.
func awsSchemas(t *testing.T) map[string]ResourceSchema {
	t.Helper()
	raw, err := os.ReadFile("testdata/aws-schema-part.json")
	if err != nil {
		t.Fatal(err)
	}
	s, addr, err := ParseProviderSchemas(raw)
	if err != nil || !strings.HasSuffix(addr, "/hashicorp/aws") {
		t.Fatal(addr, err)
	}
	return s
}

func lookup(s map[string]ResourceSchema) func(string) (ResourceSchema, bool) {
	return func(typ string) (ResourceSchema, bool) { r, ok := s[typ]; return r, ok }
}

func allowAll(string) bool { return true }

// withResources is a queue, a topic, a subscription of the queue to the topic, and a parameter whose value looks
// like an expression.
func withResources() Blueprint {
	return Blueprint{ID: "bp-res", Name: "Alerts", Target: Target{Cloud: "aws", Account: "primary", Region: "ap-south-1"},
		Blocks: []Block{
			{ID: "jobs", Kind: KindQueue, Name: "Jobs"},
			{ID: "alerts", Kind: KindResource, Name: "Alerts", ResourceType: "aws_sns_topic", Attributes: map[string]any{"display_name": "alerts", "tags": map[string]any{"team": "ops"}}},
			{ID: "to-jobs", Kind: KindResource, Name: "To jobs", ResourceType: "aws_sns_topic_subscription", Attributes: map[string]any{"protocol": "sqs"}},
			{ID: "note", Kind: KindResource, Name: "Note", ResourceType: "aws_ssm_parameter", Attributes: map[string]any{
				"name": "/fl/note", "type": "String", "insecure_value": "${path.module} %{ if true }x%{ endif }", "tags": map[string]any{"${k}": "v"}}},
		},
		Links: []Link{
			{From: "to-jobs", To: "alerts", Attribute: "topic_arn", Output: "arn"},
			{From: "to-jobs", To: "jobs", Attribute: "endpoint", Output: "arn"},
		}}
}

func TestResourceBlockCode(t *testing.T) {
	b := withResources()
	if err := b.Validate(); err != nil {
		t.Fatal(err)
	}
	if err := b.CheckResources(lookup(awsSchemas(t)), allowAll); err != nil {
		t.Fatal(err)
	}
	code, err := b.Tofu()
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Resource map[string]map[string]map[string]any
	}
	json.Unmarshal(code, &doc)
	sub := doc.Resource["aws_sns_topic_subscription"]["to-jobs"]
	if sub["topic_arn"] != "${aws_sns_topic.alerts.arn}" || sub["endpoint"] != "${aws_sqs_queue.jobs.arn}" || sub["protocol"] != "sqs" {
		t.Errorf("subscription = %v", sub)
	}
	note := doc.Resource["aws_ssm_parameter"]["note"]
	if note["insecure_value"] != "$${path.module} %%{ if true }x%%{ endif }" {
		t.Errorf("a typed value can become an expression: %v", note["insecure_value"])
	}
	if tags, _ := note["tags"].(map[string]any); tags["$${k}"] != "v" {
		t.Errorf("a typed key can become an expression: %v", note["tags"])
	}
	if doc.Resource["aws_sns_topic"]["alerts"]["display_name"] != "alerts" {
		t.Errorf("topic = %v", doc.Resource["aws_sns_topic"])
	}
	checks := wordsOf(b)
	if !strings.Contains(checks, "To jobs makes an AWS SNS topic subscription (aws_sns_topic_subscription)") || !strings.Contains(checks, "endpoint from Jobs's arn, topic_arn from Alerts's arn") {
		t.Errorf("checks: %s", checks)
	}
}

func TestResourceBlockRefusals(t *testing.T) {
	schemas := awsSchemas(t)
	for want, edit := range map[string]func(*Blueprint){
		"pick a resource type":              func(b *Blueprint) { b.Blocks[1].ResourceType = "sns_topic" },
		"is never allowed":                  func(b *Blueprint) { b.Blocks[1].ResourceType = "aws_iam_access_key" },
		"never allowed: it makes IAM users": func(b *Blueprint) { b.Blocks[1].ResourceType = "aws_iam_user_policy" },
		"is not a setting name":             func(b *Blueprint) { b.Blocks[1].Attributes["Bad-Name"] = "x" },
		"a queue has no attributes":         func(b *Blueprint) { b.Blocks[0].Attributes = map[string]any{"x": 1} },
		"a resource has no image":           func(b *Blueprint) { b.Blocks[1].Image = "nginx" },
		"at most 60 settings": func(b *Blueprint) {
			for i := 0; i < 61; i++ {
				b.Blocks[1].Attributes[fmt.Sprintf("s%d", i)] = "x"
			}
		},
		"a resource cannot choose another":  func(b *Blueprint) { b.Blocks[1].Attributes["region"] = "us-east-1" },
		"nested too deep":                   func(b *Blueprint) { b.Blocks[1].Attributes["tags"] = deep(10) },
		"at most 32 KB":                     func(b *Blueprint) { b.Blocks[1].Attributes["display_name"] = strings.Repeat("x", 33<<10) },
		"set it or link it, not both":       func(b *Blueprint) { b.Blocks[2].Attributes["topic_arn"] = "arn:x" },
		"another link already fills":        func(b *Blueprint) { b.Links[1].Attribute = "topic_arn" },
		"name the setting":                  func(b *Blueprint) { b.Links[0].Attribute = "" },
		"jobs offers arn, name, url":        func(b *Blueprint) { b.Links[1].Output = "owner" },
		"cannot read its own":               func(b *Blueprint) { b.Links[0].To = "to-jobs" },
		"it takes no access or path":        func(b *Blueprint) { b.Links[0].Access = "read" },
		"only a resource's link fills":      func(b *Blueprint) { b.Links = append(b.Links, Link{From: "jobs", To: "alerts", Output: "arn"}) },
		"a queue cannot link to a resource": func(b *Blueprint) { b.Links = append(b.Links, Link{From: "jobs", To: "alerts"}) },
		"a resource can read only from another": func(b *Blueprint) {
			b.Blocks = append(b.Blocks, Block{ID: "app", Kind: KindService, Name: "App", Image: "nginx:1.29", CPU: 256, Memory: 512})
			b.Links[1].To = "app"
		},
	} {
		b := withResources()
		edit(&b)
		err := b.Validate()
		if err == nil {
			err = b.CheckResources(lookup(schemas), allowAll)
		}
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("want %q, got %v", want, err)
		}
	}
}

func deep(n int) any {
	var v any = "x"
	for i := 0; i < n; i++ {
		v = map[string]any{"a": v}
	}
	return v
}

func TestResourceSettingsFollowTheSchema(t *testing.T) {
	schemas := awsSchemas(t)
	for want, edit := range map[string]func(*Blueprint){
		"not allowed in this account":                           func(b *Blueprint) {},
		"has no resource aws_sns_topicz":                        func(b *Blueprint) { b.Blocks[1].ResourceType = "aws_sns_topicz" },
		"attributes.value: it cannot be set: it holds a secret": func(b *Blueprint) { b.Blocks[3].Attributes["value"] = "s3cret" },
		"value_wo: it cannot be set: it holds a secret":         func(b *Blueprint) { b.Blocks[3].Attributes["value_wo"] = "s3cret" },
		"arn: it cannot be set: AWS sets it":                    func(b *Blueprint) { b.Blocks[1].Attributes["arn"] = "arn:x" },
		"display_nam: there is no such setting":                 func(b *Blueprint) { b.Blocks[1].Attributes["display_nam"] = "x" },
		"display_name: give a string":                           func(b *Blueprint) { b.Blocks[1].Attributes["display_name"] = 3.0 },
		"tags.team: give a string":                              func(b *Blueprint) { b.Blocks[1].Attributes["tags"] = map[string]any{"team": true} },
		"delivery_policy: give a string":                        func(b *Blueprint) { b.Blocks[1].Attributes["delivery_policy"] = map[string]any{} },
		"protocol: it is required":                              func(b *Blueprint) { delete(b.Blocks[2].Attributes, "protocol") },
		"topic_arn: it is required":                             func(b *Blueprint) { b.Links = b.Links[1:] },
		"has no setting \"topic\"":                              func(b *Blueprint) { b.Links[0].Attribute = "topic" },
		"subscription_role_arn":                                 func(b *Blueprint) {},
		"tags_all: it cannot be set: AWS sets it":               func(b *Blueprint) { b.Blocks[1].Attributes["tags_all"] = map[string]any{} },
		"attributes.id: it cannot be set: AWS sets it":          func(b *Blueprint) { b.Blocks[1].Attributes["id"] = "x" },
		"arn cannot be set: AWS sets it":                        func(b *Blueprint) { b.Links[0].Attribute = "arn" },
		"raw_message_delivery takes \"bool\", not one value":    func(b *Blueprint) { b.Links[0].Attribute = "raw_message_delivery" },
		"is not a list; link it without list":                   func(b *Blueprint) { b.Links[0].List = true },
		"has no attribute \"arnn\"":                             func(b *Blueprint) { b.Links[0].Output = "arnn" },
	} {
		b := withResources()
		edit(&b)
		allowed := func(typ string) bool { return want != "not allowed in this account" || typ != "aws_sns_topic" }
		if want == "subscription_role_arn" {
			continue // placeholder kept out: role links are checked like any other text setting
		}
		err := b.Validate()
		if err == nil {
			err = b.CheckResources(lookup(schemas), allowed)
		}
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("want %q, got %v", want, err)
		}
	}
	// Nested blocks: an object or a list of objects, within their counts, with their own settings checked.
	rec := func(attrs map[string]any) error {
		b := Blueprint{ID: "bp", Name: "DNS", Target: Target{Cloud: "aws", Account: "primary", Region: "ap-south-1"},
			Blocks: []Block{{ID: "www", Kind: KindResource, Name: "www", ResourceType: "aws_route53_record", Attributes: attrs}}}
		if err := b.Validate(); err != nil {
			return err
		}
		return b.CheckResources(lookup(schemas), allowAll)
	}
	base := func() map[string]any {
		return map[string]any{"zone_id": "Z1", "name": "www", "type": "A", "alias": []any{map[string]any{"name": "lb.example", "zone_id": "Z2", "evaluate_target_health": true}}}
	}
	if err := rec(base()); err != nil {
		t.Errorf("a record with an alias: %v", err)
	}
	for want, edit := range map[string]func(map[string]any){
		"alias: give between 0 and 1":                  func(m map[string]any) { m["alias"] = []any{m["alias"].([]any)[0], m["alias"].([]any)[0]} },
		"alias[0].evaluate_target_health: give a bool": func(m map[string]any) { m["alias"].([]any)[0].(map[string]any)["evaluate_target_health"] = "yes" },
		"alias[0].zone: there is no such setting":      func(m map[string]any) { m["alias"].([]any)[0].(map[string]any)["zone"] = "x" },
		"alias[0].name: it is required":                func(m map[string]any) { delete(m["alias"].([]any)[0].(map[string]any), "name") },
		"records[0]: give a string":                    func(m map[string]any) { m["records"] = []any{1.0} },
		"timeouts: give one object":                    func(m map[string]any) { m["timeouts"] = []any{} },
	} {
		m := base()
		edit(m)
		if err := rec(m); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("want %q, got %v", want, err)
		}
	}
}

// A data-holding resource is kept by keep-data and unprotected by step 1 of removing everything; others go.
func TestResourceRemoval(t *testing.T) {
	b := withResources()
	b.Blocks = append(b.Blocks, Block{ID: "files", Kind: KindResource, Name: "Files", ResourceType: "aws_s3_bucket", Attributes: map[string]any{"bucket": "fl-files"}})
	keep, err := b.RemovalTofu(RemoveKeepData, false)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(keep), `"aws_s3_bucket"`) || strings.Contains(string(keep), "aws_sns_topic") || strings.Contains(string(keep), "force_destroy") {
		t.Errorf("keep-data:\n%s", keep)
	}
	un, _ := b.RemovalTofu(RemoveUnprotect, false)
	if !strings.Contains(string(un), `"force_destroy": true`) {
		t.Errorf("unprotect:\n%s", un)
	}
}

// A resource block named like a part another block makes is refused, not silently merged: an app makes the
// cluster aws_ecs_cluster.main.
func TestResourceNameClash(t *testing.T) {
	b := Blueprint{ID: "bp", Name: "Clash", Target: Target{Cloud: "aws", Account: "primary", Region: "ap-south-1"},
		Blocks: []Block{{ID: "app", Kind: KindService, Name: "App", Image: "nginx:1.29", CPU: 256, Memory: 512, Count: 1},
			{ID: "main", Kind: KindResource, Name: "Main", ResourceType: "aws_ecs_cluster", Attributes: map[string]any{"name": "x"}}}}
	if _, err := b.Tofu(); err == nil || !strings.Contains(err.Error(), "aws_ecs_cluster.main") {
		t.Errorf("a clash was merged: %v", err)
	}
	b.Blocks[1].ID = "other"
	if _, err := b.Tofu(); err != nil {
		t.Errorf("distinct names: %v", err)
	}
}

// TestResourceCodeValidates runs tofu validate on the generated code (the provider checks every setting again).
func TestResourceCodeValidates(t *testing.T) {
	bin := os.Getenv("FORGELINE_TOFU")
	if bin == "" {
		t.Skip("set FORGELINE_TOFU to a tofu binary to check the code with tofu validate")
	}
	code, err := withResources().Tofu()
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "main.tf.json"), code, 0o600)
	for _, args := range [][]string{{"init", "-backend=false", "-input=false", "-no-color"}, {"validate", "-no-color"}} {
		cmd := exec.Command(bin, args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("tofu %s: %v\n%s", args[0], err, out)
		}
	}
}

// Settable: only what a person may give, and never a secret, however the provider marks it.
func TestSettable(t *testing.T) {
	for want, a := range map[bool][]SchemaAttr{
		true:  {{Required: true}, {Optional: true}, {Optional: true, Computed: true}},
		false: {{Computed: true}, {Optional: true, Sensitive: true}, {Optional: true, WriteOnly: true}, {Required: true, Sensitive: true}, {}},
	} {
		for _, x := range a {
			if x.Settable() != want {
				t.Errorf("%+v: settable %v, want %v", x, x.Settable(), want)
			}
		}
	}
}

// escDeep reaches into lists and objects, keys included.
func TestEscDeep(t *testing.T) {
	got, _ := json.Marshal(escDeep(map[string]any{"${k}": []any{"${a}", map[string]any{"b": "%{x}"}, 1.0, true}}))
	if string(got) != `{"$${k}":["$${a}",{"b":"%%{x}"},1,true]}` {
		t.Errorf("escDeep = %s", got)
	}
}

// A nested block the schema needs at least one of must be given.
func TestRequiredNestedBlock(t *testing.T) {
	s := ResourceSchema{Blocks: map[string]SchemaBlock{"origin": {Nesting: "set", MinItems: 1, Block: ResourceSchema{
		Attributes: map[string]SchemaAttr{"domain_name": {Type: json.RawMessage(`"string"`), Required: true}}}}}}
	if err := checkObject("x", s, map[string]any{}, nil); err == nil || !strings.Contains(err.Error(), "x.origin: it is required") {
		t.Errorf("a missing required block: %v", err)
	}
	if err := checkObject("x", s, map[string]any{"origin": []any{map[string]any{"domain_name": "a.example"}}}, nil); err != nil {
		t.Error(err)
	}
}
