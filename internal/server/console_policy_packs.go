package server

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/forgelinehq/forgeline/internal/docstore"
	"github.com/forgelinehq/forgeline/internal/guardrail"
)

// Policy packs (design note 040, step 6). An administrator turns on rule sets on top of the built-in guardrails:
// Trivy or Checkov for known misconfigurations, Conftest for the organization's own Rego. Every preview runs each
// pack as a container with no network, a read-only file system and no capabilities, on a cut-down copy of the plan
// (guardrail.Reduce: no sensitive value, no earlier state). A pack that cannot be run or read denies the plan.

const (
	idPolicyPacks  = "policy-packs"
	packTimeout    = 4 * time.Minute // includes pulling the image the first time
	packMaxOutput  = 8 << 20
	packMemory     = "1g"
	packFailedRule = "pack-failed"
)

type policyPacks struct {
	Packs     []guardrail.Pack `json:"packs"`
	UpdatedAt time.Time        `json:"updated_at"`
	UpdatedBy string           `json:"updated_by"`
}

// packRunner runs a pack's image with dir mounted read-only at guardrail.PackDir. Tests replace it.
type packRunner func(ctx context.Context, image, dir string, args []string) (stdout []byte, stderr string, exit int, err error)

func (s *Server) loadPolicyPacks(ctx context.Context) (policyPacks, error) {
	var p policyPacks
	err := s.docs.Get(ctx, kindSettings, idPolicyPacks, &p)
	if errors.Is(err, docstore.ErrNotFound) {
		err = nil
	}
	if p.Packs == nil {
		p.Packs = []guardrail.Pack{}
	}
	return p, err
}

func (s *Server) handleGetPolicyPacks(w http.ResponseWriter, r *http.Request) {
	p, err := s.loadPolicyPacks(r.Context())
	if err != nil {
		s.internalError(w, "reading the policy packs", err)
		return
	}
	tools := []guardrail.Tool{guardrail.Tools[guardrail.ToolTrivy], guardrail.Tools[guardrail.ToolCheckov], guardrail.Tools[guardrail.ToolConftest]}
	writeJSON(w, http.StatusOK, map[string]any{"packs": p.Packs, "updated_at": p.UpdatedAt, "updated_by": p.UpdatedBy, "tools": tools, "builtin": guardrail.Version})
}

func (s *Server) handlePutPolicyPacks(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Packs []guardrail.Pack `json:"packs"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	if len(req.Packs) > guardrail.MaxPacks {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("packs: at most %d", guardrail.MaxPacks))
		return
	}
	seen := map[string]bool{}
	for i, p := range req.Packs {
		at := fmt.Sprintf("packs[%d]", i)
		if err := p.Validate(); err != nil {
			writeError(w, http.StatusBadRequest, at+"."+err.Error())
			return
		}
		if seen[p.ID] {
			writeError(w, http.StatusBadRequest, fmt.Sprintf("%s.id: %q is used twice", at, p.ID))
			return
		}
		seen[p.ID] = true
	}
	ctx := r.Context()
	cur, err := s.loadPolicyPacks(ctx)
	if err != nil {
		writeError(w, http.StatusConflict, "the stored policy packs cannot be read, so they will not be changed over; fix or remove them first")
		return
	}
	next := policyPacks{Packs: req.Packs, UpdatedAt: time.Now().UTC(), UpdatedBy: principalFrom(ctx).Name}
	if next.Packs == nil {
		next.Packs = []guardrail.Pack{}
	}
	if err := s.docs.Put(ctx, kindSettings, idPolicyPacks, next); err != nil {
		s.internalError(w, "saving the policy packs", err)
		return
	}
	s.auditReq(r, "guardrail.packs", "settings/policy-packs", truncate("now "+packsSummary(next.Packs)+"; was "+packsSummary(cur.Packs), 1500), true)
	writeJSON(w, http.StatusOK, next)
}

// packsSummary names each pack for the audit log; a Conftest pack's rules are counted, not copied.
func packsSummary(ps []guardrail.Pack) string {
	if len(ps) == 0 {
		return "none"
	}
	var out []string
	for _, p := range ps {
		state := "off"
		if p.Enabled {
			state = "on"
		}
		s := fmt.Sprintf("%s (%s, %s", p.ID, p.Tool, state)
		if p.DenyAt != "" {
			s += ", denies from " + p.DenyAt
		}
		if len(p.Deny) > 0 {
			s += ", denies " + strings.Join(p.Deny, " ")
		}
		if len(p.Skip) > 0 {
			s += ", skips " + strings.Join(p.Skip, " ")
		}
		if len(p.Rules) > 0 {
			size := 0
			for _, body := range p.Rules {
				size += len(body)
			}
			s += fmt.Sprintf(", %d rule file(s), %d bytes", len(p.Rules), size)
		}
		out = append(out, s+")")
	}
	return strings.Join(out, ", ")
}

// runPacks runs every pack that is turned on against a plan (`tofu show -json`) and returns what they found, the
// record of each run, and when the packs were last changed (a preview is only good for the packs it was checked by).
func (s *Server) runPacks(ctx context.Context, show []byte) ([]guardrail.Finding, []guardrail.PackRun, time.Time, error) {
	set, err := s.loadPolicyPacks(ctx)
	if err != nil {
		return nil, nil, time.Time{}, errors.New("the policy packs could not be read, so the plan cannot be checked")
	}
	var on []guardrail.Pack
	for _, p := range set.Packs {
		if p.Enabled {
			on = append(on, p)
		}
	}
	if len(on) == 0 {
		return nil, nil, set.UpdatedAt, nil
	}
	reduced, err := guardrail.Reduce(show)
	if err != nil {
		return nil, nil, set.UpdatedAt, err
	}
	addresses := guardrail.Addresses(show)
	run := s.packRun
	if run == nil {
		run = dockerPack
	}
	var findings []guardrail.Finding
	var runs []guardrail.PackRun
	for _, p := range on {
		tool := guardrail.Tools[p.Tool]
		rec := guardrail.PackRun{ID: p.ID, Name: p.Name, Tool: p.Tool, Version: tool.Version, Image: tool.Image}
		fs, skipped, err := s.runPack(ctx, run, p, tool, reduced, addresses)
		if err != nil {
			rec.Error = err.Error()
			fs = []guardrail.Finding{{Rule: packFailedRule, Level: guardrail.Deny, Address: p.Name,
				Text: "This policy pack could not be run, so the plan is not checked by it: " + rec.Error}}
		}
		rec.Skipped = skipped
		for _, f := range fs {
			if f.Level == guardrail.Deny {
				rec.Denies++
			} else {
				rec.Warnings++
			}
		}
		findings, runs = append(findings, fs...), append(runs, rec)
	}
	return findings, runs, set.UpdatedAt, nil
}

func (s *Server) runPack(ctx context.Context, run packRunner, p guardrail.Pack, tool guardrail.Tool, reduced []byte, addresses []string) ([]guardrail.Finding, int, error) {
	base := filepath.Join(s.cfg.DataDir, "tmp")
	if err := os.MkdirAll(base, 0o700); err != nil {
		return nil, 0, err
	}
	dir, err := os.MkdirTemp(base, "pack-")
	if err != nil {
		return nil, 0, err
	}
	defer os.RemoveAll(dir)
	if err := os.WriteFile(filepath.Join(dir, guardrail.PackPlan), reduced, 0o600); err != nil {
		return nil, 0, err
	}
	if len(p.Rules) > 0 {
		if err := os.Mkdir(filepath.Join(dir, guardrail.PackPolicy), 0o700); err != nil {
			return nil, 0, err
		}
		for name, body := range p.Rules {
			if filepath.Base(name) != name {
				return nil, 0, fmt.Errorf("refused rule file %q", name)
			}
			if err := os.WriteFile(filepath.Join(dir, guardrail.PackPolicy, name), []byte(body), 0o600); err != nil {
				return nil, 0, err
			}
		}
	}
	pctx, cancel := context.WithTimeout(ctx, packTimeout)
	defer cancel()
	out, errText, exit, err := run(pctx, tool.Image, dir, p.Command())
	if pctx.Err() != nil {
		return nil, 0, fmt.Errorf("%s did not finish in %s", tool.Label, packTimeout)
	}
	if err != nil {
		return nil, 0, fmt.Errorf("%s could not be started (Docker is needed): %s", tool.Label, tail(strings.TrimSpace(errText+" "+err.Error()), 300))
	}
	fs, skipped, err := p.Findings(out, exit, addresses)
	if err != nil {
		return nil, 0, fmt.Errorf("%s: %s", err.Error(), tail(errText, 300))
	}
	return fs, skipped, nil
}

// packDockerArgs are the arguments after `docker`. The container gets no network, a read-only root, no capabilities,
// the server's own user (so it can read the private directory and nothing else of the host's), and limits on memory,
// processes and time. dir is mounted read-only.
func packDockerArgs(name, image, dir string, args []string) []string {
	out := []string{"run", "--rm", "--name", name,
		"--network", "none", "--read-only", "--tmpfs", "/tmp:rw,size=256m",
		"--cap-drop", "ALL", "--security-opt", "no-new-privileges",
		"--user", strconv.Itoa(os.Getuid()) + ":" + strconv.Itoa(os.Getgid()),
		"--memory", packMemory, "--cpus", "2", "--pids-limit", "256",
		"-e", "HOME=/tmp", "-w", "/tmp",
		"-v", dir + ":" + guardrail.PackDir + ":ro", image}
	return append(out, args...)
}

func dockerPack(ctx context.Context, image, dir string, args []string) ([]byte, string, int, error) {
	name := "forgeline-pack-" + randomID()[:12]
	cmd := exec.CommandContext(ctx, "docker", packDockerArgs(name, image, dir, args)...)
	var out, errb limitedBuffer
	out.max, errb.max = packMaxOutput, 64<<10
	cmd.Stdout, cmd.Stderr = &out, &errb
	cmd.WaitDelay = 5 * time.Second
	err := cmd.Run()
	if ctx.Err() != nil {
		// Killing the CLI does not stop the container, so remove it by name.
		exec.Command("docker", "rm", "-f", name).Run()
		return nil, "", 0, ctx.Err()
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		// 125 to 127 are Docker's own failures (no daemon, no image, cannot start), not the tool's answer.
		if code := exitErr.ExitCode(); code < 125 {
			return out.b.Bytes(), strings.TrimSpace(errb.b.String()), code, nil
		}
	}
	return bytes.Clone(out.b.Bytes()), strings.TrimSpace(errb.b.String()), 0, err
}

// packsLine says, for the approver, which rule sets checked the plan.
func packsLine(runs []guardrail.PackRun) string {
	if len(runs) == 0 {
		return ""
	}
	var parts []string
	for _, r := range runs {
		parts = append(parts, fmt.Sprintf("%s (%s %s): %d warning(s)", r.Name, guardrail.Tools[r.Tool].Label, r.Version, r.Warnings))
	}
	return " Policy packs: " + strings.Join(parts, "; ") + "."
}
