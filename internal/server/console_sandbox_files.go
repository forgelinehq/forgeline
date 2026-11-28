package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"path"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Files out of the sandbox (design note 052). The assistant builds things in the person's sandbox (a PDF, a CSV, a report) and
// until now had no way to hand them over: the sandbox has no route out and no credentials, by design. A person can fetch a
// file from their own /work folder, through the console, as themselves: the server reads it from the sandbox with `docker exec`
// and sends it to the signed-in browser as a download. The assistant offers one with `offer_file`; the chat shows a Download
// button. Nothing leaves the sandbox on its own, and nothing goes anywhere but to the person it belongs to.

const (
	offerFileTool    = "offer_file"
	maxDownloadBytes = 50 << 20
	maxSandboxPath   = 400
	maxListedFiles   = 200
)

// sandboxFileScript resolves a path inside the container and refuses anything that is not a regular file under /work (the
// agent's own home, /work/.agent, holds its credentials and sessions and is never handed out). It prints the resolved path and
// the size. The path is a positional argument, never part of the script, so it cannot change what the script does.
const sandboxFileScript = `p=$(realpath -e -- "$1" 2>/dev/null) || exit 3
case "$p" in /work/.agent|/work/.agent/*) exit 4;; /work/*) ;; *) exit 4;; esac
[ -f "$p" ] || exit 5
printf '%s\n' "$p"
wc -c < "$p"`

// sandboxFileCat is the same check, then the file's first $2 bytes, in one process so the file cannot change in between.
const sandboxFileCat = `p=$(realpath -e -- "$1" 2>/dev/null) || exit 3
case "$p" in /work/.agent|/work/.agent/*) exit 4;; /work/*) ;; *) exit 4;; esac
[ -f "$p" ] || exit 5
exec head -c "$2" -- "$p"`

// sandboxFileList lists regular files under /work, newest first, leaving out the agent's home and anything hidden.
const sandboxFileList = `find /work -maxdepth 4 -type f -not -path '/work/.*' -not -path '*/.*' -printf '%T@\t%s\t%p\n' 2>/dev/null | sort -rn | head -n 200`

// cleanSandboxPath makes a path absolute under /work and refuses what cannot be one. The container decides the rest.
func cleanSandboxPath(p string) (string, error) {
	switch {
	case strings.TrimSpace(p) == "":
		return "", errors.New("give the file's path, such as /work/report.pdf")
	case len(p) > maxSandboxPath || strings.ContainsAny(p, "\x00\r\n"):
		return "", errors.New("the path is too long or has a control character in it") // checked before trimming, so a trailing newline is refused too
	}
	p = strings.TrimSpace(p)
	if !strings.HasPrefix(p, "/") {
		p = "/work/" + p
	}
	p = path.Clean(p)
	if p != "/work" && !strings.HasPrefix(p, "/work/") {
		return "", errors.New("only files under /work can be handed out")
	}
	return p, nil
}

// sandboxFileContainer is a running container of the person's that has their /work folder, starting the plain shell one if
// none is up.
func (s *Server) sandboxFileContainer(ctx context.Context, p Principal) (string, error) {
	st := s.loadSandbox(ctx)
	if !st.Enabled {
		return "", errors.New("the sandbox is off, so there are no files to hand out")
	}
	for _, image := range []string{defaultSandboxImage, engineImage} {
		name, _, _ := sandboxNames(p, image)
		if out, _, err := s.run(ctx, "docker", "inspect", "-f", "{{.State.Running}}", name); err == nil && strings.TrimSpace(string(out)) == "true" {
			return name, nil
		}
	}
	return s.ensureSandbox(ctx, p, defaultSandboxImage, st)
}

type sandboxFile struct {
	Path string `json:"path"`
	Name string `json:"name"`
	Size int64  `json:"size"`
}

var errNoSuchSandboxFile = errors.New("there is no such file in your sandbox's /work folder")

// statSandboxFile asks the container about one file.
func (s *Server) statSandboxFile(ctx context.Context, ctr, p string) (sandboxFile, error) {
	out, stderr, err := s.run(ctx, "docker", "exec", ctr, "sh", "-c", sandboxFileScript, "sh", p)
	if err != nil {
		switch exitCode(err) {
		case 3:
			return sandboxFile{}, errNoSuchSandboxFile
		case 4:
			return sandboxFile{}, errors.New("only files under /work can be handed out, and not the agent's own home folder (/work/.agent)")
		case 5:
			return sandboxFile{}, errors.New("that is not a regular file")
		}
		return sandboxFile{}, fmt.Errorf("asking the sandbox: %s", dockerError(stderr, err))
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	if len(lines) != 2 {
		return sandboxFile{}, errors.New("the sandbox gave an answer I could not read")
	}
	size, err := strconv.ParseInt(strings.TrimSpace(lines[1]), 10, 64)
	if err != nil {
		return sandboxFile{}, errors.New("the sandbox gave a size I could not read")
	}
	return sandboxFile{Path: lines[0], Name: path.Base(lines[0]), Size: size}, nil
}

// exitCode is the exit status of a failed command, or 0 when it is not one.
func exitCode(err error) int {
	var ee interface{ ExitCode() int }
	if errors.As(err, &ee) {
		return ee.ExitCode()
	}
	return 0
}

// downloadType picks what a file is sent as. A type a browser would run (HTML, SVG, scripts) goes as plain bytes, and every
// download is an attachment with sniffing off, so an agent-made file cannot run in the console's origin.
func downloadType(name string) string {
	switch strings.ToLower(path.Ext(name)) {
	case ".pdf":
		return "application/pdf"
	case ".png":
		return "image/png"
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".gif":
		return "image/gif"
	case ".csv":
		return "text/csv; charset=utf-8"
	case ".txt", ".log", ".md":
		return "text/plain; charset=utf-8"
	case ".json":
		return "application/json"
	case ".zip":
		return "application/zip"
	case ".xlsx":
		return "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"
	case ".docx":
		return "application/vnd.openxmlformats-officedocument.wordprocessingml.document"
	}
	return "application/octet-stream"
}

// handleSandboxFiles lists the person's files, or with ?path= sends one.
func (s *Server) handleSandboxFiles(w http.ResponseWriter, r *http.Request) {
	ctx, p := r.Context(), principalFrom(r.Context())
	if p.UserID == "" {
		writeError(w, http.StatusForbidden, "files belong to a signed-in person")
		return
	}
	ctr, err := s.sandboxFileContainer(ctx, p)
	if err != nil {
		writeError(w, http.StatusConflict, err.Error())
		return
	}
	if r.URL.Query().Get("path") == "" {
		out, stderr, err := s.run(ctx, "docker", "exec", ctr, "sh", "-c", sandboxFileList)
		if err != nil {
			writeError(w, http.StatusBadGateway, "asking the sandbox: "+dockerError(stderr, err))
			return
		}
		files := []map[string]any{}
		for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
			parts := strings.SplitN(line, "\t", 3)
			if len(parts) != 3 || len(files) >= maxListedFiles {
				continue
			}
			size, _ := strconv.ParseInt(parts[1], 10, 64)
			secs, _ := strconv.ParseFloat(parts[0], 64)
			files = append(files, map[string]any{"path": parts[2], "name": path.Base(parts[2]), "size": size, "modified": time.Unix(int64(secs), 0).UTC()})
		}
		writeJSON(w, http.StatusOK, map[string]any{"files": files})
		return
	}
	want, err := cleanSandboxPath(r.URL.Query().Get("path"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	f, err := s.statSandboxFile(ctx, ctr, want)
	switch {
	case errors.Is(err, errNoSuchSandboxFile):
		writeError(w, http.StatusNotFound, err.Error())
		return
	case err != nil:
		// An attempt to read outside /work, or the agent's home, is worth a line: someone (or something) tried.
		s.auditReq(r, "assistant.file.download", "sandbox"+want, "refused: "+truncate(err.Error(), 160), false)
		writeError(w, http.StatusBadRequest, err.Error())
		return
	case f.Size > maxDownloadBytes:
		writeError(w, http.StatusRequestEntityTooLarge, fmt.Sprintf("%s is %d MB; at most %d MB can be handed out (split it or zip it smaller)", f.Name, f.Size>>20, maxDownloadBytes>>20))
		return
	}
	cmd := s.engineCmd(ctx, "docker", "exec", ctr, "sh", "-c", sandboxFileCat, "sh", f.Path, strconv.FormatInt(f.Size, 10))
	pipe, err := cmd.StdoutPipe()
	if err != nil {
		s.internalError(w, "reading the file", err)
		return
	}
	if err := cmd.Start(); err != nil {
		s.internalError(w, "reading the file", err)
		return
	}
	h := w.Header()
	h.Set("Content-Type", downloadType(f.Name))
	h.Set("Content-Length", strconv.FormatInt(f.Size, 10))
	h.Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": f.Name}))
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Content-Security-Policy", "default-src 'none'; sandbox")
	h.Set("Cache-Control", "private, no-store")
	n, copyErr := io.Copy(w, io.LimitReader(pipe, f.Size))
	waitErr := cmd.Wait()
	ok := copyErr == nil && waitErr == nil && n == f.Size
	s.auditReq(r, "assistant.file.download", "sandbox"+f.Path, fmt.Sprintf("%d bytes", n), ok)
}

const maxNamedOffers = 5

// mayNameAFile is whether an answer is worth looking at the sandbox for: it holds a path under /work or something like
// "report.pdf".
var mayNameAFile = regexp.MustCompile(`/work/|[\w-]\.[A-Za-z][A-Za-z0-9]{1,7}\b`)

// offerNamedFiles offers what the assistant made and named without offering it: a file under /work that changed during this
// message and that the answer mentions. It only looks when the sandbox is already running, and starts nothing.
func (s *Server) offerNamedFiles(ctx context.Context, p Principal, since time.Time, answer string, sink *lookupSink) {
	if p.UserID == "" || !mayNameAFile.MatchString(answer) || !s.loadSandbox(ctx).Enabled {
		return
	}
	ctr := ""
	for _, image := range []string{defaultSandboxImage, engineImage} {
		name, _, _ := sandboxNames(p, image)
		if out, _, err := s.run(ctx, "docker", "inspect", "-f", "{{.State.Running}}", name); err == nil && strings.TrimSpace(string(out)) == "true" {
			ctr = name
			break
		}
	}
	if ctr == "" {
		return
	}
	out, _, err := s.run(ctx, "docker", "exec", ctr, "sh", "-c", sandboxFileList)
	if err != nil {
		return
	}
	// A file's time can be a little coarser than the clock the message was timed with.
	changed := changedFiles(string(out), since.Add(-time.Second))
	names := map[string]int{}
	for _, f := range changed {
		names[f.Name]++
	}
	offered := map[string]bool{}
	for _, o := range sink.offers {
		offered[o.Path] = true
	}
	added := 0
	for _, f := range changed {
		rel := strings.TrimPrefix(f.Path, "/work/")
		// A bare name counts only with an extension, and only when one changed file has it.
		named := mentions(answer, f.Path) || strings.Contains(rel, "/") && mentions(answer, rel) ||
			path.Ext(f.Name) != "" && names[f.Name] == 1 && mentions(answer, f.Name)
		if !named || offered[f.Path] || f.Size > maxDownloadBytes || added == maxNamedOffers {
			continue
		}
		added++
		sink.offers = append(sink.offers, fileOffer{sandboxFile: f})
		_ = s.audit(ctx, p, "", "assistant.file.offer", "sandbox"+f.Path, fmt.Sprintf("%d bytes, named in the answer", f.Size), true)
	}
}

// changedFiles reads the output of sandboxFileList and keeps the files changed at or after since.
func changedFiles(list string, since time.Time) []sandboxFile {
	var files []sandboxFile
	for _, line := range strings.Split(list, "\n") {
		parts := strings.SplitN(line, "\t", 3)
		if len(parts) != 3 {
			continue
		}
		secs, err := strconv.ParseFloat(parts[0], 64)
		size, serr := strconv.ParseInt(parts[1], 10, 64)
		if p, perr := cleanSandboxPath(parts[2]); err == nil && serr == nil && perr == nil && secs >= float64(since.UnixNano())/1e9 {
			files = append(files, sandboxFile{Path: p, Name: path.Base(p), Size: size})
		}
	}
	return files
}

// mentions says whether text names this file and not a longer name or path that contains it.
func mentions(text, name string) bool {
	part := func(b byte) bool {
		return b == '_' || b == '-' || b == '/' || b >= '0' && b <= '9' || b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z'
	}
	for from := 0; ; {
		i := strings.Index(text[from:], name)
		if i < 0 {
			return false
		}
		start, end := from+i, from+i+len(name)
		// A full stop ends a sentence unless more of a name follows it.
		before := start == 0 || !part(text[start-1]) && text[start-1] != '.'
		after := end == len(text) || !part(text[end]) && !(text[end] == '.' && end+1 < len(text) && part(text[end+1]))
		if before && after {
			return true
		}
		from = start + 1
	}
}

// lookupOfferFile is the tool: it checks the file is there and shows the person a Download button.
func (s *Server) lookupOfferFile(ctx context.Context, raw json.RawMessage) (string, error) {
	t := memTurnFrom(ctx)
	if t == nil || scopeFrom(ctx) != nil || t.p.UserID == "" {
		return "", errors.New("offer_file is only available when a person is chatting with the assistant")
	}
	var a struct {
		Path  string `json:"path"`
		Title string `json:"title"`
	}
	if err := decodeArgs(raw, &a); err != nil {
		return "", err
	}
	want, err := cleanSandboxPath(a.Path)
	if err != nil {
		return "", err
	}
	ctr, err := s.sandboxFileContainer(ctx, t.p)
	if err != nil {
		return "", err
	}
	f, err := s.statSandboxFile(ctx, ctr, want)
	if err != nil {
		return "", err
	}
	if f.Size > maxDownloadBytes {
		return "", fmt.Errorf("%s is %d MB, over the %d MB that can be handed out; make it smaller first", f.Name, f.Size>>20, maxDownloadBytes>>20)
	}
	if sink, ok := ctx.Value(lookupSinkKey{}).(*lookupSink); ok {
		sink.offers = append(sink.offers, fileOffer{sandboxFile: f, Title: truncate(strings.TrimSpace(a.Title), 120)})
	}
	_ = s.audit(ctx, t.p, "", "assistant.file.offer", "sandbox"+f.Path, fmt.Sprintf("%d bytes", f.Size), true)
	return fmt.Sprintf("Offered %s (%d bytes). The person sees a Download button for it in the chat; tell them what is in it. Nothing was sent anywhere else.", f.Name, f.Size), nil
}

// fileOffer is a file the assistant has offered, for the card.
type fileOffer struct {
	sandboxFile
	Title string `json:"title,omitempty"`
}
