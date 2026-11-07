// Package editor is the request pane: the open request file in a vim
// editor, colored as it is typed.
package editor

import (
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/sectui/foam/commands"
	"github.com/sectui/foam/draw"
	feditor "github.com/sectui/foam/editor"
	"github.com/sectui/foam/highlight"
	"github.com/sectui/foam/overlay"
	"github.com/sectui/foam/vim"
	"github.com/sectui/operations"
	"github.com/sectui/operations/template"

	"github.com/sectui/steep/internal/request"
	"github.com/sectui/steep/internal/ui/oppicker"
)

// PickMsg asks to pick a wordlist for the file{} block at Pos, or for a new
// one there.
type PickMsg struct{ Pos vim.Pos }

type Model struct {
	ed   *feditor.Model
	name string
	w, h int
}

func New() *Model { return &Model{} }

// Open shows a request; its undo history starts there.
func (m *Model) Open(name, text string) {
	m.name = name
	m.ed = feditor.New(text)
	var cache highlight.Cache
	m.ed.Style = func(lines []string) []string { return cache.Message(lines, Highlight) }
	m.ed.SetSize(m.w, m.h)
}

func (m *Model) Close() { m.name, m.ed = "", nil }

// Name is the open request, "" when none is.
func (m *Model) Name() string { return m.name }

func (m *Model) Rename(name string) { m.name = name }

func (m *Model) Text() string {
	if m.ed == nil {
		return ""
	}
	return m.ed.Text()
}

// SetText replaces the text, as one undoable change.
func (m *Model) SetText(text string) {
	if m.ed != nil {
		m.ed.SetText(text)
	}
}

func (m *Model) SetSize(w, h int) {
	m.w, m.h = w, h
	if m.ed != nil {
		m.ed.SetSize(w, h)
	}
}

// Busy reports whether keys belong to vim before anyone else: in insert or
// visual mode, or with a command half typed.
func (m *Model) Busy() bool {
	return m.ed != nil && (m.ed.Mode() != vim.Normal || m.ed.Pending() != "")
}

// Key gives a key to vim; it reports whether vim used it.
func (m *Model) Key(msg tea.KeyPressMsg) bool {
	if m.ed == nil {
		return false
	}
	return m.ed.Key(msg)
}

func (m *Model) Paste(s string) {
	if m.ed != nil {
		m.ed.Paste(s)
	}
}

// Changed reports whether the text changed since the last call.
func (m *Model) Changed() bool { return m.ed != nil && m.ed.Changed() }

// Yanked returns what vim last yanked, once.
func (m *Model) Yanked() (string, bool) {
	if m.ed == nil {
		return "", false
	}
	return m.ed.TakeYank()
}

func (m *Model) Mouse(ev tea.MouseMsg) {
	if m.ed != nil {
		m.ed.Mouse(ev)
	}
}

func (m *Model) Scroll() draw.Scroll {
	if m.ed == nil {
		return draw.Scroll{}
	}
	return m.ed.Scroll()
}

func (m *Model) Title() string {
	if m.ed == nil {
		return "Request"
	}
	t := draw.Clean(m.name) + " · " + m.ed.Mode().String()
	if p := m.ed.Pending(); p != "" {
		t += " " + p
	}
	return t
}

// CursorPos is the cursor's place in the text.
func (m *Model) CursorPos() vim.Pos {
	if m.ed == nil {
		return vim.Pos{}
	}
	return m.ed.Cursor()
}

func (m *Model) CursorPosition() (x, y int, ok bool) {
	if m.ed == nil {
		return 0, 0, false
	}
	return m.ed.CursorPosition()
}

// lines of the request: its request line, the first header, and the first
// line of the body.
func (m *Model) landmarks() (start, headers, body int) {
	start, headers, body = -1, -1, -1
	lines := m.ed.Lines()
	i := 0
	for i < len(lines) && strings.HasPrefix(lines[i], "#") {
		i++
	}
	for i < len(lines) && strings.TrimSpace(lines[i]) == "" {
		i++
	}
	if i < len(lines) {
		start = i
	}
	if i+1 < len(lines) {
		headers = i + 1
	}
	for j := i + 1; j < len(lines); j++ {
		if strings.TrimSpace(lines[j]) == "" {
			body = min(j+1, len(lines)-1)
			break
		}
	}
	return start, headers, body
}

// pathCol is where the target starts in a request line, in runes: after the
// method and the spaces following it, or the line's end.
func pathCol(line string) int {
	r := []rune(line)
	i := 0
	for i < len(r) && r[i] != ' ' && r[i] != '\t' {
		i++
	}
	for i < len(r) && (r[i] == ' ' || r[i] == '\t') {
		i++
	}
	return min(i, max(len(r)-1, 0))
}

// BlockAt returns the path typed in the file{} block at p.
func (m *Model) BlockAt(p vim.Pos) (string, bool) {
	if m.ed == nil || p.Line >= m.ed.LineCount() {
		return "", false
	}
	line := []rune(m.ed.Line(p.Line))
	at := len(string(line[:min(p.Col, len(line))]))
	for _, b := range request.Blocks(string(line)) {
		if at >= b.Start && at < b.End {
			return b.Path, true
		}
	}
	return "", false
}

// SetBlock puts path in the file{} block at p, or inserts a block there.
func (m *Model) SetBlock(p vim.Pos, path string) {
	if m.ed == nil || p.Line >= m.ed.LineCount() {
		return
	}
	line := []rune(m.ed.Line(p.Line))
	col := min(p.Col, len(line))
	at := len(string(line[:col]))
	s := string(line)
	text := "file{" + request.EscapePath(path) + "}"
	for _, b := range request.Blocks(s) {
		if at >= b.Start && at < b.End {
			s = s[:b.Start] + text + s[b.End:]
			text = ""
			break
		}
	}
	if text != "" {
		s = s[:at] + text + s[at:]
	}
	lines := m.ed.Lines()
	lines[p.Line] = s
	m.ed.SetText(strings.Join(lines, "\n"))
}

// target is where an operation block goes: around the visual selection, or
// at the cursor.
type target struct {
	a, b     vim.Pos
	selected bool
	version  uint64
}

func (m *Model) target() target {
	if a, b, ok := m.ed.Selection(); ok {
		return target{a, b, true, m.ed.Version()}
	}
	p := m.ed.Cursor()
	return target{p, p, false, m.ed.Version()}
}

// putOperation wraps the target in an id{} block, or opens an empty one at
// the cursor, in insert mode.
func (m *Model) putOperation(t target, id string) {
	if m.ed == nil {
		return
	}
	if t.version != m.ed.Version() {
		// the text changed under the picker (reloaded from disk)
		t = m.target()
	}
	if !t.selected {
		m.ed.ReplaceInsert(t.a, t.a, id+"{", "}")
		return
	}
	m.ed.Replace(t.a, t.b, Wrap(id, textRange(m.ed.Lines(), t.a, t.b)))
}

// textRange is the text in [a, b), columns in runes.
func textRange(lines []string, a, b vim.Pos) string {
	if a.Line >= len(lines) {
		return ""
	}
	b.Line = min(b.Line, len(lines)-1)
	col := func(l, c int) int { return len(string([]rune(lines[l])[:min(c, len([]rune(lines[l])))])) }
	if a.Line == b.Line {
		return lines[a.Line][col(a.Line, a.Col):col(b.Line, max(b.Col, a.Col))]
	}
	parts := []string{lines[a.Line][col(a.Line, a.Col):]}
	parts = append(parts, lines[a.Line+1:b.Line]...)
	parts = append(parts, lines[b.Line][:col(b.Line, b.Col)])
	return strings.Join(parts, "\n")
}

var escapeAll = strings.NewReplacer(`\`, `\\`, "{", `\{`, "}", `\}`)

// Wrap puts s in an id{} block that gives back what s means in a request:
// its blocks stay blocks, and the rest is escaped where a block's content
// would read it otherwise.
func Wrap(id, s string) string {
	nodes, err := request.Engine.Parse(s)
	if err != nil {
		// a block left open or invalid in s: all of it is text
		return id + "{" + escapeAll.Replace(s) + "}"
	}
	var b strings.Builder
	b.WriteString(id + "{")
	for _, n := range nodes {
		switch n := n.(type) {
		case *template.Text:
			b.WriteString(escapeAll.Replace(n.Value))
		case *template.Block:
			b.WriteString(s[n.Pos.Start:n.Pos.End])
		}
	}
	b.WriteString("}")
	return b.String()
}

func (m *Model) Commands() []commands.Command {
	open := func() bool { return m.ed != nil }
	return []commands.Command{
		{ID: "editor.operation", Title: "Put an operation block", Help: "operation", When: open,
			Run: func() tea.Cmd {
				t := m.target()
				title := "Insert an operation"
				if t.selected {
					title = "Wrap in an operation"
				}
				return overlay.Open(oppicker.New(title, func(op *operations.Operation) tea.Cmd {
					m.putOperation(t, op.ID)
					return nil
				}))
			}},
		{ID: "editor.pick-file", Title: "Pick a wordlist", Help: "wordlist", When: open,
			Run: func() tea.Cmd {
				p := m.ed.Cursor()
				return func() tea.Msg { return PickMsg{Pos: p} }
			}},
		{ID: "editor.path", Title: "Go to the path", When: open, Run: func() tea.Cmd {
			if l, _, _ := m.landmarks(); l >= 0 {
				m.ed.GotoLine(l)
				m.ed.SetCursor(vim.Pos{Line: l, Col: pathCol(m.ed.Line(l))})
			}
			return nil
		}},
		{ID: "editor.headers", Title: "Go to the headers", When: open, Run: func() tea.Cmd {
			if _, h, _ := m.landmarks(); h >= 0 {
				m.ed.GotoLine(h)
			}
			return nil
		}},
		{ID: "editor.body", Title: "Go to the body", When: open, Run: func() tea.Cmd {
			if _, _, b := m.landmarks(); b >= 0 {
				m.ed.GotoLine(b)
			}
			return nil
		}},
	}
}

func (m *Model) View(w, h int, focused bool) string {
	if m.ed == nil {
		return draw.Hatch(w, h, "n: new request · or paste a curl command")
	}
	return m.ed.View(focused)
}

// ShotView is the view for a screenshot: no cursor, the selection kept.
func (m *Model) ShotView(w, h int) string {
	if m.ed == nil {
		return m.View(w, h, false)
	}
	m.ed.HideCursor = true
	defer func() { m.ed.HideCursor = false }()
	return m.ed.View(false)
}
