package server

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/forgelinehq/forgeline/internal/authn"
	"github.com/forgelinehq/forgeline/internal/build"
	"github.com/forgelinehq/forgeline/internal/docstore"
	"github.com/forgelinehq/forgeline/internal/pipeline"
	"github.com/forgelinehq/forgeline/internal/store"
)

// Build steps (design note 043). Any run may build: the Dockerfile's RUN lines get nothing a container step does
// not. Pushing is trusted only for a run GitHub started for a push to a branch an administrator's push rule names,
// to the exact repository the rule names. The push credential is a fresh ECR token in a private client
// configuration; it never enters a container or a log.

const (
	kindBuildPushRule = "buildpushrule"
	maxPushRules      = 50
	defaultBuildLimit = 30 * time.Minute
)

var pushRuleIDRE = regexp.MustCompile(`^[a-z][a-z0-9-]{0,39}$`)

// BuildPushRule lets builds from one repository's branches push to one ECR repository.
type BuildPushRule struct {
	ID        string    `json:"id"`
	Repo      string    `json:"repo"`     // owner/name, compared case-insensitively
	Branches  []string  `json:"branches"` // names or patterns such as release/*, as on the GitHub allow list
	Target    string    `json:"target"`   // <account>.dkr.ecr.<region>.amazonaws.com/<name>, no tag
	Account   string    `json:"account"`  // the Forgeline AWS account that pushes: "primary" or an added account's id
	UpdatedBy string    `json:"updated_by"`
	UpdatedAt time.Time `json:"updated_at"`
}

// buildRun and ecrToken are replaced in tests.
var (
	buildRun = build.Run
	ecrToken = func(ctx context.Context, s *Server, st awsSettings, registryID string) (host, auth string, err error) {
		out, err := s.aws(ctx, st, "ecr", "get-authorization-token", "--registry-ids", registryID)
		if err != nil {
			return "", "", err
		}
		var r struct {
			AuthorizationData []struct {
				AuthorizationToken string `json:"authorizationToken"`
				ProxyEndpoint      string `json:"proxyEndpoint"`
			} `json:"authorizationData"`
		}
		if err := json.Unmarshal(out, &r); err != nil || len(r.AuthorizationData) != 1 {
			return "", "", errors.New("ECR did not return one authorization token")
		}
		d := r.AuthorizationData[0]
		if raw, err := base64.StdEncoding.DecodeString(d.AuthorizationToken); err != nil || !strings.HasPrefix(string(raw), "AWS:") {
			return "", "", errors.New("ECR returned a token Forgeline does not understand")
		}
		return strings.TrimPrefix(d.ProxyEndpoint, "https://"), d.AuthorizationToken, nil
	}
)

// pushRuleProblem says what is wrong with a rule, or "".
func (s *Server) pushRuleProblem(ctx context.Context, r BuildPushRule) string {
	switch {
	case !pushRuleIDRE.MatchString(r.ID):
		return "id: lower-case letters, digits and dashes, starting with a letter, at most 40"
	case !repoNameRE.MatchString(r.Repo):
		return "repo: give the repository as owner/name"
	case len(r.Branches) == 0 || len(r.Branches) > 10:
		return "branches: list 1 to 10 branches, such as main or release/*"
	case !pipeline.BuildPushRE.MatchString(r.Target):
		return "target: an ECR repository such as 123456789012.dkr.ecr.ap-south-1.amazonaws.com/web, without a tag"
	}
	for _, b := range r.Branches {
		if _, err := path.Match(b, ""); err != nil || b == "" || b == "*" || strings.HasPrefix(b, "refs/") {
			return fmt.Sprintf("branches: %q is not a branch name or pattern (a bare * is not allowed)", b)
		}
	}
	st, err := s.awsFor(ctx, r.Account)
	if err != nil || !st.Enabled {
		return "account: no connected AWS account " + fmt.Sprintf("%q", r.Account)
	}
	return ""
}

func (s *Server) listPushRules(ctx context.Context) ([]BuildPushRule, error) {
	docs, err := s.docs.List(ctx, kindBuildPushRule, docstore.ListOptions{})
	if err != nil {
		return nil, err
	}
	out, err := docstore.Decode[BuildPushRule](docs)
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, err
}

func (s *Server) handleListPushRules(w http.ResponseWriter, r *http.Request) {
	all, err := s.listPushRules(r.Context())
	if err != nil {
		s.internalError(w, "reading the push rules", err)
		return
	}
	if all == nil {
		all = []BuildPushRule{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"rules": all})
}

func (s *Server) handlePutPushRule(w http.ResponseWriter, r *http.Request) {
	var req BuildPushRule
	if !readJSON(w, r, &req) {
		return
	}
	ctx := r.Context()
	req.ID = r.PathValue("id")
	if req.Account == "" {
		req.Account = primaryAccount
	}
	if m := s.pushRuleProblem(ctx, req); m != "" {
		writeError(w, http.StatusBadRequest, m)
		return
	}
	var old BuildPushRule
	if err := s.docs.Get(ctx, kindBuildPushRule, req.ID, &old); errors.Is(err, docstore.ErrNotFound) {
		if all, _ := s.listPushRules(ctx); len(all) >= maxPushRules {
			writeError(w, http.StatusConflict, fmt.Sprintf("at most %d push rules", maxPushRules))
			return
		}
	}
	req.UpdatedBy, req.UpdatedAt = principalFrom(ctx).Name, time.Now().UTC()
	if err := s.docs.Put(ctx, kindBuildPushRule, req.ID, req); err != nil {
		s.internalError(w, "saving the push rule", err)
		return
	}
	s.auditReq(r, "build.pushrule.put", "pushrule/"+req.ID,
		fmt.Sprintf("%s (%s) may push to %s as account %s", req.Repo, strings.Join(req.Branches, ", "), req.Target, req.Account), true)
	writeJSON(w, http.StatusOK, req)
}

func (s *Server) handleDeletePushRule(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := s.docs.Delete(r.Context(), kindBuildPushRule, id); errors.Is(err, docstore.ErrNotFound) {
		writeError(w, http.StatusNotFound, "no such push rule")
		return
	} else if err != nil {
		s.internalError(w, "deleting the push rule", err)
		return
	}
	s.auditReq(r, "build.pushrule.delete", "pushrule/"+id, "", true)
	w.WriteHeader(http.StatusNoContent)
}

// pushTrusted says whether run is one that may push at all, or why not. Only a run GitHub started for a push
// (refs/heads/<branch>) can; a pull request's code, or a console or API run, never can. Such runs still build.
func pushTrusted(run store.Run) (branch, why string) {
	if run.Trigger != store.TriggerGitHub {
		return "", "only runs GitHub starts for a push may push images (this run was started from " + string(run.Trigger) + ")"
	}
	branch, ok := strings.CutPrefix(run.SourceRef, "refs/heads/")
	if !ok || branch == "" {
		return "", "only a push to a branch may push images; this run is for " + run.SourceRef
	}
	return branch, ""
}

// pushRuleFor returns the rule that lets run push to target, or why none does.
func (s *Server) pushRuleFor(ctx context.Context, run store.Run, target string) (BuildPushRule, string) {
	branch, why := pushTrusted(run)
	if why != "" {
		return BuildPushRule{}, why
	}
	rules, err := s.listPushRules(ctx)
	if err != nil {
		return BuildPushRule{}, "the push rules could not be read"
	}
	for _, r := range rules {
		if !strings.EqualFold(r.Repo, run.SourceRepo) || r.Target != target {
			continue
		}
		for _, pat := range r.Branches {
			if ok, _ := path.Match(pat, branch); ok {
				return r, ""
			}
		}
	}
	return BuildPushRule{}, fmt.Sprintf("no push rule lets %s branch %q push to %s", run.SourceRepo, branch, target)
}

// buildStep returns the function the scheduler calls for a build step of run.
func (s *Server) buildStep(run store.Run) func(context.Context, *pipeline.Step, io.Writer) error {
	return func(ctx context.Context, st *pipeline.Step, logs io.Writer) error {
		who := Principal{Name: "run " + run.ID, Role: authn.Developer}
		b := *st.Build
		source := run.SourceRepo + "@" + shortSHA(run.SourceSHA)
		if run.SourceRepo == "" {
			source = "the run's workspace"
		}
		if st.Timeout == "" {
			var cancel context.CancelFunc
			ctx, cancel = context.WithTimeout(ctx, defaultBuildLimit)
			defer cancel()
		}
		req := build.Request{Docker: os.Getenv("FORGELINE_DOCKER"), Workspace: s.workspace(run.ID), Build: b}
		target := ""
		if _, untrusted := pushTrusted(run); b.Push != "" && untrusted != "" {
			// A pull request or a console run checks that the image builds; it never pushes, and that is not an error.
			fmt.Fprintf(logs, "building without pushing: %s\n", untrusted)
			s.audit(ctx, who, "", "build.push", "image/"+b.Push, "not pushed (built only): "+untrusted, false)
			b.Push, b.Tags = "", nil
			req.Build = b
		}
		if b.Push != "" {
			rule, why := s.pushRuleFor(ctx, run, b.Push)
			if why != "" {
				s.audit(ctx, who, "", "build.push", "image/"+b.Push, "refused: "+why, false)
				return errors.New("push refused: " + why)
			}
			tags := b.Tags
			if len(tags) == 0 {
				if run.SourceSHA == "" {
					return errors.New("push refused: give tags, since this run has no commit to tag the image with")
				}
				tags = []string{shortSHA(run.SourceSHA)}
			}
			for _, t := range tags {
				req.Refs = append(req.Refs, b.Push+":"+t)
			}
			acct, err := s.awsFor(ctx, rule.Account)
			if err != nil || !acct.Enabled {
				return fmt.Errorf("push refused: the AWS account %q of push rule %s is not connected", rule.Account, rule.ID)
			}
			registryID, _, _ := strings.Cut(b.Push, ".")
			host, auth, err := ecrToken(ctx, s, acct, registryID)
			if err != nil {
				s.audit(ctx, who, "", "build.push", "image/"+b.Push, "no credential: "+truncate(err.Error(), 200), false)
				return fmt.Errorf("could not get a push credential from ECR: %w", err)
			}
			if registryHost, _, _ := strings.Cut(b.Push, "/"); host != registryHost {
				return fmt.Errorf("ECR gave a credential for %s, not %s", host, registryHost)
			}
			req.AuthHost, req.Auth = host, auth
			target = " and pushing to " + strings.Join(req.Refs, ", ") + " (push rule " + rule.ID + ")"
		}
		fmt.Fprintf(logs, "building %s from %s%s\n", orDot(b.Context), source, target)
		start := time.Now()
		res, err := buildRun(ctx, req, logs)
		detail := fmt.Sprintf("%s, context %s, %s", source, orDot(b.Context), time.Since(start).Round(time.Second))
		if err != nil {
			s.audit(ctx, who, "", "build.run", "build/"+run.ID+"/"+st.ID, detail+": "+truncate(err.Error(), 200), false)
			return err
		}
		s.audit(ctx, who, "", "build.run", "build/"+run.ID+"/"+st.ID, detail, true)
		if len(req.Refs) > 0 {
			fmt.Fprintf(logs, "pushed %s\n", b.Push+"@"+res.Digest)
			s.audit(ctx, who, "", "build.push", "image/"+b.Push, strings.Join(req.Refs, ", ")+" "+res.Digest+" from "+source, true)
		}
		return nil
	}
}

func shortSHA(sha string) string {
	if len(sha) > 12 {
		return sha[:12]
	}
	return sha
}

func orDot(p string) string {
	if p == "" {
		return "."
	}
	return p
}
