package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"sync"
	"testing"
)

// gcloudStub stands in for the gcloud command. It records every call, so a test can prove only reads ran.
type gcloudStub struct {
	mu       sync.Mutex
	calls    []string
	assets   string // the JSON the search returns
	stderr   string // when set, `asset search-all-resources` fails with this
	noAuth   bool   // `auth list` prints nothing
	missing  bool   // the gcloud command is not installed
	project  string
	searches int
}

const gcpAssetsJSON = `[
 {"name":"//compute.googleapis.com/projects/shop-prod/zones/asia-south1-a/instances/web-1","assetType":"compute.googleapis.com/Instance","displayName":"web-1","location":"asia-south1-a","state":"RUNNING","createTime":"2026-10-01T10:00:00Z","project":"projects/123","labels":{"env":"prod","team":"payments"}},
 {"name":"//compute.googleapis.com/projects/shop-prod/zones/asia-south1-a/instances/batch-1","assetType":"compute.googleapis.com/Instance","displayName":"batch-1","location":"asia-south1-a","state":"TERMINATED","project":"projects/123"},
 {"name":"//storage.googleapis.com/shop-assets","assetType":"storage.googleapis.com/Bucket","displayName":"shop-assets","location":"ASIA-SOUTH1","project":"projects/123"},
 {"name":"//container.googleapis.com/projects/shop-prod/locations/asia-south1/clusters/main","assetType":"container.googleapis.com/Cluster","displayName":"main","location":"asia-south1","state":"RUNNING"},
 {"name":"//aiplatform.googleapis.com/projects/shop-prod/locations/asia-south1/endpoints/42","assetType":"aiplatform.googleapis.com/Endpoint","displayName":"recommender","location":"asia-south1"},
 {"name":"//aiplatform.googleapis.com/projects/shop-prod/locations/asia-south1/models/7","assetType":"aiplatform.googleapis.com/Model","displayName":"ranker-v3","location":"asia-south1"},
 {"name":"//newservice.googleapis.com/projects/shop-prod/things/t1","assetType":"newservice.googleapis.com/Thing","displayName":"a thing","location":"global"}
]`

func (g *gcloudStub) run(ctx context.Context, name string, args ...string) ([]byte, string, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.calls = append(g.calls, name+" "+strings.Join(args, " "))
	if name != "gcloud" {
		return []byte(`{}`), "", nil
	}
	if g.missing {
		return nil, "", &exec.Error{Name: "gcloud", Err: exec.ErrNotFound}
	}
	switch {
	case len(args) >= 2 && args[0] == "asset" && args[1] == "search-all-resources":
		g.searches++
		if g.stderr != "" {
			return nil, g.stderr, errors.New("exit 1")
		}
		return []byte(g.assets), "", nil
	case len(args) >= 1 && args[0] == "auth":
		if g.noAuth {
			return []byte("\n"), "", nil
		}
		return []byte("ada@example.com\n"), "", nil
	case len(args) >= 3 && args[0] == "config" && args[1] == "get-value":
		return []byte(g.project + "\n"), "", nil
	}
	return nil, "unsupported", errors.New("exit 2")
}

func (g *gcloudStub) gcloudCalls() []string {
	g.mu.Lock()
	defer g.mu.Unlock()
	var out []string
	for _, c := range g.calls {
		if strings.HasPrefix(c, "gcloud ") {
			out = append(out, c)
		}
	}
	return out
}

func gcpHarness(t *testing.T, g *gcloudStub) *mcpHarness {
	m := newMCPHarness(t)
	m.srv.run = func(ctx context.Context, name string, args ...string) ([]byte, string, error) {
		if name == "gcloud" {
			return g.run(ctx, name, args...)
		}
		return integRun(&awsStub{}, true, "")(ctx, name, args...)
	}
	return m
}

func TestGCPScopeIsCheckedAndNormalized(t *testing.T) {
	for _, c := range []struct {
		in, want string
		ok       bool
	}{
		{"shop-prod", "projects/shop-prod", true},
		{"projects/shop-prod", "projects/shop-prod", true},
		{"  folders/1234567890 ", "folders/1234567890", true},
		{"organizations/987", "organizations/987", true},
		{"", "", false},
		{"projects/", "", false},
		{"buckets/x", "", false},
		{"projects/x --format=json", "", false},
		{"--scope=projects/x", "", false},
		{"projects/-x", "", false},
		{"projects/" + strings.Repeat("a", 80), "", false},
		{"projects/a;rm", "", false},
	} {
		got, err := normalizeGCPScope(c.in)
		if (err == nil) != c.ok || got != c.want {
			t.Errorf("normalizeGCPScope(%q) = %q, %v; want %q ok=%v", c.in, got, err, c.want, c.ok)
		}
	}
}

func TestGCloudErrorsSayWhatToDo(t *testing.T) {
	for _, c := range []struct{ stderr, want string }{
		{"ERROR: (gcloud.asset) You do not currently have an active account selected.", "gcloud auth login"},
		{"Reauthentication failed. cannot prompt during non-interactive execution.", "gcloud auth login"},
		{"ERROR: ... PERMISSION_DENIED: The caller does not have permission", "roles/cloudasset.viewer"},
		{"HttpError 403: Cloud Asset API has not been used in project 123 before or it is disabled. SERVICE_DISABLED", "cloudasset.googleapis.com"},
		{"ERROR: ... 404 Project not found. NOT_FOUND", "could not find that scope"},
		{"something unexpected", "something unexpected"},
	} {
		if err := gcloudError(c.stderr, errors.New("exit 1")); !strings.Contains(err.Error(), c.want) || len(err.Error()) > 400 {
			t.Errorf("%q -> %q, want it to contain %q", c.stderr, err, c.want)
		}
	}
	if err := gcloudError("", &exec.Error{Name: "gcloud", Err: exec.ErrNotFound}); !strings.Contains(err.Error(), "not installed") {
		t.Errorf("a missing command: %v", err)
	}
}

func TestGCPConnectsOnlyWhenTheLoginCanReadTheScope(t *testing.T) {
	g := &gcloudStub{assets: gcpAssetsJSON, project: "shop-prod"}
	m := gcpHarness(t, g)
	m.dev.expect(403, "PUT", "/v1/integrations/gcp", map[string]any{"enabled": true, "scope": "shop-prod"}) // administrators only

	// Before connecting, the detected default project is offered by name.
	var view GCPSettingsJSON
	json.Unmarshal(m.dev.expect(200, "GET", "/v1/integrations/gcp", nil), &view)
	if view.Enabled || view.DetectedProject != "shop-prod" {
		t.Errorf("view = %+v", view)
	}
	m.v.expect(403, "GET", "/v1/integrations/gcp", nil)

	// A bad scope is refused before any command runs.
	before := len(g.gcloudCalls())
	m.admin.expect(400, "PUT", "/v1/integrations/gcp", map[string]any{"enabled": true, "scope": "projects/x --format=json"})
	m.admin.expect(400, "PUT", "/v1/integrations/gcp", map[string]any{"enabled": true, "scope": ""})
	if len(g.gcloudCalls()) != before {
		t.Errorf("a refused scope still ran gcloud: %v", g.gcloudCalls()[before:])
	}

	// A login that is denied is reported with what to grant, and nothing is saved.
	g.stderr = "ERROR: PERMISSION_DENIED: The caller does not have permission"
	var e struct{ Error string }
	json.Unmarshal(m.admin.expect(422, "PUT", "/v1/integrations/gcp", map[string]any{"enabled": true, "scope": "shop-prod"}), &e)
	if !strings.Contains(e.Error, "roles/cloudasset.viewer") {
		t.Errorf("error = %q", e.Error)
	}
	json.Unmarshal(m.dev.expect(200, "GET", "/v1/integrations/gcp", nil), &view)
	if view.Enabled {
		t.Errorf("a refused connection was saved: %+v", view)
	}

	// With read access it connects, normalizes a bare project id, and is audited.
	g.stderr = ""
	json.Unmarshal(m.admin.expect(200, "PUT", "/v1/integrations/gcp", map[string]any{"enabled": true, "scope": "shop-prod"}), &view)
	if !view.Enabled || view.Scope != "projects/shop-prod" {
		t.Errorf("view = %+v", view)
	}
	if !strings.Contains(string(m.admin.expect(200, "GET", "/v1/audit", nil)), "integration.gcp.update") {
		t.Error("the change was not audited")
	}
	// Turning it off needs no scope and no call.
	n := len(g.gcloudCalls())
	m.admin.expect(200, "PUT", "/v1/integrations/gcp", map[string]any{"enabled": false})
	if len(g.gcloudCalls()) != n {
		t.Error("turning it off ran gcloud")
	}
}

func TestGCPInventoryGroupsEveryResourceAndHidesNone(t *testing.T) {
	g := &gcloudStub{assets: gcpAssetsJSON}
	m := gcpHarness(t, g)
	var inv AWSInventoryJSON
	json.Unmarshal(m.v.expect(200, "GET", "/v1/infra/gcp/resources", nil), &inv)
	if inv.Enabled || len(inv.Services) != 0 {
		t.Fatalf("not connected should be empty: %+v", inv)
	}
	if len(g.gcloudCalls()) != 0 {
		t.Errorf("not connected still ran gcloud: %v", g.gcloudCalls())
	}
	m.admin.expect(200, "PUT", "/v1/integrations/gcp", map[string]any{"enabled": true, "scope": "projects/shop-prod"})
	json.Unmarshal(m.v.expect(200, "GET", "/v1/infra/gcp/resources", nil), &inv) // a viewer may read it
	if !inv.Enabled || inv.Region != "projects/shop-prod" || inv.Error != "" {
		t.Fatalf("inventory = %+v", inv)
	}
	byID := map[string]AWSServiceJSON{}
	total := 0
	for _, s := range inv.Services {
		byID[s.ID] = s
		total += s.Count
	}
	if total != 7 {
		t.Errorf("%d resources shown, 7 were found", total)
	}
	vms := byID["gce"]
	if vms.Count != 2 || vms.Category != "Compute" || vms.Resources[0].Name != "batch-1" { // sorted by name
		t.Errorf("vms = %+v", vms)
	}
	web := vms.Resources[1]
	if web.State != "RUNNING" || web.Tags["env"] != "prod" || web.Tags["team"] != "payments" || web.ID != "web-1" || !strings.HasPrefix(web.ARN, "//compute.googleapis.com/") {
		t.Errorf("web-1 = %+v", web)
	}
	if v := byID["vertex-endpoint"]; v.Category != "AI" || v.Count != 1 || v.Resources[0].Name != "recommender" {
		t.Errorf("vertex endpoint = %+v", v)
	}
	if byID["vertex-model"].Count != 1 || byID["gcs"].Count != 1 || byID["gke"].Count != 1 {
		t.Errorf("services = %+v", inv.Services)
	}
	// A type we do not know is listed under "Other resources" with its type, not dropped.
	other := byID["other"]
	if other.Count != 1 || other.Resources[0].Fields[0].Label != "Type" || other.Resources[0].Fields[0].Value != "newservice.googleapis.com/Thing" {
		t.Errorf("other = %+v", other)
	}
	for _, s := range inv.Services {
		if s.Resources == nil {
			t.Errorf("%s has a null resource list", s.ID)
		}
	}
}

func TestGCPInventoryIsCachedCappedAndOnlyReads(t *testing.T) {
	var sb strings.Builder
	sb.WriteString("[")
	for i := 0; i < 250; i++ {
		if i > 0 {
			sb.WriteString(",")
		}
		fmt.Fprintf(&sb, `{"name":"//compute.googleapis.com/projects/p/zones/z/instances/vm-%03d","assetType":"compute.googleapis.com/Instance","displayName":"vm-%03d"}`, i, i)
	}
	sb.WriteString("]")
	g := &gcloudStub{assets: sb.String()}
	m := gcpHarness(t, g)
	m.admin.expect(200, "PUT", "/v1/integrations/gcp", map[string]any{"enabled": true, "scope": "projects/p"})
	g.mu.Lock()
	g.searches = 0
	g.mu.Unlock()

	var inv AWSInventoryJSON
	json.Unmarshal(m.v.expect(200, "GET", "/v1/infra/gcp/resources", nil), &inv)
	vms := inv.Services[0]
	if vms.Count != 250 || len(vms.Resources) != inventoryMax || !vms.Truncated {
		t.Errorf("count %d shown %d truncated %v; want the true count with %d shown", vms.Count, len(vms.Resources), vms.Truncated, inventoryMax)
	}
	m.v.expect(200, "GET", "/v1/infra/gcp/resources", nil)
	if g.searches != 1 {
		t.Errorf("%d searches for two reads; the second should come from the cache", g.searches)
	}
	m.v.expect(200, "GET", "/v1/infra/gcp/resources?refresh=1", nil)
	if g.searches != 2 {
		t.Errorf("refresh did not skip the cache: %d searches", g.searches)
	}
	// Changing the scope must not show the old scope's inventory.
	m.admin.expect(200, "PUT", "/v1/integrations/gcp", map[string]any{"enabled": true, "scope": "projects/other"})
	m.v.expect(200, "GET", "/v1/infra/gcp/resources", nil)
	if g.searches != 4 { // one to prove the login when saving, one to read
		t.Errorf("searches = %d after a scope change", g.searches)
	}

	// Turning it off and on again must not show an inventory from before.
	m.admin.expect(200, "PUT", "/v1/integrations/gcp", map[string]any{"enabled": false})
	m.admin.expect(200, "PUT", "/v1/integrations/gcp", map[string]any{"enabled": true, "scope": "projects/other"})
	n := g.searches
	m.v.expect(200, "GET", "/v1/infra/gcp/resources", nil)
	if g.searches != n+1 {
		t.Errorf("an inventory read before turning it off was shown again (%d searches, want %d)", g.searches, n+1)
	}

	// Every gcloud command is one of three reads, and the scope is only ever passed as --scope=<kind>/<id>.
	for _, c := range g.gcloudCalls() {
		f := strings.Fields(c)
		ok := (len(f) > 3 && f[1] == "asset" && f[2] == "search-all-resources") || (len(f) > 2 && f[1] == "auth" && f[2] == "list") || (len(f) > 3 && f[1] == "config" && f[2] == "get-value")
		if !ok {
			t.Errorf("gcloud ran %q, which is not one of the three read commands", c)
		}
		if f[1] == "asset" {
			var scope string
			for _, a := range f {
				if strings.HasPrefix(a, "--scope=") {
					scope = strings.TrimPrefix(a, "--scope=")
				}
			}
			if !gcpScopeRE.MatchString(scope) {
				t.Errorf("the scope in %q is not a plain scope", c)
			}
			// Nothing else may ride along: each argument is one we chose, and the result limit is bounded.
			for _, arg := range f[4:] {
				switch {
				case strings.HasPrefix(arg, "--scope="), arg == "--format=json", arg == "--quiet", strings.HasPrefix(arg, "--read-mask="):
				case strings.HasPrefix(arg, "--limit="):
					var n int
					fmt.Sscanf(strings.TrimPrefix(arg, "--limit="), "%d", &n)
					if n < 1 || n > 5000 {
						t.Errorf("the result limit in %q is not between 1 and 5000", c)
					}
				default:
					t.Errorf("gcloud was given an unexpected argument %q in %q", arg, c)
				}
			}
		}
	}
}

func TestGCPInventoryErrorsAreShownNotHidden(t *testing.T) {
	g := &gcloudStub{assets: gcpAssetsJSON}
	m := gcpHarness(t, g)
	m.admin.expect(200, "PUT", "/v1/integrations/gcp", map[string]any{"enabled": true, "scope": "projects/shop-prod"})
	g.mu.Lock()
	g.stderr = "ERROR: ... You do not currently have an active account selected."
	g.mu.Unlock()
	var inv AWSInventoryJSON
	json.Unmarshal(m.v.expect(200, "GET", "/v1/infra/gcp/resources?refresh=1", nil), &inv)
	if !strings.Contains(inv.Error, "gcloud auth login") || len(inv.Services) != 0 {
		t.Errorf("inventory = %+v", inv)
	}
	// A failure is not cached: once the login works the next read shows the resources.
	g.mu.Lock()
	g.stderr = ""
	g.mu.Unlock()
	inv = AWSInventoryJSON{} // a fresh value: Unmarshal would keep the old error
	json.Unmarshal(m.v.expect(200, "GET", "/v1/infra/gcp/resources", nil), &inv)
	if inv.Error != "" || len(inv.Services) == 0 {
		t.Errorf("after the login came back: %+v", inv)
	}
}

func TestGCPHubStatusAndTest(t *testing.T) {
	g := &gcloudStub{assets: gcpAssetsJSON}
	m := gcpHarness(t, g)
	if it := hub(t, m.dev)["gcp"]; it.Status != intOff {
		t.Errorf("before connecting = %+v", it)
	}
	var res IntegrationTestJSON
	json.Unmarshal(m.admin.expect(200, "POST", "/v1/integrations/gcp/checks", nil), &res)
	if res.Failed != 1 || res.Checks[0].Fix == "" {
		t.Errorf("not connected = %+v", res)
	}
	m.admin.expect(200, "PUT", "/v1/integrations/gcp", map[string]any{"enabled": true, "scope": "projects/shop-prod"})
	if it := hub(t, m.dev)["gcp"]; it.Status != intConnected || !strings.Contains(it.Summary, "projects/shop-prod") {
		t.Errorf("connected = %+v", it)
	}
	json.Unmarshal(m.admin.expect(200, "POST", "/v1/integrations/gcp/checks", nil), &res)
	if res.Failed != 0 || res.Passed != 4 || res.Changed {
		t.Errorf("test = %+v", res)
	}
	if !strings.Contains(res.Checks[1].Detail, "ada@example.com") {
		t.Errorf("the login check should say who is signed in: %+v", res.Checks[1])
	}

	// No login, then a login without access: each failure says what to do.
	g.noAuth = true
	json.Unmarshal(m.admin.expect(200, "POST", "/v1/integrations/gcp/checks", nil), &res)
	if res.Failed != 1 || !strings.Contains(res.Checks[len(res.Checks)-1].Fix, "gcloud auth login") {
		t.Errorf("no login = %+v", res)
	}
	g.noAuth = false
	g.stderr = "PERMISSION_DENIED"
	json.Unmarshal(m.admin.expect(200, "POST", "/v1/integrations/gcp/checks", nil), &res)
	last := res.Checks[len(res.Checks)-1]
	if res.Failed != 1 || !strings.Contains(last.Detail, "roles/cloudasset.viewer") || !strings.Contains(last.Fix, "Cloud Asset") {
		t.Errorf("no access = %+v", res)
	}
	// gcloud not installed at all.
	g.stderr, g.missing = "", true
	json.Unmarshal(m.admin.expect(200, "POST", "/v1/integrations/gcp/checks", nil), &res)
	if res.Failed != 1 || !strings.Contains(res.Checks[len(res.Checks)-1].Detail, "not installed") {
		t.Errorf("no gcloud = %+v", res)
	}
}

// The console checks a scope before asking, with the same pattern. If the two drift apart, a person would be
// told a good scope is wrong, or sent a bad one.
func TestTheConsoleChecksAGoogleScopeWithTheSamePattern(t *testing.T) {
	js, err := uiFiles.ReadFile("ui/lib/hub.js")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.ReplaceAll(string(js), `\/`, `/`), gcpScopeRE.String()) {
		t.Errorf("ui/lib/hub.js does not contain the server's scope pattern %s", gcpScopeRE)
	}
}
