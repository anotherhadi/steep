package ui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/sectui/steep/internal/config"
	"github.com/sectui/steep/internal/ui/picker"
	"github.com/sectui/steep/internal/ui/response"
)

func TestPipePickerAndReset(t *testing.T) {
	m := newModel(t)
	m.response = response.New(true, true, []config.Pipe{
		{Name: "Upper", Command: "tr a-z A-Z"},
		{Name: "Bytes", Command: "wc -c"},
	})
	withRun(t, m, 1)
	key(m, '4', tea.ModAlt)

	typeKeys(m, "P")
	if _, ok := m.overlays.Top().(*picker.Model); !ok {
		t.Fatalf("no picker:\n%s", screen(m))
	}
	if s := screen(m); !strings.Contains(s, "Upper") || !strings.Contains(s, "wc -c") {
		t.Fatalf("pipes not listed:\n%s", s)
	}
	typeKeys(m, "upp")
	key(m, tea.KeyEnter, 0)
	if m.overlays.Top() != nil {
		t.Fatal("picker still open")
	}
	if s := screen(m); !strings.Contains(s, "CONTENT-TYPE") || !strings.Contains(s, "| tr a-z A-Z") {
		t.Fatalf("not piped:\n%s", s)
	}

	// a search over the pipe, closed, gives the pipe back
	typeKeys(m, "/")
	key(m, tea.KeyEscape, 0)
	if s := screen(m); !strings.Contains(s, "CONTENT-TYPE") || !strings.Contains(s, "| tr a-z A-Z") {
		t.Fatalf("pipe lost after the search:\n%s", s)
	}

	// reset drops the search and the pipe at once
	typeKeys(m, "/CONTENT")
	key(m, tea.KeyEnter, 0)
	key(m, 'g', tea.ModCtrl)
	if s := screen(m); strings.Contains(s, "CONTENT-TYPE") || !strings.Contains(s, "Content-Type") || strings.Contains(s, "| tr") {
		t.Fatalf("not reset:\n%s", s)
	}
	// and the next response is shown as is
	if m.response.Status() != "" {
		t.Fatalf("search kept: %q", m.response.Status())
	}
}

func TestDefaultPipes(t *testing.T) {
	m := newModel(t)
	withRun(t, m, 1)
	key(m, '4', tea.ModAlt)

	typeKeys(m, "Pheaders")
	key(m, tea.KeyEnter, 0)
	if s := screen(m); !strings.Contains(s, "Content-Type") || strings.Contains(s, "items") {
		t.Fatalf("not the headers only:\n%s", s)
	}

	key(m, 'g', tea.ModCtrl)
	typeKeys(m, "Pbody")
	key(m, tea.KeyEnter, 0)
	if s := screen(m); !strings.Contains(s, "items") || strings.Contains(s, "Content-Type") {
		t.Fatalf("not the body only:\n%s", s)
	}
}

func TestNoPipesNoPicker(t *testing.T) {
	m := newModel(t)
	m.response = response.New(true, true, nil)
	withRun(t, m, 1)
	key(m, '4', tea.ModAlt)
	typeKeys(m, "P")
	if m.overlays.Top() != nil {
		t.Fatal("picker opened without pipes")
	}
}

// A YAML block (command: |) gives a command with newlines: one line in the
// footer, run as written.
func TestMultilinePipe(t *testing.T) {
	m := newModel(t)
	m.response = response.New(true, true, []config.Pipe{
		{Name: "Two lines", Command: "grep -i content-type |\ntr a-z A-Z"},
	})
	withRun(t, m, 1)
	key(m, '4', tea.ModAlt)

	typeKeys(m, "P")
	key(m, tea.KeyEnter, 0)
	if s := screen(m); !strings.Contains(s, "| grep -i content-type |↵tr a-z A-Z") || !strings.Contains(s, "CONTENT-TYPE") {
		t.Fatalf("not one line, or not piped:\n%s", s)
	}

	// edited, it runs as it was
	typeKeys(m, "|")
	key(m, tea.KeyEnter, 0)
	if s := screen(m); !strings.Contains(s, "CONTENT-TYPE") {
		t.Fatalf("not run again:\n%s", s)
	}
}

// rowOf is the last screen row showing text.
func rowOf(t *testing.T, m *Model, text string) int {
	t.Helper()
	y := -1
	for i, l := range strings.Split(screen(m), "\n") {
		if strings.Contains(l, text) {
			y = i
		}
	}
	if y < 0 {
		t.Fatalf("no row for %q:\n%s", text, screen(m))
	}
	return y
}

func TestClickFocusesLines(t *testing.T) {
	m := newModel(t)
	withRun(t, m, 3)

	// the pipe line
	key(m, '4', tea.ModAlt)
	typeKeys(m, "|cat")
	key(m, tea.KeyEnter, 0)
	key(m, '2', tea.ModAlt)
	r := m.rects[paneResponse]
	do(m, tea.MouseClickMsg{X: r.X + 3, Y: rowOf(t, m, "| cat"), Button: tea.MouseLeft})
	if m.focus != paneResponse || !m.response.Capturing() {
		t.Fatalf("pipe line not focused: focus %v", m.focus)
	}
	// a click in the response leaves it
	do(m, tea.MouseClickMsg{X: r.X + 3, Y: r.Y + 2, Button: tea.MouseLeft})
	if m.response.Capturing() {
		t.Fatal("pipe line still focused")
	}

	// the HTTPQL filter
	key(m, '3', tea.ModAlt)
	typeKeys(m, "/resp.code.gt:0")
	key(m, tea.KeyEnter, 0)
	key(m, '2', tea.ModAlt)
	r = m.rects[paneResults]
	do(m, tea.MouseClickMsg{X: r.X + 3, Y: r.Y + 1, Button: tea.MouseLeft})
	if m.focus != paneResults || !m.results.Capturing() {
		t.Fatalf("filter not focused: focus %v", m.focus)
	}
}
