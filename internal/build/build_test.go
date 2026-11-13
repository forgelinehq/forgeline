package build

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/forgelinehq/forgeline/internal/pipeline"
)

func workspace(t *testing.T) string {
	t.Helper()
	ws := t.TempDir()
	os.MkdirAll(filepath.Join(ws, "app", "docker"), 0o755)
	os.WriteFile(filepath.Join(ws, "app", "Dockerfile"), []byte("FROM scratch\n"), 0o644)
	os.WriteFile(filepath.Join(ws, "app", "docker", "Dockerfile.prod"), []byte("FROM scratch\n"), 0o644)
	return ws
}

func TestPathsStayInsideTheWorkspace(t *testing.T) {
	ws := workspace(t)
	outside := t.TempDir()
	os.WriteFile(filepath.Join(outside, "Dockerfile"), []byte("FROM scratch\n"), 0o644)
	os.Symlink(outside, filepath.Join(ws, "escape"))
	os.Symlink(filepath.Join(outside, "Dockerfile"), filepath.Join(ws, "app", "Linked"))
	os.Symlink("/etc/passwd", filepath.Join(ws, "app", "Passwd"))
	// A context outside whose Dockerfile links back inside: only the context check catches it.
	back := t.TempDir()
	os.Symlink(filepath.Join(ws, "app", "Dockerfile"), filepath.Join(back, "Dockerfile"))
	os.Symlink(back, filepath.Join(ws, "out"))

	for name, b := range map[string]pipeline.Build{
		"default dockerfile":   {Context: "app"},
		"nested dockerfile":    {Context: "app", Dockerfile: "docker/Dockerfile.prod"},
		"workspace as context": {Dockerfile: "app/Dockerfile"},
	} {
		if _, _, err := Paths(ws, b); err != nil {
			t.Errorf("%s: %v", name, err)
		}
	}
	for name, b := range map[string]pipeline.Build{
		"context through a link":    {Context: "escape"},
		"context out, file back in": {Context: "out"},
		"dockerfile through a link": {Context: "app", Dockerfile: "Linked"},
		"dockerfile to /etc":        {Context: "app", Dockerfile: "Passwd"},
		"context escapes":           {Context: "../x"},
		"absolute context":          {Context: outside},
		"no such context":           {Context: "nope"},
		"no dockerfile":             {Context: "app/docker"},
		"dockerfile is a directory": {Context: "app", Dockerfile: "docker"},
	} {
		if _, _, err := Paths(ws, b); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

func TestArgsGrantNothing(t *testing.T) {
	b := pipeline.Build{Platform: "linux/amd64", Network: pipeline.NetworkNone, Args: map[string]string{"B": "2", "A": "1"}}
	only := strings.Join(Args(Request{Build: b}, "/ws/app", "/ws/app/Dockerfile", "/tmp/m.json"), " ")
	for _, want := range []string{"buildx build", "--platform linux/amd64", "--network none", "--build-arg A=1 --build-arg B=2", "--output type=cacheonly", "--provenance false"} {
		if !strings.Contains(only, want) {
			t.Errorf("build-only args lack %q: %s", want, only)
		}
	}
	push := strings.Join(Args(Request{Build: pipeline.Build{}, Refs: []string{"r/web:abc", "r/web:latest"}, AuthHost: "r", Auth: "c2VjcmV0"}, "/ws", "/ws/Dockerfile", "/tmp/m.json"), " ")
	for _, want := range []string{"--tag r/web:abc --tag r/web:latest", "--push", "--no-cache"} {
		if !strings.Contains(push, want) {
			t.Errorf("push args lack %q: %s", want, push)
		}
	}
	for _, args := range []string{only, push} {
		for _, bad := range []string{"--allow", "--secret", "--ssh", "-v ", "--mount", "c2VjcmV0", "--network host", "--network default", "insecure"} {
			if strings.Contains(args, bad) {
				t.Errorf("args contain %q: %s", bad, args)
			}
		}
	}
	if strings.Contains(push, "cacheonly") || strings.Contains(only, "--push") {
		t.Error("push and build-only outputs are mixed up")
	}
}

// fakeDocker writes a docker stand-in that records its arguments, environment and client configuration, and writes
// the metadata file a push leaves.
func fakeDocker(t *testing.T, digest string) (docker, record string) {
	t.Helper()
	dir := t.TempDir()
	record = filepath.Join(dir, "record")
	docker = filepath.Join(dir, "docker")
	script := `#!/bin/sh
rec="` + record + `"
printf '%s\n' "$@" > "$rec.args"
env > "$rec.env"
cp "$DOCKER_CONFIG/config.json" "$rec.config"
ls -ld "$DOCKER_CONFIG/config.json" | cut -c1-10 > "$rec.mode"
echo "$DOCKER_CONFIG" > "$rec.dir"
meta=""; prev=""
for a in "$@"; do [ "$prev" = "--metadata-file" ] && meta="$a"; prev="$a"; done
[ -n "` + digest + `" ] && printf '{"containerimage.digest":"%s"}' "` + digest + `" > "$meta"
echo "#1 building"
exit 0
`
	if err := os.WriteFile(docker, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return docker, record
}

func TestRunKeepsTheCredentialPrivateAndRemovesIt(t *testing.T) {
	ws := workspace(t)
	t.Setenv("AWS_SECRET_ACCESS_KEY", "server-own-secret")
	digest := "sha256:" + strings.Repeat("ab", 32)
	docker, rec := fakeDocker(t, digest)
	var logs strings.Builder
	res, err := Run(context.Background(), Request{Docker: docker, Workspace: ws, Build: pipeline.Build{Context: "app"},
		Refs: []string{"123456789012.dkr.ecr.ap-south-1.amazonaws.com/web:abc"}, AuthHost: "123456789012.dkr.ecr.ap-south-1.amazonaws.com", Auth: "QVdTOnRvcHNlY3JldA=="}, &logs)
	if err != nil || res.Digest != digest {
		t.Fatalf("Run = %+v, %v (logs %s)", res, err, logs.String())
	}
	read := func(s string) string { b, _ := os.ReadFile(rec + s); return string(b) }
	var conf struct {
		Auths map[string]map[string]string `json:"auths"`
	}
	json.Unmarshal([]byte(read(".config")), &conf)
	if conf.Auths["123456789012.dkr.ecr.ap-south-1.amazonaws.com"]["auth"] != "QVdTOnRvcHNlY3JldA==" || len(conf.Auths) != 1 {
		t.Errorf("client config = %s", read(".config"))
	}
	if mode := strings.TrimSpace(read(".mode")); mode != "-rw-------" {
		t.Errorf("client config mode = %s", mode)
	}
	if strings.Contains(read(".args"), "QVdTOnRvcHNlY3JldA") || strings.Contains(logs.String(), "QVdTOnRvcHNlY3JldA") {
		t.Error("the credential is on the command line or in the log")
	}
	if strings.Contains(read(".env"), "server-own-secret") {
		t.Error("the server's own environment reached the docker client")
	}
	if _, err := os.Stat(strings.TrimSpace(read(".dir"))); !os.IsNotExist(err) {
		t.Errorf("the client configuration was left behind: %v", err)
	}
}

func TestRunBuildOnlyHasNoLogins(t *testing.T) {
	ws := workspace(t)
	docker, rec := fakeDocker(t, "")
	if _, err := Run(context.Background(), Request{Docker: docker, Workspace: ws, Build: pipeline.Build{Context: "app"}}, &strings.Builder{}); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(rec + ".config"); strings.TrimSpace(string(b)) != `{"auths":{}}` {
		t.Errorf("build-only client config = %s", b)
	}
	if _, err := Run(context.Background(), Request{Docker: docker, Workspace: ws, Build: pipeline.Build{Context: "app"}, Refs: []string{"r/x:y"}}, &strings.Builder{}); err == nil {
		t.Error("a push without a credential ran")
	}
}

func TestRunRefusesAPushWithoutADigest(t *testing.T) {
	ws := workspace(t)
	docker, _ := fakeDocker(t, "")
	_, err := Run(context.Background(), Request{Docker: docker, Workspace: ws, Build: pipeline.Build{Context: "app"}, Refs: []string{"r/x:y"}, AuthHost: "r", Auth: "eA=="}, &strings.Builder{})
	if err == nil || !strings.Contains(err.Error(), "digest") {
		t.Errorf("err = %v", err)
	}
}
