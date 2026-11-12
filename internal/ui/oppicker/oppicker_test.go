package oppicker

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/anotherhadi/operations"
)

func typeText(m *Model, s string) {
	for _, r := range s {
		m.Update(tea.KeyPressMsg{Code: r, Text: string(r)})
	}
}

func TestFilter(t *testing.T) {
	var picked *operations.Operation
	m := New("Insert", func(op *operations.Operation) tea.Cmd { picked = op; return nil })
	if want := len(operations.All()) + 3; len(m.shown) != want { // list, range, env
		t.Fatalf("shown %d of %d", len(m.shown), want)
	}
	for query, first := range map[string]string{
		"to-base64": "to-base64",
		"sha256":    "sha256",
		"jwt":       "jwt-decode",
		"aes":       "aes-encrypt",
		"urlenc":    "url-encode",
	} {
		m.input.SetValue(query)
		m.filter()
		if op := m.Selected(); op == nil || op.ID != first {
			t.Errorf("%q: first is %v, want %s", query, op, first)
		}
	}
	// aliases and names are searched too
	m.input.SetValue("percent")
	m.filter()
	if op := m.Selected(); op == nil || !strings.HasPrefix(op.ID, "url-") {
		t.Errorf("alias: got %v", op)
	}

	m.input.SetValue("")
	typeText(m, "hex")
	if m.Selected() == nil || !strings.Contains(m.Selected().ID, "hex") {
		t.Fatalf("typed: %v", m.Selected())
	}
	m.Scope().Commands[3].Run() // down
	second := m.Selected()
	m.choose()
	if picked != second {
		t.Fatalf("picked %v, want %v", picked, second)
	}

	typeText(m, "zzzzzz")
	if m.Selected() != nil || m.choose() != nil {
		t.Fatal("no match must pick nothing")
	}
	if !strings.Contains(ansi.Strip(m.View(80, 30)), "no operation") {
		t.Error("no hint for an empty list")
	}
}

func TestView(t *testing.T) {
	m := New("Wrap in an operation", func(*operations.Operation) tea.Cmd { return nil })
	m.input.SetValue("to-hex")
	m.filter()
	v := ansi.Strip(m.View(100, 40))
	for _, want := range []string{"Wrap in an operation", "to-hex", "To Hex", "Encoding", "Encodes raw bytes", "delimiter=none|space"} {
		if !strings.Contains(v, want) {
			t.Errorf("view lacks %q:\n%s", want, v)
		}
	}
	// any size, even tiny, keeps within bounds
	for _, size := range [][2]int{{0, 0}, {3, 2}, {10, 5}, {30, 8}, {200, 100}} {
		v := m.View(size[0], size[1])
		for _, l := range strings.Split(v, "\n") {
			if w := ansi.StringWidth(l); w > max(size[0], 2) {
				t.Errorf("%v: line of width %d: %q", size, w, ansi.Strip(l))
			}
		}
	}
}

func TestArgsText(t *testing.T) {
	op, _ := operations.Lookup("bcrypt")
	if got := argsText(op); got != "rounds=10, salt=" {
		t.Errorf("got %q", got)
	}
	op, _ = operations.Lookup("url-encode")
	if got := argsText(op); got != "encode-all" {
		t.Errorf("got %q", got)
	}
	op, _ = operations.Lookup("md5")
	if got := argsText(op); got != "" {
		t.Errorf("got %q", got)
	}
}
