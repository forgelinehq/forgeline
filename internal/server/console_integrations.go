package server

import (
	"context"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/forgelinehq/forgeline/internal/authn"
)

// The integrations hub (design note 025). One list of every system Forgeline connects to: what is
// connected, what needs attention, what was found on this server, and what is coming. Status is read
// from stored settings without a network call, except the Docker host (a local command). A test is a
// separate, explicit, read-only request.

const (
	intConnected = "connected"
	intAttention = "attention"
	intDetected  = "detected"
	intOff       = "off"
	intNext      = "next"
	intLater     = "later"

	intSSOWarn = 30 * time.Minute // the AWS sign-in is flagged when it ends within this
)

// IntegrationJSON is one system in the hub.
type IntegrationJSON struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Category string `json:"category"` // Source and CI, Cloud, AI infrastructure, Containers, AI
	Tier     string `json:"tier"`     // built-in, verified or community
	Status   string `json:"status"`   // connected, attention, detected, off, next or later
	// Summary is one plain sentence about the state. Problem says what is wrong and what to do.
	// Warnings are things that do not stop it working.
	Summary  string         `json:"summary"`
	Blurb    string         `json:"blurb"` // what the system is for, one short sentence
	Problem  string         `json:"problem,omitempty"`
	Warnings []string       `json:"warnings,omitempty"`
	Details  []AWSFieldJSON `json:"details,omitempty"`
	// ReadOnlyBy says who enforces "read-only" for this system: "provider" (a role or permission set on
	// its side) or "forgeline" (the provider cannot limit a key, so Forgeline only calls read endpoints).
	ReadOnlyBy   string               `json:"read_only_by,omitempty"`
	ReadOnlyNote string               `json:"read_only_note,omitempty"`
	Testable     bool                 `json:"testable"`
	LastTest     *IntegrationTestJSON `json:"last_test,omitempty"`
	Manage       string               `json:"manage,omitempty"` // console path of the existing settings
	Order        int                  `json:"order,omitempty"`  // build order for planned systems
	Plan         string               `json:"plan,omitempty"`   // what a planned system will do
	Auth         string               `json:"auth,omitempty"`   // how a planned system will sign in
	Found        string               `json:"found,omitempty"`  // what was seen on this server (names only)
}

// IntegrationCheckJSON is one line of a test.
type IntegrationCheckJSON struct {
	Name   string `json:"name"`
	Result string `json:"result"` // ok, warn or fail
	Detail string `json:"detail,omitempty"`
	Fix    string `json:"fix,omitempty"`
	TookMS int64  `json:"took_ms"`
}

// IntegrationTestJSON is the outcome of a test. A test only reads, and says so.
type IntegrationTestJSON struct {
	ID      string                 `json:"id"`
	At      string                 `json:"at"`
	By      string                 `json:"by,omitempty"`
	TookMS  int64                  `json:"took_ms"`
	Passed  int                    `json:"passed"`
	Warned  int                    `json:"warned"`
	Failed  int                    `json:"failed"`
	Checks  []IntegrationCheckJSON `json:"checks"`
	Changed bool                   `json:"changed"` // always false: no test creates or changes anything
}

// IntegrationsJSON is the hub.
type IntegrationsJSON struct {
	Integrations []IntegrationJSON `json:"integrations"`
}

type integTestStore struct {
	mu sync.Mutex
	m  map[string]IntegrationTestJSON
}

func (t *integTestStore) put(r IntegrationTestJSON) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.m == nil {
		t.m = map[string]IntegrationTestJSON{}
	}
	t.m[r.ID] = r
}

func (t *integTestStore) get(id string) *IntegrationTestJSON {
	t.mu.Lock()
	defer t.mu.Unlock()
	if r, ok := t.m[id]; ok {
		return &r
	}
	return nil
}

// plannedIntegrations are the systems on the roadmap (note 025). They have no Connect button: a button
// that does nothing is worse than a label that says "later".
func plannedIntegrations() []IntegrationJSON {
	p := func(id, name, cat, tier, status string, order int, plan, auth, roBy, roNote string) IntegrationJSON {
		return IntegrationJSON{ID: id, Name: name, Category: cat, Tier: tier, Status: status, Order: order, Plan: plan, Auth: auth, Blurb: firstSentence(plan),
			ReadOnlyBy: roBy, ReadOnlyNote: roNote, Summary: map[string]string{intNext: "Coming soon, next", intLater: "Coming soon, later"}[status]}
	}
	return []IntegrationJSON{
		p("azure", "Microsoft Azure", "Cloud", "built-in", intNext, 2,
			"Read-only explorer, map, Guard and assistant through one Resource Graph query over the subscriptions the identity can read, paged past 1,000 rows. AI Foundry deployments appear under an AI group.",
			"Device code sign-in; a service principal with a certificate later.", "provider", "Azure, through the Reader role"),
		p("cloud-models", "Bedrock, Vertex AI and Foundry", "AI infrastructure", "built-in", intNext, 3,
			"Models, endpoints and agents of each cloud appear in its explorer, and the same identity serves the models to Forgeline agents with no extra key.",
			"The cloud connection. No new key.", "provider", "The cloud, through the same role as the connection"),
		p("model-endpoint", "Custom model endpoint", "AI infrastructure", "built-in", intNext, 3,
			"Any OpenAI-compatible address with a key in the vault and a model name, with a per-agent budget. Covers gateways and local servers.",
			"An API key in the vault, write-only.", "", ""),
		p("lambda", "Lambda", "AI infrastructure", "verified", intNext, 4,
			"Read-only first: instances, instance types and availability, filesystems. GPU launches later, only as proposals with a cost cap and an automatic destroy.",
			"An API key, write-only; an expiry is recommended.", "forgeline", "Forgeline only. Lambda's documentation shows no read-only keys, so Forgeline calls only an allow-listed set of read endpoints"),
		p("nebius", "Nebius", "AI infrastructure", "verified", intNext, 4,
			"Read-only first through the REST API with a service account: instances, GPU clusters, Managed Kubernetes. Spend may start as a usage export.",
			"A service account token from the nebius command line tool.", "provider", "Nebius, if a viewer role exists (not confirmed)"),
		p("coreweave", "CoreWeave", "AI infrastructure", "built-in", intNext, 4,
			"A preset on the Kubernetes integration plus an IAM Viewer role. No new adapter.",
			"Kubernetes access with a read-only role.", "provider", "CoreWeave, through the IAM Viewer role"),
		p("runpod", "RunPod", "AI infrastructure", "verified", intNext, 4,
			"REST API v2 only. REST v1 retires on 2026-11-15. Read-only first: pods, serverless endpoints, usage.",
			"An API key, write-only.", "forgeline", "Unknown. The overview page lists no key scopes, so Forgeline enforces read-only itself"),
		p("more-ai-infra", "More GPU and inference platforms", "AI infrastructure", "community", intLater, 10,
			"Crusoe, Vast.ai, Fluidstack, Modal, Baseten, Replicate, Together, Fireworks and Groq, as community catalog items or through the custom model endpoint. Not researched yet.",
			"Depends on the provider.", "", ""),
		p("registries", "Container registries", "Containers", "built-in", intNext, 8,
			"ECR, Artifact Registry, ACR and Docker Hub: image lists and scan results in Guard, and push credentials scoped to steps.",
			"A cloud role or a registry token.", "", ""),
		p("gitlab", "GitLab and Bitbucket", "Source and CI", "built-in", intLater, 9,
			"The same shape as GitHub: an allow list, a signed webhook, statuses posted back.",
			"A project or group access token, write-only.", "", ""),
		p("datadog", "Datadog", "Observe and notify", "verified", intNext, 6,
			"After the clouds and AI infrastructure. Monitors and incidents as evidence for the investigation agent. Muting a monitor is a proposal.",
			"An API key and an application key, write-only.", "", ""),
		p("chat", "Slack, Teams, PagerDuty and Grafana", "Observe and notify", "built-in", intNext, 7,
			"Approval requests and failed runs in chat first; alerts later. Approving still happens in Forgeline, by a different administrator.",
			"An app token, write-only.", "", ""),
	}
}

// firstSentence is a plan up to its first full stop, for a card.
func firstSentence(s string) string {
	if i := strings.Index(s, ". "); i > 0 {
		return s[:i+1]
	}
	return s
}

func (s *Server) handleListIntegrations(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	out := []IntegrationJSON{}
	for _, f := range []func(context.Context, *http.Request) []IntegrationJSON{s.integGitHub, s.integAWS, s.integGCP, s.integAWSMCP, s.integDocker, s.integK8s, s.integClaude} {
		out = append(out, f(ctx, r)...)
	}
	out = append(out, s.detectedIntegrations(ctx)...)
	out = append(out, plannedIntegrations()...)
	for i := range out {
		out[i].LastTest = s.integTests.get(out[i].ID)
	}
	writeJSON(w, http.StatusOK, IntegrationsJSON{Integrations: out})
}

func ago(d time.Duration) string {
	switch {
	case d < time.Minute:
		return "less than a minute"
	case d < time.Hour:
		return fmt.Sprintf("%d minutes", int(d.Minutes()))
	}
	return fmt.Sprintf("%d h %d min", int(d.Hours()), int(d.Minutes())%60)
}

func (s *Server) integGitHub(ctx context.Context, r *http.Request) []IntegrationJSON {
	v := s.githubView(ctx, r)
	it := IntegrationJSON{ID: "github", Name: "GitHub", Blurb: "Runs on push, commit statuses and the Actions page.", Category: "Source and CI", Tier: "built-in", Testable: true, Manage: "/integrations/github/settings",
		ReadOnlyBy: "provider", ReadOnlyNote: "GitHub, through the permissions of the app or token"}
	switch {
	case !v.Configured:
		it.Status, it.Summary = intOff, "Not connected"
	case v.Problem != "":
		it.Status, it.Summary, it.Problem = intAttention, "Needs attention", v.Problem
	case !v.Active:
		it.Status, it.Summary = intOff, "Turned off"
	default:
		it.Status = intConnected
		switch {
		case v.App != nil:
			it.Summary = "Connected through the GitHub App " + v.App.Slug
		case v.Source == "flags":
			it.Summary = "Connected, set by command-line flags"
		default:
			it.Summary = "Connected with an access token"
		}
		if v.PublicURL == "" {
			it.Warnings = append(it.Warnings, "No public address is set, so GitHub cannot deliver webhooks and pushes will not start runs")
		}
	}
	if v.Configured {
		it.Details = append(it.Details, AWSFieldJSON{Label: "Repositories allowed", Value: fmt.Sprint(len(v.Rules))})
		if v.PublicURL != "" {
			it.Details = append(it.Details, AWSFieldJSON{Label: "Public address", Value: v.PublicURL})
		}
	}
	return []IntegrationJSON{it}
}

func (s *Server) integAWS(ctx context.Context, _ *http.Request) []IntegrationJSON {
	st := s.loadAWS(ctx)
	it := IntegrationJSON{ID: "aws", Name: "AWS", Blurb: "Explorer, map, alarms, Guard and spend.", Category: "Cloud", Tier: "built-in", Testable: true, Manage: "/infrastructure?tab=aws",
		ReadOnlyBy: "provider", ReadOnlyNote: "AWS, through the role you sign in with"}
	if !st.Enabled {
		it.Status, it.Summary = intOff, "Not connected"
		return []IntegrationJSON{it}
	}
	it.Status, it.Summary = intConnected, "Connected, region "+st.Region
	if st.Profile == ssoProfile {
		it.Details = append(it.Details, AWSFieldJSON{Label: "Sign-in", Value: "IAM Identity Center"})
		if cfg, ok := s.loadSSO(ctx); ok && cfg.AccountName != "" {
			it.Details = append(it.Details, AWSFieldJSON{Label: "Account", Value: cfg.AccountName})
		}
		t, ok := s.loadSSOToken(ctx)
		switch left := time.Until(t.ExpiresAt); {
		case !ok || left <= 0:
			it.Status, it.Summary, it.Problem = intAttention, "Sign-in expired", "The AWS sign-in has expired, so AWS reads have stopped. Sign in again from Infrastructure."
		case left < intSSOWarn:
			it.Status, it.Summary = intAttention, "Sign-in ends soon"
			it.Problem = fmt.Sprintf("The AWS sign-in ends in %s. Sign in again before then, or AWS reads will stop.", ago(left))
		}
	} else if st.Profile != "" {
		it.Details = append(it.Details, AWSFieldJSON{Label: "Server profile", Value: st.Profile})
	} else {
		it.Details = append(it.Details, AWSFieldJSON{Label: "Credentials", Value: "The server's own identity"})
	}
	if as, err := s.listAWSAccounts(ctx); err == nil && len(as) > 0 {
		it.Details = append(it.Details, AWSFieldJSON{Label: "Extra accounts", Value: fmt.Sprint(len(as))})
	}
	return []IntegrationJSON{it}
}

func (s *Server) integAWSMCP(ctx context.Context, _ *http.Request) []IntegrationJSON {
	m := s.loadAWSMCP(ctx)
	it := IntegrationJSON{ID: "aws-mcp", Name: "AWS MCP Server", Blurb: "Lets the assistant read AWS and file change proposals.", Category: "AI", Tier: "built-in", Testable: true, Manage: "/infrastructure?tab=aws",
		ReadOnlyBy: "provider", ReadOnlyNote: "AWS, through the same role as the AWS connection. Changes apply only after approval"}
	switch {
	case !m.Enabled:
		it.Status, it.Summary = intOff, "Turned off"
	case !s.loadAWS(ctx).Enabled:
		it.Status, it.Summary, it.Problem = intAttention, "Needs attention", "The AWS MCP Server is on, but AWS is not connected."
	default:
		it.Status, it.Summary = intConnected, fmt.Sprintf("On, %d tools offered", len(m.Tools))
	}
	return []IntegrationJSON{it}
}

func (s *Server) integDocker(ctx context.Context, _ *http.Request) []IntegrationJSON {
	ctx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()
	d := s.dockerInfo(ctx)
	it := IntegrationJSON{ID: "docker", Name: "Docker host", Blurb: "Where pipeline steps run as containers.", Category: "Containers", Tier: "built-in", Testable: true, Manage: "/infrastructure?tab=platform",
		ReadOnlyBy: "", ReadOnlyNote: "Steps run as containers with the permissions each pipeline declares. This host is not an agent tool"}
	if !d.Available {
		it.Status, it.Summary, it.Problem = intAttention, "Not reachable", "Forgeline cannot reach Docker, so steps cannot run. "+d.Error
		return []IntegrationJSON{it}
	}
	it.Status, it.Summary = intConnected, fmt.Sprintf("Engine %s, %d of %d containers running", d.Version, d.ContainersRunning, d.Containers)
	return []IntegrationJSON{it}
}

func (s *Server) integK8s(ctx context.Context, _ *http.Request) []IntegrationJSON {
	cs, err := s.listK8sClusters(ctx)
	it := IntegrationJSON{ID: "kubernetes", Name: "Kubernetes", Blurb: "A read-only cluster explorer.", Category: "Containers", Tier: "built-in", Testable: true, Manage: "/kubernetes",
		ReadOnlyBy: "provider", ReadOnlyNote: "Kubernetes, through RBAC on the token or role"}
	switch {
	case err != nil:
		it.Status, it.Summary, it.Problem = intAttention, "Needs attention", "The clusters could not be read."
	case len(cs) == 0:
		it.Status, it.Summary = intOff, "No clusters added"
	default:
		it.Status, it.Summary = intConnected, fmt.Sprintf("%d cluster%s, read-only", len(cs), map[bool]string{true: "", false: "s"}[len(cs) == 1])
	}
	return []IntegrationJSON{it}
}

func (s *Server) integClaude(ctx context.Context, _ *http.Request) []IntegrationJSON {
	v := s.assistantView(ctx)
	it := IntegrationJSON{ID: "claude", Name: "Claude", Blurb: "The model behind the assistant and agents.", Category: "AI", Tier: "built-in", Testable: true, Manage: "/integrations/claude/settings",
		ReadOnlyBy: "", ReadOnlyNote: "A model is not a tool. What it controls is which agents may use it and how much"}
	switch {
	case v.Backend == "" || v.Backend == assistOff:
		it.Status, it.Summary = intOff, "Not set up"
	case !v.Ready:
		it.Status, it.Summary, it.Problem = intAttention, "Needs attention", "The assistant is set to "+v.Backend+" but has no key or token yet."
	default:
		it.Status, it.Summary = intConnected, "Set up, "+claudeBackendName(v.Backend)
		if v.Model != "" {
			it.Details = append(it.Details, AWSFieldJSON{Label: "Model", Value: v.Model})
		}
	}
	return []IntegrationJSON{it}
}

func claudeBackendName(b string) string {
	switch b {
	case assistCLI:
		return "the claude program on the server"
	case assistAPI:
		return "an Anthropic API key"
	}
	return "a Claude login token"
}

// detectedIntegrations lists logins already on this server, by name only. It never reads or copies a
// credential: an AWS profile is listed by its name and an administrator decides what to do with it.
func (s *Server) detectedIntegrations(ctx context.Context) []IntegrationJSON {
	ctx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()
	out, _, err := s.run(ctx, "aws", "configure", "list-profiles")
	if err != nil {
		return nil
	}
	st := s.loadAWS(ctx)
	used := map[string]bool{ssoProfile: true, st.Profile: st.Enabled}
	if as, err := s.listAWSAccounts(ctx); err == nil {
		for _, a := range as {
			used[a.Profile] = true
		}
	}
	var names []string
	for _, ln := range strings.Split(string(out), "\n") {
		if n := strings.TrimSpace(ln); n != "" && awsProfileRE.MatchString(n) && !used[n] {
			names = append(names, n)
		}
	}
	sort.Strings(names)
	var res []IntegrationJSON
	for _, n := range names {
		res = append(res, IntegrationJSON{ID: "aws-profile:" + n, Name: `AWS profile "` + n + `"`, Blurb: "Found on this server. Connect it as a second AWS account.", Category: "Cloud", Tier: "built-in", Status: intDetected,
			Summary: "Found on this server, not connected", Manage: "/infrastructure?tab=aws",
			Found:      `An AWS profile named "` + n + `" is configured for the server's user. Forgeline read only its name, never a credential, and has not used it.`,
			ReadOnlyBy: "provider", ReadOnlyNote: "AWS, through the profile's role"})
	}
	return res
}

// --- the test ---

func checkOf(name, result, detail, fix string, started time.Time) IntegrationCheckJSON {
	return IntegrationCheckJSON{Name: name, Result: result, Detail: detail, Fix: fix, TookMS: time.Since(started).Milliseconds()}
}

func (s *Server) handleTestIntegration(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
	defer cancel()
	started := time.Now()
	var checks []IntegrationCheckJSON
	switch id {
	case "github":
		checks = s.testGitHub(ctx)
	case "aws":
		checks = s.testAWS(ctx)
	case "gcp":
		checks = s.testGCP(ctx)
	case "aws-mcp":
		checks = s.testAWSMCP(ctx)
	case "docker":
		checks = s.testDocker(ctx)
	case "kubernetes":
		checks = s.testK8s(ctx)
	case "claude":
		checks = s.testClaude(ctx)
	default:
		writeError(w, http.StatusNotFound, "there is no test for that integration")
		return
	}
	res := IntegrationTestJSON{ID: id, At: time.Now().UTC().Format(time.RFC3339), By: principalFrom(r.Context()).Name, TookMS: time.Since(started).Milliseconds(), Checks: checks}
	for _, c := range checks {
		switch c.Result {
		case "ok":
			res.Passed++
		case "warn":
			res.Warned++
		default:
			res.Failed++
		}
	}
	s.integTests.put(res)
	s.auditReq(r, "integration.test", "integration/"+id, fmt.Sprintf("%d passed, %d warnings, %d failed", res.Passed, res.Warned, res.Failed), res.Failed == 0)
	writeJSON(w, http.StatusOK, res)
}

func (s *Server) testGitHub(ctx context.Context) []IntegrationCheckJSON {
	t := time.Now()
	gh := s.gh()
	if gh == nil {
		return []IntegrationCheckJSON{checkOf("GitHub is connected", "fail", "The integration is not active.", "Connect GitHub and turn it on first.", t)}
	}
	var cs []IntegrationCheckJSON
	if len(gh.Allow) == 0 {
		cs = append(cs, checkOf("Repositories allowed", "warn", "No repository is on the allow list, so no push can start a run.", "Add a repository and its branches.", t))
	}
	for _, rule := range gh.Allow {
		t = time.Now()
		c := s.checkRepo(ctx, gh, rule.Repo)
		if c.OK {
			cs = append(cs, checkOf("Repository "+rule.Repo+" is readable", "ok", c.Message, "", t))
		} else {
			cs = append(cs, checkOf("Repository "+rule.Repo+" is readable", "fail", c.Message, "Check the token or the app's access to this repository.", t))
		}
	}
	if gh.PublicURL == "" {
		cs = append(cs, checkOf("Webhook address", "warn", "No public address is set, so GitHub cannot deliver webhooks.", "Set the server's public address, or put it behind a tunnel.", time.Now()))
	}
	return cs
}

func (s *Server) testAWS(ctx context.Context) []IntegrationCheckJSON {
	t := time.Now()
	st := s.loadAWS(ctx)
	if !st.Enabled {
		return []IntegrationCheckJSON{checkOf("AWS is connected", "fail", "Not connected.", "Connect AWS under Infrastructure.", t)}
	}
	var cs []IntegrationCheckJSON
	if st.Profile == ssoProfile {
		tok, ok := s.loadSSOToken(ctx)
		switch left := time.Until(tok.ExpiresAt); {
		case !ok || left <= 0:
			cs = append(cs, checkOf("Sign-in is valid", "fail", "The sign-in has expired.", "Sign in again from Infrastructure.", t))
		case left < intSSOWarn:
			cs = append(cs, checkOf("Sign-in is valid", "warn", "It ends in "+ago(left)+".", "Sign in again before then, or AWS reads will stop.", t))
		default:
			cs = append(cs, checkOf("Sign-in is valid", "ok", "Ends in "+ago(left)+".", "", t))
		}
	}
	t = time.Now()
	if _, err := s.aws(ctx, st, "sts", "get-caller-identity"); err != nil {
		return append(cs, checkOf("Identity can be confirmed", "fail", inventoryError(err), "Sign in again, or check the profile and region.", t))
	}
	cs = append(cs, checkOf("Identity can be confirmed", "ok", "AWS accepted the credentials.", "", t))
	t = time.Now()
	if _, err := s.aws(ctx, st, "ec2", "describe-vpcs"); err != nil {
		cs = append(cs, checkOf("Resources can be read", "fail", inventoryError(err), "Give the role read access (for example ReadOnlyAccess).", t))
	} else {
		cs = append(cs, checkOf("Resources can be read", "ok", "A read of the VPC list worked.", "", t))
	}
	t = time.Now()
	if _, err := s.aws(ctx, st, "cloudwatch", "describe-alarms", "--max-items", "1"); err != nil {
		cs = append(cs, checkOf("Alarms can be read", "warn", alarmsError(err), "Allow cloudwatch:DescribeAlarms to show alarms on the map.", t))
	} else {
		cs = append(cs, checkOf("Alarms can be read", "ok", "", "", t))
	}
	return cs
}

func (s *Server) testAWSMCP(ctx context.Context) []IntegrationCheckJSON {
	t := time.Now()
	if !s.loadAWS(ctx).Enabled {
		return []IntegrationCheckJSON{checkOf("AWS is connected", "fail", "Not connected.", "Connect AWS first.", t)}
	}
	ctx, cancel := context.WithTimeout(ctx, mcpStartTimeout)
	defer cancel()
	c, err := s.startAWSMCP(ctx)
	if err != nil {
		return []IntegrationCheckJSON{checkOf("The signing proxy starts", "fail", truncate(err.Error(), 240), "Check that the proxy command is installed and AWS is connected.", t)}
	}
	defer c.Close()
	cs := []IntegrationCheckJSON{checkOf("The signing proxy starts", "ok", "", "", t)}
	t = time.Now()
	tools, err := c.Tools(ctx)
	if err != nil {
		return append(cs, checkOf("Tools are offered", "fail", truncate(err.Error(), 240), "", t))
	}
	found := false
	for _, x := range tools {
		found = found || x.Name == scriptToolName
	}
	if !found {
		return append(cs, checkOf("Tools are offered", "fail", fmt.Sprintf("%d tools, but not %s.", len(tools), scriptToolName), "", t))
	}
	return append(cs, checkOf("Tools are offered", "ok", fmt.Sprintf("%d tools.", len(tools)), "", t))
}

func (s *Server) testDocker(ctx context.Context) []IntegrationCheckJSON {
	t := time.Now()
	d := s.dockerInfo(ctx)
	if !d.Available {
		return []IntegrationCheckJSON{checkOf("Docker answers", "fail", d.Error, "Start Docker, and make sure the server's user may use it.", t)}
	}
	return []IntegrationCheckJSON{checkOf("Docker answers", "ok", "Engine "+d.Version+".", "", t)}
}

func (s *Server) testK8s(ctx context.Context) []IntegrationCheckJSON {
	t := time.Now()
	cs, err := s.listK8sClusters(ctx)
	if err != nil {
		return []IntegrationCheckJSON{checkOf("Clusters can be read", "fail", "The list of clusters could not be read.", "", t)}
	}
	if len(cs) == 0 {
		return []IntegrationCheckJSON{checkOf("A cluster is added", "fail", "No cluster has been added.", "Add a cluster on the Kubernetes page.", t)}
	}
	var out []IntegrationCheckJSON
	for _, c := range cs {
		t = time.Now()
		conn, err := s.connFor(ctx, c)
		if err != nil {
			out = append(out, checkOf("Cluster "+c.Name+" answers", "fail", err.Error(), "Add the cluster again.", t))
			continue
		}
		var ver struct{ GitVersion string }
		if err := conn.get(ctx, "/version", &ver); err != nil {
			out = append(out, checkOf("Cluster "+c.Name+" answers", "fail", truncate(err.Error(), 240), "Check the address and the token.", t))
			continue
		}
		out = append(out, checkOf("Cluster "+c.Name+" answers", "ok", "Kubernetes "+ver.GitVersion+".", "", t))
	}
	return out
}

func (s *Server) testClaude(ctx context.Context) []IntegrationCheckJSON {
	t := time.Now()
	v := s.assistantView(ctx)
	if !v.Ready {
		return []IntegrationCheckJSON{checkOf("The assistant is set up", "fail", "No backend is ready.", "Turn it on under Integrations.", t)}
	}
	cs := []IntegrationCheckJSON{checkOf("The assistant is set up", "ok", claudeBackendName(v.Backend)+".", "", t)}
	t = time.Now()
	reply, err := s.askAssistant(ctx, authn.Role(""), []chatTurn{{Role: "user", Content: "Reply with the single word OK."}}, "")
	if err != nil {
		return append(cs, checkOf("A test question is answered", "fail", truncate(err.Error(), 240), "Check the key, the login or the program.", t))
	}
	return append(cs, checkOf("A test question is answered", "ok", "It sent one short question and stored nothing. Reply: "+truncate(strings.TrimSpace(reply), 40), "", t))
}

func (s *Server) integGCP(ctx context.Context, _ *http.Request) []IntegrationJSON {
	st := s.loadGCP(ctx)
	it := IntegrationJSON{ID: "gcp", Name: "Google Cloud", Blurb: "A read-only explorer of a project, folder or organization.", Category: "Cloud", Tier: "built-in", Testable: true, Manage: "/infrastructure?tab=gcp",
		ReadOnlyBy: "provider", ReadOnlyNote: "Google, through the Cloud Asset Viewer role of the server's gcloud login. Forgeline only runs read commands"}
	if !st.Enabled {
		it.Status, it.Summary = intOff, "Not connected"
		return []IntegrationJSON{it}
	}
	it.Status, it.Summary = intConnected, "Connected, "+st.Scope
	it.Details = append(it.Details, AWSFieldJSON{Label: "Scope", Value: st.Scope}, AWSFieldJSON{Label: "Sign-in", Value: "The gcloud login of the server's user"})
	return []IntegrationJSON{it}
}

func (s *Server) testGCP(ctx context.Context) []IntegrationCheckJSON {
	t := time.Now()
	st := s.loadGCP(ctx)
	if !st.Enabled {
		return []IntegrationCheckJSON{checkOf("Google Cloud is connected", "fail", "Not connected.", "Connect it under Infrastructure, Google Cloud.", t)}
	}
	cs := []IntegrationCheckJSON{checkOf("A scope is chosen", "ok", st.Scope, "", t)}
	t = time.Now()
	out, err := s.gcloud(ctx, "auth", "list", "--filter=status:ACTIVE", "--format=value(account)")
	if err != nil {
		return append(cs, checkOf("The server has a Google login", "fail", err.Error(), "Run `gcloud auth login` as the server's user.", t))
	}
	acct := strings.TrimSpace(string(out))
	if acct == "" {
		return append(cs, checkOf("The server has a Google login", "fail", "No account is signed in.", "Run `gcloud auth login` as the server's user.", t))
	}
	cs = append(cs, checkOf("The server has a Google login", "ok", "Signed in as "+acct+".", "", t))
	t = time.Now()
	if _, err := s.searchGCP(ctx, st.Scope, 1); err != nil {
		return append(cs, checkOf("Resources can be searched", "fail", err.Error(), "Grant the login roles/cloudasset.viewer on the scope and enable the Cloud Asset API.", t))
	}
	return append(cs, checkOf("Resources can be searched", "ok", fmt.Sprintf("Cloud Asset Inventory answered for %s.", st.Scope), "", t), checkOf("Created nothing, changed nothing", "ok", "The test only searched.", "", time.Now()))
}
