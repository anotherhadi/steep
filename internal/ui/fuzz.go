package ui

import (
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/anotherhadi/foam/confirm"
	"github.com/anotherhadi/foam/notify"
	"github.com/anotherhadi/foam/overlay"

	"github.com/anotherhadi/steep/internal/fuzz"
	"github.com/anotherhadi/steep/internal/request"
	"github.com/anotherhadi/steep/internal/run"
	"github.com/anotherhadi/steep/internal/wordlist"
)

const tickEvery = 100 * time.Millisecond

type tickMsg struct{}

func tick() tea.Cmd { return tea.Tick(tickEvery, func(time.Time) tea.Msg { return tickMsg{} }) }

func (m *Model) spec(text string) (fuzz.Spec, error) {
	f := request.Parse(text)
	s := fuzz.Spec{
		Request:     f.Body,
		HeaderLines: len(f.Header),
		Settings:    f.Effective(m.cfg.Request.Values()),
		Headers:     m.cfg.HTTP.Headers,

		UserAgent:       m.cfg.HTTP.UserAgent,
		UserAgentSuffix: m.cfg.HTTP.UserAgentSuffix,
		KeepPwnFox:      !m.cfg.HTTP.StripPwnFox,
	}
	if errs := f.Problems(); len(errs) > 0 {
		return s, errs[0]
	}
	body, err := f.ParseBody()
	if err != nil {
		return s, err
	}
	for _, w := range body.Wordlists() {
		if w.Lines == nil {
			p, err := wordlist.Resolve(w.Path, m.cfg.Steep.Shortcuts, m.cwd)
			if err != nil {
				return s, err
			}
			w.Path = p
		}
		s.Wordlists = append(s.Wordlists, w)
	}
	return s, nil
}

type countMsg struct {
	gen   int
	n     int
	exact bool
	err   error
	rate  int
}

// recount counts the requests of the open request, in the background.
func (m *Model) recount() tea.Cmd {
	m.countGen++
	gen := m.countGen
	if m.editor.Name() == "" {
		m.count = countMsg{gen: gen}
		return nil
	}
	spec, err := m.spec(m.editor.Text())
	rate, _ := strconv.Atoi(spec.Settings["rateLimit"])
	return func() tea.Msg {
		if err != nil {
			return countMsg{gen: gen, err: err}
		}
		if _, err := spec.Prepare(); err != nil {
			return countMsg{gen: gen, err: err}
		}
		n, exact, err := spec.Count()
		return countMsg{gen, n, exact, err, rate}
	}
}

// countText is the request pane's footer: "2 048 req · ~3 min", or what
// stops the request from running; nothing for a single request.
func (m *Model) countText() string {
	c := m.count
	switch {
	case m.editor.Name() == "":
		return ""
	case c.err != nil:
		return "⚠ " + c.err.Error()
	case c.n == 1 && c.exact:
		return ""
	}
	s := group(c.n) + " req"
	if !c.exact {
		s += "+"
	}
	if r := c.rate; r > 0 && c.n > 1 {
		s += " · ~" + human(time.Duration(c.n/r)*time.Second)
	}
	return s
}

func group(n int) string {
	s := fmt.Sprint(n)
	var b strings.Builder
	for i, r := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			b.WriteRune(' ')
		}
		b.WriteRune(r)
	}
	return b.String()
}

func human(d time.Duration) string {
	switch {
	case d >= time.Hour:
		return fmt.Sprintf("%.1f h", d.Hours())
	case d >= time.Minute:
		return fmt.Sprintf("%.0f min", d.Minutes())
	}
	return fmt.Sprintf("%.0f s", d.Seconds())
}

type runMsg struct{}

// startRun runs the open request, asking first above the confirm threshold.
func (m *Model) startRun() tea.Cmd {
	spec, err := m.spec(m.editor.Text())
	if err == nil {
		_, err = spec.Prepare()
	}
	if err != nil {
		return notify.Err(err)
	}
	n, _, err := spec.Count()
	if err != nil {
		return notify.Err(err)
	}
	if limit := m.cfg.Steep.ConfirmAbove; limit > 0 && n > limit {
		return overlay.Open(confirm.New("Run", fmt.Sprintf("Send %s requests?", group(n)),
			func() tea.Msg { return runMsg{} }))
	}
	return m.launch()
}

func (m *Model) launch() tea.Cmd {
	if m.fz != nil {
		return notify.Warning("a run is already going")
	}
	f := request.Parse(m.editor.Text())
	switch {
	case f.ID() == "":
		f.EnsureID()
		m.editor.SetText(f.String())
	case m.col.IDTaken(f.ID(), m.editor.Name()):
		// a copy of another request: its runs must not mix with the original's
		f.NewID()
		m.editor.SetText(f.String())
	}
	spec, err := m.spec(f.String())
	if err != nil {
		return notify.Err(err)
	}
	store, err := run.Create(m.col.RunDir(f.ID()))
	if err != nil {
		return notify.Err(err)
	}
	r, err := fuzz.Start(spec, store)
	if err != nil {
		_ = store.Close(run.Meta{})
		return notify.Err(err)
	}
	m.fz, m.runName = r, m.editor.Name()
	m.response.Clear()
	m.shownHit = ""
	cmd := m.results.SetRun(r.Dir(), nil)
	m.results.SetProgress(fuzz.Progress{}, true, false)
	return tea.Batch(cmd, tick())
}

func (m *Model) tick() tea.Cmd {
	if m.fz == nil {
		return nil
	}
	u := m.fz.Drain()
	here := m.runName == m.editor.Name()
	var cmds []tea.Cmd
	if here {
		m.results.Add(u.Hits)
		m.results.SetProgress(u.Progress, !u.Finished, m.fz.Paused())
	}
	for _, s := range u.Messages {
		cmds = append(cmds, notify.Info(strings.TrimSpace(s)))
	}
	if !u.Finished {
		return tea.Batch(append(cmds, tick())...)
	}
	fz := m.fz
	m.fz = nil
	cmds = append(cmds, m.runEnd(fz, u))
	if !u.Stopped {
		// stopping cancels the requests in flight: their failures are not
		// the target's, and not worth a warning
		if e := u.Unbuilt; e > 0 {
			// the request's blocks, not the network: worth telling apart
			text := fmt.Sprintf("%d of %d requests could not be built", e, u.Progress.Done)
			if u.Progress.Done <= 1 {
				text = "the request could not be built"
			}
			cmds = append(cmds, notify.Warning(text+": "+u.FirstUnbuilt))
		}
		if e := u.Progress.Errors - u.Unbuilt; e > 0 {
			text := fmt.Sprintf("%d of %d requests failed", e, u.Progress.Done)
			if u.Progress.Done <= 1 {
				text = "the request failed"
			}
			if u.FirstError != "" {
				text += ": " + u.FirstError
			}
			cmds = append(cmds, notify.Warning(text))
		}
	}
	return tea.Batch(cmds...)
}

type runEndMsg struct {
	err error
	out string
}

// runEnd runs onRunEnd, in the background.
func (m *Model) runEnd(fz *fuzz.Run, u fuzz.Update) tea.Cmd {
	cmdline := m.cfg.Steep.OnRunEnd
	if cmdline == "" {
		return nil
	}
	status := "finished"
	if u.Stopped {
		status = "stopped"
	}
	c := exec.Command("sh", "-c", cmdline)
	c.Dir = m.cwd
	c.Env = append(os.Environ(),
		"RUN_REQUEST="+m.runName,
		"RUN_STATUS="+status,
		"RUN_HITS="+strconv.Itoa(u.Found),
		"RUN_DONE="+strconv.Itoa(u.Progress.Done),
		"RUN_TOTAL="+strconv.Itoa(u.Progress.Total),
		"RUN_ERRORS="+strconv.Itoa(u.Progress.Errors),
		"RUN_URL="+fz.URL(),
		"RUN_DIR="+fz.Dir(),
	)
	return func() tea.Msg {
		var out tail
		c.Stdout, c.Stderr = &out, &out
		err := c.Run()
		return runEndMsg{err, string(out.b)}
	}
}

// tail keeps the end of what is written to it: the last line of a command's
// output is all that is shown of it.
type tail struct{ b []byte }

const tailSize = 4096

func (t *tail) Write(p []byte) (int, error) {
	t.b = append(t.b, p...)
	if len(t.b) > 2*tailSize {
		t.b = append(t.b[:0], t.b[len(t.b)-tailSize:]...)
	}
	return len(p), nil
}

func (m *Model) ranEnd(msg runEndMsg) tea.Cmd {
	if msg.err == nil {
		return nil
	}
	text := "onRunEnd: " + msg.err.Error()
	if out := strings.TrimSpace(msg.out); out != "" {
		lines := strings.Split(out, "\n")
		text += ": " + lines[len(lines)-1]
	}
	return notify.Warning(text)
}
