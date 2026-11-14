package blueprint

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/forgelinehq/forgeline/internal/template"
)

const jobsImage = "111122223333.dkr.ecr.ap-south-1.amazonaws.com/fl-abc-jobs:1a2b3c"

// withJobs is a blueprint with a Lambda function template: triggered by a queue, using a table, a secret and an
// existing bucket.
func withJobs() Blueprint {
	return Blueprint{ID: "bp-jobs", Name: "Jobs", Target: Target{Cloud: "aws", Account: "primary", Region: "ap-south-1"},
		Blocks: []Block{
			{ID: "jobs", Kind: KindTemplate, Name: "Nightly jobs", Template: "aws-lambda-function@1.0.0",
				Inputs: map[string]any{"image": jobsImage, "memory": 1024.0, "schedule": "rate(1 day)", "env": map[string]any{"MODE": "${evil}"}}},
			{ID: "inbox", Kind: KindQueue, Name: "Inbox", RetentionDays: 4},
			{ID: "orders", Kind: KindTable, Name: "Orders", PartitionKey: "id", PartitionType: "S"},
			{ID: "key", Kind: KindSecret, Name: "API key"},
			{ID: "old", Kind: KindExisting, Name: "Old bucket", Existing: &Existing{Type: "bucket", Name: "acme-old-bucket"}},
		},
		Links: []Link{
			{From: "inbox", To: "jobs"},
			{From: "jobs", To: "orders", Access: AccessReadWrite},
			{From: "jobs", To: "key"},
			{From: "jobs", To: "old", Access: AccessRead},
		}}
}

func TestProviderPinMatchesTemplates(t *testing.T) {
	if p := template.Pinned["aws"]; p.Source != ProviderSource || p.Version != ProviderVersion {
		t.Fatalf("template pin %+v, blueprint pin %s %s", p, ProviderSource, ProviderVersion)
	}
}

func TestTemplateBlockValidates(t *testing.T) {
	if err := withJobs().Validate(); err != nil {
		t.Fatal(err)
	}
	for name, mut := range map[string]func(*Blueprint){
		"unknown template":     func(b *Blueprint) { b.Blocks[0].Template = "aws-lambda-function@9.9.9" },
		"bad input":            func(b *Blueprint) { b.Blocks[0].Inputs["memory"] = 99999.0 },
		"unknown input":        func(b *Blueprint) { b.Blocks[0].Inputs["cpu"] = 2.0 },
		"missing image":        func(b *Blueprint) { delete(b.Blocks[0].Inputs, "image") },
		"another kind's field": func(b *Blueprint) { b.Blocks[0].Image = jobsImage },
		"inputs on a table":    func(b *Blueprint) { b.Blocks[2].Inputs = map[string]any{"x": 1.0} },
		"no access on data":    func(b *Blueprint) { b.Links[1].Access = "" },
		"access on a secret":   func(b *Blueprint) { b.Links[2].Access = AccessRead },
		"access on a trigger":  func(b *Blueprint) { b.Links[0].Access = AccessRead },
		"a table triggers it":  func(b *Blueprint) { b.Links[0] = Link{From: "orders", To: "jobs"} },
		"bad access word":      func(b *Blueprint) { b.Links = append(b.Links, Link{From: "jobs", To: "inbox", Access: "fly"}) },
		"a database": func(b *Blueprint) {
			b.Blocks = append(b.Blocks, Block{ID: "db", Kind: KindDatabase, Name: "DB", Engine: "postgres", Size: "small", StorageGB: 20, BackupDays: 7})
			b.Links = append(b.Links, Link{From: "jobs", To: "db"})
		},
		"template to template": func(b *Blueprint) {
			b.Blocks = append(b.Blocks, Block{ID: "more", Kind: KindTemplate, Name: "More", Template: "aws-lambda-function@1.0.0", Inputs: map[string]any{"image": jobsImage}})
			b.Links = append(b.Links, Link{From: "jobs", To: "more"})
		},
	} {
		b := withJobs()
		mut(&b)
		if err := b.Validate(); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

func TestTemplateBlockCodeAndChecks(t *testing.T) {
	b := withJobs()
	code, err := b.Tofu()
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Module map[string]map[string]any `json:"module"`
		Output map[string]any            `json:"output"`
	}
	json.Unmarshal(code, &doc)
	m := doc.Module["jobs"]
	if m == nil || m["source"] != "./templates/aws-lambda-function/1.0.0" || m["memory"] != 1024.0 || m["architecture"] != "arm64" || m["image"] != jobsImage {
		t.Fatalf("module = %v", m)
	}
	if env := m["env"].(map[string]any); env["MODE"] != "$${evil}" {
		t.Errorf("a typed value can become an expression: %v", env)
	}
	if !strings.HasPrefix(m["name"].(string), "fl-") || len(m["name"].(string)) > 64 {
		t.Errorf("name = %v", m["name"])
	}
	j, _ := json.Marshal(m)
	for _, want := range []string{
		`"trigger_queues":[{"arn":"${aws_sqs_queue.inbox.arn}","id":"inbox"}]`,
		`{"access":"readwrite","arn":"${aws_dynamodb_table.orders.arn}","env":"TABLE_ORDERS","id":"orders","kind":"table","value":"${aws_dynamodb_table.orders.name}"}`,
		`{"access":"read","arn":"${data.aws_s3_bucket.old.arn}","env":"BUCKET_OLD","id":"old","kind":"bucket","value":"${data.aws_s3_bucket.old.id}"}`,
		`"secrets":[{"arn":"${aws_secretsmanager_secret.key.arn}","env":"KEY_SECRET_ARN","id":"key"}]`,
	} {
		if !strings.Contains(string(j), want) {
			t.Errorf("module lacks %s:\n%s", want, j)
		}
	}
	if doc.Output["jobs_function_arn"] == nil {
		t.Errorf("outputs = %v", doc.Output)
	}
	mods, err := b.Modules()
	if err != nil || len(mods) != 1 || mods["templates/aws-lambda-function/1.0.0/main.tf.json"] == "" {
		t.Errorf("modules = %v %v", mods, err)
	}
	var text []string
	for _, c := range b.Checks() {
		text = append(text, c.Text)
	}
	all := strings.Join(text, "\n")
	for _, want := range []string{"Nightly jobs: Runs " + jobsImage + " with 1024 MB", "Nightly jobs uses orders (read and write).", "inbox triggers Nightly jobs."} {
		if !strings.Contains(all, want) {
			t.Errorf("checks lack %q:\n%s", want, all)
		}
	}
}

func TestTemplateDeliveryFollowsItsArchitecture(t *testing.T) {
	b := withJobs()
	b.Blocks[0].Source = &Source{Repo: "acme/jobs"}
	d, err := b.SuggestDelivery("jobs", "111122223333")
	if err != nil || d.Platform != "linux/arm64" || !strings.Contains(d.Native.Pipeline, "platform: linux/arm64") {
		t.Fatalf("arm64 by default: %v %s", err, d.Platform)
	}
	b.Blocks[0].Inputs["architecture"] = "x86_64"
	if d, _ := b.SuggestDelivery("jobs", "111122223333"); d.Platform != "linux/amd64" {
		t.Errorf("x86_64: %s", d.Platform)
	}
}

func TestModulesForAcceptsOnlyLibraryTemplates(t *testing.T) {
	code, _ := withJobs().Tofu()
	if mods, err := ModulesFor(code); err != nil || len(mods) != 1 {
		t.Fatalf("modules = %v, %v", mods, err)
	}
	for _, src := range []string{"git::https://example.com/evil", "./templates/../../etc", "./templates/aws-lambda-function/9.9.9", "./templates/aws-lambda-function", "registry.terraform.io/x/y/aws"} {
		if _, err := ModulesFor([]byte(`{"module":{"x":{"source":"` + src + `"}}}`)); err == nil {
			t.Errorf("%s: accepted", src)
		}
	}
}

func TestTemplateBlueprintValidates(t *testing.T) {
	bin := os.Getenv("FORGELINE_TOFU")
	if bin == "" {
		t.Skip("set FORGELINE_TOFU to a tofu binary to check the code with tofu validate")
	}
	b := withJobs()
	dir := t.TempDir()
	code, err := b.Tofu()
	if err != nil {
		t.Fatal(err)
	}
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
