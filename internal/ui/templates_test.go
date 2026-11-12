package ui

import (
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/anotherhadi/steep/internal/ui/oppicker"
)

// openRequest creates a request with text and opens it in the editor.
func openRequest(t *testing.T, m *Model, text string) {
	t.Helper()
	if err := m.col.Create("t", text); err != nil {
		t.Fatal(err)
	}
	m.reloadEntries()
	do(m, m.open("t")())
	m.setFocus(paneEditor)
}

func opPicker(m *Model) *oppicker.Model {
	p, _ := m.overlays.Top().(*oppicker.Model)
	return p
}

func TestWrapSelection(t *testing.T) {
	m := newModel(t)
	openRequest(t, m, "GET /admin HTTP/1.1\nHost: h\n\n")
	// select "admin" in visual mode, then ctrl+e
	typeKeys(m, "ggfave")
	key(m, 'e', tea.ModCtrl)
	if opPicker(m) == nil {
		t.Fatalf("no picker:\n%s", screen(m))
	}
	if !strings.Contains(screen(m), "Wrap in an operation") {
		t.Fatalf("title:\n%s", screen(m))
	}
	typeKeys(m, "to-base64")
	key(m, tea.KeyEnter, 0)
	if m.overlays.Top() != nil {
		t.Fatal("the picker stayed open")
	}
	if !strings.HasPrefix(m.editor.Text(), "GET /to-base64{admin} HTTP/1.1") {
		t.Fatalf("text:\n%s", m.editor.Text())
	}
	if !strings.Contains(screen(m), "t · NORMAL") {
		t.Fatalf("mode:\n%s", screen(m))
	}
	// saved, and shown in the bar as written
	if text, _ := m.col.Read("t"); !strings.Contains(text, "to-base64{admin}") {
		t.Fatalf("not saved:\n%s", text)
	}
	if !strings.Contains(screen(m), "GET https://h/to-base64{admin}") {
		t.Fatalf("bar:\n%s", screen(m))
	}
	// one undo takes it back
	typeKeys(m, "u")
	if !strings.HasPrefix(m.editor.Text(), "GET /admin HTTP/1.1") {
		t.Fatalf("undo:\n%s", m.editor.Text())
	}
}

func TestWrapLinesAndEscape(t *testing.T) {
	m := newModel(t)
	openRequest(t, m, "POST / HTTP/1.1\nHost: h\n\n{\"p\": \"a\\\\b\"}\n")
	// the JSON body line, linewise
	typeKeys(m, "GkV")
	key(m, 'e', tea.ModCtrl)
	typeKeys(m, "url-encode")
	key(m, tea.KeyEnter, 0)
	want := `url-encode{\{"p": "a\\\\b"\}}`
	if !strings.Contains(m.editor.Text(), want) {
		t.Fatalf("text:\n%s", m.editor.Text())
	}
	// it sends the JSON as it was written, encoded
	spec, err := m.spec(m.editor.Text())
	if err != nil {
		t.Fatal(err)
	}
	h, err := spec.Prepare()
	if err != nil || strings.TrimSpace(h.Body) != `%7B%22p%22:%20%22a%5C%5Cb%22%7D` {
		t.Fatalf("body %q, %v", h.Body, err)
	}
}

func TestInsertOperation(t *testing.T) {
	m := newModel(t)
	openRequest(t, m, "GET /x HTTP/1.1\nHost: h\n\n")
	// in normal mode, before the character under the cursor, as i does
	typeKeys(m, "gg$")
	key(m, 'e', tea.ModCtrl)
	if !strings.Contains(screen(m), "Insert an operation") {
		t.Fatalf("title:\n%s", screen(m))
	}
	typeKeys(m, "md5")
	key(m, tea.KeyEnter, 0)
	if !strings.Contains(screen(m), "t · INSERT") {
		t.Fatalf("not in insert mode:\n%s", screen(m))
	}
	typeKeys(m, "abc")
	key(m, tea.KeyEscape, 0)
	if !strings.HasPrefix(m.editor.Text(), "GET /x HTTP/1.md5{abc}1") {
		t.Fatalf("text:\n%s", m.editor.Text())
	}
	// from insert mode too, where ctrl+e is not vim's
	typeKeys(m, "Go")
	key(m, 'e', tea.ModCtrl)
	if opPicker(m) == nil {
		t.Fatal("no picker from insert mode")
	}
	typeKeys(m, "to-hex")
	key(m, tea.KeyEnter, 0)
	typeKeys(m, "hi")
	key(m, tea.KeyEscape, 0)
	if !strings.HasSuffix(m.editor.Text(), "to-hex{hi}") {
		t.Fatalf("text:\n%q", m.editor.Text())
	}
}

func TestPickerCancel(t *testing.T) {
	m := newModel(t)
	openRequest(t, m, "GET /admin HTTP/1.1\nHost: h\n\n")
	before := m.editor.Text()
	typeKeys(m, "ggfave")
	key(m, 'e', tea.ModCtrl)
	typeKeys(m, "md5")
	key(m, tea.KeyEscape, 0)
	if m.overlays.Top() != nil || m.editor.Text() != before {
		t.Fatalf("cancel changed things:\n%s", m.editor.Text())
	}
	// the selection is still there: d deletes it
	if !strings.Contains(screen(m), "t · VISUAL") {
		t.Fatalf("selection lost:\n%s", screen(m))
	}
	typeKeys(m, "d")
	if !strings.HasPrefix(m.editor.Text(), "GET / HTTP/1.1") {
		t.Fatalf("text:\n%s", m.editor.Text())
	}
	// no match: enter does nothing
	key(m, 'e', tea.ModCtrl)
	typeKeys(m, "zzzzqqq")
	key(m, tea.KeyEnter, 0)
	if opPicker(m) == nil {
		t.Fatal("picked with no match")
	}
	key(m, tea.KeyEscape, 0)
	// no request open: no picker
	m.editor.Close()
	key(m, 'e', tea.ModCtrl)
	if m.overlays.Top() != nil {
		t.Fatal("picker without a request")
	}
}

func TestPickerFromPalette(t *testing.T) {
	m := newModel(t)
	openRequest(t, m, "GET / HTTP/1.1\nHost: h\n\n")
	key(m, 'p', tea.ModCtrl)
	typeKeys(m, "operation block")
	key(m, tea.KeyEnter, 0)
	if opPicker(m) == nil {
		t.Fatalf("palette did not open the picker:\n%s", screen(m))
	}
}

func TestBlocksCountAndErrors(t *testing.T) {
	m := newModel(t)
	wl := filepath.Join(t.TempDir(), "w.txt")
	os.WriteFile(wl, []byte("a\nb\n"), 0o644)
	openRequest(t, m, "GET /to-base64{file{"+wl+"}}?x=md5{a} HTTP/1.1\nHost: h\n\n")
	drainCount(m)
	if !strings.Contains(screen(m), "2 req") {
		t.Fatalf("count:\n%s", screen(m))
	}
	// an operation failing once per run shows in the footer, and stops the run
	m.editor.SetText("GET /from-base64(strict){!!} HTTP/1.1\nHost: h\n\n")
	do(m, nil)
	drainCount(m)
	if !strings.Contains(screen(m), "from-base64: invalid Base64") {
		t.Fatalf("no error in the footer:\n%s", screen(m))
	}
	// (the error's toast would hold drain for its whole life)
	m.startRun()
	if m.fz != nil {
		t.Fatal("ran despite the error")
	}
	// syntax errors, with the line in the file
	m.editor.SetText("# steep id: x\nGET /to-hex(bad){a} HTTP/1.1\nHost: h\n\n")
	do(m, nil)
	drainCount(m)
	if !strings.Contains(screen(m), `unknown argument "bad"`) || m.count.err == nil || !strings.Contains(m.count.err.Error(), "line 2, column 13") {
		t.Fatalf("no syntax error: %v\n%s", m.count.err, screen(m))
	}
}

// drainCount waits for the background count.
func drainCount(m *Model) {
	if cmd := m.recount(); cmd != nil {
		do(m, cmd())
	}
}

type recorded struct {
	mu   sync.Mutex
	reqs []*http.Request
	body []string
}

func (r *recorded) server(t *testing.T) *httptest.Server {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		b := new(strings.Builder)
		buf := make([]byte, 1024)
		for {
			n, err := req.Body.Read(buf)
			b.Write(buf[:n])
			if err != nil {
				break
			}
		}
		r.mu.Lock()
		r.reqs = append(r.reqs, req)
		r.body = append(r.body, b.String())
		r.mu.Unlock()
	}))
	t.Cleanup(srv.Close)
	return srv
}

func runToEnd(t *testing.T, m *Model) {
	t.Helper()
	typeKeys(m, "s")
	if m.fz == nil {
		t.Fatalf("not running; notes: %+v\n%s", m.notes.History(), screen(m))
	}
	for deadline := time.Now().Add(10 * time.Second); m.fz != nil && time.Now().Before(deadline); {
		time.Sleep(30 * time.Millisecond)
		do(m, tickMsg{})
	}
	if m.fz != nil {
		t.Fatal("the run did not end")
	}
}

func TestRunWithOperations(t *testing.T) {
	var rec recorded
	srv := rec.server(t)
	host := strings.TrimPrefix(srv.URL, "http://")
	dir := t.TempDir()
	users := filepath.Join(dir, "users.txt")
	os.WriteFile(users, []byte("admin\nr00t?\n"), 0o644)
	m := newModel(t)
	openRequest(t, m, "# steep proto: http\n"+
		"POST /u/url-encode(encode-all){to-base64{file{"+users+"}}}?s=to-hex{static} HTTP/1.1\n"+
		"Host: "+host+"\n"+
		"X-User: to-upper-case{file{"+users+"}}\n"+
		"Content-Type: application/json\n\n"+
		`{"u": "file{`+users+`}", "h": "md5{x}", "raw": "a\\b"}`)
	drainCount(m)
	if !strings.Contains(screen(m), "8 req") {
		t.Fatalf("count (2×2×2):\n%s", screen(m))
	}
	runToEnd(t, m)
	if len(rec.reqs) != 8 || m.results.Hits() != 8 {
		t.Fatalf("got %d requests, %d hits; notes %+v", len(rec.reqs), m.results.Hits(), m.notes.History())
	}
	var seen []string
	for i, r := range rec.reqs {
		if r.URL.Query().Get("s") != "737461746963" {
			t.Errorf("static block: %q", r.URL.RawQuery)
		}
		enc := strings.TrimPrefix(r.URL.EscapedPath(), "/u/")
		user, err := base64.StdEncoding.DecodeString(strings.NewReplacer("%3D", "=", "%2B", "+", "%2F", "/").Replace(enc))
		if err != nil {
			t.Errorf("path %q: %v", r.URL.EscapedPath(), err)
		}
		up := r.Header.Get("X-User")
		if up != "ADMIN" && up != "R00T?" {
			t.Errorf("header: %q", up)
		}
		body := rec.body[i]
		if !strings.Contains(body, `"h": "9dd4e461268c8034f5c8564e155c67a6"`) || !strings.Contains(body, `"raw": "a\\b"`) {
			t.Errorf("body: %q", body)
		}
		seen = append(seen, string(user)+"|"+up+"|"+body[8:13])
	}
	slices.Sort(seen)
	seen = slices.Compact(seen)
	if len(seen) != 8 {
		t.Errorf("not every combination was sent: %q", seen)
	}
	// the bar shows the blocks as written
	if !strings.Contains(screen(m), "url-encode(encode-all){to-base64{file{") {
		t.Errorf("bar:\n%s", screen(m))
	}
}

func TestRunOperationFailures(t *testing.T) {
	var rec recorded
	srv := rec.server(t)
	host := strings.TrimPrefix(srv.URL, "http://")
	wl := filepath.Join(t.TempDir(), "w.txt")
	os.WriteFile(wl, []byte("aGk=\n!!\n?\nb2s=\n"), 0o644)
	m := newModel(t)
	openRequest(t, m, "# steep proto: http\nGET /from-base64(strict){file{"+wl+"}} HTTP/1.1\nHost: "+host+"\n\n")
	runToEnd(t, m)
	var paths []string
	for _, r := range rec.reqs {
		paths = append(paths, r.URL.Path)
	}
	slices.Sort(paths)
	if !slices.Equal(paths, []string{"/hi", "/ok"}) {
		t.Fatalf("sent %q", paths)
	}
	// one warning for all, naming the first failure; no message for each
	var failed []string
	for _, n := range m.notes.History() {
		if strings.Contains(n.Text, "failed") || strings.Contains(n.Text, "built") || strings.Contains(n.Text, "preparing") {
			failed = append(failed, n.Text)
		}
	}
	// told apart from network failures: none here
	if len(failed) != 1 || !strings.Contains(failed[0], "2 of 4 requests could not be built") || !strings.Contains(failed[0], "invalid Base64") {
		t.Fatalf("notes: %q", failed)
	}
}

func TestPickerKeepsSelectionAcrossReload(t *testing.T) {
	m := newModel(t)
	openRequest(t, m, "GET /admin HTTP/1.1\nHost: h\n\n")
	typeKeys(m, "ggfave")
	key(m, 'e', tea.ModCtrl)
	// the file changes on disk while the picker is open
	m.editor.SetText("GET /other HTTP/1.1\nHost: h\n\n")
	typeKeys(m, "md5")
	key(m, tea.KeyEnter, 0)
	key(m, tea.KeyEscape, 0)
	if strings.Count(m.editor.Text(), "md5{") != 1 || strings.Contains(m.editor.Text(), "md5{admin}") {
		t.Fatalf("text:\n%s", m.editor.Text())
	}
}
