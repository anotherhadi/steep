package ui

import (
	"fmt"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/anotherhadi/foam/notify"
	"github.com/anotherhadi/foam/theme"

	"github.com/anotherhadi/steep/internal/config"
	"github.com/anotherhadi/steep/internal/fuzz"
	"github.com/anotherhadi/steep/internal/request"
	"github.com/anotherhadi/steep/internal/run"
	"github.com/anotherhadi/steep/internal/ui/response"
)

// checkScreen asserts that the view fills w×h exactly. The renderer drops
// the trailing blanks of a line, so only a blank line may be shorter.
func checkScreen(t *testing.T, m *Model, w, h int, state string) {
	t.Helper()
	content := m.View().Content
	lines := strings.Split(content, "\n")
	if len(lines) != h {
		t.Fatalf("%s at %dx%d: %d lines", state, w, h, len(lines))
	}
	for i, l := range lines {
		if got := ansi.StringWidth(l); got > w || got < w && strings.TrimSpace(ansi.Strip(l)) != "" {
			t.Fatalf("%s at %dx%d: line %d is %d wide: %q", state, w, h, i, got, ansi.Strip(l))
		}
	}
}

// withRun opens a request that has a kept run of n hits.
func withRun(t *testing.T, m *Model, n int) {
	t.Helper()
	text := "# steep id: r1\nPOST /api/" + strings.Repeat("segment/", 30) + "file{/tmp/x} HTTP/1.1\n" +
		"Host: a-rather-long-host-name.example.test\nX-Long: " + strings.Repeat("v", 300) + "\n\n" +
		`{"k": "` + strings.Repeat("é", 200) + `", "tab": "a	b"}`
	if err := m.col.Create("deep/folder/with/a/very-long-request-name-that-goes-on", text); err != nil {
		t.Fatal(err)
	}
	store, err := run.Create(m.col.RunDir("r1"))
	if err != nil {
		t.Fatal(err)
	}
	body := `{"items":[` + strings.Repeat(`{"name":"一二三四五","n":12345,"ok":true},`, 40) + `{}]}`
	for i := range n {
		store.Add(run.Hit{Status: 200 + i%4*100, Length: int64(len(body)), Words: 10, Lines: 1,
			Input: map[string]string{request.Keyword(1): fmt.Sprint("word", i), request.Keyword(2): strings.Repeat("x", 80)},
			URL:   "https://a.test/" + fmt.Sprint(i)},
			[]byte("POST /x HTTP/1.1\r\nHost: a.test\r\n\r\n"),
			[]byte("HTTP/1.1 200 OK\r\nContent-Type: application/json\r\n\r\n"+body))
	}
	store.Close(run.Meta{Total: n, Done: n})
	m.reloadEntries()
	do(m, m.open("deep/folder/with/a/very-long-request-name-that-goes-on")())
}

func TestResponsive(t *testing.T) {
	sizes := [][2]int{
		{80, 20}, {80, 21}, {81, 20}, {83, 22}, {90, 25}, {99, 30}, {100, 24}, {101, 33},
		{120, 40}, {137, 41}, {157, 50}, {200, 60}, {300, 80},
		// below the minimum
		{79, 20}, {80, 19}, {40, 10}, {10, 3}, {1, 1}, {2, 200}, {300, 2},
	}
	states := []struct {
		name  string
		setup func(t *testing.T, m *Model)
	}{
		{"empty", func(t *testing.T, m *Model) {}},
		{"one hit", func(t *testing.T, m *Model) { withRun(t, m, 1) }},
		{"many hits", func(t *testing.T, m *Model) { withRun(t, m, 300) }},
		{"filter typing", func(t *testing.T, m *Model) {
			withRun(t, m, 50)
			key(m, '3', tea.ModAlt)
			typeKeys(m, "/resp.code.eq:200 AND fuzz.input.cont:\"word1\" OR resp.len.gt:100000000")
		}},
		{"bad filter", func(t *testing.T, m *Model) {
			withRun(t, m, 50)
			key(m, '3', tea.ModAlt)
			typeKeys(m, "/resp.code.eq:\"x\"")
		}},
		{"search and pipe", func(t *testing.T, m *Model) {
			withRun(t, m, 5)
			key(m, '4', tea.ModAlt)
			typeKeys(m, "/name")
			key(m, tea.KeyEnter, 0)
			typeKeys(m, "w")
		}},
		{"pipe picker", func(t *testing.T, m *Model) {
			m.response = response.New(true, true, []config.Pipe{
				{Name: "a rather long pipe name that goes on and on", Command: "rg -i " + strings.Repeat("cookie|", 40)},
				{Name: "short", Command: "jq ."},
			})
			withRun(t, m, 5)
			key(m, '4', tea.ModAlt)
			typeKeys(m, "P")
		}},
		{"help", func(t *testing.T, m *Model) { withRun(t, m, 5); typeKeys(m, "?") }},
		{"palette", func(t *testing.T, m *Model) { key(m, 'p', tea.ModCtrl) }},
		{"settings", func(t *testing.T, m *Model) {
			withRun(t, m, 5)
			key(m, 'o', tea.ModCtrl)
			typeKeys(m, "jjjj")
		}},
		{"prompt", func(t *testing.T, m *Model) { typeKeys(m, "n") }},
		{"confirm", func(t *testing.T, m *Model) {
			withRun(t, m, 5)
			key(m, '1', tea.ModAlt)
			typeKeys(m, "jjjjx")
		}},
		{"toasts", func(t *testing.T, m *Model) {
			for i := range 7 {
				m.Update(notify.Msg{Level: notify.Level(i % 4), Text: strings.Repeat("a long notification ", i+1)})
			}
		}},
		{"collection hidden", func(t *testing.T, m *Model) { withRun(t, m, 5); key(m, 'b', tea.ModCtrl) }},
		{"copy popup", func(t *testing.T, m *Model) { withRun(t, m, 5); key(m, '3', tea.ModAlt); typeKeys(m, "Y") }},
		{"filter picker", func(t *testing.T, m *Model) { withRun(t, m, 5); key(m, '3', tea.ModAlt); key(m, 'r', tea.ModCtrl) }},
		{"zoomed and marked", func(t *testing.T, m *Model) {
			withRun(t, m, 50)
			key(m, '3', tea.ModAlt)
			typeKeys(m, "mmm")
			key(m, 'z', tea.ModAlt)
		}},
		{"multiline pipe", func(t *testing.T, m *Model) {
			m.response = response.New(true, true, []config.Pipe{{Name: "x", Command: "cat |\n" + strings.Repeat("cat | ", 40) + "cat"}})
			withRun(t, m, 5)
			key(m, '4', tea.ModAlt)
			typeKeys(m, "P")
			key(m, tea.KeyEnter, 0)
		}},
		{"pipe error", func(t *testing.T, m *Model) {
			withRun(t, m, 5)
			key(m, '4', tea.ModAlt)
			typeKeys(m, "|echo "+strings.Repeat("oops ", 60)+" >&2; exit 1")
			key(m, tea.KeyEnter, 0)
		}},
		{"operation picker", func(t *testing.T, m *Model) { withRun(t, m, 5); key(m, 'e', tea.ModCtrl); typeKeys(m, "base") }},
		{"collection filter", func(t *testing.T, m *Model) {
			withRun(t, m, 5)
			key(m, '1', tea.ModAlt)
			typeKeys(m, "/very-long-request")
		}},
		{"visual mode", func(t *testing.T, m *Model) { withRun(t, m, 5); typeKeys(m, "ggVG") }},
		{"run progress", func(t *testing.T, m *Model) {
			withRun(t, m, 50)
			m.results.SetProgress(fuzz.Progress{Done: 1234, Total: 99999, ReqSec: 321, Errors: 7, Started: time.Now()}, true, false)
		}},
		{"run paused", func(t *testing.T, m *Model) {
			withRun(t, m, 50)
			m.results.SetProgress(fuzz.Progress{Done: 5, Total: 10, Started: time.Now()}, true, true)
		}},
		{"insert mode", func(t *testing.T, m *Model) { withRun(t, m, 5); typeKeys(m, "Gi") }},
	}
	looks := []struct{ spacious, helpBar bool }{{false, true}, {true, true}, {true, false}, {false, false}}
	defer theme.Set(theme.Current())
	for _, look := range looks {
		th := theme.Current()
		th.Spacious, th.HelpBar = look.spacious, look.helpBar
		theme.Set(th)
		for _, st := range states {
			m := newModel(t)
			st.setup(t, m)
			for _, sz := range sizes {
				m.Update(tea.WindowSizeMsg{Width: sz[0], Height: sz[1]})
				checkScreen(t, m, sz[0], sz[1], fmt.Sprintf("%s (spacious=%v helpBar=%v)", st.name, look.spacious, look.helpBar))
			}
		}
	}
}

// Fields left show the start of what they hold, not the end.
func TestLongFieldsShowTheirStart(t *testing.T) {
	m := newModel(t)
	withRun(t, m, 5)
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 20})
	key(m, '4', tea.ModAlt)
	typeKeys(m, "|echo start "+strings.Repeat("x ", 60)+"end")
	key(m, tea.KeyEnter, 0)
	key(m, '3', tea.ModAlt)
	typeKeys(m, "/resp.len.gt:0 AND "+strings.Repeat("resp.code.gt:0 AND ", 10)+"resp.code.lt:999")
	key(m, tea.KeyEnter, 0)
	s := screen(m)
	if !strings.Contains(s, "| echo start") || !strings.Contains(s, "/ resp.len.gt:0") {
		t.Fatalf("fields show their end:\n%s", s)
	}
}

// A toast that would run past the bottom of the window is left out.
func TestToastsStayOnScreen(t *testing.T) {
	m := newModel(t)
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 20})
	for range 5 {
		m.Update(notify.Msg{Text: strings.Repeat("a long notification ", 7)})
	}
	m.toasts()
	if len(m.toastRects) == 0 {
		t.Fatal("no toast")
	}
	for _, r := range m.toastRects {
		if b := r.Y + r.H; b > m.h {
			t.Fatalf("a toast ends at row %d of %d", b, m.h)
		}
	}
}

// The selected row is cut like the others, with an ellipsis.
func TestSelectedRowEllipsis(t *testing.T) {
	m := newModel(t)
	withRun(t, m, 5)
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	key(m, '3', tea.ModAlt)
	for _, l := range strings.Split(screen(m), "\n") {
		if strings.Contains(l, "1=word0") && !strings.Contains(l, "…") {
			t.Fatalf("selected hit cut without an ellipsis: %q", l)
		}
		if strings.Contains(l, "POST   very") && !strings.Contains(l, "…") {
			t.Fatalf("selected request cut without an ellipsis: %q", l)
		}
	}
}

func TestZoom(t *testing.T) {
	m := newModel(t)
	withRun(t, m, 5)
	key(m, '4', tea.ModAlt)
	key(m, 'z', tea.ModAlt)
	body := m.rects[paneResponse]
	if body.W != m.w || !m.rects[paneEditor].Empty() || !m.rects[paneResults].Empty() || !m.rects[paneCollection].Empty() {
		t.Fatalf("not zoomed: %+v", m.rects)
	}
	// another pane takes the zoom
	key(m, '3', tea.ModAlt)
	if m.rects[paneResults] != body || !m.rects[paneResponse].Empty() {
		t.Fatalf("zoom did not follow the focus: %+v", m.rects)
	}
	checkScreen(t, m, m.w, m.h, "zoomed")
	key(m, 'z', tea.ModAlt)
	if m.rects[paneEditor].Empty() || m.rects[paneResponse].Empty() {
		t.Fatalf("not back: %+v", m.rects)
	}
}
