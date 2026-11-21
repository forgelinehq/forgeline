package server

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/forgelinehq/forgeline/internal/authn"
	"github.com/forgelinehq/forgeline/internal/docstore"
)

// Memory as files (design note 052, slice 5). The assistant keeps what it learns as small text files under /memories:
// /memories/me/... for one person, /memories/org/... for everyone, written by administrators. It uses one tool,
// `memory`, with the operations of Anthropic's memory tool (view, create, str_replace, insert, delete, rename) and a
// search, so the shape is the same for every model provider. The files are stored in the document store, never on
// the server's disk, and a path is checked before anything is read or written. The notes of slice 1 (`remember`) still
// work next to it; the files are for what does not fit in 300 characters: how a system is laid out, a procedure that
// worked, what a name means.

const (
	memoryTool    = "memory"
	kindMemFile   = "memfile"
	memRoot       = "/memories"
	maxMemFileLen = 8000 // characters in one file
	maxMyFiles    = 100
	maxOrgFiles   = 200
	memIndexBytes = 2048 // bytes of the file index in a prompt
	maxViewLines  = 400
)

// MemFile is one file of memory.
type MemFile struct {
	ID        string    `json:"id"`
	Scope     string    `json:"scope"` // me or org
	Owner     string    `json:"owner,omitempty"`
	OwnerName string    `json:"owner_name,omitempty"`
	Path      string    `json:"path"`
	Content   string    `json:"content"`
	Created   time.Time `json:"created"`
	Updated   time.Time `json:"updated"`
	By        string    `json:"by"` // person or assistant
	// Pending is a file the assistant wrote after reading data the person did not type. It is not summarised in the
	// prompt until the person keeps it.
	Pending bool `json:"pending,omitempty"`
}

var memNameRE = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._ -]{0,63}$`)

// memPath checks a path and returns its scope and its parts after the scope. dir says the path may name a folder.
func memPath(path string, dir bool) (scope string, parts []string, err error) {
	path = strings.TrimRight(strings.TrimSpace(path), "/")
	if path == memRoot {
		if !dir {
			return "", nil, errors.New("that is the memory folder, not a file")
		}
		return "", nil, nil
	}
	if !strings.HasPrefix(path, memRoot+"/") {
		return "", nil, fmt.Errorf("a path starts with %s/me/ or %s/org/", memRoot, memRoot)
	}
	segs := strings.Split(strings.TrimPrefix(path, memRoot+"/"), "/")
	if segs[0] != "me" && segs[0] != "org" {
		return "", nil, fmt.Errorf("a path starts with %s/me/ (only you) or %s/org/ (everyone)", memRoot, memRoot)
	}
	scope, parts = segs[0], segs[1:]
	if len(parts) > 4 {
		return "", nil, errors.New("folders go at most four deep")
	}
	for _, p := range parts {
		if !memNameRE.MatchString(p) || strings.Contains(p, "..") {
			return "", nil, fmt.Errorf("%q is not a usable name: letters, digits, space, dot, dash and underscore, up to 64", p)
		}
	}
	if !dir {
		if len(parts) == 0 {
			return "", nil, errors.New("name a file, such as " + memRoot + "/" + scope + "/notes.md")
		}
		if last := strings.ToLower(parts[len(parts)-1]); !strings.HasSuffix(last, ".md") && !strings.HasSuffix(last, ".txt") {
			return "", nil, errors.New("a file ends in .md or .txt")
		}
	}
	return scope, parts, nil
}

func memID(scope, owner, path string) string {
	h := sha256.Sum256([]byte(scope + "|" + owner + "|" + path))
	return "mf-" + hex.EncodeToString(h[:8])
}

func fullPath(scope string, parts []string) string {
	return memRoot + "/" + scope + "/" + strings.Join(parts, "/")
}

func cleanFileText(t string) (string, error) {
	t = strings.ReplaceAll(t, "\r\n", "\n")
	switch {
	case strings.TrimSpace(t) == "":
		return "", errors.New("a file needs some text")
	case len([]rune(t)) > maxMemFileLen:
		return "", fmt.Errorf("a file is at most %d characters; split it into two", maxMemFileLen)
	case markupRE.MatchString(t):
		return "", errors.New("a file cannot contain markup or tags")
	}
	if _, found := hideSecrets(t); found {
		return "", errors.New("that looks like a secret, and memory is not a place to keep one: it belongs in the vault")
	}
	return t, nil
}

func (s *Server) allMemFiles(ctx context.Context) ([]MemFile, error) {
	docs, err := s.docs.List(ctx, kindMemFile, docstore.ListOptions{})
	if err != nil {
		return nil, err
	}
	return docstore.Decode[MemFile](docs)
}

// visibleMemFiles are the files a person may read: their own, and the organisation's.
func visibleMemFiles(all []MemFile, p Principal) []MemFile {
	var out []MemFile
	for _, f := range all {
		if f.Scope == "org" || f.Scope == "me" && f.Owner == p.UserID && p.UserID != "" {
			out = append(out, f)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out
}

// canWriteMem says whether the person may write in a scope, and why not.
func canWriteMem(p Principal, scope string, dirty bool) error {
	switch {
	case p.UserID == "":
		return errors.New("memory belongs to a signed-in person")
	case scope == "org" && !p.Role.AtLeast(authn.Admin):
		return errors.New("only an administrator writes to " + memRoot + "/org; write it under " + memRoot + "/me, and an administrator can move it")
	case scope == "org" && dirty:
		return errors.New("what you have read in this conversation came from data the person did not type, so it is not written to the organisation's memory; save it under " + memRoot + "/me, or ask again in a new message")
	}
	return nil
}

func fileGist(s string) string {
	for _, l := range strings.Split(s, "\n") {
		if l = strings.TrimSpace(strings.TrimLeft(strings.TrimSpace(l), "#")); l != "" {
			return truncate(l, 80)
		}
	}
	return ""
}

// --- BM25 search over the files a person can read ---

func tokens(s string) []string {
	return strings.FieldsFunc(strings.ToLower(s), func(r rune) bool { return !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '-' || r == '_') })
}

type memHit struct {
	f     MemFile
	score float64
}

func bm25(files []MemFile, query string) []memHit {
	q := tokens(query)
	if len(q) == 0 || len(files) == 0 {
		return nil
	}
	docs := make([]map[string]int, len(files))
	lens := make([]int, len(files))
	df := map[string]int{}
	total := 0
	for i, f := range files {
		tf := map[string]int{}
		for _, w := range tokens(f.Path + " " + f.Content) {
			tf[w]++
			lens[i]++
		}
		for w := range tf {
			df[w]++
		}
		docs[i] = tf
		total += lens[i]
	}
	avg := float64(total) / float64(len(files))
	var hits []memHit
	for i, f := range files {
		var sc float64
		for _, w := range q {
			n := df[w]
			if n == 0 {
				continue
			}
			idf := math.Log(1 + (float64(len(files))-float64(n)+0.5)/(float64(n)+0.5))
			tf := float64(docs[i][w])
			sc += idf * tf * 2.2 / (tf + 1.2*(0.25+0.75*float64(lens[i])/math.Max(avg, 1)))
		}
		if sc > 0 {
			hits = append(hits, memHit{f, sc})
		}
	}
	sort.SliceStable(hits, func(i, j int) bool { return hits[i].score > hits[j].score })
	return hits
}

// memIndex is the always-present part of the prompt: where the files are and what each is about. The model opens
// the ones it needs. Files waiting for the person's confirmation are left out.
func (s *Server) memIndex(ctx context.Context, p Principal) string {
	all, err := s.allMemFiles(ctx)
	if err != nil {
		return ""
	}
	files := visibleMemFiles(all, p)
	sort.SliceStable(files, func(i, j int) bool { return files[i].Updated.After(files[j].Updated) })
	var b strings.Builder
	for _, f := range files {
		if f.Pending {
			continue
		}
		line := "- " + f.Path + ": " + fileGist(f.Content) + "\n"
		if b.Len()+len(line) > memIndexBytes {
			b.WriteString("- … more files: use the memory tool to view " + memRoot + "\n")
			break
		}
		b.WriteString(line)
	}
	if b.Len() == 0 {
		return ""
	}
	return "Memory files you can open with the " + memoryTool + " tool (view, search):\n" + b.String()
}

// --- the tool ---

type memArgs struct {
	Command    string `json:"command"`
	Path       string `json:"path"`
	FileText   string `json:"file_text"`
	OldStr     string `json:"old_str"`
	NewStr     string `json:"new_str"`
	InsertLine *int   `json:"insert_line"`
	InsertText string `json:"insert_text"`
	OldPath    string `json:"old_path"`
	NewPath    string `json:"new_path"`
	ViewRange  []int  `json:"view_range"`
	Query      string `json:"query"`
}

func (s *Server) lookupMemory(ctx context.Context, raw json.RawMessage) (string, error) {
	t := memTurnFrom(ctx)
	if t == nil || scopeFrom(ctx) != nil {
		return "", errors.New("memory is only available when a person is chatting with the assistant")
	}
	if !s.memoryOn(ctx) {
		return "", errors.New("memory is turned off")
	}
	var a memArgs
	if err := decodeArgs(raw, &a); err != nil {
		return "", err
	}
	p := t.p
	all, err := s.allMemFiles(ctx)
	if err != nil {
		return "", errors.New("memory could not be read")
	}
	vis := visibleMemFiles(all, p)
	switch a.Command {
	case "view":
		return s.memView(a, vis)
	case "search":
		hits := bm25(vis, a.Query)
		if len(hits) == 0 {
			return "Nothing in memory matches that.", nil
		}
		var b strings.Builder
		for i, h := range hits {
			if i == 5 {
				break
			}
			fmt.Fprintf(&b, "%s: %s\n", h.f.Path, fileGist(h.f.Content))
		}
		return b.String(), nil
	case "create", "str_replace", "insert", "delete", "rename":
		out, err := s.memWrite(ctx, t, a, vis)
		return out, err
	}
	return "", errors.New(`command is one of view, search, create, str_replace, insert, delete, rename`)
}

func (s *Server) memView(a memArgs, vis []MemFile) (string, error) {
	path := a.Path
	if path == "" {
		path = memRoot
	}
	scope, parts, err := memPath(path, true)
	if err != nil {
		return "", err
	}
	prefix := strings.TrimRight(path, "/") + "/"
	for _, f := range vis {
		if f.Path != strings.TrimRight(path, "/") {
			continue
		}
		lines := strings.Split(f.Content, "\n")
		from, to := 1, len(lines)
		if len(a.ViewRange) == 2 && a.ViewRange[0] >= 1 && a.ViewRange[1] >= a.ViewRange[0] {
			from, to = a.ViewRange[0], min(a.ViewRange[1], len(lines))
		}
		to = min(to, from+maxViewLines-1)
		var b strings.Builder
		fmt.Fprintf(&b, "Content of %s with line numbers:\n", f.Path)
		if f.Pending {
			b.WriteString("(written after reading data the person did not type; not confirmed yet)\n")
		}
		for i := from; i <= to && i <= len(lines); i++ {
			fmt.Fprintf(&b, "%6d\t%s\n", i, lines[i-1])
		}
		return b.String(), nil
	}
	_ = scope
	_ = parts
	var b strings.Builder
	fmt.Fprintf(&b, "Files in %s:\n", strings.TrimRight(path, "/"))
	n := 0
	for _, f := range vis {
		if path == memRoot || strings.HasPrefix(f.Path, prefix) {
			fmt.Fprintf(&b, "%s (%d characters): %s\n", f.Path, len(f.Content), fileGist(f.Content))
			n++
		}
	}
	if n == 0 {
		if path == memRoot {
			return "Memory is empty. Save what you learn as files under " + memRoot + "/me/ (for this person) or " + memRoot + "/org/ (for everyone; administrators only).", nil
		}
		return "", fmt.Errorf("the path %s does not exist", path)
	}
	return b.String(), nil
}

func (s *Server) memWrite(ctx context.Context, t *memTurn, a memArgs, vis []MemFile) (string, error) {
	find := func(path string) (MemFile, bool) {
		for _, f := range vis {
			if f.Path == path {
				return f, true
			}
		}
		return MemFile{}, false
	}
	scope, parts, err := memPath(a.Path, a.Command == "delete")
	if a.Command == "rename" {
		scope, parts, err = memPath(a.OldPath, false)
	}
	if err != nil {
		return "", err
	}
	if scope == "" {
		return "", errors.New("name a file or a folder under " + memRoot + "/me or " + memRoot + "/org")
	}
	if err := canWriteMem(t.p, scope, t.dirty); err != nil {
		return "", err
	}
	path := fullPath(scope, parts)
	owner := ""
	if scope == "me" {
		owner = t.p.UserID
	}
	save := func(f MemFile, action string) error {
		f.Updated = time.Now().UTC()
		f.By = "assistant"
		f.Pending = f.Scope == "me" && t.dirty
		if err := s.docs.Put(ctx, kindMemFile, f.ID, f); err != nil {
			return errors.New("it could not be saved")
		}
		s.auditNoteFile(ctx, t.p, action, f)
		if sink, ok := ctx.Value(lookupSinkKey{}).(*lookupSink); ok {
			sink.files = append(sink.files, memFileEvent{Path: f.Path, Action: action, Pending: f.Pending})
		}
		return nil
	}
	count := 0
	for _, f := range vis {
		if f.Scope == scope {
			count++
		}
	}
	switch a.Command {
	case "create":
		if _, ok := find(path); ok {
			return "", fmt.Errorf("the file %s already exists; use str_replace or insert to change it", path)
		}
		if limit := map[string]int{"me": maxMyFiles, "org": maxOrgFiles}[scope]; count >= limit {
			return "", fmt.Errorf("there are already %d files in %s; delete or merge some first", limit, scope)
		}
		text, err := cleanFileText(a.FileText)
		if err != nil {
			return "", err
		}
		now := time.Now().UTC()
		f := MemFile{ID: memID(scope, owner, path), Scope: scope, Owner: owner, OwnerName: t.p.Name, Path: path, Content: text, Created: now}
		if err := save(f, "create"); err != nil {
			return "", err
		}
		return "File created successfully at: " + path + pendingNote(t.dirty && scope == "me"), nil
	case "str_replace":
		f, ok := find(path)
		if !ok {
			return "", fmt.Errorf("the file %s does not exist", path)
		}
		if a.OldStr == "" || strings.Count(f.Content, a.OldStr) != 1 {
			return "", fmt.Errorf("old_str must appear exactly once in %s (it appears %d times)", path, strings.Count(f.Content, a.OldStr))
		}
		text, err := cleanFileText(strings.Replace(f.Content, a.OldStr, a.NewStr, 1))
		if err != nil {
			return "", err
		}
		f.Content = text
		if err := save(f, "edit"); err != nil {
			return "", err
		}
		return "The file " + path + " has been edited." + pendingNote(t.dirty && scope == "me"), nil
	case "insert":
		f, ok := find(path)
		if !ok {
			return "", fmt.Errorf("the file %s does not exist", path)
		}
		lines := strings.Split(f.Content, "\n")
		at := len(lines)
		if a.InsertLine != nil {
			at = *a.InsertLine
		}
		if at < 0 || at > len(lines) {
			return "", fmt.Errorf("insert_line must be between 0 and %d", len(lines))
		}
		lines = append(lines[:at], append([]string{a.InsertText}, lines[at:]...)...)
		text, err := cleanFileText(strings.Join(lines, "\n"))
		if err != nil {
			return "", err
		}
		f.Content = text
		if err := save(f, "edit"); err != nil {
			return "", err
		}
		return "The text has been inserted into " + path + "." + pendingNote(t.dirty && scope == "me"), nil
	case "delete":
		var gone []MemFile
		for _, f := range vis {
			if f.Scope == scope && (f.Path == path || strings.HasPrefix(f.Path, path+"/")) {
				gone = append(gone, f)
			}
		}
		if len(gone) == 0 {
			return "", fmt.Errorf("the path %s does not exist", path)
		}
		for _, f := range gone {
			if err := s.docs.Delete(ctx, kindMemFile, f.ID); err != nil {
				return "", errors.New("it could not be deleted")
			}
			s.auditNoteFile(ctx, t.p, "delete", f)
			if sink, ok := ctx.Value(lookupSinkKey{}).(*lookupSink); ok {
				sink.files = append(sink.files, memFileEvent{Path: f.Path, Action: "delete"})
			}
		}
		return fmt.Sprintf("Deleted %s (%d file%s).", path, len(gone), map[bool]string{true: "", false: "s"}[len(gone) == 1]), nil
	case "rename":
		f, ok := find(path)
		if !ok {
			return "", fmt.Errorf("the file %s does not exist", path)
		}
		ns, np, err := memPath(a.NewPath, false)
		if err != nil {
			return "", err
		}
		if ns != scope {
			return "", errors.New("a file stays in its scope; to move it, create it in the other one and delete this one")
		}
		newPath := fullPath(ns, np)
		if _, exists := find(newPath); exists {
			return "", fmt.Errorf("the file %s already exists", newPath)
		}
		if err := s.docs.Delete(ctx, kindMemFile, f.ID); err != nil {
			return "", errors.New("it could not be renamed")
		}
		f.Path, f.ID = newPath, memID(scope, owner, newPath)
		if err := save(f, "rename"); err != nil {
			return "", err
		}
		return "Renamed " + path + " to " + newPath + ".", nil
	}
	return "", errors.New("unknown command")
}

func pendingNote(pending bool) string {
	if pending {
		return " It was written after reading data the person did not type, so it stays out of the index until they keep it on the Memory page."
	}
	return ""
}

type memFileEvent struct {
	Path    string `json:"path"`
	Action  string `json:"action"`
	Pending bool   `json:"pending"`
}

func (s *Server) auditNoteFile(ctx context.Context, p Principal, action string, f MemFile) {
	ip, _ := ctx.Value(mcpIPKey{}).(string)
	_ = s.audit(ctx, p, ip, "assistant.memory.file", f.Path, action+" ("+strconv.Itoa(len(f.Content))+" characters, "+f.Scope+")", true)
}

// --- the Memory page ---

type memFileJSON struct {
	Path      string    `json:"path"`
	Scope     string    `json:"scope"`
	Content   string    `json:"content"`
	Updated   time.Time `json:"updated"`
	By        string    `json:"by"`
	Pending   bool      `json:"pending"`
	OwnerName string    `json:"owner_name,omitempty"`
	CanEdit   bool      `json:"can_edit"`
}

func (s *Server) handleListMemFiles(w http.ResponseWriter, r *http.Request) {
	p := principalFrom(r.Context())
	all, err := s.allMemFiles(r.Context())
	if err != nil {
		s.internalError(w, "reading memory", err)
		return
	}
	out := []memFileJSON{}
	for _, f := range visibleMemFiles(all, p) {
		out = append(out, memFileJSON{f.Path, f.Scope, f.Content, f.Updated, f.By, f.Pending, f.OwnerName, f.Scope == "me" || p.Role.AtLeast(authn.Admin)})
	}
	writeJSON(w, http.StatusOK, map[string]any{"files": out, "memory_on": s.memoryOn(r.Context())})
}

// handlePutMemFile is a person writing or confirming a file themselves, which is never pending.
func (s *Server) handlePutMemFile(w http.ResponseWriter, r *http.Request) {
	var q struct {
		Path    string  `json:"path"`
		Content *string `json:"content"`
		Keep    bool    `json:"keep"`
	}
	if !readJSON(w, r, &q) {
		return
	}
	p := principalFrom(r.Context())
	scope, parts, err := memPath(q.Path, false)
	if err == nil {
		err = canWriteMem(p, scope, false)
	}
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	path := fullPath(scope, parts)
	owner := ""
	if scope == "me" {
		owner = p.UserID
	}
	id := memID(scope, owner, path)
	var f MemFile
	exists := s.docs.Get(r.Context(), kindMemFile, id, &f) == nil
	if q.Content == nil && !(q.Keep && exists) {
		writeError(w, http.StatusBadRequest, "send the file's content, or keep: true for a file that exists")
		return
	}
	if q.Content != nil {
		text, err := cleanFileText(*q.Content)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		if !exists {
			all, _ := s.allMemFiles(r.Context())
			n := 0
			for _, x := range visibleMemFiles(all, p) {
				if x.Scope == scope {
					n++
				}
			}
			if n >= map[string]int{"me": maxMyFiles, "org": maxOrgFiles}[scope] {
				writeError(w, http.StatusBadRequest, "there are too many files in "+scope+"; delete some first")
				return
			}
			f = MemFile{ID: id, Scope: scope, Owner: owner, OwnerName: p.Name, Path: path, Created: time.Now().UTC()}
		}
		f.Content = text
	}
	f.Pending, f.By, f.Updated = false, "person", time.Now().UTC()
	if err := s.docs.Put(r.Context(), kindMemFile, id, f); err != nil {
		s.internalError(w, "saving the file", err)
		return
	}
	s.auditReq(r, "assistant.memory.file", path, "saved by the person ("+strconv.Itoa(len(f.Content))+" characters, "+scope+")", true)
	writeJSON(w, http.StatusOK, memFileJSON{f.Path, f.Scope, f.Content, f.Updated, f.By, false, f.OwnerName, true})
}

func (s *Server) handleDeleteMemFile(w http.ResponseWriter, r *http.Request) {
	p := principalFrom(r.Context())
	scope, parts, err := memPath(r.URL.Query().Get("path"), false)
	if err == nil {
		err = canWriteMem(p, scope, false)
	}
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	path := fullPath(scope, parts)
	owner := ""
	if scope == "me" {
		owner = p.UserID
	}
	if err := s.docs.Delete(r.Context(), kindMemFile, memID(scope, owner, path)); err != nil {
		writeError(w, http.StatusNotFound, "no such file")
		return
	}
	s.auditReq(r, "assistant.memory.file", path, "deleted by the person", true)
	w.WriteHeader(http.StatusNoContent)
}
