package server

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// askCopilot asks GitHub's copilot program (`copilot -p`) for one reply, with the server user's own copilot login.
// It is run like the claude backend: an empty scratch directory, a clean environment, the prompt on stdin (it holds
// log text, which must not show in the process list), and nothing it could act with: no tools, no MCP servers, no
// custom instructions, no updates. The program has no system-prompt option, so the instructions lead the prompt.
// model is the setting as typed; empty lets Copilot choose (its "auto").
func askCopilot(ctx context.Context, model, system string, turns []chatTurn) (string, modelUsage, error) {
	dir, err := os.MkdirTemp("", "forgeline-copilot-")
	if err != nil {
		return "", modelUsage{}, err
	}
	defer os.RemoveAll(dir)
	var prompt strings.Builder
	prompt.WriteString("Follow these instructions for the whole conversation:\n<instructions>\n")
	prompt.WriteString(system)
	prompt.WriteString("\n</instructions>\n\n")
	for i, t := range turns {
		if i < len(turns)-1 {
			who := "Person"
			if t.Role == "assistant" {
				who = "Assistant"
			}
			fmt.Fprintf(&prompt, "%s: %s\n\n", who, t.Content)
		}
	}
	if len(turns) > 1 {
		prompt.WriteString("Now the person asks:\n")
	}
	prompt.WriteString(turns[len(turns)-1].Content)

	usageFile := filepath.Join(dir, "usage.json")
	args := []string{"--output-format", "json", "--available-tools", "--disable-builtin-mcps", "--no-custom-instructions",
		"--no-auto-update", "--no-ask-user", "--disable-client-telemetry", "--usage-output-file", usageFile}
	if model != "" && !strings.HasPrefix(model, "claude-") && model != "sonnet" && model != "opus" && model != "haiku" {
		args = append(args, "--model", model)
	}
	cmd := exec.CommandContext(ctx, "copilot", args...)
	cmd.Dir = dir
	// The login is found through HOME; nothing else of the server's (its token, master key, cloud
	// credentials, a GITHUB_TOKEN that would replace the login) is passed on.
	cmd.Env = []string{"HOME=" + os.Getenv("HOME"), "PATH=" + os.Getenv("PATH"), "LANG=C.UTF-8", "COPILOT_AUTO_UPDATE=false"}
	cmd.Stdin = strings.NewReader(prompt.String())
	var out, errb limitedBuffer
	out.max, errb.max = 4<<20, 1<<14
	cmd.Stdout, cmd.Stderr = &out, &errb
	if err := cmd.Run(); err != nil {
		var nf *exec.Error
		if errors.As(err, &nf) {
			return "", modelUsage{}, errors.New("the copilot program is not installed on the server")
		}
		if ctx.Err() != nil {
			return "", modelUsage{}, errors.New("the assistant took too long")
		}
		return "", modelUsage{}, fmt.Errorf("copilot failed: %s", truncate(firstLine(strings.TrimSpace(errb.b.String()+" "+lastLine(out.b.String()))), 240))
	}
	reply, failed := readCopilotOutput(out.b.Bytes())
	use := readCopilotUsage(usageFile)
	if failed != "" {
		return "", use, errors.New("copilot failed: " + truncate(failed, 240))
	}
	if reply == "" {
		return "", use, errors.New("the assistant sent an empty reply")
	}
	return reply, use, nil
}

// readCopilotOutput reads the JSON lines of `copilot --output-format json`: the last final message is the reply;
// an error event or a non-zero exit code in the result is a failure.
func readCopilotOutput(raw []byte) (reply, failed string) {
	sc := bufio.NewScanner(strings.NewReader(string(raw)))
	sc.Buffer(make([]byte, 0, 64<<10), 4<<20)
	for sc.Scan() {
		var ev struct {
			Type     string `json:"type"`
			ExitCode *int   `json:"exitCode"`
			Data     struct {
				Content string `json:"content"`
				Message string `json:"message"`
			} `json:"data"`
		}
		if json.Unmarshal(sc.Bytes(), &ev) != nil {
			continue
		}
		switch {
		case ev.Type == "assistant.message" && strings.TrimSpace(ev.Data.Content) != "":
			reply = strings.TrimSpace(ev.Data.Content)
		case strings.HasSuffix(ev.Type, ".error") || ev.Type == "error":
			failed = firstLine(ev.Data.Message)
		case ev.Type == "result" && ev.ExitCode != nil && *ev.ExitCode != 0 && failed == "":
			failed = fmt.Sprintf("exit code %d", *ev.ExitCode)
		}
	}
	return reply, failed
}

// readCopilotUsage reads the usage file: tokens in and out. Copilot counts premium requests, not dollars, so the
// cost stays unknown.
func readCopilotUsage(path string) modelUsage {
	use := modelUsage{Calls: 1}
	raw, err := os.ReadFile(path)
	var u struct {
		TokenDetails map[string]struct {
			TokenCount int64 `json:"tokenCount"`
		} `json:"tokenDetails"`
	}
	if err != nil || json.Unmarshal(raw, &u) != nil || len(u.TokenDetails) == 0 {
		use.Unknown = true
		return use
	}
	use.Input = u.TokenDetails["input"].TokenCount + u.TokenDetails["cache_read"].TokenCount + u.TokenDetails["cache_write"].TokenCount
	use.Output = u.TokenDetails["output"].TokenCount
	return use
}

func lastLine(s string) string {
	s = strings.TrimSpace(s)
	return s[strings.LastIndex(s, "\n")+1:]
}
