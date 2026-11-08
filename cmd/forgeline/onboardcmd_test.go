package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/forgelinehq/forgeline/internal/server"
)

// fakeOnboardServer answers like a server with GitHub, AWS and the assistant left to set up.
type fakeOnboardServer struct {
	mu    sync.Mutex
	calls []string
	done  map[string]bool
	skip  map[string]bool
}

func (f *fakeOnboardServer) handler(t *testing.T) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer tok-123" {
			http.Error(w, `{"error":"sign in"}`, http.StatusUnauthorized)
			return
		}
		body, _ := io.ReadAll(r.Body)
		f.mu.Lock()
		defer f.mu.Unlock()
		f.calls = append(f.calls, r.Method+" "+r.URL.Path+" "+strings.TrimSpace(string(body)))
		switch {
		case r.Method == "PUT" && strings.HasPrefix(r.URL.Path, "/v1/onboarding/"):
			f.skip[strings.TrimPrefix(r.URL.Path, "/v1/onboarding/")] = true
		case r.Method == "PUT" && r.URL.Path == "/v1/integrations/assistant":
			f.done["assistant"] = true
		case r.Method == "PUT" && r.URL.Path == "/v1/integrations/aws":
			f.done["cloud"] = true
			w.Write([]byte(`{"enabled":true,"identity":{"account":"111122223333"}}`))
			return
		}
		o := server.OnboardingJSON{}
		for _, id := range []string{"admin", "github", "cloud", "assistant"} {
			s := server.OnboardingStep{ID: id, Title: "Step " + id, Why: "why " + id, Link: "#/" + id, Done: id == "admin" || f.done[id], Skipped: f.skip[id]}
			if s.Done {
				o.Done++
			}
			o.Steps = append(o.Steps, s)
		}
		o.Finished = true
		for _, s := range o.Steps {
			o.Finished = o.Finished && (s.Done || s.Skipped)
		}
		json.NewEncoder(w).Encode(o)
	})
}

func TestOnboardAsksAndApplies(t *testing.T) {
	f := &fakeOnboardServer{done: map[string]bool{}, skip: map[string]bool{}}
	srv := httptest.NewServer(f.handler(t))
	defer srv.Close()
	env := map[string]string{"FORGELINE_TOKEN": "tok-123", "FORGELINE_SERVER": srv.URL}
	getenv := func(k string) string { return env[k] }

	// Non-interactive: the list, and exit code 3 because something is left.
	var out, errb bytes.Buffer
	if code := runOnboard(context.Background(), []string{"--non-interactive"}, strings.NewReader(""), &out, &errb, getenv); code != 3 {
		t.Fatalf("code = %d, err = %s", code, errb.String())
	}
	if !strings.Contains(out.String(), "Forgeline setup: 1 of 4 done") || !strings.Contains(out.String(), "[x] Step admin") || !strings.Contains(out.String(), "[ ] Step github") {
		t.Errorf("list:\n%s", out.String())
	}

	// Interactive: skip GitHub, connect AWS with a profile and region, use the claude program.
	out.Reset()
	answers := "y\ndev\neu-west-1\n1\n"
	if code := runOnboard(context.Background(), nil, strings.NewReader(answers), &out, &errb, getenv); code != 0 {
		t.Fatalf("code = %d\n%s\n%s", code, out.String(), errb.String())
	}
	f.mu.Lock()
	calls := strings.Join(f.calls, "\n")
	f.mu.Unlock()
	for _, want := range []string{`PUT /v1/onboarding/github {"skip":true}`, `PUT /v1/integrations/aws {"enabled":true,"profile":"dev","region":"eu-west-1"}`, `PUT /v1/integrations/assistant {"backend":"cli"}`} {
		if !strings.Contains(calls, want) {
			t.Errorf("missing call %s in:\n%s", want, calls)
		}
	}
	if !strings.Contains(out.String(), "Connected to AWS account 111122223333 in eu-west-1") || !strings.Contains(out.String(), "4 of 4 done") && !strings.Contains(out.String(), "3 of 4 done") {
		t.Errorf("output:\n%s", out.String())
	}
	if strings.Contains(calls, "api_key") || strings.Contains(calls, "claude_token") {
		t.Errorf("the terminal must never send a key or token:\n%s", calls)
	}

	// JSON, a missing token, and a wrong one.
	out.Reset()
	if code := runOnboard(context.Background(), []string{"--json"}, strings.NewReader(""), &out, &errb, getenv); code != 0 || !strings.Contains(out.String(), `"steps"`) {
		t.Errorf("json: %d %s", code, out.String())
	}
	if code := runOnboard(context.Background(), nil, strings.NewReader(""), &out, &errb, func(string) string { return "" }); code != 2 {
		t.Errorf("no token: %d", code)
	}
	errb.Reset()
	env["FORGELINE_TOKEN"] = "wrong"
	if code := runOnboard(context.Background(), nil, strings.NewReader(""), &out, &errb, getenv); code != 1 || !strings.Contains(errb.String(), "sign in") {
		t.Errorf("wrong token: %d %s", code, errb.String())
	}
}
