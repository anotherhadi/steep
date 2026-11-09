package request

import (
	"strings"
	"testing"
)

const sample = `# steep id: abc
# a note of mine
# steep match.status: 200-299,403
GET /file{~/wl/common.txt}?u=file{$word/users.txt} HTTP/1.1
Host: api.test
Accept: */*

body`

func TestParseRoundTrip(t *testing.T) {
	f := Parse(sample)
	if len(f.Header) != 3 || !strings.HasPrefix(f.Body, "GET /file{") {
		t.Fatalf("header = %q, body = %q", f.Header, f.Body)
	}
	if f.String() != sample {
		t.Fatalf("round trip:\n%s", f.String())
	}
	if f.ID() != "abc" {
		t.Fatalf("id = %q", f.ID())
	}
}

func TestSetUnset(t *testing.T) {
	f := Parse(sample)
	f.Set("match.status", "all")
	f.Set("mode", "pitchfork")
	if v, _ := f.Get("match.status"); v != "all" {
		t.Fatalf("match.status = %q", v)
	}
	if f.Header[2] != "# steep match.status: all" || f.Header[3] != "# steep mode: pitchfork" {
		t.Fatalf("header = %q", f.Header)
	}
	f.Unset("mode")
	if _, ok := f.Get("mode"); ok || f.Header[1] != "# a note of mine" {
		t.Fatalf("header = %q", f.Header)
	}
}

func TestEffective(t *testing.T) {
	f := Parse(sample)
	f.Set("mode", "nonsense")
	got := f.Effective(map[string]string{"mode": "clusterbomb", "match.status": "all"})
	if got["mode"] != "clusterbomb" || got["match.status"] != "200-299,403" {
		t.Fatalf("got %v", got)
	}
	if errs := f.Problems(); len(errs) != 1 {
		t.Fatalf("problems = %v", errs)
	}
}

func TestValidate(t *testing.T) {
	for _, c := range []struct {
		key, value string
		ok         bool
	}{
		{"match.status", "all", true},
		{"match.status", "200-299, 403", true},
		{"match.size", "all", false},
		{"filter.regex", "(", false},
		{"recursionDepth", "-1", false},
		{"mode", "pitchfork", true},
	} {
		if err := Validate(c.key, c.value); (err == nil) != c.ok {
			t.Errorf("Validate(%q, %q) = %v", c.key, c.value, err)
		}
	}
}

func TestHTTP(t *testing.T) {
	h, err := ParseHTTP(Parse(sample).Body)
	if err != nil {
		t.Fatal(err)
	}
	if h.Method != "GET" || h.Header("host") != "api.test" || h.Body != "body" {
		t.Fatalf("got %+v", h)
	}
	if u := h.URL("https"); u != "https://api.test/file{~/wl/common.txt}?u=file{$word/users.txt}" {
		t.Fatalf("url = %q", u)
	}
	h.SetHeader("accept", "text/html")
	if len(h.Headers) != 2 || h.Headers[1] != (Header{"accept", "text/html"}) {
		t.Fatalf("headers = %+v", h.Headers)
	}
	if n := SuggestName(h, "https"); n != "api.test/file-wl-common.txt" {
		t.Fatalf("name = %q", n)
	}
	if m := Method(sample); m != "GET" {
		t.Fatalf("method = %q", m)
	}
}

func TestParseHTTPTrailingBlankLine(t *testing.T) {
	// a single trailing newline, with no body, must not read as a blank
	// header: it is sent without that last line, not rejected
	h, err := ParseHTTP("GET / HTTP/1.1\nHost: api.test\n")
	if err != nil {
		t.Fatal(err)
	}
	if h.Method != "GET" || h.Header("host") != "api.test" || h.Body != "" {
		t.Fatalf("got %+v", h)
	}
}

func TestPeek(t *testing.T) {
	for in, want := range map[string]string{
		"GET /a HTTP/1.1\nX-Being-Typed\nHost: h\n\nbody": "GET /a h",
		"\nPOST /b\nhost:h2":                              "POST /b h2",
		" /file{x} HTTP/1.1\nHost: h":                     "",
		"Host: h":                                         "",
		"GET /a HTTP/1.1\n\nHost: not-a-header":           "GET /a ",
		"get /a HTTP/1.1\nHost: h":                        "get /a h",
		"get /a":                                          "",
	} {
		method, target, host, ok := Peek(in)
		got := ""
		if ok {
			got = method + " " + target + " " + host
		}
		if got != want {
			t.Errorf("Peek(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestPwnFoxColor(t *testing.T) {
	for in, want := range map[string]string{
		"GET / HTTP/1.1\nHost: h\nX-PwnFox-Color: red\n\nbody":      "red",
		"# steep id: x\n\nGET / HTTP/1.1\nx-pwnfox-color:  Blue \n": "blue",
		"GET / HTTP/1.1\nX-Being-Typed\nX-PwnFox-Color: green":      "green",
		"GET / HTTP/1.1\nHost: h\n\nX-PwnFox-Color: red":            "",
		"X-PwnFox-Color: red":     "",
		"GET / HTTP/1.1\nHost: h": "",
	} {
		if got := PwnFoxColor(in); got != want {
			t.Errorf("PwnFoxColor(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestIDStaysInRuns(t *testing.T) {
	for _, bad := range []string{"..", ".", "../../home", "a/b", `a\b`, "/etc", ""} {
		f := Parse("# steep id: " + bad + "\nGET / HTTP/1.1\n")
		if id := f.ID(); id != "" {
			t.Errorf("id %q accepted as %q", bad, id)
		}
		id := f.EnsureID()
		if f.ID() != id || strings.Count(f.String(), "steep id:") != 1 {
			t.Errorf("%q: not replaced:\n%s", bad, f.String())
		}
	}
	if id := Parse("# steep id: 3f9a1c\n").ID(); id != "3f9a1c" {
		t.Errorf("good id = %q", id)
	}
}
