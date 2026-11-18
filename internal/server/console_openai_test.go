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

	"github.com/forgelinehq/forgeline/internal/executor"
)

const openAIKey = "sk-proj-openai-key-for-tests-0123456789"

// fakeOpenAI answers like the Chat Completions API and records each request.
type fakeOpenAI struct {
	srv  *httptest.Server
	mu   sync.Mutex
	reqs []openAIReq
	body string // answer this body with status instead of a reply, when status is non-zero
	code int
}

type openAIReq struct {
	Path, Auth string
	Header     http.Header
	Body       map[string]any
}

func newFakeOpenAI(t *testing.T, reply string) *fakeOpenAI {
	f := &fakeOpenAI{}
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var b map[string]any
		json.NewDecoder(r.Body).Decode(&b)
		f.mu.Lock()
		f.reqs = append(f.reqs, openAIReq{Path: r.URL.Path, Auth: r.Header.Get("authorization"), Header: r.Header.Clone(), Body: b})
		code, body := f.code, f.body
		f.mu.Unlock()
		if code != 0 {
			w.WriteHeader(code)
			io.WriteString(w, body)
			return
		}
		json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{{"index": 0, "message": map[string]any{"role": "assistant", "content": reply}}},
			"usage":   map[string]int{"prompt_tokens": 1000, "completion_tokens": 200, "total_tokens": 1200}})
	}))
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakeOpenAI) last() openAIReq {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.reqs[len(f.reqs)-1]
}

func (f *fakeOpenAI) lastHeader(name string) string { return f.last().Header.Get(name) }

func (f *fakeOpenAI) fail(code int, body string) {
	f.mu.Lock()
	f.code, f.body = code, body
	f.mu.Unlock()
}

func openAIProvider(id, kind, base, key string, models ...map[string]any) map[string]any {
	return map[string]any{"id": id, "kind": kind, "name": id, "base_url": base, "api_key": key, "models": models}
}

func TestOpenAIProvider(t *testing.T) {
	h := newConsoleHarness(t)
	admin := h.adminBrowser("ada")
	llm := newFakeOpenAI(t, "OK")
	luna := map[string]any{"id": "gpt-6-luna", "on": true, "input_usd_per_mtok": 0.10, "output_usd_per_mtok": 0.50}

	for name, b := range map[string]map[string]any{
		"no key":          openAIProvider("openai", provOpenAI, llm.srv.URL+"/v1", "", luna),
		"short key":       openAIProvider("openai", provOpenAI, llm.srv.URL+"/v1", "sk-short", luna),
		"query":           openAIProvider("openai", provOpenAI, llm.srv.URL+"/v1?x=1", openAIKey, luna),
		"dot dot":         openAIProvider("openai", provOpenAI, llm.srv.URL+"/v1/../admin", openAIKey, luna),
		"plain http":      openAIProvider("openai", provOpenAI, "http://llm.example.com/v1", openAIKey, luna),
		"compat, no addr": openAIProvider("local", provOpenAICompat, "", "", luna),
	} {
		if resp, out := admin.do("POST", "/v1/providers", b); resp.StatusCode != 400 {
			t.Errorf("%s: status %d %s, want 400", name, resp.StatusCode, out)
		}
	}

	admin.expect(201, "POST", "/v1/providers", openAIProvider("openai", provOpenAI, llm.srv.URL+"/v1", openAIKey, luna))
	var tested struct {
		Reply   string  `json:"reply"`
		CostUSD float64 `json:"cost_usd"`
	}
	json.Unmarshal(admin.expect(200, "POST", "/v1/providers/openai/test", nil), &tested)
	r := llm.last()
	msgs, _ := r.Body["messages"].([]any)
	first, _ := msgs[0].(map[string]any)
	if tested.Reply != "OK" || r.Path != "/v1/chat/completions" || r.Auth != "Bearer "+openAIKey || r.Body["model"] != "gpt-6-luna" ||
		len(msgs) != 2 || first["role"] != "system" || r.Body["max_completion_tokens"] == nil || r.Body["max_tokens"] != nil {
		t.Fatalf("test = %+v, request %+v", tested, r)
	}
	if want := (1000*0.10 + 200*0.50) / 1e6; math.Abs(tested.CostUSD-want) > 1e-12 {
		t.Errorf("cost = %v, want %v", tested.CostUSD, want)
	}

	// Error shapes: OpenAI's own, a bare string, and an unknown model.
	for _, c := range []struct {
		code       int
		body, want string
	}{
		{401, `{"error":{"message":"Incorrect API key provided"}}`, "Incorrect API key provided"},
		{400, `{"error":"model is required"}`, "model is required"},
		{404, `{"error":{"message":"The model gpt-x does not exist"}}`, "gpt-6-luna"},
	} {
		llm.fail(c.code, c.body)
		if resp, out := admin.do("POST", "/v1/providers/openai/test", nil); resp.StatusCode != 502 || !strings.Contains(string(out), c.want) {
			t.Errorf("%d: %d %s, want %q", c.code, resp.StatusCode, out, c.want)
		}
	}
}

func TestOpenAICompatibleServerWithoutAKey(t *testing.T) {
	h := newHarness(t, &executor.Fake{})
	admin := h.adminBrowser("ada")
	llm := newFakeOpenAI(t, "Hi from a local model.")
	llama := map[string]any{"id": "meta-llama/Llama-3.3-70B", "on": true}
	admin.expect(201, "POST", "/v1/providers", openAIProvider("local", provOpenAICompat, llm.srv.URL+"/v1", "", llama))

	var list struct {
		Providers []ProviderJSON `json:"providers"`
	}
	json.Unmarshal(admin.expect(200, "GET", "/v1/providers", nil), &list)
	if p := list.Providers[0]; p.HasKey || p.Status != "untested" {
		t.Fatalf("provider = %+v", p)
	}

	// With the assistant off, an agent on the local model answers, and no key is sent.
	body := agentBody("Local", nil, nil, "T0")
	body["model"] = "local/meta-llama/Llama-3.3-70B"
	a := newAgent(t, admin, body)
	var reply ChatReply
	json.Unmarshal(admin.expect(200, "POST", "/v1/ai/agents/"+a.ID+"/chat", map[string]any{"message": "go"}), &reply)
	r := llm.last()
	if !strings.Contains(reply.Reply, "Hi from a local model.") || r.Auth != "" || r.Body["model"] != "meta-llama/Llama-3.3-70B" || r.Body["max_tokens"] == nil || r.Body["max_completion_tokens"] != nil {
		t.Fatalf("reply = %+v, request %+v", reply, r)
	}

	// A key, when given, is sent.
	b := openAIProvider("local", provOpenAICompat, llm.srv.URL+"/v1", "local-key", llama)
	admin.expect(200, "PUT", "/v1/providers/local", b)
	admin.expect(200, "POST", "/v1/providers/local/test", nil)
	if r := llm.last(); r.Auth != "Bearer local-key" {
		t.Fatalf("auth = %q", r.Auth)
	}
}

func TestGeminiAndAzureOpenAI(t *testing.T) {
	h := newConsoleHarness(t)
	admin := h.adminBrowser("ada")
	llm := newFakeOpenAI(t, "OK")
	flash := map[string]any{"id": "gemini-3.8-flash", "on": true, "input_usd_per_mtok": 1.5, "output_usd_per_mtok": 7.5}
	admin.expect(201, "POST", "/v1/providers", openAIProvider("gemini", provGemini, llm.srv.URL+"/v1beta/openai", openAIKey, flash))
	admin.expect(200, "POST", "/v1/providers/gemini/test", nil)
	if r := llm.last(); r.Path != "/v1beta/openai/chat/completions" || r.Auth != "Bearer "+openAIKey || r.Body["max_tokens"] == nil {
		t.Fatalf("gemini request = %+v", r)
	}

	dep := map[string]any{"id": "prod-gpt", "on": true, "input_usd_per_mtok": 1, "output_usd_per_mtok": 5}
	admin.expect(400, "POST", "/v1/providers", openAIProvider("azure", provAzureOpenAI, "", openAIKey, dep)) // no resource address
	admin.expect(201, "POST", "/v1/providers", openAIProvider("azure", provAzureOpenAI, llm.srv.URL+"/openai/v1", openAIKey, dep))
	admin.expect(200, "POST", "/v1/providers/azure/test", nil)
	if r := llm.last(); r.Path != "/openai/v1/chat/completions" || r.Auth != "" || r.Body["model"] != "prod-gpt" || r.Body["max_completion_tokens"] == nil {
		t.Fatalf("azure request = %+v", r)
	}
	// Azure takes its key in the api-key header, never as a bearer token.
	if got := llm.lastHeader("api-key"); got != openAIKey {
		t.Fatalf("api-key header = %q", got)
	}
}
