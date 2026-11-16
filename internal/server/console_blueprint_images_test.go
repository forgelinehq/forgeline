package server

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/forgelinehq/forgeline/internal/blueprint"
)

func sourcedShopBody() map[string]any {
	return map[string]any{
		"name": "Shop", "target": map[string]any{"cloud": "aws", "account": "primary", "region": "ap-south-1"},
		"blocks": []map[string]any{
			{"id": "app", "kind": "service", "name": "App", "image": "nginx:1.29", "cpu": 256, "memory": 512, "count": 1, "port": 80,
				"source": map[string]any{"repo": "acme/shop", "branch": "main"}},
			{"id": "plain", "kind": "service", "name": "Plain", "image": "nginx:1.29", "cpu": 256, "memory": 512, "count": 1, "port": 80},
			{"id": "orders", "kind": "table", "name": "Orders", "partition_key": "id", "partition_type": "S"},
		},
		"links": []map[string]any{},
	}
}

func TestBlockImages(t *testing.T) {
	m := newMCPHarness(t)
	stub := &awsStub{}
	var (
		mu    sync.Mutex
		asked []string
	)
	m.srv.run = func(ctx context.Context, name string, args ...string) ([]byte, string, error) {
		if len(args) > 0 && strings.Contains(strings.Join(args, " "), "ecr describe-images") {
			mu.Lock()
			asked = append(asked, strings.Join(args, " "))
			mu.Unlock()
			return []byte(`{"imageDetails":[
				{"imageDigest":"sha256:` + strings.Repeat("a", 64) + `","imageTags":["c0ffee1"],"imagePushedAt":"2026-11-16T07:00:00+00:00","imageSizeInBytes":52428800},
				{"imageDigest":"sha256:` + strings.Repeat("b", 64) + `","imageTags":["d00d2","latest-main"],"imagePushedAt":"2026-11-16T08:00:00+00:00","imageSizeInBytes":1048576},
				{"imageDigest":"sha256:` + strings.Repeat("c", 64) + `","imagePushedAt":"2026-11-15T08:00:00+00:00"}]}`), "", nil
		}
		return stub.run(ctx, name, args...)
	}
	m.admin.expect(200, "PUT", "/v1/integrations/aws", map[string]any{"enabled": true, "profile": "dev", "region": "ap-south-1"})
	var bp BlueprintJSON
	json.Unmarshal(m.dev.expect(201, "POST", "/v1/blueprints", sourcedShopBody()), &bp)
	base := "/v1/blueprints/" + bp.ID + "/blocks/"

	// Not built yet: there is no repository to read.
	if msg := string(m.dev.expect(409, "GET", base+"app/images", nil)); !strings.Contains(msg, "build the blueprint first") {
		t.Errorf("unbuilt: %s", msg)
	}
	m.srv.docs.Put(t.Context(), kindBPState, bp.ID, blueprintState{ID: bp.ID, AWSAccount: "111122223333", Region: "ap-south-1", Serial: 1, UpdatedAt: time.Now()})

	// Blocks without a source, or that run no image, have nothing to list.
	m.dev.expect(409, "GET", base+"plain/images", nil)
	m.dev.expect(409, "GET", base+"orders/images", nil)
	m.dev.expect(404, "GET", base+"nope/images", nil)
	m.v.expect(403, "GET", base+"app/images", nil)

	repo := "111122223333.dkr.ecr.ap-south-1.amazonaws.com/" + bp.ImageRepoName("app")
	var list struct {
		Repository string
		Images     []BlockImageJSON
	}
	json.Unmarshal(m.dev.expect(200, "GET", base+"app/images", nil), &list)
	if list.Repository != repo || len(list.Images) != 3 {
		t.Fatalf("images = %+v", list)
	}
	mu.Lock()
	got := append([]string(nil), asked...)
	mu.Unlock()
	if len(got) != 1 || !strings.Contains(got[0], "--repository-name "+bp.ImageRepoName("app")) || !strings.Contains(got[0], "--region ap-south-1") {
		t.Errorf("asked ECR: %v", got)
	}
	newest := list.Images[0]
	if newest.Image != repo+":d00d2" || newest.SizeMB != 1 || newest.Current || list.Images[2].Image != repo+"@sha256:"+strings.Repeat("c", 64) {
		t.Errorf("newest first, tagged by tag, untagged by digest: %+v", list.Images)
	}

	// Images from anywhere but the block's own repository are refused.
	for _, img := range []string{"nginx:1.29", repo + "x:1", repo + ":bad tag", repo + "@sha256:abc", "evil.example/" + repo + ":1", repo} {
		if msg := string(m.dev.expect(400, "POST", base+"app/image", map[string]any{"image": img, "revision": bp.Revision})); !strings.Contains(msg, "choose an image from") {
			t.Errorf("%s: %s", img, msg)
		}
	}
	m.dev.expect(409, "POST", base+"app/image", map[string]any{"image": newest.Image, "revision": bp.Revision + 5})

	var after BlueprintJSON
	json.Unmarshal(m.dev.expect(200, "POST", base+"app/image", map[string]any{"image": newest.Image, "revision": bp.Revision}), &after)
	if after.Revision != bp.Revision+1 || after.Blocks[0].Image != newest.Image || after.Blocks[1].Image != "nginx:1.29" {
		t.Fatalf("after = rev %d, %+v", after.Revision, after.Blocks)
	}
	json.Unmarshal(m.dev.expect(200, "GET", base+"app/images", nil), &list)
	if !list.Images[0].Current || list.Images[1].Current {
		t.Errorf("current = %+v", list.Images)
	}
	// A digest works too.
	digest := repo + "@sha256:" + strings.Repeat("a", 64)
	m.dev.expect(200, "POST", base+"app/image", map[string]any{"image": digest, "revision": after.Revision})
}

func TestTemplateBlockImage(t *testing.T) {
	b := blueprint.Blueprint{Blocks: []blueprint.Block{
		{ID: "fn", Kind: blueprint.KindTemplate, Template: "aws-lambda-function@1.0.0", Inputs: map[string]any{"image": "old"}},
		{ID: "wf", Kind: blueprint.KindTemplate, Template: "aws-step-functions-workflow@1.0.0"},
	}}
	if img, ok := b.BlockImage("fn"); !ok || img != "old" {
		t.Errorf("fn = %q %v", img, ok)
	}
	if _, ok := b.BlockImage("wf"); ok {
		t.Error("a workflow runs no image")
	}
	n, err := b.WithImage("fn", "new")
	if err != nil || n.Blocks[0].Inputs["image"] != "new" || b.Blocks[0].Inputs["image"] != "old" {
		t.Errorf("with image: %v, %v, original %v", err, n.Blocks[0].Inputs, b.Blocks[0].Inputs)
	}
	if _, err := b.WithImage("wf", "new"); err == nil {
		t.Error("a workflow took an image")
	}
}
