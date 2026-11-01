package server

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/forgelinehq/forgeline/internal/mcp"
	"github.com/forgelinehq/forgeline/internal/plugin"
)

const pluginImage = "example/weather@sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

type fakePlugins struct {
	mu       sync.Mutex
	pingErr  error
	offered  []string // tool names the image really has
	calls    []plugin.Spec
	callArgs []map[string]any
	reply    string
}

func (f *fakePlugins) Ping(context.Context) error           { return f.pingErr }
func (f *fakePlugins) Ensure(context.Context, string) error { return nil }
func (f *fakePlugins) Tools(_ context.Context, _ plugin.Spec) ([]mcp.Tool, error) {
	var out []mcp.Tool
	for _, n := range f.offered {
		out = append(out, mcp.Tool{Name: n})
	}
	return out, nil
}
func (f *fakePlugins) Call(_ context.Context, s plugin.Spec, tool string, args map[string]any) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, s)
	f.callArgs = append(f.callArgs, args)
	return f.reply, nil
}

func pluginPkg(version int) MarketPackage {
	p := samplePkg(version, "Steps.")
	p.ID = "weather-pack"
	p.Agents[0].Tools = []string{"plugin_weather_pack_wx_forecast"}
	p.Agents[0].Tier = "T0"
	p.Plugins = []MarketPlugin{{Key: "wx", Name: "Weather", Description: "d", Image: pluginImage, Network: true, Secrets: []string{"WEATHER_KEY"},
		Tools: []MarketPluginTool{{Name: "forecast", Description: "forecast for a city",
			Schema: json.RawMessage(`{"type":"object","properties":{"city":{"type":"string","maxLength":40},"days":{"type":"integer"}},"required":["city"]}`)}}}}
	return p
}

func pluginHarness(t *testing.T) (*mcpHarness, *fakePlugins) {
	t.Helper()
	m := newMCPHarness(t)
	f := &fakePlugins{offered: []string{"forecast"}, reply: "sunny"}
	m.srv.plugins = f
	return m, f
}

func installPlugin(m *mcpHarness, hash string, status int, grants map[string]any) []byte {
	return m.admin.expect(status, "POST", "/v1/market/install", map[string]any{"id": "weather-pack", "hash": hash, "grants": grants})
}

func TestPluginPackageChecks(t *testing.T) {
	bad := map[string]func(*MarketPackage){
		"tag image":     func(p *MarketPackage) { p.Plugins[0].Image = "example/weather:latest" },
		"bad tool name": func(p *MarketPackage) { p.Plugins[0].Tools[0].Name = "Forecast!" },
		"object arg": func(p *MarketPackage) {
			p.Plugins[0].Tools[0].Schema = json.RawMessage(`{"type":"object","properties":{"x":{"type":"object"}}}`)
		},
		"no schema":           func(p *MarketPackage) { p.Plugins[0].Tools[0].Schema = nil },
		"bad env":             func(p *MarketPackage) { p.Plugins[0].Secrets = []string{"lower"} },
		"unknown plugin tool": func(p *MarketPackage) { p.Agents[0].Tools = []string{"plugin_weather_pack_wx_nope"} },
		"no tools":            func(p *MarketPackage) { p.Plugins[0].Tools = nil },
	}
	for name, mut := range bad {
		p := pluginPkg(1)
		mut(&p)
		p.SHA256 = p.contentHash()
		if p.check() == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	p := pluginPkg(1)
	p.SHA256 = p.contentHash()
	if err := p.check(); err != nil {
		t.Fatalf("good plugin package refused: %v", err)
	}
}

func TestPluginsMustBeTurnedOnAndDockerMustAnswer(t *testing.T) {
	m, f := pluginHarness(t)
	publish(t, pluginPkg(1))
	e := listMarket(t, m.admin)[0]
	installPlugin(m, e.SHA256, 409, nil) // off
	m.dev.expect(403, "PUT", "/v1/plugins/settings", map[string]any{"enabled": true})
	f.pingErr = errors.New("docker did not answer")
	m.admin.expect(409, "PUT", "/v1/plugins/settings", map[string]any{"enabled": true})
	f.pingErr = nil
	m.admin.expect(200, "PUT", "/v1/plugins/settings", map[string]any{"enabled": true})
	installPlugin(m, e.SHA256, 201, nil)
}

func TestInstallGrantsAreExplicitAndChecked(t *testing.T) {
	m, f := pluginHarness(t)
	m.admin.expect(200, "PUT", "/v1/plugins/settings", map[string]any{"enabled": true})
	m.srv.putSecret(context.Background(), "test", "weather-key", "k-123", "")
	publish(t, pluginPkg(1))
	e := listMarket(t, m.admin)[0]

	installPlugin(m, e.SHA256, 400, map[string]any{"wx": map[string]any{"secrets": map[string]string{"OTHER": "weather-key"}}}) // not asked for
	installPlugin(m, e.SHA256, 400, map[string]any{"wx": map[string]any{"secrets": map[string]string{"WEATHER_KEY": "missing"}}})
	f.offered = []string{"other"}
	installPlugin(m, e.SHA256, 409, nil) // the image does not offer the listed tool
	f.offered = []string{"forecast"}

	var in MarketInstall
	json.Unmarshal(installPlugin(m, e.SHA256, 201, map[string]any{"wx": map[string]any{"network": true, "secrets": map[string]string{"WEATHER_KEY": "weather-key"}}}), &in)
	g := in.Plugins["wx"]
	if !g.Network || g.Secrets["WEATHER_KEY"] != "weather-key" || len(g.Tools) != 1 {
		t.Fatalf("grant = %+v", g)
	}
	var list struct {
		Enabled bool
		Plugins []struct {
			Tools   []string
			Secrets []string
		}
	}
	json.Unmarshal(m.dev.expect(200, "GET", "/v1/plugins", nil), &list)
	if !list.Enabled || len(list.Plugins) != 1 || list.Plugins[0].Tools[0] != "plugin_weather_pack_wx_forecast" || strings.Contains(string(m.dev.expect(200, "GET", "/v1/plugins", nil)), "k-123") {
		t.Errorf("listing = %+v", list)
	}
}

func TestNetworkIsRefusedIfThePluginDidNotAskForIt(t *testing.T) {
	m, _ := pluginHarness(t)
	m.admin.expect(200, "PUT", "/v1/plugins/settings", map[string]any{"enabled": true})
	p := pluginPkg(1)
	p.Plugins[0].Network = false
	publish(t, p)
	e := listMarket(t, m.admin)[0]
	installPlugin(m, e.SHA256, 400, map[string]any{"wx": map[string]any{"network": true}})
}

func TestPluginLookupChecksArgsRoleScopeAndPassesOnlyGrants(t *testing.T) {
	m, f := pluginHarness(t)
	m.admin.expect(200, "PUT", "/v1/plugins/settings", map[string]any{"enabled": true})
	m.srv.putSecret(context.Background(), "test", "weather-key", "k-123", "")
	m.srv.putSecret(context.Background(), "test", "other-secret", "nope", "")
	publish(t, pluginPkg(1))
	e := listMarket(t, m.admin)[0]
	installPlugin(m, e.SHA256, 201, map[string]any{"wx": map[string]any{"network": false, "secrets": map[string]string{"WEATHER_KEY": "weather-key"}}})

	tool := "plugin_weather_pack_wx_forecast"
	ctx := context.Background()
	dev := Principal{Name: "dev", Role: "developer"}
	out, err := m.srv.runLookup(ctx, dev, lookupCall{Tool: tool, Args: json.RawMessage(`{"city":"Pune","days":3}`)})
	if err != nil || !strings.Contains(out, "sunny") || !strings.Contains(out, "untrusted") {
		t.Fatalf("out = %q, %v", out, err)
	}
	spec := f.calls[0]
	if spec.Network || spec.Env["WEATHER_KEY"] != "k-123" || len(spec.Env) != 1 || spec.Image != pluginImage {
		t.Errorf("spec = %+v", spec)
	}
	for name, args := range map[string]string{"unknown arg": `{"city":"x","evil":1}`, "missing": `{}`, "wrong type": `{"city":5}`, "float int": `{"city":"x","days":1.5}`, "too long": `{"city":"` + strings.Repeat("a", 41) + `"}`, "not object": `[1]`} {
		if _, err := m.srv.runLookup(ctx, dev, lookupCall{Tool: tool, Args: json.RawMessage(args)}); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	if len(f.calls) != 1 {
		t.Errorf("a refused call started a container (%d calls)", len(f.calls))
	}
	if _, err := m.srv.runLookup(ctx, Principal{Name: "v", Role: "viewer"}, lookupCall{Tool: tool, Args: json.RawMessage(`{"city":"x"}`)}); err == nil {
		t.Error("a viewer ran a plugin")
	}
	// An agent that does not list the tool cannot use it.
	scope := &agentScope{tools: map[string]bool{"get_run": true}, label: "x@1"}
	actx := context.WithValue(ctx, agentScopeKey{}, scope)
	if _, err := m.srv.runLookup(actx, dev, lookupCall{Tool: tool, Args: json.RawMessage(`{"city":"x"}`)}); err == nil {
		t.Error("an agent used a plugin tool it was not given")
	}
	if strings.Contains(m.srv.lookupHelp(actx, "developer"), tool) || !strings.Contains(m.srv.lookupHelp(ctx, "developer"), tool) || strings.Contains(m.srv.lookupHelp(ctx, "viewer"), tool) {
		t.Error("help does not follow scope and role")
	}
	// Turning plugins off removes the tools at once.
	m.admin.expect(200, "PUT", "/v1/plugins/settings", map[string]any{"enabled": false})
	if _, err := m.srv.runLookup(ctx, dev, lookupCall{Tool: tool, Args: json.RawMessage(`{"city":"x"}`)}); err == nil {
		t.Error("a plugin ran while plugins are off")
	}
}

func TestAnAgentCanBeGivenAPluginToolOnlyWhenItExists(t *testing.T) {
	m, _ := pluginHarness(t)
	m.admin.expect(400, "POST", "/v1/ai/agents", agentBody("Wx", nil, []string{"plugin_weather_pack_wx_forecast"}, "T0"))
	m.admin.expect(200, "PUT", "/v1/plugins/settings", map[string]any{"enabled": true})
	publish(t, pluginPkg(1))
	e := listMarket(t, m.admin)[0]
	installPlugin(m, e.SHA256, 201, nil)
	m.admin.expect(201, "POST", "/v1/ai/agents", agentBody("Wx", nil, []string{"plugin_weather_pack_wx_forecast"}, "T0"))
	var tools struct {
		Tools []struct{ Name, Needs string }
	}
	json.Unmarshal(m.dev.expect(200, "GET", "/v1/ai/tools", nil), &tools)
	found := false
	for _, x := range tools.Tools {
		found = found || x.Name == "plugin_weather_pack_wx_forecast" && x.Needs == "plugins"
	}
	if !found {
		t.Error("the tools list does not show the plugin tool")
	}
}

func TestUpgradeNeedsAFreshGrantWhenThePluginAsksForMore(t *testing.T) {
	m, _ := pluginHarness(t)
	m.admin.expect(200, "PUT", "/v1/plugins/settings", map[string]any{"enabled": true})
	m.srv.putSecret(context.Background(), "test", "weather-key", "k-123", "")
	p1 := pluginPkg(1)
	p1.Plugins[0].Network = false
	publish(t, p1)
	e1 := listMarket(t, m.admin)[0]
	installPlugin(m, e1.SHA256, 201, map[string]any{"wx": map[string]any{"secrets": map[string]string{"WEATHER_KEY": "weather-key"}}})

	// Same ask, new version: the old grant stays without being repeated.
	p2 := pluginPkg(2)
	p2.Plugins[0].Network = false
	p2.Skills[0].Body = "Steps v2."
	publish(t, p2)
	e2 := listMarket(t, m.admin)[0]
	var in MarketInstall
	json.Unmarshal(installPlugin(m, e2.SHA256, 201, nil), &in)
	if in.Plugins["wx"].Secrets["WEATHER_KEY"] != "weather-key" || in.Plugins["wx"].Network {
		t.Fatalf("grant lost: %+v", in.Plugins["wx"])
	}

	// Now it asks for network (and a new image): that needs a new approval.
	p3 := pluginPkg(3)
	p3.Plugins[0].Network = true
	p3.Plugins[0].Image = "example/weather@sha256:" + strings.Repeat("f", 64)
	publish(t, p3)
	e3 := listMarket(t, m.admin)[0]
	installPlugin(m, e3.SHA256, 409, nil)
	json.Unmarshal(installPlugin(m, e3.SHA256, 201, map[string]any{"wx": map[string]any{"network": false}}), &in)
	if in.Plugins["wx"].Image != p3.Plugins[0].Image || in.Plugins["wx"].Network || len(in.Plugins["wx"].Secrets) != 0 {
		t.Errorf("new grant = %+v", in.Plugins["wx"])
	}
}

func hostsPkg(version int, hosts ...string) MarketPackage {
	p := pluginPkg(version)
	p.Plugins[0].Network = false
	p.Plugins[0].Hosts = hosts
	return p
}

func TestAPluginAsksForHostsAndTheAdministratorGrantsSome(t *testing.T) {
	m, f := pluginHarness(t)
	m.admin.expect(200, "PUT", "/v1/plugins/settings", map[string]any{"enabled": true})
	publish(t, hostsPkg(1, "api.weather.example", "*.cdn.weather.example", "files.weather.example:8443"))
	e := listMarket(t, m.admin)[0]

	// A host that was not asked for, and the open network when only hosts were asked for, are refused.
	installPlugin(m, e.SHA256, 400, map[string]any{"wx": map[string]any{"hosts": []string{"evil.example.com"}}})
	installPlugin(m, e.SHA256, 400, map[string]any{"wx": map[string]any{"hosts": []string{"api.weather.example:8443"}}}) // another port than asked
	installPlugin(m, e.SHA256, 400, map[string]any{"wx": map[string]any{"network": true}})
	var in MarketInstall
	json.Unmarshal(installPlugin(m, e.SHA256, 201, map[string]any{"wx": map[string]any{"hosts": []string{"files.weather.example:8443", "api.weather.example"}}}), &in)
	g := in.Plugins["wx"]
	if g.Network || strings.Join(g.Hosts, ",") != "api.weather.example,files.weather.example:8443" || len(g.WantsHosts) != 3 {
		t.Fatalf("grant = %+v", g)
	}
	// The plugin runs with exactly the granted hosts and never the open network.
	dev := Principal{Name: "dev", Role: "developer"}
	if _, err := m.srv.runLookup(context.Background(), dev, lookupCall{Tool: "plugin_weather_pack_wx_forecast", Args: json.RawMessage(`{"city":"Pune"}`)}); err != nil {
		t.Fatal(err)
	}
	if spec := f.calls[0]; spec.Network || strings.Join(spec.Hosts, ",") != "api.weather.example,files.weather.example:8443" {
		t.Errorf("spec = %+v", spec)
	}
	// The listing shows what is allowed.
	var list struct{ Plugins []struct{ Hosts []string } }
	json.Unmarshal(m.dev.expect(200, "GET", "/v1/plugins", nil), &list)
	if len(list.Plugins) != 1 || len(list.Plugins[0].Hosts) != 2 {
		t.Errorf("listing = %+v", list)
	}
	// Granting none is allowed, and leaves the plugin with no network at all.
	m.admin.expect(200, "DELETE", "/v1/market/install/weather-pack", nil)
	json.Unmarshal(installPlugin(m, e.SHA256, 201, nil), &in)
	if g := in.Plugins["wx"]; g.Network || len(g.Hosts) != 0 {
		t.Errorf("a grant of nothing = %+v", g)
	}
	if _, err := m.srv.runLookup(context.Background(), dev, lookupCall{Tool: "plugin_weather_pack_wx_forecast", Args: json.RawMessage(`{"city":"Pune"}`)}); err != nil {
		t.Fatal(err)
	}
	if spec := f.calls[len(f.calls)-1]; spec.Network || len(spec.Hosts) != 0 {
		t.Errorf("spec = %+v", spec)
	}
}

func TestAPackageThatMixesNetworkAndHostsOrListsBadHostsIsDamaged(t *testing.T) {
	for name, mut := range map[string]func(*MarketPlugin){
		"both":         func(p *MarketPlugin) { p.Network = true; p.Hosts = []string{"a.example.com"} },
		"an ip":        func(p *MarketPlugin) { p.Hosts = []string{"10.0.0.1"} },
		"a star":       func(p *MarketPlugin) { p.Hosts = []string{"*"} },
		"a url":        func(p *MarketPlugin) { p.Hosts = []string{"https://a.example.com"} },
		"a bare label": func(p *MarketPlugin) { p.Hosts = []string{"localhost"} },
		"a repeat":     func(p *MarketPlugin) { p.Hosts = []string{"a.example.com", "a.example.com"} },
		"too many":     func(p *MarketPlugin) { p.Hosts = manyHosts(21) },
	} {
		p := hostsPkg(1)
		mut(&p.Plugins[0])
		p.SHA256 = p.contentHash()
		if p.check() == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	p := hostsPkg(1, "a.example.com", "*.b.example.com:8443")
	p.SHA256 = p.contentHash()
	if err := p.check(); err != nil {
		t.Errorf("good hosts refused: %v", err)
	}
}

func manyHosts(n int) []string {
	var out []string
	for i := 0; i < n; i++ {
		out = append(out, "h"+strings.Repeat("x", i%5)+string(rune('a'+i%26))+".example.com")
	}
	return out
}

func TestAnUpgradeThatAsksForDifferentHostsNeedsAFreshGrant(t *testing.T) {
	m, _ := pluginHarness(t)
	m.admin.expect(200, "PUT", "/v1/plugins/settings", map[string]any{"enabled": true})
	publish(t, hostsPkg(1, "api.weather.example"))
	e1 := listMarket(t, m.admin)[0]
	installPlugin(m, e1.SHA256, 201, map[string]any{"wx": map[string]any{"hosts": []string{"api.weather.example"}}})

	// The same ask in a new version keeps the grant.
	p2 := hostsPkg(2, "api.weather.example")
	p2.Skills[0].Body = "Steps v2."
	publish(t, p2)
	e2 := listMarket(t, m.admin)[0]
	var in MarketInstall
	json.Unmarshal(installPlugin(m, e2.SHA256, 201, nil), &in)
	if strings.Join(in.Plugins["wx"].Hosts, ",") != "api.weather.example" {
		t.Fatalf("grant lost: %+v", in.Plugins["wx"])
	}
	// One more host is a different ask: refused until the administrator grants again.
	p3 := hostsPkg(3, "api.weather.example", "tracker.example.net")
	publish(t, p3)
	e3 := listMarket(t, m.admin)[0]
	installPlugin(m, e3.SHA256, 409, nil)
	json.Unmarshal(installPlugin(m, e3.SHA256, 201, map[string]any{"wx": map[string]any{"hosts": []string{"api.weather.example"}}}), &in)
	if strings.Join(in.Plugins["wx"].Hosts, ",") != "api.weather.example" || len(in.Plugins["wx"].WantsHosts) != 2 {
		t.Errorf("new grant = %+v", in.Plugins["wx"])
	}
	// A plugin that moves from hosts to the open network is also a different ask.
	p4 := pluginPkg(4)
	p4.Plugins[0].Network = true
	publish(t, p4)
	e4 := listMarket(t, m.admin)[0]
	installPlugin(m, e4.SHA256, 409, nil)
}

func TestWhatAPluginsProxyDidIsAudited(t *testing.T) {
	m, _ := pluginHarness(t)
	m.srv.reportEgress(plugin.Spec{ID: "weather-pack-wx"}, plugin.EgressReport{Allowed: map[string]int{"api.weather.example": 2}, Denied: map[string]int{"evil.example.com": 1}, Bytes: 900})
	log := string(m.admin.expect(200, "GET", "/v1/audit?limit=20", nil))
	if !strings.Contains(log, "plugin.egress") || !strings.Contains(log, "evil.example.com×1") || !strings.Contains(log, "plugin:weather-pack-wx") {
		t.Errorf("audit: %s", log)
	}
}

func TestOnlyPolicyRefusalsMakeTheEgressAuditEntryNotOK(t *testing.T) {
	m, _ := pluginHarness(t)
	allowedOf := func(id string) bool {
		var evs struct{ Events []AuditEvent }
		json.Unmarshal(m.admin.expect(200, "GET", "/v1/audit?limit=50", nil), &evs)
		for _, e := range evs.Events {
			if e.Action == "plugin.egress" && e.Target == "plugin/"+id {
				return e.Allowed
			}
		}
		t.Fatalf("no audit entry for %s", id)
		return false
	}
	m.srv.reportEgress(plugin.Spec{ID: "failing"}, plugin.EgressReport{Allowed: map[string]int{}, Denied: map[string]int{}, Failed: map[string]int{"api.weather.example": 3}})
	m.srv.reportEgress(plugin.Spec{ID: "denied"}, plugin.EgressReport{Denied: map[string]int{"evil.example.com": 1}})
	m.srv.reportEgress(plugin.Spec{ID: "foreign"}, plugin.EgressReport{Unauthorized: 2})
	if !allowedOf("failing") {
		t.Error("an upstream failure for an allowed host marked the audit entry as a refusal")
	}
	if allowedOf("denied") || allowedOf("foreign") {
		t.Error("a refusal left the audit entry ok")
	}
}

func TestGrantedHostsAreNormalisedAndDeduplicatedSoACallNeverFailsTheCheck(t *testing.T) {
	m, f := pluginHarness(t)
	m.admin.expect(200, "PUT", "/v1/plugins/settings", map[string]any{"enabled": true})
	publish(t, hostsPkg(1, "api.weather.example", "files.weather.example:8443"))
	e := listMarket(t, m.admin)[0]

	// Not valid hosts, whatever the plugin asked for.
	for _, bad := range []string{"API.weather.example", "api.weather.example:0", "10.0.0.1", "*", "https://api.weather.example", ""} {
		installPlugin(m, e.SHA256, 400, map[string]any{"wx": map[string]any{"hosts": []string{bad}}})
	}
	// The same host written three ways is one grant, in normal form.
	var in MarketInstall
	json.Unmarshal(installPlugin(m, e.SHA256, 201, map[string]any{"wx": map[string]any{"hosts": []string{
		"api.weather.example", "api.weather.example:443", "api.weather.example", "files.weather.example:8443"}}}), &in)
	g := in.Plugins["wx"]
	if strings.Join(g.Hosts, ",") != "api.weather.example,files.weather.example:8443" {
		t.Fatalf("hosts = %v", g.Hosts)
	}
	// Only the explicit-port spelling of a default-port host is stored in normal form too.
	m.admin.expect(200, "DELETE", "/v1/market/install/weather-pack", nil)
	json.Unmarshal(installPlugin(m, e.SHA256, 201, map[string]any{"wx": map[string]any{"hosts": []string{"api.weather.example:443"}}}), &in)
	if h := in.Plugins["wx"].Hosts; len(h) != 1 || h[0] != "api.weather.example" {
		t.Fatalf("hosts = %v", h)
	}
	m.admin.expect(200, "DELETE", "/v1/market/install/weather-pack", nil)
	json.Unmarshal(installPlugin(m, e.SHA256, 201, map[string]any{"wx": map[string]any{"hosts": []string{
		"api.weather.example", "api.weather.example:443", "api.weather.example", "files.weather.example:8443"}}}), &in)
	g = in.Plugins["wx"]
	spec := plugin.Spec{ID: "weather-pack-wx", Image: g.Image, Hosts: g.Hosts}
	if err := spec.Check(); err != nil {
		t.Errorf("a stored grant fails the check on every call: %v", err)
	}
	dev := Principal{Name: "dev", Role: "developer"}
	if _, err := m.srv.runLookup(context.Background(), dev, lookupCall{Tool: "plugin_weather_pack_wx_forecast", Args: json.RawMessage(`{"city":"Pune"}`)}); err != nil {
		t.Fatal(err)
	}
	if got := f.calls[len(f.calls)-1].Hosts; strings.Join(got, ",") != "api.weather.example,files.weather.example:8443" {
		t.Errorf("hosts at call time = %v", got)
	}
}
