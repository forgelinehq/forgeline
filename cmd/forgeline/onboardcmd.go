package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/forgelinehq/forgeline/internal/server"
)

const onboardHelp = `usage: forgeline onboard [--server URL] [--non-interactive] [--json]

Shows what is set up on a Forgeline server and what is left (design note 038), and asks the questions a terminal can
answer: which assistant to use (the claude program on the server, or none) and which AWS profile and region to use.
Everything else opens in the console, which also takes API keys and tokens (never typed into a terminal).
Each answer goes through the same checks as the console. Run it again at any time; it never undoes anything.

  --non-interactive  print the steps and exit (exit code 0 when everything is done or skipped, 3 otherwise)
  --json             print the steps as JSON

Environment:
  FORGELINE_TOKEN   an administrator's API token, or the server token (required)
  FORGELINE_SERVER  server URL, if --server is not given (default http://127.0.0.1:8080)
`

type onboardAPI struct {
	base, token string
	client      *http.Client
}

func (a onboardAPI) call(ctx context.Context, method, path string, body any, out any) error {
	var r io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		r = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, strings.TrimRight(a.base, "/")+path, r)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+a.token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	res, err := a.client.Do(req)
	if err != nil {
		return fmt.Errorf("could not reach %s: %w", a.base, err)
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if res.StatusCode >= 300 {
		var e struct{ Error string }
		if json.Unmarshal(raw, &e) == nil && e.Error != "" {
			return fmt.Errorf("%s", e.Error)
		}
		return fmt.Errorf("%s %s: %s", method, path, res.Status)
	}
	if out != nil {
		return json.Unmarshal(raw, out)
	}
	return nil
}

// runOnboard lists the setup steps and, when interactive, asks the questions a terminal can answer.
func runOnboard(ctx context.Context, args []string, stdin io.Reader, stdout, stderr io.Writer, getenv func(string) string) int {
	fs := flag.NewFlagSet("onboard", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = func() { fmt.Fprint(stderr, onboardHelp) }
	def := getenv("FORGELINE_SERVER")
	if def == "" {
		def = "http://127.0.0.1:8080"
	}
	serverURL := fs.String("server", def, "server URL")
	nonInteractive := fs.Bool("non-interactive", false, "print the steps and exit")
	asJSON := fs.Bool("json", false, "print the steps as JSON")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	token := getenv("FORGELINE_TOKEN")
	if token == "" {
		fmt.Fprintln(stderr, "forgeline: set FORGELINE_TOKEN to an administrator's API token or the server token")
		return 2
	}
	api := onboardAPI{base: *serverURL, token: token, client: &http.Client{Timeout: 60 * time.Second}}
	var o server.OnboardingJSON
	if err := api.call(ctx, "GET", "/v1/onboarding", nil, &o); err != nil {
		fmt.Fprintf(stderr, "forgeline: %v\n", err)
		return 1
	}
	if *asJSON {
		b, _ := json.MarshalIndent(o, "", "  ")
		fmt.Fprintln(stdout, string(b))
		return finishedCode(o)
	}
	printSteps(stdout, o, *serverURL)
	if *nonInteractive || o.Finished {
		return finishedCode(o)
	}

	in := bufio.NewReader(stdin)
	ask := func(q string) string {
		fmt.Fprint(stdout, q)
		line, err := in.ReadString('\n')
		if err != nil && line == "" {
			return ""
		}
		return strings.TrimSpace(line)
	}
	for _, s := range o.Steps {
		if s.Done || s.Skipped {
			continue
		}
		fmt.Fprintf(stdout, "\n%s\n  %s\n", s.Title, s.Why)
		var err error
		switch s.ID {
		case "assistant":
			err = askAssistant(ctx, api, ask, stdout, *serverURL+"/"+s.Link)
		case "cloud":
			err = askAWS(ctx, api, ask, stdout)
		default:
			fmt.Fprintf(stdout, "  Do it in the console: %s/%s\n", strings.TrimRight(*serverURL, "/"), s.Link)
			if strings.EqualFold(ask("  Skip it for now? [y/N] "), "y") {
				err = api.call(ctx, "PUT", "/v1/onboarding/"+s.ID, map[string]bool{"skip": true}, nil)
				if err == nil {
					fmt.Fprintln(stdout, "  Skipped. It stays on the list in Settings › Setup.")
				}
			}
		}
		if err != nil {
			fmt.Fprintf(stdout, "  Not done: %v\n", err)
		}
	}
	if err := api.call(ctx, "GET", "/v1/onboarding", nil, &o); err == nil {
		fmt.Fprintln(stdout)
		printSteps(stdout, o, *serverURL)
	}
	return finishedCode(o)
}

func finishedCode(o server.OnboardingJSON) int {
	if o.Finished {
		return 0
	}
	return 3
}

func printSteps(w io.Writer, o server.OnboardingJSON, base string) {
	fmt.Fprintf(w, "Forgeline setup: %d of %d done\n", o.Done, len(o.Steps))
	for _, s := range o.Steps {
		mark, note := "[ ]", ""
		switch {
		case s.Done:
			mark, note = "[x]", " ("+s.Detail+")"
		case s.Skipped:
			mark, note = "[-]", " (skipped)"
		}
		fmt.Fprintf(w, "  %s %s%s\n", mark, s.Title, note)
	}
}

// askAssistant offers the choices that need no secret: the claude program on the server, or none for now. A key or a
// token is entered in the console, where it is stored write-only.
func askAssistant(ctx context.Context, api onboardAPI, ask func(string) string, out io.Writer, consoleURL string) error {
	fmt.Fprintln(out, "  1) the claude program on this server, using its login (good for one person)")
	fmt.Fprintln(out, "  2) an Anthropic API key or a Claude Code token: open the console to paste it")
	fmt.Fprintln(out, "  3) not now")
	switch ask("  Choose [1/2/3]: ") {
	case "1":
		if err := api.call(ctx, "PUT", "/v1/integrations/assistant", map[string]any{"backend": "cli"}, nil); err != nil {
			return err
		}
		fmt.Fprintln(out, "  The assistant now uses the claude program on the server.")
	case "2":
		fmt.Fprintln(out, "  Open "+consoleURL+" and paste the key or token there.")
	}
	return nil
}

// askAWS connects AWS with a profile the server's user already has (it never asks for keys).
func askAWS(ctx context.Context, api onboardAPI, ask func(string) string, out io.Writer) error {
	fmt.Fprintln(out, "  Forgeline uses an AWS login the server's user already has (a profile in ~/.aws/config). It never stores AWS keys.")
	profile := ask("  AWS profile (empty for the default, or 'skip'): ")
	if strings.EqualFold(profile, "skip") {
		return api.call(ctx, "PUT", "/v1/onboarding/cloud", map[string]bool{"skip": true}, nil)
	}
	region := ask("  Region (for example eu-west-1): ")
	if region == "" {
		return fmt.Errorf("a region is needed")
	}
	var res struct {
		Error    string `json:"error"`
		Identity *struct {
			Account string `json:"account"`
		} `json:"identity"`
	}
	if err := api.call(ctx, "PUT", "/v1/integrations/aws", map[string]any{"enabled": true, "profile": profile, "region": region}, &res); err != nil {
		return err
	}
	if res.Identity != nil {
		fmt.Fprintf(out, "  Connected to AWS account %s in %s.\n", res.Identity.Account, region)
	}
	return nil
}
