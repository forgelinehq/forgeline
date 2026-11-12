package blueprint

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// webStack is the maintainer's request (note 039): a React frontend and a PHP backend behind one front door, the
// backend at /api/*, a queue worker, both backend services scaling, MySQL, Redis and a secret.
func webStack() Blueprint {
	return Blueprint{
		ID: "bp-web", Name: "Web stack", Target: Target{Cloud: "aws", Account: "primary", Region: "ap-south-1"},
		Blocks: []Block{
			{ID: "door", Kind: KindFrontDoor, Name: "Front door"},
			{ID: "frontend", Kind: KindService, Name: "Frontend (React)", Image: "nginx:1.29", CPU: 256, Memory: 512, Count: 1, Port: 80},
			{ID: "backend", Kind: KindService, Name: "Backend (PHP)", Image: "php:8.3-apache", CPU: 512, Memory: 1024, Count: 2, Port: 8080, Min: 2, Max: 6, CPUTarget: 60},
			{ID: "worker", Kind: KindService, Name: "Worker (PHP)", Image: "php:8.3-cli", CPU: 256, Memory: 512, Count: 1, Min: 1, Max: 4},
			{ID: "jobs", Kind: KindQueue, Name: "Jobs", RetentionDays: 4},
			{ID: "db", Kind: KindDatabase, Name: "MySQL", Engine: "mysql", Size: "small", StorageGB: 20},
			{ID: "redis", Kind: KindCache, Name: "Redis", Size: "small"},
			{ID: "stripe-key", Kind: KindSecret, Name: "Stripe key", Description: "the payments API key"},
		},
		Links: []Link{
			{From: "door", To: "frontend"},
			{From: "door", To: "backend", Path: "/api/*"},
			{From: "frontend", To: "backend"},
			{From: "backend", To: "db"}, {From: "backend", To: "redis"}, {From: "backend", To: "stripe-key"},
			{From: "backend", To: "jobs", Access: AccessReadWrite},
			{From: "worker", To: "jobs", Access: AccessReadWrite}, {From: "worker", To: "db"},
		},
	}
}

func TestMoreBlocksValidate(t *testing.T) {
	if err := webStack().Validate(); err != nil {
		t.Fatalf("the web stack is valid: %v", err)
	}
	cases := []struct {
		name, want string
		edit       func(*Blueprint)
	}{
		{"database engine", "engine", func(b *Blueprint) { b.Blocks[5].Engine = "oracle" }},
		{"database size", "blocks[5].size", func(b *Blueprint) { b.Blocks[5].Size = "huge" }},
		{"database storage", "storage_gb", func(b *Blueprint) { b.Blocks[5].StorageGB = 5 }},
		{"cache size", "blocks[6].size", func(b *Blueprint) { b.Blocks[6].Size = "large" }},
		{"cache nodes", "nodes", func(b *Blueprint) { b.Blocks[6].Nodes = 9 }},
		{"secret description", "description", func(b *Blueprint) { b.Blocks[7].Description = "${evil}" }},
		{"scaling range", "blocks[2].max", func(b *Blueprint) { b.Blocks[2].Max = 1 }},
		{"scaling too far", "blocks[2].max", func(b *Blueprint) { b.Blocks[2].Max = 30 }},
		{"cpu target", "cpu_target", func(b *Blueprint) { b.Blocks[2].CPUTarget = 99 }},
		{"start outside the range", "blocks[2].count", func(b *Blueprint) { b.Blocks[2].Count = 9 }},
		{"access on a database link", "leave access out", func(b *Blueprint) { b.Links[3].Access = AccessRead }},
		{"call needs a port", "needs a port to be called", func(b *Blueprint) { b.Links[2] = Link{From: "frontend", To: "worker"} }},
		{"a service calling itself", "cannot call itself", func(b *Blueprint) { b.Links[2] = Link{From: "frontend", To: "frontend"} }},
		{"a bad path", "links[1].path", func(b *Blueprint) { b.Links[1].Path = "api" }},
		{"a path on a data link", "only a front door", func(b *Blueprint) { b.Links[3].Path = "/x" }},
		{"no default", "default traffic", func(b *Blueprint) { b.Links[0].Path = "/web/*" }},
		{"same path twice", "already sends", func(b *Blueprint) {
			b.Blocks = append(b.Blocks, Block{ID: "admin", Kind: KindService, Name: "Admin", Image: "nginx:1.29", CPU: 256, Memory: 512, Count: 1, Port: 80})
			b.Links = append(b.Links, Link{From: "door", To: "admin", Path: "/api/*"})
		}},
		{"existing database name", "existing.name", func(b *Blueprint) {
			b.Blocks[5] = Block{ID: "db", Kind: KindExisting, Name: "Old DB", Existing: &Existing{Type: "database", Name: "1bad"}}
		}},
		{"a database cannot link out", "cannot link", func(b *Blueprint) { b.Links = append(b.Links, Link{From: "db", To: "redis"}) }},
	}
	for _, c := range cases {
		b := webStack()
		c.edit(&b)
		if err := b.Validate(); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: got %v, want %q", c.name, err, c.want)
		}
	}
}

func TestMoreBlocksCode(t *testing.T) {
	code, err := webStack().Tofu()
	if err != nil {
		t.Fatal(err)
	}
	s := string(code)
	r := resources(t, code)

	db := r["aws_db_instance"]["db"]
	for k, want := range map[string]any{"engine": "mysql", "instance_class": "db.t4g.micro", "manage_master_user_password": true, "publicly_accessible": false,
		"deletion_protection": true, "storage_encrypted": true, "skip_final_snapshot": false, "backup_retention_period": float64(7)} {
		if db[k] != want {
			t.Errorf("database %s = %v, want %v", k, db[k], want)
		}
	}
	if _, has := db["password"]; has || strings.Contains(s, `"password":`) {
		t.Errorf("no password may be in the code")
	}
	if db["lifecycle"].(map[string]any)["prevent_destroy"] != true {
		t.Errorf("the database can be destroyed")
	}
	rg := r["aws_elasticache_replication_group"]["redis"]
	if rg["transit_encryption_enabled"] != true || rg["at_rest_encryption_enabled"] != true || rg["node_type"] != "cache.t4g.micro" {
		t.Errorf("redis = %v", rg)
	}
	if sec := r["aws_secretsmanager_secret"]["stripe-key"]; sec["lifecycle"] == nil || strings.Contains(s, "aws_secretsmanager_secret_version") {
		t.Errorf("a secret is created empty and kept: %v", sec)
	}

	// The backend reaches the database and Redis, and only it and the worker are let in.
	ing := r["aws_vpc_security_group_ingress_rule"]
	if ing["db_db_from_backend"]["from_port"] != float64(3306) || ing["db_db_from_worker"] == nil || ing["redis_cache_from_backend"]["from_port"] != float64(6379) || ing["redis_cache_from_worker"] != nil {
		t.Errorf("ingress: %v", ing)
	}
	// The frontend calls the backend by name in the private namespace; the backend lets it in.
	if ing["backend_svc_from_frontend"]["referenced_security_group_id"] != "${aws_security_group.frontend_svc.id}" || r["aws_service_discovery_service"]["backend"] == nil {
		t.Errorf("service call: %v", ing["backend_svc_from_frontend"])
	}
	// The open-to-the-internet rule is still only the front door's.
	for name, rule := range ing {
		if rule["cidr_ipv4"] == "0.0.0.0/0" && !strings.HasPrefix(name, "door_lb") {
			t.Errorf("%s is open to the internet", name)
		}
	}
	// Containers: the database password and the secret are injected by ECS, by reference; never a value.
	cd := r["aws_ecs_task_definition"]["backend"]["container_definitions"].(string)
	for _, want := range []string{`"DB_HOST"`, `"REDIS_HOST"`, `"QUEUE_URL_JOBS"`, `"name":"DB_PASSWORD","valueFrom":"${aws_db_instance.db.master_user_secret[0].secret_arn}:password::"`, `"name":"STRIPE_KEY","valueFrom":"${aws_secretsmanager_secret.stripe-key.arn}"`} {
		if !strings.Contains(cd, want) {
			t.Errorf("backend container misses %s: %s", want, cd)
		}
	}
	if fcd := r["aws_ecs_task_definition"]["frontend"]["container_definitions"].(string); !strings.Contains(fcd, `"BACKEND_URL","value":"http://backend.fl-`) || strings.Contains(fcd, "DB_PASSWORD") {
		t.Errorf("frontend container: %s", fcd)
	}
	pol := r["aws_iam_role_policy"]["backend_exec_secrets"]["policy"].(string)
	if !strings.Contains(pol, "secretsmanager:GetSecretValue") || !strings.Contains(pol, "master_user_secret") || !strings.Contains(pol, "stripe-key") || r["aws_iam_role_policy"]["frontend_exec_secrets"] != nil {
		t.Errorf("secret access: %s", pol)
	}
	// /api/* goes to the backend's own target group; the default to the frontend's.
	rule := r["aws_lb_listener_rule"]["door_backend"]
	if rule == nil || r["aws_ecs_service"]["backend"]["load_balancer"].([]any)[0].(map[string]any)["target_group_arn"] != "${aws_lb_target_group.door_backend.arn}" ||
		r["aws_ecs_service"]["frontend"]["load_balancer"].([]any)[0].(map[string]any)["target_group_arn"] != "${aws_lb_target_group.door.arn}" {
		t.Errorf("paths: %v", rule)
	}
	// Scaling.
	if at := r["aws_appautoscaling_target"]["backend"]; at["min_capacity"] != float64(2) || at["max_capacity"] != float64(6) || r["aws_appautoscaling_policy"]["worker"] == nil || r["aws_appautoscaling_target"]["frontend"] != nil {
		t.Errorf("scaling: %v", at)
	}
	var checks []string
	for _, c := range webStack().Checks() {
		checks = append(checks, c.Text)
	}
	all := strings.Join(checks, "\n")
	for _, want := range []string{"AWS keeps its password", "Redis", "starts empty", "calls Backend (PHP) privately", "scales between 2 and 6"} {
		if !strings.Contains(all, want) {
			t.Errorf("checks miss %q:\n%s", want, all)
		}
	}
}

// TestMoreBlocksTofuValidates checks the web stack with the real OpenTofu, like TestTofuValidates.
func TestMoreBlocksTofuValidates(t *testing.T) {
	bin := os.Getenv("FORGELINE_TOFU")
	if bin == "" {
		t.Skip("set FORGELINE_TOFU to a tofu binary to check the code with tofu validate")
	}
	b := webStack()
	b.Blocks = append(b.Blocks, Block{ID: "old-db", Kind: KindExisting, Name: "Old DB", Existing: &Existing{Type: "database", Name: "legacy"}},
		Block{ID: "old-cache", Kind: KindExisting, Name: "Old cache", Existing: &Existing{Type: "cache", Name: "legacy-cache"}},
		Block{ID: "old-secret", Kind: KindExisting, Name: "Old secret", Existing: &Existing{Type: "secret", Name: "prod/api"}})
	b.Links = append(b.Links, Link{From: "worker", To: "old-db"}, Link{From: "worker", To: "old-cache"}, Link{From: "worker", To: "old-secret"})
	code, err := b.Tofu()
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "main.tf.json"), code, 0o600)
	for _, args := range [][]string{{"init", "-backend=false", "-input=false"}, {"validate", "-no-color"}} {
		cmd := exec.Command(bin, args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), "TF_PLUGIN_CACHE_DIR="+os.Getenv("TF_PLUGIN_CACHE_DIR"))
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("tofu %s: %v\n%s", args[0], err, out)
		}
	}
}
