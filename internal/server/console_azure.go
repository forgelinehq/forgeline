package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os/exec"
	"regexp"
	"sort"
	"strings"
	"time"
)

// Microsoft Azure, read-only (design note 025, slice S6). Forgeline uses the server user's own `az` login and
// one Azure Resource Graph query over the subscriptions it can read (or the ones an administrator chose). The
// query is a constant: nothing a person types is ever part of it. Nothing here can change an Azure resource: the
// only commands are `graph query`, `account list` and `account show`.

const (
	idAzure       = "azure"
	azureMaxSubs  = 20
	azureAssetMax = 3000 // resources read in total, in pages of 1000 (Resource Graph's default cap per query)
	azurePage     = 1000
)

// azureQuery is fixed. The columns are the ones the explorer shows.
const azureQuery = "Resources | project id, name, type, location, resourceGroup, subscriptionId, kind, tags, state=tostring(properties.provisioningState) | order by id asc"

var azureSubRE = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

type azureSettings struct {
	Enabled       bool      `json:"enabled"`
	Subscriptions []string  `json:"subscriptions,omitempty"` // empty means every subscription the login can read
	UpdatedAt     time.Time `json:"updated_at"`
	UpdatedBy     string    `json:"updated_by"`
}

// AzureSubscriptionJSON is one subscription the login can see.
type AzureSubscriptionJSON struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	State string `json:"state,omitempty"`
}

// AzureSettingsJSON is the connection as the console shows it.
type AzureSettingsJSON struct {
	Enabled       bool                    `json:"enabled"`
	Subscriptions []string                `json:"subscriptions,omitempty"`
	Available     []AzureSubscriptionJSON `json:"available,omitempty"` // for an administrator, before connecting: what the login can see
	UpdatedBy     string                  `json:"updated_by,omitempty"`
}

func (s *Server) loadAzure(ctx context.Context) azureSettings {
	var st azureSettings
	_ = s.docs.Get(ctx, kindSettings, idAzure, &st)
	return st
}

// normalizeAzureSubs checks a list of subscription ids: each a GUID, no repeats, at most azureMaxSubs.
func normalizeAzureSubs(in []string) ([]string, error) {
	seen := map[string]bool{}
	var out []string
	for _, x := range in {
		x = strings.ToLower(strings.TrimSpace(x))
		if x == "" || seen[x] {
			continue
		}
		if !azureSubRE.MatchString(x) {
			return nil, fmt.Errorf("%q is not a subscription id; it looks like 00000000-0000-0000-0000-000000000000", truncate(x, 40))
		}
		seen[x] = true
		out = append(out, x)
	}
	if len(out) > azureMaxSubs {
		return nil, fmt.Errorf("choose at most %d subscriptions, or none to read every subscription the login can see", azureMaxSubs)
	}
	sort.Strings(out)
	return out, nil
}

// az runs one read-only Azure CLI command as the server's user.
func (s *Server) az(ctx context.Context, args ...string) ([]byte, error) {
	out, errText, err := s.run(ctx, "az", append(append([]string{}, args...), "--only-show-errors", "--output", "json")...)
	if err != nil {
		return nil, azError(errText, err)
	}
	return out, nil
}

// azError turns a failed az call into something a person can act on.
func azError(stderr string, err error) error {
	var nf *exec.Error
	if errors.As(err, &nf) {
		return errors.New("the az command is not installed on the server")
	}
	low := strings.ToLower(stderr)
	switch {
	case strings.Contains(low, "az login"), strings.Contains(low, "aadsts"), strings.Contains(low, "no subscription found"), strings.Contains(low, "token has expired"), strings.Contains(low, "interactive authentication is needed"):
		return errors.New("there is no valid Azure login on the server; run `az login --use-device-code` (or sign in with a service principal) as the server's user and try again")
	case strings.Contains(low, "resource-graph"), strings.Contains(low, "is misspelled or not recognized"), strings.Contains(low, "'graph' is not in the"):
		return errors.New("the Resource Graph extension is missing from the az command; run `az extension add --name resource-graph` as the server's user")
	case strings.Contains(low, "authorizationfailed"), strings.Contains(low, "does not have authorization"), strings.Contains(low, "forbidden"):
		return errors.New("Azure denied the request: the login needs the Reader role on the subscriptions")
	case strings.Contains(low, "subscriptionnotfound"), strings.Contains(low, "invalidsubscriptionid"), strings.Contains(low, "could not be found"):
		return errors.New("Azure could not find one of those subscriptions; check the ids and that the login can see them")
	}
	if stderr == "" {
		return err
	}
	return errors.New(truncate(firstLine(stderr), 300))
}

// azureRow is one resource from the query.
type azureRow struct {
	ID, Name, Type, Location, ResourceGroup, SubscriptionID, Kind, State string
	Tags                                                                 map[string]string
}

// queryAzure reads resources in pages of azurePage until limit is reached or there are no more.
func (s *Server) queryAzure(ctx context.Context, subs []string, limit int) ([]azureRow, error) {
	var rows []azureRow
	// A hard bound on pages, so a misbehaving answer that never ends cannot loop forever.
	for page, skip := 0, 0; len(rows) < limit && page <= azureAssetMax/azurePage; page, skip = page+1, skip+azurePage {
		first := min(azurePage, limit-len(rows))
		args := []string{"graph", "query", "-q", azureQuery, "--first", fmt.Sprint(first)}
		if skip > 0 {
			args = append(args, "--skip", fmt.Sprint(skip))
		}
		if len(subs) > 0 {
			args = append(args, "--subscriptions")
			args = append(args, subs...)
		}
		out, err := s.az(ctx, args...)
		if err != nil {
			return nil, err
		}
		var got struct {
			Data []map[string]any `json:"data"`
		}
		if err := json.Unmarshal(out, &got); err != nil {
			return nil, errors.New("could not read the answer from az: " + err.Error())
		}
		for _, m := range got.Data {
			r := azureRow{ID: str(m["id"]), Name: str(m["name"]), Type: strings.ToLower(str(m["type"])), Location: str(m["location"]), ResourceGroup: str(m["resourceGroup"]),
				SubscriptionID: str(m["subscriptionId"]), Kind: str(m["kind"]), State: str(m["state"])}
			if t, ok := m["tags"].(map[string]any); ok {
				r.Tags = map[string]string{}
				for k, v := range t {
					r.Tags[k] = str(v)
				}
			}
			rows = append(rows, r)
		}
		if len(got.Data) < first {
			break
		}
	}
	return rows, nil
}

// azureKinds maps a resource type (lower case) to a service. A type not listed still appears, under "Other
// resources", so nothing in the subscriptions is hidden.
var azureKinds = []struct{ typ, id, label, category string }{
	{"microsoft.compute/virtualmachines", "vm", "Virtual machines", "Compute"},
	{"microsoft.web/sites", "app", "App Service and Functions", "Compute"},
	{"microsoft.containerservice/managedclusters", "aks", "AKS clusters", "Containers"},
	{"microsoft.containerregistry/registries", "acr", "Container registries", "Containers"},
	{"microsoft.storage/storageaccounts", "storage", "Storage accounts", "Storage"},
	{"microsoft.compute/disks", "disk", "Managed disks", "Storage"},
	{"microsoft.sql/servers", "sql", "SQL servers", "Databases"},
	{"microsoft.documentdb/databaseaccounts", "cosmos", "Cosmos DB accounts", "Databases"},
	{"microsoft.network/virtualnetworks", "vnet", "Virtual networks", "Network"},
	{"microsoft.network/networksecuritygroups", "nsg", "Network security groups", "Network"},
	{"microsoft.network/publicipaddresses", "pip", "Public IP addresses", "Network"},
	{"microsoft.network/loadbalancers", "lb", "Load balancers", "Network"},
	{"microsoft.servicebus/namespaces", "bus", "Service Bus namespaces", "Messaging"},
	{"microsoft.eventhub/namespaces", "eventhub", "Event Hubs namespaces", "Messaging"},
	{"microsoft.cognitiveservices/accounts", "ai-services", "Azure AI services and OpenAI", "AI"},
	{"microsoft.machinelearningservices/workspaces", "ml", "Machine Learning and AI Foundry workspaces", "AI"},
	{"microsoft.search/searchservices", "search", "AI Search services", "AI"},
	{"microsoft.keyvault/vaults", "vault", "Key Vaults (names only)", "Management"},
}

func azureServices(rows []azureRow) []AWSServiceJSON {
	res := map[string][]AWSResourceJSON{}
	meta := map[string]AWSServiceJSON{}
	for _, r := range rows {
		id := "other"
		for _, k := range azureKinds {
			if k.typ == r.Type {
				id = k.id
				meta[id] = AWSServiceJSON{ID: k.id, Label: k.label, Category: k.category, Resources: []AWSResourceJSON{}}
				break
			}
		}
		x := AWSResourceJSON{ID: r.Name, Name: r.Name, State: r.State, ARN: r.ID, Tags: r.Tags}
		if id == "other" {
			x.Fields = append(x.Fields, AWSFieldJSON{Label: "Type", Value: r.Type})
		}
		if r.Location != "" {
			x.Fields = append(x.Fields, AWSFieldJSON{Label: "Location", Value: r.Location})
		}
		if r.ResourceGroup != "" {
			x.Fields = append(x.Fields, AWSFieldJSON{Label: "Resource group", Value: r.ResourceGroup})
		}
		if r.Kind != "" {
			x.Fields = append(x.Fields, AWSFieldJSON{Label: "Kind", Value: r.Kind})
		}
		if r.SubscriptionID != "" {
			x.Fields = append(x.Fields, AWSFieldJSON{Label: "Subscription", Value: r.SubscriptionID})
		}
		res[id] = append(res[id], x)
	}
	out := []AWSServiceJSON{}
	seen := map[string]bool{}
	for _, k := range azureKinds {
		if seen[k.id] || len(res[k.id]) == 0 {
			continue
		}
		seen[k.id] = true
		svc := meta[k.id]
		fillService(&svc, res[k.id])
		out = append(out, svc)
	}
	if len(res["other"]) > 0 {
		svc := AWSServiceJSON{ID: "other", Label: "Other resources", Category: "Management", Resources: []AWSResourceJSON{}}
		fillService(&svc, res["other"])
		out = append(out, svc)
	}
	return out
}

func (s *Server) fetchAzure(ctx context.Context, st azureSettings) AWSInventoryJSON {
	scope := "every subscription the login can read"
	if len(st.Subscriptions) > 0 {
		scope = fmt.Sprintf("%d subscription%s", len(st.Subscriptions), map[bool]string{true: "", false: "s"}[len(st.Subscriptions) == 1])
	}
	d := AWSInventoryJSON{Enabled: true, Profile: "az login", Region: scope, FetchedAt: time.Now().UTC().Format(time.RFC3339), Services: []AWSServiceJSON{}}
	ctx, cancel := context.WithTimeout(ctx, inventoryTimeout)
	defer cancel()
	rows, err := s.queryAzure(ctx, st.Subscriptions, azureAssetMax)
	if err != nil {
		d.Error = err.Error()
		return d
	}
	d.Services = azureServices(rows)
	if len(rows) >= azureAssetMax {
		d.Notice = fmt.Sprintf("Only the first %d resources were read, so counts may be low. Choose fewer subscriptions to see the rest.", azureAssetMax)
	}
	return d
}

// azureAvailable lists the subscriptions the login can see (names and ids, never a credential).
func (s *Server) azureAvailable(ctx context.Context) []AzureSubscriptionJSON {
	out, err := s.az(ctx, "account", "list", "--query", "[].{id:id,name:name,state:state}")
	if err != nil {
		return nil
	}
	var subs []AzureSubscriptionJSON
	if json.Unmarshal(out, &subs) != nil {
		return nil
	}
	ok := subs[:0]
	for _, x := range subs {
		if azureSubRE.MatchString(x.ID) {
			x.Name, x.State = truncate(x.Name, 80), truncate(x.State, 20)
			ok = append(ok, x)
		}
	}
	sort.Slice(ok, func(i, j int) bool { return strings.ToLower(ok[i].Name) < strings.ToLower(ok[j].Name) })
	if len(ok) > 200 {
		ok = ok[:200]
	}
	return ok
}

func (s *Server) handleGetAzure(w http.ResponseWriter, r *http.Request) {
	st := s.loadAzure(r.Context())
	out := AzureSettingsJSON{Enabled: st.Enabled, Subscriptions: st.Subscriptions, UpdatedBy: st.UpdatedBy}
	if !st.Enabled && principalFrom(r.Context()).Role.AtLeast("admin") {
		ctx, cancel := context.WithTimeout(r.Context(), 8*time.Second)
		defer cancel()
		out.Available = s.azureAvailable(ctx)
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handlePutAzure(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Enabled       bool     `json:"enabled"`
		Subscriptions []string `json:"subscriptions"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	subs, err := normalizeAzureSubs(req.Subscriptions)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	st := azureSettings{Enabled: req.Enabled, Subscriptions: subs, UpdatedAt: time.Now().UTC(), UpdatedBy: principalFrom(r.Context()).Name}
	if req.Enabled {
		// Prove the login can query before saving anything.
		ctx, cancel := context.WithTimeout(r.Context(), 40*time.Second)
		defer cancel()
		if _, err := s.queryAzure(ctx, subs, 1); err != nil {
			s.auditReq(r, "integration.azure.update", "integration/azure", "refused: "+err.Error(), false)
			writeError(w, http.StatusUnprocessableEntity, err.Error())
			return
		}
	}
	if err := s.docs.Put(r.Context(), kindSettings, idAzure, st); err != nil {
		s.internalError(w, "saving the settings", err)
		return
	}
	s.azureInv.mu.Lock()
	s.azureInv.entries = nil
	s.azureInv.mu.Unlock()
	s.auditReq(r, "integration.azure.update", "integration/azure", fmt.Sprintf("enabled %v, %d subscriptions", st.Enabled, len(st.Subscriptions)), true)
	writeJSON(w, http.StatusOK, AzureSettingsJSON{Enabled: st.Enabled, Subscriptions: st.Subscriptions, UpdatedBy: st.UpdatedBy})
}

func (s *Server) handleAzureInventory(w http.ResponseWriter, r *http.Request) {
	st := s.loadAzure(r.Context())
	if !st.Enabled {
		writeJSON(w, http.StatusOK, AWSInventoryJSON{Services: []AWSServiceJSON{}})
		return
	}
	refresh := r.URL.Query().Get("refresh") == "1"
	key := strings.Join(st.Subscriptions, ",")
	s.azureInv.mu.Lock()
	if e, ok := s.azureInv.entries[key]; ok && !refresh && time.Since(e.at) < inventoryTTL {
		s.azureInv.mu.Unlock()
		writeJSON(w, http.StatusOK, e.data)
		return
	}
	s.azureInv.mu.Unlock()
	d := s.fetchAzure(r.Context(), st)
	if d.Error == "" {
		s.azureInv.mu.Lock()
		if s.azureInv.entries == nil || len(s.azureInv.entries) >= maxAWSAccounts+1 {
			s.azureInv.entries = map[string]invEntry{}
		}
		s.azureInv.entries[key] = invEntry{at: time.Now(), data: d}
		s.azureInv.mu.Unlock()
	}
	writeJSON(w, http.StatusOK, d)
}
