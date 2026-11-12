package server

import (
	"context"
	"errors"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/forgelinehq/forgeline/internal/authn"
	"github.com/forgelinehq/forgeline/internal/blueprint"
	"github.com/forgelinehq/forgeline/internal/docstore"
)

// Blueprints (design note 035, slice 1): drafts of what should exist in a cloud account, saved by developers,
// checked in plain words and turned into OpenTofu code. Nothing here calls AWS or runs tofu; building one is
// a later slice, and it will be a cloud change approved by a different administrator.

const (
	kindBlueprint = "blueprint"
	maxBlueprints = 200
)

// StoredBlueprint is a blueprint with who made and changed it. Revision goes up on every save, so two people
// editing the same draft cannot overwrite each other without noticing.
type StoredBlueprint struct {
	blueprint.Blueprint
	Revision  int       `json:"revision"`
	CreatedBy string    `json:"created_by"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedBy string    `json:"updated_by"`
	UpdatedAt time.Time `json:"updated_at"`
}

// BlueprintJSON is a blueprint as the console reads it: the document and its checks.
type BlueprintJSON struct {
	StoredBlueprint
	Checks []blueprint.Check `json:"checks"`
}

// BlueprintSummaryJSON is one row of the list.
type BlueprintSummaryJSON struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Account   string    `json:"account"`
	Region    string    `json:"region"`
	Blocks    int       `json:"blocks"`
	Warnings  int       `json:"warnings"`
	UpdatedBy string    `json:"updated_by"`
	UpdatedAt time.Time `json:"updated_at"`
}

type blueprintBody struct {
	Name        string            `json:"name"`
	Description string            `json:"description"`
	Target      blueprint.Target  `json:"target"`
	Blocks      []blueprint.Block `json:"blocks"`
	Links       []blueprint.Link  `json:"links"`
	Revision    int               `json:"revision"` // on update: the revision the edit started from
}

func (b blueprintBody) doc(id string) blueprint.Blueprint {
	d := blueprint.Blueprint{ID: id, Name: strings.TrimSpace(b.Name), Description: b.Description, Target: b.Target, Blocks: b.Blocks, Links: b.Links}
	if d.Blocks == nil {
		d.Blocks = []blueprint.Block{}
	}
	if d.Links == nil {
		d.Links = []blueprint.Link{}
	}
	return d
}

func checksOf(b blueprint.Blueprint) []blueprint.Check {
	c := b.Checks()
	if c == nil {
		c = []blueprint.Check{}
	}
	return c
}

// checkTarget makes sure the blueprint names an AWS account this server knows.
func (s *Server) checkTarget(ctx context.Context, t blueprint.Target) *httpErr {
	if _, err := s.awsFor(ctx, t.Account); errors.Is(err, errNoAccount) {
		return refusal(http.StatusBadRequest, "target.account: no AWS account "+t.Account+" is connected")
	} else if err != nil {
		return refusal(http.StatusInternalServerError, "could not read the AWS accounts")
	}
	return nil
}

func (s *Server) handleListBlueprints(w http.ResponseWriter, r *http.Request) {
	docs, err := s.docs.List(r.Context(), kindBlueprint, docstore.ListOptions{})
	if err != nil {
		s.internalError(w, "reading the blueprints", err)
		return
	}
	all, err := docstore.Decode[StoredBlueprint](docs)
	if err != nil {
		s.internalError(w, "reading the blueprints", err)
		return
	}
	out := []BlueprintSummaryJSON{}
	for _, b := range all {
		warn := 0
		for _, c := range b.Checks() {
			if c.Level == "warn" {
				warn++
			}
		}
		out = append(out, BlueprintSummaryJSON{ID: b.ID, Name: b.Name, Account: b.Target.Account, Region: b.Target.Region, Blocks: len(b.Blocks), Warnings: warn, UpdatedBy: b.UpdatedBy, UpdatedAt: b.UpdatedAt})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].UpdatedAt.After(out[j].UpdatedAt) })
	writeJSON(w, http.StatusOK, map[string]any{"blueprints": out})
}

func (s *Server) loadBlueprint(ctx context.Context, id string) (StoredBlueprint, error) {
	var b StoredBlueprint
	err := s.docs.Get(ctx, kindBlueprint, id, &b)
	return b, err
}

func (s *Server) handleGetBlueprint(w http.ResponseWriter, r *http.Request) {
	b, err := s.loadBlueprint(r.Context(), r.PathValue("id"))
	if err != nil {
		s.defError(w, "blueprint", err)
		return
	}
	writeJSON(w, http.StatusOK, BlueprintJSON{StoredBlueprint: b, Checks: checksOf(b.Blueprint)})
}

// handleCheckBlueprint validates a draft that is not saved, so the canvas can say what is wrong as people edit.
func (s *Server) handleCheckBlueprint(w http.ResponseWriter, r *http.Request) {
	var body blueprintBody
	if !readJSON(w, r, &body) {
		return
	}
	d := body.doc("draft")
	if err := d.Validate(); err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"error": err.Error(), "checks": []blueprint.Check{}})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"checks": checksOf(d)})
}

func (s *Server) handleCreateBlueprint(w http.ResponseWriter, r *http.Request) {
	var body blueprintBody
	if !readJSON(w, r, &body) {
		return
	}
	p := principalFrom(r.Context())
	d := body.doc("bp-" + strings.ToLower(authn.NewID()[:10]))
	if err := d.Validate(); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if e := s.checkTarget(r.Context(), d.Target); e != nil {
		writeError(w, e.status, e.msg)
		return
	}
	if n, err := s.docs.Count(r.Context(), kindBlueprint); err == nil && n >= maxBlueprints {
		writeError(w, http.StatusConflict, "this server already keeps the most blueprints it may; delete one first")
		return
	}
	now := time.Now().UTC()
	b := StoredBlueprint{Blueprint: d, Revision: 1, CreatedBy: p.Name, CreatedAt: now, UpdatedBy: p.Name, UpdatedAt: now}
	if err := s.docs.Create(r.Context(), kindBlueprint, d.ID, b); err != nil {
		s.internalError(w, "saving the blueprint", err)
		return
	}
	s.audit(r.Context(), p, clientIP(r), "blueprint.create", d.ID, d.Name, true)
	writeJSON(w, http.StatusCreated, BlueprintJSON{StoredBlueprint: b, Checks: checksOf(d)})
}

func (s *Server) handleUpdateBlueprint(w http.ResponseWriter, r *http.Request) {
	var body blueprintBody
	if !readJSON(w, r, &body) {
		return
	}
	p := principalFrom(r.Context())
	cur, err := s.loadBlueprint(r.Context(), r.PathValue("id"))
	if err != nil {
		s.defError(w, "blueprint", err)
		return
	}
	if body.Revision != cur.Revision {
		writeError(w, http.StatusConflict, cur.UpdatedBy+" changed this blueprint after you opened it; reload to see their version")
		return
	}
	d := body.doc(cur.ID)
	if err := d.Validate(); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	// What was built stays where it was built: its state belongs to that account and region.
	if built, ok, err := s.loadBPState(r.Context(), cur.ID); err != nil {
		s.internalError(w, "reading the blueprint's state", err)
		return
	} else if ok && (d.Target.Account != cur.Target.Account || d.Target.Region != cur.Target.Region) {
		writeError(w, http.StatusConflict, "target: this blueprint was built in "+cur.Target.Account+", "+built.Region+"; it cannot be moved. Make a new blueprint for another account or region")
		return
	}
	if e := s.checkTarget(r.Context(), d.Target); e != nil {
		writeError(w, e.status, e.msg)
		return
	}
	cur.Blueprint, cur.Revision, cur.UpdatedBy, cur.UpdatedAt = d, cur.Revision+1, p.Name, time.Now().UTC()
	if err := s.docs.Put(r.Context(), kindBlueprint, cur.ID, cur); err != nil {
		s.internalError(w, "saving the blueprint", err)
		return
	}
	s.audit(r.Context(), p, clientIP(r), "blueprint.update", cur.ID, d.Name, true)
	writeJSON(w, http.StatusOK, BlueprintJSON{StoredBlueprint: cur, Checks: checksOf(d)})
}

// handleDeleteBlueprint removes a draft. Only the person who made it or an administrator may.
func (s *Server) handleDeleteBlueprint(w http.ResponseWriter, r *http.Request) {
	p := principalFrom(r.Context())
	b, err := s.loadBlueprint(r.Context(), r.PathValue("id"))
	if err != nil {
		s.defError(w, "blueprint", err)
		return
	}
	if b.CreatedBy != p.Name && !p.Role.AtLeast(authn.Admin) {
		s.audit(r.Context(), p, clientIP(r), "blueprint.delete", b.ID, "not the author", false)
		writeError(w, http.StatusForbidden, "only "+b.CreatedBy+" or an administrator can delete this blueprint")
		return
	}
	// The state is kept even after the blueprint is deleted: its count of applies is what stops an old approved plan
	// of this blueprint from ever applying.
	if st, built, err := s.loadBPState(r.Context(), b.ID); err != nil {
		s.internalError(w, "reading the blueprint's state", err)
		return
	} else if built && !st.Empty {
		writeError(w, http.StatusConflict, "this blueprint has built things that still exist; deleting it would leave them without a record. Remove what it built first (Remove… on the design canvas)")
		return
	}
	s.docs.Delete(r.Context(), kindBPPlan, b.ID) // its preview, if any
	if err := s.docs.Delete(r.Context(), kindBlueprint, b.ID); err != nil {
		s.internalError(w, "deleting the blueprint", err)
		return
	}
	s.audit(r.Context(), p, clientIP(r), "blueprint.delete", b.ID, b.Name, true)
	w.WriteHeader(http.StatusNoContent)
}

// handleBlueprintDelivery suggests how a block's image is built from its repository (note 040, step 4): a GitHub
// Actions workflow for the person to add to the repository, and a Forgeline pipeline that starts it. Nothing is
// written anywhere; the account comes from the blueprint's state or latest preview when there is one.
func (s *Server) handleBlueprintDelivery(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	b, err := s.loadBlueprint(ctx, r.PathValue("id"))
	if err != nil {
		s.defError(w, "blueprint", err)
		return
	}
	account := ""
	if st, built, err := s.loadBPState(ctx, b.ID); err == nil && built {
		account = st.AWSAccount
	} else {
		var p storedPlan
		if s.docs.Get(ctx, kindBPPlan, b.ID, &p) == nil {
			account = p.AWSAccount
		}
	}
	d, err := b.SuggestDelivery(r.PathValue("block"), account)
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, d)
}

// handleBlueprintCode returns the OpenTofu configuration the blueprint generates (main.tf.json).
func (s *Server) handleBlueprintCode(w http.ResponseWriter, r *http.Request) {
	b, err := s.loadBlueprint(r.Context(), r.PathValue("id"))
	if err != nil {
		s.defError(w, "blueprint", err)
		return
	}
	code, err := b.Tofu()
	if err != nil {
		writeError(w, http.StatusConflict, "this blueprint is not valid any more: "+err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Content-Disposition", `inline; filename="main.tf.json"`)
	w.Write(code)
}
