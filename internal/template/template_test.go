package template

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
)

const lambdaDir = "library/aws-lambda-function/1.0.0"

func TestLibraryLoads(t *testing.T) {
	all, err := Library()
	if err != nil {
		t.Fatal(err)
	}
	l, ok := all["aws-lambda-function@1.0.0"]
	if !ok {
		t.Fatalf("library = %v", all)
	}
	if l.Inputs.Order[0] != "image" || l.Diagram.Icon == "" || len(l.Diagram.Links) != 3 || l.HoldsData {
		t.Errorf("lambda = %+v", l)
	}
	if r, ok := l.Role("in", "queue"); !ok || r.Variable != "trigger_queues" {
		t.Errorf("a queue that triggers it: %+v %v", r, ok)
	}
	if _, ok := l.Role("out", "database"); ok {
		t.Error("a database link is offered")
	}
}

func TestResolveChecksInputsAndFillsDefaults(t *testing.T) {
	l, _ := Get("aws-lambda-function@1.0.0")
	img := "111122223333.dkr.ecr.ap-south-1.amazonaws.com/fl-abc-jobs:1a2b3c"
	got, err := l.Inputs.Resolve(map[string]any{"image": img, "env": map[string]any{"MODE": "batch"}, "schedule": "rate(5 minutes)"})
	if err != nil {
		t.Fatal(err)
	}
	if got["memory"] != float64(512) || got["architecture"] != "arm64" || got["timeout"] != float64(30) || got["schedule"] != "rate(5 minutes)" {
		t.Errorf("resolved = %v", got)
	}
	for name, in := range map[string]map[string]any{
		"missing image":        {},
		"docker hub image":     {"image": "nginx:latest"},
		"unknown input":        {"image": img, "cpu": 2.0},
		"memory too big":       {"image": img, "memory": 20000.0},
		"memory not whole":     {"image": img, "memory": 512.5},
		"bad architecture":     {"image": img, "architecture": "risc-v"},
		"bad schedule":         {"image": img, "schedule": "every minute"},
		"schedule injection":   {"image": img, "schedule": "rate(5 minutes)\"; x = \"y"},
		"bad env name":         {"image": img, "env": map[string]any{"mode": "x"}},
		"env value not text":   {"image": img, "env": map[string]any{"MODE": 1.0}},
		"retention not listed": {"image": img, "log_retention_days": 2.0},
	} {
		if _, err := l.Inputs.Resolve(in); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

func TestSchemaSubsetIsEnforced(t *testing.T) {
	for name, s := range map[string]string{
		"unknown keyword":         `{"type":"object","additionalProperties":false,"properties":{"a":{"type":"string","maxLength":5,"format":"email"}}}`,
		"open object":             `{"type":"object","properties":{"a":{"type":"string","maxLength":5}}}`,
		"array input":             `{"type":"object","additionalProperties":false,"properties":{"a":{"type":"array"}}}`,
		"text without a limit":    `{"type":"object","additionalProperties":false,"properties":{"a":{"type":"string"}}}`,
		"unanchored pattern":      `{"type":"object","additionalProperties":false,"properties":{"a":{"type":"string","maxLength":5,"pattern":"a+"}}}`,
		"bad default":             `{"type":"object","additionalProperties":false,"properties":{"a":{"type":"integer","maximum":3,"default":4}}}`,
		"forgeline's name":        `{"type":"object","additionalProperties":false,"properties":{"name":{"type":"string","maxLength":5}}}`,
		"map without key pattern": `{"type":"object","additionalProperties":false,"properties":{"a":{"type":"object","maxProperties":3,"additionalProperties":{"type":"string","maxLength":5}}}}`,
		"required unknown":        `{"type":"object","additionalProperties":false,"required":["b"],"properties":{"a":{"type":"boolean"}}}`,
		"min on text":             `{"type":"object","additionalProperties":false,"properties":{"a":{"type":"string","maxLength":5,"minimum":1}}}`,
	} {
		if _, err := ParseSchema(json.RawMessage(s)); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

// fsWith copies the Lambda template into a test file system, with one file changed.
func fsWith(t *testing.T, file string, change func(map[string]any)) fstest.MapFS {
	t.Helper()
	m := fstest.MapFS{}
	for _, f := range []string{"template.json", "diagram.json", "main.tf.json"} {
		b, err := library.ReadFile(lambdaDir + "/" + f)
		if err != nil {
			t.Fatal(err)
		}
		if f == file {
			var doc map[string]any
			json.Unmarshal(b, &doc)
			change(doc)
			b, _ = json.Marshal(doc)
		}
		m["t/aws-lambda-function/1.0.0/"+f] = &fstest.MapFile{Data: b}
	}
	return m
}

func TestCodeRulesAreEnforced(t *testing.T) {
	if _, err := Load(fsWith(t, "", nil), "t/aws-lambda-function/1.0.0"); err != nil {
		t.Fatalf("the unchanged copy: %v", err)
	}
	res := func(d map[string]any) map[string]any { return d["resource"].(map[string]any) }
	for name, c := range map[string]struct {
		file   string
		change func(map[string]any)
	}{
		"a provider block": {"main.tf.json", func(d map[string]any) { d["provider"] = map[string]any{"aws": map[string]any{"region": "us-east-1"}} }},
		"a nested module": {"main.tf.json", func(d map[string]any) {
			d["module"] = map[string]any{"x": map[string]any{"source": "git::https://example.com/x"}}
		}},
		"a backend": {"main.tf.json", func(d map[string]any) {
			d["terraform"].(map[string]any)["backend"] = map[string]any{"s3": map[string]any{}}
		}},
		"another provider": {"main.tf.json", func(d map[string]any) { res(d)["null_resource"] = map[string]any{"x": map[string]any{}} }},
		"an external reader": {"main.tf.json", func(d map[string]any) {
			d["data"].(map[string]any)["external"] = map[string]any{"x": map[string]any{"program": []string{"sh"}}}
		}},
		"a secret reader": {"main.tf.json", func(d map[string]any) {
			d["data"].(map[string]any)["aws_secretsmanager_secret_version"] = map[string]any{"x": map[string]any{"secret_id": "a"}}
		}},
		"a provisioner": {"main.tf.json", func(d map[string]any) {
			res(d)["aws_iam_role"].(map[string]any)["fn"].(map[string]any)["provisioner"] = []any{map[string]any{"local-exec": map[string]any{"command": "curl x | sh"}}}
		}},
		"its own provider": {"main.tf.json", func(d map[string]any) {
			res(d)["aws_iam_role"].(map[string]any)["fn"].(map[string]any)["provider"] = "aws.other"
		}},
		"an extra variable":  {"main.tf.json", func(d map[string]any) { d["variable"].(map[string]any)["sneaky"] = map[string]any{"type": "string"} }},
		"a missing variable": {"main.tf.json", func(d map[string]any) { delete(d["variable"].(map[string]any), "memory") }},
		"a loose type":       {"main.tf.json", func(d map[string]any) { d["variable"].(map[string]any)["uses"] = map[string]any{"type": "any"} }},
		"older provider pin": {"main.tf.json", func(d map[string]any) {
			d["terraform"].(map[string]any)["required_providers"] = map[string]any{"aws": map[string]any{"source": "hashicorp/aws", "version": ">= 1.0"}}
		}},
		"template's own pin": {"template.json", func(d map[string]any) {
			d["providers"] = map[string]any{"aws": map[string]any{"source": "hashicorp/aws", "version": ">= 1.0"}}
		}},
		"wrong directory":     {"template.json", func(d map[string]any) { d["version"] = "1.0.1" }},
		"a link to anything":  {"diagram.json", func(d map[string]any) { d["links"].([]any)[0].(map[string]any)["kinds"] = []any{"table", "database"} }},
		"secret as data":      {"diagram.json", func(d map[string]any) { d["links"].([]any)[1].(map[string]any)["shape"] = "data" }},
		"access on a trigger": {"diagram.json", func(d map[string]any) { d["links"].([]any)[2].(map[string]any)["access"] = true }},
		"a link on an input":  {"diagram.json", func(d map[string]any) { d["links"].([]any)[0].(map[string]any)["variable"] = "memory" }},
		"unknown field":       {"diagram.json", func(d map[string]any) { d["script"] = "x" }},
	} {
		if _, err := Load(fsWith(t, c.file, c.change), "t/aws-lambda-function/1.0.0"); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

// A template that pins another provider version consistently in both files is still refused: only Forgeline's pin.
func TestProviderPinIsForgelines(t *testing.T) {
	loose := map[string]any{"aws": map[string]any{"source": "hashicorp/aws", "version": ">= 1.0"}}
	m := fsWith(t, "template.json", func(d map[string]any) { d["providers"] = loose })
	code := fsWith(t, "main.tf.json", func(d map[string]any) { d["terraform"].(map[string]any)["required_providers"] = loose })
	m["t/aws-lambda-function/1.0.0/main.tf.json"] = code["t/aws-lambda-function/1.0.0/main.tf.json"]
	if _, err := Load(m, "t/aws-lambda-function/1.0.0"); err == nil || !strings.Contains(err.Error(), "Forgeline pins") {
		t.Errorf("a template with its own provider version: %v", err)
	}
}

func TestVersionOrder(t *testing.T) {
	if !newer("1.10.0", "1.9.3") || newer("1.2.0", "1.10.0") || newer("2.0.0", "2.0.0") || !newer("2.0.0", "1.99.99") {
		t.Error("versions compare as text")
	}
}

// TestLambdaModuleValidates runs tofu validate on a root that calls the module with sample values. Set FORGELINE_TOFU to
// a tofu binary; providers come from the registry (or TF_PLUGIN_CACHE_DIR).
func TestLambdaModuleValidates(t *testing.T) {
	bin := os.Getenv("FORGELINE_TOFU")
	if bin == "" {
		t.Skip("set FORGELINE_TOFU to a tofu binary to check the module with tofu validate")
	}
	dir := t.TempDir()
	mod := filepath.Join(dir, "templates", "aws-lambda-function", "1.0.0")
	os.MkdirAll(mod, 0o755)
	code, _ := library.ReadFile(lambdaDir + "/main.tf.json")
	os.WriteFile(filepath.Join(mod, "main.tf.json"), code, 0o644)
	root := map[string]any{
		"terraform": map[string]any{"required_providers": map[string]any{"aws": map[string]any{"source": "hashicorp/aws", "version": "~> 6.72"}}},
		"provider":  map[string]any{"aws": map[string]any{"region": "ap-south-1"}},
		"module": map[string]any{"jobs": map[string]any{
			"source": "./templates/aws-lambda-function/1.0.0", "name": "fl-abc-jobs", "tags": map[string]any{"forgeline:block": "jobs"},
			"image": "111122223333.dkr.ecr.ap-south-1.amazonaws.com/fl-abc-jobs:1", "architecture": "arm64", "memory": 512, "timeout": 30,
			"schedule": "rate(5 minutes)", "batch_size": 10, "log_retention_days": 14, "env": map[string]any{"MODE": "batch"},
			"uses":           []any{map[string]any{"id": "orders", "kind": "table", "arn": "arn:aws:dynamodb:ap-south-1:111122223333:table/x", "value": "x", "env": "TABLE_ORDERS", "access": "readwrite"}},
			"secrets":        []any{},
			"trigger_queues": []any{map[string]any{"id": "inbox", "arn": "arn:aws:sqs:ap-south-1:111122223333:inbox"}},
		}},
	}
	b, _ := json.Marshal(root)
	os.WriteFile(filepath.Join(dir, "main.tf.json"), b, 0o644)
	for _, args := range [][]string{{"init", "-backend=false", "-input=false", "-no-color"}, {"validate", "-no-color"}} {
		cmd := exec.Command(bin, args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("tofu %s: %v\n%s", args[0], err, out)
		} else if args[0] == "validate" && !strings.Contains(string(out), "valid") {
			t.Fatalf("tofu validate: %s", out)
		}
	}
}
