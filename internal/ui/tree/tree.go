// Package tree is the collection pane: requests and folders as a tree, with
// a fuzzy filter. What is done to them is the app's business: the pane only
// sends messages.
package tree

import (
	"path"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/sectui/foam/commands"
	"github.com/sectui/foam/draw"
	"github.com/sectui/foam/fuzzy"
	"github.com/sectui/foam/input"
	"github.com/sectui/foam/list"
	"github.com/sectui/foam/theme"

	"github.com/sectui/steep/internal/collection"
	"github.com/sectui/steep/internal/highlight"
)

type OpenMsg struct{ Name string }

// NewMsg asks for a new request (or folder) in Dir.
type NewMsg struct {
	Dir    string
	Folder bool
}

type RenameMsg struct {
	Name string
	Dir  bool
}

type DuplicateMsg struct{ Name string }

type DeleteMsg struct {
	Name string
	Dir  bool
}

type row struct {
	collection.Entry
	depth int
}

type Model struct {
	entries  []collection.Entry
	expanded map[string]bool
	rows     []row
	list     list.Model

	filter    input.Model
	filtering bool // typing in the filter
	current   string
	colored   bool // a request has a PwnFox color
}

func New() *Model {
	m := &Model{expanded: map[string]bool{}, filter: input.New("/ ")}
	m.filter.Placeholder = "filter requests"
	return m
}

// SetEntries replaces the tree, keeping the selection on the same entry.
func (m *Model) SetEntries(es []collection.Entry) {
	sel, _ := m.Selected()
	m.entries = es
	m.colored = false
	for _, e := range es {
		if _, ok := highlight.PwnFoxColor(e.Color); ok && !e.Dir {
			m.colored = true
		}
	}
	m.rebuild(sel.Name)
}

// SetCurrent marks the open request, and reveals it.
func (m *Model) SetCurrent(name string) {
	m.current = name
	for d := collection.Parent(name); d != ""; d = collection.Parent(d) {
		m.expanded[d] = true
	}
	m.rebuild(name)
}

func (m *Model) Selected() (collection.Entry, bool) {
	if len(m.rows) == 0 {
		return collection.Entry{}, false
	}
	return m.rows[m.list.Selected()].Entry, true
}

// Dir is the folder new entries go in: the selected folder, or the folder of
// the selected request.
func (m *Model) Dir() string {
	e, ok := m.Selected()
	switch {
	case !ok:
		return ""
	case e.Dir:
		return e.Name
	}
	return collection.Parent(e.Name)
}

func (m *Model) rebuild(keep string) {
	m.rows = m.rows[:0]
	q := m.filter.Value()
	if q == "" {
		for _, e := range m.entries {
			if m.visible(e.Name) {
				m.rows = append(m.rows, row{e, strings.Count(e.Name, "/")})
			}
		}
	} else {
		keepDirs := map[string]bool{}
		for _, e := range m.entries {
			if _, _, ok := fuzzy.Match(q, e.Name); ok && !e.Dir {
				for d := collection.Parent(e.Name); d != ""; d = collection.Parent(d) {
					keepDirs[d] = true
				}
				keepDirs[e.Name] = true
			}
		}
		for _, e := range m.entries {
			if keepDirs[e.Name] {
				m.rows = append(m.rows, row{e, strings.Count(e.Name, "/")})
			}
		}
	}
	m.list.SetLen(len(m.rows))
	for i, r := range m.rows {
		if r.Name == keep {
			m.list.Select(i)
			break
		}
	}
}

func (m *Model) visible(name string) bool {
	for d := collection.Parent(name); d != ""; d = collection.Parent(d) {
		if !m.expanded[d] {
			return false
		}
	}
	return true
}

func (m *Model) Capturing() bool { return m.filtering }

// Input handles a key while the filter is being typed.
func (m *Model) Input(msg tea.KeyPressMsg) bool {
	switch msg.String() {
	case "enter", "esc", "ctrl+c":
		m.filtering = false
	case "up", "ctrl+k", "ctrl+p":
		m.list.Move(-1)
	case "down", "ctrl+j", "ctrl+n":
		m.list.Move(1)
	default:
		if !m.filter.Update(msg) {
			return false
		}
		m.refilter()
	}
	return true
}

func (m *Model) Paste(s string) {
	if m.filtering {
		m.filter.Update(tea.PasteMsg{Content: s})
		m.refilter()
	}
}

// refilter applies the filter typed, keeping the selected request when it
// still matches, the first request otherwise.
func (m *Model) refilter() {
	keep := ""
	if e, ok := m.Selected(); ok && !e.Dir {
		keep = e.Name
	}
	m.rebuild(keep)
	if e, ok := m.Selected(); !ok || e.Name != keep {
		m.list.Select(m.firstRequest())
	}
}

func (m *Model) firstRequest() int {
	for i, r := range m.rows {
		if !r.Dir {
			return i
		}
	}
	return 0
}

func (m *Model) setExpanded(open bool) tea.Cmd {
	e, ok := m.Selected()
	if !ok {
		return nil
	}
	if !e.Dir {
		if open {
			return nil
		}
		// h on a request goes to its folder
		if d := collection.Parent(e.Name); d != "" {
			m.expanded[d] = false
			m.rebuild(d)
		}
		return nil
	}
	m.expanded[e.Name] = open
	m.rebuild(e.Name)
	return nil
}

func (m *Model) setAll(open bool) tea.Cmd {
	sel, _ := m.Selected()
	for _, e := range m.entries {
		if e.Dir {
			m.expanded[e.Name] = open
		}
	}
	keep := sel.Name
	if !open {
		for keep != "" && !m.visible(keep) {
			keep = collection.Parent(keep)
		}
	}
	m.rebuild(keep)
	return nil
}

func (m *Model) open() tea.Cmd {
	e, ok := m.Selected()
	if !ok {
		return nil
	}
	if e.Dir {
		m.expanded[e.Name] = !m.expanded[e.Name]
		m.rebuild(e.Name)
		return nil
	}
	return func() tea.Msg { return OpenMsg{e.Name} }
}

func send(v tea.Msg) tea.Cmd { return func() tea.Msg { return v } }

func (m *Model) Commands() []commands.Command {
	selected := func() bool { _, ok := m.Selected(); return ok }
	return append(m.list.Commands(),
		commands.Command{ID: "tree.open", Help: "open", Run: m.open},
		commands.Command{ID: "tree.expand", Run: func() tea.Cmd { return m.setExpanded(true) }},
		commands.Command{ID: "tree.collapse", Run: func() tea.Cmd { return m.setExpanded(false) }},
		commands.Command{ID: "tree.expand-all", Title: "Expand every folder", Run: func() tea.Cmd { return m.setAll(true) }},
		commands.Command{ID: "tree.collapse-all", Title: "Collapse every folder", Run: func() tea.Cmd { return m.setAll(false) }},
		commands.Command{ID: "tree.filter", Help: "filter", Run: func() tea.Cmd { m.filtering = true; return nil }},
		commands.Command{ID: "tree.clear-filter", When: func() bool { return m.filter.Value() != "" },
			Run: func() tea.Cmd {
				sel, _ := m.Selected()
				m.filter.Reset()
				m.rebuild(sel.Name)
				return nil
			}},
		commands.Command{ID: "tree.new", Title: "New request", Help: "new",
			Run: func() tea.Cmd { return send(NewMsg{Dir: m.Dir()}) }},
		commands.Command{ID: "tree.new-folder", Title: "New folder",
			Run: func() tea.Cmd { return send(NewMsg{Dir: m.Dir(), Folder: true}) }},
		commands.Command{ID: "tree.rename", Title: "Rename the selected entry", When: selected,
			Run: func() tea.Cmd { e, _ := m.Selected(); return send(RenameMsg{e.Name, e.Dir}) }},
		commands.Command{ID: "tree.duplicate", Title: "Duplicate the selected request", Help: "duplicate",
			When: func() bool { e, ok := m.Selected(); return ok && !e.Dir },
			Run:  func() tea.Cmd { e, _ := m.Selected(); return send(DuplicateMsg{e.Name}) }},
		commands.Command{ID: "tree.delete", Title: "Delete the selected entry", When: selected,
			Run: func() tea.Cmd { e, _ := m.Selected(); return send(DeleteMsg{e.Name, e.Dir}) }},
	)
}

// Mouse handles an event in the pane. A click while the pane is not focused
// only moves the cursor there; otherwise it opens a request, or a folder
// clicked twice.
func (m *Model) Mouse(ev tea.MouseMsg, focused bool) tea.Cmd {
	top := 0
	if m.showFilter() {
		top = 2
	}
	mm := ev.Mouse()
	mm.Y -= top
	if _, ok := ev.(tea.MouseClickMsg); ok {
		before := m.list.Selected()
		if i := m.list.Mouse(tea.MouseClickMsg(mm)); i >= 0 && focused && (i == before || !m.rows[i].Dir) {
			return m.open()
		}
		return nil
	}
	if w, ok := ev.(tea.MouseWheelMsg); ok {
		m.list.Mouse(w)
	}
	return nil
}

func (m *Model) showFilter() bool { return m.filtering || m.filter.Value() != "" }

func (m *Model) Scroll() draw.Scroll { return m.list.Scroll() }

func (m *Model) View(w, h int, focused bool) string {
	var lines []string
	if m.showFilter() {
		f := m.filter
		if !m.filtering {
			f.Update(tea.KeyPressMsg{Code: tea.KeyHome})
		}
		lines = append(lines, f.View(w, m.filtering), draw.Rule(w))
		h -= 2
	}
	m.list.SetHeight(max(h, 0))
	if len(m.rows) == 0 {
		msg := "no request yet: n to add one"
		if m.filter.Value() != "" {
			msg = "nothing matches"
		}
		return strings.Join(append(lines, theme.Subtle().Render(" "+ansi.Truncate(msg, w-1, "…"))), "\n")
	}
	// a column for the PwnFox colors, when a request has one
	lead := " "
	if m.colored {
		lead = "   "
	}
	start, end := m.list.Visible()
	for i := start; i < end; i++ {
		r := m.rows[i]
		base := draw.Clean(path.Base(r.Name))
		indent := strings.Repeat("  ", r.depth)
		var s string
		if r.Dir {
			icon := theme.Icon("\uf07b ", "▸ ")
			if m.expanded[r.Name] || m.filter.Value() != "" {
				icon = theme.Icon("\uf07c ", "▾ ")
			}
			s = indent + theme.Accent().Render(icon+base)
		} else {
			meth := r.Method
			if meth == "" {
				meth = "?"
			}
			name := theme.Fg().Render(base)
			if r.Name == m.current {
				name = theme.Fg().Style().Bold(true).Underline(true).Render(base)
			}
			s = indent + highlight.MethodColor(meth).Style().Bold(true).Render(draw.Pad(ansi.Truncate(meth, 6, ""), 7)) + name
		}
		s = ansi.Truncate(lead+s, w, "…")
		sel := i == m.list.Selected()
		if sel {
			s = draw.Selected(s, w, focused)
		}
		if c, ok := highlight.PwnFoxColor(r.Color); ok && !r.Dir && w >= 3 {
			// drawn over the selection, which takes the colors away
			st := c.Style()
			if sel {
				bg := theme.Selection()
				if !focused {
					bg = theme.Surface()
				}
				st = st.Background(bg.Value())
			}
			s = ansi.Cut(s, 0, 1) + st.Render(highlight.PwnFoxIcon()) + ansi.Cut(s, 2, w)
		}
		lines = append(lines, s)
	}
	return strings.Join(lines, "\n")
}
