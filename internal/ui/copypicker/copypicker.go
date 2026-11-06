// Package copypicker is an overlay asking what to copy of one hit or of the
// hits shown: the request (curl, raw HTTP), the response or a part of it, the
// URL or the input.
package copypicker

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/sectui/foam/commands"
	"github.com/sectui/foam/draw"
	"github.com/sectui/foam/keys"
	"github.com/sectui/foam/list"
	"github.com/sectui/foam/overlay"
	"github.com/sectui/foam/theme"
)

type Kind int

const (
	Curl Kind = iota
	HTTP
	Response
	Body
	Headers
	URL
	Input
)

// Options are the choices, in the order shown; ID is the command picking one.
var Options = []struct {
	Kind  Kind
	ID    string
	Label string
}{
	{Curl, "copy.curl", "request as curl"},
	{HTTP, "copy.http", "request as raw HTTP"},
	{Response, "copy.response", "response (headers + body)"},
	{Body, "copy.body", "response body"},
	{Headers, "copy.headers", "response headers"},
	{URL, "copy.url", "URL"},
	{Input, "copy.input", "input"},
}

type Model struct {
	title  string
	list   list.Model
	keysOf func(id string) keys.List
	pick   func(Kind) tea.Cmd
}

// New asks what to copy, under title ("Copy hit #12", "Copy 18 hits"); pick
// gets the choice once the overlay is closed. keysOf gives the keys shown
// beside each option.
func New(title string, keysOf func(id string) keys.List, pick func(Kind) tea.Cmd) *Model {
	m := &Model{title: title, keysOf: keysOf, pick: pick}
	m.list.SetLen(len(Options))
	return m
}

func (m *Model) choose(k Kind) tea.Cmd {
	// run here, in Update: pick reads the app's state
	return tea.Sequence(overlay.Close, m.pick(k))
}

func (m *Model) Scope() commands.Scope {
	cmds := []commands.Command{
		{ID: "copy.close", Help: "close", Run: func() tea.Cmd { return overlay.Close }},
		{ID: "copy.pick", Help: "copy", Run: func() tea.Cmd { return m.choose(Options[m.list.Selected()].Kind) }},
		{ID: "copy.up", Run: func() tea.Cmd { m.list.Move(-1); return nil }},
		{ID: "copy.down", Run: func() tea.Cmd { m.list.Move(1); return nil }},
	}
	for _, o := range Options {
		cmds = append(cmds, commands.Command{ID: o.ID, Title: "Copy the " + o.Label, Run: func() tea.Cmd { return m.choose(o.Kind) }})
	}
	return commands.Scope{Name: "Copy", Commands: cmds}
}

func (m *Model) Update(msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case tea.MouseClickMsg:
		// the border comes before the first row
		mm := msg.Mouse()
		mm.Y--
		if mm.Y >= 0 {
			if i := m.list.Mouse(tea.MouseClickMsg(mm)); i >= 0 {
				return m.choose(Options[i].Kind)
			}
		}
	case tea.MouseWheelMsg:
		m.list.Mouse(msg)
	}
	return nil
}

func (m *Model) View(maxW, maxH int) string {
	w := min(44, maxW)
	inner := max(w-2, 0)
	m.list.SetHeight(max(min(len(Options), maxH-2), 0))

	keyW := 0
	shown := make([]string, len(Options))
	for i, o := range Options {
		if m.keysOf != nil {
			if ks := m.keysOf(o.ID); len(ks) > 0 {
				shown[i] = string(ks[0])
			}
		}
		keyW = max(keyW, ansi.StringWidth(shown[i]))
	}
	var lines []string
	start, end := m.list.Visible()
	for i := start; i < end; i++ {
		k := shown[i] + strings.Repeat(" ", keyW-ansi.StringWidth(shown[i]))
		row := " " + theme.Primary().Render(k) + "  " + theme.Fg().Render(ansi.Truncate(Options[i].Label, max(inner-keyW-4, 0), "…"))
		if i == m.list.Selected() {
			row = draw.Selected(row, inner, true)
		}
		lines = append(lines, row)
	}
	return draw.Frame(strings.Join(lines, "\n"), w, len(lines)+2, draw.FrameOpts{Title: m.title, Active: true})
}
