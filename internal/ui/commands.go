package ui

import (
	tea "charm.land/bubbletea/v2"

	"github.com/anotherhadi/foam/commands"
	"github.com/anotherhadi/foam/form"
	"github.com/anotherhadi/foam/geom"
	"github.com/anotherhadi/foam/help"
	"github.com/anotherhadi/foam/overlay"
	"github.com/anotherhadi/foam/palette"

	"github.com/anotherhadi/steep/internal/ui/copypicker"
	"github.com/anotherhadi/steep/internal/ui/oppicker"
	"github.com/anotherhadi/steep/internal/ui/picker"
	"github.com/anotherhadi/steep/internal/ui/settings"
)

func (m *Model) global() []commands.Command {
	running := func() bool { return m.fz != nil }
	open := func() bool { return m.editor.Name() != "" }
	focus := func(p pane) func() tea.Cmd {
		return func() tea.Cmd { m.setFocus(p); return nil }
	}
	nav := func(d geom.Dir) func() tea.Cmd {
		return func() tea.Cmd { m.navigate(d); return nil }
	}
	return []commands.Command{
		{ID: "app.quit", Title: "Quit", Run: func() tea.Cmd { return tea.Quit }},
		{ID: "app.help", Title: "Show the shortcuts", Help: "help", Run: func() tea.Cmd {
			return overlay.Open(help.New(m.reg.Groups(m.path())))
		}},
		{ID: "app.palette", Title: "Command palette", Run: func() tea.Cmd {
			return overlay.Open(palette.New(m.reg.Active(m.path()), "app.palette"))
		}},
		{ID: "focus.collection", Title: "Go to the collection", Run: focus(paneCollection)},
		{ID: "focus.editor", Title: "Go to the request", Run: focus(paneEditor)},
		{ID: "focus.results", Title: "Go to the results", When: func() bool { return m.results.Visible() }, Run: focus(paneResults)},
		{ID: "focus.response", Title: "Go to the response", Run: focus(paneResponse)},
		{ID: "pane.left", Title: "Go to the pane on the left", Run: nav(geom.Left)},
		{ID: "pane.down", Title: "Go to the pane below", Run: nav(geom.Down)},
		{ID: "pane.up", Title: "Go to the pane above", Run: nav(geom.Up)},
		{ID: "pane.right", Title: "Go to the pane on the right", Run: nav(geom.Right)},
		{ID: "pane.zoom", Title: "Zoom the pane in or out", Run: func() tea.Cmd {
			m.zoomed = !m.zoomed
			m.layout()
			return nil
		}},
		{ID: "collection.copy-path", Title: "Copy the path of the collection", Run: m.copyCollectionPath},
		{ID: "collection.toggle", Title: "Show or hide the collection", Run: func() tea.Cmd {
			if m.w < collectionHideBelow {
				// too narrow to keep it: show it for a while
				m.peek = !m.peek
			} else {
				m.showCollection, m.peek = !m.showCollection, false
			}
			if m.collectionShown() {
				m.focus = paneCollection
			} else if m.focus == paneCollection {
				m.focus = paneEditor
			}
			m.layout()
			return nil
		}},
		{ID: "run.start", Title: "Run the request", Help: "run", When: func() bool { return open() && !running() }, Run: m.startRun},
		{ID: "run.stop", Title: "Stop the run", Help: "stop", When: running, Run: func() tea.Cmd {
			m.fz.Stop()
			return nil
		}},
		{ID: "run.pause", Title: "Pause or resume the run", Help: "pause", When: running, Run: func() tea.Cmd {
			if m.fz.Paused() {
				m.fz.Resume()
			} else {
				m.fz.Pause()
			}
			return nil
		}},
		{ID: "screenshot.take", Title: "Screenshot the request and its response", When: open, Run: m.screenshot},
		{ID: "settings.open", Title: "Settings", When: open, Run: func() tea.Cmd {
			return overlay.Open(form.New("Settings · "+m.editor.Name(), settings.Request{
				Text:     m.editor.Text,
				SetText:  m.editor.SetText,
				Defaults: m.cfg.Request.Values(),
			}))
		}},
	}
}

// hitCommands copy the selected hit, or the hits shown, from the results or
// the response.
func (m *Model) hitCommands() []commands.Command {
	selected := func() bool { _, ok := m.results.Selected(); return ok }
	return []commands.Command{
		{ID: "export.hit", Title: "Copy the hit (request, response, URL, input)", Help: "copy", When: selected, Run: func() tea.Cmd { return m.askCopy(false) }},
		{ID: "export.all", Title: "Copy the hits shown (requests, responses, URLs, inputs)", Help: "copy all",
			When: func() bool { return len(m.results.Shown()) > 1 }, Run: func() tea.Cmd { return m.askCopy(true) }},
		{ID: "export.path", Title: "Copy the path of the hit's response file", When: selected, Run: func() tea.Cmd { return m.copyPath(false) }},
		{ID: "export.dir", Title: "Copy the path of the run's directory", When: selected, Run: func() tea.Cmd { return m.copyPath(true) }},
		{ID: "export.edit", Title: "Save the hit's request as a new request", When: selected, Run: m.editHit},
	}
}

// editorCommands are the request pane's, and n to create a request while
// none is open.
func (m *Model) editorCommands() []commands.Command {
	return append(m.editor.Commands(), commands.Command{
		ID: "tree.new", Title: "New request", Help: "new",
		When: func() bool { return m.editor.Name() == "" },
		Run:  func() tea.Cmd { return m.askNew(m.tree.Dir(), false, "") },
	})
}

// path is the chain of scopes for the focused pane, outermost first.
func (m *Model) path() []commands.Scope {
	s := []commands.Scope{{Name: "steep", Commands: m.global()}}
	switch m.focus {
	case paneCollection:
		s = append(s, commands.Scope{Name: "Collection", Commands: m.tree.Commands()})
	case paneEditor:
		s = append(s, commands.Scope{Name: "Request", Commands: m.editorCommands()})
	case paneResults:
		s = append(s, commands.Scope{Name: "Results", Commands: append(m.results.Commands(), m.hitCommands()...)})
	case paneResponse:
		s = append(s, commands.Scope{Name: "Response", Commands: append(m.response.Commands(), m.hitCommands()...)})
	}
	return s
}

// allScopes lists every scope, for checking the keys at startup.
func (m *Model) allScopes() []commands.Scope {
	return []commands.Scope{
		{Name: "steep", Commands: m.global()},
		{Name: "Collection", Commands: m.tree.Commands()},
		{Name: "Request", Commands: m.editorCommands()},
		{Name: "Results", Commands: append(m.results.Commands(), m.hitCommands()...)},
		{Name: "Response", Commands: append(m.response.Commands(), m.hitCommands()...)},
		oppicker.New("", nil).Scope(),
		picker.New("Pipes", "pipes", nil, nil).Scope(),
		picker.New("Filters", "filters", nil, nil).Scope(),
		copypicker.New("", nil, nil).Scope(),
	}
}

func (m *Model) navigate(d geom.Dir) {
	var cands []geom.Rect
	var panes []pane
	for p := range paneCount {
		if p != m.focus && !m.rects[p].Empty() {
			cands = append(cands, m.rects[p])
			panes = append(panes, p)
		}
	}
	if i := geom.Neighbor(m.rects[m.focus], cands, d); i >= 0 {
		m.setFocus(panes[i])
	}
}
