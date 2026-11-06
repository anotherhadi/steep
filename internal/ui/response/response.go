// Package response is the response pane: one hit's response, colored, with
// a search and a shell pipe over it.
package response

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"slices"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/sectui/foam/commands"
	"github.com/sectui/foam/draw"
	"github.com/sectui/foam/input"
	"github.com/sectui/foam/notify"
	"github.com/sectui/foam/overlay"
	"github.com/sectui/foam/theme"
	"github.com/sectui/foam/viewer"

	"github.com/sectui/steep/internal/config"
	"github.com/sectui/steep/internal/format"
	"github.com/sectui/steep/internal/highlight"
	"github.com/sectui/steep/internal/ui/picker"
)

const (
	pipeTimeout = 3 * time.Second
	pipeLimit   = 1 << 20
)

type lineKind int

const (
	noLine lineKind = iota
	searchLine
	pipeLine
)

type pipedMsg struct {
	gen int
	out []byte
	err error
}

type Model struct {
	view   viewer.Model
	raw    []byte
	notes  []string // comment lines shown above the response
	has    bool
	pretty bool

	kind    lineKind
	line    input.Model
	editing bool

	footY int // first row of the search or pipe line's footer, -1 without

	pipes   []config.Pipe // offered by the pipe picker
	pipe    string        // the command, kept across responses
	piped   []byte
	pipeErr string
	gen     int
	// stale: the pipe's output is of the previous response, the one of this
	// response is on its way
	stale bool
}

func New(wrap, pretty bool, pipes []config.Pipe) *Model {
	m := &Model{pretty: pretty, line: input.New(""), pipes: pipes, footY: -1}
	m.view.Wrap = wrap
	return m
}

func (m *Model) SetWrap(wrap bool) {
	m.view.Wrap = wrap
	m.render()
}

func (m *Model) SetPretty(pretty bool) {
	m.pretty = pretty
	m.render()
}

// Show displays a response, as stored, under the comment lines notes.
func (m *Model) Show(raw []byte, notes []string) tea.Cmd {
	m.raw, m.notes, m.has = raw, notes, true
	m.view.Top()
	if m.pipe != "" {
		m.stale = true
		m.view.SetContent(nil, nil)
		return m.runPipe()
	}
	m.render()
	return nil
}

func (m *Model) Clear() {
	m.raw, m.notes, m.has, m.piped, m.stale = nil, nil, false, nil, false
	m.view.SetContent(nil, nil)
}

func (m *Model) Has() bool { return m.has }

// Raw is the response shown, as stored.
func (m *Model) Raw() []byte { return m.raw }

func (m *Model) Pretty() bool { return m.pretty }

// Parts is a stored response as the pane shows it: the head without its \r,
// and the body (nil without one), formatted when pretty.
func Parts(raw []byte, pretty bool) (head, body string, hasBody bool) {
	h, b := split(raw)
	if b == nil {
		return strings.ReplaceAll(string(h), "\r", ""), "", false
	}
	if pretty {
		b, _ = format.Body(b, contentType(h))
	}
	return strings.ReplaceAll(string(h), "\r", ""), strings.ReplaceAll(string(b), "\r\n", "\n"), true
}

func split(raw []byte) (head, body []byte) {
	for _, sep := range [][]byte{[]byte("\r\n\r\n"), []byte("\n\n")} {
		if i := bytes.Index(raw, sep); i >= 0 {
			return raw[:i], raw[i+len(sep):]
		}
	}
	return raw, nil
}

func (m *Model) render() {
	if !m.has {
		return
	}
	if m.pipe != "" && m.stale {
		m.view.SetContent(nil, nil)
		return
	}
	if m.pipe != "" && (m.piped != nil || m.pipeErr != "") {
		lines := strings.Split(strings.TrimRight(string(m.piped), "\n"), "\n")
		m.view.SetContent(lines, nil)
		m.search()
		return
	}
	head, body, hasBody := Parts(m.raw, m.pretty)
	lines := append(slices.Clone(m.notes), strings.Split(head, "\n")...)
	if hasBody {
		lines = append(lines, "")
		lines = append(lines, strings.Split(body, "\n")...)
	}
	for i, l := range lines {
		lines[i] = viewer.Sanitize(l)
	}
	m.view.SetContent(lines, highlight.Message(lines, highlight.Options{}))
	m.search()
}

func contentType(head []byte) string {
	for _, l := range strings.Split(string(head), "\n") {
		if name, value, ok := strings.Cut(l, ":"); ok && strings.EqualFold(strings.TrimSpace(name), "Content-Type") {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func (m *Model) search() {
	if m.kind == searchLine {
		m.view.Search(m.line.Value())
	} else {
		m.view.ClearSearch()
	}
}

func (m *Model) runPipe() tea.Cmd {
	m.gen++
	gen, cmdline, raw := m.gen, m.pipe, m.raw
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), pipeTimeout)
		defer cancel()
		c := exec.CommandContext(ctx, "sh", "-c", cmdline)
		c.Stdin = bytes.NewReader(raw)
		var out, errb limited
		out.n, errb.n = pipeLimit, 4096
		c.Stdout, c.Stderr = &out, &errb
		err := c.Run()
		switch {
		case ctx.Err() != nil:
			err = fmt.Errorf("stopped after %s", pipeTimeout)
		case err != nil && errb.Len() > 0:
			err = errors.New(strings.TrimSpace(strings.SplitN(errb.String(), "\n", 2)[0]))
		case out.cut:
			err = fmt.Errorf("output cut at %d bytes", pipeLimit)
		}
		return pipedMsg{gen, out.Bytes(), err}
	}
}

type limited struct {
	bytes.Buffer
	n   int
	cut bool
}

func (l *limited) Write(p []byte) (int, error) {
	if room := l.n - l.Len(); len(p) > room {
		l.Buffer.Write(p[:max(room, 0)])
		l.cut = true
		return len(p), nil
	}
	return l.Buffer.Write(p)
}

// Update takes the output of a pipe.
func (m *Model) Update(msg tea.Msg) bool {
	if p, ok := msg.(pipedMsg); ok {
		if p.gen == m.gen {
			m.piped, m.pipeErr, m.stale = p.out, "", false
			if p.err != nil {
				m.pipeErr = p.err.Error()
			}
			if m.piped == nil {
				m.piped = []byte{}
			}
			m.render()
		}
		return true
	}
	return false
}

func (m *Model) Capturing() bool { return m.editing }

func (m *Model) open(k lineKind) tea.Cmd {
	m.kind, m.editing = k, true
	m.line.Prompt = "/ "
	m.line.Placeholder = "search"
	value := ""
	if k == pipeLine {
		m.line.Prompt, m.line.Placeholder, value = "| ", "a shell command: rg -i cookie, jq .", oneLine(m.pipe)
		m.view.ClearSearch()
	}
	m.line.SetValue(value)
	return nil
}

func (m *Model) closeLine() {
	if m.kind == searchLine && m.pipe != "" {
		// back to the pipe the search was over
		m.kind, m.editing = pipeLine, false
		m.line.Prompt, m.line.Placeholder = "| ", ""
		m.line.SetValue(oneLine(m.pipe))
		m.render()
		return
	}
	if m.kind == pipeLine {
		m.pipe, m.piped, m.pipeErr, m.stale = "", nil, "", false
	}
	m.kind, m.editing = noLine, false
	m.line.Reset()
	m.render()
}

func (m *Model) reset() {
	m.pipe, m.piped, m.pipeErr, m.stale = "", nil, "", false
	m.kind, m.editing = noLine, false
	m.line.Reset()
	m.render()
}

// usePipe runs the response through cmdline, as if typed after |.
func (m *Model) usePipe(cmdline string) tea.Cmd {
	m.pipe = cmdline
	m.open(pipeLine)
	m.editing = false
	if m.has {
		return m.runPipe()
	}
	return nil
}

// The pipe line is one line: the newlines of a command (a YAML block in the
// config) show as ↵ in it, and are sent as newlines.
func oneLine(cmd string) string  { return strings.ReplaceAll(cmd, "\n", "↵") }
func fromLine(cmd string) string { return strings.ReplaceAll(cmd, "↵", "\n") }

// Input handles a key while the search or the pipe is being typed.
func (m *Model) Input(msg tea.KeyPressMsg) (bool, tea.Cmd) {
	switch msg.String() {
	case "enter":
		m.editing = false
		if m.line.Value() == "" {
			m.closeLine()
			return true, nil
		}
		if m.kind == pipeLine {
			m.pipe = fromLine(m.line.Value())
			if m.has {
				return true, m.runPipe()
			}
		}
		return true, nil
	case "esc", "ctrl+c":
		m.editing = false
		if m.line.Value() == "" || m.kind == pipeLine && m.pipe == "" {
			m.closeLine()
		}
		return true, nil
	}
	if !m.line.Update(msg) {
		return false, nil
	}
	if m.kind == searchLine {
		m.view.Search(m.line.Value())
	}
	return true, nil
}

func (m *Model) Paste(s string) {
	if m.editing {
		m.line.Update(tea.PasteMsg{Content: s})
		if m.kind == searchLine {
			m.view.Search(m.line.Value())
		}
	}
}

func (m *Model) openEditor() tea.Cmd {
	editor := os.Getenv("VISUAL")
	if editor == "" {
		editor = os.Getenv("EDITOR")
	}
	if editor == "" {
		return notify.Warning("set $VISUAL or $EDITOR to open responses")
	}
	f, err := os.CreateTemp("", "steep-response-*.http")
	if err != nil {
		return notify.Err(err)
	}
	_, err = f.Write(m.raw)
	f.Close()
	if err != nil {
		return notify.Err(err)
	}
	name := f.Name()
	c := exec.Command("sh", "-c", editor+` "$1"`, "sh", name)
	return tea.ExecProcess(c, func(err error) tea.Msg {
		os.Remove(name)
		if err != nil {
			return notify.Msg{Level: notify.ErrorLevel, Text: "editor: " + err.Error()}
		}
		return nil
	})
}

func (m *Model) Commands() []commands.Command {
	has := func() bool { return m.has }
	searching := func() bool { _, n := m.view.Status(); return n > 0 }
	return append(m.view.Commands(),
		commands.Command{ID: "response.search", Help: "search", When: has, Run: func() tea.Cmd { return m.open(searchLine) }},
		commands.Command{ID: "response.next", When: searching, Run: func() tea.Cmd { m.view.Next(1); return nil }},
		commands.Command{ID: "response.prev", When: searching, Run: func() tea.Cmd { m.view.Next(-1); return nil }},
		commands.Command{ID: "response.pipe", Title: "Pipe the response through a command", Help: "pipe", When: has,
			Run: func() tea.Cmd { return m.open(pipeLine) }},
		commands.Command{ID: "response.pipes", Title: "Pipe the response through a configured command", Help: "pipes",
			When: func() bool { return m.has && len(m.pipes) > 0 },
			Run: func() tea.Cmd {
				items := make([]picker.Item, len(m.pipes))
				for i, p := range m.pipes {
					items[i] = picker.Item{Name: p.Name, Value: p.Command}
				}
				return overlay.Open(picker.New("Pipes", "pipes", items, func(p picker.Item) tea.Cmd { return m.usePipe(p.Value) }))
			}},
		commands.Command{ID: "response.clear", Title: "Reset the search and the pipe", Help: "reset",
			When: func() bool { return m.kind != noLine || m.pipe != "" },
			Run:  func() tea.Cmd { m.reset(); return nil }},
		commands.Command{ID: "response.copy", Title: "Copy the response", Help: "copy", When: has,
			Run: func() tea.Cmd {
				return tea.Batch(tea.SetClipboard(string(m.raw)), notify.Success("Response copied"))
			}},
		commands.Command{ID: "response.editor", Title: "Open the response in $EDITOR", When: has, Run: m.openEditor},
		commands.Command{ID: "response.wrap", Title: "Wrap long lines or not", Help: "wrap",
			Run: func() tea.Cmd { m.SetWrap(!m.view.Wrap); return nil }},
		commands.Command{ID: "response.format", Title: "Format the body (JSON, HTML) or show it as received", Help: "format",
			Run: func() tea.Cmd { m.SetPretty(!m.pretty); return nil }},
	)
}

// Mouse focuses the search or pipe line on a click on it, leaves it on a
// click in the response, and scrolls.
func (m *Model) Mouse(ev tea.MouseMsg) {
	if _, click := ev.(tea.MouseClickMsg); click {
		switch {
		case m.footY >= 0 && ev.Mouse().Y >= m.footY:
			m.editing = true
			return
		case m.editing:
			m.Input(tea.KeyPressMsg{Code: tea.KeyEscape})
		}
	}
	m.view.Mouse(ev)
}

func (m *Model) Scroll() draw.Scroll { return m.view.Scroll() }

// Status is the search position, "3/12", or "".
func (m *Model) Status() string {
	if cur, n := m.view.Status(); n > 0 {
		return fmt.Sprintf("%d/%d", cur, n)
	}
	if m.kind == searchLine && m.line.Value() != "" {
		return "no match"
	}
	return ""
}

func (m *Model) View(w, h int, focused bool, empty string) string {
	m.footY = -1
	if !m.has {
		return draw.Hatch(w, h, empty)
	}
	var foot []string
	if m.kind != noLine {
		l := m.line
		if !m.editing {
			l.Update(tea.KeyPressMsg{Code: tea.KeyHome}) // a long command shows its start
		}
		foot = append(foot, draw.Rule(w), l.View(w, m.editing))
		if m.kind == pipeLine && m.pipeErr != "" {
			foot = append(foot, theme.Danger().Render(ansi.Truncate(m.pipeErr, w, "…")))
		}
	}
	m.view.SetSize(w, h-len(foot))
	body := draw.Fit(m.view.View(), w, h-len(foot))
	if len(foot) == 0 {
		return body
	}
	m.footY = h - len(foot)
	return body + "\n" + strings.Join(foot, "\n")
}
