package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/forgelinehq/forgeline/internal/docstore"
	"github.com/forgelinehq/forgeline/internal/pipeline"
)

const (
	kindPipeline    = "pipeline"
	maxVersionsKept = 20
)

// SavedPipeline is a pipeline definition kept by the console. Its ID is the name inside the YAML.
type SavedPipeline struct {
	ID          string            `json:"id"`
	Name        string            `json:"name"`
	Description string            `json:"description,omitempty"`
	YAML        string            `json:"yaml"`
	Version     int               `json:"version"`
	CreatedAt   time.Time         `json:"created_at"`
	CreatedBy   string            `json:"created_by"`
	UpdatedAt   time.Time         `json:"updated_at"`
	UpdatedBy   string            `json:"updated_by"`
	History     []PipelineVersion `json:"history,omitempty"`
	// Source, when set, is the repository and branch each run downloads first (design note 043, slice 2).
	Source *PipelineSource `json:"source,omitempty"`
}

// PipelineVersion is an earlier version of a saved pipeline, newest first in SavedPipeline.History.
type PipelineVersion struct {
	Version int       `json:"version"`
	YAML    string    `json:"yaml"`
	By      string    `json:"by"`
	At      time.Time `json:"at"`
}

func (s *Server) loadPipelines(ctx context.Context) ([]SavedPipeline, error) {
	docs, err := s.docs.List(ctx, kindPipeline, docstore.ListOptions{})
	if err != nil {
		return nil, err
	}
	return docstore.Decode[SavedPipeline](docs)
}

// --- validation and graph preview ---

// GraphJSON is a pipeline's shape, for the editor's preview.
type GraphJSON struct {
	Steps  []GraphStep `json:"steps"`
	Layers [][]string  `json:"layers"`
}

// GraphStep is one node of the preview.
type GraphStep struct {
	ID      string   `json:"id"`
	Image   string   `json:"image"`
	Needs   []string `json:"needs"`
	Layer   int      `json:"layer"`
	Network string   `json:"network"`
	Secrets []string `json:"secrets"`
	Cached  bool     `json:"cacheable"`
}

// ValidateJSON is the result of checking a pipeline without running it.
type ValidateJSON struct {
	Valid    bool          `json:"valid"`
	Name     string        `json:"name,omitempty"`
	Problems []ProblemJSON `json:"problems"`
	Graph    *GraphJSON    `json:"graph,omitempty"`
	// MissingSecrets are secrets steps request that no vault entry or environment variable provides.
	MissingSecrets []string `json:"missing_secrets"`
	// Warnings are things that are valid but probably not what was meant.
	Warnings []string `json:"warnings"`
}

func (s *Server) validateYAML(ctx context.Context, text string) ValidateJSON {
	out := ValidateJSON{Problems: []ProblemJSON{}, MissingSecrets: []string{}, Warnings: []string{}}
	if len(text) > maxPipelineBytes {
		out.Problems = append(out.Problems, ProblemJSON{Path: "", Msg: "pipeline is larger than 1 MiB"})
		return out
	}
	p, err := pipeline.Load(strings.NewReader(text))
	var g *pipeline.Graph
	if err == nil {
		g, err = pipeline.Plan(p)
	}
	if err != nil {
		if apiErr := validationProblems(err); len(apiErr.body.Problems) > 0 {
			out.Problems = append(out.Problems, apiErr.body.Problems...)
		} else {
			out.Problems = append(out.Problems, ProblemJSON{Path: "", Msg: err.Error()})
		}
		return out
	}
	out.Valid, out.Name = true, p.Name
	have, _ := s.secretNames(ctx)
	layerOf := map[string]int{}
	gj := &GraphJSON{Steps: []GraphStep{}, Layers: [][]string{}}
	for i, layer := range g.Layers {
		ids := []string{}
		for _, st := range layer {
			layerOf[st.ID] = i
			ids = append(ids, st.ID)
		}
		gj.Layers = append(gj.Layers, ids)
	}
	missing := map[string]bool{}
	for _, st := range p.Steps {
		net := st.Permissions.Network
		if net == "" {
			net = "egress"
		}
		needs := st.Needs
		if needs == nil {
			needs = []string{}
		}
		secs := st.Permissions.Secrets
		if secs == nil {
			secs = []string{}
		}
		gj.Steps = append(gj.Steps, GraphStep{ID: st.ID, Image: st.Image, Needs: needs, Layer: layerOf[st.ID],
			Network: net, Secrets: secs, Cached: len(st.Inputs) > 0 && len(secs) == 0})
		for _, name := range secs {
			if !have[name] {
				missing[name] = true
			}
		}
		if len(secs) > 0 && net == "egress" {
			out.Warnings = append(out.Warnings, fmt.Sprintf("step %q holds secrets and has network access; set permissions.network: none unless it needs to call out", st.ID))
		}
		// Workflow and build steps run no image of their own.
		if st.Image != "" && (strings.HasSuffix(strings.SplitN(st.Image, "@", 2)[0], ":latest") || !strings.ContainsAny(st.Image, ":@")) {
			out.Warnings = append(out.Warnings, fmt.Sprintf("step %q uses a mutable image tag (%s); pin a version or digest for repeatable runs", st.ID, st.Image))
		}
	}
	for name := range missing {
		out.MissingSecrets = append(out.MissingSecrets, name)
	}
	sort.Strings(out.MissingSecrets)
	out.Graph = gj
	return out
}

func (s *Server) handleValidatePipeline(w http.ResponseWriter, r *http.Request) {
	var req struct {
		YAML string `json:"yaml"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	writeJSON(w, http.StatusOK, s.validateYAML(r.Context(), req.YAML))
}

// --- CRUD ---

// PipelineListItem is a saved pipeline without its text or history.
type PipelineListItem struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Description string    `json:"description,omitempty"`
	Version     int       `json:"version"`
	UpdatedAt   time.Time `json:"updated_at"`
	UpdatedBy   string    `json:"updated_by"`
	Steps       int       `json:"steps"`
}

func (s *Server) handleListPipelines(w http.ResponseWriter, r *http.Request) {
	all, err := s.loadPipelines(r.Context())
	if err != nil {
		s.internalError(w, "listing pipelines", err)
		return
	}
	out := make([]PipelineListItem, len(all))
	for i, sp := range all {
		n := 0
		if p, err := pipeline.Load(strings.NewReader(sp.YAML)); err == nil {
			n = len(p.Steps)
		}
		out[i] = PipelineListItem{ID: sp.ID, Name: sp.Name, Description: sp.Description, Version: sp.Version,
			UpdatedAt: sp.UpdatedAt, UpdatedBy: sp.UpdatedBy, Steps: n}
	}
	writeJSON(w, http.StatusOK, map[string]any{"pipelines": out})
}

func (s *Server) handleGetPipeline(w http.ResponseWriter, r *http.Request) {
	var sp SavedPipeline
	if err := s.docs.Get(r.Context(), kindPipeline, r.PathValue("id"), &sp); errors.Is(err, docstore.ErrNotFound) {
		writeError(w, http.StatusNotFound, "no such pipeline")
		return
	} else if err != nil {
		s.internalError(w, "reading the pipeline", err)
		return
	}
	writeJSON(w, http.StatusOK, sp)
}

type savePipelineRequest struct {
	YAML        string `json:"yaml"`
	Description string `json:"description"`
	// BaseVersion is the version the editor started from. A save fails if the pipeline has
	// moved on, so two people editing do not silently overwrite each other.
	BaseVersion int `json:"base_version"`
	// Source is the repository and branch runs check out: an object sets it, null clears it, and leaving it out keeps
	// what the pipeline has.
	Source json.RawMessage `json:"source"`
}

// source reads the request's source: keep says it was left out.
func (r savePipelineRequest) source() (src *PipelineSource, keep bool, err error) {
	if len(r.Source) == 0 {
		return nil, true, nil
	}
	if string(r.Source) == "null" {
		return nil, false, nil
	}
	dec := json.NewDecoder(bytes.NewReader(r.Source))
	dec.DisallowUnknownFields()
	var s PipelineSource
	if err := dec.Decode(&s); err != nil {
		return nil, false, errors.New(`source: give {"repo": "owner/name", "branch": "main"}, or null for none`)
	}
	return &s, false, nil
}

// checkSavable validates text and returns the pipeline's name.
func (s *Server) checkSavable(text, description string) (string, *apiError) {
	if len(description) > 300 {
		return "", badRequest("description is longer than 300 characters")
	}
	if len(text) > maxPipelineBytes {
		return "", badRequest("pipeline is larger than 1 MiB")
	}
	p, err := pipeline.Load(strings.NewReader(text))
	if err == nil {
		_, err = pipeline.Plan(p)
	}
	if err != nil {
		return "", validationProblems(err)
	}
	return p.Name, nil
}

func (s *Server) handleCreatePipeline(w http.ResponseWriter, r *http.Request) {
	var req savePipelineRequest
	if !readJSON(w, r, &req) {
		return
	}
	name, apiErr := s.checkSavable(req.YAML, req.Description)
	if apiErr != nil {
		apiErr.write(w)
		return
	}
	src, _, err := req.source()
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if m := s.checkPipelineSource(src); m != "" {
		writeError(w, http.StatusBadRequest, m)
		return
	}
	p := principalFrom(r.Context())
	now := time.Now().UTC()
	sp := SavedPipeline{ID: name, Name: name, Description: req.Description, YAML: req.YAML, Version: 1,
		CreatedAt: now, CreatedBy: p.Name, UpdatedAt: now, UpdatedBy: p.Name, Source: src}
	if err := s.docs.Create(r.Context(), kindPipeline, sp.ID, sp); errors.Is(err, docstore.ErrExists) {
		writeError(w, http.StatusConflict, fmt.Sprintf("a pipeline named %q already exists", name))
		return
	} else if err != nil {
		s.internalError(w, "saving the pipeline", err)
		return
	}
	s.auditReq(r, "pipeline.create", "pipeline/"+sp.ID, "version 1"+sourceWords(sp.Source), true)
	writeJSON(w, http.StatusCreated, sp)
}

func (s *Server) handleUpdatePipeline(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var req savePipelineRequest
	if !readJSON(w, r, &req) {
		return
	}
	name, apiErr := s.checkSavable(req.YAML, req.Description)
	if apiErr != nil {
		apiErr.write(w)
		return
	}
	if name != id {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("the name in the YAML is %q but this pipeline is %q; to rename, create a new pipeline", name, id))
		return
	}
	src, keep, err := req.source()
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	s.pipeMu.Lock()
	defer s.pipeMu.Unlock()
	var sp SavedPipeline
	if err := s.docs.Get(r.Context(), kindPipeline, id, &sp); errors.Is(err, docstore.ErrNotFound) {
		writeError(w, http.StatusNotFound, "no such pipeline")
		return
	} else if err != nil {
		s.internalError(w, "reading the pipeline", err)
		return
	}
	if req.BaseVersion != 0 && req.BaseVersion != sp.Version {
		writeJSON(w, http.StatusConflict, map[string]any{
			"error":   fmt.Sprintf("this pipeline changed (now version %d, by %s) since you opened it", sp.Version, sp.UpdatedBy),
			"current": sp,
		})
		return
	}
	if keep {
		src = sp.Source
	} else if !sameSource(src, sp.Source) {
		// A new source is checked against the allow list; one the pipeline already has is checked when it runs.
		if m := s.checkPipelineSource(src); m != "" {
			writeError(w, http.StatusBadRequest, m)
			return
		}
	}
	if req.YAML == sp.YAML && req.Description == sp.Description && sameSource(src, sp.Source) {
		writeJSON(w, http.StatusOK, sp)
		return
	}
	p := principalFrom(r.Context())
	now := time.Now().UTC()
	sp.History = append([]PipelineVersion{{Version: sp.Version, YAML: sp.YAML, By: sp.UpdatedBy, At: sp.UpdatedAt}}, sp.History...)
	if len(sp.History) > maxVersionsKept {
		sp.History = sp.History[:maxVersionsKept]
	}
	sp.YAML, sp.Description, sp.Version, sp.UpdatedAt, sp.UpdatedBy = req.YAML, req.Description, sp.Version+1, now, p.Name
	sp.Source = src
	if err := s.docs.Put(r.Context(), kindPipeline, id, sp); err != nil {
		s.internalError(w, "saving the pipeline", err)
		return
	}
	s.auditReq(r, "pipeline.update", "pipeline/"+id, fmt.Sprintf("version %d", sp.Version)+sourceWords(sp.Source), true)
	writeJSON(w, http.StatusOK, sp)
}

func (s *Server) handleDeletePipeline(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := s.docs.Delete(r.Context(), kindPipeline, id); errors.Is(err, docstore.ErrNotFound) {
		writeError(w, http.StatusNotFound, "no such pipeline")
		return
	} else if err != nil {
		s.internalError(w, "deleting the pipeline", err)
		return
	}
	s.auditReq(r, "pipeline.delete", "pipeline/"+id, "", true)
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleRestorePipeline(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var v int
	if _, err := fmt.Sscanf(r.PathValue("version"), "%d", &v); err != nil {
		writeError(w, http.StatusBadRequest, "version must be a number")
		return
	}
	s.pipeMu.Lock()
	defer s.pipeMu.Unlock()
	var sp SavedPipeline
	if err := s.docs.Get(r.Context(), kindPipeline, id, &sp); errors.Is(err, docstore.ErrNotFound) {
		writeError(w, http.StatusNotFound, "no such pipeline")
		return
	} else if err != nil {
		s.internalError(w, "reading the pipeline", err)
		return
	}
	var old *PipelineVersion
	for i := range sp.History {
		if sp.History[i].Version == v {
			old = &sp.History[i]
		}
	}
	if old == nil {
		writeError(w, http.StatusNotFound, fmt.Sprintf("version %d is not kept (the last %d are)", v, maxVersionsKept))
		return
	}
	p := principalFrom(r.Context())
	now := time.Now().UTC()
	sp.History = append([]PipelineVersion{{Version: sp.Version, YAML: sp.YAML, By: sp.UpdatedBy, At: sp.UpdatedAt}}, sp.History...)
	if len(sp.History) > maxVersionsKept {
		sp.History = sp.History[:maxVersionsKept]
	}
	sp.YAML, sp.Version, sp.UpdatedAt, sp.UpdatedBy = old.YAML, sp.Version+1, now, p.Name
	if err := s.docs.Put(r.Context(), kindPipeline, id, sp); err != nil {
		s.internalError(w, "saving the pipeline", err)
		return
	}
	s.auditReq(r, "pipeline.restore", "pipeline/"+id, fmt.Sprintf("version %d restored as %d", v, sp.Version), true)
	writeJSON(w, http.StatusOK, sp)
}

// handleRunPipeline starts a run of the saved pipeline's current version.
func (s *Server) handleRunPipeline(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var sp SavedPipeline
	if err := s.docs.Get(r.Context(), kindPipeline, id, &sp); errors.Is(err, docstore.ErrNotFound) {
		writeError(w, http.StatusNotFound, "no such pipeline")
		return
	} else if err != nil {
		s.internalError(w, "reading the pipeline", err)
		return
	}
	run, steps, apiErr := s.runSaved(r.Context(), sp)
	if apiErr != nil {
		apiErr.write(w)
		return
	}
	p := principalFrom(r.Context())
	s.recordRunMeta(r.Context(), run.ID, runMeta{Actor: p.Name, PipelineID: sp.ID, Version: sp.Version})
	s.auditReq(r, "run.start", "run/"+run.ID, fmt.Sprintf("%s v%d from the console", sp.ID, sp.Version)+sourceNote(run), true)
	w.Header().Set("Location", "/v1/runs/"+run.ID)
	writeJSON(w, http.StatusAccepted, s.decorate(r.Context(), runDetail(run, steps)))
}

func sameSource(a, b *PipelineSource) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

func sourceWords(src *PipelineSource) string {
	if src == nil {
		return ", no source"
	}
	return fmt.Sprintf(", source %s branch %s", src.Repo, src.Branch)
}
