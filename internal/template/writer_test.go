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

const pipelineDir = "library/aws-data-pipeline/1.0.0"

// pipelineWith is a copy of the data pipeline template with one file changed.
func pipelineWith(t *testing.T, file string, change func(map[string]any)) fstest.MapFS {
	t.Helper()
	m := fstest.MapFS{}
	for _, f := range []string{"template.json", "diagram.json", "main.tf.json"} {
		b, err := library.ReadFile(pipelineDir + "/" + f)
		if err != nil {
			t.Fatal(err)
		}
		if f == file {
			var doc map[string]any
			json.Unmarshal(b, &doc)
			change(doc)
			b, _ = json.Marshal(doc)
		}
		m["t/aws-data-pipeline/1.0.0/"+f] = &fstest.MapFile{Data: b}
	}
	return m
}

func TestWriterLinks(t *testing.T) {
	p, err := Get("aws-data-pipeline@1.0.0")
	if err != nil {
		t.Fatal(err)
	}
	r, ok := p.Role("in", "service")
	if !ok || r.Shape != ShapeWriter || r.Output != "stream_arn" || r.EnvSuffix != "STREAM" || strings.Join(r.Actions, ",") != "firehose:PutRecord,firehose:PutRecordBatch" {
		t.Fatalf("writer role = %+v %v", r, ok)
	}
	if s, _ := p.Role("out", "bucket"); s.Min != 1 || s.Max != 1 {
		t.Errorf("store role = %+v", s)
	}
	if _, err := Load(pipelineWith(t, "", nil), "t/aws-data-pipeline/1.0.0"); err != nil {
		t.Fatalf("the unchanged copy: %v", err)
	}
	writer := func(d map[string]any) map[string]any { return d["links"].([]any)[2].(map[string]any) }
	outputs := func(d map[string]any) map[string]any { return d["output"].(map[string]any) }
	for name, c := range map[string]struct {
		file   string
		change func(map[string]any)
	}{
		"a wildcard action":      {"diagram.json", func(d map[string]any) { writer(d)["actions"] = []any{"firehose:*"} }},
		"every action":           {"diagram.json", func(d map[string]any) { writer(d)["actions"] = []any{"*"} }},
		"an IAM action":          {"diagram.json", func(d map[string]any) { writer(d)["actions"] = []any{"iam:PassRole"} }},
		"an STS action":          {"diagram.json", func(d map[string]any) { writer(d)["actions"] = []any{"sts:AssumeRole"} }},
		"a KMS action":           {"diagram.json", func(d map[string]any) { writer(d)["actions"] = []any{"kms:Decrypt"} }},
		"no actions":             {"diagram.json", func(d map[string]any) { writer(d)["actions"] = []any{} }},
		"no env output":          {"diagram.json", func(d map[string]any) { delete(writer(d), "env_output") }},
		"a bad env suffix":       {"diagram.json", func(d map[string]any) { writer(d)["env_suffix"] = "stream name" }},
		"a writer pointing out":  {"diagram.json", func(d map[string]any) { writer(d)["direction"] = "out" }},
		"a writer for a table":   {"diagram.json", func(d map[string]any) { writer(d)["kinds"] = []any{"table"} }},
		"actions on a data link": {"diagram.json", func(d map[string]any) { d["links"].([]any)[0].(map[string]any)["actions"] = []any{"s3:GetObject"} }},
		"a service as a trigger": {"diagram.json", func(d map[string]any) { writer(d)["shape"] = "source"; delete(writer(d), "output") }},
		"min above max":          {"diagram.json", func(d map[string]any) { d["links"].([]any)[0].(map[string]any)["min"] = 2 }},
		"a missing output":       {"diagram.json", func(d map[string]any) { writer(d)["output"] = "nope" }},
		"a missing env output":   {"diagram.json", func(d map[string]any) { writer(d)["env_output"] = "nope" }},
		"an output that is *":    {"main.tf.json", func(d map[string]any) { outputs(d)["stream_arn"] = map[string]any{"value": "*"} }},
		"an output from a var":   {"main.tf.json", func(d map[string]any) { outputs(d)["stream_arn"] = map[string]any{"value": "${var.store[0].arn}"} }},
		"an output not an ARN": {"main.tf.json", func(d map[string]any) {
			outputs(d)["stream_arn"] = map[string]any{"value": "${aws_kinesis_firehose_delivery_stream.stream.name}"}
		}},
		"an ARN of nothing here": {"main.tf.json", func(d map[string]any) {
			outputs(d)["stream_arn"] = map[string]any{"value": "${aws_sns_topic.other.arn}"}
		}},
		"an ARN with an addition": {"main.tf.json", func(d map[string]any) {
			outputs(d)["stream_arn"] = map[string]any{"value": "${aws_kinesis_firehose_delivery_stream.stream.arn}*"}
		}},
	} {
		if _, err := Load(pipelineWith(t, c.file, c.change), "t/aws-data-pipeline/1.0.0"); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

// TestPipelineModuleValidates runs tofu validate on a root that calls the data pipeline with sample values.
func TestPipelineModuleValidates(t *testing.T) {
	bin := os.Getenv("FORGELINE_TOFU")
	if bin == "" {
		t.Skip("set FORGELINE_TOFU to a tofu binary to check the module with tofu validate")
	}
	dir := t.TempDir()
	mod := filepath.Join(dir, "templates", "aws-data-pipeline", "1.0.0")
	os.MkdirAll(mod, 0o755)
	code, _ := library.ReadFile(pipelineDir + "/main.tf.json")
	os.WriteFile(filepath.Join(mod, "main.tf.json"), code, 0o644)
	for name, transform := range map[string][]any{"plain": {}, "reshaped": {map[string]any{"id": "fn", "arn": "arn:aws:lambda:ap-south-1:111122223333:function:fl-abc-fn"}}} {
		root := map[string]any{
			"terraform": map[string]any{"required_providers": map[string]any{"aws": map[string]any{"source": "hashicorp/aws", "version": "~> 6.72"}}},
			"provider":  map[string]any{"aws": map[string]any{"region": "ap-south-1"}},
			"module": map[string]any{"events": map[string]any{
				"source": "./templates/aws-data-pipeline/1.0.0", "name": "fl-abc-events", "tags": map[string]any{"forgeline:block": "events"},
				"columns": map[string]any{"event": "string", "at": "timestamp"}, "buffer_seconds": 60, "buffer_mb": 1, "scan_limit_gb": 1, "log_retention_days": 7,
				"store":     []any{map[string]any{"id": "lake", "kind": "bucket", "arn": "arn:aws:s3:::lake", "value": "lake", "env": "LAKE_BUCKET", "access": "readwrite"}},
				"transform": transform,
				"writers":   []any{map[string]any{"id": "app"}},
			}},
		}
		b, _ := json.Marshal(root)
		os.WriteFile(filepath.Join(dir, "main.tf.json"), b, 0o644)
		for _, args := range [][]string{{"init", "-backend=false", "-input=false", "-no-color"}, {"validate", "-no-color"}} {
			cmd := exec.Command(bin, args...)
			cmd.Dir = dir
			if out, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("%s: tofu %s: %v\n%s", name, args[0], err, out)
			}
		}
	}
}
