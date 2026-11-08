package server

import (
	"context"
	"encoding/json"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"
)

// What AWS itself says connects the resources on the map (design note 034). Two read-only sources:
//
//   - `cloudformation list-stack-resources` for each stack the inventory found: which resources a stack made.
//   - `lambda list-event-source-mappings`: which queue, table stream or stream each function reads from.
//
// Nothing is inferred from names or tags. A resource that AWS does not link is drawn without a line.

const (
	linksTimeout   = 45 * time.Second
	linksMaxStacks = 25 // stacks read per account; the rest are counted, not read
	linksMaxParts  = 500
	linksWorkers   = 4
)

// StackPartJSON is one resource a stack made, as CloudFormation names it.
type StackPartJSON struct {
	Type     string `json:"type"`               // AWS::DynamoDB::Table
	Logical  string `json:"logical"`            // the name in the template
	Physical string `json:"physical,omitempty"` // the resource's own id or name in AWS
	Status   string `json:"status,omitempty"`   // CREATE_COMPLETE, DELETE_SKIPPED, ...
}

// StackPartsJSON is what one stack made. Error is set when it could not be read.
type StackPartsJSON struct {
	Name      string          `json:"name"`
	Error     string          `json:"error,omitempty"`
	Truncated bool            `json:"truncated,omitempty"`
	Parts     []StackPartJSON `json:"parts"`
}

// LinkEndJSON names a resource the way the inventory does: its service id and its id there. Service is
// empty when the inventory does not list that kind of resource; ID is then its ARN.
type LinkEndJSON struct {
	Service string `json:"service,omitempty"`
	ID      string `json:"id"`
}

// LinkJSON is one stated link: From uses To.
type LinkJSON struct {
	From  LinkEndJSON `json:"from"`
	To    LinkEndJSON `json:"to"`
	Kind  string      `json:"kind"`            // event-source
	State string      `json:"state,omitempty"` // Enabled, Disabled, ...
}

// LinksJSON is the links view for one account.
type LinksJSON struct {
	Enabled       bool             `json:"enabled"`
	Region        string           `json:"region,omitempty"`
	FetchedAt     string           `json:"fetched_at,omitempty"`
	Stacks        []StackPartsJSON `json:"stacks"`
	StacksSkipped int              `json:"stacks_skipped,omitempty"` // stacks over the limit, not read
	Links         []LinkJSON       `json:"links"`
	LinksError    string           `json:"links_error,omitempty"`
}

type linksCache struct {
	mu      sync.Mutex
	entries map[string]linksEntry
}

type linksEntry struct {
	at   time.Time
	data LinksJSON
}

func (s *Server) handleAWSLinks(w http.ResponseWriter, r *http.Request) {
	st, acct, ok := s.accountParam(w, r)
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, s.linksFor(r.Context(), acct, st, r.URL.Query().Get("refresh") == "1"))
}

// linksFor reads an account's links, or returns the read from the last minute (like the inventory).
func (s *Server) linksFor(ctx context.Context, acct string, st awsSettings, refresh bool) LinksJSON {
	if !st.Enabled {
		return LinksJSON{Stacks: []StackPartsJSON{}, Links: []LinkJSON{}}
	}
	key := acct + "|" + st.Profile + "|" + st.Region
	s.awsLinks.mu.Lock()
	if e, ok := s.awsLinks.entries[key]; ok && !refresh && time.Since(e.at) < inventoryTTL {
		s.awsLinks.mu.Unlock()
		return e.data
	}
	s.awsLinks.mu.Unlock()

	// The stacks come from the inventory, so this costs no describe-stacks call of its own.
	inv := s.inventoryFor(ctx, acct, st, refresh)
	d := s.fetchLinks(ctx, st, stackNames(inv))
	s.awsLinks.mu.Lock()
	if s.awsLinks.entries == nil || len(s.awsLinks.entries) >= maxAWSAccounts+1 {
		s.awsLinks.entries = map[string]linksEntry{}
	}
	s.awsLinks.entries[key] = linksEntry{at: time.Now(), data: d}
	s.awsLinks.mu.Unlock()
	return d
}

// stackNames lists the stacks the inventory found, in its order.
func stackNames(inv AWSInventoryJSON) []string {
	var out []string
	for _, svc := range inv.Services {
		if svc.ID != "cfn" {
			continue
		}
		for _, r := range svc.Resources {
			// A deleted stack made nothing that is still there.
			if r.ID != "" && !strings.EqualFold(r.State, "DELETE_COMPLETE") {
				out = append(out, r.ID)
			}
		}
	}
	return out
}

func (s *Server) fetchLinks(ctx context.Context, st awsSettings, stacks []string) LinksJSON {
	d := LinksJSON{Enabled: true, Region: st.Region, FetchedAt: time.Now().UTC().Format(time.RFC3339), Stacks: []StackPartsJSON{}, Links: []LinkJSON{}}
	ctx, cancel := context.WithTimeout(ctx, linksTimeout)
	defer cancel()
	if len(stacks) > linksMaxStacks {
		d.StacksSkipped = len(stacks) - linksMaxStacks
		stacks = stacks[:linksMaxStacks]
	}
	d.Stacks = make([]StackPartsJSON, len(stacks))
	var wg sync.WaitGroup
	sem := make(chan struct{}, linksWorkers)
	for i, name := range stacks {
		wg.Add(1)
		go func() {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			d.Stacks[i] = s.stackParts(ctx, st, name)
		}()
	}
	links, err := s.eventSourceLinks(ctx, st)
	wg.Wait()
	if err != nil {
		d.LinksError = linksError(err, "lambda:ListEventSourceMappings")
	} else {
		d.Links = links
	}
	return d
}

func (s *Server) stackParts(ctx context.Context, st awsSettings, name string) StackPartsJSON {
	p := StackPartsJSON{Name: name, Parts: []StackPartJSON{}}
	raw, err := s.aws(ctx, st, "cloudformation", "list-stack-resources", "--stack-name", name)
	if err != nil {
		p.Error = linksError(err, "cloudformation:ListStackResources")
		return p
	}
	var in struct {
		StackResourceSummaries []struct {
			ResourceType, LogicalResourceId, PhysicalResourceId, ResourceStatus string
		}
	}
	if err := json.Unmarshal(raw, &in); err != nil {
		p.Error = "could not read the answer: " + err.Error()
		return p
	}
	for _, r := range in.StackResourceSummaries {
		// A part the stack deleted is gone; listing it would put a ghost on the map.
		if r.ResourceStatus == "DELETE_COMPLETE" {
			continue
		}
		if len(p.Parts) == linksMaxParts {
			p.Truncated = true
			break
		}
		p.Parts = append(p.Parts, StackPartJSON{Type: r.ResourceType, Logical: r.LogicalResourceId, Physical: r.PhysicalResourceId, Status: r.ResourceStatus})
	}
	sort.SliceStable(p.Parts, func(i, j int) bool { return p.Parts[i].Logical < p.Parts[j].Logical })
	return p
}

func (s *Server) eventSourceLinks(ctx context.Context, st awsSettings) ([]LinkJSON, error) {
	raw, err := s.aws(ctx, st, "lambda", "list-event-source-mappings")
	if err != nil {
		return nil, err
	}
	var in struct {
		EventSourceMappings []struct {
			FunctionArn, EventSourceArn, State string
		}
	}
	if err := json.Unmarshal(raw, &in); err != nil {
		return nil, err
	}
	out := []LinkJSON{}
	for _, m := range in.EventSourceMappings {
		fn := functionName(m.FunctionArn)
		if fn == "" || m.EventSourceArn == "" {
			continue
		}
		out = append(out, LinkJSON{From: LinkEndJSON{Service: "lambda", ID: fn}, To: arnEnd(m.EventSourceArn), Kind: "event-source", State: m.State})
	}
	return out, nil
}

// functionName reads a function's name from its ARN, without a version or alias:
// arn:aws:lambda:region:acct:function:name[:alias].
func functionName(arn string) string {
	parts := strings.Split(arn, ":")
	if len(parts) < 7 || parts[5] != "function" {
		return ""
	}
	return parts[6]
}

// arnEnd names an event source the way the inventory lists it: an SQS queue by its name, a DynamoDB table
// (for a table's stream) by its name. Anything else keeps its ARN and no service.
func arnEnd(arn string) LinkEndJSON {
	parts := strings.SplitN(arn, ":", 6)
	if len(parts) == 6 {
		switch parts[2] {
		case "sqs":
			return LinkEndJSON{Service: "sqs", ID: parts[5]}
		case "dynamodb":
			// table/NAME/stream/LABEL
			if seg := strings.Split(parts[5], "/"); len(seg) >= 2 && seg[0] == "table" {
				return LinkEndJSON{Service: "dynamodb", ID: seg[1]}
			}
		}
	}
	return LinkEndJSON{ID: arn}
}

// linksError names the permission when the role was refused.
func linksError(err error, perm string) string {
	m := inventoryError(err)
	if m == "this role is not allowed to list it" {
		return "this role is not allowed to read it (it needs " + perm + ")"
	}
	return m
}
