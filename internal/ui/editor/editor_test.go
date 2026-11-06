package editor

import (
	"strings"
	"testing"

	"github.com/sectui/foam/vim"

	"github.com/sectui/steep/internal/request"
)

// meaning is what s gives in a request: its blocks run, file{} as its path.
func meaning(t *testing.T, s string) string {
	t.Helper()
	b, err := request.ParseBody(s)
	if err != nil {
		t.Fatalf("%q: %v", s, err)
	}
	e, err := b.Expand()
	if err != nil {
		t.Fatalf("%q: %v", s, err)
	}
	return e.Text
}

func TestWrap(t *testing.T) {
	cases := map[string]string{
		"admin":                     "to-hex{admin}",
		"":                          "to-hex{}",
		`{"a": 1}`:                  `to-hex{\{"a": 1\}}`,
		`C:\\path \x`:               `to-hex{C:\\\\path \\x}`,
		"} {":                       `to-hex{\} \{}`,
		"md5{a} b":                  `to-hex{md5{a} b}`,
		"file{x} y":                 `to-hex{file{x} y}`,
		`\md5{a}`:                   `to-hex{md5\{a\}}`,
		"to-hex{open":               `to-hex{to-hex\{open}`,
		"to-hex(nope){a}":           `to-hex{to-hex(nope)\{a\}}`,
		"line1\nline2":              "to-hex{line1\nline2}",
		"url-encode(encode-all){?}": "to-hex{url-encode(encode-all){?}}",
	}
	for in, want := range cases {
		got := Wrap("to-hex", in)
		if got != want {
			t.Errorf("Wrap(%q) = %q, want %q", in, got, want)
			continue
		}
		// the block encodes what the selection meant, whatever it holds
		if strings.Contains(in, "file{") {
			continue
		}
		var plain string
		if b, err := request.ParseBody(in); err == nil {
			e, _ := b.Expand()
			plain = e.Text
		} else {
			plain = in
		}
		if got, want := meaning(t, Wrap("to-hex", in)), meaning(t, "to-hex{"+strings.NewReplacer(`\`, `\\`, "{", `\{`, "}", `\}`).Replace(plain)+"}"); got != want {
			t.Errorf("Wrap(%q) means %q, want %q", in, got, want)
		}
	}
}

func TestTextRange(t *testing.T) {
	lines := []string{"héllo wörld", "second", "third"}
	for _, c := range []struct {
		a, b vim.Pos
		want string
	}{
		{vim.Pos{Line: 0, Col: 6}, vim.Pos{Line: 0, Col: 11}, "wörld"},
		{vim.Pos{Line: 0, Col: 1}, vim.Pos{Line: 0, Col: 2}, "é"},
		{vim.Pos{Line: 0, Col: 6}, vim.Pos{Line: 1, Col: 3}, "wörld\nsec"},
		{vim.Pos{Line: 0, Col: 0}, vim.Pos{Line: 2, Col: 5}, "héllo wörld\nsecond\nthird"},
		{vim.Pos{Line: 1, Col: 3}, vim.Pos{Line: 1, Col: 3}, ""},
		{vim.Pos{Line: 1, Col: 0}, vim.Pos{Line: 9, Col: 99}, "second\nthird"},
		{vim.Pos{Line: 9, Col: 0}, vim.Pos{Line: 9, Col: 1}, ""},
	} {
		if got := textRange(lines, c.a, c.b); got != c.want {
			t.Errorf("%v-%v: got %q, want %q", c.a, c.b, got, c.want)
		}
	}
}

func TestPutOperation(t *testing.T) {
	m := New()
	m.SetSize(80, 20)
	m.Open("r", "GET /admin HTTP/1.1\nHost: h\n")
	m.ed.SetCursor(vim.Pos{Line: 0, Col: 5})
	m.ed.Editor.Key("v", "v")
	m.ed.Editor.Key("e", "e")
	m.putOperation(m.target(), "to-base64")
	if got := m.Text(); got != "GET /to-base64{admin} HTTP/1.1\nHost: h\n" || m.ed.Mode() != vim.Normal {
		t.Fatalf("got %q, mode %v", got, m.ed.Mode())
	}

	// no selection: an empty block, typed into
	m.ed.SetCursor(vim.Pos{Line: 1, Col: 6})
	m.putOperation(m.target(), "md5")
	if m.ed.Mode() != vim.Insert {
		t.Fatalf("mode %v", m.ed.Mode())
	}
	m.ed.Editor.Key("x", "x")
	m.ed.Editor.Key("esc", "")
	if got := m.Text(); got != "GET /to-base64{admin} HTTP/1.1\nHost: md5{x}h\n" {
		t.Fatalf("got %q", got)
	}

	// a target that went stale is not used
	m.ed.Editor.Key("V", "V")
	tg := m.target()
	m.SetText("other\n")
	m.ed.SetCursor(vim.Pos{Line: 0, Col: 2})
	m.putOperation(tg, "md5")
	m.ed.Editor.Key("esc", "")
	if got := m.Text(); got != "otmd5{}her\n" {
		t.Fatalf("stale target: %q", got)
	}

	// nothing open: nothing happens
	m.Close()
	m.putOperation(target{}, "md5")
}
