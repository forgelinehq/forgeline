package blueprint

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// interviewer is the frame's example: a front door, one small service, a new table, an existing table, a
// bucket, a queue and a logs block.
func interviewer() Blueprint {
	return Blueprint{
		ID: "bp-interviewer", Name: "AI interviewer", Target: Target{Cloud: "aws", Account: "primary", Region: "ap-south-1"},
		Blocks: []Block{
			{ID: "web", Kind: KindFrontDoor, Name: "Front door", HealthPath: "/health"},
			{ID: "app", Kind: KindService, Name: "Interviewer app", Image: "nginx:1.29", CPU: 512, Memory: 1024, Count: 1, Port: 8080, Env: map[string]string{"MODE": "prod ${not.evaluated}"}},
			{ID: "sessions", Kind: KindTable, Name: "Sessions", PartitionKey: "id", PartitionType: "S", SortKey: "at", SortType: "N"},
			{ID: "old-answers", Kind: KindExisting, Name: "Answers", Existing: &Existing{Type: "table", Name: "ai_interviewer_answers"}},
			{ID: "media", Kind: KindBucket, Name: "Recordings", Versioning: true},
			{ID: "jobs", Kind: KindQueue, Name: "Jobs", RetentionDays: 4},
			{ID: "logs", Kind: KindLogs, Name: "Logs", RetentionDays: 30},
		},
		Links: []Link{
			{From: "web", To: "app"},
			{From: "app", To: "sessions", Access: AccessReadWrite},
			{From: "app", To: "old-answers", Access: AccessRead},
			{From: "app", To: "media", Access: AccessReadWrite},
			{From: "app", To: "jobs", Access: AccessReadWrite},
			{From: "app", To: "logs"},
		},
	}
}

func TestValidate(t *testing.T) {
	if err := interviewer().Validate(); err != nil {
		t.Fatalf("the example is valid: %v", err)
	}
	cases := []struct {
		name, want string
		edit       func(*Blueprint)
	}{
		{"no name", "name:", func(b *Blueprint) { b.Name = " " }},
		{"other cloud", "target.cloud", func(b *Blueprint) { b.Target.Cloud = "gcp" }},
		{"bad region", "target.region", func(b *Blueprint) { b.Target.Region = "mumbai" }},
		{"bad account", "target.account", func(b *Blueprint) { b.Target.Account = "Primary Account" }},
		{"bad id", "blocks[0].id", func(b *Blueprint) { b.Blocks[0].ID = "Web" }},
		{"duplicate id", "used twice", func(b *Blueprint) { b.Blocks[2].ID = "app" }},
		{"unknown kind", "blocks[0].kind", func(b *Blueprint) { b.Blocks[0].Kind = "lambda" }},
		{"field of another kind", "blocks[2].image: a table has no image", func(b *Blueprint) { b.Blocks[2].Image = "x" }},
		{"bad image", "blocks[1].image", func(b *Blueprint) { b.Blocks[1].Image = "nginx latest" }},
		{"bad cpu", "blocks[1].cpu", func(b *Blueprint) { b.Blocks[1].CPU = 300 }},
		{"memory not allowed for cpu", "blocks[1].memory", func(b *Blueprint) { b.Blocks[1].Memory = 512 }},
		{"too many tasks", "blocks[1].count", func(b *Blueprint) { b.Blocks[1].Count = 11 }},
		{"bad env name", "blocks[1].env", func(b *Blueprint) { b.Blocks[1].Env = map[string]string{"mode": "x"} }},
		{"bad health path", "blocks[0].health_path", func(b *Blueprint) { b.Blocks[0].HealthPath = "/${x}" }},
		{"bad certificate", "blocks[0].certificate", func(b *Blueprint) { b.Blocks[0].Certificate = "my-cert" }},
		{"bad key type", "blocks[2].partition_type", func(b *Blueprint) { b.Blocks[2].PartitionType = "B" }},
		{"sort key same as partition", "blocks[2].sort_key", func(b *Blueprint) { b.Blocks[2].SortKey = "id" }},
		{"queue retention", "blocks[5].retention_days", func(b *Blueprint) { b.Blocks[5].RetentionDays = 15 }},
		{"log retention", "blocks[6].retention_days", func(b *Blueprint) { b.Blocks[6].RetentionDays = 31 }},
		{"existing without what", "blocks[3].existing", func(b *Blueprint) { b.Blocks[3].Existing = nil }},
		{"existing bad type", "existing.type", func(b *Blueprint) { b.Blocks[3].Existing.Type = "function" }},
		{"existing bad name", "existing.name", func(b *Blueprint) { b.Blocks[3].Existing = &Existing{Type: "bucket", Name: "Bad_Bucket"} }},
		{"link to nothing", "links[0].to", func(b *Blueprint) { b.Links[0].To = "nope" }},
		{"link twice", "already linked", func(b *Blueprint) { b.Links = append(b.Links, Link{From: "web", To: "app"}) }},
		{"wrong direction", "a table cannot link to a service", func(b *Blueprint) { b.Links[1] = Link{From: "sessions", To: "app", Access: AccessRead} }},
		{"data without access", "links[1].access", func(b *Blueprint) { b.Links[1].Access = "" }},
		{"traffic with access", "links[0].access", func(b *Blueprint) { b.Links[0].Access = AccessRead }},
		{"front door to portless service", "needs a port", func(b *Blueprint) { b.Blocks[1].Port = 0 }},
		{"front door to nothing", "exactly one service", func(b *Blueprint) { b.Links = b.Links[1:] }},
		{"two front doors to one service", "already takes traffic", func(b *Blueprint) {
			b.Blocks = append(b.Blocks, Block{ID: "web2", Kind: KindFrontDoor, Name: "Second"})
			b.Links = append(b.Links, Link{From: "web2", To: "app"})
		}},
		{"two logs blocks", "already writes its logs", func(b *Blueprint) {
			b.Blocks = append(b.Blocks, Block{ID: "logs2", Kind: KindLogs, Name: "More logs", RetentionDays: 7})
			b.Links = append(b.Links, Link{From: "app", To: "logs2"})
		}},
	}
	for _, c := range cases {
		b := interviewer()
		c.edit(&b)
		err := b.Validate()
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: got %v, want an error containing %q", c.name, err, c.want)
		}
	}
}

func TestChecks(t *testing.T) {
	b := interviewer()
	b.Blocks[4].Versioning = false
	var got []string
	for _, c := range b.Checks() {
		got = append(got, c.Level+": "+c.Text)
	}
	text := strings.Join(got, "\n")
	for _, want := range []string{
		"ok: Only the front door is reachable from the internet",
		"warn: Front door uses plain HTTP",
		"ok: Tables and buckets keep their data",
		"warn: Recordings keeps no old versions",
		"ok: Logs: logs are kept 30 days.",
		"ok: One existing resource is used as it is",
		"warn: One region only (ap-south-1)",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("checks miss %q:\n%s", want, text)
		}
	}
	if strings.Contains(text, "writes its logs to its own log group") {
		t.Errorf("a service with a logs block uses it:\n%s", text)
	}
	worker := Blueprint{ID: "w", Name: "Worker", Target: b.Target, Blocks: []Block{{ID: "w", Kind: KindService, Name: "Worker", Image: "busybox", CPU: 256, Memory: 512, Count: 1}}}
	wt := ""
	for _, c := range worker.Checks() {
		wt += c.Level + ": " + c.Text + "\n"
	}
	if !strings.Contains(wt, "ok: Nothing in it is reachable from the internet.") || !strings.Contains(wt, "warn: Worker runs, but no front door") {
		t.Errorf("worker checks:\n%s", wt)
	}
}

func resources(t *testing.T, code []byte) map[string]map[string]map[string]any {
	t.Helper()
	var doc struct {
		Resource map[string]map[string]map[string]any `json:"resource"`
	}
	if err := json.Unmarshal(code, &doc); err != nil {
		t.Fatal(err)
	}
	return doc.Resource
}

func TestTofuCode(t *testing.T) {
	code, err := interviewer().Tofu()
	if err != nil {
		t.Fatal(err)
	}
	s := string(code)
	r := resources(t, code)

	// What a person types is never evaluated.
	if !strings.Contains(s, `prod $${not.evaluated}`) || strings.Contains(s, `prod ${not.evaluated}`) {
		t.Errorf("an environment value was not escaped")
	}
	// Data is kept.
	tbl := r["aws_dynamodb_table"]["sessions"]
	if tbl["lifecycle"].(map[string]any)["prevent_destroy"] != true || tbl["deletion_protection_enabled"] != true || tbl["billing_mode"] != "PAY_PER_REQUEST" {
		t.Errorf("table: %v", tbl)
	}
	if r["aws_s3_bucket"]["media"]["lifecycle"].(map[string]any)["prevent_destroy"] != true {
		t.Errorf("bucket can be destroyed")
	}
	pab := r["aws_s3_bucket_public_access_block"]["media"]
	for _, k := range []string{"block_public_acls", "block_public_policy", "ignore_public_acls", "restrict_public_buckets"} {
		if pab[k] != true {
			t.Errorf("bucket public access %s = %v", k, pab[k])
		}
	}
	if r["aws_sqs_queue"]["jobs"]["sqs_managed_sse_enabled"] != true || r["aws_sqs_queue"]["jobs"]["message_retention_seconds"] != float64(4*86400) {
		t.Errorf("queue: %v", r["aws_sqs_queue"]["jobs"])
	}
	// Only the front door is open; the service takes traffic only from it.
	open := 0
	for name, rule := range r["aws_vpc_security_group_ingress_rule"] {
		if rule["cidr_ipv4"] == "0.0.0.0/0" {
			open++
			if !strings.HasPrefix(name, "web_lb") {
				t.Errorf("%s is open to the internet", name)
			}
		}
	}
	from := r["aws_vpc_security_group_ingress_rule"]["app_svc_from_web"]
	if open != 1 || from["referenced_security_group_id"] != "${aws_security_group.web_lb.id}" || from["from_port"] != float64(8080) {
		t.Errorf("ingress: open=%d, from door = %v", open, from)
	}
	// The service gets exactly the access its links give, and the names of what it uses.
	pol := r["aws_iam_role_policy"]["app_task"]["policy"].(string)
	for _, want := range []string{`"dynamodb:PutItem"`, `${aws_dynamodb_table.sessions.arn}/index/*`, `${data.aws_dynamodb_table.old-answers.arn}`, `${aws_s3_bucket.media.arn}/*`, `"sqs:SendMessage"`} {
		if !strings.Contains(pol, want) {
			t.Errorf("policy misses %s: %s", want, pol)
		}
	}
	var stmts struct{ Statement []struct{ Action []string } }
	json.Unmarshal([]byte(pol), &stmts)
	for _, st := range stmts.Statement {
		for _, a := range st.Action {
			if a == "dynamodb:PutItem" && len(st.Action) == len(grants["table"][AccessRead]) {
				t.Errorf("read access got a write action")
			}
		}
	}
	if len(stmts.Statement) != 4 || strings.Count(pol, "dynamodb:PutItem") != 1 {
		t.Errorf("one statement per use, read-only for the existing table: %s", pol)
	}
	cd := r["aws_ecs_task_definition"]["app"]["container_definitions"].(string)
	for _, want := range []string{`"TABLE_SESSIONS"`, `"TABLE_OLD_ANSWERS"`, `"BUCKET_MEDIA"`, `"QUEUE_URL_JOBS"`, `${aws_cloudwatch_log_group.logs.name}`, `"containerPort":8080`} {
		if !strings.Contains(cd, want) {
			t.Errorf("container misses %s: %s", want, cd)
		}
	}
	if _, own := r["aws_cloudwatch_log_group"]["app"]; own {
		t.Errorf("a service with a logs block must not get its own log group")
	}
	// The same blueprint gives the same code, byte for byte.
	again, _ := interviewer().Tofu()
	if string(again) != s {
		t.Errorf("code is not deterministic")
	}
	// An invalid blueprint gives no code.
	bad := interviewer()
	bad.Links = nil
	if _, err := bad.Tofu(); err == nil {
		t.Errorf("an invalid blueprint generated code")
	}
}

func TestTofuHTTPSAndNames(t *testing.T) {
	b := interviewer()
	b.Blocks[0].Certificate = "arn:aws:acm:ap-south-1:111122223333:certificate/abc-123"
	b.Blocks[0].ID = "a-very-long-front-door-name-xx"
	b.Links[0].From = b.Blocks[0].ID
	code, err := b.Tofu()
	if err != nil {
		t.Fatal(err)
	}
	r := resources(t, code)
	id := b.Blocks[0].ID
	if l := r["aws_lb_listener"][id+"_https"]; l["certificate_arn"] != b.Blocks[0].Certificate || l["port"] != float64(443) {
		t.Errorf("https listener: %v", l)
	}
	if r["aws_lb_listener"][id+"_http"]["default_action"].([]any)[0].(map[string]any)["type"] != "redirect" {
		t.Errorf("plain HTTP must redirect when there is a certificate")
	}
	if n := r["aws_lb"][id]["name"].(string); len(n) > 32 || strings.HasSuffix(n, "-") {
		t.Errorf("load balancer name %q", n)
	}
	if n := r["aws_lb_target_group"][id]["name"].(string); len(n) > 32 {
		t.Errorf("target group name %q", n)
	}
}

// TestTofuValidates runs the real OpenTofu on the generated code when FORGELINE_TOFU names a tofu binary. It
// downloads the AWS provider, so it is not part of the normal run.
func TestTofuValidates(t *testing.T) {
	bin := os.Getenv("FORGELINE_TOFU")
	if bin == "" {
		t.Skip("set FORGELINE_TOFU to a tofu binary to check the code with tofu validate")
	}
	for name, b := range map[string]Blueprint{"interviewer": interviewer(), "https": func() Blueprint {
		x := interviewer()
		x.Blocks[0].Certificate = "arn:aws:acm:ap-south-1:111122223333:certificate/abc-123"
		return x
	}()} {
		dir := t.TempDir()
		code, err := b.Tofu()
		if err != nil {
			t.Fatal(err)
		}
		os.WriteFile(filepath.Join(dir, "main.tf.json"), code, 0o600)
		cache := os.Getenv("TF_PLUGIN_CACHE_DIR")
		for _, args := range [][]string{{"init", "-backend=false", "-input=false"}, {"validate", "-no-color"}} {
			cmd := exec.Command(bin, args...)
			cmd.Dir = dir
			cmd.Env = append(os.Environ(), "TF_PLUGIN_CACHE_DIR="+cache)
			if out, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("%s: tofu %s: %v\n%s", name, args[0], err, out)
			}
		}
	}
}
