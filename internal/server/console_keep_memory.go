package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"
)

// "Keep this?" (design note 052, slice 5): after a task the assistant can offer what it learned as a card. The file is
// not written until the person presses Confirm, so it is the person's own write: never pending, whatever the assistant
// had read, and never silently under autopilot when the conversation has read data the person did not type.

const actKeepMemory = "save_memory"

type keepMemorySpec struct {
	Path       string `json:"path"`
	Content    string `json:"content"`
	ValidUntil string `json:"valid_until"`
}

// checkKeepMemory validates the spec for p now. It runs when the card is prepared and again when it is confirmed.
func (s *Server) checkKeepMemory(ctx context.Context, p Principal, raw json.RawMessage) (spec keepMemorySpec, scope, path string, err error) {
	if !s.memoryOn(ctx) {
		return spec, "", "", errors.New("memory is turned off")
	}
	if err = json.Unmarshal(raw, &spec); err != nil {
		return spec, "", "", errors.New(`give spec as {"path": "/memories/me/x.md", "content": "...", "valid_until": "optional date"}`)
	}
	scope, parts, err := memPath(spec.Path, false)
	if err != nil {
		return spec, "", "", err
	}
	// The conversation's own taint does not matter here: the person reads the text and confirms it.
	if err = canWriteMem(p, scope, false); err != nil {
		return spec, "", "", err
	}
	if spec.Content, err = cleanFileText(spec.Content); err != nil {
		return spec, "", "", err
	}
	if spec.ValidUntil, err = checkValidUntil(spec.ValidUntil); err != nil {
		return spec, "", "", err
	}
	path = fullPath(scope, parts)
	spec.Path = path
	all, err := s.allMemFiles(ctx)
	if err != nil {
		return spec, "", "", errors.New("memory could not be read")
	}
	vis := visibleMemFiles(all, p)
	n := 0
	for _, f := range vis {
		if f.Path == path {
			return spec, "", "", fmt.Errorf("the file %s already exists; use the memory tool to edit it, or choose another name", path)
		}
		if f.Scope == scope {
			n++
		}
	}
	if limit := map[string]int{"me": maxMyFiles, "org": maxOrgFiles}[scope]; n >= limit {
		return spec, "", "", fmt.Errorf("there are already %d files in %s; delete or merge some first", limit, scope)
	}
	return spec, scope, path, nil
}

// prepareKeepMemory fills in the card for a file the assistant suggests keeping.
func (s *Server) prepareKeepMemory(ctx context.Context, p Principal, raw json.RawMessage, act *AssistAction) error {
	spec, scope, path, err := s.checkKeepMemory(ctx, p, raw)
	if err != nil {
		return err
	}
	who := "only you can see it"
	if scope == "org" {
		who = "everyone who uses the assistant can see it"
	}
	act.Spec, _ = json.Marshal(spec)
	act.Pipeline, act.Detail = path, spec.Content
	act.Effects = []string{fmt.Sprintf("Saves %s (%d characters) to the assistant's memory; %s.", path, len([]rune(spec.Content)), who)}
	if spec.ValidUntil != "" {
		act.Effects = append(act.Effects, "It stays valid until "+spec.ValidUntil+", then is kept as history and no longer used.")
	}
	act.Effects = append(act.Effects, "The assistant reads it at the start of related work. You can edit or delete it on the Memory page.")
	if act.Title == "" {
		act.Title = "Keep " + path
	}
	return nil
}

// doKeepMemory writes the file as the person who confirmed it.
func (s *Server) doKeepMemory(ctx context.Context, p Principal, a AssistAction) (string, string, *apiError) {
	spec, scope, path, err := s.checkKeepMemory(ctx, p, a.Spec)
	if err != nil {
		return "", "", &apiError{http.StatusBadRequest, ErrorJSON{Error: err.Error()}}
	}
	owner := ""
	if scope == "me" {
		owner = p.UserID
	}
	now := time.Now().UTC()
	f := MemFile{ID: memID(scope, owner, path), Scope: scope, Owner: owner, OwnerName: p.Name, Path: path, Content: spec.Content,
		Created: now, Updated: now, By: "person", ValidUntil: spec.ValidUntil}
	if err := s.docs.Put(ctx, kindMemFile, f.ID, f); err != nil {
		return "", "", &apiError{http.StatusInternalServerError, ErrorJSON{Error: "could not save the file"}}
	}
	ip, _ := ctx.Value(mcpIPKey{}).(string)
	_ = s.audit(ctx, p, ip, "assistant.memory.file", path, fmt.Sprintf("kept from a card the person confirmed (%d characters, %s)", len(f.Content), scope), true)
	return "Kept " + path + " in memory.", "/memory", nil
}
