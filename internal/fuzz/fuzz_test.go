package fuzz

import (
	"encoding/base64"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ffuf/ffuf/v2/pkg/ffuf"

	"github.com/sectui/steep/internal/request"
	"github.com/sectui/steep/internal/run"
)

func init() { log.SetOutput(io.Discard) }

func settings(over map[string]string) map[string]string {
	s := map[string]string{
		"mode": "clusterbomb", "proto": "http", "followRedirects": "false", "recursion": "false",
		"recursionDepth": "0", "autocalibration": "false", "match.status": "all",
		"concurrency": "2", "timeout": "5", "rateLimit": "0",
	}
	for k, v := range over {
		s[k] = v
	}
	return s
}

func wordlistFile(t *testing.T, words ...string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "wl.txt")
	if err := os.WriteFile(p, []byte(strings.Join(words, "\n")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func wait(t *testing.T, r *Run) []run.Hit {
	t.Helper()
	var hits []run.Hit
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		u := r.Drain()
		hits = append(hits, u.Hits...)
		if u.Finished {
			return append(hits, r.Drain().Hits...)
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("run did not finish")
	return nil
}

func TestRun(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/admin" {
			w.WriteHeader(403)
		}
		fmt.Fprintf(w, "path=%s ua=%s", r.URL.Path, r.Header.Get("User-Agent"))
	}))
	defer srv.Close()
	host := strings.TrimPrefix(srv.URL, "http://")

	spec := Spec{
		Request:   "GET /FUZZ HTTP/1.1\nHost: " + host + "\nUser-Agent: mine\nContent-Length: 99\n\n",
		Settings:  settings(map[string]string{"filter.status": "404"}),
		Wordlists: []request.Wordlist{{Keyword: "FUZZ", Path: wordlistFile(t, "a", "admin", "b")}},
		Headers:   []string{"user-agent: imposed"},
	}
	if n, exact, err := spec.Count(); n != 3 || !exact || err != nil {
		t.Fatalf("count = %d, %v, %v", n, exact, err)
	}
	store, err := run.Create(filepath.Join(t.TempDir(), "run"))
	if err != nil {
		t.Fatal(err)
	}
	r, err := Start(spec, store)
	if err != nil {
		t.Fatal(err)
	}
	hits := wait(t, r)
	if len(hits) != 3 {
		t.Fatalf("%d hits: %+v", len(hits), hits)
	}
	var admin run.Hit
	for _, h := range hits {
		if h.Input["FUZZ"] == "admin" {
			admin = h
		}
	}
	if admin.Status != 403 || admin.URL != srv.URL+"/admin" || admin.Redirect != "" {
		t.Fatalf("admin hit = %+v", admin)
	}
	resp, _ := run.ReadResponse(store.Dir, admin.N)
	if !strings.HasPrefix(string(resp), "HTTP/1.1 403") || !strings.HasSuffix(string(resp), "path=/admin ua=imposed") {
		t.Fatalf("response dump = %q", resp)
	}
	req, _ := run.ReadRequest(store.Dir, admin.N)
	if !strings.Contains(string(req), "GET /admin") {
		t.Fatalf("request dump = %q", req)
	}
	m, kept, err := run.Load(store.Dir)
	if err != nil || len(kept) != 3 || m.Done != 3 {
		t.Fatalf("kept: %+v, %d hits, %v", m, len(kept), err)
	}
}

func TestPitchforkAndStop(t *testing.T) {
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-release
	}))
	defer srv.Close()
	defer close(release)
	u, _ := url.Parse(srv.URL)

	words := make([]string, 200)
	for i := range words {
		words[i] = fmt.Sprint(i)
	}
	spec := Spec{
		Request:  "GET /A/B HTTP/1.1\nHost: " + u.Host + "\n\n",
		Settings: settings(map[string]string{"mode": "pitchfork"}),
		Wordlists: []request.Wordlist{
			{Keyword: "A", Path: wordlistFile(t, words...)},
			{Keyword: "B", Path: wordlistFile(t, "x", "y")},
		},
	}
	if n, _, _ := spec.Count(); n != 200 {
		t.Fatalf("pitchfork count = %d", n)
	}
	store, _ := run.Create(filepath.Join(t.TempDir(), "run"))
	r, err := Start(spec, store)
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(100 * time.Millisecond)
	r.Stop()
	wait(t, r)
	if m, _, _ := run.Load(store.Dir); !m.Stopped {
		t.Fatalf("meta = %+v", m)
	}
}

func TestWithoutWordlist(t *testing.T) {
	var got []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		got = append(got, r.Method+" "+r.URL.RequestURI()+" "+string(b))
	}))
	defer srv.Close()
	u, _ := url.Parse(srv.URL)
	spec := Spec{
		Request:  "POST /a?b=c HTTP/1.1\nHost: " + u.Host + "\nContent-Type: text/plain\n\nhello",
		Settings: settings(nil),
	}
	if n, _, _ := spec.Count(); n != 1 {
		t.Fatalf("count = %d", n)
	}
	store, _ := run.Create(filepath.Join(t.TempDir(), "run"))
	r, err := Start(spec, store)
	if err != nil {
		t.Fatal(err)
	}
	hits := wait(t, r)
	if len(hits) != 1 || len(hits[0].Input) != 0 || len(got) != 1 || got[0] != "POST /a?b=c hello" {
		t.Fatalf("hits = %+v, server got %q", hits, got)
	}
}

func TestHTTP2(t *testing.T) {
	if raceEnabled {
		t.Skip("ffuf's response timer races over HTTP/2")
	}
	var got []string
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = append(got, r.Proto)
	}))
	srv.EnableHTTP2 = true
	srv.StartTLS()
	defer srv.Close()
	u, _ := url.Parse(srv.URL)
	for _, version := range []string{"HTTP/1.1", "HTTP/2", "http/2.0"} {
		got = nil
		spec := Spec{
			Request:  "GET / " + version + "\nHost: " + u.Host + "\n\n",
			Settings: settings(map[string]string{"proto": "https"}),
		}
		store, _ := run.Create(filepath.Join(t.TempDir(), "run"))
		r, err := Start(spec, store)
		if err != nil {
			t.Fatal(err)
		}
		wait(t, r)
		want := "HTTP/2.0"
		if version == "HTTP/1.1" {
			want = "HTTP/1.1"
		}
		if len(got) != 1 || got[0] != want {
			t.Errorf("%s: server got %q", version, got)
		}
	}
}

func TestPrepareErrors(t *testing.T) {
	wl := []request.Wordlist{{Keyword: "FUZZ", Path: "/x"}}
	for _, s := range []Spec{
		{Request: "GET / HTTP/1.1\n\n", Wordlists: wl},
		{Request: "GET /x HTTP/1.1\nHost: a\n\n", Wordlists: wl},
	} {
		if _, err := s.Prepare(); err == nil {
			t.Errorf("%q accepted", s.Request)
		}
	}
}

func TestUserAgent(t *testing.T) {
	for _, c := range []struct {
		req, def, suffix string
		imposed          []string
		want             string
	}{
		{"", "Mozilla/5.0", "", nil, "Mozilla/5.0"},
		{"", "Mozilla/5.0", "hunter", nil, "Mozilla/5.0 hunter"},
		{"mine", "Mozilla/5.0", "", nil, "mine"},
		{"mine", "Mozilla/5.0", "hunter", nil, "mine hunter"},
		{"mine", "Mozilla/5.0", "hunter", []string{"User-Agent: imposed"}, "imposed hunter"},
		{"", "", "hunter", nil, "hunter"},
		{"", "", "", nil, ""},
	} {
		req := "GET / HTTP/1.1\nHost: a.test\n"
		if c.req != "" {
			req += "user-agent: " + c.req + "\n"
		}
		s := Spec{Request: req + "\n", Headers: c.imposed, UserAgent: c.def, UserAgentSuffix: c.suffix}
		h, err := s.Prepare()
		if err != nil {
			t.Fatal(err)
		}
		if got := h.Header("User-Agent"); got != c.want {
			t.Errorf("%+v: User-Agent = %q", c, got)
		}
	}
}

func TestPrepareDropsPwnFox(t *testing.T) {
	s := Spec{Request: "GET / HTTP/1.1\nHost: a.test\nx-pwnfox-color: red\nAccept: */*\n\n"}
	h, err := s.Prepare()
	if err != nil {
		t.Fatal(err)
	}
	if h.Header("X-PwnFox-Color") != "" || h.Header("Accept") != "*/*" {
		t.Fatalf("headers = %+v", h.Headers)
	}
	s.KeepPwnFox = true
	if h, _ = s.Prepare(); h.Header("X-PwnFox-Color") != "red" {
		t.Fatalf("kept: headers = %+v", h.Headers)
	}
}

// fakeRunner records what Prepare is given.
type fakeRunner struct{ got *ffuf.Request }

func (f *fakeRunner) Prepare(input map[string][]byte, base *ffuf.Request) (ffuf.Request, error) {
	req := ffuf.CopyRequest(base)
	f.got = &req
	return req, nil
}
func (f *fakeRunner) Execute(*ffuf.Request) (ffuf.Response, error) { return ffuf.Response{}, nil }
func (f *fakeRunner) Dump(*ffuf.Request) ([]byte, error)           { return nil, nil }

func TestRecorderPrepare(t *testing.T) {
	body, err := request.ParseBody("to-upper-case{file{m}} /to-hex{file{u}}?q=url-encode{file{u}}\nto-base64{file{h}}: md5{file{h}}\n\nb64=to-base64{file{u}}")
	if err != nil {
		t.Fatal(err)
	}
	e, err := body.Expand()
	if err != nil {
		t.Fatal(err)
	}
	ph := func(i int) string { return e.Dynamic[i].Placeholder }
	base := ffuf.Request{
		Method:  ph(0),
		Host:    ph(1),
		Url:     "http://" + ph(1) + "/x?q=" + ph(2),
		Headers: map[string]string{ph(3): ph(4)},
		Data:    []byte("b64=" + ph(5)),
	}
	fake := &fakeRunner{}
	rec := &recorder{RunnerProvider: fake, r: &Run{}, dynamic: e.Dynamic}
	input := map[string][]byte{"§1§": []byte("get"), "§2§": []byte("a b"), "§3§": []byte("a b"), "§4§": []byte("hh"), "§5§": []byte("hh"), "§6§": []byte("a b")}
	if _, err := rec.Prepare(input, &base); err != nil {
		t.Fatal(err)
	}
	got := fake.got
	if got.Method != "GET" || got.Host != "612062" || got.Url != "http://612062/x?q=a%20b" ||
		got.Headers["aGg="] != "5e36941b3d856737e81516acd45edc50" || string(got.Data) != "b64=YSBi" {
		t.Fatalf("got %+v", got)
	}
	// the base request is left as it was, for the next ones
	if base.Method != ph(0) || string(base.Data) != "b64="+ph(5) {
		t.Fatalf("base changed: %+v", base)
	}
	// a failure is kept as the run's first error
	body, _ = request.ParseBody("from-base64(strict){file{x}}")
	e, _ = body.Expand()
	rec = &recorder{RunnerProvider: fake, r: &Run{}, dynamic: e.Dynamic}
	if _, err := rec.Prepare(map[string][]byte{"§1§": []byte("!")}, &ffuf.Request{}); err == nil || !strings.Contains(rec.r.firstUnbuilt, `"!": from-base64: invalid Base64`) {
		t.Fatalf("got %v, first error %q", err, rec.r.firstUnbuilt)
	}
}

func TestOperationsRun(t *testing.T) {
	var mu sync.Mutex
	got := map[string]string{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		got[r.URL.Query().Get("u")] = r.Header.Get("X-Up")
		mu.Unlock()
	}))
	defer srv.Close()
	host := strings.TrimPrefix(srv.URL, "http://")
	words := make([]string, 300)
	for i := range words {
		words[i] = fmt.Sprintf("wörd%d", i)
	}
	wl := wordlistFile(t, words...)
	spec := Spec{
		Request:     "GET /?u=url-encode(encode-all){to-base64{file{x}}} HTTP/1.1\nHost: " + host + "\nX-Up: to-upper-case{file{y}}\n\n",
		HeaderLines: 1,
		Settings:    settings(map[string]string{"concurrency": "40", "mode": "pitchfork"}),
		Wordlists:   []request.Wordlist{{Keyword: "§1§", Path: wl}, {Keyword: "§2§", Path: wl}},
	}
	store, _ := run.Create(filepath.Join(t.TempDir(), "run"))
	r, err := Start(spec, store)
	if err != nil {
		t.Fatal(err)
	}
	hits := wait(t, r)
	if len(hits) != 300 || len(got) != 300 {
		t.Fatalf("%d hits, %d received", len(hits), len(got))
	}
	for _, w := range words {
		b64 := base64.StdEncoding.EncodeToString([]byte(w))
		if up := got[b64]; up != strings.ToUpper(w) {
			t.Errorf("%s: header %q", w, up)
		}
	}
	for _, h := range hits {
		if len(h.Input) != 2 || !strings.HasPrefix(h.Input["§1§"], "wörd") {
			t.Fatalf("inputs %v", h.Input)
		}
	}
	if !strings.Contains(r.URL(), "url-encode(encode-all){to-base64{file{x}}}") {
		t.Errorf("run URL: %q", r.URL())
	}
	req, _ := run.ReadRequest(store.Dir, hits[0].N)
	if strings.Contains(string(req), "§") {
		t.Errorf("placeholder left in the dump: %q", req)
	}

	// errors once per run: in the spec, with the line in the file
	spec.Request = "GET /from-base64(strict){!} HTTP/1.1\nHost: x\n\n"
	if _, err := spec.Prepare(); err == nil || !strings.Contains(err.Error(), "line 2, column 6") {
		t.Errorf("got %v", err)
	}
}

// A run that cannot start says why: ffuf's Multierror wraps nothing.
func TestStartError(t *testing.T) {
	wl := wordlistFile(t, "a")
	spec := Spec{
		Request:   "GET /FUZZ HTTP/1.1\nHost: example.com\n\n",
		Settings:  settings(nil),
		Wordlists: []request.Wordlist{{Keyword: "FUZZ", Path: wl}},
	}
	os.Remove(wl) // gone between the count and the run
	store, _ := run.Create(filepath.Join(t.TempDir(), "run"))
	defer store.Close(run.Meta{})
	_, err := Start(spec, store)
	if err == nil || !strings.Contains(err.Error(), wl) || strings.Contains(err.Error(), "occured") {
		t.Fatalf("err = %v", err)
	}
}

func TestMultiError(t *testing.T) {
	for in, want := range map[string]string{
		"1 errors occured.\n\t* a\n":        "a",
		"2 errors occured.\n\t* a\n\t* b\n": "a; b",
		"something else":                    "something else",
		"x errors occured.\n\t* a\n":        "x errors occured.\n\t* a\n",
	} {
		if got := multiError(fmt.Errorf("%s", in)).Error(); got != want {
			t.Errorf("%q: %q, want %q", in, got, want)
		}
	}
}

func TestInputTextOrder(t *testing.T) {
	in := map[string][]byte{"§10§": []byte("j"), "§2§": []byte("b"), "§1§": []byte("a"), "FFUFHASH": nil}
	if got := inputText(in); got != `"a", "b", "j"` {
		t.Fatalf("got %s", got)
	}
}

func TestInlineWordlistsRun(t *testing.T) {
	var mu sync.Mutex
	got := map[string]bool{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		got[r.URL.Path] = true
		mu.Unlock()
	}))
	defer srv.Close()
	host := strings.TrimPrefix(srv.URL, "http://")
	text := "GET /list{admin,root}/range{08-09} HTTP/1.1\nHost: " + host + "\n\n"
	body, err := request.ParseBody(text)
	if err != nil {
		t.Fatal(err)
	}
	spec := Spec{Request: text, Settings: settings(nil), Wordlists: body.Wordlists()}
	if n, exact, err := spec.Count(); n != 4 || !exact || err != nil {
		t.Fatalf("count = %d, %v, %v", n, exact, err)
	}
	store, _ := run.Create(filepath.Join(t.TempDir(), "run"))
	r, err := Start(spec, store)
	if err != nil {
		t.Fatal(err)
	}
	if hits := wait(t, r); len(hits) != 4 {
		t.Fatalf("%d hits", len(hits))
	}
	for _, p := range []string{"/admin/08", "/admin/09", "/root/08", "/root/09"} {
		if !got[p] {
			t.Errorf("%s not sent; got %v", p, got)
		}
	}
}

func TestRecursion(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/admin":
			http.Redirect(w, r, "/admin/", http.StatusMovedPermanently)
		case "/admin/secret":
			w.WriteHeader(http.StatusOK)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()
	host := strings.TrimPrefix(srv.URL, "http://")

	kw := request.Keyword(1)
	spec := Spec{
		Request:   "GET /file{wl} HTTP/1.1\nHost: " + host + "\n\n",
		Settings:  settings(map[string]string{"recursion": "true", "filter.status": "404"}),
		Wordlists: []request.Wordlist{{Keyword: kw, Path: wordlistFile(t, "admin", "secret")}},
	}
	store, err := run.Create(filepath.Join(t.TempDir(), "run"))
	if err != nil {
		t.Fatal(err)
	}
	r, err := Start(spec, store)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for _, h := range wait(t, r) {
		got[strings.TrimPrefix(h.URL, srv.URL)] = h.Input[kw]
	}
	want := map[string]string{"/admin": "admin", "/admin/secret": "secret"}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("hits by URL, with their %s: %v, want %v", kw, got, want)
	}
	if r.URL() != srv.URL+"/"+kw {
		t.Fatalf("run URL = %q", r.URL())
	}
}

func TestRecursionErrors(t *testing.T) {
	one := []request.Wordlist{{Keyword: request.Keyword(1), Path: "/x"}}
	two := append(one, request.Wordlist{Keyword: request.Keyword(2), Path: "/y"})
	for _, s := range []Spec{
		{Request: "GET /admin HTTP/1.1\nHost: a\n\n"},
		{Request: "GET /file{x}?a=1 HTTP/1.1\nHost: a\n\n", Wordlists: one},
		{Request: "GET /file{x}/file{y} HTTP/1.1\nHost: a\n\n", Wordlists: two},
		{Request: "GET /file{x} HTTP/1.1\nHost: a\nX-Test: FUZZ\n\n", Wordlists: one},
	} {
		s.Settings = settings(map[string]string{"recursion": "true"})
		if _, err := s.Prepare(); err == nil {
			t.Errorf("%q accepted with recursion", s.Request)
		}
	}
}
