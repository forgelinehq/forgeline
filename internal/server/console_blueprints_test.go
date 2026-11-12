package server

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/forgelinehq/forgeline/internal/authn"
	"github.com/forgelinehq/forgeline/internal/blueprint"
)

func bpBody(name string) map[string]any {
	return map[string]any{
		"name": name, "target": map[string]any{"cloud": "aws", "account": "primary", "region": "ap-south-1"},
		"blocks": []map[string]any{
			{"id": "web", "kind": "frontdoor", "name": "Front door"},
			{"id": "app", "kind": "service", "name": "App", "image": "nginx:1.29", "cpu": 256, "memory": 512, "count": 1, "port": 80, "env": map[string]string{"GREETING": "hi ${x}"}},
			{"id": "data", "kind": "table", "name": "Data", "partition_key": "id", "partition_type": "S"},
		},
		"links": []map[string]any{{"from": "web", "to": "app"}, {"from": "app", "to": "data", "access": "readwrite"}},
	}
}

func TestBlueprintsAreDraftsWithChecksAndCode(t *testing.T) {
	m := newMCPHarness(t)
	m.admin.createUser("ben", authn.Developer)
	ben := m.login("ben")
	m.admin.expect(200, "PUT", "/v1/integrations/aws", map[string]any{"enabled": true, "profile": "dev", "region": "ap-south-1"})

	m.v.expect(403, "GET", "/v1/blueprints", nil)
	m.v.expect(403, "POST", "/v1/blueprints", bpBody("x"))
	bad := bpBody("Bad")
	bad["links"] = []map[string]any{{"from": "data", "to": "app", "access": "read"}}
	if out := string(m.dev.expect(400, "POST", "/v1/blueprints", bad)); !strings.Contains(out, "links[0]") {
		t.Errorf("an invalid blueprint must say where: %s", out)
	}
	other := bpBody("Elsewhere")
	other["target"] = map[string]any{"cloud": "aws", "account": "nope", "region": "ap-south-1"}
	m.dev.expect(400, "POST", "/v1/blueprints", other)

	var made BlueprintJSON
	json.Unmarshal(m.dev.expect(201, "POST", "/v1/blueprints", bpBody("Shop")), &made)
	if !strings.HasPrefix(made.ID, "bp-") || made.Revision != 1 || made.CreatedBy == "" || len(made.Checks) == 0 {
		t.Fatalf("made = %+v", made)
	}
	var list struct{ Blueprints []BlueprintSummaryJSON }
	json.Unmarshal(m.dev.expect(200, "GET", "/v1/blueprints", nil), &list)
	if len(list.Blueprints) != 1 || list.Blueprints[0].Blocks != 3 || list.Blueprints[0].Warnings == 0 {
		t.Errorf("list = %+v", list)
	}

	// The code is OpenTofu JSON, with what people typed escaped.
	code := string(m.dev.expect(200, "GET", "/v1/blueprints/"+made.ID+"/code", nil))
	if !strings.Contains(code, `"hashicorp/aws"`) || !strings.Contains(code, `hi $${x}`) || !strings.Contains(code, `"aws_dynamodb_table"`) {
		t.Errorf("code: %.400s", code)
	}

	// Edits must start from the latest revision.
	upd := bpBody("Shop v2")
	upd["revision"] = 1
	m.dev.expect(200, "PUT", "/v1/blueprints/"+made.ID, upd)
	if out := string(ben.expect(409, "PUT", "/v1/blueprints/"+made.ID, upd)); !strings.Contains(out, "changed this blueprint") {
		t.Errorf("a stale edit must be refused: %s", out)
	}
	upd["revision"] = 2
	upd["blocks"] = []map[string]any{{"id": "web", "kind": "frontdoor", "name": "Front door"}}
	upd["links"] = []map[string]any{}
	m.dev.expect(400, "PUT", "/v1/blueprints/"+made.ID, upd) // a front door with nothing behind it

	// Check works on drafts that are not saved.
	var chk struct {
		Error  string
		Checks []map[string]string
	}
	json.Unmarshal(m.dev.expect(200, "POST", "/v1/blueprints/check", upd), &chk)
	if !strings.Contains(chk.Error, "exactly one service") {
		t.Errorf("check = %+v", chk)
	}
	chk.Error = ""
	json.Unmarshal(m.dev.expect(200, "POST", "/v1/blueprints/check", bpBody("ok")), &chk)
	if chk.Error != "" || len(chk.Checks) == 0 {
		t.Errorf("check = %+v", chk)
	}

	// Only the author or an administrator deletes.
	ben.expect(403, "DELETE", "/v1/blueprints/"+made.ID, nil)
	m.admin.expect(204, "DELETE", "/v1/blueprints/"+made.ID, nil)
	m.dev.expect(404, "GET", "/v1/blueprints/"+made.ID, nil)
	a := string(m.admin.expect(200, "GET", "/v1/audit?action=blueprint", nil))
	for _, want := range []string{"blueprint.create", "blueprint.update", "blueprint.delete"} {
		if !strings.Contains(a, want) {
			t.Errorf("audit misses %s", want)
		}
	}
}

// A block with a source gets a suggested workflow and pipeline; nothing is written anywhere.
func TestBlueprintDeliverySuggestion(t *testing.T) {
	m := newMCPHarness(t)
	body := bpBody("Shop")
	blocks := body["blocks"].([]map[string]any)
	blocks[1]["source"] = map[string]any{"repo": "acme/shop", "path": "app"}
	var made BlueprintJSON
	json.Unmarshal(m.dev.expect(201, "POST", "/v1/blueprints", body), &made)
	var d blueprint.Delivery
	json.Unmarshal(m.dev.expect(200, "GET", "/v1/blueprints/"+made.ID+"/blocks/app/delivery", nil), &d)
	if d.Repo != "acme/shop" || d.Branch != "main" || !strings.Contains(d.Workflow, "context: app") || !strings.Contains(d.Next, "<account-id>") {
		t.Errorf("delivery = %+v", d)
	}
	m.dev.expect(404, "GET", "/v1/blueprints/"+made.ID+"/blocks/data/delivery", nil) // a table has no source
	m.dev.expect(404, "GET", "/v1/blueprints/"+made.ID+"/blocks/ghost/delivery", nil)
	m.v.expect(403, "GET", "/v1/blueprints/"+made.ID+"/blocks/app/delivery", nil)
}
