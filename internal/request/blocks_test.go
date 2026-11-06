package request

import (
	"errors"
	"strings"
	"testing"

	"github.com/sectui/operations/template"
)

func mustParse(t *testing.T, s string) *Body {
	t.Helper()
	b, err := ParseBody(s)
	if err != nil {
		t.Fatalf("%q: %v", s, err)
	}
	return b
}

func TestWordlists(t *testing.T) {
	b := mustParse(t, Parse(sample).Body)
	wls := b.Wordlists()
	if len(wls) != 2 || wls[0].Keyword != "§1§" || wls[0].Path != "~/wl/common.txt" || wls[1].Keyword != "§2§" || wls[1].Path != "$word/users.txt" {
		t.Fatalf("wordlists = %+v", wls)
	}
	e, err := b.Expand()
	if err != nil || !strings.HasPrefix(e.Text, "GET /§1§?u=§2§ HTTP/1.1") || len(e.Dynamic) != 0 {
		t.Fatalf("expanded = %q, %v", e.Text, err)
	}
	if w := mustParse(t, "x file{ a b } y").Wordlists(); w[0].Path != "a b" {
		t.Fatalf("path not trimmed: %q", w[0].Path)
	}
	// Numbered in the order they are written, nested ones included.
	w := mustParse(t, "a to-hex{file{x}} file{y} url-encode{to-base64{file{z}}}").Wordlists()
	if len(w) != 3 || w[0].Path != "x" || w[1].Keyword != "§2§" || w[1].Path != "y" || w[2].Path != "z" {
		t.Fatalf("wordlists = %+v", w)
	}
}

func TestBodyProblems(t *testing.T) {
	for bad, want := range map[string]string{
		"GET /file{} HTTP/1.1":                         "file{} needs a path",
		"GET /file{a HTTP/1.1":                         "missing closing brace",
		"GET /file{c:/x} HTTP/1.1":                     "cannot hold ':'",
		"GET /file{to-hex{a}} HTTP/1.1":                "plain path",
		"GET /file{a\nb} HTTP/1.1":                     "on one line",
		"GET /to-hex(nope){a} HTTP/1.1":                `unknown argument "nope"`,
		"GET / HTTP/1.1\n\nto-base64{a{b}":             "missing closing brace",
		"GET /x HTTP/1.1\nX: to-hex(delimiter=bad){a}": `invalid value "bad"`,
	} {
		_, err := ParseBody(bad)
		var terr *template.Error
		if !errors.As(err, &terr) || !strings.Contains(err.Error(), want) {
			t.Errorf("%q: got %v, want %q", bad, err, want)
		}
	}
	// Lines count from the top of the file.
	f := Parse("# steep id: x\n# steep mode: pitchfork\nGET / HTTP/1.1\nX: file{}\n")
	errs := f.Problems()
	if len(errs) != 1 || !strings.Contains(errs[0].Error(), "line 4, column 4") {
		t.Fatalf("problems = %v", errs)
	}
	var terr *template.Error
	if errors.As(errs[0], &terr); terr.Pos != strings.Index(f.String(), "file{}") {
		t.Errorf("pos = %d", terr.Pos)
	}
	// Plain JSON, backslashes and lone braces are not blocks.
	for _, ok := range []string{`{"a": "b\\c"} } {`, "xfile{a}", `\file{a}`, "md5 {x}"} {
		b := mustParse(t, ok)
		if len(b.Wordlists()) != 0 {
			t.Errorf("%q has a wordlist", ok)
		}
		if e, _ := b.Expand(); e.Text != strings.Replace(ok, `\file`, "file", 1) {
			t.Errorf("%q expanded to %q", ok, e.Text)
		}
	}
}

func TestExpand(t *testing.T) {
	src := "POST /to-hex{ab} HTTP/1.1\nX: to-base64{file{u.txt}}\n\n{\"p\": \"url-encode(encode-all){to-base64{file{p.txt}}:x}\", \"q\": \"md5{a}\"}"
	b := mustParse(t, src)
	e, err := b.Expand()
	if err != nil {
		t.Fatal(err)
	}
	want := "POST /6162 HTTP/1.1\nX: §op1:§1§§\n\n{\"p\": \"§op2:§2§§\", \"q\": \"0cc175b9c0f1b6a831c399e269772661\"}"
	if e.Text != want {
		t.Fatalf("got\n%q\nwant\n%q", e.Text, want)
	}
	if len(e.Dynamic) != 2 || e.Dynamic[0].Source != "to-base64{file{u.txt}}" {
		t.Fatalf("dynamic = %+v", e.Dynamic)
	}
	v, err := e.Dynamic[0].Eval(map[string][]byte{"§1§": []byte("admin"), "§2§": []byte("x")})
	if err != nil || string(v) != "YWRtaW4=" {
		t.Errorf("got %q, %v", v, err)
	}
	v, err = e.Dynamic[1].Eval(map[string][]byte{"§1§": []byte("x"), "§2§": []byte("a?b")})
	if err != nil || string(v) != "YT9i%3Ax" {
		t.Errorf("got %q, %v", v, err)
	}

	// A failing operation names itself and its place.
	_, err = mustParse(t, "GET / HTTP/1.1\n\nfrom-base64(strict){!}").Expand()
	if err == nil || !strings.Contains(err.Error(), "from-base64: invalid Base64") || !strings.Contains(err.Error(), "line 3") {
		t.Errorf("got %v", err)
	}
	d, _ := mustParse(t, "from-base64(strict){file{x}}").Expand()
	if _, err := d.Dynamic[0].Eval(map[string][]byte{"§1§": []byte("!")}); err == nil {
		t.Error("no error for each request")
	}
	// Chains keep their order.
	e, _ = mustParse(t, "to-base64|url-encode(encode-all){file{x}}").Expand()
	if v, _ := e.Dynamic[0].Eval(map[string][]byte{"§1§": []byte("hi?")}); string(v) != "aGk%2F" {
		t.Errorf("chain: %q", v)
	}
}

func TestMask(t *testing.T) {
	b := mustParse(t, "GET /a/to-hex(delimiter=space){x y}/file{my list.txt}?q=1 HTTP/1.1\nHost: h")
	masked, unmask := b.Mask()
	if masked != "GET /a/§b1§/§b2§?q=1 HTTP/1.1\nHost: h" {
		t.Fatalf("masked = %q", masked)
	}
	if got := unmask("https://h/a/§b1§/§b2§?q=1"); got != "https://h/a/to-hex(delimiter=space){x y}/file{my list.txt}?q=1" {
		t.Errorf("unmasked = %q", got)
	}
}

func TestLineBlocks(t *testing.T) {
	bs := Blocks("a file{x} to-hex{file{ y }} xfile{z} -file{w}")
	if len(bs) != 2 || bs[0].Path != "x" || bs[1].Path != "y" {
		t.Fatalf("blocks = %+v", bs)
	}
}

func TestErrorLinesFromTheTop(t *testing.T) {
	f := Parse("# steep id: x\n# note\nGET /from-base64(strict){!} HTTP/1.1\nHost: h\n")
	b, err := f.ParseBody()
	if err != nil {
		t.Fatal(err)
	}
	_, err = b.Expand()
	var terr *template.Error
	if !errors.As(err, &terr) || terr.Line != 3 || terr.Column != 6 || terr.Pos != strings.Index(f.String(), "from-base64") {
		t.Fatalf("got %+v", err)
	}
	// a body parsed on its own counts from its own start
	b, _ = ParseBody(f.Body)
	if _, err := b.Expand(); !strings.Contains(err.Error(), "line 1, column 6") {
		t.Fatalf("got %v", err)
	}
}

// A path with braces, escaped, reads back as written by both parsers.
func TestEscapePath(t *testing.T) {
	path := `/w/a}b{c\d.txt`
	line := "GET /file{" + EscapePath(path) + "} HTTP/1.1"
	if bs := Blocks(line); len(bs) != 1 || bs[0].Path != path || bs[0].End != len(line)-len(" HTTP/1.1") {
		t.Fatalf("Blocks = %+v", bs)
	}
	b, err := ParseBody(line + "\nHost: h\n\n")
	if err != nil {
		t.Fatal(err)
	}
	if ws := b.Wordlists(); len(ws) != 1 || ws[0].Path != path {
		t.Fatalf("Wordlists = %+v", ws)
	}
}

func TestInlineWordlists(t *testing.T) {
	w := mustParse(t, `GET /list{admin, root ,a\,b}/range{08-11}/range{3-4} HTTP/1.1`).Wordlists()
	if len(w) != 3 || w[0].Keyword != "§1§" || strings.Join(w[0].Lines, "|") != "admin|root|a,b" {
		t.Fatalf("list = %+v", w)
	}
	if strings.Join(w[1].Lines, " ") != "08 09 10 11" || strings.Join(w[2].Lines, " ") != "3 4" {
		t.Fatalf("ranges = %q %q", w[1].Lines, w[2].Lines)
	}
	b := mustParse(t, "a to-base64{list{x,y}} b")
	e, err := b.Expand()
	if err != nil || len(e.Dynamic) != 1 {
		t.Fatalf("expanded = %+v, %v", e, err)
	}
	if v, _ := e.Dynamic[0].Eval(map[string][]byte{"§1§": []byte("x")}); string(v) != "eHg=" && string(v) != "eA==" {
		t.Fatalf("eval = %q", v)
	}
	for _, bad := range []string{"range{5-1}", "range{a-b}", "range{1-99999999}", "list{}", "range{}"} {
		if _, err := ParseBody(bad); err == nil {
			t.Errorf("%s accepted", bad)
		}
	}
}

func TestEnv(t *testing.T) {
	t.Setenv("STEEP_TEST_TOKEN", "from-shell")
	Env = map[string]string{"token": "from-config", "STEEP_TEST_TOKEN": "config-wins"}
	defer func() { Env = nil }()
	e, err := mustParse(t, "a env{token} env{STEEP_TEST_TOKEN} b").Expand()
	if err != nil || e.Text != "a from-config config-wins b" {
		t.Fatalf("expanded = %q, %v", e.Text, err)
	}
	Env = nil
	if e, err := mustParse(t, "env{STEEP_TEST_TOKEN}").Expand(); err != nil || e.Text != "from-shell" {
		t.Fatalf("from the shell: %q, %v", e.Text, err)
	}
	if _, err := mustParse(t, "env{steep_nope_unset}").Expand(); err == nil || !strings.Contains(err.Error(), "steep_nope_unset") {
		t.Fatalf("unset: %v", err)
	}
}

func TestEscapeBlocks(t *testing.T) {
	for _, s := range []string{
		"GET /a?x=md5{1}&y=file{/etc/passwd} HTTP/1.1",
		`{"a": to-base64{x}, "b": "list{a,b}", "c": env{HOME}}`,
		"to-hex{a md5{b} c}",
		"unclosed md5{x and range{1-2",
		"to-hex(delimiter=colon){ab} and url-encode|md5{z}",
		"no blocks {here}",
	} {
		e := EscapeBlocks(s)
		b, err := ParseBody(e)
		if err != nil {
			t.Fatalf("%q -> %q: %v", s, e, err)
		}
		x, err := b.Expand()
		if err != nil || x.Text != s || len(b.Wordlists()) != 0 {
			t.Errorf("%q -> %q -> %q, %v", s, e, x.Text, err)
		}
	}
}
