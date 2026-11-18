package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/forgelinehq/forgeline/internal/docstore"
)

// Models and providers (design note 045, part 1). An administrator adds providers; each lists its models, and a
// model must be turned on before an agent or the assistant can use it. A model chosen from a provider is written
// `provider/model` (anthropic/claude-haiku-5-5). Such a model works whatever the assistant's own backend is, so agents
// and workflows no longer depend on that single setting. A bare model name keeps using the assistant's backend, so
// agent versions made before providers keep their fingerprints and their behaviour.
//
// Anthropic, OpenAI, OpenAI-compatible servers (Ollama, vLLM, LiteLLM, OpenRouter, Groq…), Gemini, Azure OpenAI and
// AWS Bedrock (through an AWS account the console is connected to, with no key of its own) are built. Keys go into the vault and are never shown again. A provider's address gets the
// MCP server rules (note 042): https only, no redirects, and no private or reserved address unless an administrator
// says the provider is on a private network. Every call is priced from the model's prices, counted per provider per
// month, and kept in a short log of recent calls. A provider can have a monthly limit and a fallback model, used when
// the limit is reached or the provider is unavailable.

const (
	kindProvider     = "provider"
	kindProviderUse  = "provideruse"
	kindModelCalls   = "modelcalls"
	idRecentCalls    = "recent"
	maxProviders     = 20
	maxProviderModel = 50
	maxRecentCalls   = 200
	maxMonthlyUSD    = 1_000_000
	maxModelPrice    = 1000 // dollars per million tokens
	providerMaxToken = 8000
	provAnthropic    = "anthropic"
	provOpenAI       = "openai"
	provOpenAICompat = "openai-compatible"
	defaultOpenAI    = "https://api.openai.com/v1"
	provGemini       = "gemini"
	defaultGemini    = "https://generativelanguage.googleapis.com/v1beta/openai"
	provAzureOpenAI  = "azure-openai"
	provBedrock      = "bedrock"
	assistProvider   = "provider" // the assistant's backend when its model comes from a provider
)

var (
	providerIDRE = regexp.MustCompile(`^[a-z][a-z0-9-]{0,29}$`)
	// providerModelRE is a model's name inside its provider. It is only ever sent in a JSON body, never put in a path
	// or on a command line. Slashes and capitals are allowed for servers that name models like meta-llama/Llama-3.3-70B.
	providerModelRE = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:/\-]{0,99}$`)
)

// providerCatalog is what Forgeline knows about each kind: its models and their list prices in dollars per million
// tokens (input, output). Prices are filled in when a provider is added and can be changed there.
var providerCatalog = map[string]struct {
	Label  string
	Models []ProviderModel
}{
	provAnthropic: {Label: "Anthropic", Models: []ProviderModel{
		{ID: "claude-haiku-5-5", Label: "Claude Haiku 5.5", InUSD: 0.10, OutUSD: 0.50},
		{ID: "claude-sonnet-5-5", Label: "Claude Sonnet 5.5", InUSD: 2, OutUSD: 10},
		{ID: "claude-opus-5-5", Label: "Claude Opus 5.5", InUSD: 4, OutUSD: 20},
		{ID: "claude-fable-5-2", Label: "Claude Fable 5.2", InUSD: 8, OutUSD: 40},
	}},
	// From OpenAI's model pages, November 2026.
	provOpenAI: {Label: "OpenAI", Models: []ProviderModel{
		{ID: "gpt-6-luna", Label: "GPT-6 Luna", InUSD: 0.10, OutUSD: 0.50},
		{ID: "gpt-6.1-terra", Label: "GPT-6.1 Terra", InUSD: 1, OutUSD: 5},
		{ID: "gpt-6.1-sol", Label: "GPT-6.1 Sol", InUSD: 2, OutUSD: 10},
	}},
	// From Google's Gemini API price list, November 2026 (prompts up to 200K tokens; Flash at its list price, not the
	// introductory one that ends on 2026-12-31).
	provGemini: {Label: "Google Gemini", Models: []ProviderModel{
		{ID: "gemini-3.1-flash-lite", Label: "Gemini 3.1 Flash-Lite", InUSD: 0.25, OutUSD: 1.50},
		{ID: "gemini-3.8-flash", Label: "Gemini 3.8 Flash", InUSD: 1.50, OutUSD: 7.50},
		{ID: "gemini-3.1-pro-preview", Label: "Gemini 3.1 Pro (preview)", InUSD: 2, OutUSD: 12},
	}},
	// An OpenAI-compatible server, Azure OpenAI (models are the administrator's deployment names) and Bedrock (model
	// ids and inference profiles vary by account and region) have no known models: the administrator adds them by name.
}

// keyOptional says whether a kind of provider may have no key: a local Ollama or vLLM usually needs none.
func keyOptional(kind string) bool { return kind == provOpenAICompat || kind == provBedrock }

// builtKind says whether a kind of provider can be added.
func builtKind(kind string) bool {
	for _, k := range providerKinds {
		if k.Kind == kind {
			return k.Built
		}
	}
	return false
}

// providerKinds lists every kind note 045 plans, in the order the console shows them. Only the built ones can be added.
var providerKinds = []ProviderKindJSON{
	{Kind: provAnthropic, Label: "Anthropic", How: "API key", Built: true},
	{Kind: provOpenAI, Label: "OpenAI", How: "API key", Built: true},
	{Kind: provBedrock, Label: "AWS Bedrock", How: "an AWS account the console is connected to", Built: true},
	{Kind: provGemini, Label: "Google Gemini", How: "API key", Built: true},
	{Kind: provAzureOpenAI, Label: "Azure OpenAI", How: "endpoint and key", Built: true},
	{Kind: provOpenAICompat, Label: "OpenAI-compatible", How: "Ollama, vLLM, LiteLLM, OpenRouter, Groq…", Built: true},
}

// ProviderKindJSON is one kind of provider.
type ProviderKindJSON struct {
	Kind   string          `json:"kind"`
	Label  string          `json:"label"`
	How    string          `json:"how"`
	Built  bool            `json:"built"`
	Models []ProviderModel `json:"models,omitempty"` // the known models, with list prices
}

// ProviderModel is one model a provider offers.
type ProviderModel struct {
	ID     string  `json:"id"`
	Label  string  `json:"label,omitempty"`
	On     bool    `json:"on"`
	InUSD  float64 `json:"input_usd_per_mtok"`
	OutUSD float64 `json:"output_usd_per_mtok"`
}

// Provider is one provider an administrator added. Its key is in the vault under providerKeyName(ID).
type Provider struct {
	ID           string          `json:"id"`
	Kind         string          `json:"kind"`
	Name         string          `json:"name"`
	BaseURL      string          `json:"base_url,omitempty"`
	AllowPrivate bool            `json:"allow_private,omitempty"`
	AWSAccount   string          `json:"aws_account,omitempty"` // Bedrock: an added account's id; empty is the primary
	Region       string          `json:"region,omitempty"`      // Bedrock: the region, when not the account's own
	Models       []ProviderModel `json:"models"`
	MonthlyUSD   float64         `json:"monthly_usd,omitempty"` // zero means no limit
	Fallback     string          `json:"fallback,omitempty"`    // provider/model
	UpdatedBy    string          `json:"updated_by"`
	UpdatedAt    time.Time       `json:"updated_at"`
}

func providerKeyName(id string) string { return "forgeline.provider." + id + ".key" }

func (p Provider) model(id string) (ProviderModel, bool) {
	for _, m := range p.Models {
		if m.ID == id {
			return m, true
		}
	}
	return ProviderModel{}, false
}

// splitModelRef splits provider/model. ok is false for a bare model name.
func splitModelRef(ref string) (provider, model string, ok bool) {
	provider, model, ok = strings.Cut(ref, "/")
	return provider, model, ok
}

func (s *Server) loadProvider(ctx context.Context, id string) (Provider, error) {
	var p Provider
	err := s.docs.Get(ctx, kindProvider, id, &p)
	return p, err
}

func (s *Server) listProviders(ctx context.Context) ([]Provider, error) {
	docs, err := s.docs.List(ctx, kindProvider, docstore.ListOptions{})
	if err != nil {
		return nil, err
	}
	out := make([]Provider, 0, len(docs))
	for _, d := range docs {
		var p Provider
		if json.Unmarshal(d.Body, &p) == nil {
			out = append(out, p)
		}
	}
	return out, nil
}

// checkModelRef says what is wrong with provider/model, or "" when an agent or the assistant can use it.
func (s *Server) checkModelRef(ctx context.Context, ref string) string {
	prov, model, ok := splitModelRef(ref)
	if !ok || !providerIDRE.MatchString(prov) || !providerModelRE.MatchString(model) {
		return "a model from a provider is written provider/model, such as anthropic/claude-haiku-5-5"
	}
	p, err := s.loadProvider(ctx, prov)
	if err != nil {
		return fmt.Sprintf("there is no provider %q; add it under Settings › Models and providers", prov)
	}
	m, ok := p.model(model)
	if !ok || !m.On {
		return fmt.Sprintf("the model %q is not turned on for provider %q", model, prov)
	}
	return ""
}

// aiReady says whether any model can answer: the assistant's backend, or a provider with a key and a model turned on.
func (s *Server) aiReady(ctx context.Context) bool {
	if s.loadAssistant(ctx).Backend != assistOff {
		return true
	}
	ps, _ := s.listProviders(ctx)
	for _, p := range ps {
		if _, ok := s.internalSecretValue(ctx, providerKeyName(p.ID)); !ok && !keyOptional(p.Kind) {
			continue
		}
		for _, m := range p.Models {
			if m.On {
				return true
			}
		}
	}
	return false
}

const noModelSetUp = "no model is set up. An administrator can add one under Settings › Models and providers, or turn on the assistant"

// --- calls ---

// providerError is a provider call that failed; transient ones (unreachable, rate limited, overloaded) may fall back.
type providerError struct {
	msg       string
	transient bool
}

func (e *providerError) Error() string { return e.msg }

// providerHTTPClient is replaced in tests that need a fake provider on a private address.
var providerHTTPClient = func(p Provider) *http.Client {
	u, _ := url.Parse(p.BaseURL)
	return mcpHTTPClient(p.AllowPrivate, u != nil && u.Scheme == "http" && isLoopbackHost(u.Hostname()))
}

// callProvider asks provider/model. When the provider is over its monthly limit, or the call fails for a reason that
// may pass, its fallback is asked instead, once.
func (s *Server) callProvider(ctx context.Context, ref, system string, turns []chatTurn) (string, modelUsage, error) {
	reply, use, err := s.callProviderOnce(ctx, ref, system, turns)
	var pe *providerError
	if err == nil || !errors.As(err, &pe) || !pe.transient {
		return reply, use, err
	}
	prov, _, _ := splitModelRef(ref)
	p, lerr := s.loadProvider(ctx, prov)
	if lerr != nil || p.Fallback == "" || p.Fallback == ref {
		return reply, use, err
	}
	if msg := s.checkModelRef(ctx, p.Fallback); msg != "" {
		return reply, use, fmt.Errorf("%v; its fallback %s cannot be used: %s", err, p.Fallback, msg)
	}
	reply, use, ferr := s.callProviderOnce(ctx, p.Fallback, system, turns)
	if ferr != nil {
		return reply, use, fmt.Errorf("%v; its fallback %s failed too: %v", err, p.Fallback, ferr)
	}
	return reply, use, nil
}

func (s *Server) callProviderOnce(ctx context.Context, ref, system string, turns []chatTurn) (string, modelUsage, error) {
	if msg := s.checkModelRef(ctx, ref); msg != "" {
		return "", modelUsage{}, errors.New(msg)
	}
	prov, model, _ := splitModelRef(ref)
	p, err := s.loadProvider(ctx, prov)
	if err != nil {
		return "", modelUsage{}, err
	}
	m, _ := p.model(model)
	if p.MonthlyUSD > 0 {
		if spent := s.providerSpend(ctx, p.ID, monthOf(time.Now())); spent.CostUSD >= p.MonthlyUSD {
			err := &providerError{msg: fmt.Sprintf("provider %q reached its monthly limit of $%.2f", p.ID, p.MonthlyUSD), transient: true}
			s.noteCall(ctx, ModelCall{Provider: p.ID, Model: model, Error: err.msg})
			return "", modelUsage{}, err
		}
	}
	key, ok := s.internalSecretValue(ctx, providerKeyName(p.ID))
	if !ok && !keyOptional(p.Kind) {
		return "", modelUsage{}, fmt.Errorf("provider %q has no API key", p.ID)
	}
	ctx, cancel := context.WithTimeout(ctx, assistTimeout)
	defer cancel()
	start := time.Now()
	var (
		reply  string
		use    modelUsage
		status int
	)
	switch p.Kind {
	case provBedrock:
		reply, use, status, err = s.bedrockConverse(ctx, p, model, system, turns, providerMaxToken)
	case provOpenAI, provOpenAICompat, provGemini, provAzureOpenAI:
		base := p.BaseURL
		if base == "" && p.Kind == provGemini {
			base = defaultGemini
		} else if base == "" {
			base = defaultOpenAI
		}
		reply, use, status, err = openAIChat(ctx, providerHTTPClient(p), base, key, model, system, turns, providerMaxToken, openAIStyles[p.Kind])
	default:
		base := p.BaseURL
		if base == "" {
			base = defaultAnthropic
		}
		reply, use, status, err = anthropicMessages(ctx, providerHTTPClient(p), base, key, model, system, turns, providerMaxToken)
	}
	ms := time.Since(start).Milliseconds()
	use.Provider, use.Model, use.Millis = p.ID, model, ms
	if err == nil {
		use.CostUSD = (float64(use.Input)*m.InUSD + float64(use.Output)*m.OutUSD) / 1e6
		use.Costed = 1
	}
	call := ModelCall{Provider: p.ID, Model: model, Input: use.Input, Output: use.Output, CostUSD: use.CostUSD, Millis: ms}
	if err != nil {
		call.Error = truncate(err.Error(), 240)
		transient := status == 0 || status == http.StatusTooManyRequests || status >= 500
		if ctx.Err() != nil {
			err = errors.New("the model took too long")
		}
		s.noteCall(ctx, call)
		return "", use, &providerError{msg: err.Error(), transient: transient}
	}
	s.noteCall(ctx, call)
	return reply, use, nil
}

// --- the log of recent calls, and spend per provider ---

// ModelCall is one call in the log of recent calls.
type ModelCall struct {
	At       time.Time `json:"at"`
	Provider string    `json:"provider"` // a provider's id, or the assistant's backend (claude-token, cli, api, copilot)
	Model    string    `json:"model"`
	Who      string    `json:"who"` // the agent's name, or "Assistant"
	Input    int64     `json:"input_tokens"`
	Output   int64     `json:"output_tokens"`
	CostUSD  float64   `json:"cost_usd"`
	Millis   int64     `json:"millis"`
	Error    string    `json:"error,omitempty"`
}

type recentCalls struct {
	Calls []ModelCall `json:"calls"` // newest first
}

// ProviderUse is what one provider used in one month.
type ProviderUse struct {
	Provider string `json:"provider"`
	Month    string `json:"month"`
	Failed   int64  `json:"failed_calls"`
	modelUsage
}

type callLog struct{ mu sync.Mutex }

// callWhoKey names the caller in the log when it is neither an agent nor the assistant.
type callWhoKey struct{}

// noteCall adds a call to the log and, for a provider, to its month.
func (s *Server) noteCall(ctx context.Context, c ModelCall) {
	ctx = context.WithoutCancel(ctx)
	if c.At.IsZero() {
		c.At = time.Now().UTC()
	}
	if who, _ := ctx.Value(callWhoKey{}).(string); c.Who == "" && who != "" {
		c.Who = who
	}
	if c.Who == "" {
		if _, name := usageWho(ctx); name != "" {
			c.Who = name
		} else {
			c.Who = "Assistant"
		}
	}
	s.calls.mu.Lock()
	defer s.calls.mu.Unlock()
	var log recentCalls
	s.docs.Get(ctx, kindModelCalls, idRecentCalls, &log)
	log.Calls = append([]ModelCall{c}, log.Calls...)
	if len(log.Calls) > maxRecentCalls {
		log.Calls = log.Calls[:maxRecentCalls]
	}
	if err := s.docs.Put(ctx, kindModelCalls, idRecentCalls, log); err != nil {
		s.log.Error("saving the model call log", "err", err)
	}
	if _, isBackend := backendNames[c.Provider]; isBackend || c.Provider == "" {
		return
	}
	month := monthOf(c.At)
	u := s.providerSpend(ctx, c.Provider, month)
	u.Calls++
	if c.Error != "" {
		u.Failed++
	} else {
		u.Input += c.Input
		u.Output += c.Output
		u.Millis += c.Millis
		u.CostUSD += c.CostUSD
		u.Costed++
	}
	if err := s.docs.Put(ctx, kindProviderUse, c.Provider+"@"+month, u); err != nil {
		s.log.Error("saving provider use", "provider", c.Provider, "err", err)
	}
}

var backendNames = map[string]bool{assistAPI: true, assistCLI: true, assistToken: true, assistCopilot: true}

func (s *Server) providerSpend(ctx context.Context, id, month string) ProviderUse {
	u := ProviderUse{Provider: id, Month: month}
	s.docs.Get(ctx, kindProviderUse, id+"@"+month, &u)
	return u
}

func (s *Server) recentModelCalls(ctx context.Context) []ModelCall {
	var log recentCalls
	s.docs.Get(ctx, kindModelCalls, idRecentCalls, &log)
	if log.Calls == nil {
		log.Calls = []ModelCall{}
	}
	return log.Calls
}

// --- API ---

// ProviderJSON is a provider as the console shows it; the key is never returned.
type ProviderJSON struct {
	Provider
	HasKey       bool        `json:"has_key"`
	Month        ProviderUse `json:"month"`
	MedianMillis int64       `json:"median_millis,omitempty"` // of its recent successful calls
	Status       string      `json:"status"`                  // working, failing, untested or no-key
	LastError    string      `json:"last_error,omitempty"`
}

func (s *Server) providerView(ctx context.Context, p Provider, calls []ModelCall) ProviderJSON {
	_, hasKey := s.internalSecretValue(ctx, providerKeyName(p.ID))
	v := ProviderJSON{Provider: p, HasKey: hasKey, Month: s.providerSpend(ctx, p.ID, monthOf(time.Now())), Status: "untested"}
	var ms []int64
	seen := false
	for _, c := range calls {
		if c.Provider != p.ID {
			continue
		}
		if !seen {
			seen = true
			if c.Error != "" {
				v.Status, v.LastError = "failing", c.Error
			} else {
				v.Status = "working"
			}
		}
		if c.Error == "" {
			ms = append(ms, c.Millis)
		}
	}
	if len(ms) > 0 {
		sort.Slice(ms, func(i, j int) bool { return ms[i] < ms[j] })
		v.MedianMillis = ms[len(ms)/2]
	}
	if !hasKey && !keyOptional(p.Kind) {
		v.Status = "no-key"
	}
	return v
}

func (s *Server) handleListProviders(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	ps, err := s.listProviders(ctx)
	if err != nil {
		s.internalError(w, "listing providers", err)
		return
	}
	calls := s.recentModelCalls(ctx)
	out := make([]ProviderJSON, 0, len(ps))
	for _, p := range ps {
		out = append(out, s.providerView(ctx, p, calls))
	}
	kinds := make([]ProviderKindJSON, 0, len(providerKinds))
	for _, k := range providerKinds {
		k.Models = providerCatalog[k.Kind].Models
		kinds = append(kinds, k)
	}
	writeJSON(w, http.StatusOK, map[string]any{"providers": out, "kinds": kinds})
}

// ModelChoiceJSON is one model an agent or the assistant can be given.
type ModelChoiceJSON struct {
	Ref          string  `json:"ref"` // provider/model
	Provider     string  `json:"provider"`
	ProviderName string  `json:"provider_name"`
	Kind         string  `json:"kind"`
	Model        string  `json:"model"`
	Label        string  `json:"label"`
	InUSD        float64 `json:"input_usd_per_mtok"`
	OutUSD       float64 `json:"output_usd_per_mtok"`
}

// handleListModels lists the models turned on, for the model pickers.
func (s *Server) handleListModels(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	ps, err := s.listProviders(ctx)
	if err != nil {
		s.internalError(w, "listing providers", err)
		return
	}
	out := []ModelChoiceJSON{}
	for _, p := range ps {
		for _, m := range p.Models {
			if !m.On {
				continue
			}
			label := m.Label
			if label == "" {
				label = m.ID
			}
			out = append(out, ModelChoiceJSON{Ref: p.ID + "/" + m.ID, Provider: p.ID, ProviderName: p.Name, Kind: p.Kind, Model: m.ID, Label: label, InUSD: m.InUSD, OutUSD: m.OutUSD})
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"models": out})
}

func (s *Server) handleListModelCalls(w http.ResponseWriter, r *http.Request) {
	calls := s.recentModelCalls(r.Context())
	if p := r.URL.Query().Get("provider"); p != "" {
		kept := []ModelCall{}
		for _, c := range calls {
			if c.Provider == p {
				kept = append(kept, c)
			}
		}
		calls = kept
	}
	writeJSON(w, http.StatusOK, map[string]any{"calls": calls})
}

type providerRequest struct {
	ID           string          `json:"id"`
	Kind         string          `json:"kind"`
	Name         string          `json:"name"`
	BaseURL      string          `json:"base_url"`
	AllowPrivate bool            `json:"allow_private"`
	AWSAccount   string          `json:"aws_account"`
	Region       string          `json:"region"`
	APIKey       string          `json:"api_key"` // write-only; empty keeps the stored key
	Models       []ProviderModel `json:"models"`
	MonthlyUSD   float64         `json:"monthly_usd"`
	Fallback     string          `json:"fallback"`
}

// checkProvider returns the provider a request describes, or what is wrong with it.
func (s *Server) checkProvider(ctx context.Context, req providerRequest) (Provider, string) {
	req.Name, req.BaseURL, req.APIKey, req.Fallback = strings.TrimSpace(req.Name), strings.TrimSpace(req.BaseURL), strings.TrimSpace(req.APIKey), strings.TrimSpace(req.Fallback)
	switch {
	case !providerIDRE.MatchString(req.ID):
		return Provider{}, "id: lower-case letters, digits and dashes, starting with a letter, at most 30"
	case req.Name == "" || len(req.Name) > 60:
		return Provider{}, "name: give a name of at most 60 characters"
	case !builtKind(req.Kind):
		return Provider{}, `kind: one of "anthropic", "openai", "openai-compatible", "gemini", "azure-openai" or "bedrock"`
	case req.Kind == provOpenAICompat && req.BaseURL == "":
		return Provider{}, "base_url: an OpenAI-compatible provider needs its address, such as http://localhost:11434/v1 for Ollama"
	case req.Kind == provAzureOpenAI && req.BaseURL == "":
		return Provider{}, "base_url: give the resource's address, such as https://my-resource.openai.azure.com/openai/v1"
	case req.Kind == provBedrock && (req.BaseURL != "" || req.APIKey != "" || req.AllowPrivate):
		return Provider{}, "bedrock: it uses an AWS account the console is connected to; give no address or key"
	case req.Kind != provBedrock && (req.AWSAccount != "" || req.Region != ""):
		return Provider{}, "aws_account and region are only for a Bedrock provider"
	case req.Region != "" && !awsRegionRE.MatchString(req.Region):
		return Provider{}, "region: an AWS region such as us-east-1"
	case len(req.Models) > maxProviderModel:
		return Provider{}, fmt.Sprintf("models: at most %d", maxProviderModel)
	case req.MonthlyUSD < 0 || req.MonthlyUSD > maxMonthlyUSD || math.IsNaN(req.MonthlyUSD):
		return Provider{}, "monthly_usd: a limit in dollars, or 0 for none"
	case req.APIKey != "" && ((len(req.APIKey) < 20 && !keyOptional(req.Kind)) || len(req.APIKey) > 400 || strings.ContainsAny(req.APIKey, " \t\r\n")):
		return Provider{}, "api_key: that does not look like an API key"
	}
	if req.BaseURL != "" {
		if err := checkProviderURL(req.Kind, req.BaseURL); err != nil {
			return Provider{}, "base_url: " + err.Error()
		}
	}
	if req.Kind == provBedrock {
		if _, err := s.awsFor(ctx, req.AWSAccount); err != nil {
			return Provider{}, "aws_account: " + err.Error()
		}
	}
	p := Provider{ID: req.ID, Kind: req.Kind, Name: req.Name, BaseURL: req.BaseURL, AllowPrivate: req.AllowPrivate, AWSAccount: req.AWSAccount, Region: req.Region, MonthlyUSD: req.MonthlyUSD, Models: []ProviderModel{}}
	seen := map[string]bool{}
	for i, m := range req.Models {
		switch {
		case !providerModelRE.MatchString(m.ID):
			return Provider{}, fmt.Sprintf("models[%d]: %q is not a model name", i, m.ID)
		case seen[m.ID]:
			return Provider{}, fmt.Sprintf("models[%d]: %q is listed twice", i, m.ID)
		case m.InUSD < 0 || m.OutUSD < 0 || m.InUSD > maxModelPrice || m.OutUSD > maxModelPrice || math.IsNaN(m.InUSD) || math.IsNaN(m.OutUSD):
			return Provider{}, fmt.Sprintf("models[%d]: prices are dollars per million tokens, from 0 to %d", i, maxModelPrice)
		case len(m.Label) > 60:
			return Provider{}, fmt.Sprintf("models[%d]: the label is longer than 60 characters", i)
		}
		seen[m.ID] = true
		p.Models = append(p.Models, ProviderModel{ID: m.ID, Label: strings.TrimSpace(m.Label), On: m.On, InUSD: m.InUSD, OutUSD: m.OutUSD})
	}
	if req.Fallback != "" {
		prov, model, ok := splitModelRef(req.Fallback)
		switch {
		case !ok:
			return Provider{}, "fallback: a model written provider/model"
		case prov == p.ID:
			if m, ok := p.model(model); !ok || !m.On {
				return Provider{}, fmt.Sprintf("fallback: the model %q is not turned on here", model)
			}
		default:
			if msg := s.checkModelRef(ctx, req.Fallback); msg != "" {
				return Provider{}, "fallback: " + msg
			}
		}
		p.Fallback = req.Fallback
	}
	return p, ""
}

func (s *Server) saveProvider(w http.ResponseWriter, r *http.Request, req providerRequest, create bool) {
	ctx := r.Context()
	p, msg := s.checkProvider(ctx, req)
	if msg != "" {
		writeError(w, http.StatusBadRequest, msg)
		return
	}
	key := strings.TrimSpace(req.APIKey)
	if _, ok := s.internalSecretValue(ctx, providerKeyName(p.ID)); !ok && key == "" && !keyOptional(p.Kind) {
		writeError(w, http.StatusBadRequest, "api_key: the provider needs an API key")
		return
	}
	pr := principalFrom(ctx)
	p.UpdatedBy, p.UpdatedAt = pr.Name, time.Now().UTC()
	if create {
		if n, _ := s.docs.Count(ctx, kindProvider); n >= maxProviders {
			writeError(w, http.StatusConflict, fmt.Sprintf("there are already %d providers", maxProviders))
			return
		}
		if err := s.docs.Create(ctx, kindProvider, p.ID, p); err != nil {
			if errors.Is(err, docstore.ErrExists) {
				writeError(w, http.StatusConflict, fmt.Sprintf("there is already a provider %q", p.ID))
				return
			}
			s.internalError(w, "saving the provider", err)
			return
		}
	} else if err := s.docs.Put(ctx, kindProvider, p.ID, p); err != nil {
		s.internalError(w, "saving the provider", err)
		return
	}
	if key != "" {
		if _, _, err := s.putSecret(ctx, pr.Name, providerKeyName(p.ID), key, "API key of provider "+p.ID); err != nil {
			s.internalError(w, "saving the key", err)
			return
		}
	}
	on := 0
	for _, m := range p.Models {
		if m.On {
			on++
		}
	}
	detail := fmt.Sprintf("%s provider, %d models on", p.Kind, on)
	if p.MonthlyUSD > 0 {
		detail += fmt.Sprintf(", limit $%.2f a month", p.MonthlyUSD)
	}
	if p.Fallback != "" {
		detail += ", fallback " + p.Fallback
	}
	if p.AllowPrivate {
		detail += ", private network"
	}
	if p.Kind == provBedrock {
		acct := p.AWSAccount
		if acct == "" {
			acct = primaryAccount
		}
		detail += ", AWS account " + acct
		if p.Region != "" {
			detail += " in " + p.Region
		}
	}
	if key != "" {
		detail += ", key replaced"
	}
	action := "provider.update"
	status := http.StatusOK
	if create {
		action, status = "provider.create", http.StatusCreated
	}
	s.auditReq(r, action, "provider/"+p.ID, detail, true)
	writeJSON(w, status, s.providerView(ctx, p, s.recentModelCalls(ctx)))
}

func (s *Server) handleCreateProvider(w http.ResponseWriter, r *http.Request) {
	var req providerRequest
	if !readJSON(w, r, &req) {
		return
	}
	s.saveProvider(w, r, req, true)
}

func (s *Server) handleUpdateProvider(w http.ResponseWriter, r *http.Request) {
	var req providerRequest
	if !readJSON(w, r, &req) {
		return
	}
	old, err := s.loadProvider(r.Context(), r.PathValue("id"))
	if err != nil {
		s.defError(w, "provider", err)
		return
	}
	req.ID, req.Kind = old.ID, old.Kind // neither changes
	s.saveProvider(w, r, req, false)
}

func (s *Server) handleDeleteProvider(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id := r.PathValue("id")
	if _, err := s.loadProvider(ctx, id); err != nil {
		s.defError(w, "provider", err)
		return
	}
	if err := s.docs.Delete(ctx, kindProvider, id); err != nil {
		s.internalError(w, "removing the provider", err)
		return
	}
	s.docs.Delete(ctx, kindSecret, providerKeyName(id))
	s.auditReq(r, "provider.delete", "provider/"+id, "key removed from the vault", true)
	w.WriteHeader(http.StatusNoContent)
}

// handleTestProvider asks one model a tiny question, so the console can show the key and address work.
func (s *Server) handleTestProvider(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	p, err := s.loadProvider(ctx, r.PathValue("id"))
	if err != nil {
		s.defError(w, "provider", err)
		return
	}
	var req struct {
		Model string `json:"model"`
	}
	if r.ContentLength > 0 && !readJSON(w, r, &req) {
		return
	}
	model := req.Model
	if model == "" {
		for _, m := range p.Models {
			if m.On {
				model = m.ID
				break
			}
		}
	}
	if model == "" {
		writeError(w, http.StatusBadRequest, "turn on a model first")
		return
	}
	ctx = context.WithValue(ctx, callWhoKey{}, "Test from the providers page")
	reply, use, err := s.callProviderOnce(ctx, p.ID+"/"+model, "Reply with the single word OK.", []chatTurn{{Role: "user", Content: "Are you there?"}})
	s.auditReq(r, "provider.test", "provider/"+p.ID, model, err == nil)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"reply": truncate(reply, 200), "model": model, "millis": use.Millis, "cost_usd": use.CostUSD})
}

// checkProviderURL checks a provider's address. Anthropic's is a plain address; OpenAI-style addresses end in a path
// such as /v1 or /api/v1, which is kept.
func checkProviderURL(kind, raw string) error {
	if kind == provAnthropic {
		return checkBaseURL(raw)
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || strings.Contains(u.Path, "..") {
		return errors.New("the API address must be a plain address such as https://api.openai.com/v1")
	}
	return checkBaseURL(u.Scheme + "://" + u.Host)
}
