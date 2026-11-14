package server

import (
	"encoding/json"
	"net/http"

	"github.com/forgelinehq/forgeline/internal/template"
)

// Templates (design note 040, step 2): reviewed OpenTofu modules the canvas offers as one block. The list gives the
// canvas what it needs to draw a template and its form; the code is there for anyone who wants to read what a
// template builds before using it.

// TemplateJSON is one template as the canvas sees it.
type TemplateJSON struct {
	Ref       string              `json:"ref"`
	Name      string              `json:"name"`
	Version   string              `json:"version"`
	Title     string              `json:"title"`
	Summary   string              `json:"summary"`
	Icon      string              `json:"icon"`
	HoldsData bool                `json:"holds_data"`
	Checks    []string            `json:"checks"`
	Inputs    json.RawMessage     `json:"inputs"`      // the JSON Schema subset, as written
	Order     []string            `json:"input_order"` // the inputs in the template's order, for the form
	Links     []template.LinkRole `json:"links"`
}

func (s *Server) handleListTemplates(w http.ResponseWriter, r *http.Request) {
	all, err := template.List()
	if err != nil {
		s.internalError(w, "reading the templates", err)
		return
	}
	out := make([]TemplateJSON, 0, len(all))
	for _, t := range all {
		links := t.Diagram.Links
		if links == nil {
			links = []template.LinkRole{}
		}
		out = append(out, TemplateJSON{Ref: t.Ref(), Name: t.Name, Version: t.Version, Title: t.Title, Summary: t.Summary,
			Icon: t.Diagram.Icon, HoldsData: t.HoldsData, Checks: t.Checks, Inputs: t.RawInputs, Order: t.Inputs.Order, Links: links})
	}
	writeJSON(w, http.StatusOK, map[string]any{"templates": out})
}

func (s *Server) handleTemplateCode(w http.ResponseWriter, r *http.Request) {
	t, err := template.Get(r.PathValue("name") + "@" + r.PathValue("version"))
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Content-Disposition", `inline; filename="main.tf.json"`)
	w.Write(t.Code)
}
