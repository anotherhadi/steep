package editor

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/anotherhadi/foam/highlight"
)

func roles(s string) []highlight.Role {
	r := make([]highlight.Role, len([]rune(s)))
	for _, sp := range BlockSpans(s) {
		for i := sp.Start; i < sp.End && i < len(r); i++ {
			r[i] = sp.Role
		}
	}
	return r
}

func TestOperationBlocks(t *testing.T) {
	s := `é to-hex(delimiter=colon)|url-encode{file{x} y} md5{a}`
	r := roles(s)
	check := func(sub string, ro highlight.Role) {
		t.Helper()
		i := strings.Index(s, sub)
		if i < 0 {
			t.Fatalf("%q not found", sub)
		}
		start := len([]rune(s[:i]))
		for k := range []rune(sub) {
			if r[start+k] != ro {
				t.Errorf("%q at %d: role %v, want %v", sub, start+k, r[start+k], ro)
			}
		}
	}
	check("to-hex", highlight.OpName)
	check("(delimiter=colon)", highlight.OpArgs)
	check("|", highlight.OpBrace)
	check("url-encode", highlight.OpName)
	check("file{x}", highlight.Block)
	check(" y", highlight.Plain)
	check("md5", highlight.OpName)
	if r[len(r)-1] != highlight.OpBrace || r[len(r)-3] != highlight.OpBrace {
		t.Errorf("braces of md5{a}: %v", r[len(r)-3:])
	}
	// A block going on below still has its head colored.
	r = roles(`x: to-base64{abc`)
	if r[3] != highlight.OpName || r[12] != highlight.OpBrace || r[13] != highlight.Plain {
		t.Errorf("open block: %v", r)
	}
	// Unknown ids, JSON and blocks with errors are left alone.
	for _, s := range []string{`{"a": "nope{x}"}`, `xmd5{a}`, `md5(bad){a}`} {
		for i, ro := range roles(s) {
			if ro != highlight.Plain {
				t.Errorf("%q: rune %d colored %v", s, i, ro)
			}
		}
	}
	lines := []string{"GET /to-hex{a b}/file{x} HTTP/1.1", "X: url-encode(encode-all){", "  multi", "}", `{"a": "md5{x}"}`}
	for i, l := range highlight.Message(lines, Highlight) {
		if ansi.Strip(l) != lines[i] {
			t.Errorf("line %d changed: %q", i, ansi.Strip(l))
		}
	}
}
