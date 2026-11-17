package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/forgelinehq/forgeline/internal/blueprint"
	"github.com/forgelinehq/forgeline/internal/docstore"
)

// The generic resource block (design note 040, step 7). An administrator allows resource types per AWS account;
// a resource block may then use any of them, with settings checked against the AWS provider's own schema. The
// schema comes from `tofu providers schema -json` with the provider blueprints use, made once and kept in the data
// directory (it is about 17 MB from tofu, a few MB cut down).

const (
	idResourceTypes     = "resource-types"
	maxAllowedTypes     = 300
	maxSchemaOutput     = 128 << 20
	schemaFile          = "aws-resource-schemas.json"
	schemaMaxAge        = 30 * 24 * time.Hour
	maxTypeSearchAnswer = 40
)

type resourceTypes struct {
	Accounts  map[string][]string `json:"accounts"`
	UpdatedAt time.Time           `json:"updated_at"`
	UpdatedBy string              `json:"updated_by"`
}

// awsSchemas is the cut-down provider schema, made once.
type awsSchemas struct {
	Provider   string                              `json:"provider"`
	Constraint string                              `json:"constraint"` // the version constraint it was made for
	Version    string                              `json:"version"`    // the provider release, from the lock file
	MadeAt     time.Time                           `json:"made_at"`
	Schemas    map[string]blueprint.ResourceSchema `json:"schemas"`
}

type schemaCache struct {
	mu  sync.Mutex
	val *awsSchemas
}

// schemaRunner returns the output of `tofu providers schema -json` and the provider version. Tests replace it.
type schemaRunner func(ctx context.Context) (full []byte, version string, err error)

func (s *Server) loadResourceTypes(ctx context.Context) (resourceTypes, error) {
	var rt resourceTypes
	err := s.docs.Get(ctx, kindSettings, idResourceTypes, &rt)
	if errors.Is(err, docstore.ErrNotFound) {
		err = nil
	}
	if rt.Accounts == nil {
		rt.Accounts = map[string][]string{}
	}
	return rt, err
}

// resourceSchemas returns the AWS resource schemas, making them the first time (a minute or so: OpenTofu fetches
// the provider unless it is cached).
func (s *Server) resourceSchemas(ctx context.Context) (*awsSchemas, error) {
	s.schemas.mu.Lock()
	defer s.schemas.mu.Unlock()
	if v := s.schemas.val; v != nil && v.Constraint == blueprint.ProviderVersion && time.Since(v.MadeAt) < schemaMaxAge {
		return v, nil
	}
	path := filepath.Join(s.cfg.DataDir, "tools", schemaFile)
	if raw, err := os.ReadFile(path); err == nil {
		var v awsSchemas
		if json.Unmarshal(raw, &v) == nil && v.Constraint == blueprint.ProviderVersion && time.Since(v.MadeAt) < schemaMaxAge && len(v.Schemas) > 0 {
			s.schemas.val = &v
			return &v, nil
		}
	}
	run := s.schemaRun
	if run == nil {
		run = s.tofuSchema
	}
	full, version, err := run(ctx)
	if err != nil {
		return nil, fmt.Errorf("the AWS provider's schema could not be made: %w", err)
	}
	schemas, provider, err := blueprint.ParseProviderSchemas(full)
	if err != nil {
		return nil, err
	}
	v := &awsSchemas{Provider: provider, Constraint: blueprint.ProviderVersion, Version: version, MadeAt: time.Now().UTC(), Schemas: schemas}
	raw, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o600); err != nil {
		return nil, err
	}
	if err := os.Rename(tmp, path); err != nil {
		return nil, err
	}
	s.schemas.val = v
	return v, nil
}

var lockVersionRE = regexp.MustCompile(`provider "registry\.opentofu\.org/hashicorp/aws" \{\s*version\s*=\s*"([0-9.]+)"`)

// tofuSchema runs OpenTofu with only the AWS provider required, as blueprints require it, and no credentials: the
// schema needs none.
func (s *Server) tofuSchema(ctx context.Context) ([]byte, string, error) {
	if _, err := s.tofu(ctx); err != nil {
		return nil, "", err
	}
	s.tofuBin.mu.Lock()
	bin := s.tofuBin.bin
	s.tofuBin.mu.Unlock()
	dir, err := s.workdir(fmt.Sprintf(`{"terraform":{"required_providers":{"aws":{"source":%q,"version":%q}}}}`, blueprint.ProviderSource, blueprint.ProviderVersion), "", "", nil)
	if err != nil {
		return nil, "", err
	}
	defer os.RemoveAll(dir)
	env, err := s.tofuEnv(ctx, awsSettings{Region: "us-east-1"}, dir)
	if err != nil {
		return nil, "", err
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	if out, errText, err := execTofu(bin)(ctx, dir, env, "init", "-input=false", "-no-color", "-backend=false"); err != nil {
		return nil, "", fmt.Errorf("tofu init failed: %s", tail(errText+" "+string(out), 600))
	}
	cmd := exec.CommandContext(ctx, bin, "providers", "schema", "-json")
	cmd.Dir, cmd.Env = dir, env
	var out, errb limitedBuffer
	out.max, errb.max = maxSchemaOutput, 64<<10
	cmd.Stdout, cmd.Stderr = &out, &errb
	if err := cmd.Run(); err != nil {
		return nil, "", fmt.Errorf("tofu providers schema failed: %s", tail(errb.b.String(), 600))
	}
	if out.b.Len() >= maxSchemaOutput {
		return nil, "", errors.New("the provider schema is larger than expected")
	}
	lock, _ := os.ReadFile(filepath.Join(dir, ".terraform.lock.hcl"))
	version := ""
	if m := lockVersionRE.FindSubmatch(lock); m != nil {
		version = string(m[1])
	}
	return bytes.Clone(out.b.Bytes()), version, nil
}

// checkResources checks a blueprint's resource blocks against the schema and its account's allow list. A blueprint
// without one needs neither, so the schema is only made when someone uses the block.
func (s *Server) checkResources(ctx context.Context, b blueprint.Blueprint) *httpErr {
	uses := false
	for _, bl := range b.Blocks {
		uses = uses || bl.Kind == blueprint.KindResource
	}
	if !uses {
		return nil
	}
	rt, err := s.loadResourceTypes(ctx)
	if err != nil {
		return refusal(http.StatusInternalServerError, "the allowed resource types could not be read")
	}
	sc, err := s.resourceSchemas(ctx)
	if err != nil {
		return refusal(http.StatusServiceUnavailable, err.Error())
	}
	allowed := map[string]bool{}
	for _, t := range rt.Accounts[b.Target.Account] {
		allowed[t] = true
	}
	err = b.CheckResources(func(typ string) (blueprint.ResourceSchema, bool) { r, ok := sc.Schemas[typ]; return r, ok },
		func(typ string) bool { return allowed[typ] })
	if err != nil {
		return refusal(http.StatusBadRequest, err.Error())
	}
	return nil
}

func (s *Server) handleGetResourceTypes(w http.ResponseWriter, r *http.Request) {
	rt, err := s.loadResourceTypes(r.Context())
	if err != nil {
		s.internalError(w, "reading the allowed resource types", err)
		return
	}
	writeJSON(w, http.StatusOK, rt)
}

func (s *Server) handlePutResourceTypes(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Accounts map[string][]string `json:"accounts"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	ctx := r.Context()
	known := map[string]bool{primaryAccount: true}
	if added, err := s.listAWSAccounts(ctx); err == nil {
		for _, a := range added {
			known[a.ID] = true
		}
	}
	next := resourceTypes{Accounts: map[string][]string{}, UpdatedAt: time.Now().UTC(), UpdatedBy: principalFrom(ctx).Name}
	var sc *awsSchemas
	for acct, types := range req.Accounts {
		if !known[acct] {
			writeError(w, http.StatusBadRequest, fmt.Sprintf("accounts.%s: no such AWS account", acct))
			return
		}
		if len(types) > maxAllowedTypes {
			writeError(w, http.StatusBadRequest, fmt.Sprintf("accounts.%s: at most %d resource types", acct, maxAllowedTypes))
			return
		}
		seen := map[string]bool{}
		for i, t := range types {
			at := fmt.Sprintf("accounts.%s[%d]", acct, i)
			if why := blueprint.NeverAllowed(t); why != "" {
				writeError(w, http.StatusBadRequest, at+": "+why)
				return
			}
			if sc == nil {
				var err error
				if sc, err = s.resourceSchemas(ctx); err != nil {
					writeError(w, http.StatusServiceUnavailable, err.Error())
					return
				}
			}
			if _, ok := sc.Schemas[t]; !ok {
				writeError(w, http.StatusBadRequest, fmt.Sprintf("%s: the AWS provider has no resource %q", at, t))
				return
			}
			if !seen[t] {
				seen[t] = true
				next.Accounts[acct] = append(next.Accounts[acct], t)
			}
		}
		sort.Strings(next.Accounts[acct])
		if len(next.Accounts[acct]) == 0 {
			delete(next.Accounts, acct)
		}
	}
	cur, err := s.loadResourceTypes(ctx)
	if err != nil {
		writeError(w, http.StatusConflict, "the stored resource types cannot be read, so they will not be changed over; fix or remove them first")
		return
	}
	if err := s.docs.Put(ctx, kindSettings, idResourceTypes, next); err != nil {
		s.internalError(w, "saving the allowed resource types", err)
		return
	}
	s.auditReq(r, "blueprint.resource-types", "settings/resource-types", truncate(typesDiff(cur.Accounts, next.Accounts), 1500), true)
	writeJSON(w, http.StatusOK, next)
}

// typesDiff says, per account, which types were allowed and which were taken away.
func typesDiff(before, after map[string][]string) string {
	accts := map[string]bool{}
	for a := range before {
		accts[a] = true
	}
	for a := range after {
		accts[a] = true
	}
	names := make([]string, 0, len(accts))
	for a := range accts {
		names = append(names, a)
	}
	sort.Strings(names)
	var parts []string
	for _, a := range names {
		was, now := map[string]bool{}, map[string]bool{}
		for _, t := range before[a] {
			was[t] = true
		}
		for _, t := range after[a] {
			now[t] = true
		}
		var add, del []string
		for t := range now {
			if !was[t] {
				add = append(add, t)
			}
		}
		for t := range was {
			if !now[t] {
				del = append(del, t)
			}
		}
		sort.Strings(add)
		sort.Strings(del)
		if len(add) > 0 {
			parts = append(parts, a+" allows "+strings.Join(add, " "))
		}
		if len(del) > 0 {
			parts = append(parts, a+" no longer allows "+strings.Join(del, " "))
		}
	}
	if len(parts) == 0 {
		return "no change"
	}
	return strings.Join(parts, "; ")
}

// ResourceTypeJSON is one type for the canvas and the settings page.
type ResourceTypeJSON struct {
	Type    string `json:"type"`
	Label   string `json:"label"`
	Allowed bool   `json:"allowed,omitempty"`
}

// handleSearchResourceTypes finds resource types by name, for an administrator choosing what to allow.
func (s *Server) handleSearchResourceTypes(w http.ResponseWriter, r *http.Request) {
	q := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("q")))
	q = strings.ReplaceAll(q, " ", "_")
	sc, err := s.resourceSchemas(r.Context())
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, err.Error())
		return
	}
	out := []ResourceTypeJSON{}
	if q != "" {
		var names []string
		for t := range sc.Schemas {
			if strings.Contains(t, q) || strings.Contains(strings.ToLower(blueprint.ResourceLabel(t)), strings.ReplaceAll(q, "_", " ")) {
				names = append(names, t)
			}
		}
		// Shortest first: aws_sns_topic before aws_sns_topic_subscription.
		sort.Slice(names, func(i, j int) bool {
			if len(names[i]) != len(names[j]) {
				return len(names[i]) < len(names[j])
			}
			return names[i] < names[j]
		})
		for _, t := range names {
			if blueprint.NeverAllowed(t) != "" {
				continue
			}
			if out = append(out, ResourceTypeJSON{Type: t, Label: blueprint.ResourceLabel(t)}); len(out) == maxTypeSearchAnswer {
				break
			}
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"types": out, "provider_version": sc.Version, "count": len(sc.Schemas)})
}

// ResourceFieldJSON is one setting of a resource type, for the canvas form.
type ResourceFieldJSON struct {
	Name       string          `json:"name"`
	Kind       string          `json:"kind"` // string, number, bool, list (of text), map (of text), json, block
	Type       json.RawMessage `json:"type,omitempty"`
	Required   bool            `json:"required,omitempty"`
	Settable   bool            `json:"settable"`
	Why        string          `json:"why,omitempty"` // why it cannot be set
	Deprecated bool            `json:"deprecated,omitempty"`
	Nesting    string          `json:"nesting,omitempty"` // for a nested block
	MinItems   int             `json:"min_items,omitempty"`
	MaxItems   int             `json:"max_items,omitempty"`
}

// handleResourceType describes one allowed or not type: its settings (required first) and the attributes another
// resource's link can read.
func (s *Server) handleResourceType(w http.ResponseWriter, r *http.Request) {
	typ := r.PathValue("type")
	sc, err := s.resourceSchemas(r.Context())
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, err.Error())
		return
	}
	rs, ok := sc.Schemas[typ]
	if !ok {
		writeError(w, http.StatusNotFound, "the AWS provider has no resource "+typ)
		return
	}
	var fields []ResourceFieldJSON
	var outputs []string
	for n, a := range rs.Attributes {
		outputs = append(outputs, n)
		f := ResourceFieldJSON{Name: n, Type: a.Type, Required: a.Required, Settable: a.Settable(), Deprecated: a.Deprecated, Kind: fieldKind(a)}
		if n == "region" {
			f.Settable, f.Why = false, "the blueprint's region"
		} else if !f.Settable {
			if a.Sensitive || a.WriteOnly {
				f.Why = "holds a secret"
			} else {
				f.Why = "set by AWS"
			}
		}
		fields = append(fields, f)
	}
	for n, b := range rs.Blocks {
		fields = append(fields, ResourceFieldJSON{Name: n, Kind: "block", Settable: true, Required: b.MinItems > 0, Nesting: b.Nesting, MinItems: b.MinItems, MaxItems: b.MaxItems})
	}
	sort.Slice(fields, func(i, j int) bool {
		if fields[i].Required != fields[j].Required {
			return fields[i].Required
		}
		if fields[i].Settable != fields[j].Settable {
			return fields[i].Settable
		}
		return fields[i].Name < fields[j].Name
	})
	sort.Strings(outputs)
	writeJSON(w, http.StatusOK, map[string]any{"type": typ, "label": blueprint.ResourceLabel(typ), "never": blueprint.NeverAllowed(typ),
		"fields": fields, "outputs": outputs, "provider_version": sc.Version})
}

func fieldKind(a blueprint.SchemaAttr) string {
	if a.Nested {
		return "json"
	}
	var prim string
	if json.Unmarshal(a.Type, &prim) == nil {
		switch prim {
		case "string", "number", "bool":
			return prim
		}
		return "json"
	}
	var l []json.RawMessage
	if json.Unmarshal(a.Type, &l) == nil && len(l) == 2 {
		var c, e string
		json.Unmarshal(l[0], &c)
		json.Unmarshal(l[1], &e)
		if e == "string" {
			switch c {
			case "list", "set":
				return "list"
			case "map":
				return "map"
			}
		}
	}
	return "json"
}
