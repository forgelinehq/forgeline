package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/forgelinehq/forgeline/internal/authn"
)

// call_aws lets the assistant read the connected AWS account with the AWS CLI, the way the AWS
// MCP servers' call_aws tool does, but only for read operations and only through the credentials
// the console already holds. It is not an MCP client: the server parses one command, checks it
// against the rules below and runs it without a shell.
const (
	maxAWSCommand = 600
	awsCallTime   = 30 * time.Second
)

// awsReadOp is the only shape of operation allowed: describe-*, list-* and get-*.
var awsReadOp = regexp.MustCompile(`^(describe|list|get)-[a-z0-9-]+$`)

// awsNeverOp names operations that read secrets, credentials, tokens, code, data or logs even
// though they look like reads. Matched against "service operation".
var awsNeverOp = regexp.MustCompile(`(?i)(secret-value|password|credential|token|authorization|session|federation|decrypt|get-object|get-log-events|get-log-record|get-query-results|console-output|console-screenshot|get-function|get-layer|download|export|presign|generate|get-parameter|get-key-material|public-access-key|login)`)

// awsBlockedService are services the assistant never calls.
var awsBlockedService = map[string]bool{"configure": true, "sso": true, "sso-oidc": true, "secretsmanager": true, "kms": true, "s3": true}

// awsBlockedFlag are options that change where or as whom the call is made, read local files or
// fetch URLs, or reveal secrets.
var awsBlockedFlag = map[string]bool{
	"--profile": true, "--endpoint-url": true, "--cli-input-json": true, "--cli-input-yaml": true,
	"--generate-cli-skeleton": true, "--ca-bundle": true, "--no-verify-ssl": true, "--debug": true,
	"--no-sign-request": true, "--with-decryption": true, "--cli-read-timeout": true,
	"--cli-connect-timeout": true, "--output": true, "--color": true,
}

// tokenize splits a command line on spaces, honouring simple '...' and "..." quotes. There is no
// expansion of any kind.
func tokenize(s string) ([]string, error) {
	var out []string
	var cur strings.Builder
	var quote rune
	have := false
	for _, r := range s {
		switch {
		case quote != 0:
			if r == quote {
				quote = 0
			} else {
				cur.WriteRune(r)
			}
		case r == '\'' || r == '"':
			quote, have = r, true
		case r == ' ' || r == '\t':
			if have || cur.Len() > 0 {
				out = append(out, cur.String())
				cur.Reset()
				have = false
			}
		default:
			cur.WriteRune(r)
		}
	}
	if quote != 0 {
		return nil, errors.New("an opening quote is never closed")
	}
	if have || cur.Len() > 0 {
		out = append(out, cur.String())
	}
	return out, nil
}

// parseAWSCommand checks a command and returns the CLI arguments after "aws" (service,
// operation, options) and the region the person asked for, if any.
func parseAWSCommand(cmd string) (args []string, region string, err error) {
	args, region, change, err := parseAWS(cmd, false)
	_ = change
	return args, region, err
}

// parseAWSCall is parseAWSCommand for a call that may change something (design note 052). Everything else about the
// command is held to the same rules: one plain command, no pipes or substitution, no options that redirect it, read
// local files or fetch addresses, and no operation that returns a secret. change says it is not a describe, list or get.
func parseAWSCall(cmd string) (args []string, region string, change bool, err error) {
	return parseAWS(cmd, true)
}

// awsNeverChange names changes that return a secret to whoever made them, so the secret would land in the model's
// context. They are refused, not asked about.
var awsNeverChange = regexp.MustCompile(`(?i)(create-access-key|create-login-profile|update-login-profile|service-specific-credential|create-virtual-mfa|rotate-secret|get-credential|put-secret-value|create-secret)`)

func parseAWS(cmd string, allowChange bool) (args []string, region string, change bool, err error) {
	args, region, err = parseAWSChecked(cmd, allowChange)
	if err != nil {
		return nil, "", false, err
	}
	return args, region, len(args) >= 2 && !awsReadOp.MatchString(args[1]), nil
}

func parseAWSChecked(cmd string, allowChange bool) (args []string, region string, err error) {
	cmd = strings.TrimSpace(cmd)
	switch {
	case cmd == "":
		return nil, "", errors.New("give a cli_command such as: aws ecs list-clusters")
	case len(cmd) > maxAWSCommand:
		return nil, "", fmt.Errorf("the command is longer than %d characters", maxAWSCommand)
	case strings.ContainsAny(cmd, "\n\r\x00`$;|&<>"):
		return nil, "", errors.New("one plain AWS CLI command only: no pipes, redirects, substitution or several commands")
	}
	toks, err := tokenize(cmd)
	if err != nil {
		return nil, "", err
	}
	if len(toks) > 0 && toks[0] == "aws" {
		toks = toks[1:]
	}
	if len(toks) < 2 || strings.HasPrefix(toks[0], "-") || strings.HasPrefix(toks[1], "-") {
		return nil, "", errors.New("the command must be: aws <service> <operation> [options]")
	}
	service, op := toks[0], toks[1]
	switch {
	case awsBlockedService[service]:
		return nil, "", fmt.Errorf("the %s service is not available to the assistant", service)
	case service == "sts" && op != "get-caller-identity":
		return nil, "", errors.New("for sts only get-caller-identity is available")
	case !awsReadOp.MatchString(op) && !allowChange:
		return nil, "", fmt.Errorf("%s is not a read operation; only describe-*, list-* and get-* are allowed. To change something, propose a change in the console", op)
	case !awsReadOp.MatchString(op) && awsNeverChange.MatchString(service+" "+op):
		return nil, "", fmt.Errorf("%s %s returns a secret or a credential, which must not pass through the assistant; create it in the console or with a runbook", service, op)
	case awsNeverOp.MatchString(service + " " + op):
		return nil, "", fmt.Errorf("%s %s can return secrets, credentials, code or data and is not available", service, op)
	}
	rest := toks[2:]
	out := []string{service, op}
	for i := 0; i < len(rest); i++ {
		t := rest[i]
		name, value, hasValue := strings.Cut(t, "=")
		if strings.HasPrefix(name, "--") {
			if awsBlockedFlag[name] {
				return nil, "", fmt.Errorf("the option %s is not allowed", name)
			}
			if name == "--region" {
				if !hasValue {
					if i+1 >= len(rest) {
						return nil, "", errors.New("--region needs a value")
					}
					i++
					value = rest[i]
				}
				if !awsRegionRE.MatchString(value) {
					return nil, "", fmt.Errorf("%q is not a region name", value)
				}
				region = value
				continue
			}
			if hasValue && badAWSValue(value) {
				return nil, "", errors.New("option values cannot read local files or fetch addresses")
			}
			out = append(out, t)
			continue
		}
		if badAWSValue(t) {
			return nil, "", errors.New("option values cannot read local files or fetch addresses")
		}
		out = append(out, t)
	}
	return out, region, nil
}

func badAWSValue(v string) bool {
	l := strings.ToLower(v)
	for _, p := range []string{"file://", "fileb://", "http://", "https://", "s3://", "env:", "ssm:", "secretsmanager:"} {
		if strings.Contains(l, p) {
			return true
		}
	}
	return false
}

func (s *Server) lookupCallAWS(ctx context.Context, raw json.RawMessage) (string, error) {
	var a struct {
		Command string `json:"cli_command"`
		Account string `json:"account"`
	}
	if err := decodeArgs(raw, &a); err != nil {
		return "", err
	}
	args, region, change, err := parseAWSCall(a.Command)
	if err != nil {
		return "", err
	}
	st, err := s.awsFor(ctx, a.Account)
	if err != nil {
		return "", err
	}
	if !st.Enabled {
		return "", errors.New("AWS is not connected to this Forgeline server")
	}
	if change {
		if err := s.permitChange(ctx, a.Account, args); err != nil {
			return "", err
		}
	}
	if region != "" {
		st.Region = region
	}
	ctx, cancel := context.WithTimeout(ctx, awsCallTime)
	defer cancel()
	out, err := s.aws(ctx, st, args...)
	if err != nil {
		return "", err
	}
	text := strings.TrimSpace(string(out))
	if text == "" {
		return "The call succeeded and returned nothing.", nil
	}
	if len(text) > maxLookupResult {
		text = text[:maxLookupResult] + "\n[output cut; narrow it with --query or --max-items]"
	}
	return text, nil
}

// lookupDetail is what the audit log records for a lookup: the tool, and for call_aws the command.
func lookupDetail(c lookupCall) string {
	if c.Tool != "call_aws" {
		return c.Tool
	}
	var a struct {
		Command string `json:"cli_command"`
		Account string `json:"account"`
	}
	_ = json.Unmarshal(c.Args, &a)
	if a.Account != "" {
		return "call_aws [" + truncate(a.Account, 40) + "]: " + truncate(strings.TrimSpace(a.Command), 200)
	}
	return "call_aws: " + truncate(strings.TrimSpace(a.Command), 200)
}

// isCLIChange says whether a call_aws call would change something (the command is not a describe, list or get).
func isCLIChange(c lookupCall) bool {
	if c.Tool != "call_aws" {
		return false
	}
	var a struct {
		Command string `json:"cli_command"`
	}
	if json.Unmarshal(c.Args, &a) != nil {
		return false
	}
	_, _, change, err := parseAWSCall(a.Command)
	return err == nil && change
}

// permitChange decides whether the assistant may make this change as the person who is chatting (design note 052).
// Everything it refuses here is said plainly, so the person can fix the cause or use the proposal route.
func (s *Server) permitChange(ctx context.Context, account string, args []string) error {
	p := principalFrom(ctx)
	command := "aws " + strings.Join(args, " ")
	if account == "" {
		account = primaryAccount
	}
	refuse := func(msg string) error {
		_ = s.audit(ctx, p, "", "assistant.cli.change", "aws/"+account, "refused: "+truncate(command, 300)+": "+msg, false)
		return errors.New(msg)
	}
	switch {
	case scopeFrom(ctx) != nil:
		return refuse("an agent cannot change anything this way; propose the change (propose_aws_change) and a person approves it")
	case !p.Role.AtLeast(authn.Admin):
		return refuse("changing AWS needs an administrator; propose the change (propose_aws_change) for one to approve")
	case !s.ownApproval(ctx, account):
		return refuse("this account is protected: a change to it is a proposal (propose_aws_change) that a different administrator approves. An administrator can mark it \"allow with my own approval\" under Settings › Assistant permissions")
	}
	d, err := s.loadPermissions(ctx)
	if err != nil {
		return refuse("the permission rules could not be read, so nothing is changed until they can")
	}
	dec := d.decide(p, permToolAWS, command)
	switch dec.Effect {
	case permDeny:
		return refuse(dec.Why)
	case permAllow:
		_ = s.audit(ctx, p, "", "assistant.cli.change", "aws/"+account, "allowed by rule: "+truncate(command, 300), true)
		return nil
	}
	ask := permAskerFrom(ctx)
	if ask == nil {
		return refuse("this change needs the person's approval and nobody is here to give it; ask in the chat, or propose it")
	}
	choice, err := ask(permRequest{Tool: "call_aws", Command: command, Account: account, Why: dec.Why, Pattern: alwaysPattern(command)})
	if err != nil || choice == decisionDenied {
		msg := "you said no, so it did not run"
		if err != nil {
			msg = err.Error()
		}
		return refuse(msg)
	}
	if choice == decisionAlways {
		if pat := alwaysPattern(command); pat != "" {
			if err := s.addAlwaysRule(ctx, p, pat); err != nil {
				return refuse("it was not run, because the rule could not be saved: " + err.Error())
			}
		}
	}
	_ = s.audit(ctx, p, "", "assistant.cli.change", "aws/"+account, map[string]string{decisionOnce: "approved once", decisionAlways: "approved, and always allowed for this kind"}[choice]+": "+truncate(command, 300), true)
	return nil
}

// addAlwaysRule records "always allow this kind" as the person's own rule, which they can see and remove.
func (s *Server) addAlwaysRule(ctx context.Context, p Principal, pattern string) error {
	permMu.Lock()
	defer permMu.Unlock()
	d, err := s.loadPermissions(ctx)
	if err != nil {
		return err
	}
	n := 0
	for _, x := range d.Rules {
		if x.Scope == scopePerson && x.Owner == p.UserID {
			if x.Pattern == pattern && x.Effect == permAllow {
				return nil
			}
			n++
		}
	}
	if n >= maxPermRules {
		return fmt.Errorf("you already have %d rules", maxPermRules)
	}
	r := PermRule{ID: authn.NewID(), Scope: scopePerson, Owner: p.UserID, OwnerName: p.Name, Tool: permToolAWS, Pattern: pattern, Effect: permAllow, Note: "from \"always allow\" in the chat", By: p.Name, At: time.Now().UTC()}
	if msg := ruleProblem(r); msg != "" {
		return errors.New(msg)
	}
	d.Rules = append(d.Rules, r)
	return s.docs.Put(ctx, kindSettings, idPermissions, d)
}
