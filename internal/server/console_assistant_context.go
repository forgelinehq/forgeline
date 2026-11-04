package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/forgelinehq/forgeline/internal/docstore"
	"github.com/forgelinehq/forgeline/internal/store"
)

// What the assistant is given besides the question (design note 027, "context"): a short description of the
// setup written by the server from its own state, and the page the person is on. The setup holds no text a person
// wrote (only fixed names, counts and the person's own account), so it does not make a turn "dirty" for memory.

type setupKey struct{}

// setupText describes this installation: who is asking, the date, what is connected, and what needs a person.
func (s *Server) setupText(ctx context.Context, p Principal) string {
	var b strings.Builder
	fmt.Fprintf(&b, "About this setup (written by the server):\n- Today is %s (UTC). You are talking to %s, whose role is %s.\n",
		time.Now().UTC().Format("2006-01-02"), p.Name, p.Role)
	// Stored settings only: no command is run to describe the setup, because this is built for every question.
	var on []string
	if s.gh() != nil {
		on = append(on, "GitHub")
	}
	if s.loadAWS(ctx).Enabled {
		on = append(on, "AWS")
	}
	if s.loadAWSMCP(ctx).Enabled {
		on = append(on, "AWS MCP Server")
	}
	if s.loadGCP(ctx).Enabled {
		on = append(on, "Google Cloud")
	}
	if s.loadAzure(ctx).Enabled {
		on = append(on, "Azure")
	}
	if cs, err := s.listK8sClusters(ctx); err == nil && len(cs) > 0 {
		on = append(on, fmt.Sprintf("Kubernetes (%d clusters)", len(cs)))
	}
	if h := s.hostLine(ctx); h != "" {
		b.WriteString(h)
	}
	if len(on) == 0 {
		b.WriteString("- Nothing is connected yet.\n")
	}
	if len(on) > 0 {
		b.WriteString("- Connected: " + strings.Join(on, ", ") + ".\n")
	}
	if runs, err := s.cfg.Store.ListRuns(ctx, store.ListOptions{Limit: 200}); err == nil {
		total, failed, running := 0, 0, 0
		for _, run := range runs {
			if run.Status == store.RunRunning {
				running++
			}
			if time.Since(run.CreatedAt) < 24*time.Hour {
				total++
				if run.Status == store.RunFailed {
					failed++
				}
			}
		}
		fmt.Fprintf(&b, "- Runs in the last 24 hours: %d, of which %d failed; %d running now.\n", total, failed, running)
	}
	if docs, err := s.docs.List(ctx, kindAIProposal, docstore.ListOptions{}); err == nil {
		if ps, err := docstore.Decode[AIProposal](docs); err == nil {
			n := 0
			for _, pr := range ps {
				if pr.Status == apProposed && time.Since(pr.ProposedAt) <= aiProposalTTL {
					n++
				}
			}
			fmt.Fprintf(&b, "- Change proposals waiting for approval: %d.\n", n)
		}
	}
	return b.String()
}

// hostCache keeps the line about the Docker host for a minute: it asks Docker, so it is not done per question.
type hostCache struct {
	mu   sync.Mutex
	at   time.Time
	line string
}

// hostLine says what the machine Forgeline runs on looks like: Docker's state and how many containers run.
func (s *Server) hostLine(ctx context.Context) string {
	c := &s.host
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.at.IsZero() && time.Since(c.at) < time.Minute {
		return c.line
	}
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	d := s.dockerInfo(ctx)
	line := "- This server's own Docker host is not reachable, so pipeline steps cannot run.\n"
	if d.Available {
		line = fmt.Sprintf("- This server runs pipeline steps in Docker (engine %s); %d of %d containers on the host are running, most of them not Forgeline's.\n", d.Version, d.ContainersRunning, d.Containers)
	}
	c.at, c.line = time.Now(), line
	return line
}

// assistantContextFor builds the context for a page. The kinds that need cloud or cluster data go through the
// same lookups the assistant would use, so the person's role and the access policy apply to them too.
func (s *Server) assistantContextFor(ctx context.Context, p Principal, c chatContext) (text, note string, err error) {
	via := func(tool string, args any, label string) (string, string, error) {
		raw, _ := json.Marshal(args)
		out, lerr := s.runLookup(ctx, p, lookupCall{Tool: tool, Args: raw})
		if lerr != nil {
			return "Not available here: " + lerr.Error() + "\n", label, nil
		}
		return truncate(out, maxAssistContext), label, nil
	}
	switch c.Kind {
	case "k8s":
		if c.ID == "" {
			return via("k8s_clusters", map[string]string{}, "Kubernetes clusters")
		}
		return via("k8s_resources", map[string]string{"cluster": c.ID}, "cluster "+c.ID)
	case "guard":
		return via("guard_findings", map[string]string{}, "Guard findings")
	case "proposal":
		var pr AIProposal
		if e := s.docs.Get(ctx, kindAIProposal, c.ID, &pr); errors.Is(e, docstore.ErrNotFound) {
			return "", "", errors.New("no such proposal")
		} else if e != nil {
			return "", "", errors.New("could not read the proposal")
		}
		var b strings.Builder
		fmt.Fprintf(&b, "Change proposal %q (id %s), status %s, risk %s, proposed by %s for %s.\nReason: %s\n\nWhat it would run:\n%s\n", pr.Title, pr.ID, pr.Status, pr.Risk, pr.ProposedBy, pr.Target, pr.Reason, truncate(pr.Code, 6<<10))
		return truncate(b.String(), maxAssistContext), "proposal " + pr.ID, nil
	case "agent":
		var ag AIAgent
		if e := s.docs.Get(ctx, kindAIAgent, c.ID, &ag); errors.Is(e, docstore.ErrNotFound) {
			return "", "", errors.New("no such agent")
		} else if e != nil {
			return "", "", errors.New("could not read the agent")
		}
		var b strings.Builder
		fmt.Fprintf(&b, "Agent %q (id %s): %s\nDisabled: %v. Current version %d of %d.\n", ag.Name, ag.ID, ag.Description, ag.Disabled, ag.Current, len(ag.Versions))
		for _, v := range ag.Versions {
			if v.N == ag.Current {
				fmt.Fprintf(&b, "Tier %s. Tools: %s.\nInstructions:\n%s\n", v.Tier, strings.Join(v.Tools, ", "), truncate(v.Instructions, 4<<10))
			}
		}
		return truncate(b.String(), maxAssistContext), "agent " + ag.ID, nil
	}
	return s.assistantContext(ctx, c)
}

// lookupMorningCheck is one look at everything that may need a person: the host, failed runs, waiting proposals and the
// worst open findings. It only reads, from the same sources as the console pages.
func (s *Server) lookupMorningCheck(ctx context.Context, _ json.RawMessage) (string, error) {
	var b strings.Builder
	b.WriteString("Host:\n" + s.hostLine(ctx))
	if runs, err := s.cfg.Store.ListRuns(ctx, store.ListOptions{Limit: 200}); err == nil {
		total, failed := 0, []string{}
		for _, r := range runs {
			if time.Since(r.CreatedAt) > 24*time.Hour {
				continue
			}
			total++
			if r.Status == store.RunFailed && len(failed) < 8 {
				failed = append(failed, fmt.Sprintf("%s (%s, run %s)", r.PipelineName, r.CreatedAt.Format("15:04 UTC"), r.ID))
			}
		}
		fmt.Fprintf(&b, "Runs in the last 24 hours: %d. Failed: %d", total, len(failed))
		if len(failed) > 0 {
			b.WriteString(": " + strings.Join(failed, "; "))
		}
		b.WriteString(".\n")
	}
	if docs, err := s.docs.List(ctx, kindAIProposal, docstore.ListOptions{}); err == nil {
		if ps, err := docstore.Decode[AIProposal](docs); err == nil {
			var waiting []string
			for _, pr := range ps {
				if pr.Status == apProposed && time.Since(pr.ProposedAt) <= aiProposalTTL {
					waiting = append(waiting, fmt.Sprintf("%q (id %s, risk %s)", truncate(pr.Title, 80), pr.ID, pr.Risk))
				}
			}
			fmt.Fprintf(&b, "Change proposals waiting for approval: %d", len(waiting))
			if len(waiting) > 0 {
				b.WriteString(": " + strings.Join(waiting, "; "))
			}
			b.WriteString(".\n")
		}
	}
	if all, err := s.guardViews(ctx, time.Now()); err == nil {
		bySev := map[string]int{}
		var top []string
		for _, f := range all {
			if f.Status != "open" || f.Accepted != nil {
				continue
			}
			bySev[f.Severity]++
			if (f.Severity == "critical" || f.Severity == "high") && len(top) < 5 {
				top = append(top, fmt.Sprintf("[%s] %s: %s (%s)", f.Severity, f.Title, f.Name, f.AccountName))
			}
		}
		fmt.Fprintf(&b, "Open Guard findings: %d critical, %d high, %d medium, %d low.", bySev["critical"], bySev["high"], bySev["medium"], bySev["low"])
		if len(top) > 0 {
			b.WriteString(" Worst: " + strings.Join(top, "; ") + ".")
		}
		b.WriteString(" (From the last scan; none means no scan has run if the counts are all zero.)\n")
	}
	return b.String(), nil
}
