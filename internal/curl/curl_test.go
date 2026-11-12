package curl

import (
	"os"
	"strings"
	"testing"

	"github.com/anotherhadi/steep/internal/request"
)

func TestParse(t *testing.T) {
	cmd := `curl 'https://api.test/login?x=1' \
  -H 'Content-Type: application/json' \
  -H $'X-Quote: it\'s' \
  -b 'sid=1' \
  --data-raw '{"u":"a"}' \
  -k --compressed -x http://127.0.0.1:8080 --max-time 5`
	h, err := Parse(cmd)
	if err != nil {
		t.Fatal(err)
	}
	if h.Method != "POST" || h.Target != "/login?x=1" || h.Body != `{"u":"a"}` {
		t.Fatalf("got %+v", h)
	}
	if h.Header("Host") != "api.test" || h.Header("Cookie") != "sid=1" || h.Header("X-Quote") != "it's" {
		t.Fatalf("headers = %+v", h.Headers)
	}
	if Scheme(cmd) != "https" {
		t.Fatal("scheme")
	}
}

func TestParseGet(t *testing.T) {
	h, err := Parse(`curl -G -d a=1 -XPUT example.com`)
	if err != nil {
		t.Fatal(err)
	}
	if h.Method != "PUT" || h.Target != "/?a=1" || h.Body != "" {
		t.Fatalf("got %+v", h)
	}
}

func TestFormat(t *testing.T) {
	h := request.HTTP{Method: "POST", Target: "/a", Version: "HTTP/1.1", Body: "it's $x",
		Headers: []request.Header{{Name: "Host", Value: "x.test"}, {Name: "Accept-Encoding", Value: "gzip"}, {Name: "Content-Length", Value: "7"}, {Name: "X-A", Value: "b"}}}
	got := Format(h, "https://x.test/a")
	want := "curl \\\n  -X 'POST' \\\n  'https://x.test/a' \\\n  -H 'X-A: b' \\\n  --compressed \\\n  --data-binary 'it'\\''s $x'"
	if got != want {
		t.Fatalf("got\n%s\nwant\n%s", got, want)
	}
	back, err := Parse(got)
	if err != nil || back.Body != "it's $x" || back.Method != "POST" {
		t.Fatalf("round trip: %+v, %v", back, err)
	}
}

func TestHTTP2(t *testing.T) {
	h, err := Parse("curl --http2 https://x.test/a")
	if err != nil || !h.IsHTTP2() {
		t.Fatalf("import: %+v, %v", h, err)
	}
	if got := Format(h, "https://x.test/a"); !strings.Contains(got, "--http2") {
		t.Fatalf("export: %s", got)
	}
	h, _ = Parse("curl https://x.test/a")
	if h.IsHTTP2() || strings.Contains(Format(h, "https://x.test/a"), "--http2") {
		t.Fatal("HTTP/2 without --http2")
	}
}

func TestNotCurl(t *testing.T) {
	if _, err := Parse("wget x"); err == nil || !strings.Contains(err.Error(), "curl") {
		t.Fatalf("got %v", err)
	}
}

func TestLenientURL(t *testing.T) {
	for in, want := range map[string]string{
		`curl -is "https:/dmarc.live/info/free.fr"`: "dmarc.live /info/free.fr https",
		`curl https:///a.test/x`:                    "a.test /x https",
		`curl HTTP://a.test`:                        "a.test / http",
		`curl a.test:8080/p?q=1`:                    "a.test:8080 /p?q=1 http",
	} {
		h, err := Parse(in)
		if err != nil {
			t.Fatalf("%s: %v", in, err)
		}
		if got := h.Header("Host") + " " + h.Target + " " + Scheme(in); got != want {
			t.Errorf("%s: got %q, want %q", in, got, want)
		}
	}
}

func TestParseMore(t *testing.T) {
	dir := t.TempDir()
	f := dir + "/body.txt"
	os.WriteFile(f, []byte("a=1\nb=2\n"), 0o644)
	for in, want := range map[string]string{
		`curl --proto =https --noproxy '*' https://a.test/x`:             "GET /x ",
		`curl --data-urlencode 'q=a b&c' --data-urlencode '=x y' a.test`: "POST / q=a+b%26c&x+y",
		`curl -d @` + f + ` a.test`:                                      "POST / a=1b=2",
		`curl --data-binary @` + f + ` a.test`:                           "POST / a=1\nb=2\n",
		`curl --oauth2-bearer tok a.test`:                                "GET / Bearer tok",
	} {
		h, err := Parse(in)
		if err != nil {
			t.Fatalf("%s: %v", in, err)
		}
		got := h.Method + " " + h.Target + " " + h.Body
		if h.Header("Authorization") != "" {
			got = h.Method + " " + h.Target + " " + h.Header("Authorization")
		}
		if got != want {
			t.Errorf("%s:\n got %q\nwant %q", in, got, want)
		}
	}
	if _, err := Parse(`curl -F 'file=@x' https://a.test`); err == nil || !strings.Contains(err.Error(), "-F") {
		t.Fatalf("-F: %v", err)
	}
}

func TestImportArgs(t *testing.T) {
	text, err := ImportArgs([]string{"-i", "http://a.test/x?y=1", "-H", "X-A: b"})
	if err != nil {
		t.Fatal(err)
	}
	want := "# steep proto: http\nGET /x?y=1 HTTP/1.1\nHost: a.test\nX-A: b\n\n"
	if text != want {
		t.Fatalf("got %q", text)
	}
}

func TestSplitANSIC(t *testing.T) {
	got, err := split(`curl --data-binary $'a\tb\x41\a\b\e\f\v\'c'`)
	if err != nil {
		t.Fatal(err)
	}
	if want := "a\tbA\a\b\x1b\f\v'c"; len(got) != 3 || got[2] != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}
