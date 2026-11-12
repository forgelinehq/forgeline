package blueprint

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func resourcesOf(t *testing.T, code []byte) map[string]map[string]map[string]any {
	t.Helper()
	var doc struct {
		Resource map[string]map[string]map[string]any `json:"resource"`
	}
	if err := json.Unmarshal(code, &doc); err != nil {
		t.Fatal(err)
	}
	if doc.Resource == nil {
		doc.Resource = map[string]map[string]map[string]any{}
	}
	return doc.Resource
}

func TestRemovalKeepDataKeepsOnlyWhatHoldsDataStillProtected(t *testing.T) {
	code, err := webStack().RemovalTofu(RemoveKeepData, true)
	if err != nil {
		t.Fatal(err)
	}
	res := resourcesOf(t, code)
	for _, gone := range []string{"aws_ecs_service", "aws_lb", "aws_sqs_queue", "aws_elasticache_replication_group", "aws_ecs_cluster"} {
		if len(res[gone]) != 0 {
			t.Errorf("%s should be removed when keeping data, still in the code", gone)
		}
	}
	db := res["aws_db_instance"]["db"]
	if db == nil || db["deletion_protection"] != true || db["lifecycle"] == nil {
		t.Errorf("the database should stay, protected: %v", db)
	}
	if res["aws_secretsmanager_secret"]["stripe-key"] == nil {
		t.Error("the secret holds a value a person set; it should stay")
	}
}

func TestRemovalUnprotectTurnsOffProtectionOnly(t *testing.T) {
	b := webStack()
	b.Blocks = append(b.Blocks, Block{ID: "files", Kind: KindBucket, Name: "Files"}, Block{ID: "orders", Kind: KindTable, Name: "Orders", PartitionKey: "id", PartitionType: "S"})
	code, err := b.RemovalTofu(RemoveUnprotect, false)
	if err != nil {
		t.Fatal(err)
	}
	res := resourcesOf(t, code)
	db := res["aws_db_instance"]["db"]
	if db["deletion_protection"] != false || db["skip_final_snapshot"] != true || db["final_snapshot_identifier"] != nil || db["lifecycle"] != nil {
		t.Errorf("database: %v", db)
	}
	if bk := res["aws_s3_bucket"]["files"]; bk["force_destroy"] != true || bk["lifecycle"] != nil {
		t.Errorf("bucket: %v", bk)
	}
	if tb := res["aws_dynamodb_table"]["orders"]; tb["deletion_protection_enabled"] != false || tb["lifecycle"] != nil {
		t.Errorf("table: %v", tb)
	}
	if len(res["aws_ecs_service"]) != 0 {
		t.Error("everything without data goes in step 1")
	}
	// With a final snapshot asked for, it is kept.
	code, _ = b.RemovalTofu(RemoveUnprotect, true)
	if db := resourcesOf(t, code)["aws_db_instance"]["db"]; db["skip_final_snapshot"] != false || db["final_snapshot_identifier"] == nil {
		t.Errorf("final snapshot: %v", db)
	}
	// The build code is untouched by removal.
	full, _ := b.Tofu()
	if db := resourcesOf(t, full)["aws_db_instance"]["db"]; db["deletion_protection"] != true || db["lifecycle"] == nil {
		t.Errorf("the build code lost its protection: %v", db)
	}
}

func TestRemovalAllHasNoResources(t *testing.T) {
	code, err := webStack().RemovalTofu(RemoveAll, true)
	if err != nil {
		t.Fatal(err)
	}
	if res := resourcesOf(t, code); len(res) != 0 {
		t.Errorf("removing everything keeps nothing: %v", res)
	}
	if _, err := webStack().RemovalTofu("most", true); err == nil {
		t.Error("an unknown stage must be refused")
	}
}

func TestNeedsUnprotectReadsTheState(t *testing.T) {
	state := func(typ string, attrs map[string]any) []byte {
		b, _ := json.Marshal(map[string]any{"resources": []any{map[string]any{"mode": "managed", "type": typ, "instances": []any{map[string]any{"attributes": attrs}}}}})
		return b
	}
	for name, c := range map[string]struct {
		state []byte
		snap  bool
		want  bool
	}{
		"protected db":            {state("aws_db_instance", map[string]any{"deletion_protection": true, "skip_final_snapshot": false}), true, true},
		"open db, snapshot kept":  {state("aws_db_instance", map[string]any{"deletion_protection": false, "skip_final_snapshot": false}), true, false},
		"open db, wrong snapshot": {state("aws_db_instance", map[string]any{"deletion_protection": false, "skip_final_snapshot": false}), false, true},
		"protected table":         {state("aws_dynamodb_table", map[string]any{"deletion_protection_enabled": true}), true, true},
		"bucket not forced":       {state("aws_s3_bucket", map[string]any{"force_destroy": false}), true, true},
		"bucket forced":           {state("aws_s3_bucket", map[string]any{"force_destroy": true}), true, false},
		"queue only":              {state("aws_sqs_queue", map[string]any{}), true, false},
	} {
		got, err := NeedsUnprotect(c.state, c.snap)
		if err != nil || got != c.want {
			t.Errorf("%s: got %v, %v; want %v", name, got, err, c.want)
		}
	}
	if n, _ := Resources(state("aws_sqs_queue", map[string]any{})); n != 1 {
		t.Errorf("resources = %d", n)
	}
	if n, _ := Resources([]byte(`{"resources":[]}`)); n != 0 {
		t.Errorf("empty state = %d", n)
	}
}

func TestRemovalTofuValidates(t *testing.T) {
	bin := os.Getenv("FORGELINE_TOFU")
	if bin == "" {
		t.Skip("set FORGELINE_TOFU to a tofu binary to check the code with tofu validate")
	}
	b := webStack()
	b.Blocks = append(b.Blocks, Block{ID: "files", Kind: KindBucket, Name: "Files"})
	for _, stage := range []string{RemoveKeepData, RemoveUnprotect, RemoveAll} {
		dir := t.TempDir()
		code, err := b.RemovalTofu(stage, false)
		if err != nil {
			t.Fatal(err)
		}
		os.WriteFile(filepath.Join(dir, "main.tf.json"), code, 0o600)
		for _, args := range [][]string{{"init", "-backend=false", "-input=false"}, {"validate", "-no-color"}} {
			cmd := exec.Command(bin, args...)
			cmd.Dir = dir
			cmd.Env = append(os.Environ(), "TF_PLUGIN_CACHE_DIR="+os.Getenv("TF_PLUGIN_CACHE_DIR"))
			if out, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("%s: tofu %s: %v\n%s", stage, args[0], err, out)
			}
		}
	}
}
