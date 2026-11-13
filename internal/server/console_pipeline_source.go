package server

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"

	"github.com/forgelinehq/forgeline/internal/store"
)

// A saved pipeline can name a source: a repository and branch on the GitHub allow list. Every run started from the
// console (or by the assistant) then downloads that branch's newest commit into its workspace first, as a run GitHub
// starts does, and records which commit it ran (design note 043, slice 2). The pipeline itself still comes from the
// console, so such a run is a console run: it gets the console's secrets and never pushes images.

// PipelineSource is the repository and branch a saved pipeline's runs check out.
type PipelineSource struct {
	Repo   string `json:"repo"`
	Branch string `json:"branch"`
}

var errSourceDownload = errors.New("could not download the pipeline's source")

var branchNameRE = regexp.MustCompile(`^[A-Za-z0-9._/-]{1,200}$`)

// checkPipelineSource says what is wrong with a source, or "". The branch must be allowed by the GitHub allow list,
// read now; a pattern is not a branch.
func (s *Server) checkPipelineSource(src *PipelineSource) string {
	if src == nil {
		return ""
	}
	gh := s.gh()
	switch {
	case !repoNameRE.MatchString(src.Repo):
		return "source.repo: give the repository as owner/name"
	case !branchNameRE.MatchString(src.Branch) || strings.Contains(src.Branch, "..") || strings.HasPrefix(src.Branch, "refs/") ||
		strings.HasPrefix(src.Branch, "/") || strings.HasSuffix(src.Branch, "/"):
		return "source.branch: give one branch name such as main or release/2"
	case gh == nil:
		return "source: GitHub is not connected on this server, so a run cannot download a repository"
	case !gh.allowed(src.Repo, src.Branch):
		return fmt.Sprintf("source: %s branch %q is not on the GitHub allow list (Settings › Connections › GitHub)", src.Repo, src.Branch)
	}
	return ""
}

// branchHead asks GitHub for the newest commit of a branch.
func (s *Server) branchHead(ctx context.Context, repo, branch string) (string, error) {
	gh := s.gh()
	if gh == nil {
		return "", errors.New("GitHub is not connected")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, gh.APIBase+"/repos/"+repo+"/commits/"+branch, nil)
	if err != nil {
		return "", err
	}
	if err := s.githubHeaders(req); err != nil {
		return "", err
	}
	req.Header.Set("Accept", "application/vnd.github.sha")
	resp, err := gh.HTTPClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 200))
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("GitHub answered %s for %s branch %q", resp.Status, repo, branch)
	}
	sha := strings.TrimSpace(string(body))
	if !shaRE.MatchString(sha) {
		return "", fmt.Errorf("GitHub did not return a commit for %s branch %q", repo, branch)
	}
	return sha, nil
}

// runSaved starts a run of a saved pipeline's current version, checking out its source first when it has one.
func (s *Server) runSaved(ctx context.Context, sp SavedPipeline) (store.Run, []store.Step, *apiError) {
	if sp.Source == nil {
		return s.enqueue(ctx, sp.YAML, store.TriggerUI, nil, nil)
	}
	// The allow list is read again now: a branch taken off it stops being checked out.
	if m := s.checkPipelineSource(sp.Source); m != "" {
		return store.Run{}, nil, &apiError{http.StatusConflict, ErrorJSON{Error: m}}
	}
	select {
	case s.checkouts <- struct{}{}:
		defer func() { <-s.checkouts }()
	default:
		return store.Run{}, nil, &apiError{http.StatusServiceUnavailable, ErrorJSON{Error: "too many commits are being downloaded; try again in a moment"}}
	}
	cctx, cancel := context.WithTimeout(ctx, checkoutTimeout)
	defer cancel()
	sha, err := s.branchHead(cctx, sp.Source.Repo, sp.Source.Branch)
	if err != nil {
		return store.Run{}, nil, &apiError{http.StatusBadGateway, ErrorJSON{Error: "could not find the branch's newest commit: " + err.Error()}}
	}
	unpack := func(ws string) error {
		_, err := s.fetchCommit(cctx, sp.Source.Repo, sha, ws)
		if err != nil {
			return fmt.Errorf("%w: %s@%.7s: %v", errSourceDownload, sp.Source.Repo, sha, err)
		}
		return nil
	}
	return s.enqueue(ctx, sp.YAML, store.TriggerUI, unpack, &SourceRef{Repo: sp.Source.Repo, Ref: "refs/heads/" + sp.Source.Branch, SHA: sha})
}

// sourceNote is the audit's words for where a run's files came from.
func sourceNote(run store.Run) string {
	if run.SourceRepo == "" {
		return ""
	}
	return fmt.Sprintf(", source %s@%.7s", run.SourceRepo, run.SourceSHA)
}
