package results

import (
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/sectui/steep/internal/run"
)

// A new run replaces hits that were shown: nothing may point into the old ones.
func TestSetRunAfterHits(t *testing.T) {
	m := New(nil, nil)
	m.SetRun("a", []run.Hit{{N: 1}, {N: 2}, {N: 3}})
	m.list.Select(2)
	m.SetRun("b", nil)
	if _, ok := m.Selected(); ok {
		t.Fatal("a hit selected in an empty run")
	}
	m.Add([]run.Hit{{N: 1}})
	if h, ok := m.Selected(); !ok || h.N != 1 {
		t.Fatalf("selected = %+v, %v", h, ok)
	}
}

func TestMouseOpensOnlyWhenAlreadyFocused(t *testing.T) {
	m := New(nil, nil)
	m.SetRun("a", []run.Hit{{N: 1}})
	click := tea.MouseClickMsg{Y: 1, Button: tea.MouseLeft} // row 0, below the column header

	if cmd := m.Mouse(click, false); cmd != nil {
		t.Fatalf("a click arriving from another pane must not open the row, got %T", cmd())
	}
	cmd := m.Mouse(click, true)
	if cmd == nil {
		t.Fatal("a click on the already-focused, already-selected row must open it")
	}
	if _, ok := cmd().(OpenMsg); !ok {
		t.Fatalf("expected OpenMsg, got %T", cmd())
	}
}

func TestColumnsFollowWidth(t *testing.T) {
	names := func(w int) (out []string) {
		for _, c := range visibleColumns(w) {
			out = append(out, c.name)
		}
		return out
	}
	if got := names(30); len(got) != 0 {
		t.Fatalf("30: %v", got)
	}
	if got := names(45); len(got) != 1 || got[0] != "time" {
		t.Fatalf("45: %v", got)
	}
	if got := names(200); len(got) != 3 || got[0] != "words" || got[2] != "time" {
		t.Fatalf("200: %v", got)
	}
}

func TestAddSkipsHeldHits(t *testing.T) {
	m := New(nil, nil)
	m.SetRun("a", []run.Hit{{N: 1}, {N: 2}})
	m.Add([]run.Hit{{N: 2}, {N: 4}, {N: 3}})
	m.Add([]run.Hit{{N: 3}})
	if m.Hits() != 4 {
		t.Fatalf("hits = %d", m.Hits())
	}
}

// Hits arriving while a raw filter is computed stay matched once it lands.
func TestRawFilterKeepsHitsAddedMeanwhile(t *testing.T) {
	dir := t.TempDir()
	s, err := run.Create(dir)
	if err != nil {
		t.Fatal(err)
	}
	add := func(body string) run.Hit {
		h, err := s.Add(run.Hit{}, nil, []byte("HTTP/1.1 200 OK\r\n\r\n"+body))
		if err != nil {
			t.Fatal(err)
		}
		return h
	}
	m := New(nil, nil)
	m.SetRun(dir, []run.Hit{add("flag{1}"), add("no")})
	m.filter.SetValue(`"flag{"`)
	cmd := m.refilter()
	if cmd == nil {
		t.Fatal("a raw filter is computed in the background")
	}
	m.Add([]run.Hit{add("flag{3}")})
	if !m.Update(cmd()) {
		t.Fatal("the filter's result was not taken")
	}
	var got []int
	for _, i := range m.shown {
		got = append(got, m.hits[i].N)
	}
	if len(got) != 2 || got[0] != 1 || got[1] != 3 {
		t.Fatalf("shown = %v, want [1 3]", got)
	}
}

// A raw filter replaced by another stops early.
func TestRawFilterReplacedIsDropped(t *testing.T) {
	dir := t.TempDir()
	s, _ := run.Create(dir)
	h, _ := s.Add(run.Hit{}, nil, []byte("HTTP/1.1 200 OK\r\n\r\nflag{1}"))
	m := New(nil, nil)
	m.SetRun(dir, []run.Hit{h})
	m.filter.SetValue(`"flag{"`)
	old := m.refilter()
	m.filter.SetValue(`"nope"`)
	m.refilter()
	if msg := old(); msg != nil {
		t.Fatalf("outdated filter returned %v", msg)
	}
}
