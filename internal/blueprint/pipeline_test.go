package blueprint

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// withPipeline is an app that sends events to a data pipeline, which a Lambda function reshapes into a bucket.
func withPipeline() Blueprint {
	b := withJobs()
	b.Blocks = append(b.Blocks,
		Block{ID: "app", Kind: KindService, Name: "App", Image: "nginx:1.29", CPU: 256, Memory: 512, Count: 1},
		Block{ID: "lake", Kind: KindBucket, Name: "Lake"},
		Block{ID: "events", Kind: KindTemplate, Name: "Events", Template: "aws-data-pipeline@1.0.0", Inputs: map[string]any{"columns": map[string]any{"event": "string", "at": "timestamp"}}})
	b.Links = append(b.Links, Link{From: "app", To: "events"}, Link{From: "events", To: "lake", Access: AccessReadWrite}, Link{From: "events", To: "jobs"})
	return b
}

func TestAppSendsToAPipeline(t *testing.T) {
	b := withPipeline()
	if err := b.Validate(); err != nil {
		t.Fatal(err)
	}
	code, _ := b.Tofu()
	var doc struct {
		Module   map[string]map[string]any            `json:"module"`
		Resource map[string]map[string]map[string]any `json:"resource"`
	}
	json.Unmarshal(code, &doc)
	ev := doc.Module["events"]
	j, _ := json.Marshal([]any{ev["writers"], ev["transform"], ev["store"]})
	if !strings.Contains(string(j), `[{"id":"app"}]`) || !strings.Contains(string(j), `"${module.jobs.function_arn}"`) || !strings.Contains(string(j), `"id":"lake"`) {
		t.Errorf("module links = %s", j)
	}
	// The app's role may send to exactly the pipeline's stream, and it gets the stream's name.
	policy := doc.Resource["aws_iam_role_policy"]["app_task"]["policy"].(string)
	var p struct{ Statement []map[string]any }
	json.Unmarshal([]byte(policy), &p)
	if len(p.Statement) != 1 {
		t.Fatalf("policy = %s", policy)
	}
	st, _ := json.Marshal(p.Statement[0])
	if string(st) != `{"Action":["firehose:PutRecord","firehose:PutRecordBatch"],"Effect":"Allow","Resource":["${module.events.stream_arn}"]}` {
		t.Errorf("statement = %s", st)
	}
	td := doc.Resource["aws_ecs_task_definition"]["app"]["container_definitions"].(string)
	if !strings.Contains(td, `{"name":"EVENTS_STREAM","value":"${module.events.stream_name}"}`) {
		t.Errorf("container env = %s", td)
	}
	var text []string
	for _, c := range b.Checks() {
		text = append(text, c.Text)
	}
	if all := strings.Join(text, "\n"); !strings.Contains(all, "app sends to Events (its role may only firehose:PutRecord, firehose:PutRecordBatch on it).") ||
		!strings.Contains(all, "Events stores in lake (read and write).") {
		t.Errorf("checks:\n%s", all)
	}

	for name, mut := range map[string]func(*Blueprint){
		"no bucket": func(b *Blueprint) {
			b.Links = b.Links[:len(b.Links)-2]
			b.Links = append(b.Links, Link{From: "events", To: "jobs"})
		},
		"two buckets": func(b *Blueprint) {
			b.Blocks = append(b.Blocks, Block{ID: "lake2", Kind: KindBucket, Name: "Lake 2"})
			b.Links = append(b.Links, Link{From: "events", To: "lake2", Access: AccessReadWrite})
		},
		"two transforms": func(b *Blueprint) {
			b.Blocks = append(b.Blocks, Block{ID: "fn2", Kind: KindTemplate, Name: "Fn 2", Template: "aws-lambda-function@1.0.0", Inputs: map[string]any{"image": jobsImage}})
			b.Links = append(b.Links, Link{From: "events", To: "fn2"})
		},
		"a queue sends to it":     func(b *Blueprint) { b.Links = append(b.Links, Link{From: "inbox", To: "events"}) },
		"access on a send":        func(b *Blueprint) { b.Links[len(b.Links)-3].Access = AccessReadWrite },
		"a pipeline uses a table": func(b *Blueprint) { b.Links = append(b.Links, Link{From: "events", To: "orders", Access: AccessRead}) },
		"no columns":              func(b *Blueprint) { b.Blocks[len(b.Blocks)-1].Inputs = map[string]any{} },
		"a column of an odd type": func(b *Blueprint) {
			b.Blocks[len(b.Blocks)-1].Inputs = map[string]any{"columns": map[string]any{"x": "map<string,string>"}}
		},
	} {
		x := withPipeline()
		mut(&x)
		if err := x.Validate(); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

func TestPipelineBlueprintValidates(t *testing.T) {
	bin := os.Getenv("FORGELINE_TOFU")
	if bin == "" {
		t.Skip("set FORGELINE_TOFU to a tofu binary to check the code with tofu validate")
	}
	b := withPipeline()
	dir := t.TempDir()
	code, _ := b.Tofu()
	os.WriteFile(filepath.Join(dir, "main.tf.json"), code, 0o600)
	mods, _ := b.Modules()
	for p, body := range mods {
		os.MkdirAll(filepath.Join(dir, filepath.Dir(p)), 0o755)
		os.WriteFile(filepath.Join(dir, p), []byte(body), 0o600)
	}
	for _, args := range [][]string{{"init", "-backend=false", "-input=false", "-no-color"}, {"validate", "-no-color"}} {
		cmd := exec.Command(bin, args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("tofu %s: %v\n%s", args[0], err, out)
		}
	}
}
