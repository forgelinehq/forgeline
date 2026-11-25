package server

import (
	"context"
	"fmt"
	"strings"

	"github.com/forgelinehq/forgeline/internal/authn"
	"github.com/forgelinehq/forgeline/internal/store"
)

// Every secret a step is given, and every refusal, is in the audit log (design note 005, T13 and slice 4): which run and step, the
// secret NAMES (never the values), the network the step has, and how the run started. A step that has secrets and network access
// in the same step, the shape of a prompt-injection leak (T4), gets a second record of its own that can be searched for.

func runActor(run store.Run) Principal {
	return Principal{Name: "run " + run.ID, Role: authn.Developer}
}

// auditSecretGrant records that a step is about to be handed secrets.
func (s *Server) auditSecretGrant(ctx context.Context, run store.Run, stepID string, names []string, network string) {
	if network == "" {
		network = "egress" // the default
	}
	target := "run/" + run.ID + "/" + stepID
	detail := fmt.Sprintf("secrets %s; network %s; run started by %s", strings.Join(names, ", "), network, run.Trigger)
	_ = s.audit(ctx, runActor(run), "", "run.secret.grant", target, detail, true)
	if network != "none" {
		_ = s.audit(ctx, runActor(run), "", "run.secret.network", target, fmt.Sprintf("a step with secrets (%s) can also reach the network", strings.Join(names, ", ")), true)
	}
}

// auditSecretRefusal records that a step asked for secrets and was refused them.
func (s *Server) auditSecretRefusal(ctx context.Context, run store.Run, stepID string, names []string, why string) {
	_ = s.audit(ctx, runActor(run), "", "run.secret.refused", "run/"+run.ID+"/"+stepID,
		fmt.Sprintf("asked for %s; %s; run started by %s", strings.Join(names, ", "), truncate(why, 200), run.Trigger), false)
}
