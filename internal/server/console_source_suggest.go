package server

import (
	"context"
	"encoding/base64"
	"fmt"
	"net/http"
	"net/url"
	"path"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/forgelinehq/forgeline/internal/blueprint"
)

// Which repository fits a block (design note 040, step 4). Forgeline reads the repositories on the GitHub allow list
// (default branch, read only): their file tree, and the Dockerfiles in it. It scores each Dockerfile's folder against
// the block (what it is, its port, its name) and says why in plain words. What a repository holds was written by other
// people, so nothing from it is passed on as text: only facts parsed from it (port numbers, whether it starts from a
// Lambda base image, which language manifests sit next to it) and paths that pass the same checks as a block's source.

const (
	maxSuggestRepos       = 20
	maxDockerfilesPerRepo = 5
	maxDockerfilesRead    = 25
	maxSuggestions        = 5
	maxDockerfileDepth    = 4 // folders deep
	suggestTimeout        = 30 * time.Second
)

// SourceSuggestionJSON is one repository folder that could hold a block's code.
type SourceSuggestionJSON struct {
	Repo    string   `json:"repo"`
	Branch  string   `json:"branch"`
	Path    string   `json:"path"` // the folder with the Dockerfile; "" is the top
	Score   int      `json:"score"`
	Reasons []string `json:"reasons"`
}

// dockerfileFacts is what Forgeline takes from one Dockerfile and the folder it sits in.
type dockerfileFacts struct {
	Repo, Branch, Dir string
	Ports             []int
	Lambda            bool     // starts from an AWS Lambda base image
	Agent             bool     // uses the Bedrock AgentCore or Strands Agents SDK
	Arm64             bool     // builds for arm64
	Langs             []string // from manifests in the same folder
}

var (
	fromRE      = regexp.MustCompile(`(?i)^FROM\s+(?:--platform=(\S+)\s+)?(\S+)(?:\s+AS\s+(\S+))?`)
	exposeRE    = regexp.MustCompile(`(?i)^EXPOSE\s+(.+)$`)
	agentSDKRE  = regexp.MustCompile(`(?i)bedrock[-_]agentcore|strands[-_]agents`)
	lambdaBases = []string{"public.ecr.aws/lambda/", "amazon/aws-lambda-"}
	manifests   = map[string]string{"go.mod": "Go", "package.json": "Node.js", "requirements.txt": "Python", "pyproject.toml": "Python",
		"pom.xml": "Java", "build.gradle": "Java", "build.gradle.kts": "Kotlin", "Cargo.toml": "Rust", "Gemfile": "Ruby", "composer.json": "PHP"}
	skipDirs = []string{"node_modules", "vendor", "testdata", "third_party", ".git"}
)

// parseDockerfile reads the facts Forgeline uses from a Dockerfile's text.
func parseDockerfile(text string) (f dockerfileFacts) {
	type stage struct {
		base  string
		arm64 bool
	}
	stages := map[string]stage{} // stage name -> the base it starts from and whether it is built for arm64
	last := ""
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if m := fromRE.FindStringSubmatch(line); m != nil {
			cur := stage{base: strings.ToLower(m[2]), arm64: strings.Contains(strings.ToLower(m[1]), "arm64")}
			if s, ok := stages[cur.base]; ok {
				cur = stage{base: s.base, arm64: cur.arm64 || s.arm64} // FROM an earlier stage: what is underneath it
			}
			if m[3] != "" {
				stages[strings.ToLower(m[3])] = cur
			}
			last, f.Arm64 = cur.base, cur.arm64
		}
		if m := exposeRE.FindStringSubmatch(line); m != nil {
			for _, p := range strings.Fields(m[1]) {
				p, _, _ = strings.Cut(p, "/")
				if n, err := strconv.Atoi(p); err == nil && n > 0 && n < 65536 && len(f.Ports) < 5 && !slices.Contains(f.Ports, n) {
					f.Ports = append(f.Ports, n)
				}
			}
		}
	}
	for _, b := range lambdaBases {
		f.Lambda = f.Lambda || strings.HasPrefix(last, b)
	}
	f.Agent = agentSDKRE.MatchString(text)
	return f
}

// suggestKind is what a block needs from its code: "service", "agent" or "lambda". "" means the block runs no image.
func suggestKind(bl blueprint.Block) string {
	switch bl.Kind {
	case blueprint.KindService:
		return "service"
	case blueprint.KindAgent:
		return "agent"
	case blueprint.KindTemplate:
		if strings.HasPrefix(bl.Template, "aws-lambda-function@") {
			return "lambda"
		}
	}
	return ""
}

func nameWords(s string) []string {
	var out []string
	for _, w := range strings.FieldsFunc(strings.ToLower(s), func(r rune) bool { return !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9') }) {
		if len(w) >= 3 && !slices.Contains(out, w) {
			out = append(out, w)
		}
	}
	return out
}

// scoreFolder says how well one Dockerfile's folder fits a block, and why.
func scoreFolder(bl blueprint.Block, f dockerfileFacts) (int, []string) {
	score, why := 0, []string{}
	where := "the folder " + f.Dir
	if f.Dir == "" {
		where = "the top of the repository"
	}
	why = append(why, "Has a Dockerfile at "+where)
	switch suggestKind(bl) {
	case "service":
		if f.Lambda {
			return -1, nil // built for Lambda: not a long-running app
		}
		switch {
		case bl.Port != 0 && slices.Contains(f.Ports, bl.Port):
			score += 3
			why = append(why, fmt.Sprintf("Listens on port %d, as %s is set to", bl.Port, bl.Name))
		case len(f.Ports) > 0 && bl.Port != 0:
			score++
			why = append(why, fmt.Sprintf("Listens on port %d; %s is set to %d", f.Ports[0], bl.Name, bl.Port))
		case len(f.Ports) > 0:
			score++
			why = append(why, fmt.Sprintf("Listens on port %d", f.Ports[0]))
		default:
			score++
			why = append(why, "Says no port: fine for a worker")
		}
	case "agent":
		if f.Lambda {
			return -1, nil
		}
		if f.Agent {
			score += 4
			why = append(why, "Uses the Bedrock AgentCore or Strands Agents SDK")
		}
		if slices.Contains(f.Ports, 8080) {
			score += 2
			why = append(why, "Listens on port 8080, as AgentCore expects")
		}
		if f.Arm64 {
			score++
			why = append(why, "Builds for arm64, as AgentCore needs")
		}
	case "lambda":
		if !f.Lambda {
			return -1, nil // Lambda runs only images built on its runtime interface
		}
		score += 5
		why = append(why, "Starts from an AWS Lambda base image")
	}
	// The block's name in the repository's name or the folder's path.
	have := nameWords(f.Repo[strings.Index(f.Repo, "/")+1:] + " " + f.Dir)
	matched := 0
	for _, w := range nameWords(bl.ID + " " + bl.Name) {
		if slices.Contains(have, w) && matched < 2 {
			matched++
			score += 2
			why = append(why, fmt.Sprintf("Its name has %q, like the block", w))
		}
	}
	if len(f.Langs) > 0 {
		why = append(why, "Written in "+strings.Join(f.Langs, " and "))
	}
	return score, why
}

// suggestBody is the block to suggest a repository for; it need not be saved.
type suggestBody struct {
	Block blueprint.Block `json:"block"`
}

func (s *Server) handleSuggestSource(w http.ResponseWriter, r *http.Request) {
	var body suggestBody
	if !readJSON(w, r, &body) {
		return
	}
	bl := body.Block
	if suggestKind(bl) == "" {
		writeError(w, http.StatusBadRequest, "block: suggestions are for apps, agents and Lambda functions, the blocks that run code from a repository")
		return
	}
	gh := s.gh()
	if gh == nil {
		writeError(w, http.StatusConflict, "GitHub is not connected; connect it under Connections to get suggestions")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), suggestTimeout)
	defer cancel()
	var all []SourceSuggestionJSON
	var problems []string
	read := 0
	seen := map[string]bool{}
	for _, rule := range gh.Allow {
		key := strings.ToLower(rule.Repo)
		if seen[key] || len(seen) >= maxSuggestRepos || !repoNameRE.MatchString(rule.Repo) {
			continue
		}
		seen[key] = true
		facts, err := s.repoDockerfiles(ctx, gh, rule, &read)
		if err != nil {
			problems = append(problems, rule.Repo+": "+err.Error())
			continue
		}
		for _, f := range facts {
			if score, why := scoreFolder(bl, f); score > 0 {
				all = append(all, SourceSuggestionJSON{Repo: f.Repo, Branch: f.Branch, Path: f.Dir, Score: score, Reasons: why})
			}
		}
	}
	sort.SliceStable(all, func(i, j int) bool { return all[i].Score > all[j].Score })
	if len(all) > maxSuggestions {
		all = all[:maxSuggestions]
	}
	if all == nil {
		all = []SourceSuggestionJSON{}
	}
	if problems == nil {
		problems = []string{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"suggestions": all, "repositories": len(seen), "problems": problems})
}

// repoDockerfiles reads one allowed repository's tree and the Dockerfiles in it. *read counts files read across
// repositories, so one request reads at most maxDockerfilesRead.
func (s *Server) repoDockerfiles(ctx context.Context, gh *GitHubConfig, rule RepoRule, read *int) ([]dockerfileFacts, error) {
	var repo struct {
		DefaultBranch string `json:"default_branch"`
	}
	if _, err := s.ghActionsCall(ctx, gh, "GET", "/repos/"+rule.Repo, nil, &repo); err != nil {
		return nil, err
	}
	// The default branch if the allow list lets it run, else the first branch the allow list names outright.
	branch := ""
	if gh.allowed(rule.Repo, repo.DefaultBranch) {
		branch = repo.DefaultBranch
	} else {
		for _, b := range rule.Branches {
			if !strings.ContainsAny(b, "*?[") {
				branch = b
				break
			}
		}
	}
	if branch == "" || !blueprint.ValidBranch(branch) {
		return nil, fmt.Errorf("no branch on the allow list to suggest")
	}
	var tree struct {
		Tree []struct {
			Path string `json:"path"`
			Type string `json:"type"`
		} `json:"tree"`
	}
	if _, err := s.ghActionsCall(ctx, gh, "GET", "/repos/"+rule.Repo+"/git/trees/"+branch+"?recursive=1", nil, &tree); err != nil {
		return nil, err
	}
	langs := map[string][]string{} // folder -> languages
	var dirs []string
	for _, e := range tree.Tree {
		if e.Type != "blob" {
			continue
		}
		dir, name := path.Split(e.Path)
		dir = strings.TrimSuffix(dir, "/")
		if l, ok := manifests[name]; ok && !slices.Contains(langs[dir], l) {
			langs[dir] = append(langs[dir], l)
		}
		if name != "Dockerfile" || strings.Count(e.Path, "/") > maxDockerfileDepth || !blueprint.ValidSourcePath(dir) {
			continue
		}
		skip := false
		for _, part := range strings.Split(dir, "/") {
			skip = skip || slices.Contains(skipDirs, part)
		}
		if !skip {
			dirs = append(dirs, dir)
		}
	}
	// The shallowest folders first: a service's Dockerfile is usually near the top.
	sort.SliceStable(dirs, func(i, j int) bool { return strings.Count(dirs[i], "/") < strings.Count(dirs[j], "/") })
	var out []dockerfileFacts
	for _, dir := range dirs {
		if len(out) >= maxDockerfilesPerRepo || *read >= maxDockerfilesRead {
			break
		}
		*read++
		file := strings.TrimPrefix(dir+"/Dockerfile", "/")
		var parts []string
		for _, p := range strings.Split(file, "/") {
			parts = append(parts, url.PathEscape(p))
		}
		var body struct {
			Encoding string `json:"encoding"`
			Content  string `json:"content"`
		}
		if _, err := s.ghActionsCall(ctx, gh, "GET", "/repos/"+rule.Repo+"/contents/"+strings.Join(parts, "/")+"?ref="+url.QueryEscape(branch), nil, &body); err != nil || body.Encoding != "base64" {
			continue
		}
		data, err := base64.StdEncoding.DecodeString(strings.ReplaceAll(body.Content, "\n", ""))
		if err != nil {
			continue
		}
		if len(data) > maxRepoFileBytes {
			data = data[:maxRepoFileBytes]
		}
		f := parseDockerfile(string(data))
		f.Repo, f.Branch, f.Dir, f.Langs = rule.Repo, branch, dir, langs[dir]
		out = append(out, f)
	}
	return out, nil
}
