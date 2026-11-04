package server

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strings"
)

// Reading a repository for the assistant (design note 028, "set up CI"): which repositories are on the allow list, and
// the files in them, so it can write a pipeline that fits what is there. Read only, allow list only, default branch only.
// What a repository holds was written by other people, so a lookup result is data, and a turn that read it is "dirty" for
// memory like any other lookup.

const (
	maxRepoFileBytes = 12 << 10
	maxRepoListing   = 80
)

var (
	repoPathRE = regexp.MustCompile(`^[A-Za-z0-9._/@+-]{0,200}$`)
	// Files that hold secrets are never read, whatever the allow list says.
	secretFileRE = regexp.MustCompile(`(?i)(^|/)(\.env[^/]*|id_(rsa|dsa|ecdsa|ed25519)|[^/]*\.(pem|key|p12|pfx|jks|kdbx)|\.npmrc|\.pypirc|\.netrc|credentials|[^/]*secret[^/]*)$`)
)

func (s *Server) lookupGitHubRepos(ctx context.Context, _ json.RawMessage) (string, error) {
	gh := s.gh()
	if gh == nil {
		return "", errors.New("GitHub is not connected")
	}
	var b strings.Builder
	for _, rule := range gh.Allow {
		fmt.Fprintf(&b, "- %s (branches that may run: %s)\n", rule.Repo, strings.Join(rule.Branches, ", "))
	}
	if b.Len() == 0 {
		return "GitHub is connected but no repository is on the allow list. An administrator adds them under Integrations, GitHub.", nil
	}
	return b.String(), nil
}

// lookupGitHubFiles lists a directory or reads one file of an allow-listed repository.
func (s *Server) lookupGitHubFiles(ctx context.Context, raw json.RawMessage) (string, error) {
	var a struct {
		Repo string `json:"repo"`
		Path string `json:"path"`
	}
	if err := decodeArgs(raw, &a); err != nil {
		return "", err
	}
	gh := s.gh()
	if gh == nil {
		return "", errors.New("GitHub is not connected")
	}
	if !repoNameRE.MatchString(a.Repo) {
		return "", errors.New("repo must look like owner/name")
	}
	allowed := false
	for _, rule := range gh.Allow {
		if strings.EqualFold(rule.Repo, a.Repo) {
			allowed = true
		}
	}
	if !allowed {
		return "", errors.New("that repository is not on the GitHub allow list (see github_repos)")
	}
	a.Path = strings.Trim(a.Path, "/")
	if !repoPathRE.MatchString(a.Path) || strings.Contains(a.Path, "..") {
		return "", errors.New("path is not valid")
	}
	if secretFileRE.MatchString(a.Path) {
		return "", errors.New("that file may hold secrets and is never read")
	}
	var parts []string
	for _, p := range strings.Split(a.Path, "/") {
		parts = append(parts, url.PathEscape(p))
	}
	api := "/repos/" + a.Repo + "/contents"
	if a.Path != "" {
		api += "/" + strings.Join(parts, "/")
	}
	var body json.RawMessage
	if _, err := s.ghActionsCall(ctx, gh, "GET", api, nil, &body); err != nil {
		return "", err
	}
	// A directory is a list, a file is an object.
	var dir []struct {
		Name string `json:"name"`
		Type string `json:"type"`
		Size int    `json:"size"`
	}
	if json.Unmarshal(body, &dir) == nil {
		var b strings.Builder
		n := 0
		for _, e := range dir {
			if secretFileRE.MatchString(e.Name) {
				continue
			}
			if n++; n > maxRepoListing {
				b.WriteString("... more entries not shown\n")
				break
			}
			fmt.Fprintf(&b, "- %s%s\n", e.Name, map[bool]string{true: "/", false: ""}[e.Type == "dir"])
		}
		return "Directory /" + a.Path + " of " + a.Repo + ":\n" + b.String(), nil
	}
	var file struct {
		Type     string `json:"type"`
		Size     int    `json:"size"`
		Encoding string `json:"encoding"`
		Content  string `json:"content"`
	}
	if err := json.Unmarshal(body, &file); err != nil || file.Type != "file" || file.Encoding != "base64" {
		return "", errors.New("that path is not a readable file")
	}
	data, err := base64.StdEncoding.DecodeString(strings.ReplaceAll(file.Content, "\n", ""))
	if err != nil {
		return "", errors.New("the file could not be read")
	}
	text := string(data)
	if !isMostlyText(text) {
		return "", errors.New("that file is not text")
	}
	note := ""
	if len(text) > maxRepoFileBytes {
		text, note = text[:maxRepoFileBytes], fmt.Sprintf("\n[cut: showing the first %d of %d bytes]", maxRepoFileBytes, file.Size)
	}
	text, _ = hideSecrets(text)
	return "File /" + a.Path + " of " + a.Repo + ":\n" + text + note, nil
}

func isMostlyText(s string) bool {
	for _, r := range s[:min(len(s), 2048)] {
		if r == 0 {
			return false
		}
	}
	return true
}
