package server

import (
	"encoding/json"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/forgelinehq/forgeline/internal/authn"
	"github.com/forgelinehq/forgeline/internal/executor"
)

const providerKey = "sk-ant-api03-provider-key-for-tests-0123456789"

// fakeProvider answers like the Messages API, with token counts, and records what it was asked.
type fakeProvider struct {
	srv    *httptest.Server
	mu     sync.Mutex
	models []string
	keys   []string
	status int // answer this status instead of 200 when non-zero
	reply  string
}

func newFakeProvider(t *testing.T, reply string) *fakeProvider {
	f := &fakeProvider{reply: reply}
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var m struct {
			Model string `json:"model"`
		}
		json.NewDecoder(r.Body).Decode(&m)
		f.mu.Lock()
		f.models, f.keys = append(f.models, m.Model), append(f.keys, r.Header.Get("x-api-key"))
		status, reply := f.status, f.reply
		f.mu.Unlock()
		if status != 0 {
			w.WriteHeader(status)
			io.WriteString(w, `{"error":{"message":"overloaded"}}`)
			return
		}
		json.NewEncoder(w).Encode(map[string]any{"content": []map[string]string{{"type": "text", "text": reply}},
			"usage": map[string]int{"input_tokens": 1000, "output_tokens": 200}})
	}))
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakeProvider) calls() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.models)
}

func (f *fakeProvider) setStatus(n int) {
	f.mu.Lock()
	f.status = n
	f.mu.Unlock()
}

func anthropicProvider(id, base string, on ...string) map[string]any {
	models := []map[string]any{}
	for _, m := range providerCatalog[provAnthropic].Models {
		models = append(models, map[string]any{"id": m.ID, "label": m.Label, "on": contains(on, m.ID), "input_usd_per_mtok": m.InUSD, "output_usd_per_mtok": m.OutUSD})
	}
	return map[string]any{"id": id, "kind": "anthropic", "name": "Anthropic " + id, "base_url": base, "api_key": providerKey, "models": models}
}

func TestProvidersAreForAdministratorsAndNeverShowTheKey(t *testing.T) {
	h := newConsoleHarness(t)
	admin := h.adminBrowser("ada")
	admin.createUser("dev", authn.Developer)
	dev := h.login("dev")
	llm := newFakeProvider(t, "OK")

	body := anthropicProvider("anthropic", llm.srv.URL, "claude-haiku-5-5")
	dev.expect(403, "POST", "/v1/providers", body)
	for name, change := range map[string]func(map[string]any){
		"bad id":           func(b map[string]any) { b["id"] = "Not An Id" },
		"kind not built":   func(b map[string]any) { b["kind"] = "openai" },
		"no key":           func(b map[string]any) { b["api_key"] = "" },
		"plain http":       func(b map[string]any) { b["base_url"] = "http://llm.example.com" },
		"address path":     func(b map[string]any) { b["base_url"] = "https://llm.example.com/v1?x=1" },
		"negative limit":   func(b map[string]any) { b["monthly_usd"] = -1 },
		"unknown fallback": func(b map[string]any) { b["fallback"] = "nowhere/claude-haiku-5-5" },
		"fallback off":     func(b map[string]any) { b["fallback"] = "anthropic/claude-opus-5-5" },
		"model with slash": func(b map[string]any) {
			b["models"] = []map[string]any{{"id": "a/b", "on": true}}
		},
	} {
		b := anthropicProvider("anthropic", llm.srv.URL, "claude-haiku-5-5")
		change(b)
		if resp, _ := admin.do("POST", "/v1/providers", b); resp.StatusCode != 400 {
			t.Errorf("%s: status %d, want 400", name, resp.StatusCode)
		}
	}

	created := admin.expect(201, "POST", "/v1/providers", body)
	admin.expect(409, "POST", "/v1/providers", body)
	list := dev.expect(200, "GET", "/v1/providers", nil)
	for _, out := range [][]byte{created, list} {
		if strings.Contains(string(out), providerKey) {
			t.Fatal("the key came back")
		}
	}
	var got struct {
		Providers []ProviderJSON     `json:"providers"`
		Kinds     []ProviderKindJSON `json:"kinds"`
	}
	json.Unmarshal(list, &got)
	if len(got.Providers) != 1 || !got.Providers[0].HasKey || got.Providers[0].Status != "untested" || len(got.Kinds) < 2 || !got.Kinds[0].Built || got.Kinds[1].Built {
		t.Fatalf("list = %+v", got)
	}
	// The key is an internal secret: not in the vault listing, not usable by steps.
	if strings.Contains(string(admin.expect(200, "GET", "/v1/secrets", nil)), "provider") {
		t.Error("the provider key is listed with the step secrets")
	}

	var tested struct {
		Reply   string  `json:"reply"`
		CostUSD float64 `json:"cost_usd"`
	}
	json.Unmarshal(admin.expect(200, "POST", "/v1/providers/anthropic/test", nil), &tested)
	if tested.Reply != "OK" || llm.keys[0] != providerKey || llm.models[0] != "claude-haiku-5-5" {
		t.Fatalf("test = %+v, keys %v, models %v", tested, llm.keys, llm.models)
	}
	// 1000 in at $0.10 and 200 out at $0.50 a million.
	if want := (1000*0.10 + 200*0.50) / 1e6; math.Abs(tested.CostUSD-want) > 1e-12 {
		t.Errorf("cost = %v, want %v", tested.CostUSD, want)
	}

	// Changing it without a key keeps the key; removing it removes the key too.
	b := anthropicProvider("anthropic", llm.srv.URL, "claude-haiku-5-5", "claude-sonnet-5-5")
	b["api_key"] = ""
	admin.expect(200, "PUT", "/v1/providers/anthropic", b)
	admin.expect(200, "POST", "/v1/providers/anthropic/test", nil)
	if k := llm.keys[len(llm.keys)-1]; k != providerKey {
		t.Fatalf("after an update without a key, the provider got key %q", k)
	}
	var models struct {
		Models []ModelChoiceJSON `json:"models"`
	}
	json.Unmarshal(dev.expect(200, "GET", "/v1/models", nil), &models)
	if len(models.Models) != 2 || models.Models[0].Ref != "anthropic/claude-haiku-5-5" {
		t.Fatalf("models = %+v", models)
	}
	admin.expect(204, "DELETE", "/v1/providers/anthropic", nil)
	if _, ok := h.srv.internalSecretValue(t.Context(), providerKeyName("anthropic")); ok {
		t.Fatal("removing the provider left its key in the vault")
	}
}

func TestProviderRefusesPrivateAddressesUnlessAllowed(t *testing.T) {
	h := newConsoleHarness(t)
	admin := h.adminBrowser("ada")
	admin.expect(201, "POST", "/v1/providers", anthropicProvider("inside", "https://10.0.0.1", "claude-haiku-5-5"))
	resp, out := admin.do("POST", "/v1/providers/inside/test", nil)
	if code := resp.StatusCode; code != 502 || !strings.Contains(string(out), "private or reserved address") {
		t.Fatalf("test of a private address = %d %s", resp.StatusCode, out)
	}
	// The cloud metadata address too.
	admin.expect(201, "POST", "/v1/providers", anthropicProvider("meta", "https://169.254.169.254", "claude-haiku-5-5"))
	if resp, out := admin.do("POST", "/v1/providers/meta/test", nil); resp.StatusCode != 502 || !strings.Contains(string(out), "private or reserved address") {
		t.Fatalf("test of the metadata address = %d %s", resp.StatusCode, out)
	}
}

func TestAgentWithAProviderModelWorksWhileTheAssistantIsOff(t *testing.T) {
	h := newHarness(t, &executor.Fake{})
	admin := h.adminBrowser("ada")
	llm := newFakeProvider(t, "All good.")

	a := newAgent(t, admin, agentBody("Triage", nil, nil, "T0"))
	admin.expect(409, "POST", "/v1/ai/agents/"+a.ID+"/chat", map[string]any{"message": "go"}) // nothing can answer yet

	admin.expect(201, "POST", "/v1/providers", anthropicProvider("anthropic", llm.srv.URL, "claude-haiku-5-5"))
	off := agentBody("Opus", nil, nil, "T0")
	off["model"] = "anthropic/claude-opus-5-5"
	admin.expect(400, "POST", "/v1/ai/agents", off) // a model that is not turned on
	off["model"] = "elsewhere/claude-haiku-5-5"
	admin.expect(400, "POST", "/v1/ai/agents", off) // a provider that does not exist

	body := agentBody("Quick", nil, nil, "T0")
	body["model"] = "anthropic/claude-haiku-5-5"
	q := newAgent(t, admin, body)
	var reply ChatReply
	json.Unmarshal(admin.expect(200, "POST", "/v1/ai/agents/"+q.ID+"/chat", map[string]any{"message": "go"}), &reply)
	if !strings.Contains(reply.Reply, "All good.") || llm.models[len(llm.models)-1] != "claude-haiku-5-5" {
		t.Fatalf("reply = %+v, models %v", reply, llm.models)
	}
	// An agent with no model of its own still needs the assistant.
	if resp, _ := admin.do("POST", "/v1/ai/agents/"+a.ID+"/chat", map[string]any{"message": "go"}); resp.StatusCode == 200 {
		t.Error("an agent without a model answered with the assistant off")
	}

	var calls struct {
		Calls []ModelCall `json:"calls"`
	}
	json.Unmarshal(admin.expect(200, "GET", "/v1/model-calls?provider=anthropic", nil), &calls)
	if len(calls.Calls) == 0 || calls.Calls[0].Who != "Quick" || calls.Calls[0].Model != "claude-haiku-5-5" || calls.Calls[0].CostUSD <= 0 || calls.Calls[0].Error != "" {
		t.Fatalf("calls = %+v", calls.Calls)
	}
	var list struct {
		Providers []ProviderJSON `json:"providers"`
	}
	json.Unmarshal(admin.expect(200, "GET", "/v1/providers", nil), &list)
	if p := list.Providers[0]; p.Status != "working" || p.Month.Calls == 0 || p.Month.CostUSD <= 0 {
		t.Fatalf("provider = %+v", p)
	}
}

func TestProviderLimitAndFallback(t *testing.T) {
	h := newHarness(t, &executor.Fake{})
	admin := h.adminBrowser("ada")
	main, backup := newFakeProvider(t, "from main"), newFakeProvider(t, "from backup")
	admin.expect(201, "POST", "/v1/providers", anthropicProvider("backup", backup.srv.URL, "claude-haiku-5-5"))
	p := anthropicProvider("main", main.srv.URL, "claude-haiku-5-5")
	p["monthly_usd"] = 0.0001 // one call (0.0002) is over it
	admin.expect(201, "POST", "/v1/providers", p)
	body := agentBody("Quick", nil, nil, "T0")
	body["model"] = "main/claude-haiku-5-5"
	a := newAgent(t, admin, body)
	ask := func() (int, string) {
		resp, out := admin.do("POST", "/v1/ai/agents/"+a.ID+"/chat", map[string]any{"message": "go"})
		return resp.StatusCode, string(out)
	}

	if code, out := ask(); code != 200 || !strings.Contains(out, "from main") {
		t.Fatalf("first call = %d %s", code, out)
	}
	// Over the limit with no fallback: refused, and the provider is not called.
	if code, out := ask(); code == 200 || !strings.Contains(out, "monthly limit") || main.calls() != 1 {
		t.Fatalf("over the limit = %d %s (main called %d times)", code, out, main.calls())
	}
	// With a fallback, the call goes there instead.
	p["fallback"], p["api_key"] = "backup/claude-haiku-5-5", ""
	admin.expect(200, "PUT", "/v1/providers/main", p)
	if code, out := ask(); code != 200 || !strings.Contains(out, "from backup") || main.calls() != 1 {
		t.Fatalf("fallback on the limit = %d %s", code, out)
	}

	// Without a limit: overloaded falls back; a refused key does not.
	p["monthly_usd"] = 0
	admin.expect(200, "PUT", "/v1/providers/main", p)
	main.setStatus(529)
	if code, out := ask(); code != 200 || !strings.Contains(out, "from backup") {
		t.Fatalf("fallback when overloaded = %d %s", code, out)
	}
	main.setStatus(401)
	before := backup.calls()
	if code, out := ask(); code == 200 || backup.calls() != before || !strings.Contains(out, "401") {
		t.Fatalf("a refused key fell back: %d %s", code, out)
	}
}

func TestAssistantCanUseAProviderModel(t *testing.T) {
	h := newHarness(t, &executor.Fake{})
	admin := h.adminBrowser("ada")
	llm := newFakeProvider(t, "Hello from the provider.")
	admin.expect(400, "PUT", "/v1/integrations/assistant", map[string]any{"backend": "provider", "model": "claude-haiku-5-5"})           // bare name
	admin.expect(400, "PUT", "/v1/integrations/assistant", map[string]any{"backend": "provider", "model": "anthropic/claude-haiku-5-5"}) // no such provider yet
	admin.expect(201, "POST", "/v1/providers", anthropicProvider("anthropic", llm.srv.URL, "claude-haiku-5-5"))
	admin.expect(400, "PUT", "/v1/integrations/assistant", map[string]any{"backend": "cli", "model": "anthropic/claude-haiku-5-5"}) // a provider model needs the provider backend
	var cfg AssistantJSON
	json.Unmarshal(admin.expect(200, "PUT", "/v1/integrations/assistant", map[string]any{"backend": "provider", "model": "anthropic/claude-haiku-5-5"}), &cfg)
	if !cfg.Ready {
		t.Fatalf("config = %+v", cfg)
	}
	var reply ChatReply
	json.Unmarshal(admin.expect(200, "POST", "/v1/assistant/chat", map[string]any{"message": "hi"}), &reply)
	if !strings.Contains(reply.Reply, "Hello from the provider.") {
		t.Fatalf("reply = %+v", reply)
	}
	var calls struct {
		Calls []ModelCall `json:"calls"`
	}
	json.Unmarshal(admin.expect(200, "GET", "/v1/model-calls", nil), &calls)
	if len(calls.Calls) != 1 || calls.Calls[0].Who != "Assistant" || calls.Calls[0].Provider != "anthropic" {
		t.Fatalf("calls = %+v", calls.Calls)
	}
}
