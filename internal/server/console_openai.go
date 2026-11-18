package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// openAIChat asks a model through the OpenAI Chat Completions API, which OpenAI and most self-hosted servers
// (Ollama, vLLM, LiteLLM) and routers (OpenRouter, Groq) speak. base ends in the API's version, such as
// https://api.openai.com/v1. OpenAI's own API takes max_completion_tokens; compatible servers mostly know only
// max_tokens. An empty key sends no Authorization header, for a local server that needs none.
func openAIChat(ctx context.Context, client *http.Client, base, key, model, system string, turns []chatTurn, maxTokens int, openAI bool) (string, modelUsage, int, error) {
	msgs := make([]chatTurn, 0, len(turns)+1)
	if system != "" {
		msgs = append(msgs, chatTurn{Role: "system", Content: system})
	}
	msgs = append(msgs, turns...)
	body := map[string]any{"model": model, "messages": msgs}
	if openAI {
		body["max_completion_tokens"] = maxTokens
	} else {
		body["max_tokens"] = maxTokens
	}
	raw, _ := json.Marshal(body)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(base, "/")+"/chat/completions", bytes.NewReader(raw))
	if err != nil {
		return "", modelUsage{}, 0, err
	}
	req.Header.Set("content-type", "application/json")
	if key != "" {
		req.Header.Set("authorization", "Bearer "+key)
	}
	who := "the provider"
	if openAI {
		who = "the OpenAI API"
	}
	resp, err := client.Do(req)
	if err != nil {
		if strings.Contains(err.Error(), "refused") {
			return "", modelUsage{}, 0, fmt.Errorf("could not reach %s: %s", who, truncate(err.Error(), 200))
		}
		return "", modelUsage{}, 0, fmt.Errorf("could not reach %s", who)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode != http.StatusOK {
		// OpenAI sends {"error":{"message":…}}; some servers send {"error":"…"} or {"message":…}.
		var e struct {
			Error   json.RawMessage `json:"error"`
			Message string          `json:"message"`
		}
		json.Unmarshal(data, &e)
		msg := e.Message
		var inner struct{ Message string }
		if json.Unmarshal(e.Error, &inner) == nil && inner.Message != "" {
			msg = inner.Message
		} else if s := ""; json.Unmarshal(e.Error, &s) == nil && s != "" {
			msg = s
		}
		if msg == "" {
			msg = resp.Status
		}
		if resp.StatusCode == http.StatusNotFound && strings.Contains(strings.ToLower(msg), "model") {
			return "", modelUsage{}, resp.StatusCode, unknownModel(model)
		}
		return "", modelUsage{}, resp.StatusCode, fmt.Errorf("%s refused the request (%d): %s", who, resp.StatusCode, truncate(msg, 200))
	}
	var out struct {
		Choices []struct {
			Message struct {
				Content *string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
		Usage struct {
			Prompt     int64 `json:"prompt_tokens"`
			Completion int64 `json:"completion_tokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(data, &out); err != nil {
		return "", modelUsage{}, resp.StatusCode, fmt.Errorf("%s sent a reply that could not be read", who)
	}
	if len(out.Choices) == 0 || out.Choices[0].Message.Content == nil || strings.TrimSpace(*out.Choices[0].Message.Content) == "" {
		return "", modelUsage{}, resp.StatusCode, errors.New("the model sent an empty reply")
	}
	return *out.Choices[0].Message.Content, modelUsage{Calls: 1, Input: out.Usage.Prompt, Output: out.Usage.Completion}, resp.StatusCode, nil
}
