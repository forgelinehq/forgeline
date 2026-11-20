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
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

// askCopilot asks GitHub's copilot program for one reply, with the server user's own copilot login. It is run like the
// claude backend: an empty scratch directory, a clean environment (but for proxy and CA settings), the prompt on stdin
// where the CLI reads it, and nothing it could act with: no tools, no MCP servers, no custom instructions, no updates. The program has no system-prompt option, so the instructions lead the prompt.
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
	help := copilotHelp(ctx)
	// --available-tools takes an allow list: naming a tool that does not exist leaves the model none. Given no value
	// at all, or --excluded-tools '*', the CLI (checked with 1.0.107) still offers bash, apply_patch, web_fetch and more.
	args := []string{"--output-format", "json", "--available-tools", noCopilotTool, "--disable-builtin-mcps", "--no-custom-instructions",
		"--no-auto-update", "--no-ask-user"}
	// Older CLIs (1.0.34) reject flags they do not know, so these are sent only when --help lists them.
	if strings.Contains(help, "--disable-client-telemetry") {
		args = append(args, "--disable-client-telemetry")
	}
	if strings.Contains(help, "--usage-output-file") {
		args = append(args, "--usage-output-file", usageFile)
	}
	if model != "" && !strings.HasPrefix(model, "claude-") && model != "sonnet" && model != "opus" && model != "haiku" {
		args = append(args, "--model", model)
	}
	// The prompt holds log text, which should not show in the process list, so it goes on stdin when the CLI reads a
	// piped prompt. An older CLI that does not waits for a person instead, so there it is passed with --prompt.
	viaStdin := strings.Contains(help, "piped stdin")
	if !viaStdin {
		args = append(args, "--prompt", prompt.String())
	}
	cmd := exec.CommandContext(ctx, "copilot", args...)
	cmd.Dir = dir
	cmd.Env = copilotEnv()
	if viaStdin {
		cmd.Stdin = strings.NewReader(prompt.String())
	}
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
	if n := copilotToolCount(out.b.Bytes()); n > 0 {
		// A reply written by a model that could run commands is not trusted, whatever it says.
		return "", modelUsage{}, fmt.Errorf("copilot offered the model %d tools although Forgeline asked for none; refusing its reply (check the copilot CLI version)", n)
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

// noCopilotTool is a tool name that does not exist, so the --available-tools allow list leaves the model none.
const noCopilotTool = "forgeline-no-tools"

// copilotEnvKeep is what a corporate network needs to reach GitHub: proxies and extra certificate authorities. Nothing
// else of the server's environment (its token, master key, cloud credentials, a GITHUB_TOKEN that would replace the
// login) is passed on.
var copilotEnvKeep = []string{"HTTPS_PROXY", "HTTP_PROXY", "ALL_PROXY", "NO_PROXY", "NODE_EXTRA_CA_CERTS", "NODE_USE_SYSTEM_CA", "SSL_CERT_FILE", "SSL_CERT_DIR"}

func copilotEnv() []string {
	env := []string{"HOME=" + os.Getenv("HOME"), "PATH=" + os.Getenv("PATH"), "LANG=C.UTF-8", "COPILOT_AUTO_UPDATE=false"}
	for _, k := range copilotEnvKeep {
		for _, name := range []string{k, strings.ToLower(k)} {
			if v, ok := os.LookupEnv(name); ok && v != "" {
				env = append(env, name+"="+v)
			}
		}
	}
	return env
}

var (
	copilotHelpMu   sync.Mutex
	copilotHelpText string
	copilotHelpAt   time.Time
	copilotHelpPath string
)

// copilotHelp is `copilot --help`, read at most every ten minutes (or when the program found on PATH changes), to tell
// which flags the installed CLI takes.
func copilotHelp(ctx context.Context) string {
	copilotHelpMu.Lock()
	defer copilotHelpMu.Unlock()
	path, _ := exec.LookPath("copilot")
	if copilotHelpText != "" && path == copilotHelpPath && time.Since(copilotHelpAt) < 10*time.Minute {
		return copilotHelpText
	}
	copilotHelpPath = path
	hctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	cmd := exec.CommandContext(hctx, "copilot", "--help")
	cmd.Env = copilotEnv()
	out, _ := cmd.CombinedOutput()
	copilotHelpText, copilotHelpAt = string(out), time.Now()
	return copilotHelpText
}

var copilotToolCountRE = regexp.MustCompile(`"tool_count":\s*(\d+)`)

// copilotToolCount is the most tools any model call in the output was offered, as the CLI reports it (0 when it does
// not say).
func copilotToolCount(raw []byte) int {
	most := 0
	for _, m := range copilotToolCountRE.FindAllSubmatch(raw, -1) {
		if n, err := strconv.Atoi(string(m[1])); err == nil && n > most {
			most = n
		}
	}
	return most
}
