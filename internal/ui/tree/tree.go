// Package tree is the collection pane: requests and folders as a tree, with
// a fuzzy filter. What is done to them is the app's business: the pane only
// sends messages.
package tree

import (
	"path"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/sectui/foam/commands"
	"github.com/sectui/foam/draw"
	"github.com/sectui/foam/highlight"
	"github.com/sectui/foam/theme"
	ftree "github.com/sectui/foam/tree"

	"github.com/sectui/steep/internal/collection"
)

type OpenMsg = ftree.OpenMsg

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

type Model struct {
	*ftree.Model[collection.Entry]
}

func New() *Model {
	t := ftree.New(func(e collection.Entry) (string, bool) { return e.Name, e.Dir })
	t.SetPlaceholder("filter requests")
	t.Empty = "no request yet: n to add one"
	t.Leaf = func(e collection.Entry, current bool) string {
		meth := e.Method
		if meth == "" {
			meth = "?"
		}
		base := draw.Clean(path.Base(e.Name))
		name := theme.Fg().Render(base)
		if current {
			name = theme.Fg().Style().Bold(true).Underline(true).Render(base)
		}
		return highlight.MethodColor(meth).Style().Bold(true).Render(draw.Pad(ansi.Truncate(meth, 6, ""), 7)) + name
	}
	t.Dot = func(e collection.Entry) (theme.Color, bool) { return highlight.PwnFoxColor(e.Color) }
	return &Model{t}
}

func send(v tea.Msg) tea.Cmd { return func() tea.Msg { return v } }

func (m *Model) Commands() []commands.Command {
	selected := func() bool { _, ok := m.Selected(); return ok }
	return append(m.Model.Commands(),
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
