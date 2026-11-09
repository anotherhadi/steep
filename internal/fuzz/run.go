package fuzz

import (
	"bytes"
	"cmp"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/ffuf/ffuf/v2/pkg/engine"
	"github.com/ffuf/ffuf/v2/pkg/ffuf"
	"github.com/ffuf/ffuf/v2/pkg/input"
	"github.com/ffuf/ffuf/v2/pkg/runner"

	"github.com/sectui/steep/internal/request"
	"github.com/sectui/steep/internal/run"
)

// Progress is ffuf's counter.
type Progress struct {
	Done, Total int
	ReqSec      int64
	Errors      int
	Started     time.Time
}

// Update is what a run produced since the previous Drain.
type Update struct {
	Hits     []run.Hit
	Messages []string
	Progress Progress
	Finished bool
	// Stopped and Found are set once the run is finished: whether it was
	// stopped before its end, and how many hits it kept.
	Stopped bool
	Found   int
	// FirstError is the first request that failed, and why: ffuf only
	// counts them.
	FirstError string
	// Unbuilt are the failures of Progress.Errors that are not the network's:
	// requests whose operations failed, never sent. FirstUnbuilt is the first.
	Unbuilt      int
	FirstUnbuilt string
}

// Run is a run in progress. ffuf calls its output from its own goroutines;
// the UI collects the results with Drain.
type Run struct {
	job   *engine.Job
	store *run.Store
	url   string
	// recursed is the keyword ffuf's FUZZ stands for, with recursion
	recursed string

	mu           sync.Mutex
	pending      Update
	progress     Progress
	finished     bool
	paused       bool
	stopped      bool
	storeFailed  bool
	firstErr     string
	unbuilt      int
	firstUnbuilt string
}

// recorder is ffuf's runner: it computes the operations over wordlists, and
// keeps the first error it meets.
type recorder struct {
	ffuf.RunnerProvider
	r       *Run
	dynamic []request.Dynamic
}

// errPrepare marks the errors of Prepare, which ffuf would report one by one.
var errPrepare = errors.New("preparing the request")

func (c *recorder) Prepare(input map[string][]byte, base *ffuf.Request) (ffuf.Request, error) {
	if len(c.dynamic) == 0 {
		return c.RunnerProvider.Prepare(input, base)
	}
	req := ffuf.CopyRequest(base)
	pairs := make([]string, 0, 2*len(c.dynamic))
	for _, d := range c.dynamic {
		v, err := d.Eval(input)
		if err != nil {
			c.r.unbuild(inputText(input) + ": " + err.Error())
			return req, fmt.Errorf("%w: %w", errPrepare, err)
		}
		pairs = append(pairs, d.Placeholder, string(v))
	}
	r := strings.NewReplacer(pairs...)
	req.Method = r.Replace(req.Method)
	req.Host = r.Replace(req.Host)
	req.Url = r.Replace(req.Url)
	req.Data = []byte(r.Replace(string(req.Data)))
	headers := make(map[string]string, len(req.Headers))
	for k, v := range req.Headers {
		headers[r.Replace(k)] = r.Replace(v)
	}
	req.Headers = headers
	return c.RunnerProvider.Prepare(input, &req)
}

// inputText names a request by its wordlist values, as "a, b".
func inputText(input map[string][]byte) string {
	keys := make([]string, 0, len(input))
	for k := range input {
		if k != "FFUFHASH" && k != "" {
			keys = append(keys, k)
		}
	}
	slices.SortFunc(keys, keywordCompare)
	values := make([]string, len(keys))
	for i, k := range keys {
		values[i] = strconv.Quote(string(input[k]))
	}
	return strings.Join(values, ", ")
}

// keywordCompare orders the keywords of operations by their number, §2§
// before §10§, and the others by name.
func keywordCompare(a, b string) int {
	na, aerr := strconv.Atoi(strings.Trim(a, "§"))
	nb, berr := strconv.Atoi(strings.Trim(b, "§"))
	if aerr == nil && berr == nil {
		return cmp.Compare(na, nb)
	}
	return strings.Compare(a, b)
}

func (r *Run) fail(text string) {
	r.mu.Lock()
	if r.firstErr == "" {
		r.firstErr = text
	}
	r.mu.Unlock()
}

func (r *Run) unbuild(text string) {
	r.mu.Lock()
	r.unbuilt++
	if r.firstUnbuilt == "" {
		r.firstUnbuilt = text
	}
	r.mu.Unlock()
}

func (c *recorder) Execute(req *ffuf.Request) (ffuf.Response, error) {
	resp, err := c.RunnerProvider.Execute(req)
	if err != nil {
		c.r.fail(req.Url + ": " + errorText(err))
	}
	return resp, err
}

// errorText keeps the end of a Go network error, the part that says why:
// "connection refused" rather than the whole dial chain.
func errorText(err error) string {
	s := err.Error()
	if i := strings.LastIndex(s, ": "); i >= 0 && len(s)-i > 6 {
		return s[i+2:]
	}
	return s
}

func Start(spec Spec, store *run.Store) (*Run, error) {
	conf, dynamic, err := spec.config(store.Dir)
	if err != nil {
		return nil, err
	}
	job := engine.NewJob(conf)
	var errs ffuf.Multierror
	job.Input, errs = input.NewInputProvider(conf)
	if err := errs.ErrorOrNil(); err != nil {
		conf.Cancel()
		return nil, multiError(err)
	}
	url := conf.Url
	for _, d := range dynamic {
		url = strings.ReplaceAll(url, d.Placeholder, d.Source)
	}
	r := &Run{job: job, store: store, url: url}
	if spec.recursing() {
		r.recursed = spec.Wordlists[0].Keyword
		r.url = strings.TrimSuffix(url, recursionKeyword) + r.recursed
	}
	job.Runner = &recorder{RunnerProvider: runner.NewSimpleRunner(conf, false), r: r, dynamic: dynamic}
	r.progress.Started = time.Now()
	job.Output = &output{r: r}
	go func() {
		job.Start()
		r.mu.Lock()
		p := r.progress
		stopped := r.stopped
		r.mu.Unlock()
		_ = store.Close(run.Meta{URL: r.url, Started: p.Started, Finished: time.Now(),
			Total: p.Total, Done: p.Done, Errors: p.Errors, Stopped: stopped})
		// finished only once run.json is written: what follows the run reads it
		r.mu.Lock()
		r.finished = true
		r.mu.Unlock()
	}()
	return r, nil
}

// Drain returns what happened since the last call.
func (r *Run) Drain() Update {
	r.mu.Lock()
	defer r.mu.Unlock()
	u := r.pending
	u.Progress = r.progress
	u.Finished = r.finished
	u.FirstError = r.firstErr
	u.Unbuilt, u.FirstUnbuilt = r.unbuilt, r.firstUnbuilt
	if r.finished {
		u.Stopped = r.stopped
		u.Found = r.store.Len()
	}
	r.pending = Update{}
	return u
}

// Progress is the latest counter, without draining anything.
func (r *Run) Progress() Progress {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.progress
}

func (r *Run) Pause() {
	r.mu.Lock()
	r.paused = true
	r.mu.Unlock()
	r.job.Pause()
}

func (r *Run) Resume() {
	r.mu.Lock()
	r.paused = false
	r.mu.Unlock()
	r.job.Resume()
}

func (r *Run) Paused() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.paused
}

func (r *Run) Stop() {
	r.mu.Lock()
	r.stopped, r.paused = true, false
	r.mu.Unlock()
	r.job.Stop()
}

func (r *Run) Dir() string { return r.store.Dir }

// URL is the target of the run, its wordlist keywords left in.
func (r *Run) URL() string { return r.url }

func (r *Run) message(s string) {
	if strings.TrimSpace(s) == "" {
		// ffuf warns with its stored error even when there is none, e.g. once
		// Stop() ends the job without having called setError
		return
	}
	r.mu.Lock()
	r.pending.Messages = append(r.pending.Messages, s)
	r.mu.Unlock()
}

func (r *Run) result(resp ffuf.Response) {
	h := run.Hit{
		Status:      int(resp.StatusCode),
		Length:      resp.ContentLength,
		Words:       resp.ContentWords,
		Lines:       resp.ContentLines,
		ContentType: resp.ContentType,
		Duration:    resp.Duration,
	}
	if resp.GetRedirectLocation(false) != "" {
		// resolved against the request only when there is a Location: ffuf
		// would make an absent one the request's own URL
		h.Redirect = resp.GetRedirectLocation(true)
	}
	var req []byte
	if resp.Request != nil {
		h.Method = resp.Request.Method
		h.URL = resp.Request.Url
		h.Position = resp.Request.Position
		h.Input = make(map[string]string, len(resp.Request.Input))
		for k, v := range resp.Request.Input {
			if k == recursionKeyword && r.recursed != "" {
				k = r.recursed
			}
			if k != "FFUFHASH" && k != "" {
				h.Input[k] = string(v)
			}
		}
		req = []byte(resp.Request.Raw)
	}
	h, err := r.store.Add(h, req, responseBytes(resp))
	r.mu.Lock()
	defer r.mu.Unlock()
	if err != nil && !r.storeFailed {
		r.storeFailed = true // once is enough: the next ones fail the same way
		r.pending.Messages = append(r.pending.Messages, "keeping the responses: "+err.Error())
	}
	r.pending.Hits = append(r.pending.Hits, h)
}

// responseBytes is the response as received, its body decoded: the head of
// ffuf's dump, then the body ffuf read.
func responseBytes(resp ffuf.Response) []byte {
	head := []byte(resp.Raw)
	if i := bytes.Index(head, []byte("\r\n\r\n")); i >= 0 {
		head = head[:i+4]
	} else {
		var b bytes.Buffer
		fmt.Fprintf(&b, "HTTP/1.1 %d %s\r\n", resp.StatusCode, http.StatusText(int(resp.StatusCode)))
		names := make([]string, 0, len(resp.Headers))
		for k := range resp.Headers {
			names = append(names, k)
		}
		sort.Strings(names)
		for _, k := range names {
			for _, v := range resp.Headers[k] {
				fmt.Fprintf(&b, "%s: %s\r\n", k, v)
			}
		}
		b.WriteString("\r\n")
		head = b.Bytes()
	}
	return append(slices.Clip(head), resp.Data...)
}

// output is ffuf's OutputProvider, forwarding to the Run.
type output struct{ r *Run }

func (o *output) Banner()         {}
func (o *output) Finalize() error { return nil }
func (o *output) Info(string)     {} // ffuf's banners: PAUSING, RESUMING
func (o *output) Error(s string) {
	// counted, and the first one kept: a message for each would flood
	if !strings.Contains(s, errPrepare.Error()) {
		o.r.message("error: " + s)
	}
}
func (o *output) Warning(s string) { o.r.message(s) }
func (o *output) Raw(string)       {}
func (o *output) Result(resp ffuf.Response) {
	o.r.result(resp)
}
func (o *output) PrintResult(ffuf.Result)                     {}
func (o *output) SaveFile(string, string) error               { return nil }
func (o *output) GetCurrentResults() []ffuf.Result            { return nil }
func (o *output) SetCurrentResults([]ffuf.Result)             {}
func (o *output) FilterCurrentResults(func(ffuf.Result) bool) {}
func (o *output) SetPaused(bool)                              {}
func (o *output) PendingResults() int                         { return 0 }
func (o *output) Reset()                                      {}
func (o *output) Cycle()                                      {}
func (o *output) Progress(p ffuf.Progress) {
	o.r.mu.Lock()
	o.r.progress = Progress{Done: p.ReqCount, Total: p.ReqTotal, ReqSec: p.ReqSec, Errors: p.ErrorCount, Started: p.StartedAt}
	o.r.mu.Unlock()
}
