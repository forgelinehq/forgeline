package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os/exec"
	"regexp"
	"strings"
	"time"
)

// Google Cloud, read-only (design note 025, slice S5). Forgeline runs the server user's own `gcloud` login
// and asks Cloud Asset Inventory for every resource in one scope (a project, folder or organization) with a
// single call, the way the AWS explorer asks the AWS CLI. Nothing here can change a Google resource: the only
// commands are `asset search-all-resources`, `auth list` and `config get-value`.

const (
	idGCP        = "gcp"
	gcpAssetMax  = 3000 // resources requested in one search; the explorer shows 200 per service and reports the true count
	gcpScopeHint = "projects/my-project, folders/1234567890 or organizations/1234567890"
)

// gcpScopeRE is a Cloud Asset scope. It must start with its kind, so a value can never be taken as a flag.
var gcpScopeRE = regexp.MustCompile(`^(projects|folders|organizations)/[A-Za-z0-9][A-Za-z0-9._:-]{0,62}$`)

type gcpSettings struct {
	Enabled   bool      `json:"enabled"`
	Scope     string    `json:"scope,omitempty"`
	UpdatedAt time.Time `json:"updated_at"`
	UpdatedBy string    `json:"updated_by"`
}

// GCPSettingsJSON is the connection as the console shows it.
type GCPSettingsJSON struct {
	Enabled         bool   `json:"enabled"`
	Scope           string `json:"scope,omitempty"`
	DetectedProject string `json:"detected_project,omitempty"` // the gcloud default project, offered as a starting point (a name, never a credential)
	UpdatedBy       string `json:"updated_by,omitempty"`
}

func (s *Server) loadGCP(ctx context.Context) gcpSettings {
	var st gcpSettings
	_ = s.docs.Get(ctx, kindSettings, idGCP, &st)
	return st
}

// normalizeGCPScope accepts a bare project id and returns a full scope, or says what is wrong.
func normalizeGCPScope(in string) (string, error) {
	in = strings.TrimSpace(in)
	if in == "" {
		return "", errors.New("give a project, folder or organization, such as " + gcpScopeHint)
	}
	if !strings.Contains(in, "/") {
		in = "projects/" + in
	}
	if !gcpScopeRE.MatchString(in) {
		return "", errors.New("that is not a scope Google understands; use " + gcpScopeHint)
	}
	return in, nil
}

// gcloud runs one read-only gcloud command as the server's user. --quiet stops any prompt.
func (s *Server) gcloud(ctx context.Context, args ...string) ([]byte, error) {
	out, errText, err := s.run(ctx, "gcloud", append(append([]string{}, args...), "--quiet")...)
	if err != nil {
		return nil, gcloudError(errText, err)
	}
	return out, nil
}

// gcloudError turns a failed gcloud call into something a person can act on.
func gcloudError(stderr string, err error) error {
	var nf *exec.Error
	if errors.As(err, &nf) {
		return errors.New("the gcloud command is not installed on the server")
	}
	low := strings.ToLower(stderr)
	switch {
	case strings.Contains(low, "do not currently have an active account"), strings.Contains(low, "gcloud auth login"), strings.Contains(low, "reauthentication"), strings.Contains(low, "invalid_grant"):
		return errors.New("there is no valid Google login on the server; run `gcloud auth login` (or `gcloud auth activate-service-account`) as the server's user and try again")
	case strings.Contains(low, "service_disabled"), strings.Contains(low, "has not been used in project"), strings.Contains(low, "api has not been enabled"):
		return errors.New("the Cloud Asset API is not enabled for the project; enable cloudasset.googleapis.com and try again")
	case strings.Contains(low, "permission_denied"), strings.Contains(low, "permission denied"), strings.Contains(low, "does not have permission"), strings.Contains(low, "403"):
		return errors.New("Google denied the request: the login needs the Cloud Asset Viewer role (roles/cloudasset.viewer) on this scope, and permission to use the project's APIs")
	case strings.Contains(low, "not_found"), strings.Contains(low, "was not found"), strings.Contains(low, "404"):
		return errors.New("Google could not find that scope; check the project, folder or organization id")
	}
	if stderr == "" {
		return err
	}
	return errors.New(truncate(firstLine(stderr), 300))
}

// searchGCP asks Cloud Asset Inventory for the resources in a scope.
func (s *Server) searchGCP(ctx context.Context, scope string, limit int) ([]gcpAsset, error) {
	out, err := s.gcloud(ctx, "asset", "search-all-resources", "--scope="+scope, "--format=json",
		"--read-mask=name,assetType,displayName,location,state,labels,createTime,project", fmt.Sprintf("--limit=%d", limit))
	if err != nil {
		return nil, err
	}
	var raw []map[string]any
	if err := json.Unmarshal(out, &raw); err != nil {
		return nil, errors.New("could not read the answer from gcloud: " + err.Error())
	}
	as := make([]gcpAsset, 0, len(raw))
	for _, m := range raw {
		a := gcpAsset{Name: str(m["name"]), Type: str(m["assetType"]), Display: str(m["displayName"]), Location: str(m["location"]),
			State: str(m["state"]), Created: str(m["createTime"]), Project: str(m["project"])}
		if l, ok := m["labels"].(map[string]any); ok {
			a.Labels = map[string]string{}
			for k, v := range l {
				a.Labels[k] = str(v)
			}
		}
		if a.Name != "" || a.Type != "" {
			as = append(as, a)
		}
	}
	return as, nil
}

func str(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}

// gcpAsset is one result of the search.
type gcpAsset struct {
	Name, Type, Display, Location, State, Created, Project string
	Labels                                                 map[string]string
}

// gcpKinds maps an asset type to a service the explorer lists. A type not listed here still appears, under
// "Other resources", so nothing in the scope is hidden.
var gcpKinds = []struct{ asset, id, label, category string }{
	{"compute.googleapis.com/Instance", "gce", "Compute Engine VMs", "Compute"},
	{"run.googleapis.com/Service", "run", "Cloud Run services", "Compute"},
	{"cloudfunctions.googleapis.com/CloudFunction", "gfn", "Cloud Functions", "Compute"},
	{"cloudfunctions.googleapis.com/Function", "gfn", "Cloud Functions", "Compute"},
	{"container.googleapis.com/Cluster", "gke", "GKE clusters", "Containers"},
	{"artifactregistry.googleapis.com/Repository", "ar", "Artifact Registry repositories", "Containers"},
	{"storage.googleapis.com/Bucket", "gcs", "Cloud Storage buckets", "Storage"},
	{"compute.googleapis.com/Disk", "gdisk", "Persistent disks", "Storage"},
	{"sqladmin.googleapis.com/Instance", "sql", "Cloud SQL instances", "Databases"},
	{"bigquery.googleapis.com/Dataset", "bq", "BigQuery datasets", "Databases"},
	{"compute.googleapis.com/Network", "gvpc", "VPC networks", "Network"},
	{"compute.googleapis.com/Subnetwork", "gsubnet", "Subnets", "Network"},
	{"compute.googleapis.com/Firewall", "gfw", "Firewall rules", "Network"},
	{"compute.googleapis.com/ForwardingRule", "glb", "Forwarding rules (load balancers)", "Network"},
	{"pubsub.googleapis.com/Topic", "topic", "Pub/Sub topics", "Messaging"},
	{"aiplatform.googleapis.com/Endpoint", "vertex-endpoint", "Vertex AI endpoints", "AI"},
	{"aiplatform.googleapis.com/Model", "vertex-model", "Vertex AI models", "AI"},
	{"secretmanager.googleapis.com/Secret", "gsecret", "Secret Manager secrets (names only)", "Management"},
}

func lastSeg(s string) string {
	if i := strings.LastIndex(s, "/"); i >= 0 {
		return s[i+1:]
	}
	return s
}

// gcpServices groups the assets into the explorer's services.
func gcpServices(as []gcpAsset) []AWSServiceJSON {
	byID := map[string]*AWSServiceJSON{}
	var order []string
	other := AWSServiceJSON{ID: "other", Label: "Other resources", Category: "Management", Resources: []AWSResourceJSON{}}
	res := map[string][]AWSResourceJSON{}
	for _, a := range as {
		id, known := "", false
		for _, k := range gcpKinds {
			if k.asset == a.Type {
				id, known = k.id, true
				if byID[id] == nil {
					byID[id] = &AWSServiceJSON{ID: k.id, Label: k.label, Category: k.category, Resources: []AWSResourceJSON{}}
					order = append(order, id)
				}
				break
			}
		}
		if !known {
			id = "other"
		}
		name := a.Display
		if name == "" {
			name = lastSeg(a.Name)
		}
		r := AWSResourceJSON{ID: lastSeg(a.Name), Name: name, State: a.State, Created: a.Created, ARN: a.Name, Tags: a.Labels}
		if known {
			if a.Location != "" {
				r.Fields = append(r.Fields, AWSFieldJSON{Label: "Location", Value: a.Location})
			}
		} else {
			r.Fields = append(r.Fields, AWSFieldJSON{Label: "Type", Value: a.Type})
			if a.Location != "" {
				r.Fields = append(r.Fields, AWSFieldJSON{Label: "Location", Value: a.Location})
			}
		}
		if a.Project != "" {
			r.Fields = append(r.Fields, AWSFieldJSON{Label: "Project", Value: lastSeg(a.Project)})
		}
		res[id] = append(res[id], r)
	}
	out := []AWSServiceJSON{}
	// Keep the table's order, one service per id (two asset types can share a service).
	seen := map[string]bool{}
	for _, k := range gcpKinds {
		if seen[k.id] || byID[k.id] == nil {
			continue
		}
		seen[k.id] = true
		svc := *byID[k.id]
		fillService(&svc, res[k.id])
		out = append(out, svc)
	}
	if len(res["other"]) > 0 {
		fillService(&other, res["other"])
		out = append(out, other)
	}
	return out
}

// fetchGCP reads one scope's inventory.
func (s *Server) fetchGCP(ctx context.Context, st gcpSettings) AWSInventoryJSON {
	d := AWSInventoryJSON{Enabled: true, Profile: "gcloud login", Region: st.Scope, FetchedAt: time.Now().UTC().Format(time.RFC3339), Services: []AWSServiceJSON{}}
	ctx, cancel := context.WithTimeout(ctx, inventoryTimeout)
	defer cancel()
	as, err := s.searchGCP(ctx, st.Scope, gcpAssetMax)
	if err != nil {
		d.Error = err.Error()
		return d
	}
	d.Services = gcpServices(as)
	if len(as) >= gcpAssetMax {
		d.Notice = fmt.Sprintf("Only the first %d resources were read, so counts may be low. Choose a narrower scope to see the rest.", gcpAssetMax)
	}
	return d
}

func (s *Server) handleGetGCP(w http.ResponseWriter, r *http.Request) {
	st := s.loadGCP(r.Context())
	out := GCPSettingsJSON{Enabled: st.Enabled, Scope: st.Scope, UpdatedBy: st.UpdatedBy}
	if !st.Enabled {
		ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
		defer cancel()
		if b, _, err := s.run(ctx, "gcloud", "config", "get-value", "project", "--quiet"); err == nil {
			if p := strings.TrimSpace(string(b)); p != "" && !strings.ContainsAny(p, " \t\n()") && gcpScopeRE.MatchString("projects/"+p) {
				out.DetectedProject = p
			}
		}
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handlePutGCP(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Enabled bool   `json:"enabled"`
		Scope   string `json:"scope"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	st := gcpSettings{Enabled: req.Enabled, UpdatedAt: time.Now().UTC(), UpdatedBy: principalFrom(r.Context()).Name}
	if req.Enabled || strings.TrimSpace(req.Scope) != "" {
		scope, err := normalizeGCPScope(req.Scope)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		st.Scope = scope
	}
	if req.Enabled {
		// Prove the login can read the scope before saving anything.
		ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
		defer cancel()
		if _, err := s.searchGCP(ctx, st.Scope, 1); err != nil {
			s.auditReq(r, "integration.gcp.update", "integration/gcp", "refused: "+err.Error(), false)
			writeError(w, http.StatusUnprocessableEntity, err.Error())
			return
		}
	}
	if err := s.docs.Put(r.Context(), kindSettings, idGCP, st); err != nil {
		s.internalError(w, "saving the settings", err)
		return
	}
	s.gcpInv.mu.Lock()
	s.gcpInv.entries = nil // a new scope or a turn-off must not show the old inventory
	s.gcpInv.mu.Unlock()
	s.auditReq(r, "integration.gcp.update", "integration/gcp", fmt.Sprintf("enabled %v, scope %s", st.Enabled, st.Scope), true)
	writeJSON(w, http.StatusOK, GCPSettingsJSON{Enabled: st.Enabled, Scope: st.Scope, UpdatedBy: st.UpdatedBy})
}

// gcpInventory is the cached read of the connected scope, shared by the console tab and the assistant.
func (s *Server) gcpInventory(ctx context.Context, st gcpSettings, refresh bool) AWSInventoryJSON {
	key := st.Scope
	s.gcpInv.mu.Lock()
	if e, ok := s.gcpInv.entries[key]; ok && !refresh && time.Since(e.at) < inventoryTTL {
		s.gcpInv.mu.Unlock()
		return e.data
	}
	s.gcpInv.mu.Unlock()
	d := s.fetchGCP(ctx, st)
	if d.Error == "" {
		s.gcpInv.mu.Lock()
		if s.gcpInv.entries == nil || len(s.gcpInv.entries) >= maxAWSAccounts+1 {
			s.gcpInv.entries = map[string]invEntry{}
		}
		s.gcpInv.entries[key] = invEntry{at: time.Now(), data: d}
		s.gcpInv.mu.Unlock()
	}
	return d
}

func (s *Server) handleGCPInventory(w http.ResponseWriter, r *http.Request) {
	st := s.loadGCP(r.Context())
	if !st.Enabled {
		writeJSON(w, http.StatusOK, AWSInventoryJSON{Services: []AWSServiceJSON{}})
		return
	}
	writeJSON(w, http.StatusOK, s.gcpInventory(r.Context(), st, r.URL.Query().Get("refresh") == "1"))
}

// lookupGCPResources is the assistant's read of the connected Google Cloud scope: the same inventory as the
// console tab, as compact text. It reads nothing the tab does not show and changes nothing.
func (s *Server) lookupGCPResources(ctx context.Context, raw json.RawMessage) (string, error) {
	var a struct {
		Service string `json:"service"`
	}
	if err := decodeArgs(raw, &a); err != nil {
		return "", err
	}
	st := s.loadGCP(ctx)
	if !st.Enabled {
		return "", errors.New("Google Cloud is not connected to this Forgeline server")
	}
	d := s.gcpInventory(ctx, st, false)
	if d.Error != "" {
		return "", errors.New(d.Error)
	}
	return formatInventory(fmt.Sprintf("Google Cloud scope %s, read %s. Counts include every resource found; names are listed up to %d per service.", st.Scope, d.FetchedAt, inventoryMax), d, a.Service)
}
