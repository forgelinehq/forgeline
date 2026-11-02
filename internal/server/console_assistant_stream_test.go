package server

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"
)

// frames reads the data: lines of a server-sent event body as JSON objects.
func frames(t *testing.T, body []byte) []map[string]any {
	t.Helper()
	var out []map[string]any
	for _, block := range strings.Split(string(body), "\n\n") {
		for _, ln := range strings.Split(block, "\n") {
			if !strings.HasPrefix(ln, "data: ") {
				continue
			}
			var m map[string]any
			if err := json.Unmarshal([]byte(strings.TrimPrefix(ln, "data: ")), &m); err != nil {
				t.Fatalf("bad frame %q: %v", ln, err)
			}
			out = append(out, m)
		}
	}
	return out
}

func types(fs []map[string]any) []string {
	var t []string
	for _, f := range fs {
		s, _ := f["type"].(string)
		if s == "CUSTOM" {
			s += ":" + f["name"].(string)
		}
		t = append(t, s)
	}
	return t
}

func TestAssistantRunStreamsTheStepsThenTheAnswer(t *testing.T) {
	llm := newScriptedLLM(t, "", "The build step fails because make is missing.")
	_, _, dev, runID := assistantHarness(t, llm, "make: not found")
	llm.mu.Lock()
	llm.replies[0] = lookupReply("get_step_log", map[string]string{"run_id": runID, "step_id": "build"})
	llm.mu.Unlock()

	resp, body := dev.do("POST", "/v1/assistant/runs", map[string]any{"message": "why did build fail?"})
	if resp.StatusCode != 200 || !strings.HasPrefix(resp.Header.Get("Content-Type"), "text/event-stream") {
		t.Fatalf("status %d, type %q: %s", resp.StatusCode, resp.Header.Get("Content-Type"), body)
	}
	fs := frames(t, body)
	want := []string{"RUN_STARTED", "CUSTOM:forgeline.context", "TOOL_CALL_START", "TOOL_CALL_ARGS", "TOOL_CALL_END", "TOOL_CALL_RESULT", "CUSTOM:forgeline.tool_status",
		"TEXT_MESSAGE_START", "TEXT_MESSAGE_CONTENT", "TEXT_MESSAGE_END", "RUN_FINISHED"}
	if got := types(fs); strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("events = %v\nwant     %v", got, want)
	}
	byType := map[string]map[string]any{}
	for _, f := range fs {
		byType[f["type"].(string)] = f
	}
	if byType["TOOL_CALL_START"]["toolCallName"] != "get_step_log" {
		t.Errorf("tool call = %v", byType["TOOL_CALL_START"])
	}
	id := byType["TOOL_CALL_START"]["toolCallId"]
	for _, ty := range []string{"TOOL_CALL_ARGS", "TOOL_CALL_END", "TOOL_CALL_RESULT"} {
		if byType[ty]["toolCallId"] != id {
			t.Errorf("%s belongs to another call: %v", ty, byType[ty])
		}
	}
	if !strings.Contains(byType["TOOL_CALL_ARGS"]["delta"].(string), runID) {
		t.Errorf("args = %v", byType["TOOL_CALL_ARGS"])
	}
	if !strings.Contains(byType["TOOL_CALL_RESULT"]["content"].(string), "make: not found") {
		t.Errorf("the step shows no result: %v", byType["TOOL_CALL_RESULT"])
	}
	if got := byType["TEXT_MESSAGE_CONTENT"]["delta"]; got != "The build step fails because make is missing." {
		t.Errorf("answer = %v", got)
	}
	if byType["TEXT_MESSAGE_CONTENT"]["messageId"] != byType["TEXT_MESSAGE_START"]["messageId"] {
		t.Error("the answer's pieces carry different message ids")
	}
	if byType["RUN_FINISHED"]["runId"] != byType["RUN_STARTED"]["runId"] {
		t.Error("RUN_FINISHED is for another run")
	}
}

func TestAssistantRunWithoutLookupsHasNoToolEvents(t *testing.T) {
	llm := newScriptedLLM(t, "Nothing needs attention.")
	_, _, dev, _ := assistantHarness(t, llm, "x")
	_, body := dev.do("POST", "/v1/assistant/runs", map[string]any{"message": "status?"})
	got := strings.Join(types(frames(t, body)), ",")
	if got != "RUN_STARTED,CUSTOM:forgeline.context,TEXT_MESSAGE_START,TEXT_MESSAGE_CONTENT,TEXT_MESSAGE_END,RUN_FINISHED" {
		t.Errorf("events = %s", got)
	}
}

// The stream shows what a person's own role allows and nothing more: an audit lookup asked for by a
// developer is shown as failed, with no audit data in any event.
func TestAssistantRunNeverShowsWhatTheRoleMayNotRead(t *testing.T) {
	llm := newScriptedLLM(t, lookupReply("search_audit", nil), "done")
	_, admin, dev, _ := assistantHarness(t, llm, "x")
	admin.expect(200, "GET", "/v1/audit", nil) // make sure audit entries exist
	_, body := dev.do("POST", "/v1/assistant/runs", map[string]any{"message": "show the audit log"})
	fs := frames(t, body)
	var res map[string]any
	for _, f := range fs {
		if f["type"] == "TOOL_CALL_RESULT" {
			res = f
		}
	}
	if res == nil || !strings.Contains(res["content"].(string), "not available to your role") {
		t.Fatalf("the refusal is not shown: %v", res)
	}
	if strings.Contains(string(body), "assistant.chat") || strings.Contains(string(body), "user.create") {
		t.Errorf("audit data reached a developer's stream:\n%s", body)
	}
	for _, f := range fs {
		if f["type"] == "CUSTOM" && f["name"] == "forgeline.tool_status" && f["value"].(map[string]any)["ok"] != false {
			t.Errorf("a refused lookup is not marked failed: %v", f)
		}
	}
}

// No event carries a credential or the instructions given to the model.
func TestAssistantRunLeaksNeitherTheKeyNorThePrompt(t *testing.T) {
	llm := newScriptedLLM(t, lookupReply("list_pipelines", nil), "done")
	_, _, dev, _ := assistantHarness(t, llm, "x")
	_, body := dev.do("POST", "/v1/assistant/runs", map[string]any{"message": "what pipelines exist?"})
	for _, banned := range []string{assistKey, "You are the assistant built into Forgeline", "untrusted_context"} {
		if strings.Contains(string(body), banned) {
			t.Errorf("the stream contains %q", banned)
		}
	}
}

func TestAssistantRunReportsAFailedModelAsRunError(t *testing.T) {
	llm := newScriptedLLM(t, "x")
	_, _, dev, _ := assistantHarness(t, llm, "x")
	llm.srv.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Error(w, "overloaded", 500) })
	_, chat := dev.do("POST", "/v1/assistant/chat", map[string]any{"message": "hi"})
	var e struct{ Error string }
	json.Unmarshal(chat, &e)
	_, body := dev.do("POST", "/v1/assistant/runs", map[string]any{"message": "hi"})
	fs := frames(t, body)
	last := fs[len(fs)-1]
	if last["type"] != "RUN_ERROR" || e.Error == "" || last["message"] != e.Error {
		t.Errorf("last event = %v, the JSON endpoint says %q", last, e.Error)
	}
	for _, f := range fs {
		if f["type"] == "RUN_FINISHED" || f["type"] == "TEXT_MESSAGE_START" {
			t.Errorf("a failed run also sent %v", f["type"])
		}
	}
}

// Checks that fail before the model is asked are ordinary JSON errors, the same as for /chat.
func TestAssistantRunRefusesLikeChatDoes(t *testing.T) {
	llm := newScriptedLLM(t, "x")
	h, admin, dev, _ := assistantHarness(t, llm, "x")
	for _, c := range []struct {
		who  *browser
		body map[string]any
		want int
	}{
		{dev, map[string]any{"message": "  "}, 400},
		{dev, map[string]any{"message": strings.Repeat("a", maxAssistMessage+1)}, 400},
		{dev, map[string]any{"message": "hi", "context": map[string]any{"kind": "run", "id": "nope"}}, 404},
	} {
		resp, _ := c.who.do("POST", "/v1/assistant/runs", c.body)
		if resp.StatusCode != c.want || strings.HasPrefix(resp.Header.Get("Content-Type"), "text/event-stream") {
			t.Errorf("%v: status %d %s, want %d", c.body["message"], resp.StatusCode, resp.Header.Get("Content-Type"), c.want)
		}
	}
	admin.createUser("view", "viewer")
	if resp, _ := h.login("view").do("POST", "/v1/assistant/runs", map[string]any{"message": "hi"}); resp.StatusCode != 403 {
		t.Errorf("a viewer got %d", resp.StatusCode)
	}
	admin.expect(200, "PUT", "/v1/integrations/assistant", map[string]any{"backend": "off"})
	if resp, _ := dev.do("POST", "/v1/assistant/runs", map[string]any{"message": "hi"}); resp.StatusCode != 409 {
		t.Errorf("an assistant that is off gave %d", resp.StatusCode)
	}
}

// The stream is audited exactly like /chat: one entry per lookup and one for the question.
func TestAssistantRunIsAuditedLikeChat(t *testing.T) {
	llm := newScriptedLLM(t, lookupReply("list_pipelines", nil), "done")
	_, admin, dev, _ := assistantHarness(t, llm, "x")
	dev.do("POST", "/v1/assistant/runs", map[string]any{"message": "what pipelines exist?"})
	aud := string(admin.expect(200, "GET", "/v1/audit", nil))
	if strings.Count(aud, "assistant.lookup") != 1 || strings.Count(aud, "assistant.chat") != 1 {
		t.Errorf("audit = %s", aud)
	}
}

// Closing the stream cancels the model call, and the question is still audited, as a failure.
func TestAssistantRunStopsWhenTheClientGoes(t *testing.T) {
	llm := newScriptedLLM(t, "slow")
	llm.block = make(chan struct{})
	h, admin, dev, _ := assistantHarness(t, llm, "x")
	// Registered after the harness, so it runs first: the model must be released before the test
	// server can shut down, or a handler that never saw the cancel would hang the suite.
	t.Cleanup(func() { close(llm.block) })
	_ = dev
	ctx, cancel := context.WithTimeout(context.Background(), 400*time.Millisecond)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, "POST", h.http.URL+"/v1/assistant/runs", strings.NewReader(`{"message":"hi"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(csrfHeader, "1")
	if resp, err := dev.c.Do(req); err == nil {
		resp.Body.Close()
	}
	var aud string
	for i := 0; i < 50; i++ {
		aud = string(admin.expect(200, "GET", "/v1/audit", nil))
		if strings.Contains(aud, "assistant.chat") {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if !strings.Contains(aud, "assistant.chat") {
		t.Errorf("the abandoned question was never finished or audited: %s", aud)
	}
}

// A step card shows a cut of the result, never all of a large one.
func TestAssistantRunCutsLargeLookupResultsToAPreview(t *testing.T) {
	llm := newScriptedLLM(t, "", "done")
	_, _, dev, runID := assistantHarness(t, llm, strings.Repeat("line of log output ", 400))
	llm.mu.Lock()
	llm.replies[0] = lookupReply("get_step_log", map[string]string{"run_id": runID, "step_id": "build"})
	llm.mu.Unlock()
	_, body := dev.do("POST", "/v1/assistant/runs", map[string]any{"message": "show the log"})
	for _, f := range frames(t, body) {
		if f["type"] == "TOOL_CALL_RESULT" {
			if n := len(f["content"].(string)); n == 0 || n > previewMax+16 {
				t.Errorf("preview is %d bytes, want at most about %d", n, previewMax)
			}
			return
		}
	}
	t.Fatal("no result event")
}

// When a lookup files a request for an environment, the stream says so, with its id, so the console
// can show the request as a card. The assistant still only asks; an administrator approves.
func TestAssistantRunAnnouncesAnEnvironmentRequest(t *testing.T) {
	c := envHarness(t)
	llm := newScriptedLLM(t, lookupReply("request_environment", map[string]any{"template": "qa-sandbox", "name": "Queue check", "purpose": "Trying the queue consumer"}), "I asked for it.")
	c.admin.expect(200, "PUT", "/v1/integrations/assistant", map[string]any{"backend": "api", "api_key": assistKey, "api_base": llm.srv.URL})
	_, body := c.dev.do("POST", "/v1/assistant/runs", map[string]any{"message": "make me a queue test environment"})
	fs := frames(t, body)
	e := c.envByName("Queue check")
	var seen int
	for _, f := range fs {
		if f["type"] == "CUSTOM" && f["name"] == "forgeline.environment" {
			seen++
			if f["value"].(map[string]any)["id"] != e.ID {
				t.Errorf("announced %v, the request is %s", f["value"], e.ID)
			}
		}
	}
	if seen != 1 {
		t.Fatalf("%d environment events in %v", seen, types(fs))
	}
	if e.Status != envRequested {
		t.Errorf("the stream changed the environment: %+v", e)
	}
}
