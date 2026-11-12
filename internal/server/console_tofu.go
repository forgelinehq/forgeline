package server

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
)

// OpenTofu for blueprints (design note 035, slice 3). Forgeline runs a pinned release, downloaded once into the
// data directory and checked against the release's published SHA-256 sums, or the binary an operator names in
// FORGELINE_TOFU. OpenTofu and the providers it downloads are third-party programs: they get a small, fixed
// environment (AWS settings, a provider cache, proxies), never the server's own variables such as its token.

const (
	tofuVersion     = "1.13.9"
	tofuMaxDownload = 200 << 20
	tofuMaxOutput   = 1 << 20
)

// The release's SHA-256 sums (tofu_1.13.9_SHA256SUMS), per platform.
var tofuSums = map[string]string{
	"linux_amd64":  "bc44eb6b06446ba83bd4072114891987f590f66cca712737a7f1147c50c539ce",
	"linux_arm64":  "04f7c93828b859d420269ed30f923ffebf09157b5d7fb95bf8d88a4d208b7fa3",
	"darwin_amd64": "ac5505dd19d896f327d0ab727d4b853413c694931a7602b31f93c3a3e9d46d00",
	"darwin_arm64": "29ba2f4c78556a8d25476e5070fe4745495ef649c4a3b827170eb7436d23feb3",
}

// tofuRunner runs tofu in a directory with exactly the environment given. Tests replace it.
type tofuRunner func(ctx context.Context, dir string, env []string, args ...string) (stdout []byte, stderr string, err error)

func execTofu(bin string) tofuRunner {
	return func(ctx context.Context, dir string, env []string, args ...string) ([]byte, string, error) {
		cmd := exec.CommandContext(ctx, bin, args...)
		cmd.Dir, cmd.Env = dir, env
		var out, errb limitedBuffer
		out.max, errb.max = tofuMaxOutput, tofuMaxOutput
		cmd.Stdout, cmd.Stderr = &out, &errb
		err := cmd.Run()
		return out.b.Bytes(), strings.TrimSpace(errb.b.String()), err
	}
}

type tofuState struct {
	mu  sync.Mutex
	bin string // the binary in use, once found
}

// tofu returns the runner, finding or downloading the binary the first time.
func (s *Server) tofu(ctx context.Context) (tofuRunner, error) {
	if s.tofuRun != nil {
		return s.tofuRun, nil
	}
	s.tofuBin.mu.Lock()
	defer s.tofuBin.mu.Unlock()
	if s.tofuBin.bin != "" {
		return execTofu(s.tofuBin.bin), nil
	}
	if p := os.Getenv("FORGELINE_TOFU"); p != "" {
		s.tofuBin.bin = p
		return execTofu(p), nil
	}
	dir := filepath.Join(s.cfg.DataDir, "tools", "tofu-"+tofuVersion)
	bin := filepath.Join(dir, "tofu")
	if _, err := os.Stat(bin); err != nil {
		if err := downloadTofu(ctx, dir); err != nil {
			return nil, fmt.Errorf("getting OpenTofu %s: %w", tofuVersion, err)
		}
	}
	s.tofuBin.bin = bin
	return execTofu(bin), nil
}

var tofuURL = func(platform string) string {
	return fmt.Sprintf("https://github.com/opentofu/opentofu/releases/download/v%s/tofu_%s_%s.zip", tofuVersion, tofuVersion, platform)
}

// downloadTofu fetches the release for this platform, refuses it unless its SHA-256 matches the pinned sum,
// and unpacks only the tofu binary.
func downloadTofu(ctx context.Context, dir string) error {
	platform := runtime.GOOS + "_" + runtime.GOARCH
	want, ok := tofuSums[platform]
	if !ok {
		return fmt.Errorf("no pinned OpenTofu for %s; install it and set FORGELINE_TOFU", platform)
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, tofuURL(platform), nil)
	if err != nil {
		return err
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("download answered %s", res.Status)
	}
	data, err := io.ReadAll(io.LimitReader(res.Body, tofuMaxDownload+1))
	if err != nil {
		return err
	}
	return installTofu(data, want, dir)
}

func installTofu(data []byte, want, dir string) error {
	if len(data) > tofuMaxDownload {
		return errors.New("the download is larger than expected")
	}
	sum := sha256.Sum256(data)
	if got := hex.EncodeToString(sum[:]); got != want {
		return fmt.Errorf("checksum mismatch (got %s, want %s); refusing to use it", got, want)
	}
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return err
	}
	for _, f := range zr.File {
		if f.Name != "tofu" {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return err
		}
		defer rc.Close()
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return err
		}
		tmp, err := os.CreateTemp(dir, "tofu-*")
		if err != nil {
			return err
		}
		if _, err := io.Copy(tmp, io.LimitReader(rc, tofuMaxDownload)); err != nil {
			tmp.Close()
			os.Remove(tmp.Name())
			return err
		}
		tmp.Close()
		if err := os.Chmod(tmp.Name(), 0o755); err != nil {
			return err
		}
		return os.Rename(tmp.Name(), filepath.Join(dir, "tofu"))
	}
	return errors.New("the release has no tofu binary")
}

// tofuEnv is everything OpenTofu sees: a fixed list from the server's environment (where AWS and proxies are
// configured), the AWS profile and region of the target, and the provider cache. Nothing else, so a provider
// never sees Forgeline's token or other secrets.
func (s *Server) tofuEnv(ctx context.Context, st awsSettings, dir string) ([]string, error) {
	pass := []string{"PATH", "HOME", "TMPDIR", "AWS_CONFIG_FILE", "AWS_SHARED_CREDENTIALS_FILE", "AWS_CA_BUNDLE", "SSL_CERT_FILE", "SSL_CERT_DIR",
		"HTTPS_PROXY", "HTTP_PROXY", "NO_PROXY", "https_proxy", "http_proxy", "no_proxy"}
	var env []string
	for _, k := range pass {
		if v, ok := os.LookupEnv(k); ok {
			env = append(env, k+"="+v)
		}
	}
	cache := filepath.Join(s.cfg.DataDir, "tools", "tofu-plugins")
	if err := os.MkdirAll(cache, 0o700); err != nil {
		return nil, err
	}
	rc := filepath.Join(dir, ".tofurc")
	if err := os.WriteFile(rc, nil, 0o600); err != nil { // no personal CLI settings
		return nil, err
	}
	env = append(env, "TF_IN_AUTOMATION=1", "TF_INPUT=0", "TF_PLUGIN_CACHE_DIR="+cache, "TF_CLI_CONFIG_FILE="+rc, "AWS_REGION="+st.Region)
	switch {
	case st.Profile == ssoProfile:
		extra, err := s.ssoEnv(ctx)
		if err != nil {
			return nil, err
		}
		env = append(env, extra...)
	case st.Profile != "":
		env = append(env, "AWS_PROFILE="+st.Profile)
	}
	return env, nil
}

// tail keeps the end of a long output, where tofu says what went wrong.
func tail(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) <= n {
		return s
	}
	return "…" + s[len(s)-n:]
}

// tofuFailure is what a person reads when tofu fails: its error output first (where the reason is), cut from the end
// if long, then the last lines of its normal output (what it had done). Keeping only the end of everything used to
// show a page of plan output and cut the error off.
func tofuFailure(errText string, out []byte) string {
	e := strings.TrimSpace(errText)
	if len(e) > 2400 {
		e = e[:2400] + " …"
	}
	o := tail(strings.TrimSpace(string(out)), 600)
	switch {
	case e == "":
		return tail(strings.TrimSpace(string(out)), 3000)
	case o == "":
		return e
	}
	return e + "\n\n" + o
}
