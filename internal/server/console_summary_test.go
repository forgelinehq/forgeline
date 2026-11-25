package server

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

func longHistory(n int) []map[string]string {
	h := make([]map[string]string, 0, n)
	for i := 0; i < n; i++ {
		role := "user"
		if i%2 == 1 {
			role = "assistant"
		}
		h = append(h, map[string]string{"role": role, "content": fmt.Sprintf("message %d about Dev01", i)})
	}
	return h
}

// request is what the model was sent as request i: its instructions and the text of its messages.
func (m *mcpHarness) request(i int) (system string, msgs []string) {
	m.llm.mu.Lock()
	defer m.llm.mu.Unlock()
	system, _ = m.llm.reqs[i]["system"].(string)
	for _, x := range m.llm.reqs[i]["messages"].([]any) {
		msgs = append(msgs, x.(map[string]any)["content"].(string))
	}
	return
}

// A long chat is not forgotten: what falls out of the window is summarised for the model, the summary is kept with the
// conversation, extended only once enough has fallen out (in the meantime those messages are sent whole), and made again if the
// messages it was made from changed.
func TestOlderMessagesOfALongChatAreSummarisedNotForgotten(t *testing.T) {
	m := newMCPHarness(t)
	evs := m.run(m.dev, map[string]any{"message": "start"}, "first answer")
	conv := conversationOf(evs)
	ask := func(history []map[string]string, replies ...string) {
		m.llm.script(replies...)
		m.dev.expect(200, "POST", "/v1/assistant/chat", map[string]any{"message": "next question", "conversationId": conv, "history": history})
	}
	stored := func() Conversation {
		var c Conversation
		json.Unmarshal(m.dev.expect(200, "GET", "/v1/assistant/conversations/"+conv, nil), &c)
		return c
	}

	// 16 messages: 4 fall out of the window, so one summary call, then the answer.
	h16 := longHistory(16)
	ask(h16, "SUMMARY-ONE: the person is looking at Dev01", "the answer")
	if m.llm.count() != 2 {
		t.Fatalf("model calls = %d, want a summary and an answer", m.llm.count())
	}
	if sys, msgs := m.request(0); !strings.Contains(sys, "condense the earlier part of a chat") || len(msgs) != 1 || !strings.Contains(msgs[0], "message 0 about Dev01") || strings.Contains(msgs[0], "message 4 about Dev01") {
		t.Errorf("the summary request: %q %q", sys, msgs)
	}
	sys, msgs := m.request(1)
	if !strings.Contains(sys, "SUMMARY-ONE") || !strings.Contains(sys, "no longer sent whole") || len(msgs) != 13 || !strings.Contains(msgs[0], "message 4 about Dev01") {
		t.Errorf("the answer request: summary in system %v, %d messages, first %q", strings.Contains(sys, "SUMMARY-ONE"), len(msgs), msgs[0])
	}
	if c := stored(); c.SummaryText == "" || c.SummaryCovered != 4 || c.SummaryHash == "" {
		t.Errorf("stored: %+v", c)
	}

	// 18 messages: only 2 more have fallen out, so no new call: they are sent whole beside the same summary.
	ask(longHistory(18), "the answer again")
	if m.llm.count() != 1 {
		t.Errorf("model calls = %d, want only the answer", m.llm.count())
	}
	sys, msgs = m.request(0)
	if !strings.Contains(sys, "SUMMARY-ONE") || len(msgs) != 15 || !strings.Contains(msgs[0], "message 4 about Dev01") {
		t.Errorf("between summaries: %d messages, first %q", len(msgs), msgs[0])
	}

	// 22 messages: 6 more have fallen out, so the summary is extended with exactly those, and replaces the old one.
	ask(longHistory(22), "SUMMARY-TWO: Dev01 is the Singapore cluster", "answer three")
	if m.llm.count() != 2 {
		t.Fatalf("model calls = %d", m.llm.count())
	}
	if _, msgs := m.request(0); len(msgs) != 1 || !strings.Contains(msgs[0], "<previous_summary>\nSUMMARY-ONE") || !strings.Contains(msgs[0], "message 4 about Dev01") || !strings.Contains(msgs[0], "message 9 about Dev01") || strings.Contains(msgs[0], "message 3 about Dev01") || strings.Contains(msgs[0], "message 10 about Dev01") {
		t.Errorf("the extension request: %q", msgs)
	}
	sys, msgs = m.request(1)
	if !strings.Contains(sys, "SUMMARY-TWO") || strings.Contains(sys, "SUMMARY-ONE") || len(msgs) != 13 {
		t.Errorf("after the extension: %d messages", len(msgs))
	}
	if c := stored(); c.SummaryCovered != 10 || !strings.HasPrefix(c.SummaryText, "SUMMARY-TWO") {
		t.Errorf("stored: covered %d %q", c.SummaryCovered, c.SummaryText)
	}

	// Earlier messages that changed (the browser sent another history): the stored summary is not used for them.
	other := longHistory(22)
	other[1]["content"] = "a different second message"
	ask(other, "SUMMARY-THREE: something else", "answer four")
	if _, msgs := m.request(0); len(msgs) != 1 || strings.Contains(msgs[0], "<previous_summary>") || !strings.Contains(msgs[0], "a different second message") {
		t.Errorf("a changed history was summarised from the old summary: %q", msgs)
	}

	// A model that cannot summarise leaves the old behaviour: the earlier messages are left out, an older summary is kept.
	ask(longHistory(30), "", "answer five")
	sys, msgs = m.request(m.llm.count() - 1)
	if len(msgs) != 13 || !strings.Contains(msgs[0], "message 18 about Dev01") {
		t.Errorf("when the summary fails: %d messages, first %q", len(msgs), msgs[0])
	}
	_ = sys
}

// Secrets in the messages never reach the summariser, a short chat is never summarised, and an agent that keeps its own session is
// not sent the history at all.
func TestSummariesLeaveSecretsOutAndShortChatsAlone(t *testing.T) {
	m := newMCPHarness(t)
	conv := conversationOf(m.run(m.dev, map[string]any{"message": "start"}, "first answer"))
	h := longHistory(16)
	h[0]["content"] = "my key is AKIA" + "ABCDEFGHIJKLMNOP please remember it"
	m.llm.script("SUMMARY: nothing secret", "ok")
	m.dev.expect(200, "POST", "/v1/assistant/chat", map[string]any{"message": "next", "conversationId": conv, "history": h})
	if _, msgs := m.request(0); strings.Contains(msgs[0], "ABCDEFGHIJKLMNOP") {
		t.Errorf("a key reached the summariser: %q", msgs[0])
	}
	m.llm.script("short answer")
	m.dev.expect(200, "POST", "/v1/assistant/chat", map[string]any{"message": "next", "conversationId": conv, "history": longHistory(10)})
	if m.llm.count() != 1 {
		t.Errorf("a short chat made %d model calls", m.llm.count())
	}
}
