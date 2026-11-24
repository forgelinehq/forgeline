package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
)

// forgeline_api (design note 052). The assistant, and any agent running through it, can do in Forgeline what the person can do in
// the console: read anything, and change agents, skills, teams, workflows, pipelines and memory (update, version, archive,
// delete), by calling the console's own API as that person. The same handlers run, with the same role checks, validation and
// audit entries, so there is one set of rules and not one tool per object. A change asks first, like every other change, unless
// autopilot is on. What it can never call is the part that decides who may do what: users, tokens, secrets, sign-in, the
// permission and sandbox and autopilot settings, the model settings, approving or applying anything, and starting runs.

const forgelineAPITool = "forgeline_api"

// forgelineWritable are the parts of the API the assistant may change.
var forgelineWritable = []string{"/v1/ai/agents", "/v1/ai/skills", "/v1/ai/teams", "/v1/ai/workflows", "/v1/ai/team-schedules", "/v1/ai/evals", "/v1/pipelines", "/v1/assistant/memory"}

// forgelineNever is refused for every method: the paths that decide who may do what, approve or apply something, or start work.
var forgelineNever = regexp.MustCompile(`(?i)^/v1/(auth|users|tokens|secrets|me|settings/sso|assistant/(permissions|sandbox|autopilot|approvals|actions|questions)|integrations/assistant|providers|onboarding|market)\b|/(approve|reject|apply|destroy|confirm|dismiss|login|logout|setup|run|runs|start|push|trigger|execute|chat|gate|publish|budget)(/|$)`)

func (s *Server) lookupForgelineAPI(ctx context.Context, raw json.RawMessage) (string, error) {
	t := memTurnFrom(ctx)
	if t == nil || scopeFrom(ctx) != nil {
		return "", errors.New("forgeline_api is only available when a person is chatting with the assistant")
	}
	var a struct {
		Method string          `json:"method"`
		Path   string          `json:"path"`
		Body   json.RawMessage `json:"body"`
		Offset int             `json:"offset"` // for a GET whose answer was cut: where to read on from
	}
	if err := decodeArgs(raw, &a); err != nil {
		return "", err
	}
	method := strings.ToUpper(strings.TrimSpace(a.Method))
	path := strings.TrimSpace(a.Path)
	if !strings.HasPrefix(path, "/v1/") || strings.ContainsAny(path, " \t\r\n#\\") || strings.Contains(path, "..") || strings.Contains(path, "//") {
		return "", errors.New("path must be an API path such as /v1/ai/agents")
	}
	bare := path
	if i := strings.IndexByte(bare, '?'); i >= 0 {
		bare = bare[:i]
	}
	if forgelineNever.MatchString(bare) {
		return "", fmt.Errorf("%s is not something the assistant may call: it decides who may do what, approves, applies or starts work. Use the console, or a card or proposal where one exists", bare)
	}
	switch method {
	case "GET":
	case "POST", "PUT", "PATCH", "DELETE":
		ok := false
		for _, pre := range forgelineWritable {
			ok = ok || bare == pre || strings.HasPrefix(bare, pre+"/")
		}
		if !ok {
			return "", fmt.Errorf("the assistant can change agents, skills, teams, workflows, pipelines and memory (%s); %s is not one of them", strings.Join(forgelineWritable, ", "), bare)
		}
		if err := s.gateForgelineWrite(ctx, t.p, method, path, a.Body); err != nil {
			return "", err
		}
	default:
		return "", errors.New("method is GET, POST, PUT, PATCH or DELETE")
	}
	s.inprocOnce.Do(func() { s.inproc = s.Handler() })
	var body *bytes.Reader
	if len(a.Body) > 0 && string(bytes.TrimSpace(a.Body)) != "null" {
		body = bytes.NewReader(a.Body)
	} else {
		body = bytes.NewReader(nil)
	}
	req := httptest.NewRequest(method, path, body).WithContext(context.WithValue(ctx, delegatedKey{}, t.p))
	req.RemoteAddr = "127.0.0.1:1"
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(csrfHeader, "1") // an in-process call by the server itself, not a browser
	rec := httptest.NewRecorder()
	s.inproc.ServeHTTP(rec, req)
	_ = s.audit(ctx, t.p, "", "assistant.api", method+" "+bare, fmt.Sprintf("status %d", rec.Code), rec.Code < 400)
	out := pageAnswer(strings.TrimSpace(rec.Body.String()), a.Offset, maxLookupResult-256)
	if method != "GET" || rec.Code >= 300 {
		out = fmt.Sprintf("%s %s: %d %s\n%s", method, bare, rec.Code, http.StatusText(rec.Code), out)
	}
	if mt := memTurnFrom(ctx); mt != nil && !strings.HasPrefix(bare, "/v1/assistant/memory") {
		mt.dirty = true // what it read may carry text the person did not type
	}
	return out, nil
}

// pageAnswer returns one window of a long answer, and says where the next one starts, so the assistant can read a long report in
// pieces instead of seeing only its first 12 KB. Counted in characters, so a window never ends inside one.
func pageAnswer(full string, offset, window int) string {
	r := []rune(full)
	if offset < 0 || offset > len(r) {
		offset = 0
	}
	end := min(offset+window, len(r))
	out := string(r[offset:end])
	if offset > 0 {
		out = fmt.Sprintf("[characters %d to %d of %d]\n", offset, end, len(r)) + out
	}
	if end < len(r) {
		out += fmt.Sprintf("\n[… cut here: %d more characters. Call again with \"offset\": %d to read on]", len(r)-end, end)
	}
	return out
}

// gateForgelineWrite is the one question every change asks: does autopilot approve it, else the person.
func (s *Server) gateForgelineWrite(ctx context.Context, p Principal, method, path string, body json.RawMessage) error {
	detail := method + " " + path
	if b := bytes.TrimSpace(body); len(b) > 0 && string(b) != "null" {
		detail += "  " + truncate(string(b), 280)
	}
	if s.autopilotApproves(ctx, p, gateRequest{Kind: gateForgelineWrite, Tool: forgelineAPITool, Detail: detail}) {
		return nil
	}
	ask := permAskerFrom(ctx)
	if ask == nil {
		return errors.New("this change needs the person's approval and nobody is here to give it")
	}
	choice, err := ask(permRequest{Tool: forgelineAPITool, Command: detail, Why: "the assistant wants to change this in Forgeline, as you. Autopilot makes these without asking"})
	if err != nil || choice == decisionDenied {
		_ = s.audit(ctx, p, "", "assistant.api", method+" "+path, "refused", false)
		if err != nil {
			return err
		}
		return errors.New("you said no, so it did not run")
	}
	return nil
}
