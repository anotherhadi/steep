package highlight

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func TestWidthsKept(t *testing.T) {
	lines := []string{
		"# steep mode: pitchfork",
		"# a note",
		"POST /FUZZ HTTP/1.1",
		"Host: api.test",
		"Content-Type: application/json",
		"",
		`{"user": "FUZZ", "n": -1.5e3, "ok": true, "x": null}`,
		"<html><a href=\"x\">é</a>",
	}
	out := Message(lines, Options{Keywords: []string{"FUZZ"}})
	for i, l := range out {
		if ansi.Strip(l) != lines[i] {
			t.Errorf("line %d: %q became %q", i, lines[i], ansi.Strip(l))
		}
	}
}

func TestRoles(t *testing.T) {
	l := &line{text: []rune(`{"a": "b", "n": 12}`)}
	l.roles = make([]role, len(l.text))
	json(l)
	if l.roles[1] != key || l.roles[6] != str || l.roles[16] != number {
		t.Fatalf("roles = %v", l.roles)
	}
	r := &line{text: []rune("HTTP/1.1 404 Not Found")}
	r.roles = make([]role, len(r.text))
	firstLine(r)
	if r.code != 404 || r.roles[9] != status || r.roles[0] != version {
		t.Fatalf("status line: code %d, roles %v", r.code, r.roles)
	}
	if !strings.Contains(MethodColor("GET").Render("x"), "x") {
		t.Fatal("method color")
	}
}

func TestCacheSameAsFresh(t *testing.T) {
	lines := []string{"# steep mode: x", "GET /a HTTP/1.1", "Content-Type: text/html", "", "{not json}", "<b>x</b>"}
	var c Cache
	c.Message(lines, Options{Blocks: true})
	// the same lines in other places of the message are colored for there
	moved := []string{"GET /a HTTP/1.1", "# steep mode: x", "", "<b>x</b>", "{not json}"}
	got := c.Message(moved, Options{Blocks: true})
	want := Message(moved, Options{Blocks: true})
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("line %d: cached %q, fresh %q", i, got[i], want[i])
		}
	}
}

func TestURL(t *testing.T) {
	u := "https://api.test:8443/a/file{$w/x}?q=1&r=a=b"
	got := URL(u)
	if ansi.Strip(got) != u {
		t.Fatalf("text changed: %q", ansi.Strip(got))
	}
	for _, part := range []string{
		style(target, 0).Render("api.test:8443"),
		style(path, 0).Render("/a/"),
		style(block, 0).Render("file{$w/x}"),
		style(key, 0).Render("q"),
		style(str, 0).Render("a=b"),
	} {
		if !strings.Contains(got, part) {
			t.Errorf("%q not colored as expected in %q", ansi.Strip(part), got)
		}
	}
}

func roles(s string) []role {
	l := &line{text: []rune(s)}
	l.roles = make([]role, len(l.text))
	blocks(l, s)
	return l.roles
}

func TestOperationBlocks(t *testing.T) {
	s := `é to-hex(delimiter=colon)|url-encode{file{x} y} md5{a}`
	r := roles(s)
	runes := []rune(s)
	check := func(sub string, from int, ro role) {
		t.Helper()
		i := strings.Index(string(runes[from:]), sub)
		if i < 0 {
			t.Fatalf("%q not found", sub)
		}
		start := from + len([]rune(string(runes[from:])[:i]))
		for k := range []rune(sub) {
			if r[start+k] != ro {
				t.Errorf("%q at %d: role %v, want %v", sub, start+k, r[start+k], ro)
			}
		}
	}
	check("to-hex", 0, opName)
	check("(delimiter=colon)", 0, opArgs)
	check("|", 0, opBrace)
	check("url-encode", 0, opName)
	check("file{x}", 0, block)
	check(" y", 0, plain)
	check("md5", 0, opName)
	if r[len(r)-1] != opBrace || r[len(r)-3] != opBrace {
		t.Errorf("braces of md5{a}: %v", r[len(r)-3:])
	}
	// A block going on below still has its head colored.
	r = roles(`x: to-base64{abc`)
	if r[3] != opName || r[12] != opBrace || r[13] != plain {
		t.Errorf("open block: %v", r)
	}
	// Unknown ids, JSON and blocks with errors are left alone.
	for _, s := range []string{`{"a": "nope{x}"}`, `xmd5{a}`, `md5(bad){a}`} {
		for i, ro := range roles(s) {
			if ro != plain {
				t.Errorf("%q: rune %d colored %v", s, i, ro)
			}
		}
	}
	lines := []string{"GET /to-hex{a b}/file{x} HTTP/1.1", "X: url-encode(encode-all){", "  multi", "}", `{"a": "md5{x}"}`}
	for i, l := range Message(lines, Options{Blocks: true}) {
		if ansi.Strip(l) != lines[i] {
			t.Errorf("line %d changed: %q", i, ansi.Strip(l))
		}
	}
	if got := URL("https://h/to-hex{a}"); !strings.Contains(got, style(opName, 0).Render("to-hex")) {
		t.Errorf("URL: %q", got)
	}
}
