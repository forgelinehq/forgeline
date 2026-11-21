package server

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSchemaFromHelpReadsTheExampleArguments(t *testing.T) {
	schema, desc := schemaFromHelp(`{"pipeline":"optional name","limit":"optional, up to 30","verbose":true,"ids":["a"],"timeout_seconds":120} recent runs`)
	props := schema["properties"].(map[string]any)
	typeOf := func(k string) any { return props[k].(map[string]any)["type"] }
	if desc != "recent runs" || typeOf("pipeline") != "string" || typeOf("limit") != "integer" || typeOf("verbose") != "boolean" || typeOf("ids") != "array" || typeOf("timeout_seconds") != "integer" {
		t.Errorf("schema: %v / %q", schema, desc)
	}
	if _, has := schema["required"]; has {
		t.Errorf("an optional argument is required: %v", schema["required"])
	}
	if s, d := schemaFromHelp("no arguments, just does it"); len(s["properties"].(map[string]any)) != 0 || d != "no arguments, just does it" {
		t.Errorf("a help line with no example: %v %q", s, d)
	}
	// Braces and quotes inside a string value do not end the example early.
	s, d := schemaFromHelp(`{"code":"x = {'a': \"}\"}\nreturn x","account":"optional"} runs code`)
	if len(s["properties"].(map[string]any)) != 2 || d != "runs code" {
		t.Errorf("nested braces: %v %q", s, d)
	}
}

func TestAnEscapedQuoteInAnExampleDoesNotEndItEarly(t *testing.T) {
	s, d := schemaFromHelp(`{"a":"x\"}y","b":"z"} desc`)
	if len(s["properties"].(map[string]any)) != 2 || d != "desc" {
		t.Errorf("%v %q", s, d)
	}
}

func TestNativeNamesAndBlocks(t *testing.T) {
	set := buildNativeSet([]helpLine{{"get_run", `{"id":"run id"} one run`}, {"mcp:server.tool", `{} a tool whose name providers would refuse`}, {"get_run", "dup"}})
	if len(set.tools) != 1 || set.tools[0].wire != "get_run" {
		t.Fatalf("tools: %+v", set.tools)
	}
	if got := set.asBlock("get_run", json.RawMessage(`{"id":"r1"}`)); got != "```forgeline-lookup\n{\"args\":{\"id\":\"r1\"},\"tool\":\"get_run\"}\n```" {
		t.Errorf("block: %s", got)
	}
	if c, ok := parseLookup(set.asBlock("get_run", json.RawMessage(`not json`))); !ok || c.Tool != "get_run" || string(c.Args) != "{}" {
		t.Errorf("bad arguments are not an empty call: %v %v", c, ok)
	}
	if c, ok := parseLookup(set.asBlock("made_up", nil)); !ok || c.Tool != "made_up" {
		t.Errorf("an unknown name: %v %v", c, ok)
	}
}

func TestAnAnthropicToolCallRunsTheToolAndTheToolsAreSent(t *testing.T) {
	m := newMCPHarness(t)
	m.run(m.dev, map[string]any{"message": "what runs?"}, "TOOL:list_pipelines:{}", "Here they are.")
	m.llm.mu.Lock()
	req := m.llm.reqs[0]
	m.llm.mu.Unlock()
	tools, _ := req["tools"].([]any)
	var found map[string]any
	for _, x := range tools {
		if tm := x.(map[string]any); tm["name"] == "get_run" {
			found = tm
		}
	}
	if found == nil {
		t.Fatalf("get_run is not among the %d tools sent", len(tools))
	}
	props := found["input_schema"].(map[string]any)["properties"].(map[string]any)
	if props["id"] == nil || found["description"] == "" {
		t.Errorf("the tool definition is thin: %v", found)
	}
	if tc, _ := req["tool_choice"].(map[string]any); tc["type"] != "auto" {
		t.Errorf("tool_choice: %v", req["tool_choice"])
	}
	// The call ran: the second request carries its result, as text, and the history stays text.
	if m.llm.count() != 2 || !strings.Contains(m.llm.lastUser(1), "Result of lookup list_pipelines") {
		t.Errorf("the call did not run: %d requests", m.llm.count())
	}
	m.llm.mu.Lock()
	second := m.llm.reqs[1]["messages"].([]any)
	m.llm.mu.Unlock()
	for _, x := range second {
		if _, isString := x.(map[string]any)["content"].(string); !isString {
			t.Errorf("the history is not plain text: %v", x)
		}
	}
}

func TestAnOpenAICompatibleToolCallBecomesABlockAndToolsAreSent(t *testing.T) {
	var sent map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		json.Unmarshal(b, &sent)
		json.NewEncoder(w).Encode(map[string]any{"choices": []map[string]any{{"message": map[string]any{"content": "I will check.", "tool_calls": []map[string]any{
			{"id": "c1", "type": "function", "function": map[string]any{"name": "get_run", "arguments": `{"id":"r7"}`}},
			{"id": "c2", "type": "function", "function": map[string]any{"name": "list_pipelines", "arguments": `{}`}}}}}}, "usage": map[string]int{"prompt_tokens": 9, "completion_tokens": 3}})
	}))
	defer srv.Close()
	set := buildNativeSet([]helpLine{{"get_run", `{"id":"run id"} one run`}, {"list_pipelines", `{} pipelines`}})
	ctx := context.WithValue(context.Background(), nativeToolsKey{}, set)
	reply, use, _, err := openAIChat(ctx, srv.Client(), srv.URL, "k", "m", "sys", []chatTurn{{Role: "user", Content: "hi"}}, 100, openAIStyle{maxParam: "max_tokens", who: "x"})
	if err != nil {
		t.Fatal(err)
	}
	if c, ok := parseLookup(reply); !ok || c.Tool != "get_run" || !strings.Contains(string(c.Args), "r7") || use.Output != 3 {
		t.Errorf("reply: %q", reply)
	}
	tools, _ := sent["tools"].([]any)
	if len(tools) != 2 || sent["tool_choice"] != "auto" || tools[0].(map[string]any)["type"] != "function" {
		t.Errorf("sent: %v", sent)
	}
	// Without tools in the context nothing is sent, and a plain reply is a plain reply.
	sent = nil
	srv2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		json.Unmarshal(b, &sent)
		json.NewEncoder(w).Encode(map[string]any{"choices": []map[string]any{{"message": map[string]any{"content": "plain"}}}})
	}))
	defer srv2.Close()
	if r, _, _, err := openAIChat(context.Background(), srv2.Client(), srv2.URL, "k", "m", "", []chatTurn{{Role: "user", Content: "hi"}}, 100, openAIStyle{maxParam: "max_tokens", who: "x"}); err != nil || r != "plain" || sent["tools"] != nil {
		t.Errorf("plain: %q %v %v", r, err, sent)
	}
}

func TestArgumentsAreReadLooselyWhenAModelSendsTheWrongType(t *testing.T) {
	var a struct {
		Limit int    `json:"limit"`
		Name  string `json:"name"`
	}
	if err := decodeArgs(json.RawMessage(`{"limit":"30","name":"x"}`), &a); err != nil || a.Limit != 30 || a.Name != "x" {
		t.Errorf("string for number: %+v %v", a, err)
	}
	a.Limit, a.Name = 0, ""
	if err := decodeArgs(json.RawMessage(`{"limit":5,"name":12}`), &a); err != nil || a.Limit != 5 || a.Name != "12" {
		t.Errorf("number for string: %+v %v", a, err)
	}
	if err := decodeArgs(json.RawMessage(`{"limit":"lots"}`), &a); err == nil {
		t.Error("nonsense was accepted")
	}
	if err := decodeArgs(json.RawMessage(`not json`), &a); err == nil {
		t.Error("not JSON was accepted")
	}
}

func TestSchemasCarryChoicesRequirementsAndNestedShapes(t *testing.T) {
	s, _ := schemaFromHelp(`{"action":"save_pipeline|run_pipeline","title":"short label","yaml":"for save_pipeline: the file","spec":"for save_skill {\"name\"}"} prepares a card`)
	props := s["properties"].(map[string]any)
	act := props["action"].(map[string]any)
	if enum, _ := act["enum"].([]string); len(enum) != 2 || enum[0] != "save_pipeline" {
		t.Errorf("action is not a choice: %v", act)
	}
	req, _ := s["required"].([]string)
	if len(req) != 2 || req[0] != "action" || req[1] != "title" {
		t.Errorf("required: %v", req)
	}
	if props["spec"].(map[string]any)["type"] != "object" || props["yaml"].(map[string]any)["type"] != "string" {
		t.Errorf("spec/yaml types: %v %v", props["spec"], props["yaml"])
	}
	plan, _ := schemaFromHelp(`{"items":[{"text":"short step","status":"todo|doing|done"}]} a checklist`)
	items := plan["properties"].(map[string]any)["items"].(map[string]any)
	obj := items["items"].(map[string]any)
	if items["type"] != "array" || obj["type"] != "object" || obj["properties"].(map[string]any)["status"].(map[string]any)["enum"] == nil {
		t.Errorf("a list of objects is not described: %v", items)
	}
}
