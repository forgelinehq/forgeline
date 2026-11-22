package server

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/forgelinehq/forgeline/internal/authn"
	"github.com/forgelinehq/forgeline/internal/pipeline"
	"github.com/forgelinehq/forgeline/internal/store"
)

// An eval step (design note 050, slice 3) runs an agent's suite as part of a pipeline, so a change to an agent is tested
// by the pipeline that ships it. It is the same run as one started in Agents › Evals: nothing it does changes anything,
// every attempt is a trace, and the step fails unless every case passes.

// evalDeniedFor says why a run may not run evals, or "". A run started by GitHub is built from a push or a pull request
// and an eval spends model tokens, so it cannot start one.
func evalDeniedFor(run store.Run) string {
	if run.Trigger == store.TriggerGitHub {
		return "runs started by GitHub may not run evals (the pipeline came from a push or pull request, and an eval spends model tokens)"
	}
	return ""
}

// evalStep returns the function the scheduler calls for an eval step of run.
func (s *Server) evalStep(run store.Run) func(context.Context, *pipeline.Step, io.Writer) error {
	return func(ctx context.Context, st *pipeline.Step, logs io.Writer) error {
		who := Principal{Name: "run " + run.ID, Role: authn.Developer}
		return s.runEvalStep(ctx, who, st.Eval, logs)
	}
}

func (s *Server) runEvalStep(ctx context.Context, who Principal, e *pipeline.Eval, logs io.Writer) error {
	say := func(format string, a ...any) { fmt.Fprintf(logs, format+"\n", a...) }
	x, err := s.loadSuite(ctx, e.Suite)
	if err != nil {
		return fmt.Errorf("no eval suite %q (see Agents › Evals)", e.Suite)
	}
	if len(x.Cases) == 0 {
		return fmt.Errorf("suite %q has no cases yet", x.ID)
	}
	if !s.aiReady(ctx) {
		return errors.New(noModelSetUp)
	}
	a, err := s.loadAgent(ctx, x.Agent)
	if err != nil || a.archived() {
		return fmt.Errorf("the agent of suite %q no longer exists", x.ID)
	}
	n := e.Version
	if n == 0 {
		n = a.Current
	}
	v, ok := a.version(n)
	if !ok {
		return fmt.Errorf("agent %s has no version %d", a.ID, n)
	}
	model := strings.TrimSpace(e.Model)
	if model != "" {
		if m := s.checkModelRef(ctx, model); m != "" {
			return errors.New("model: " + m)
		}
	}
	say("Suite %q: %d cases, each asked %d times, agent %s v%d%s", x.ID, len(x.Cases), x.Attempts, a.ID, v.N, map[bool]string{true: " on " + model, false: ""}[model != ""])
	run, status, msg := s.beginSuiteRun(ctx, who, x, a, v, model, 0)
	if msg != "" {
		return fmt.Errorf("%s (status %d)", msg, status)
	}
	defer s.evals.done(x.ID)
	s.runSuite(ctx, who, x, a, v, &run)
	if err := ctx.Err(); err != nil {
		return err
	}
	var failed []string
	for _, c := range run.Results {
		mark := "pass"
		if !c.Pass {
			mark = "FAIL"
			failed = append(failed, c.Name)
		}
		say("%s  %s (%d of %d attempts)", mark, c.Name, c.Passed, len(c.Attempts))
	}
	say("%d of %d cases passed; run %s", run.Passed, run.Total, run.ID)
	if run.Passed != run.Total {
		return fmt.Errorf("%d of %d cases failed: %s", run.Total-run.Passed, run.Total, strings.Join(failed, "; "))
	}
	return nil
}
