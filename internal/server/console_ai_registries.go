package server

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/forgelinehq/forgeline/internal/agentfile"
	"github.com/forgelinehq/forgeline/internal/authn"
	"github.com/forgelinehq/forgeline/internal/docstore"
	"github.com/forgelinehq/forgeline/internal/oci"
	"github.com/forgelinehq/forgeline/internal/version"
)

// Pushing an agent to a registry (design note 046, step 3). An administrator adds a registry under Settings (its token
// goes into the vault and is never shown again). Pushing a version is a proposal: it holds the exact Docker Agent YAML
// and the exact reference, and a different administrator approves that text before anything leaves Forgeline. The
// artifact is the one `docker agent share push` makes, so `docker agent run <ref>` runs it.

const (
	kindAIRegistry   = "airegistry"
	kindAIPush       = "aipush"
	maxRegistries    = 20
	pushTTL          = 24 * time.Hour
	pushTimeout      = 90 * time.Second
	approvalKindPush = "publish" // approval rules (note 032): publishing an agent outside Forgeline
)

// AgentRegistry is a registry agents may be pushed to. Its token is in the vault under registryTokenName(ID).
type AgentRegistry struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Host      string    `json:"host"`                // ghcr.io, registry-1.docker.io, 127.0.0.1:5000
	Namespace string    `json:"namespace,omitempty"` // repositories go under it: acme/agents
	Username  string    `json:"username,omitempty"`
	HasToken  bool      `json:"has_token"`
	CreatedBy string    `json:"created_by"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedBy string    `json:"updated_by,omitempty"`
	UpdatedAt time.Time `json:"updated_at"`
}

func registryTokenName(id string) string { return "forgeline.registry." + id + ".token" }

// where is what a push to this registry is bound to: if the host or namespace changes, a waiting push is stale.
func (g AgentRegistry) where() string { return g.Host + "/" + g.Namespace }

type registryRequest struct {
	Name      string  `json:"name"`
	Host      string  `json:"host"`
	Namespace string  `json:"namespace"`
	Username  string  `json:"username"`
	Token     *string `json:"token"` // nil keeps the stored one; "" removes it
}

func (q *registryRequest) check() string {
	q.Name, q.Host, q.Namespace, q.Username = strings.TrimSpace(q.Name), strings.ToLower(strings.TrimSpace(q.Host)), strings.Trim(strings.TrimSpace(q.Namespace), "/"), strings.TrimSpace(q.Username)
	switch {
	case q.Name == "" || len(q.Name) > 60:
		return "name: 1 to 60 characters"
	case oci.CheckHost(q.Host) != nil:
		return oci.CheckHost(q.Host).Error() + " (plain http is used only for localhost and 127.0.0.1)"
	case q.Namespace != "" && oci.CheckRepository(q.Namespace) != nil:
		return "namespace: " + strings.TrimPrefix(oci.CheckRepository(q.Namespace).Error(), "repository: ")
	case len(q.Username) > 200 || strings.ContainsAny(q.Username, ":\r\n"):
		return "username: at most 200 characters, without a colon"
	case q.Token != nil && len(*q.Token) > 4096:
		return "token: at most 4096 characters"
	}
	return ""
}

func (s *Server) listRegistries(ctx context.Context) ([]AgentRegistry, error) {
	docs, err := s.docs.List(ctx, kindAIRegistry, docstore.ListOptions{})
	if err != nil {
		return nil, err
	}
	out, err := docstore.Decode[AgentRegistry](docs)
	if err != nil {
		return nil, err
	}
	for i := range out {
		_, out[i].HasToken = s.internalSecretValue(ctx, registryTokenName(out[i].ID))
	}
	sort.Slice(out, func(i, j int) bool { return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name) })
	return out, nil
}

func (s *Server) loadRegistry(ctx context.Context, id string) (AgentRegistry, error) {
	var g AgentRegistry
	if err := s.docs.Get(ctx, kindAIRegistry, id, &g); err != nil {
		return g, err
	}
	_, g.HasToken = s.internalSecretValue(ctx, registryTokenName(g.ID))
	return g, nil
}

// client is how Forgeline signs in to a registry, with the token from the vault.
func (s *Server) registryClient(ctx context.Context, g AgentRegistry) oci.Registry {
	tok, _ := s.internalSecretValue(ctx, registryTokenName(g.ID))
	return oci.Registry{Host: g.Host, Username: g.Username, Password: tok}
}

func (s *Server) handleListRegistries(w http.ResponseWriter, r *http.Request) {
	gs, err := s.listRegistries(r.Context())
	if err != nil {
		s.internalError(w, "reading the registries", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"registries": gs})
}

func (s *Server) handleSaveRegistry(w http.ResponseWriter, r *http.Request) {
	var q registryRequest
	if !readJSON(w, r, &q) {
		return
	}
	if msg := q.check(); msg != "" {
		writeError(w, http.StatusBadRequest, msg)
		return
	}
	ctx, who, id := r.Context(), principalFrom(r.Context()), r.PathValue("id")
	now := time.Now().UTC()
	var g AgentRegistry
	if id == "" {
		gs, err := s.listRegistries(ctx)
		if err != nil {
			s.internalError(w, "reading the registries", err)
			return
		}
		if len(gs) >= maxRegistries {
			writeError(w, http.StatusConflict, fmt.Sprintf("at most %d registries; remove one first", maxRegistries))
			return
		}
		g = AgentRegistry{ID: authn.NewID(), CreatedBy: who.Name, CreatedAt: now}
	} else {
		var err error
		if g, err = s.loadRegistry(ctx, id); err != nil {
			s.defError(w, "registry", err)
			return
		}
	}
	g.Name, g.Host, g.Namespace, g.Username, g.UpdatedBy, g.UpdatedAt = q.Name, q.Host, q.Namespace, q.Username, who.Name, now
	switch {
	case q.Token != nil && *q.Token != "":
		if _, _, err := s.putSecret(ctx, who.Name, registryTokenName(g.ID), *q.Token, "token of registry "+g.ID); err != nil {
			s.internalError(w, "saving the token", err)
			return
		}
	case q.Token != nil:
		s.docs.Delete(ctx, kindSecret, registryTokenName(g.ID))
	}
	g.HasToken = false
	if err := s.docs.Put(ctx, kindAIRegistry, g.ID, g); err != nil {
		s.internalError(w, "saving the registry", err)
		return
	}
	_, g.HasToken = s.internalSecretValue(ctx, registryTokenName(g.ID))
	action, status := "ai.registry.update", http.StatusOK
	if id == "" {
		action, status = "ai.registry.create", http.StatusCreated
	}
	s.auditReq(r, action, "registry/"+g.ID, fmt.Sprintf("%s (%s, token %s)", g.Name, g.where(), map[bool]string{true: "set", false: "none"}[g.HasToken]), true)
	writeJSON(w, status, g)
}

func (s *Server) handleDeleteRegistry(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	g, err := s.loadRegistry(ctx, r.PathValue("id"))
	if err != nil {
		s.defError(w, "registry", err)
		return
	}
	if err := s.docs.Delete(ctx, kindAIRegistry, g.ID); err != nil {
		s.internalError(w, "removing the registry", err)
		return
	}
	s.docs.Delete(ctx, kindSecret, registryTokenName(g.ID))
	s.auditReq(r, "ai.registry.delete", "registry/"+g.ID, g.Name+": token removed from the vault", true)
	w.WriteHeader(http.StatusNoContent)
}

// handleTestRegistry signs in and reads a repository's tags, so a wrong host or token shows before a push is approved.
func (s *Server) handleTestRegistry(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	g, err := s.loadRegistry(ctx, r.PathValue("id"))
	if err != nil {
		s.defError(w, "registry", err)
		return
	}
	repo := "forgeline-check"
	if g.Namespace != "" {
		repo = g.Namespace + "/" + repo
	}
	tctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	if err := oci.Check(tctx, s.registryClient(ctx, g), repo); err != nil {
		s.auditReq(r, "ai.registry.test", "registry/"+g.ID, "failed: "+err.Error(), false)
		writeJSON(w, http.StatusBadGateway, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	s.auditReq(r, "ai.registry.test", "registry/"+g.ID, "ok", true)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// --- pushes ---

// AgentPush is a proposal to push one agent version. Nothing leaves Forgeline until a different administrator
// approves exactly this YAML to exactly this reference.
type AgentPush struct {
	ID           string             `json:"id"`
	Agent        string             `json:"agent"`
	AgentName    string             `json:"agent_name"`
	Version      int                `json:"version"`
	Fingerprint  string             `json:"fingerprint"`
	Registry     string             `json:"registry"`
	RegistryName string             `json:"registry_name"`
	Where        string             `json:"where"` // the registry's host and namespace when this was proposed
	Repository   string             `json:"repository"`
	Tag          string             `json:"tag"`
	Ref          string             `json:"ref"`
	YAML         string             `json:"yaml"`
	LeftOut      []pushLeftOut      `json:"left_out"`
	Created      time.Time          `json:"created"` // the version's time, written into the artifact
	Hash         string             `json:"hash"`
	Reason       string             `json:"reason"`
	Status       string             `json:"status"` // proposed, pushing, pushed, failed, rejected, cancelled, expired
	ProposedBy   string             `json:"proposed_by"`
	ProposedAt   time.Time          `json:"proposed_at"`
	DecidedBy    string             `json:"decided_by,omitempty"`
	DecidedAt    *time.Time         `json:"decided_at,omitempty"`
	Note         string             `json:"note,omitempty"`
	Approvals    []approvalVote     `json:"approvals,omitempty"`
	Approval     *ApprovalStateJSON `json:"approval,omitempty"`
	Digest       string             `json:"digest,omitempty"`
	Result       string             `json:"result,omitempty"`
	FinishedAt   *time.Time         `json:"finished_at,omitempty"`
}

// pushLeftOut is a tool the pushed copy does without, and why.
type pushLeftOut struct {
	Tool string `json:"tool"`
	Why  string `json:"why"`
}

const (
	pushPushing = "pushing"
	pushPushed  = "pushed"
)

func pushHash(where, ref, yaml string) string {
	sum := sha256.Sum256([]byte(where + "\x00" + ref + "\x00" + yaml))
	return hex.EncodeToString(sum[:])
}

func (p AgentPush) expired() AgentPush {
	if p.Status == apProposed && time.Since(p.ProposedAt) > pushTTL {
		p.Status = apExpired
	}
	return p
}

func (s *Server) loadPush(ctx context.Context, id string) (AgentPush, error) {
	var p AgentPush
	if err := s.docs.Get(ctx, kindAIPush, id, &p); err != nil {
		return p, err
	}
	return p.expired(), nil
}

type pushRequest struct {
	Registry   string `json:"registry"`
	Repository string `json:"repository"` // under the registry's namespace; empty is the agent's id
	Version    int    `json:"version"`    // 0 is the live version
	Reason     string `json:"reason"`
}

// handleProposePush files a push of one agent version. It needs an administrator, like every way of publishing an
// agent (note 046, decision 1), and a different administrator approves it.
func (s *Server) handleProposePush(w http.ResponseWriter, r *http.Request) {
	var q pushRequest
	if !readJSON(w, r, &q) {
		return
	}
	ctx, who := r.Context(), principalFrom(r.Context())
	q.Reason = strings.TrimSpace(q.Reason)
	if len(q.Reason) < 10 || len(q.Reason) > 800 {
		writeError(w, http.StatusBadRequest, "reason: 10 to 800 characters; the approver reads it")
		return
	}
	a, err := s.loadAgent(ctx, r.PathValue("id"))
	if err != nil {
		s.defError(w, "agent", err)
		return
	}
	if a.archived() {
		writeError(w, http.StatusConflict, "this agent is archived; restore it first")
		return
	}
	n := q.Version
	if n == 0 {
		n = a.Current
	}
	var v *AgentVersion
	for i := range a.Versions {
		if a.Versions[i].N == n {
			v = &a.Versions[i]
		}
	}
	if v == nil {
		writeError(w, http.StatusNotFound, fmt.Sprintf("version: %s has no version %d", a.ID, n))
		return
	}
	g, err := s.loadRegistry(ctx, q.Registry)
	if errors.Is(err, docstore.ErrNotFound) {
		writeError(w, http.StatusBadRequest, "registry: no such registry; add one under Settings › Agent registries")
		return
	} else if err != nil {
		s.internalError(w, "reading the registry", err)
		return
	}
	repo := strings.Trim(strings.TrimSpace(q.Repository), "/")
	if repo == "" {
		repo = agentfile.SkillName(a.ID)
	}
	if g.Namespace != "" {
		repo = g.Namespace + "/" + repo
	}
	if err := oci.CheckRepository(repo); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	ex, msg := s.exportable(ctx, a, *v)
	if msg != "" {
		writeError(w, http.StatusConflict, msg)
		return
	}
	ex.agent.At = v.At
	body, err := agentfile.DockerYAML(ex.agent, ex.model, ex.mcps, ex.left)
	if err != nil {
		s.internalError(w, "making the Docker Agent file", err)
		return
	}
	tag := fmt.Sprintf("v%d", v.N)
	ref := g.Host + "/" + repo + ":" + tag
	p := AgentPush{ID: authn.NewID(), Agent: a.ID, AgentName: a.Name, Version: v.N, Fingerprint: v.Hash, Registry: g.ID, RegistryName: g.Name,
		Where: g.where(), Repository: repo, Tag: tag, Ref: ref, YAML: string(body), LeftOut: []pushLeftOut{}, Created: v.At, Reason: q.Reason,
		Status: apProposed, ProposedBy: who.Name, ProposedAt: time.Now().UTC()}
	for _, l := range ex.left {
		p.LeftOut = append(p.LeftOut, pushLeftOut{Tool: l.Tool, Why: l.Why})
	}
	p.Hash = pushHash(p.Where, p.Ref, p.YAML)
	if err := s.docs.Put(ctx, kindAIPush, p.ID, p); err != nil {
		s.internalError(w, "saving the proposal", err)
		return
	}
	s.auditReq(r, "ai.agent.push.propose", "agent/"+a.ID, fmt.Sprintf("v%d to %s (push %s)", v.N, ref, p.ID), true)
	p.Approval = s.approvalState(ctx, approvalKindPush, "", false, nil)
	writeJSON(w, http.StatusCreated, p)
}

func (s *Server) handleListPushes(w http.ResponseWriter, r *http.Request) {
	docs, err := s.docs.List(r.Context(), kindAIPush, docstore.ListOptions{})
	if err != nil {
		s.internalError(w, "reading the pushes", err)
		return
	}
	ps, err := docstore.Decode[AgentPush](docs)
	if err != nil {
		s.internalError(w, "reading the pushes", err)
		return
	}
	sort.SliceStable(ps, func(i, j int) bool { return ps[i].ProposedAt.After(ps[j].ProposedAt) })
	if len(ps) > 100 {
		ps = ps[:100]
	}
	for i := range ps {
		ps[i] = ps[i].expired()
		if ps[i].Status == apProposed {
			ps[i].Approval = s.approvalState(r.Context(), approvalKindPush, "", false, ps[i].Approvals)
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"pushes": ps})
}

func (s *Server) handleGetPush(w http.ResponseWriter, r *http.Request) {
	p, err := s.loadPush(r.Context(), r.PathValue("id"))
	if err != nil {
		s.defError(w, "push", err)
		return
	}
	if p.Status == apProposed {
		p.Approval = s.approvalState(r.Context(), approvalKindPush, "", false, p.Approvals)
	}
	writeJSON(w, http.StatusOK, p)
}

// handleApprovePush checks the approval like every other (not the proposer, the exact text, the approval rules), then
// pushes exactly what was approved and records the digest the registry confirmed.
func (s *Server) handleApprovePush(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Hash string `json:"hash"`
		Note string `json:"note"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	ctx, who := r.Context(), principalFrom(r.Context())
	s.pushMu.Lock()
	p, err := s.loadPush(ctx, r.PathValue("id"))
	if err != nil {
		s.pushMu.Unlock()
		s.defError(w, "push", err)
		return
	}
	refuse := func(status int, msg string) {
		s.pushMu.Unlock()
		s.auditReq(r, "ai.agent.push.approve", "push/"+p.ID, "refused: "+msg, false)
		writeError(w, status, msg)
	}
	g, gerr := s.loadRegistry(ctx, p.Registry)
	switch {
	case p.Status == apExpired:
		refuse(http.StatusConflict, "this push is older than 24 hours; propose it again")
		return
	case p.Status != apProposed:
		refuse(http.StatusConflict, "this push is "+p.Status+", not waiting for approval")
		return
	case p.ProposedBy == who.Name:
		refuse(http.StatusForbidden, "you proposed this push; someone else must approve it")
		return
	case req.Hash != p.Hash || pushHash(p.Where, p.Ref, p.YAML) != p.Hash:
		refuse(http.StatusConflict, "the text you approved is not the text of this push; reload and read it again")
		return
	case gerr != nil:
		refuse(http.StatusConflict, "the registry was removed since this was proposed; propose it again")
		return
	case g.where() != p.Where:
		refuse(http.StatusConflict, "the registry's address changed since this was proposed; propose it again")
		return
	}
	votes, need, done, vstatus, vmsg := s.vote(ctx, approvalKindPush, "", false, p.Approvals, who.Name, req.Note)
	if vmsg != "" {
		refuse(vstatus, vmsg)
		return
	}
	p.Approvals = votes
	if !done {
		if err := s.docs.Put(ctx, kindAIPush, p.ID, p); err != nil {
			s.pushMu.Unlock()
			s.internalError(w, "saving the approval", err)
			return
		}
		s.pushMu.Unlock()
		s.auditReq(r, "ai.agent.push.approve", "push/"+p.ID, fmt.Sprintf("%s — approval %d of %d recorded; waiting for another administrator", p.Ref, len(votes), need.Needed), true)
		p.Approval = s.approvalState(ctx, approvalKindPush, "", false, p.Approvals)
		writeJSON(w, http.StatusOK, p)
		return
	}
	now := time.Now().UTC()
	p.Status, p.DecidedBy, p.DecidedAt, p.Note = pushPushing, who.Name, &now, truncate(strings.TrimSpace(req.Note), 300)
	if err := s.docs.Put(ctx, kindAIPush, p.ID, p); err != nil {
		s.pushMu.Unlock()
		s.internalError(w, "saving the approval", err)
		return
	}
	s.pushMu.Unlock()
	s.auditReq(r, "ai.agent.push.approve", "push/"+p.ID, p.Ref, true)

	// The push itself, outside the lock: the status above already stops a second approval.
	pctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), pushTimeout)
	defer cancel()
	ann := map[string]string{
		"org.opencontainers.image.title":   p.AgentName,
		"org.opencontainers.image.version": p.Tag,
		"io.forgeline.agent.id":            p.Agent,
		"io.forgeline.agent.version":       fmt.Sprint(p.Version),
		"io.forgeline.agent.fingerprint":   p.Fingerprint,
	}
	art, err := oci.AgentArtifact([]byte(p.YAML), version.String(), p.Created, ann)
	digest := ""
	if err == nil {
		digest, err = oci.Push(pctx, s.registryClient(ctx, g), p.Repository, p.Tag, art)
	}
	end := time.Now().UTC()
	p.FinishedAt = &end
	if err != nil {
		p.Status, p.Result = apFailed, truncate(err.Error(), 600)
	} else {
		p.Status, p.Digest, p.Result = pushPushed, digest, "pushed "+p.Ref+"@"+digest
	}
	if perr := s.docs.Put(context.WithoutCancel(ctx), kindAIPush, p.ID, p); perr != nil {
		s.log.Error("saving a push result", "push", p.ID, "err", perr)
	}
	s.audit(context.WithoutCancel(ctx), who, clientIP(r), "ai.agent.push", "agent/"+p.Agent, p.Status+": "+p.Result, err == nil)
	writeJSON(w, http.StatusOK, p)
}

func (s *Server) handleDecidePush(reject bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx, who := r.Context(), principalFrom(r.Context())
		s.pushMu.Lock()
		defer s.pushMu.Unlock()
		p, err := s.loadPush(ctx, r.PathValue("id"))
		if err != nil {
			s.defError(w, "push", err)
			return
		}
		if p.Status != apProposed && p.Status != apExpired {
			writeError(w, http.StatusConflict, "this push is "+p.Status)
			return
		}
		status, action := apCancelled, "ai.agent.push.cancel"
		if reject {
			status, action = apRejected, "ai.agent.push.reject"
		} else if who.Name != p.ProposedBy && !who.Role.AtLeast(authn.Admin) {
			writeError(w, http.StatusForbidden, "only the person who proposed it, or an administrator, can cancel it")
			return
		}
		now := time.Now().UTC()
		p.Status, p.DecidedBy, p.DecidedAt = status, who.Name, &now
		if err := s.docs.Put(ctx, kindAIPush, p.ID, p); err != nil {
			s.internalError(w, "saving the decision", err)
			return
		}
		s.auditReq(r, action, "push/"+p.ID, p.Ref, true)
		writeJSON(w, http.StatusOK, p)
	}
}
