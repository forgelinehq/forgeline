package server

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/forgelinehq/forgeline/internal/authn"
)

func exitWith(code string) error { return exec.Command("sh", "-c", "exit "+code).Run() }

// filesHarness is a sandbox that is on, with a stand-in Docker that knows one file and hands out its bytes.
func filesHarness(t *testing.T) (*mcpHarness, *dockerStub) {
	t.Helper()
	m := newMCPHarness(t)
	d := &dockerStub{runtimes: `{"runc":{"path":"runc"}}`}
	m.srv.run = d.run
	m.admin.expect(200, "PUT", "/v1/assistant/sandbox", map[string]any{"enabled": true})
	known := map[string]string{"/work/report.pdf": "1234", "/work/out/big.zip": "62914560", "/work/notes.html": "20"}
	d.onExec = func(args []string) ([]byte, string, error, bool) {
		if len(args) < 7 || args[len(args)-2] != "sh" || args[len(args)-3] != sandboxFileScript {
			if len(args) >= 4 && args[len(args)-1] == sandboxFileList {
				return []byte("1795500000.5\t1234\t/work/report.pdf\n1795400000.0\t20\t/work/notes.html\nbroken line\n"), "", nil, true
			}
			return nil, "", nil, false
		}
		p := args[len(args)-1]
		switch {
		case strings.HasPrefix(p, "/work/.agent"):
			return nil, "", exitWith("4"), true
		case p == "/work/data":
			return nil, "", exitWith("5"), true
		}
		if size, ok := known[p]; ok {
			return []byte(p + "\n" + size + "\n"), "", nil, true
		}
		return nil, "", exitWith("3"), true
	}
	m.srv.engineExec = func(_ context.Context, name string, args ...string) *exec.Cmd {
		return exec.Command("sh", "-c", "printf %s PDF-BYTES-OF-THE-REPORT")
	}
	return m, d
}

// A person fetches a file from their own sandbox, as themselves: an attachment with sniffing off, only from /work, never the
// agent's home, never someone else's, and with a size limit.
func TestAPersonCanDownloadAFileFromTheirSandboxOnly(t *testing.T) {
	m, d := filesHarness(t)
	resp, body := m.dev.do("GET", "/v1/assistant/files?path=%2Fwork%2Freport.pdf", nil)
	if resp.StatusCode != 200 || string(body) != "PDF-BYTES-OF-THE-REPORT" {
		t.Fatalf("download = %d %q", resp.StatusCode, body)
	}
	for k, want := range map[string]string{"Content-Type": "application/pdf", "X-Content-Type-Options": "nosniff", "Content-Disposition": `attachment; filename=report.pdf`, "Cache-Control": "private, no-store"} {
		if got := resp.Header.Get(k); got != want {
			t.Errorf("%s = %q, want %q", k, got, want)
		}
	}
	if !strings.Contains(resp.Header.Get("Content-Security-Policy"), "sandbox") {
		t.Error("the download has no sandbox policy")
	}
	// A relative name is read from /work; a page a browser would run is sent as plain bytes, not as HTML.
	resp, _ = m.dev.do("GET", "/v1/assistant/files?path=notes.html", nil)
	if resp.StatusCode != 200 || resp.Header.Get("Content-Type") != "application/octet-stream" {
		t.Errorf("html: %d %q", resp.StatusCode, resp.Header.Get("Content-Type"))
	}

	// What it is asked of is the person's own container, and nobody else's.
	devName, _, _ := sandboxNames(Principal{UserID: "dev", Name: "dev"}, defaultSandboxImage)
	for _, c := range d.find("exec") {
		if !strings.Contains(strings.Join(c, " "), "forgeline-sbx-") || strings.Contains(strings.Join(c, " "), "forgeline-sbx-") && !strings.Contains(strings.Join(c, " "), devName[:len("forgeline-sbx-")+12]) {
			t.Errorf("a call went to another person's sandbox or none: %v", c)
		}
	}

	for name, tc := range map[string]struct {
		path string
		want int
	}{
		"outside /work":     {"/etc/passwd", 400},
		"dot dot":           {"/work/../etc/passwd", 400},
		"the agent's home":  {"/work/.agent/.copilot/config.json", 400},
		"a folder":          {"/work/data", 400},
		"no such file":      {"/work/missing.pdf", 404},
		"too big":           {"/work/out/big.zip", 413},
		"control character": {"/work/report.pdf%0a", 400},
		"empty":             {"", 200}, // lists instead
	} {
		resp, body := m.dev.do("GET", "/v1/assistant/files?path="+strings.ReplaceAll(tc.path, "/", "%2F"), nil)
		if resp.StatusCode != tc.want {
			t.Errorf("%s: %d %s, want %d", name, resp.StatusCode, body, tc.want)
		}
	}
	m.v.expect(403, "GET", "/v1/assistant/files?path=%2Fwork%2Freport.pdf", nil)
	aud := string(m.admin.expect(200, "GET", "/v1/audit?limit=100", nil))
	if !strings.Contains(aud, "assistant.file.download") || !strings.Contains(aud, "sandbox/work/report.pdf") {
		t.Error("the download is not in the audit log")
	}
	if !strings.Contains(aud, "sandbox/work/.agent/.copilot/config.json") || !strings.Contains(aud, "refused: only files under /work") {
		t.Error("the attempt on the agent's home is not in the audit log as refused")
	}

	// The list shows what is there, newest first, and skips what it cannot read.
	var list struct {
		Files []sandboxFile `json:"files"`
	}
	json.Unmarshal(m.dev.expect(200, "GET", "/v1/assistant/files", nil), &list)
	if len(list.Files) != 2 || list.Files[0].Path != "/work/report.pdf" || list.Files[0].Size != 1234 || list.Files[1].Name != "notes.html" {
		t.Errorf("list: %+v", list.Files)
	}
	// With the sandbox off there is nothing to hand out.
	m.admin.expect(200, "PUT", "/v1/assistant/sandbox", map[string]any{"enabled": false})
	m.dev.expect(409, "GET", "/v1/assistant/files?path=%2Fwork%2Freport.pdf", nil)
}

// The two shell scripts that decide what leaves the sandbox are run for real, against a folder standing in for /work: a
// symlink that points out, the agent's home, a folder and a path with shell characters all fail, and the bytes are exact.
func TestTheSandboxFileScriptsRefuseWhatIsNotAFileUnderWork(t *testing.T) {
	root := t.TempDir()
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	must(os.MkdirAll(filepath.Join(root, ".agent", "home"), 0o755))
	must(os.MkdirAll(filepath.Join(root, "dir"), 0o755))
	must(os.WriteFile(filepath.Join(root, "report.txt"), []byte("0123456789"), 0o644))
	must(os.WriteFile(filepath.Join(root, ".agent", "home", "token"), []byte("secret"), 0o644))
	outside := filepath.Join(t.TempDir(), "outside.txt")
	must(os.WriteFile(outside, []byte("not yours"), 0o644))
	must(os.Symlink(outside, filepath.Join(root, "escape.txt")))
	must(os.Symlink(filepath.Join(root, ".agent", "home", "token"), filepath.Join(root, "to-home.txt")))
	must(os.Symlink(filepath.Join(root, "report.txt"), filepath.Join(root, "inside.txt")))
	must(os.WriteFile(filepath.Join(root, "a b; echo hi $(id).txt"), []byte("odd"), 0o644))

	scope := func(script string) string { return strings.ReplaceAll(script, "/work", root) }
	run := func(script string, args ...string) (string, int) {
		out, err := exec.Command("sh", append([]string{"-c", scope(script), "sh"}, args...)...).Output()
		code := 0
		if err != nil {
			code = exitCode(err)
		}
		return string(out), code
	}
	for name, tc := range map[string]struct {
		path string
		code int
	}{
		"a plain file":               {root + "/report.txt", 0},
		"a link inside /work":        {root + "/inside.txt", 0},
		"a link that leaves /work":   {root + "/escape.txt", 4},
		"a link into the agent home": {root + "/to-home.txt", 4},
		"the agent's home":           {root + "/.agent/home/token", 4},
		"a folder":                   {root + "/dir", 5},
		"missing":                    {root + "/nothing.txt", 3},
		"shell characters in a name": {root + "/a b; echo hi $(id).txt", 0},
		"dot dot out of /work":       {root + "/../" + filepath.Base(filepath.Dir(outside)) + "/outside.txt", 4},
	} {
		out, code := run(sandboxFileScript, tc.path)
		if code != tc.code {
			t.Errorf("stat %s: exit %d, want %d (%q)", name, code, tc.code, out)
		}
		if tc.code == 0 && !strings.Contains(out, "\n") {
			t.Errorf("stat %s: no size in %q", name, out)
		}
		if _, ccode := run(sandboxFileCat, tc.path, "1000"); ccode != tc.code {
			t.Errorf("cat %s: exit %d, want %d", name, ccode, tc.code)
		}
	}
	if out, _ := run(sandboxFileCat, root+"/report.txt", "1000"); out != "0123456789" {
		t.Errorf("the bytes: %q", out)
	}
	if out, _ := run(sandboxFileCat, root+"/report.txt", "4"); out != "0123" {
		t.Errorf("the limit: %q", out)
	}
}

// The assistant hands a file over with offer_file: the chat gets a Download card; nothing is sent anywhere else, and the tool
// is absent when the sandbox is off.
func TestTheAssistantCanOfferAFileForDownload(t *testing.T) {
	m, d := filesHarness(t)
	_ = d
	evs := m.run(m.dev, map[string]any{"message": "make me a pdf"}, lookupReply(offerFileTool, map[string]any{"path": "/work/report.pdf", "title": "AWS report"}), "The report is ready.")
	var got map[string]any
	for _, f := range evs {
		if f["type"] == "CUSTOM" && f["name"] == "forgeline.file" {
			got = f["value"].(map[string]any)
		}
	}
	if got == nil || got["path"] != "/work/report.pdf" || got["name"] != "report.pdf" || got["size"] != float64(1234) || got["title"] != "AWS report" {
		t.Fatalf("the file card: %v", got)
	}
	if r := results(evs); len(r) != 1 || !strings.Contains(r[0], "Offered report.pdf") {
		t.Errorf("tool result: %v", r)
	}
	if !hasAudit(t, m.admin, "assistant.file.offer") {
		t.Error("the offer is not audited")
	}
	// A file that is not there, or is out of bounds, is refused with the reason, and no card is made.
	for _, p := range []string{"/work/missing.pdf", "/work/.agent/x", "/etc/passwd", "/work/out/big.zip"} {
		evs := m.run(m.dev, map[string]any{"message": "offer it"}, lookupReply(offerFileTool, map[string]any{"path": p}), "Done.")
		for _, f := range evs {
			if f["type"] == "CUSTOM" && f["name"] == "forgeline.file" {
				t.Errorf("a card for %s", p)
			}
		}
		if r := results(evs); len(r) != 1 || !strings.Contains(r[0], "lookup failed") {
			t.Errorf("%s: %v", p, r)
		}
	}
	has := func() bool {
		ctx := context.WithValue(t.Context(), memTurnKey{}, &memTurn{p: Principal{Name: "dev", UserID: "dev", Role: authn.Developer}})
		for _, l := range m.srv.lookupLines(ctx, authn.Developer) {
			if l.name == offerFileTool {
				return true
			}
		}
		return false
	}
	if !has() {
		t.Error("the tool is not offered although the sandbox is on")
	}
	m.admin.expect(200, "PUT", "/v1/assistant/sandbox", map[string]any{"enabled": false})
	if has() {
		t.Error("the tool is offered with the sandbox off")
	}
	evs = m.run(m.dev, map[string]any{"message": "offer it"}, lookupReply(offerFileTool, map[string]any{"path": "/work/report.pdf"}), "Done.")
	if r := results(evs); len(r) != 1 || !strings.Contains(r[0], "the sandbox is off") {
		t.Errorf("with the sandbox off: %v", r)
	}
}

func TestPageAnswerReadsALongAnswerInPieces(t *testing.T) {
	full := strings.Repeat("héllo wörld ", 300) // multi-byte characters, 3600 characters
	var got strings.Builder
	offset, pages := 0, 0
	for {
		out := pageAnswer(full, offset, 1000)
		pages++
		body := out
		if offset > 0 {
			body = strings.SplitN(out, "\n", 2)[1]
		}
		next := -1
		if i := strings.LastIndex(body, "\n[… cut here: "); i >= 0 {
			var more int
			if _, err := fmtSscanf(body[i:], &more, &next); err != nil {
				t.Fatalf("the trailer: %q", body[i:])
			}
			body = body[:i]
		}
		got.WriteString(body)
		if next < 0 {
			break
		}
		offset = next
		if pages > 10 {
			t.Fatal("never ends")
		}
	}
	if got.String() != full || pages != 4 {
		t.Errorf("pages = %d, reassembled = %v", pages, got.String() == full)
	}
	if out := pageAnswer("short", 0, 1000); out != "short" {
		t.Errorf("a short answer: %q", out)
	}
	if out := pageAnswer(full, -5, 10); !strings.HasPrefix(out, "héllo wörl") || strings.Contains(out, "[characters") {
		t.Errorf("a bad offset: %q", out)
	}
	if out := pageAnswer(full, 99999, 10); !strings.HasPrefix(out, "héllo wörl") {
		t.Errorf("an offset past the end starts over: %q", out)
	}
}

// A report longer than one window is read in pieces through forgeline_api, with the next offset in the answer.
func TestForgelineApiReadsALongAnswerWithAnOffset(t *testing.T) {
	m := newMCPHarness(t)
	for _, name := range []string{"a", "b", "c"} {
		m.dev.expect(200, "PUT", "/v1/assistant/memory/files", map[string]any{"path": "/memories/me/" + name + ".md", "content": strings.Repeat(name+" line of text.\n", 480)})
	}
	m.run(m.dev, map[string]any{"message": "read it all"}, lookupReply(forgelineAPITool, map[string]any{"method": "GET", "path": "/v1/assistant/memory/files"}))
	first := m.llm.lastUser(1) // what the model was given (the chat's preview of a result is shorter)
	if !strings.Contains(first, "cut here") || !strings.Contains(first, `"offset": `) || len(first) < 11000 {
		t.Fatalf("first window (%d bytes): %.200q ... %.200q", len(first), first, first[max(0, len(first)-200):])
	}
	var next int
	i := strings.LastIndex(first, `"offset": `)
	if _, err := fmtSscanf2(first[i:], &next); err != nil || next < 5000 {
		t.Fatalf("offset in %q", first[i:])
	}
	m.run(m.dev, map[string]any{"message": "read on"}, lookupReply(forgelineAPITool, map[string]any{"method": "GET", "path": "/v1/assistant/memory/files", "offset": next}))
	second := m.llm.lastUser(1)
	if !strings.Contains(second, fmt.Sprintf("[characters %d to ", next)) {
		t.Fatalf("second window: %.200q", second)
	}
	if first[:600] == second[:600] {
		t.Error("the second window repeats the first")
	}
}

// fmtSscanf reads "[… cut here: N more characters. Call again with \"offset\": M to read on]".
func fmtSscanf(s string, more, next *int) (int, error) {
	return fmt.Sscanf(strings.TrimPrefix(s, "\n"), "[… cut here: %d more characters. Call again with \"offset\": %d to read on]", more, next)
}

func fmtSscanf2(s string, next *int) (int, error) { return fmt.Sscanf(s, "\"offset\": %d", next) }
