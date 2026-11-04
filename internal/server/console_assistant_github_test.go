package server

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/forgelinehq/forgeline/internal/authn"
)

func TestTheAssistantReadsAllowedRepositoriesOnly(t *testing.T) {
	var hits []string
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits = append(hits, r.URL.Path)
		file := func(s string) {
			json.NewEncoder(w).Encode(map[string]any{"type": "file", "size": len(s), "encoding": "base64", "content": base64.StdEncoding.EncodeToString([]byte(s))})
		}
		switch r.URL.Path {
		case "/repos/acme/app/contents":
			json.NewEncoder(w).Encode([]map[string]any{{"name": "go.mod", "type": "file"}, {"name": "cmd", "type": "dir"}, {"name": ".env", "type": "file"}})
		case "/repos/acme/app/contents/go.mod":
			file("module acme/app\n\ngo 1.27\n")
		case "/repos/acme/app/contents/config.txt":
			file("token = ghp_abcdefghijklmnopqrstuvwxyz0123456789\n")
		case "/repos/acme/app/contents/big.txt":
			file(strings.Repeat("a", maxRepoFileBytes+500))
		case "/repos/acme/app/contents/bin.dat":
			file("ab\x00cd")
		default:
			http.NotFound(w, r)
		}
	}))
	defer api.Close()
	m := newMCPHarness(t)
	m.srv.ghCfg.Store(&GitHubConfig{Token: ghToken, APIBase: api.URL, HTTPClient: api.Client(), Allow: []RepoRule{{Repo: "acme/app", Branches: []string{"main"}}}})
	dev := Principal{Name: "dev", Role: authn.Developer}
	call := func(tool string, args map[string]string) (string, error) {
		raw, _ := json.Marshal(args)
		return m.srv.runLookup(t.Context(), dev, lookupCall{Tool: tool, Args: raw})
	}

	if out, err := call("github_repos", nil); err != nil || !strings.Contains(out, "acme/app") {
		t.Fatalf("repos = %q, %v", out, err)
	}
	// A directory is listed, without the file that holds secrets.
	out, err := call("github_files", map[string]string{"repo": "acme/app"})
	if err != nil || !strings.Contains(out, "go.mod") || !strings.Contains(out, "cmd/") || strings.Contains(out, ".env") {
		t.Fatalf("listing = %q, %v", out, err)
	}
	if out, err := call("github_files", map[string]string{"repo": "acme/app", "path": "go.mod"}); err != nil || !strings.Contains(out, "module acme/app") {
		t.Fatalf("file = %q, %v", out, err)
	}
	// A secret inside a file is hidden; a long file is cut; a binary file is refused.
	if out, _ := call("github_files", map[string]string{"repo": "acme/app", "path": "config.txt"}); strings.Contains(out, "ghp_") || !strings.Contains(out, "[hidden]") {
		t.Errorf("a secret reached the model: %q", out)
	}
	if out, _ := call("github_files", map[string]string{"repo": "acme/app", "path": "big.txt"}); len(out) > maxRepoFileBytes+300 || !strings.Contains(out, "[cut") {
		t.Errorf("a long file was not cut: %d bytes", len(out))
	}
	if _, err := call("github_files", map[string]string{"repo": "acme/app", "path": "bin.dat"}); err == nil {
		t.Error("a binary file was returned")
	}

	// Refused without reaching GitHub: other repositories, secret-looking files, odd paths.
	hits = nil
	for name, args := range map[string]map[string]string{
		"not on the allow list": {"repo": "evil/app", "path": "go.mod"},
		"a dotenv file":         {"repo": "acme/app", "path": ".env"},
		"a private key":         {"repo": "acme/app", "path": "deploy/id_rsa"},
		"a pem file":            {"repo": "acme/app", "path": "certs/server.pem"},
		"parent directory":      {"repo": "acme/app", "path": "../other/go.mod"},
		"a bad repo name":       {"repo": "acme", "path": "go.mod"},
	} {
		if _, err := call("github_files", args); err == nil {
			t.Errorf("%s was allowed", name)
		}
	}
	if len(hits) != 0 {
		t.Errorf("refused reads still reached GitHub: %v", hits)
	}
	// Viewers do not get the tools.
	if _, err := m.srv.runLookup(t.Context(), Principal{Name: "v", Role: authn.Viewer}, lookupCall{Tool: "github_files", Args: json.RawMessage(`{"repo":"acme/app"}`)}); err == nil {
		t.Error("a viewer read a repository")
	}
}
