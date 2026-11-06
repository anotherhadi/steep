// Package fuzz runs ffuf's engine, in process, over a request and its
// wordlists.
package fuzz

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/ffuf/ffuf/v2/pkg/ffuf"
	"github.com/ffuf/ffuf/v2/pkg/filter"

	"github.com/sectui/steep/internal/request"
	"github.com/sectui/steep/internal/wordlist"
)

type Spec struct {
	// Request is the raw HTTP request, without its `# steep` block, its
	// blocks as written.
	Request string
	// HeaderLines is how many lines come before Request in its file, for the
	// positions of errors.
	HeaderLines int
	// Settings are the request's effective settings (request.Settings keys).
	Settings map[string]string
	// Wordlists have their paths resolved.
	Wordlists []request.Wordlist

	// Headers are imposed on the request: "Name: value".
	Headers []string
	// UserAgent is the User-Agent of a request without one ("": ffuf's).
	UserAgent string
	// UserAgentSuffix is appended to the User-Agent, after a space.
	UserAgentSuffix string
	// KeepPwnFox sends the PwnFox color header, for a proxy that reads it.
	KeepPwnFox bool
}

// Prepare checks the spec and returns the request as ffuf will read it: its
// blocks computed (wordlists as keywords, operations over wordlists as
// placeholders), imposed headers applied, the User-Agent completed,
// Content-Length dropped (ffuf's client computes it), and the PwnFox color
// unless kept.
func (s Spec) Prepare() (request.HTTP, error) {
	h, _, err := s.prepare()
	return h, err
}

func (s Spec) prepare() (request.HTTP, []request.Dynamic, error) {
	f := request.File{Header: make([]string, s.HeaderLines), Body: s.Request}
	body, err := f.ParseBody()
	if err != nil {
		return request.HTTP{}, nil, err
	}
	exp, err := body.Expand()
	if err != nil {
		return request.HTTP{}, nil, err
	}
	h, err := s.prepareHTTP(exp.Text)
	return h, exp.Dynamic, err
}

func (s Spec) prepareHTTP(text string) (request.HTTP, error) {
	h, err := request.ParseHTTP(text)
	if err != nil {
		return h, err
	}
	if h.Header("Host") == "" && !strings.Contains(h.Target, "://") {
		return h, errors.New("the request has no Host header")
	}
	for _, hd := range s.Headers {
		name, value, _ := strings.Cut(hd, ":")
		h.SetHeader(strings.TrimSpace(name), strings.TrimSpace(value))
	}
	ua := h.Header("User-Agent")
	if ua == "" {
		ua = s.UserAgent
	}
	if s.UserAgentSuffix != "" {
		ua = strings.TrimSpace(ua + " " + s.UserAgentSuffix)
	}
	if ua != "" {
		h.SetHeader("User-Agent", ua)
	}
	h.DelHeader("Content-Length")
	if !s.KeepPwnFox {
		h.DelHeader(request.PwnFoxHeader)
	}
	text = h.String()
	seen := map[string]bool{}
	for _, w := range s.Wordlists {
		if seen[w.Keyword] {
			return h, fmt.Errorf("keyword %s has two wordlists", w.Keyword)
		}
		seen[w.Keyword] = true
		if !strings.Contains(text, w.Keyword) {
			return h, fmt.Errorf("keyword %s is not in the request", w.Keyword)
		}
	}
	if s.recursing() {
		if len(s.Wordlists) != 1 || !strings.HasSuffix(h.Target, s.Wordlists[0].Keyword) {
			return h, errors.New("recursion takes one wordlist, at the end of the path: GET /admin/file{$w/common}")
		}
		if strings.Contains(text, recursionKeyword) {
			return h, errors.New("with recursion, the request cannot hold FUZZ: ffuf would replace it")
		}
	}
	return h, nil
}

// recursionKeyword is the only keyword ffuf recurses with: the URL must end
// with it, and ffuf appends "/FUZZ" to each directory it finds.
const recursionKeyword = "FUZZ"

func (s Spec) recursing() bool { return s.Settings["recursion"] == "true" }

// Count is how many requests the run sends. exact is false with recursion,
// which adds requests as it finds directories.
func (s Spec) Count() (n int, exact bool, err error) {
	if len(s.Wordlists) == 0 {
		return 1, true, nil
	}
	pitchfork := s.Settings["mode"] == "pitchfork"
	if !pitchfork {
		n = 1
	}
	for _, w := range s.Wordlists {
		c := len(w.Lines)
		if w.Lines == nil {
			var err error
			if c, err = wordlist.Count(w.Path); err != nil {
				return 0, false, err
			}
		}
		if c == 0 {
			return 0, false, fmt.Errorf("%s is empty", w.Path)
		}
		if pitchfork {
			n = max(n, c)
		} else {
			n *= c
		}
	}
	return n, s.Settings["recursion"] != "true", nil
}

func (s Spec) options(reqFile string) *ffuf.ConfigOptions {
	o := ffuf.NewConfigOptions()
	set := s.Settings
	o.Input.Request = reqFile
	o.Input.RequestProto = set["proto"]
	o.Input.InputMode = set["mode"]
	for _, w := range s.Wordlists {
		o.Input.Wordlists = append(o.Input.Wordlists, w.Path+":"+w.Keyword)
	}
	num := func(key string) int { n, _ := strconv.Atoi(set[key]); return n }
	o.General.Threads = max(num("concurrency"), 1)
	o.General.Rate = num("rateLimit")
	o.General.Quiet = true
	o.General.Noninteractive = true
	o.General.AutoCalibration = set["autocalibration"] == "true"
	o.HTTP.Timeout = max(num("timeout"), 1)
	o.HTTP.ProxyURL = set["upstream"]
	o.HTTP.FollowRedirects = set["followRedirects"] == "true"
	o.HTTP.Recursion = set["recursion"] == "true"
	o.HTTP.RecursionDepth = num("recursionDepth")

	o.Matcher.Status = set["match.status"]
	o.Matcher.Size = set["match.size"]
	o.Matcher.Words = set["match.words"]
	o.Matcher.Lines = set["match.lines"]
	o.Matcher.Regexp = set["match.regex"]
	if o.Matcher.Status == "" && o.Matcher.Size == "" && o.Matcher.Words == "" &&
		o.Matcher.Lines == "" && o.Matcher.Regexp == "" {
		o.Matcher.Status = "all" // without any matcher, ffuf reports nothing
	}
	o.Filter.Status = set["filter.status"]
	o.Filter.Size = set["filter.size"]
	o.Filter.Words = set["filter.words"]
	o.Filter.Lines = set["filter.lines"]
	o.Filter.Regexp = set["filter.regex"]
	return o
}

func (s Spec) config(dir string) (*ffuf.Config, []request.Dynamic, error) {
	h, dyn, err := s.prepare()
	if err != nil {
		return nil, nil, err
	}
	s.Wordlists = slices.Clone(s.Wordlists)
	if s.recursing() {
		// checked by prepare: the one wordlist ends the path
		h.Target = strings.TrimSuffix(h.Target, s.Wordlists[0].Keyword) + recursionKeyword
		s.Wordlists[0].Keyword = recursionKeyword
	}
	reqFile := filepath.Join(dir, "request.http")
	if err := os.WriteFile(reqFile, []byte(h.String()), 0o600); err != nil {
		return nil, nil, err
	}
	if len(s.Wordlists) == 0 {
		// ffuf needs a wordlist and its keyword in the request. An empty
		// keyword is in every request, and replacing it with the one empty
		// line of this wordlist changes nothing: the request is sent once.
		once := filepath.Join(dir, "once.txt")
		if err := os.WriteFile(once, []byte("\n"), 0o600); err != nil {
			return nil, nil, err
		}
		s.Wordlists = []request.Wordlist{{Keyword: "", Path: once}}
	}
	for i, w := range s.Wordlists {
		if w.Lines != nil {
			// list{} and range{}: ffuf reads wordlists from files
			p := filepath.Join(dir, "list"+strconv.Itoa(i+1)+".txt")
			if err := os.WriteFile(p, []byte(strings.Join(w.Lines, "\n")+"\n"), 0o600); err != nil {
				return nil, nil, err
			}
			s.Wordlists[i].Path = p
		}
	}
	opts := s.options(reqFile)
	// ffuf reads only the method of the request line: the version is ours.
	// Over TLS only, and Go falls back to HTTP/1.1 if the server has no h2.
	opts.HTTP.Http2 = h.IsHTTP2()
	ctx, cancel := context.WithCancel(context.Background())
	conf, err := ffuf.ConfigFromOptions(opts, ctx, cancel)
	if err != nil {
		cancel()
		return nil, nil, multiError(err)
	}
	mm, err := filter.FromConfig(opts, opts.Matcher.Status != "")
	if err != nil {
		cancel()
		return nil, nil, multiError(err)
	}
	conf.MatcherManager = mm
	// With an output directory, ffuf's runner keeps each raw request and
	// response; the Output of this package is what writes them.
	conf.OutputDirectory = dir
	return conf, dyn, nil
}

// multiError rewrites the text of ffuf's Multierror, "2 errors occured.\n\t*
// a\n\t* b\n", as "a; b". It wraps nothing: its errors are lost to it.
func multiError(err error) error {
	head, rest, ok := strings.Cut(err.Error(), " errors occured.\n")
	if _, nerr := strconv.Atoi(head); !ok || nerr != nil {
		return err
	}
	var parts []string
	for _, l := range strings.Split(rest, "\n") {
		if l = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(l), "* ")); l != "" {
			parts = append(parts, l)
		}
	}
	if len(parts) == 0 {
		return err
	}
	return errors.New(strings.Join(parts, "; "))
}
