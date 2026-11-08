package server

import (
	"context"
	"encoding/json"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/forgelinehq/forgeline/internal/blueprint"
	"github.com/forgelinehq/forgeline/internal/docstore"
)

// Built by Forgeline: everything Forgeline created and keeps in a cloud account, in one place. Blueprints that were
// built (each resource their OpenTofu state records), stacks deployed through approved cloud changes, and environments
// that are still up. Only names, ids and ARNs leave the server: the state itself stays sealed. With ?check=1 each
// blueprint resource is looked up in the account's inventory (a read) and marked as still there or missing.

// BuiltResource is one resource a blueprint's state records.
type BuiltResource struct {
	Address string `json:"address"` // aws_sqs_queue.jobs
	Type    string `json:"type"`
	Block   string `json:"block,omitempty"` // the blueprint block it belongs to; empty for shared pieces
	Name    string `json:"name,omitempty"`  // its name in AWS, when it has one
	ID      string `json:"id,omitempty"`
	ARN     string `json:"arn,omitempty"`
	Present *bool  `json:"present,omitempty"` // with ?check=1: found in the inventory; absent when it cannot be checked
}

// BuiltBlueprint is a blueprint that was built, with what it made.
type BuiltBlueprint struct {
	ID         string            `json:"id"`
	Name       string            `json:"name"`
	Account    string            `json:"account"`
	Region     string            `json:"region"`
	AWSAccount string            `json:"aws_account"`
	Applies    int               `json:"applies"`
	UpdatedAt  time.Time         `json:"updated_at"`
	UpdatedBy  string            `json:"updated_by"`
	Change     *BuiltChange      `json:"change,omitempty"` // the last apply
	Blocks     []blueprint.Block `json:"blocks"`
	Links      []blueprint.Link  `json:"links"`
	Resources  []BuiltResource   `json:"resources"`
	Error      string            `json:"error,omitempty"`
}

// BuiltChange is the approved change that made or last changed something.
type BuiltChange struct {
	ID         string     `json:"id"`
	Title      string     `json:"title"`
	ProposedBy string     `json:"proposed_by"`
	DecidedBy  string     `json:"decided_by"`
	FinishedAt *time.Time `json:"finished_at,omitempty"`
}

// BuiltStack is a stack deployed through an approved cloud change and not deleted since.
type BuiltStack struct {
	Stack   string      `json:"stack"`
	Account string      `json:"account"` // the account's name in the console; empty is the primary
	Region  string      `json:"region"`
	Change  BuiltChange `json:"change"`
}

// BuiltEnvironment is an environment that is still up.
type BuiltEnvironment struct {
	ID         string     `json:"id"`
	Name       string     `json:"name"`
	Stack      string     `json:"stack"`
	Template   string     `json:"template"`
	Owner      string     `json:"owner"`
	Status     string     `json:"status"`
	MonthlyUSD float64    `json:"monthly_usd"`
	ExpiresAt  *time.Time `json:"expires_at,omitempty"`
}

// BuiltJSON is the whole page.
type BuiltJSON struct {
	Blueprints   []BuiltBlueprint   `json:"blueprints"`
	Stacks       []BuiltStack       `json:"stacks"`
	Environments []BuiltEnvironment `json:"environments"`
	Checked      bool               `json:"checked"`
	CheckedAt    string             `json:"checked_at,omitempty"`
}

func changeOf(c CloudChange) BuiltChange {
	return BuiltChange{ID: c.ID, Title: c.Title, ProposedBy: c.ProposedBy, DecidedBy: c.DecidedBy, FinishedAt: c.FinishedAt}
}

func (s *Server) handleBuilt(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	out := BuiltJSON{Blueprints: []BuiltBlueprint{}, Stacks: []BuiltStack{}, Environments: []BuiltEnvironment{}}
	changes, err := s.loadChanges(ctx, 0)
	if err != nil {
		s.internalError(w, "reading the cloud changes", err)
		return
	}
	sort.Slice(changes, func(i, j int) bool { return finished(changes[i]).Before(finished(changes[j])) })

	// Blueprints: each one with a saved state.
	docs, err := s.docs.List(ctx, kindBPState, docstore.ListOptions{})
	if err != nil {
		s.internalError(w, "reading what blueprints built", err)
		return
	}
	states, _ := docstore.Decode[blueprintState](docs)
	for _, st := range states {
		b := BuiltBlueprint{ID: st.ID, AWSAccount: st.AWSAccount, Region: st.Region, Applies: st.Serial, UpdatedAt: st.UpdatedAt, UpdatedBy: st.UpdatedBy,
			Blocks: []blueprint.Block{}, Links: []blueprint.Link{}, Resources: []BuiltResource{}}
		if bp, err := s.loadBlueprint(ctx, st.ID); err == nil {
			b.Name, b.Account, b.Blocks, b.Links = bp.Name, bp.Target.Account, bp.Blocks, bp.Links
		} else {
			b.Name, b.Error = st.ID, "the blueprint's draft is missing"
		}
		for _, c := range changes {
			if c.Kind == kindBlueprintApply && c.Blueprint == st.ID && c.Status == stSucceeded {
				bc := changeOf(c)
				b.Change = &bc
			}
		}
		text, err := s.vault.Open("blueprint-state/"+st.ID, st.Sealed)
		if err != nil {
			b.Error = "the saved state could not be opened"
		} else {
			b.Resources = stateResources(text, b.Blocks)
		}
		out.Blueprints = append(out.Blueprints, b)
	}
	sort.Slice(out.Blueprints, func(i, j int) bool { return out.Blueprints[i].UpdatedAt.After(out.Blueprints[j].UpdatedAt) })

	// Stacks: deployed through an approved change and not deleted by a later one.
	live := map[string]BuiltStack{}
	for _, c := range changes {
		if c.Status != stSucceeded {
			continue
		}
		switch c.Kind {
		case kindStackDeploy:
			live[c.target()] = BuiltStack{Stack: c.Stack, Account: c.ConnName, Region: c.Region, Change: changeOf(c)}
		case kindStackDelete:
			delete(live, c.target())
		}
	}
	for _, st := range live {
		out.Stacks = append(out.Stacks, st)
	}
	sort.Slice(out.Stacks, func(i, j int) bool { return out.Stacks[i].Stack < out.Stacks[j].Stack })

	// Environments that are still up.
	if envs, err := s.listEnvs(ctx); err == nil {
		for _, e := range envs {
			if e.live() && e.Status != envRequested {
				out.Environments = append(out.Environments, BuiltEnvironment{ID: e.ID, Name: e.Name, Stack: e.Stack, Template: e.Template, Owner: e.Owner, Status: e.Status, MonthlyUSD: e.MonthlyUSD, ExpiresAt: e.ExpiresAt})
			}
		}
	}

	if r.URL.Query().Get("check") == "1" {
		s.checkBuilt(ctx, &out)
	}
	writeJSON(w, http.StatusOK, out)
}

func finished(c CloudChange) time.Time {
	if c.FinishedAt != nil {
		return *c.FinishedAt
	}
	return c.ProposedAt
}

// stateResources reads the managed resources from an OpenTofu state: address, type, and only the name, id and ARN.
// The stand-in's state (a list of addresses) is read too.
func stateResources(text string, blocks []blueprint.Block) []BuiltResource {
	known := map[string]bool{}
	for _, b := range blocks {
		known[b.ID] = true
	}
	blockOf := func(name string) string {
		id, _, _ := strings.Cut(name, "_")
		if known[id] {
			return id
		}
		return ""
	}
	var st struct {
		Resources []struct {
			Mode, Type, Name string
			Instances        []struct {
				Attributes map[string]any `json:"attributes"`
			}
		}
		Addresses []string
	}
	out := []BuiltResource{}
	if json.Unmarshal([]byte(text), &st) != nil {
		return out
	}
	str := func(m map[string]any, k string) string { v, _ := m[k].(string); return v }
	for _, r := range st.Resources {
		if r.Mode != "managed" {
			continue
		}
		res := BuiltResource{Address: r.Type + "." + r.Name, Type: r.Type, Block: blockOf(r.Name)}
		if len(r.Instances) > 0 {
			a := r.Instances[0].Attributes
			res.ID, res.ARN, res.Name = str(a, "id"), str(a, "arn"), str(a, "name")
			if res.Name == "" {
				res.Name = str(a, "bucket")
			}
		}
		out = append(out, res)
	}
	for _, addr := range st.Addresses {
		typ, name, _ := strings.Cut(addr, ".")
		out = append(out, BuiltResource{Address: addr, Type: typ, Block: blockOf(name)})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Address < out[j].Address })
	return out
}

// Which inventory service lists a resource type, and which of its values the inventory uses as the id or name.
var builtInventory = map[string]string{
	"aws_sqs_queue": "sqs", "aws_dynamodb_table": "dynamodb", "aws_s3_bucket": "s3", "aws_cloudwatch_log_group": "logs",
	"aws_lb": "elb", "aws_ecs_cluster": "ecs", "aws_ecs_service": "ecs-services", "aws_security_group": "sg",
}

// checkBuilt marks each blueprint resource the inventory can list as present or missing.
func (s *Server) checkBuilt(ctx context.Context, out *BuiltJSON) {
	out.Checked, out.CheckedAt = true, time.Now().UTC().Format(time.RFC3339)
	invs := map[string]map[string]bool{} // account -> "svc|id or name" -> true
	for i := range out.Blueprints {
		b := &out.Blueprints[i]
		have, ok := invs[b.Account]
		if !ok {
			have = map[string]bool{}
			if st, err := s.awsFor(ctx, b.Account); err == nil && st.Enabled {
				inv := s.inventoryFor(ctx, b.Account, st, false)
				if inv.Error == "" {
					for _, svc := range inv.Services {
						if svc.Error != "" {
							continue
						}
						have[svc.ID+"|listed"] = true
						for _, r := range svc.Resources {
							for _, k := range []string{r.ID, r.Name, r.ARN} {
								if k != "" {
									have[svc.ID+"|"+k] = true
								}
							}
						}
					}
				}
			}
			invs[b.Account] = have
		}
		for j := range b.Resources {
			res := &b.Resources[j]
			svc, ok := builtInventory[res.Type]
			if !ok || !have[svc+"|listed"] || (res.ID == "" && res.Name == "" && res.ARN == "") {
				continue // the inventory does not list this kind, or could not, or the state names nothing to look for
			}
			found := have[svc+"|"+res.ID] || have[svc+"|"+res.Name] || have[svc+"|"+res.ARN]
			res.Present = &found
		}
	}
}
