package ui

import (
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/sectui/steep/internal/ui/copypicker"
)

func TestCopyText(t *testing.T) {
	m := newModel(t)
	withRun(t, m, 3)
	dir, hits := m.results.Dir(), m.results.Shown()

	text := func(k copypicker.Kind, pretty bool, n int) string {
		t.Helper()
		s, err := copyText(dir, hits[:n], k, pretty)
		if err != nil {
			t.Fatal(err)
		}
		return s
	}
	if s := text(copypicker.URL, false, 3); strings.Count(s, "\n") != 2 || !strings.HasPrefix(s, "https://a.test/") || strings.Contains(s, "#") {
		t.Fatalf("URLs, one per line:\n%s", s)
	}
	if s := text(copypicker.Input, false, 2); !strings.HasPrefix(s, "1=word") || strings.Count(s, "\n") != 1 {
		t.Fatalf("inputs, one per line:\n%s", s)
	}
	// blocks: under a comment naming the hit, when there are several
	s := text(copypicker.Curl, false, 2)
	if strings.Count(s, "-X 'POST'") != 2 || !strings.HasPrefix(s, "# #") || !strings.Contains(s, "\n\n# #") {
		t.Fatalf("curl commands:\n%s", s)
	}
	if s := text(copypicker.HTTP, false, 1); !strings.HasPrefix(s, "POST /x HTTP/1.1\r\n") {
		t.Fatalf("one raw request, as sent:\n%q", s)
	}
	// the response as shown: no \r, the body formatted or not
	if s := text(copypicker.Headers, false, 1); s != "HTTP/1.1 200 OK\nContent-Type: application/json" {
		t.Fatalf("headers: %q", s)
	}
	if s := text(copypicker.Body, false, 1); !strings.HasPrefix(s, `{"items":[{`) {
		t.Fatalf("raw body: %.40q", s)
	}
	if s := text(copypicker.Body, true, 1); !strings.HasPrefix(s, "{\n  \"items\": [") {
		t.Fatalf("formatted body: %.40q", s)
	}
	if s := text(copypicker.Response, true, 1); !strings.HasPrefix(s, "HTTP/1.1 200 OK\nContent-Type: application/json\n\n{\n") {
		t.Fatalf("response: %.80q", s)
	}
}

func TestCopyPopup(t *testing.T) {
	m := newModel(t)
	withRun(t, m, 3)
	key(m, '3', tea.ModAlt)

	typeKeys(m, "Y")
	s := screen(m)
	if !strings.Contains(s, "Copy 3 hits") || !strings.Contains(s, "u  URL") || !strings.Contains(s, "c  request as curl") {
		t.Fatalf("no popup:\n%s", s)
	}
	typeKeys(m, "u")
	if h := fmt.Sprint(m.notes.History()); m.overlays.Top() != nil || !strings.Contains(h, "Copied 3 URLs") {
		t.Fatalf("not copied: %s", h)
	}

	// the selected hit, picked with enter
	typeKeys(m, "y")
	if !strings.Contains(screen(m), "Copy hit #") {
		t.Fatalf("no popup for one hit:\n%s", screen(m))
	}
	key(m, tea.KeyEnter, 0)
	if h := fmt.Sprint(m.notes.History()); !strings.Contains(h, "'s request as curl") || m.overlays.Top() != nil {
		t.Fatalf("not copied as curl: %s", h)
	}

	// a filter narrows what Y copies
	typeKeys(m, "/resp.code.eq:200")
	key(m, tea.KeyEnter, 0)
	if n := len(m.results.Shown()); n != 1 {
		t.Fatalf("%d hits shown", n)
	}
	typeKeys(m, "Y")
	if m.overlays.Top() != nil {
		t.Fatal("copy all offered for one hit shown")
	}
}

func TestCopyBigAsks(t *testing.T) {
	m := newModel(t)
	m.copied(copiedMsg{text: "small", what: "x"})
	if m.overlays.Top() != nil {
		t.Fatal("asked for a small copy")
	}
	do(m, copiedMsg{text: strings.Repeat("a", copyAskAbove+1), what: "9 responses"})
	if s := screen(m); !strings.Contains(s, "Copy 9 responses (1.0 MB)?") {
		t.Fatalf("not asked:\n%s", s)
	}
}

func TestCopyCollectionPath(t *testing.T) {
	m := newModel(t)
	var copied []string
	for _, c := range m.copyCollectionPath()().(tea.BatchMsg) {
		copied = append(copied, fmt.Sprint(c()))
	}
	if !strings.Contains(strings.Join(copied, "\n"), m.col.Root) {
		t.Fatalf("copied %q, want %s", copied, m.col.Root)
	}
	// no key: from the palette
	key(m, 'p', tea.ModCtrl)
	typeKeys(m, "copy the path of the coll")
	key(m, tea.KeyEnter, 0)
	if !strings.Contains(fmt.Sprint(m.notes.History()), "Copied the collection's path") {
		t.Fatalf("not run from the palette; notes: %+v", m.notes.History())
	}
}
