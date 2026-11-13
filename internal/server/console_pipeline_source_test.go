package server

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/forgelinehq/forgeline/internal/executor"
)

const sourcePipeline = "version: v1alpha1\nname: from-repo\nsteps:\n  - id: look\n    image: alpine:3.24\n    run: cat README.md\n"

func TestPipelineSourceIsCheckedOutForConsoleRuns(t *testing.T) {
	gh := newFakeGitHub(t)
	gh.addCommit(ghRepo, sha1st, map[string]string{"README.md": "hello from the repository\n", "app/Dockerfile": "FROM scratch\n"})
	gh.setHead(ghRepo, "main", sha1st)
	var mu sync.Mutex
	var seen []string
	fake := &executor.Fake{Handler: func(_ context.Context, s executor.StepRun, _ io.Writer) (executor.Result, error) {
		b, _ := os.ReadFile(filepath.Join(s.Workspace, "README.md"))
		mu.Lock()
		seen = append(seen, string(b))
		mu.Unlock()
		return executor.Result{}, nil
	}}
	h := newHarness(t, fake)
	admin := h.adminBrowser("ada")
	connectGitHub(t, admin, gh, map[string]any{"rules": []map[string]any{{"repo": ghRepo, "branches": []string{"main", "release/*"}}}})
	body := func(src any) map[string]any { return map[string]any{"yaml": sourcePipeline, "source": src} }

	// Refused sources.
	for name, src := range map[string]map[string]string{
		"branch not allowed": {"repo": ghRepo, "branch": "feature"},
		"other repository":   {"repo": "acme/other", "branch": "main"},
		"a pattern":          {"repo": ghRepo, "branch": "release/*"},
		"a full ref":         {"repo": ghRepo, "branch": "refs/heads/main"},
		"climbing":           {"repo": ghRepo, "branch": "../main"},
		"climbing a pattern": {"repo": ghRepo, "branch": "release/.."}, // release/* would match it
		"bad repo":           {"repo": "acme", "branch": "main"},
	} {
		if msg := string(admin.expect(400, "POST", "/v1/pipelines", body(src))); !strings.Contains(msg, "source") {
			t.Errorf("%s: %s", name, msg)
		}
	}
	admin.expect(201, "POST", "/v1/pipelines", body(map[string]string{"repo": ghRepo, "branch": "main"}))
	if !hasAudit(t, admin, "pipeline.create") || !strings.Contains(string(admin.expect(200, "GET", "/v1/audit?action=pipeline.create", nil)), "source "+ghRepo+" branch main") {
		t.Error("the source is not in the audit log")
	}

	// A run downloads the branch's newest commit and records it.
	var run RunJSON
	json.Unmarshal(admin.expect(202, "POST", "/v1/pipelines/from-repo/run", nil), &run)
	if run.Source == nil || run.Source.Repo != ghRepo || run.Source.Ref != "refs/heads/main" || run.Source.SHA != sha1st || run.Trigger != "ui" {
		t.Fatalf("run = %+v", run)
	}
	h.waitStatus(run.ID, "succeeded")
	mu.Lock()
	if len(seen) != 1 || !strings.Contains(seen[0], "hello from the repository") {
		t.Fatalf("the step did not see the repository's files: %q", seen)
	}
	mu.Unlock()
	if a := string(admin.expect(200, "GET", "/v1/audit?action=run.start", nil)); !strings.Contains(a, "source "+ghRepo+"@"+sha1st[:7]) {
		t.Errorf("the run's source is not audited: %s", a)
	}

	// Saving the YAML without naming a source keeps it; a bad source object is refused.
	var sp0 SavedPipeline
	json.Unmarshal(admin.expect(200, "GET", "/v1/pipelines/from-repo", nil), &sp0)
	json.Unmarshal(admin.expect(200, "PUT", "/v1/pipelines/from-repo", map[string]any{"yaml": sourcePipeline + "# edited\n", "base_version": sp0.Version}), &sp0)
	if sp0.Source == nil || sp0.Source.Branch != "main" {
		t.Fatalf("a save without a source dropped it: %+v", sp0.Source)
	}
	admin.expect(400, "PUT", "/v1/pipelines/from-repo", map[string]any{"yaml": sourcePipeline, "base_version": sp0.Version, "source": map[string]string{"repo": ghRepo, "branch": "main", "token": "x"}})
	admin.expect(400, "PUT", "/v1/pipelines/from-repo", map[string]any{"yaml": sourcePipeline, "base_version": sp0.Version, "source": map[string]string{"repo": ghRepo, "branch": "feature"}})

	// The branch moves: the next run takes the new commit.
	sha2 := strings.Repeat("b", 40)
	gh.addCommit(ghRepo, sha2, map[string]string{"README.md": "second\n"})
	gh.setHead(ghRepo, "main", sha2)
	json.Unmarshal(admin.expect(202, "POST", "/v1/pipelines/from-repo/run", nil), &run)
	if run.Source.SHA != sha2 {
		t.Errorf("the second run took %s", run.Source.SHA)
	}
	h.waitStatus(run.ID, "succeeded")

	// The branch is taken off the allow list: runs refuse instead of downloading it.
	connectGitHub(t, admin, gh, map[string]any{"rules": []map[string]any{{"repo": ghRepo, "branches": []string{"release/*"}}}})
	if msg := string(admin.expect(409, "POST", "/v1/pipelines/from-repo/run", nil)); !strings.Contains(msg, "allow list") {
		t.Errorf("a run of a branch no longer allowed: %s", msg)
	}

	// A branch GitHub does not know: no run.
	connectGitHub(t, admin, gh, map[string]any{"rules": []map[string]any{{"repo": ghRepo, "branches": []string{"main", "gone"}}}})
	var sp SavedPipeline
	json.Unmarshal(admin.expect(200, "GET", "/v1/pipelines/from-repo", nil), &sp)
	admin.expect(200, "PUT", "/v1/pipelines/from-repo", map[string]any{"yaml": sourcePipeline, "base_version": sp.Version, "source": map[string]string{"repo": ghRepo, "branch": "gone"}})
	before := len(h.runs())
	admin.expect(502, "POST", "/v1/pipelines/from-repo/run", nil)
	if len(h.runs()) != before {
		t.Error("a run was created for a branch that does not exist")
	}

	// GitHub answers with something that is not a commit, or the commit cannot be downloaded: no run either.
	gh.setHead(ghRepo, "gone", "../../../widgets/tarball/"+sha1st) // never used as a path
	if msg := string(admin.expect(502, "POST", "/v1/pipelines/from-repo/run", nil)); !strings.Contains(msg, "did not return a commit") {
		t.Errorf("an answer that is not a commit: %s", msg)
	}
	gh.setHead(ghRepo, "gone", strings.Repeat("c", 40)) // no tarball for it
	if msg := string(admin.expect(502, "POST", "/v1/pipelines/from-repo/run", nil)); !strings.Contains(msg, "could not download") {
		t.Errorf("a commit that cannot be downloaded: %s", msg)
	}
	if len(h.runs()) != before {
		t.Error("a run was created without its source")
	}

	// Without a source, a run starts with an empty workspace, as before.
	json.Unmarshal(admin.expect(200, "GET", "/v1/pipelines/from-repo", nil), &sp)
	admin.expect(200, "PUT", "/v1/pipelines/from-repo", map[string]any{"yaml": sourcePipeline, "base_version": sp.Version, "source": nil})
	run = RunJSON{}
	json.Unmarshal(admin.expect(202, "POST", "/v1/pipelines/from-repo/run", nil), &run)
	if run.Source != nil {
		t.Errorf("a run without a source = %+v", run.Source)
	}
}
