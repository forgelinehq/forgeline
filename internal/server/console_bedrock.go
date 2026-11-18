package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

// bedrockConverse asks a model on AWS Bedrock through the Converse API, with the AWS CLI and the credentials of an
// AWS account the console is connected to, as every other AWS call does. A model is a model id or an inference
// profile, for example anthropic.claude-haiku-5-5-v1:0 or us.anthropic.claude-haiku-5-5-v1:0. This calls a model; it
// changes nothing in the account, so it does not go through a proposal (note 006). The account's role needs
// bedrock:InvokeModel.
//
// The messages and the system prompt hold log text, so they go in private files (file://) rather than on the
// command line, where other users of the machine could read them and where a long prompt would be too big.
func (s *Server) bedrockConverse(ctx context.Context, p Provider, model, system string, turns []chatTurn, maxTokens int) (string, modelUsage, int, error) {
	st, err := s.awsFor(ctx, p.AWSAccount)
	if err != nil {
		return "", modelUsage{}, 0, fmt.Errorf("bedrock: %v", err)
	}
	if !st.Enabled {
		return "", modelUsage{}, 0, errors.New("bedrock: the console is not connected to that AWS account")
	}
	if p.Region != "" {
		st.Region = p.Region
	}
	type block struct {
		Text string `json:"text"`
	}
	type message struct {
		Role    string  `json:"role"`
		Content []block `json:"content"`
	}
	msgs := make([]message, 0, len(turns))
	for _, t := range turns {
		msgs = append(msgs, message{Role: t.Role, Content: []block{{Text: t.Content}}})
	}
	dir, err := os.MkdirTemp("", "forgeline-bedrock-")
	if err != nil {
		return "", modelUsage{}, 0, err
	}
	defer os.RemoveAll(dir)
	write := func(name string, v any) (string, error) {
		raw, _ := json.Marshal(v)
		f := filepath.Join(dir, name)
		return "file://" + f, os.WriteFile(f, raw, 0o600)
	}
	mf, err := write("messages.json", msgs)
	if err != nil {
		return "", modelUsage{}, 0, err
	}
	cf, err := write("config.json", map[string]int{"maxTokens": maxTokens})
	if err != nil {
		return "", modelUsage{}, 0, err
	}
	args := []string{"bedrock-runtime", "converse", "--model-id", model, "--messages", mf, "--inference-config", cf}
	if system != "" {
		sf, err := write("system.json", []block{{Text: system}})
		if err != nil {
			return "", modelUsage{}, 0, err
		}
		args = append(args, "--system", sf)
	}
	out, err := s.aws(ctx, st, args...)
	if err != nil {
		return "", modelUsage{}, bedrockStatus(err.Error()), fmt.Errorf("bedrock: %v", err)
	}
	var r struct {
		Output struct {
			Message struct {
				Content []struct {
					Text *string `json:"text"`
				} `json:"content"`
			} `json:"message"`
		} `json:"output"`
		Usage struct {
			Input  int64 `json:"inputTokens"`
			Output int64 `json:"outputTokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(out, &r); err != nil {
		return "", modelUsage{}, http.StatusOK, errors.New("bedrock sent a reply that could not be read")
	}
	var sb strings.Builder
	for _, c := range r.Output.Message.Content {
		if c.Text != nil {
			sb.WriteString(*c.Text)
		}
	}
	if strings.TrimSpace(sb.String()) == "" {
		return "", modelUsage{}, http.StatusOK, errors.New("the model sent an empty reply")
	}
	return sb.String(), modelUsage{Calls: 1, Input: r.Usage.Input, Output: r.Usage.Output}, http.StatusOK, nil
}

// bedrockStatus maps a Bedrock error to the HTTP status it stands for, so a provider's fallback is used only when the
// failure may pass (throttled, overloaded, not ready, unreachable), never when access is denied or the model is wrong.
func bedrockStatus(msg string) int {
	switch {
	case strings.Contains(msg, "ThrottlingException"), strings.Contains(msg, "TooManyRequests"):
		return http.StatusTooManyRequests
	case strings.Contains(msg, "ServiceUnavailable"), strings.Contains(msg, "InternalServer"), strings.Contains(msg, "ModelNotReady"), strings.Contains(msg, "ModelTimeout"):
		return http.StatusServiceUnavailable
	case strings.Contains(msg, "Could not connect"), strings.Contains(msg, "Connect timeout"):
		return 0
	}
	return http.StatusBadRequest
}
