package blueprint

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func spotShop(capacity string) Blueprint {
	return Blueprint{ID: "bp-x", Name: "X", Target: Target{Cloud: "aws", Account: "primary", Region: "ap-south-1"},
		Blocks: []Block{{ID: "web", Kind: KindFrontDoor, Name: "Web"},
			{ID: "app", Kind: KindService, Name: "App", Image: "nginx:1.29", CPU: 256, Memory: 512, Count: 2, Port: 80, Capacity: capacity},
			{ID: "worker", Kind: KindService, Name: "Worker", Image: "busybox:1.37", CPU: 256, Memory: 512, Count: 1}},
		Links: []Link{{From: "web", To: "app"}}}
}

func TestAppCapacity(t *testing.T) {
	code := func(b Blueprint) map[string]map[string]map[string]any {
		t.Helper()
		c, err := b.Tofu()
		if err != nil {
			t.Fatal(err)
		}
		var doc struct {
			Resource map[string]map[string]map[string]any `json:"resource"`
		}
		json.Unmarshal(c, &doc)
		return doc.Resource
	}
	// Regular Fargate: as before, with no capacity providers on the cluster.
	r := code(spotShop(""))
	if r["aws_ecs_service"]["app"]["launch_type"] != "FARGATE" || r["aws_ecs_service"]["app"]["capacity_provider_strategy"] != nil || r["aws_ecs_cluster_capacity_providers"] != nil {
		t.Errorf("regular: %v / %v", r["aws_ecs_service"]["app"], r["aws_ecs_cluster_capacity_providers"])
	}
	// Spot: the cluster offers both, the app uses Spot, the other app stays regular.
	r = code(spotShop(CapacitySpot))
	j, _ := json.Marshal(r["aws_ecs_service"]["app"]["capacity_provider_strategy"])
	if string(j) != `[{"capacity_provider":"FARGATE_SPOT","weight":1}]` || r["aws_ecs_service"]["app"]["launch_type"] != nil {
		t.Errorf("spot strategy = %s, launch_type %v", j, r["aws_ecs_service"]["app"]["launch_type"])
	}
	deps, _ := json.Marshal(r["aws_ecs_service"]["app"]["depends_on"])
	if !strings.Contains(string(deps), "aws_ecs_cluster_capacity_providers.main") || !strings.Contains(string(deps), "aws_lb_listener.web_http") {
		t.Errorf("depends_on = %s", deps)
	}
	if r["aws_ecs_service"]["worker"]["launch_type"] != "FARGATE" {
		t.Errorf("the other app moved off regular Fargate: %v", r["aws_ecs_service"]["worker"])
	}
	cp, _ := json.Marshal(r["aws_ecs_cluster_capacity_providers"]["main"])
	if !strings.Contains(string(cp), `"capacity_providers":["FARGATE","FARGATE_SPOT"]`) {
		t.Errorf("cluster providers = %s", cp)
	}
	// Mixed: one regular task as a base, the rest on Spot.
	r = code(spotShop(CapacityMixed))
	j, _ = json.Marshal(r["aws_ecs_service"]["app"]["capacity_provider_strategy"])
	if string(j) != `[{"base":1,"capacity_provider":"FARGATE","weight":1},{"capacity_provider":"FARGATE_SPOT","weight":3}]` {
		t.Errorf("mixed strategy = %s", j)
	}
	// Checks say what Spot means.
	var all []string
	for _, c := range spotShop(CapacitySpot).Checks() {
		all = append(all, c.Level+": "+c.Text)
	}
	if !strings.Contains(strings.Join(all, "\n"), "warn: App runs on Fargate Spot") {
		t.Errorf("checks:\n%s", strings.Join(all, "\n"))
	}
	for name, b := range map[string]Blueprint{
		"unknown capacity":    spotShop("cheap"),
		"capacity on a table": {ID: "bp-x", Name: "X", Target: Target{Cloud: "aws", Account: "primary", Region: "ap-south-1"}, Blocks: []Block{{ID: "t", Kind: KindTable, Name: "T", PartitionKey: "id", PartitionType: "S", Capacity: CapacitySpot}}},
	} {
		if err := b.Validate(); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

// Blueprints without Spot generate exactly the code they did before capacity existed, so existing apps see no change.
func TestNoCapacityKeepsTheCodeAsItWas(t *testing.T) {
	before, err := os.ReadFile(filepath.Join("testdata", "app-before-capacity.tf.json"))
	if err != nil {
		t.Fatal(err)
	}
	b := spotShop("")
	b.Blocks = b.Blocks[:2]
	b.Blocks[1].Count = 1
	now, _ := b.Tofu()
	if string(now) != string(before) {
		t.Error("an app without capacity generates different code than before")
	}
}

func TestSpotBlueprintValidates(t *testing.T) {
	bin := os.Getenv("FORGELINE_TOFU")
	if bin == "" {
		t.Skip("set FORGELINE_TOFU to a tofu binary to check the code with tofu validate")
	}
	dir := t.TempDir()
	c, _ := spotShop(CapacityMixed).Tofu()
	os.WriteFile(filepath.Join(dir, "main.tf.json"), c, 0o600)
	for _, args := range [][]string{{"init", "-backend=false", "-input=false", "-no-color"}, {"validate", "-no-color"}} {
		cmd := exec.Command(bin, args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("tofu %s: %v\n%s", args[0], err, out)
		}
	}
}
