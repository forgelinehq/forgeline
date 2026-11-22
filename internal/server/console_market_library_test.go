package server

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/forgelinehq/forgeline/internal/guardrail"
)

func libraryPkg(version int, url, rule string) MarketPackage {
	return MarketPackage{
		ID: "lib", Version: version, Title: "Library", Description: "d", Publisher: "Forgeline", ReviewedBy: "the maintainer",
		PolicyPacks: []guardrail.Pack{
			{ID: "lib-rules", Name: "Library rules", Tool: guardrail.ToolConftest, Rules: map[string]string{"r.rego": rule}},
			{ID: "lib-trivy", Name: "Library Trivy", Tool: guardrail.ToolTrivy, DenyAt: "critical"},
		},
		MCPServers: []MarketMCPServer{{Key: "docs", Name: "Docs", Description: "d", Kind: mcpRemote, URL: url,
			AuthHeader: "Authorization", AuthScheme: "Bearer", Token: "a token that can read", Docs: "https://example.com/docs"}},
	}
}

func packsNow(t *testing.T, m *mcpHarness) []guardrail.Pack {
	t.Helper()
	var got struct{ Packs []guardrail.Pack }
	json.Unmarshal(m.dev.expect(200, "GET", "/v1/settings/policy-packs", nil), &got)
	return got.Packs
}

func TestLibraryPackageChecks(t *testing.T) {
	for want, edit := range map[string]func(*MarketPackage){
		"reviewed_by: at most 80":             func(p *MarketPackage) { p.ReviewedBy = strings.Repeat("x", 81) },
		"installed turned off":                func(p *MarketPackage) { p.PolicyPacks[0].Enabled = true },
		`policy pack "lib-trivy": deny_at`:    func(p *MarketPackage) { p.PolicyPacks[1].DenyAt = "huge" },
		`policy pack "lib-rules" is repeated`: func(p *MarketPackage) { p.PolicyPacks[1] = p.PolicyPacks[0] },
		"a full https address":                func(p *MarketPackage) { p.MCPServers[0].URL = "http://mcp.example.com/mcp" },
		"cannot point at this machine":        func(p *MarketPackage) { p.MCPServers[0].URL = "https://127.0.0.1/mcp" },
		"name the header":                     func(p *MarketPackage) { p.MCPServers[0].AuthHeader = "" },
		"docs is an https":                    func(p *MarketPackage) { p.MCPServers[0].Docs = "javascript:alert(1)" },
		"docs is an https address":            func(p *MarketPackage) { p.MCPServers[0].Docs = "http://example.com/docs" },
		"kind is":                             func(p *MarketPackage) { p.MCPServers[0].Kind = "other" },
		"no image or network":                 func(p *MarketPackage) { p.MCPServers[0].Network = true },
		`mcp server "docs" is repeated`:       func(p *MarketPackage) { p.MCPServers = append(p.MCPServers, p.MCPServers[0]) },
		"its key is lower-case":               func(p *MarketPackage) { p.MCPServers[0].Key = "Docs" },
	} {
		p := libraryPkg(1, "https://mcp.example.com/mcp", "package main\n")
		edit(&p)
		p.SHA256 = p.contentHash()
		if err := p.check(); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("want %q, got %v", want, err)
		}
	}
	p := libraryPkg(1, "https://mcp.example.com/mcp", "package main\n")
	p.SHA256 = p.contentHash()
	if err := p.check(); err != nil {
		t.Errorf("a good package: %v", err)
	}
}

// Installing puts the packs in place turned off and the MCP server in place with no token and no tools; an update
// keeps what the administrator turned on, forgets a moved server's tools, and an uninstall takes both away.
func TestLibraryInstallsPacksAndToolsSwitchedOff(t *testing.T) {
	m := newMCPHarness(t)
	publish(t, libraryPkg(1, "https://mcp.example.com/mcp", "package main\n"))
	e := listMarket(t, m.admin)[0]
	if e.ReviewedBy != "the maintainer" {
		t.Errorf("listing: %+v", e)
	}
	var in MarketInstall
	json.Unmarshal(m.admin.expect(201, "POST", "/v1/market/install", map[string]any{"id": "lib", "hash": e.SHA256}), &in)
	packs := packsNow(t, m)
	if len(packs) != 2 || packs[0].Enabled || packs[1].Enabled || strings.Join(in.PolicyPacks, ",") != "lib-rules,lib-trivy" {
		t.Fatalf("packs = %+v, install %+v", packs, in)
	}
	var srv MCPServer
	m.srv.docs.Get(t.Context(), kindMCPServer, in.MCPServers["docs"], &srv)
	if srv.ID != "docs" || srv.AuthSecret != "" || len(srv.Tools) != 0 || srv.AllowPrivate || srv.URL != "https://mcp.example.com/mcp" {
		t.Fatalf("server = %+v", srv)
	}
	if a := string(m.admin.expect(200, "GET", "/v1/audit?action=market", nil)); !strings.Contains(a, "policy packs (off) lib-rules lib-trivy") || !strings.Contains(a, "MCP servers (no token, tools off) docs") {
		t.Errorf("audit: %s", a)
	}

	// The administrator turns one pack on and one tool on.
	packs[1].Enabled = true
	m.admin.expect(200, "PUT", "/v1/settings/policy-packs", map[string]any{"packs": packs})
	srv.Tools = []MCPServerTool{{Name: "read", Enabled: true, ReadOnly: true}}
	srv.AllowPrivate = true // set by hand since; an update of the package does not keep it
	m.srv.docs.Put(t.Context(), kindMCPServer, srv.ID, srv)

	// v2 changes a rule (kept on/off as set) and moves the server: its tools are forgotten.
	publish(t, libraryPkg(2, "https://mcp2.example.com/mcp", "package main\n# v2\n"))
	e = listMarket(t, m.admin)[0]
	m.admin.expect(201, "POST", "/v1/market/install", map[string]any{"id": "lib", "hash": e.SHA256})
	packs = packsNow(t, m)
	if packs[0].Enabled || !packs[1].Enabled || !strings.Contains(packs[0].Rules["r.rego"], "v2") {
		t.Errorf("after the update: %+v", packs)
	}
	srv = MCPServer{} // a fresh value: fields left out of the stored JSON must not keep their old values
	m.srv.docs.Get(t.Context(), kindMCPServer, "docs", &srv)
	if srv.URL != "https://mcp2.example.com/mcp" || len(srv.Tools) != 0 || srv.AllowPrivate {
		t.Errorf("a moved server kept its tools: %+v", srv)
	}

	m.admin.expect(200, "DELETE", "/v1/market/install/lib", nil)
	if packs = packsNow(t, m); len(packs) != 0 {
		t.Errorf("packs left after uninstall: %+v", packs)
	}
	if err := m.srv.docs.Get(t.Context(), kindMCPServer, "docs", &srv); err == nil {
		t.Errorf("the MCP server is still there")
	}
}

// A package cannot take over a pack or a server someone made by hand.
func TestLibraryInstallRefusesNamesInUse(t *testing.T) {
	m := newMCPHarness(t)
	publish(t, libraryPkg(1, "https://mcp.example.com/mcp", "package main\n"))
	e := listMarket(t, m.admin)[0]
	m.admin.expect(200, "PUT", "/v1/settings/policy-packs", map[string]any{"packs": []map[string]any{{"id": "lib-trivy", "name": "Mine", "tool": "trivy", "enabled": true}}})
	if out := string(m.admin.expect(409, "POST", "/v1/market/install", map[string]any{"id": "lib", "hash": e.SHA256})); !strings.Contains(out, `a policy pack \"lib-trivy\" already exists`) {
		t.Errorf("took over a pack: %s", out)
	}
	if p := packsNow(t, m); len(p) != 1 || p[0].Name != "Mine" || !p[0].Enabled {
		t.Errorf("the pack was changed: %+v", p)
	}
	m.admin.expect(200, "PUT", "/v1/settings/policy-packs", map[string]any{"packs": []any{}})
	m.srv.docs.Put(t.Context(), kindMCPServer, "docs", MCPServer{ID: "docs", Name: "Mine", Kind: mcpRemote, URL: "https://mine.example.com/mcp", Tools: []MCPServerTool{}})
	if out := string(m.admin.expect(409, "POST", "/v1/market/install", map[string]any{"id": "lib", "hash": e.SHA256})); !strings.Contains(out, `an MCP server \"docs\" already exists`) {
		t.Errorf("took over a server: %s", out)
	}
	var srv MCPServer
	m.srv.docs.Get(t.Context(), kindMCPServer, "docs", &srv)
	if srv.URL != "https://mine.example.com/mcp" {
		t.Errorf("the server was changed: %+v", srv)
	}
	// A refused install leaves nothing behind: not even the packs it would have added.
	if p := packsNow(t, m); len(p) != 0 {
		t.Errorf("a refused install left packs: %+v", p)
	}
}

// blueprints_built tells an agent each resource's address and AWS identifier, and nothing else from the state.
func TestLookupBlueprintsBuilt(t *testing.T) {
	m := newMCPHarness(t)
	m.admin.expect(200, "PUT", "/v1/integrations/aws", map[string]any{"enabled": true, "profile": "dev", "region": "ap-south-1"})
	var made BlueprintJSON
	json.Unmarshal(m.dev.expect(201, "POST", "/v1/blueprints", bpBody("Shop")), &made)
	json.Unmarshal(m.dev.expect(201, "POST", "/v1/blueprints", bpBody("Unbuilt")), new(BlueprintJSON))
	state := `{"version":4,"resources":[
	 {"mode":"managed","type":"aws_dynamodb_table","name":"data","instances":[{"attributes":{"arn":"arn:aws:dynamodb:ap-south-1:111122223333:table/fl-data","name":"fl-data"}}]},
	 {"mode":"managed","type":"aws_db_instance","name":"db","instances":[{"attributes":{"id":"db-ABC","password":"S3CR3T-PASSWORD","arn":""}}]},
	 {"module":"module.fn","mode":"managed","type":"aws_sqs_queue","name":"q","instances":[{"index_key":0,"attributes":{"id":"https://sqs/q"}}]},
	 {"mode":"managed","type":"aws_iam_role_policy","name":"p","instances":[{"attributes":{"policy":"S3CR3T-POLICY"}}]},
	 {"mode":"data","type":"aws_vpc","name":"default","instances":[{"attributes":{"id":"vpc-1"}}]}]}`
	sealed, err := m.srv.vault.Seal("blueprint-state/"+made.ID, state)
	if err != nil {
		t.Fatal(err)
	}
	m.srv.docs.Put(t.Context(), kindBPState, made.ID, blueprintState{ID: made.ID, AWSAccount: "111122223333", Region: "ap-south-1", Serial: 2, Sealed: sealed})
	out, err := m.srv.lookupBlueprintsBuilt(t.Context(), json.RawMessage(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"aws_dynamodb_table.data: arn:aws:dynamodb:ap-south-1:111122223333:table/fl-data", "aws_db_instance.db: db-ABC", "module.fn.aws_sqs_queue.q[0]: https://sqs/q", "aws_iam_role_policy.p: \n", `"Unbuilt": nothing built`, "state serial 2"} {
		if !strings.Contains(out, want) {
			t.Errorf("lacks %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "S3CR3T") || strings.Contains(out, "vpc-1") {
		t.Errorf("more than identifiers:\n%s", out)
	}
	one, _ := m.srv.lookupBlueprintsBuilt(t.Context(), json.RawMessage(`{"blueprint":"`+made.ID+`"}`))
	if strings.Contains(one, "Unbuilt") || !strings.Contains(one, made.ID) {
		t.Errorf("one blueprint:\n%s", one)
	}
}

// TestShippedConftestPacks runs the library's Conftest packs in the real container on a plan that breaks every rule,
// and on a removal of the same, which must break none. Set FORGELINE_PACK_DOCKER=1 (needs Docker).
func TestShippedConftestPacks(t *testing.T) {
	if os.Getenv("FORGELINE_PACK_DOCKER") == "" {
		t.Skip("set FORGELINE_PACK_DOCKER=1 to run the shipped Conftest packs in their container")
	}
	m := newMCPHarness(t)
	rc := func(addr, typ, action string, after map[string]any) map[string]any {
		return map[string]any{"address": addr, "mode": "managed", "type": typ, "name": "x", "change": map[string]any{"actions": []string{action}, "after": after}}
	}
	plan := func(action string) []byte {
		b, _ := json.Marshal(map[string]any{"format_version": "1.2", "resource_changes": []any{
			rc("aws_dynamodb_table.t", "aws_dynamodb_table", action, map[string]any{"billing_mode": "PROVISIONED", "point_in_time_recovery": []any{map[string]any{"enabled": false}}}),
			rc("aws_dynamodb_table.ok", "aws_dynamodb_table", action, map[string]any{"billing_mode": "PAY_PER_REQUEST", "point_in_time_recovery": []any{map[string]any{"enabled": true}}}),
			rc("aws_db_instance.db", "aws_db_instance", action, map[string]any{"backup_retention_period": 1, "multi_az": true, "instance_class": "db.r6g.4xlarge"}),
			rc("aws_secretsmanager_secret.s", "aws_secretsmanager_secret", action, map[string]any{"recovery_window_in_days": 0}),
			rc("aws_cloudwatch_log_group.short", "aws_cloudwatch_log_group", action, map[string]any{"retention_in_days": 7}),
			rc("aws_cloudwatch_log_group.ever", "aws_cloudwatch_log_group", action, map[string]any{"retention_in_days": 0}),
			rc("aws_cloudwatch_log_group.ok", "aws_cloudwatch_log_group", action, map[string]any{"retention_in_days": 30}),
			rc("aws_nat_gateway.n", "aws_nat_gateway", action, map[string]any{}),
			rc("aws_eks_cluster.k", "aws_eks_cluster", action, map[string]any{"name": "k"}),
			rc("aws_kms_key.key", "aws_kms_key", action, map[string]any{"deletion_window_in_days": 7}),
		}})
		return b
	}
	var packs []guardrail.Pack
	for _, e := range m.srv.marketCatalog(t.Context()) {
		for _, pk := range e.PolicyPacks {
			if pk.Tool == guardrail.ToolConftest {
				pk.Enabled = true
				packs = append(packs, pk)
			}
		}
	}
	if len(packs) != 2 {
		t.Fatalf("shipped Conftest packs: %d", len(packs))
	}
	m.admin.expect(200, "PUT", "/v1/settings/policy-packs", map[string]any{"packs": packs})
	got := func(show []byte) map[string]string {
		fs, runs, _, err := m.srv.runPacks(t.Context(), show)
		if err != nil {
			t.Fatal(err)
		}
		for _, r := range runs {
			if r.Error != "" {
				t.Fatalf("%s did not run: %s", r.Name, r.Error)
			}
		}
		out := map[string]string{}
		for _, f := range fs {
			out[f.Rule+" "+f.Address] = f.Level
		}
		return out
	}
	create := got(plan("create"))
	want := map[string]string{
		"conftest:data-protection aws_dynamodb_table.t":           guardrail.Deny,
		"conftest:data-protection aws_db_instance.db":             guardrail.Deny,
		"conftest:data-protection aws_secretsmanager_secret.s":    guardrail.Deny,
		"conftest:data-protection aws_cloudwatch_log_group.short": guardrail.Deny,
		"conftest:data-protection aws_kms_key.key":                guardrail.Warn,
		"conftest:cost-guard aws_eks_cluster.k":                   guardrail.Warn,
		"conftest:cost-guard aws_nat_gateway.n":                   guardrail.Warn,
		"conftest:cost-guard aws_db_instance.db":                  guardrail.Warn,
		"conftest:cost-guard aws_dynamodb_table.t":                guardrail.Warn,
		"conftest:cost-guard aws_cloudwatch_log_group.ever":       guardrail.Warn,
	}
	for k, level := range want {
		if create[k] != level {
			t.Errorf("%s: %q, want %q", k, create[k], level)
		}
	}
	for k := range create {
		if strings.Contains(k, ".ok") {
			t.Errorf("a good resource was flagged: %s", k)
		}
	}
	if removal := got(plan("delete")); len(removal) != 0 {
		t.Errorf("a removal was flagged: %v", removal)
	}
	if eks, _ := os.ReadFile("../guardrail/testdata/eks-plan.json"); len(eks) > 0 {
		t.Logf("the EKS template's plan: %v", got(eks))
	}
}

// A packaged pack is installed turned off even if a manifest got past the check that says so.
func TestPackagedPackIsInstalledOff(t *testing.T) {
	m := newMCPHarness(t)
	pkg := libraryPkg(1, "https://mcp.example.com/mcp", "package main\n")
	pkg.PolicyPacks[0].Enabled = true
	in := MarketInstall{}
	if err := m.srv.installPolicyPacks(t.Context(), &in, pkg, "ada", time.Now()); err != nil {
		t.Fatal(err)
	}
	for _, p := range packsNow(t, m) {
		if p.Enabled {
			t.Errorf("%s was installed turned on", p.ID)
		}
	}
}

// The ready-made agents the product ships are the first thing a person tries. They are written out in full: a role, how to
// work, what never to do and what the answer looks like, with a skill that is a real procedure. A one-line agent is a bug.
func TestTheDeepAgentsAreNotOneLiners(t *testing.T) {
	for id, min := range map[string][2]int{"security-review": {1200, 2500}, "cost-watch": {900, 2000}, "cluster-health": {600, 1500}, "failed-run-triage": {600, 1500}} {
		b, err := marketFS.ReadFile("market/" + id + ".json")
		if err != nil {
			t.Fatal(err)
		}
		var p MarketPackage
		if err := json.Unmarshal(b, &p); err != nil {
			t.Fatal(err)
		}
		if len(p.Agents) == 0 || len(p.Skills) == 0 {
			t.Fatalf("%s has no agent or no skill", id)
		}
		if n := len(p.Agents[0].Instructions); n < min[0] {
			t.Errorf("%s: the agent's instructions are %d characters, want at least %d", id, n, min[0])
		}
		if n := len(p.Skills[0].Body); n < min[1] {
			t.Errorf("%s: the skill is %d characters, want at least %d", id, n, min[1])
		}
		for _, l := range strings.Split(p.Skills[0].Body, "\n") {
			if strings.HasPrefix(l, "   - ") {
				t.Errorf("%s: a nested list item renders as numbered steps in the console: %q", id, truncate(l, 50))
			}
		}
	}
}
