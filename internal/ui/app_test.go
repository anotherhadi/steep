package ui

import (
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/sectui/foam"
	fconfig "github.com/sectui/foam/config"
	"github.com/sectui/foam/theme"

	"github.com/sectui/steep/internal/config"
	"github.com/sectui/steep/internal/remote"
)

func init() { log.SetOutput(io.Discard) }

func newModel(t *testing.T) *Model {
	t.Helper()
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())
	cfg, err := fconfig.LoadBytes[config.Config](foam.Defaults, config.Defaults)
	if err != nil {
		t.Fatal(err)
	}
	cfg.Steep.Collection = t.TempDir()
	cfg.Steep.ConfirmAbove = 0
	m, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(m.Close)
	m.Update(tea.WindowSizeMsg{Width: 140, Height: 40})
	return m
}

// do runs msg through Update, then the commands it returns, as bubbletea
// would (ticks and pickers excepted).
func do(m *Model, msg tea.Msg) {
	_, cmd := m.Update(msg)
	drain(m, cmd, 0)
}

func drain(m *Model, cmd tea.Cmd, depth int) {
	if cmd == nil || depth > 20 {
		return
	}
	msg := cmd()
	switch msg := msg.(type) {
	case tea.BatchMsg:
		for _, c := range msg {
			drain(m, c, depth+1)
		}
	case tickMsg, nil:
	default:
		if v := reflect.ValueOf(msg); v.Kind() == reflect.Slice && v.Type().Elem() == reflect.TypeFor[tea.Cmd]() {
			for i := range v.Len() {
				drain(m, v.Index(i).Interface().(tea.Cmd), depth+1)
			}
			return
		}
		_, next := m.Update(msg)
		drain(m, next, depth+1)
	}
}

func typeKeys(m *Model, s string) {
	for _, r := range s {
		do(m, tea.KeyPressMsg{Code: r, Text: string(r)})
	}
}

func key(m *Model, code rune, mod tea.KeyMod) { do(m, tea.KeyPressMsg{Code: code, Mod: mod}) }

func screen(m *Model) string { return ansi.Strip(m.View().Content) }

func TestNewRequestAndEdit(t *testing.T) {
	m := newModel(t)
	if !strings.Contains(screen(m), "n: new request") {
		t.Fatalf("empty editor:\n%s", screen(m))
	}
	key(m, '1', tea.ModAlt) // to the collection
	typeKeys(m, "n")
	if m.overlays.Top() == nil {
		t.Fatal("no prompt for the name")
	}
	typeKeys(m, "api/login")
	key(m, tea.KeyEnter, 0)
	if m.editor.Name() != "api/login" || m.focus != paneEditor {
		t.Fatalf("open = %q, focus = %v", m.editor.Name(), m.focus)
	}
	// vim: go to the Host line and change it
	typeKeys(m, "2GccHost: api.test")
	key(m, tea.KeyEscape, 0)
	text, _ := m.col.Read("api/login")
	if !strings.Contains(text, "Host: api.test") {
		t.Fatalf("not saved as typed:\n%s", text)
	}
	if !strings.Contains(screen(m), "GET https://api.test/") {
		t.Fatalf("bar does not follow the request:\n%s", screen(m))
	}
	if !strings.Contains(screen(m), "api/login · NORMAL") {
		t.Fatalf("title:\n%s", screen(m))
	}
}

func TestPasteCurl(t *testing.T) {
	m := newModel(t)
	do(m, tea.PasteMsg{Content: "curl -X POST 'http://shop.test/cart/add' -d 'id=1'"})
	if m.overlays.Top() == nil {
		t.Fatal("no prompt")
	}
	key(m, tea.KeyEnter, 0)
	if m.editor.Name() != "shop.test/cart-add" {
		t.Fatalf("name = %q", m.editor.Name())
	}
	if !strings.HasPrefix(m.editor.Text(), "# steep proto: http\nPOST /cart/add") {
		t.Fatalf("text:\n%s", m.editor.Text())
	}
}

func TestRunEndToEnd(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/admin" {
			w.WriteHeader(403)
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"path":%q}`, r.URL.Path)
	}))
	defer srv.Close()
	m := newModel(t)
	wl := filepath.Join(t.TempDir(), "words.txt")
	os.WriteFile(wl, []byte("a\nadmin\nb\n"), 0o644)
	host := strings.TrimPrefix(srv.URL, "http://")
	text := "# steep proto: http\nGET /file{" + wl + "} HTTP/1.1\nHost: " + host + "\n\n"
	if err := m.col.Create("t", text); err != nil {
		t.Fatal(err)
	}
	m.reloadEntries()
	do(m, m.open("t")())
	if !strings.Contains(screen(m), "3 req") {
		t.Fatalf("count:\n%s", screen(m))
	}
	typeKeys(m, "s")
	if m.fz == nil {
		t.Fatalf("not running; notes: %+v", m.notes.History())
	}
	for deadline := time.Now().Add(5 * time.Second); m.fz != nil && time.Now().Before(deadline); {
		time.Sleep(50 * time.Millisecond)
		do(m, tickMsg{})
	}
	if m.fz != nil || m.results.Hits() != 3 {
		t.Fatalf("hits = %d, notes = %+v", m.results.Hits(), m.notes.History())
	}
	if !strings.Contains(screen(m), "Results (3)") {
		t.Fatalf("results pane:\n%s", screen(m))
	}
	// filter with HTTPQL
	key(m, '3', tea.ModAlt)
	typeKeys(m, "/resp.code.eq:403")
	key(m, tea.KeyEnter, 0)
	if !strings.Contains(screen(m), "Results (1/3)") || !strings.Contains(screen(m), `"path": "/admin"`) {
		t.Fatalf("filtered:\n%s", screen(m))
	}
	if !strings.Contains(screen(m), "# input: admin") || strings.Contains(screen(m), "· admin") {
		t.Fatalf("inputs not over the response:\n%s", screen(m))
	}
	// a raw filter runs in the background
	key(m, 'g', tea.ModCtrl)
	typeKeys(m, `/"/b"`)
	key(m, tea.KeyEnter, 0)
	if !strings.Contains(screen(m), "Results (1/3)") || !strings.Contains(screen(m), `"path": "/b"`) {
		t.Fatalf("raw filter:\n%s", screen(m))
	}
	// the run is kept: reopening the request shows it
	do(m, m.open("t")())
	if m.results.Hits() != 3 {
		t.Fatalf("kept hits = %d", m.results.Hits())
	}
	// running again replaces the run, from the request pane
	key(m, '2', tea.ModAlt)
	key(m, 'n', tea.ModCtrl)
	for deadline := time.Now().Add(5 * time.Second); m.fz != nil && time.Now().Before(deadline); {
		time.Sleep(50 * time.Millisecond)
		do(m, tickMsg{})
	}
	if m.results.Hits() != 3 {
		t.Fatalf("second run: hits = %d", m.results.Hits())
	}
}

func TestHelpAndSettings(t *testing.T) {
	m := newModel(t)
	typeKeys(m, "?")
	if !strings.Contains(screen(m), "Help") {
		t.Fatal("no help")
	}
	key(m, tea.KeyEscape, 0)
	typeKeys(m, "n")
	typeKeys(m, "r")
	key(m, tea.KeyEnter, 0)

	// from the palette, the settings: rate limit is the fourth row
	key(m, 'p', tea.ModCtrl)
	typeKeys(m, "settings")
	key(m, tea.KeyEnter, 0)
	if !strings.Contains(screen(m), "Settings · r") {
		t.Fatalf("palette did not open the settings:\n%s", screen(m))
	}
	typeKeys(m, "jjj")
	key(m, tea.KeyEnter, 0)
	typeKeys(m, "50")
	key(m, tea.KeyEnter, 0)
	if text, _ := m.col.Read("r"); !strings.Contains(text, "# steep rateLimit: 50\n") {
		t.Fatalf("not written in the request:\n%s", text)
	}
	typeKeys(m, "d")
	if text, _ := m.col.Read("r"); strings.Contains(text, "rateLimit") {
		t.Fatalf("d did not go back to the default:\n%s", text)
	}
}

func TestImportAtStart(t *testing.T) {
	m := newModel(t)
	m.Import("GET /x HTTP/1.1\nHost: a.test\n\n")
	m.watch = nil // Init would wait on it, and on the socket
	m.remote.Close()
	m.remote = nil
	drain(m, m.Init(), 0)
	if m.overlays.Top() == nil {
		t.Fatal("no prompt for the name")
	}
	key(m, tea.KeyEnter, 0)
	if m.editor.Name() != "a.test/x" {
		t.Fatalf("name = %q", m.editor.Name())
	}
}

func TestOnRunEnd(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer srv.Close()
	m := newModel(t)
	out := filepath.Join(t.TempDir(), "out")
	m.cfg.Steep.OnRunEnd = `echo "$RUN_REQUEST $RUN_STATUS $RUN_HITS $RUN_DONE/$RUN_TOTAL $RUN_ERRORS" > ` + out +
		` && test -f "$RUN_DIR/run.json"`
	text := "# steep proto: http\nGET / HTTP/1.1\nHost: " + strings.TrimPrefix(srv.URL, "http://") + "\n\n"
	if err := m.col.Create("t", text); err != nil {
		t.Fatal(err)
	}
	do(m, m.open("t")())
	typeKeys(m, "s")
	for deadline := time.Now().Add(5 * time.Second); m.fz != nil && time.Now().Before(deadline); {
		time.Sleep(50 * time.Millisecond)
		do(m, tickMsg{})
	}
	b, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("%v; notes = %+v", err, m.notes.History())
	}
	if got := strings.TrimSpace(string(b)); got != "t finished 1 1/1 0" {
		t.Fatalf("got %q", got)
	}
	m.cfg.Steep.OnRunEnd = "echo oops >&2; exit 3"
	typeKeys(m, "s")
	for deadline := time.Now().Add(5 * time.Second); m.fz != nil && time.Now().Before(deadline); {
		time.Sleep(50 * time.Millisecond)
		do(m, tickMsg{})
	}
	if h := fmt.Sprint(m.notes.History()); !strings.Contains(h, "exit status 3: oops") {
		t.Fatalf("notes = %s", h)
	}
}

func TestDuplicateAndPath(t *testing.T) {
	m := newModel(t)
	text := "# steep id: abc\n# steep mode: pitchfork\nPOST  /login?x=1 HTTP/1.1\nHost: api.test\n\n"
	if err := m.col.Create("api/login", text); err != nil {
		t.Fatal(err)
	}
	m.reloadEntries()
	do(m, m.open("api/login")())
	// one request: no count in the border
	if strings.Contains(screen(m), "1 req") {
		t.Fatalf("count shown for one request:\n%s", screen(m))
	}
	typeKeys(m, "U")
	if p := m.editor.CursorPos(); p.Line != 2 || p.Col != 6 {
		t.Fatalf("go to path: cursor at %+v", p)
	}

	key(m, '1', tea.ModAlt)
	typeKeys(m, "d")
	if m.overlays.Top() == nil {
		t.Fatal("no prompt for the copy's name")
	}
	key(m, tea.KeyEnter, 0)
	if m.editor.Name() != "api/login-copy" {
		t.Fatalf("open = %q", m.editor.Name())
	}
	got, err := m.col.Read("api/login-copy")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(got, "id: abc") || !strings.Contains(got, "# steep mode: pitchfork\nPOST  /login?x=1") {
		t.Fatalf("copy:\n%s", got)
	}
	if orig, _ := m.col.Read("api/login"); orig != text {
		t.Fatalf("original changed:\n%s", orig)
	}
}

func TestClickCollectionFocusesFirst(t *testing.T) {
	m := newModel(t)
	for _, name := range []string{"a", "b"} {
		m.col.Create(name, "GET / HTTP/1.1\nHost: h\n\n")
	}
	m.reloadEntries()
	do(m, m.open("a")())
	m.setFocus(paneEditor)
	// the row of "b" on screen
	y := -1
	for i, l := range strings.Split(screen(m), "\n") {
		if strings.Contains(l, " b ") && strings.HasPrefix(l, "│") {
			y = i
		}
	}
	if y < 0 {
		t.Fatalf("no row for b:\n%s", screen(m))
	}
	click := tea.MouseClickMsg{X: 5, Y: y, Button: tea.MouseLeft}
	do(m, click)
	if m.focus != paneCollection || m.editor.Name() != "a" {
		t.Fatalf("first click: focus %v, open %q", m.focus, m.editor.Name())
	}
	if e, _ := m.tree.Selected(); e.Name != "b" {
		t.Fatalf("cursor not on the clicked row: %q", e.Name)
	}
	// once focused, a click opens the request
	do(m, click)
	if m.editor.Name() != "b" {
		t.Fatalf("second click: open %q", m.editor.Name())
	}
}

func TestHeaderIcon(t *testing.T) {
	m := newModel(t)
	if strings.Contains(m.topBar(), "󰶞") {
		t.Fatal("icon without nerd fonts")
	}
	th := theme.Current()
	t.Cleanup(func() { theme.Set(th) })
	nerd := th
	nerd.NerdFonts = true
	theme.Set(nerd)
	if !strings.Contains(ansi.Strip(m.topBar()), "󰶞 steep") {
		t.Fatalf("bar: %q", ansi.Strip(m.topBar()))
	}
}

// A change on disk reloads the open request, unless it holds unsaved edits.
func TestDiskChangeKeepsUnsavedEdits(t *testing.T) {
	m := newModel(t)
	if err := m.col.Create("a", newRequest); err != nil {
		t.Fatal(err)
	}
	m.open("a")
	external := "GET /external HTTP/1.1\nHost: example.com\n\n"
	if err := m.col.Write("a", external); err != nil {
		t.Fatal(err)
	}
	m.diskChanged()
	if m.editor.Text() != external {
		t.Fatalf("not reloaded: %q", m.editor.Text())
	}
	m.diskChanged() // the same event again: nothing to do
	edited := "GET /edited HTTP/1.1\nHost: example.com\n\n"
	m.editor.SetText(edited) // its save failed
	if err := m.col.Write("a", newRequest); err != nil {
		t.Fatal(err)
	}
	m.diskChanged()
	if m.editor.Text() != edited {
		t.Fatalf("unsaved edits lost: %q", m.editor.Text())
	}
}

func TestImportedLowercaseMethod(t *testing.T) {
	if _, ok := imported("get / HTTP/1.1\nHost: example.com\n"); !ok {
		t.Error("lowercase method not imported")
	}
	if _, ok := imported("get some words"); ok {
		t.Error("words imported as a request")
	}
}

func TestFocusOrder(t *testing.T) {
	m := newModel(t)
	if err := m.col.Write("spilltea/api.test/login", "POST /login HTTP/1.1\nHost: api.test\n\n"); err != nil {
		t.Fatal(err)
	}
	if n, err := remote.Send(m.col.Root, remote.Order{Focus: "spilltea/api.test/login"}); err != nil || n != 1 {
		t.Fatalf("Send = %d, %v", n, err)
	}
	select {
	case o := <-m.remote.Orders():
		drain(m, m.focusOrder(o.Focus), 0)
	case <-time.After(2 * time.Second):
		t.Fatal("no order")
	}
	if m.editor.Name() != "spilltea/api.test/login" || m.focus != paneEditor {
		t.Fatalf("open %q, focus %v", m.editor.Name(), m.focus)
	}
	if e, _ := m.tree.Selected(); e.Name != "spilltea/api.test/login" {
		t.Fatalf("tree on %q", e.Name)
	}
}
