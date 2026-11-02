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

const (
	azSubA = "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa"
	azSubB = "bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb"
)

// azStub stands in for the az command. It records every call so a test can prove only reads ran.
type azStub struct {
	mu      sync.Mutex
	calls   []string
	rows    []map[string]any // what Resource Graph holds
	stderr  string           // when set, `graph query` fails with this
	noLogin bool             // `account show` fails
	missing bool             // the az command is not installed
	queries int
}

func azRow(typ, name string, extra map[string]any) map[string]any {
	m := map[string]any{"id": "/subscriptions/" + azSubA + "/resourceGroups/rg1/providers/" + typ + "/" + name, "name": name, "type": typ, "location": "centralindia", "resourceGroup": "rg1", "subscriptionId": azSubA}
	for k, v := range extra {
		m[k] = v
	}
	return m
}

func azBasicRows() []map[string]any {
	return []map[string]any{
		azRow("Microsoft.Compute/virtualMachines", "web-1", map[string]any{"state": "Succeeded", "tags": map[string]any{"env": "prod", "team": "payments"}}),
		azRow("Microsoft.Compute/virtualMachines", "batch-1", map[string]any{"state": "Succeeded"}),
		azRow("Microsoft.Storage/storageAccounts", "shopassets", map[string]any{"kind": "StorageV2"}),
		azRow("Microsoft.ContainerService/managedClusters", "aks-main", nil),
		azRow("Microsoft.CognitiveServices/accounts", "shop-openai", map[string]any{"kind": "OpenAI"}),
		azRow("Microsoft.MachineLearningServices/workspaces", "foundry-hub", map[string]any{"kind": "Hub"}),
		azRow("Microsoft.NewThing/things", "t1", nil),
	}
}

func (a *azStub) run(ctx context.Context, name string, args ...string) ([]byte, string, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.calls = append(a.calls, name+" "+strings.Join(args, " "))
	if name != "az" {
		return []byte(`{}`), "", nil
	}
	if a.missing {
		return nil, "", &exec.Error{Name: "az", Err: exec.ErrNotFound}
	}
	switch {
	case len(args) >= 2 && args[0] == "graph" && args[1] == "query":
		a.queries++
		if a.stderr != "" {
			return nil, a.stderr, errors.New("exit 1")
		}
		first, skip := 1000, 0
		for i, x := range args {
			if x == "--first" {
				fmt.Sscanf(args[i+1], "%d", &first)
			}
			if x == "--skip" {
				fmt.Sscanf(args[i+1], "%d", &skip)
			}
		}
		end := min(skip+first, len(a.rows))
		page := []map[string]any{}
		if skip < len(a.rows) {
			page = a.rows[skip:end]
		}
		b, _ := json.Marshal(map[string]any{"data": page, "count": len(page), "total_records": len(a.rows)})
		return b, "", nil
	case len(args) >= 2 && args[0] == "account" && args[1] == "show":
		if a.noLogin {
			return nil, "Please run 'az login' to set up an account.", errors.New("exit 1")
		}
		return []byte(`"ada@example.com"`), "", nil
	case len(args) >= 2 && args[0] == "account" && args[1] == "list":
		return []byte(`[{"id":"` + azSubB + `","name":"Staging","state":"Enabled"},{"id":"` + azSubA + `","name":"Production","state":"Enabled"},{"id":"not-a-guid","name":"Bad","state":"Enabled"}]`), "", nil
	}
	return nil, "unsupported", errors.New("exit 2")
}

func (a *azStub) azCalls() []string {
	a.mu.Lock()
	defer a.mu.Unlock()
	var out []string
	for _, c := range a.calls {
		if strings.HasPrefix(c, "az ") {
			out = append(out, c)
		}
	}
	return out
}

func azHarness(t *testing.T, a *azStub) *mcpHarness {
	m := newMCPHarness(t)
	m.srv.run = func(ctx context.Context, name string, args ...string) ([]byte, string, error) {
		if name == "az" {
			return a.run(ctx, name, args...)
		}
		return integRun(&awsStub{}, true, "")(ctx, name, args...)
	}
	return m
}

func TestAzureSubscriptionsAreCheckedAsGuids(t *testing.T) {
	many := make([]string, azureMaxSubs+1)
	for i := range many {
		many[i] = fmt.Sprintf("%08d-0000-0000-0000-000000000000", i+1)
	}
	for _, c := range []struct {
		in   []string
		want string
		ok   bool
	}{
		{nil, "", true},
		{[]string{azSubA}, azSubA, true},
		{[]string{" " + strings.ToUpper(azSubB) + " ", azSubA, azSubA}, azSubA + "," + azSubB, true}, // lower-cased, de-duplicated, sorted
		{[]string{"my-subscription"}, "", false},
		{[]string{azSubA + " --query x"}, "", false},
		{[]string{"--subscriptions"}, "", false},
		{[]string{"--subscriptions " + azSubA}, "", false}, // a flag in front of a valid id
		{[]string{"x" + azSubA}, "", false},
		{[]string{azSubA + ";rm"}, "", false},
		{many, "", false},
	} {
		got, err := normalizeAzureSubs(c.in)
		if (err == nil) != c.ok || strings.Join(got, ",") != c.want {
			t.Errorf("normalizeAzureSubs(%q) = %v, %v; want %q ok=%v", c.in, got, err, c.want, c.ok)
		}
	}
}

func TestAzErrorsSayWhatToDo(t *testing.T) {
	for _, c := range []struct{ stderr, want string }{
		{"Please run 'az login' to set up an account.", "az login --use-device-code"},
		{"AADSTS70043: The refresh token has expired", "az login --use-device-code"},
		{"ERROR: 'graph' is misspelled or not recognized by the system.", "az extension add --name resource-graph"},
		{"(AuthorizationFailed) The client does not have authorization to perform action", "Reader role"},
		{"(SubscriptionNotFound) Subscription x was not found", "could not find one of those subscriptions"},
		{"something unexpected", "something unexpected"},
	} {
		if err := azError(c.stderr, errors.New("exit 1")); !strings.Contains(err.Error(), c.want) || len(err.Error()) > 400 {
			t.Errorf("%q -> %q, want it to contain %q", c.stderr, err, c.want)
		}
	}
	if err := azError("", &exec.Error{Name: "az", Err: exec.ErrNotFound}); !strings.Contains(err.Error(), "not installed") {
		t.Errorf("a missing command: %v", err)
	}
}

func TestAzureConnectsOnlyWhenTheLoginCanQuery(t *testing.T) {
	a := &azStub{rows: azBasicRows()}
	m := azHarness(t, a)
	m.dev.expect(403, "PUT", "/v1/integrations/azure", map[string]any{"enabled": true})
	m.v.expect(403, "GET", "/v1/integrations/azure", nil)

	// The subscriptions the login can see are offered to administrators only, with ids that are not GUIDs left out.
	var view AzureSettingsJSON
	json.Unmarshal(m.dev.expect(200, "GET", "/v1/integrations/azure", nil), &view)
	if view.Enabled || len(view.Available) != 0 {
		t.Errorf("a developer must not see the subscription list: %+v", view)
	}
	json.Unmarshal(m.admin.expect(200, "GET", "/v1/integrations/azure", nil), &view)
	if len(view.Available) != 2 || view.Available[0].Name != "Production" || view.Available[1].Name != "Staging" {
		t.Errorf("available = %+v", view.Available)
	}

	// A subscription that is not an id is refused before any command runs.
	before := len(a.azCalls())
	m.admin.expect(400, "PUT", "/v1/integrations/azure", map[string]any{"enabled": true, "subscriptions": []string{"prod; rm -rf"}})
	if len(a.azCalls()) != before {
		t.Errorf("a refused list still ran az: %v", a.azCalls()[before:])
	}

	// A login that is not allowed to read is refused with what to grant, and nothing is saved.
	a.stderr = "(AuthorizationFailed) The client does not have authorization"
	var e struct{ Error string }
	json.Unmarshal(m.admin.expect(422, "PUT", "/v1/integrations/azure", map[string]any{"enabled": true}), &e)
	if !strings.Contains(e.Error, "Reader role") {
		t.Errorf("error = %q", e.Error)
	}
	view = AzureSettingsJSON{}
	json.Unmarshal(m.dev.expect(200, "GET", "/v1/integrations/azure", nil), &view)
	if view.Enabled {
		t.Errorf("a refused connection was saved: %+v", view)
	}

	a.stderr = ""
	view = AzureSettingsJSON{}
	json.Unmarshal(m.admin.expect(200, "PUT", "/v1/integrations/azure", map[string]any{"enabled": true, "subscriptions": []string{strings.ToUpper(azSubA)}}), &view)
	if !view.Enabled || len(view.Subscriptions) != 1 || view.Subscriptions[0] != azSubA {
		t.Errorf("view = %+v", view)
	}
	if !strings.Contains(string(m.admin.expect(200, "GET", "/v1/audit", nil)), "integration.azure.update") {
		t.Error("the change was not audited")
	}
	n := len(a.azCalls())
	m.admin.expect(200, "PUT", "/v1/integrations/azure", map[string]any{"enabled": false})
	if len(a.azCalls()) != n {
		t.Error("turning it off ran az")
	}
}

func TestAzureInventoryGroupsEveryResourceAndHidesNone(t *testing.T) {
	a := &azStub{rows: azBasicRows()}
	m := azHarness(t, a)
	var inv AWSInventoryJSON
	json.Unmarshal(m.v.expect(200, "GET", "/v1/infra/azure/resources", nil), &inv)
	if inv.Enabled || len(inv.Services) != 0 || len(a.azCalls()) != 0 {
		t.Fatalf("not connected should be empty and run nothing: %+v %v", inv, a.azCalls())
	}
	m.admin.expect(200, "PUT", "/v1/integrations/azure", map[string]any{"enabled": true})
	a.mu.Lock()
	queriesBefore := a.queries
	a.mu.Unlock()
	inv = AWSInventoryJSON{}
	json.Unmarshal(m.v.expect(200, "GET", "/v1/infra/azure/resources", nil), &inv) // a viewer may read it
	if !inv.Enabled || inv.Error != "" || inv.Notice != "" || !strings.Contains(inv.Region, "every subscription") {
		t.Fatalf("inventory = %+v", inv)
	}
	if n := a.queries - queriesBefore; n != 1 {
		t.Errorf("a short page means there is no more; the read ran %d queries, want 1", n)
	}
	byID := map[string]AWSServiceJSON{}
	total := 0
	for _, s := range inv.Services {
		byID[s.ID] = s
		total += s.Count
		if s.Resources == nil {
			t.Errorf("%s has a null resource list", s.ID)
		}
	}
	if total != 7 {
		t.Errorf("%d resources shown, 7 exist", total)
	}
	vms := byID["vm"]
	if vms.Count != 2 || vms.Category != "Compute" || vms.Resources[0].Name != "batch-1" {
		t.Errorf("vms = %+v", vms)
	}
	web := vms.Resources[1]
	if web.State != "Succeeded" || web.Tags["env"] != "prod" || !strings.HasPrefix(web.ARN, "/subscriptions/") {
		t.Errorf("web-1 = %+v", web)
	}
	// Azure OpenAI and AI Foundry workspaces are the AI group.
	if byID["ai-services"].Category != "AI" || byID["ai-services"].Resources[0].Name != "shop-openai" || byID["ml"].Category != "AI" || byID["ml"].Count != 1 {
		t.Errorf("AI services = %+v / %+v", byID["ai-services"], byID["ml"])
	}
	if byID["storage"].Count != 1 || byID["aks"].Count != 1 {
		t.Errorf("services = %+v", inv.Services)
	}
	// A type we do not know is under "Other resources" with its type, not dropped.
	other := byID["other"]
	if other.Count != 1 || other.Resources[0].Fields[0].Value != "microsoft.newthing/things" {
		t.Errorf("other = %+v", other)
	}
}

func TestAzureInventoryPagesCapsCachesAndOnlyRunsTheFixedQuery(t *testing.T) {
	rows := make([]map[string]any, 0, 3500)
	for i := 0; i < 3500; i++ {
		rows = append(rows, azRow("Microsoft.Compute/virtualMachines", fmt.Sprintf("vm-%04d", i), nil))
	}
	a := &azStub{rows: rows}
	m := azHarness(t, a)
	m.admin.expect(200, "PUT", "/v1/integrations/azure", map[string]any{"enabled": true, "subscriptions": []string{azSubA, azSubB}})
	a.mu.Lock()
	a.queries, a.calls = 0, nil
	a.mu.Unlock()

	var inv AWSInventoryJSON
	json.Unmarshal(m.v.expect(200, "GET", "/v1/infra/azure/resources", nil), &inv)
	vms := inv.Services[0]
	if vms.Count != azureAssetMax || len(vms.Resources) != inventoryMax || !vms.Truncated {
		t.Errorf("count %d shown %d truncated %v; want %d read, %d shown", vms.Count, len(vms.Resources), vms.Truncated, azureAssetMax, inventoryMax)
	}
	if !strings.Contains(inv.Notice, "first 3000") {
		t.Errorf("reaching the cap must be said, notice = %q", inv.Notice)
	}
	if a.queries != 3 {
		t.Errorf("%d queries for 3000 resources, want 3 pages of 1000", a.queries)
	}
	m.v.expect(200, "GET", "/v1/infra/azure/resources", nil)
	if a.queries != 3 {
		t.Errorf("the second read ran %d more queries; it should come from the cache", a.queries-3)
	}
	m.v.expect(200, "GET", "/v1/infra/azure/resources?refresh=1", nil)
	if a.queries != 6 {
		t.Errorf("refresh did not skip the cache: %d queries", a.queries)
	}
	// Turning it off and on must not show an inventory from before.
	m.admin.expect(200, "PUT", "/v1/integrations/azure", map[string]any{"enabled": false})
	m.admin.expect(200, "PUT", "/v1/integrations/azure", map[string]any{"enabled": true, "subscriptions": []string{azSubA, azSubB}})
	q := a.queries
	m.v.expect(200, "GET", "/v1/infra/azure/resources", nil)
	if a.queries <= q+1 { // one query was the proof when it was turned on; the read needs its own pages
		t.Errorf("an inventory from before turning it off was shown again (%d -> %d queries)", q, a.queries)
	}

	// Every az command is one of three reads, and the query is always the constant.
	for _, c := range a.azCalls() {
		f := strings.Fields(c)
		isGraph := len(f) > 2 && f[1] == "graph" && f[2] == "query"
		isList := len(f) > 2 && f[1] == "account" && (f[2] == "list" || f[2] == "show")
		if !isGraph && !isList {
			t.Errorf("az ran %q, which is not one of the three read commands", c)
			continue
		}
		if !strings.Contains(c, "--only-show-errors --output json") {
			t.Errorf("%q does not ask for quiet JSON", c)
		}
		if !isGraph {
			continue
		}
		if !strings.Contains(c, "-q "+azureQuery+" --first") {
			t.Errorf("the query in %q is not the fixed one", c)
		}
		// After the constant query: only --first N, --skip N, --subscriptions GUIDs, and the two output flags.
		rest := strings.Fields(strings.SplitN(c, azureQuery, 2)[1])
		mode := ""
		for _, x := range rest {
			switch {
			case x == "--first" || x == "--skip" || x == "--subscriptions" || x == "--only-show-errors" || x == "--output":
				mode = x
			case mode == "--first":
				var n int
				if _, err := fmt.Sscanf(x, "%d", &n); err != nil || n < 1 || n > azurePage {
					t.Errorf("--first %q in %q is not between 1 and %d", x, c, azurePage)
				}
			case mode == "--skip":
				var n int
				if _, err := fmt.Sscanf(x, "%d", &n); err != nil || n%azurePage != 0 {
					t.Errorf("--skip %q in %q is not a multiple of a page", x, c)
				}
			case mode == "--subscriptions":
				if !azureSubRE.MatchString(x) {
					t.Errorf("a value that is not a subscription id reached --subscriptions: %q in %q", x, c)
				}
			case mode == "--output":
				if x != "json" {
					t.Errorf("unexpected output format in %q", c)
				}
			default:
				t.Errorf("unexpected argument %q in %q", x, c)
			}
		}
	}
}

// A stand-in that always answers with a full page would loop forever without a bound.
func TestAzureQueryStopsEvenIfTheAnswerNeverEnds(t *testing.T) {
	m := newMCPHarness(t)
	calls := 0
	page := make([]map[string]any, 0, azurePage)
	for i := 0; i < azurePage; i++ {
		page = append(page, azRow("Microsoft.Compute/virtualMachines", fmt.Sprintf("vm-%d", i), nil))
	}
	b, _ := json.Marshal(map[string]any{"data": page})
	m.srv.run = func(ctx context.Context, name string, args ...string) ([]byte, string, error) {
		if name == "az" && len(args) > 1 && args[0] == "graph" {
			calls++
			return b, "", nil
		}
		return []byte(`{}`), "", nil
	}
	rows, err := m.srv.queryAzure(context.Background(), nil, azureAssetMax*10)
	if err != nil || calls > azureAssetMax/azurePage+1 {
		t.Errorf("%d queries and err %v for an answer that never ends; want a bounded number", calls, err)
	}
	if len(rows) > azureAssetMax+azurePage {
		t.Errorf("%d rows read", len(rows))
	}
}

func TestAzureInventoryErrorsAreShownNotCached(t *testing.T) {
	a := &azStub{rows: azBasicRows()}
	m := azHarness(t, a)
	m.admin.expect(200, "PUT", "/v1/integrations/azure", map[string]any{"enabled": true})
	a.mu.Lock()
	a.stderr = "Please run 'az login' to set up an account."
	a.mu.Unlock()
	var inv AWSInventoryJSON
	json.Unmarshal(m.v.expect(200, "GET", "/v1/infra/azure/resources?refresh=1", nil), &inv)
	if !strings.Contains(inv.Error, "az login") || len(inv.Services) != 0 {
		t.Errorf("inventory = %+v", inv)
	}
	a.mu.Lock()
	a.stderr = ""
	a.mu.Unlock()
	inv = AWSInventoryJSON{}
	json.Unmarshal(m.v.expect(200, "GET", "/v1/infra/azure/resources", nil), &inv)
	if inv.Error != "" || len(inv.Services) == 0 {
		t.Errorf("after the login came back: %+v", inv)
	}
}

func TestAzureHubStatusAndTest(t *testing.T) {
	a := &azStub{rows: azBasicRows()}
	m := azHarness(t, a)
	if it := hub(t, m.dev)["azure"]; it.Status != intOff {
		t.Errorf("before connecting = %+v", it)
	}
	var res IntegrationTestJSON
	json.Unmarshal(m.admin.expect(200, "POST", "/v1/integrations/azure/checks", nil), &res)
	if res.Failed != 1 || res.Checks[0].Fix == "" {
		t.Errorf("not connected = %+v", res)
	}
	m.admin.expect(200, "PUT", "/v1/integrations/azure", map[string]any{"enabled": true})
	if it := hub(t, m.dev)["azure"]; it.Status != intConnected || !strings.Contains(it.Summary, "every subscription") {
		t.Errorf("connected = %+v", it)
	}
	res = IntegrationTestJSON{}
	json.Unmarshal(m.admin.expect(200, "POST", "/v1/integrations/azure/checks", nil), &res)
	if res.Failed != 0 || res.Passed != 4 || res.Changed || !strings.Contains(res.Checks[0].Detail, "ada@example.com") {
		t.Errorf("test = %+v", res)
	}
	a.noLogin = true
	res = IntegrationTestJSON{}
	json.Unmarshal(m.admin.expect(200, "POST", "/v1/integrations/azure/checks", nil), &res)
	if res.Failed != 1 || !strings.Contains(res.Checks[0].Fix, "az login") {
		t.Errorf("no login = %+v", res)
	}
	a.noLogin = false
	a.stderr = "(AuthorizationFailed) The client does not have authorization"
	res = IntegrationTestJSON{}
	json.Unmarshal(m.admin.expect(200, "POST", "/v1/integrations/azure/checks", nil), &res)
	last := res.Checks[len(res.Checks)-1]
	if res.Failed != 1 || !strings.Contains(last.Detail, "Reader role") || !strings.Contains(last.Fix, "Resource Graph") {
		t.Errorf("no access = %+v", res)
	}
	a.stderr, a.missing = "", true
	res = IntegrationTestJSON{}
	json.Unmarshal(m.admin.expect(200, "POST", "/v1/integrations/azure/checks", nil), &res)
	if res.Failed != 1 || !strings.Contains(res.Checks[0].Detail, "not installed") {
		t.Errorf("no az = %+v", res)
	}
}

// The same cap notice exists for Google Cloud: reaching the read limit is never silent.
func TestGCPNoticeWhenTheReadLimitIsReached(t *testing.T) {
	var sb strings.Builder
	sb.WriteString("[")
	for i := 0; i < gcpAssetMax; i++ {
		if i > 0 {
			sb.WriteString(",")
		}
		fmt.Fprintf(&sb, `{"name":"//compute.googleapis.com/projects/p/zones/z/instances/vm-%04d","assetType":"compute.googleapis.com/Instance"}`, i)
	}
	sb.WriteString("]")
	g := &gcloudStub{assets: sb.String()}
	m := gcpHarness(t, g)
	m.admin.expect(200, "PUT", "/v1/integrations/gcp", map[string]any{"enabled": true, "scope": "projects/p"})
	var inv AWSInventoryJSON
	json.Unmarshal(m.v.expect(200, "GET", "/v1/infra/gcp/resources", nil), &inv)
	if !strings.Contains(inv.Notice, "first 3000") {
		t.Errorf("notice = %q", inv.Notice)
	}
}
