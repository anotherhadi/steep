package ui

import (
	"fmt"
	"os"
	"regexp"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/sectui/steep/internal/highlight"
	"github.com/sectui/steep/internal/request"
	"github.com/sectui/steep/internal/run"
	"github.com/sectui/steep/internal/ui/picker"
)

func TestMarks(t *testing.T) {
	m := newModel(t)
	withRun(t, m, 5)
	key(m, '3', tea.ModAlt)
	typeKeys(m, "mjm") // marks 1, moves to 3 after j, marks 3
	if got := run.Marks(m.results.Dir()); len(got) != 2 || !got[1] || !got[3] {
		t.Fatalf("marks on disk = %v", got)
	}
	if n := strings.Count(screen(m), "●"); n != 2 {
		t.Fatalf("%d marks shown:\n%s", n, screen(m))
	}
	typeKeys(m, "/row.marked.eq:true")
	key(m, tea.KeyEnter, 0)
	if n := len(m.results.Shown()); n != 2 {
		t.Fatalf("%d hits marked shown", n)
	}
	// reopened, the marks are still there
	m2 := newModel(t)
	m2.col = m.col
	withRunKeep(t, m2)
	if got := strings.Count(screen(m2), "●"); got != 2 {
		t.Fatalf("%d marks after reopening", got)
	}
}

func TestForgetSelected(t *testing.T) {
	m := newModel(t)
	withRun(t, m, 5)
	key(m, '3', tea.ModAlt)
	typeKeys(m, "jx") // forgets 2, at once
	if hit, _ := m.results.Selected(); hit.N != 3 {
		t.Fatalf("selected after forgetting = #%d, want #3", hit.N)
	}
	typeKeys(m, "Gx") // the last one: the selection goes up
	var ns []int
	for _, h := range m.results.Shown() {
		ns = append(ns, h.N)
	}
	if fmt.Sprint(ns) != "[1 3 4]" {
		t.Fatalf("hits left = %v", ns)
	}
	if hit, _ := m.results.Selected(); hit.N != 4 {
		t.Fatalf("selected = #%d, want #4", hit.N)
	}
}

func TestForgetShown(t *testing.T) {
	m := newModel(t)
	withRun(t, m, 5)
	key(m, '3', tea.ModAlt)
	typeKeys(m, "mjm") // marks 1 and 3
	typeKeys(m, `/preset:unmarked`)
	key(m, tea.KeyEnter, 0)
	if n := len(m.results.Shown()); n != 3 {
		t.Fatalf("%d unmarked hits shown", n)
	}
	typeKeys(m, "X")
	if !strings.Contains(screen(m), "Forget the 3 hits shown?") {
		t.Fatalf("no question:\n%s", screen(m))
	}
	typeKeys(m, "y")
	key(m, 'g', tea.ModCtrl) // drops the filter
	var ns []int
	for _, h := range m.results.Shown() {
		ns = append(ns, h.N)
	}
	if fmt.Sprint(ns) != "[1 3]" {
		t.Fatalf("hits left = %v", ns)
	}
	if _, hits, _ := run.Load(m.results.Dir()); len(hits) != 2 {
		t.Fatalf("%d hits on disk", len(hits))
	}
}

// withRunKeep opens the request of withRun without writing its run again.
func withRunKeep(t *testing.T, m *Model) {
	t.Helper()
	m.reloadEntries()
	do(m, m.open("deep/folder/with/a/very-long-request-name-that-goes-on")())
}

func TestFilterHistoryAndPresets(t *testing.T) {
	m := newModel(t)
	withRun(t, m, 5)
	key(m, '3', tea.ModAlt)
	for _, q := range []string{"resp.code.eq:200", "resp.code.eq:300"} {
		typeKeys(m, "/")
		key(m, 'u', tea.ModCtrl)
		typeKeys(m, q)
		key(m, tea.KeyEnter, 0)
	}
	b, _ := os.ReadFile(historyPath())
	if string(b) != "resp.code.eq:200\nresp.code.eq:300\n" {
		t.Fatalf("history file = %q", b)
	}
	// up goes back through it, down comes back to what was typed
	typeKeys(m, "/")
	key(m, 'u', tea.ModCtrl)
	typeKeys(m, "draft")
	key(m, tea.KeyUp, 0)
	key(m, tea.KeyUp, 0)
	if !strings.Contains(screen(m), "/ resp.code.eq:200") {
		t.Fatalf("not the oldest:\n%s", screen(m))
	}
	key(m, tea.KeyDown, 0)
	key(m, tea.KeyDown, 0)
	if !strings.Contains(screen(m), "/ draft") {
		t.Fatalf("draft lost:\n%s", screen(m))
	}
	key(m, tea.KeyEscape, 0)

	// ctrl+r: the presets of the config, then the history
	key(m, 'r', tea.ModCtrl)
	if _, ok := m.overlays.Top().(*picker.Model); !ok {
		t.Fatalf("no picker:\n%s", screen(m))
	}
	typeKeys(m, "errors")
	key(m, tea.KeyEnter, 0)
	if !strings.Contains(screen(m), "/ resp.code.gte:500") || len(m.results.Shown()) != 1 {
		t.Fatalf("preset not applied (%d shown):\n%s", len(m.results.Shown()), screen(m))
	}
	// and preset:"name" in a query
	typeKeys(m, "/")
	key(m, 'u', tea.ModCtrl)
	typeKeys(m, `preset:"errors" OR resp.code.eq:300`)
	key(m, tea.KeyEnter, 0)
	if n := len(m.results.Shown()); n != 2 {
		t.Fatalf("%d shown with presets", n)
	}
}

func TestHitPaths(t *testing.T) {
	m := newModel(t)
	withRun(t, m, 3)
	key(m, '3', tea.ModAlt)
	typeKeys(m, "fF")
	h := fmt.Sprint(m.notes.History())
	if !strings.Contains(h, "path of hit #1's response") || !strings.Contains(h, "run's directory") {
		t.Fatalf("not copied: %s", h)
	}
	if _, err := os.Stat(run.ResponsePath(m.results.Dir(), 1)); err != nil {
		t.Fatal(err)
	}
}

func TestEditHit(t *testing.T) {
	m := newModel(t)
	withRun(t, m, 3)
	// a request whose sent text looks like blocks
	dir := m.results.Dir()
	os.WriteFile(dir+"/dumps/2.req", []byte("POST /x HTTP/1.1\r\nHost: a.test\r\n\r\n{\"h\": \"md5{x}\", \"f\": \"file{/etc/passwd}\"}"), 0o600)
	key(m, '3', tea.ModAlt)
	typeKeys(m, "je")
	if !strings.Contains(screen(m), "very-long-request-name-that-goes-on-2") {
		t.Fatalf("no name asked:\n%s", screen(m))
	}
	key(m, tea.KeyEnter, 0)
	name := "deep/folder/with/a/very-long-request-name-that-goes-on-2"
	text, err := m.col.Read(name)
	if err != nil {
		t.Fatalf("not saved: %v\n%s", err, screen(m))
	}
	f := request.Parse(text)
	if f.ID() != "" || strings.Contains(text, "steep id") {
		t.Fatalf("id kept:\n%s", text)
	}
	b, err := f.ParseBody()
	if err != nil {
		t.Fatal(err)
	}
	if e, err := b.Expand(); err != nil || !strings.Contains(e.Text, `"md5{x}"`) || len(b.Wordlists()) != 0 {
		t.Fatalf("blocks not kept as text: %q, %v\n%s", e.Text, err, text)
	}
	if m.editor.Name() != name {
		t.Fatalf("open = %q", m.editor.Name())
	}
}

func TestPwnFoxColor(t *testing.T) {
	m := newModel(t)
	m.col.Create("admin", "GET /a HTTP/1.1\nHost: x\nX-PwnFox-Color: red\n\n")
	m.col.Create("anon", "GET /b HTTP/1.1\nHost: x\n\n")
	m.reloadEntries()
	dot := highlight.PwnFoxIcon()
	s := screen(m)
	if !strings.Contains(s, " "+dot+" GET    admin") || !strings.Contains(s, "   GET    anon") {
		t.Fatalf("tree:\n%s", s)
	}
	do(m, m.open("admin")())
	if !regexp.MustCompile(`admin · \w+ ` + dot + ` ─`).MatchString(screen(m)) {
		t.Fatalf("title:\n%s", screen(m))
	}
	do(m, m.open("anon")())
	if strings.Contains(screen(m), "anon · NORMAL "+dot) {
		t.Fatalf("dot without a color:\n%s", screen(m))
	}
}
