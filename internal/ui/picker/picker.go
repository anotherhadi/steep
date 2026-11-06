// Package picker is an overlay that finds an item by name or value: a pipe
// of the config, a saved or recent filter.
package picker

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/sectui/foam/commands"
	"github.com/sectui/foam/draw"
	"github.com/sectui/foam/fuzzy"
	"github.com/sectui/foam/input"
	"github.com/sectui/foam/list"
	"github.com/sectui/foam/overlay"
	"github.com/sectui/foam/theme"
)

type Item struct{ Name, Value string }

type Model struct {
	title string
	group string // of the commands: pipes.pick, filters.pick
	all   []Item
	shown []Item
	input input.Model
	list  list.Model
	pick  func(Item) tea.Cmd
}

// New offers items under title; pick gets the chosen one, and its command
// runs once the overlay is closed. group names the overlay's commands.
func New(title, group string, items []Item, pick func(Item) tea.Cmd) *Model {
	m := &Model{title: title, group: group, all: items, input: input.New("> "), pick: pick}
	m.input.Placeholder = "find"
	m.filter()
	return m
}

func (m *Model) Capturing() bool { return true }

func (m *Model) filter() {
	texts := make([]string, len(m.all))
	for i, p := range m.all {
		texts[i] = p.Name + " " + p.Value
	}
	m.shown = m.shown[:0]
	for _, i := range fuzzy.Filter(m.input.Value(), texts) {
		m.shown = append(m.shown, m.all[i])
	}
	m.list.SetLen(len(m.shown))
	m.list.Select(0)
}

// Selected is the item under the cursor; ok is false when none matches.
func (m *Model) Selected() (p Item, ok bool) {
	if len(m.shown) == 0 {
		return p, false
	}
	return m.shown[m.list.Selected()], true
}

func (m *Model) choose() tea.Cmd {
	p, ok := m.Selected()
	if !ok {
		return nil
	}
	// run here, in Update: pick edits the app's state
	return tea.Sequence(overlay.Close, m.pick(p))
}

func (m *Model) Scope() commands.Scope {
	return commands.Scope{Name: m.title, Commands: []commands.Command{
		{ID: m.group + ".close", Help: "close", Run: func() tea.Cmd { return overlay.Close }},
		{ID: m.group + ".pick", Help: "pick", Run: m.choose},
		{ID: m.group + ".up", Run: func() tea.Cmd { m.list.Move(-1); return nil }},
		{ID: m.group + ".down", Run: func() tea.Cmd { m.list.Move(1); return nil }},
	}}
}

func (m *Model) Update(msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case tea.KeyPressMsg, tea.PasteMsg:
		if m.input.Update(msg) {
			m.filter()
		}
	case tea.MouseClickMsg:
		// border, input and rule come before the first row
		mm := msg.Mouse()
		mm.Y -= 3
		if mm.Y >= 0 && m.list.Mouse(tea.MouseClickMsg(mm)) >= 0 {
			return m.choose()
		}
	case tea.MouseWheelMsg:
		m.list.Mouse(msg)
	}
	return nil
}

func (m *Model) View(maxW, maxH int) string {
	w := min(72, maxW)
	inner := max(w-2, 0)
	m.list.SetHeight(max(min(12, maxH-4), 0))

	lines := []string{m.input.View(inner, true), draw.Rule(inner)}
	nameW := 0
	for _, p := range m.shown {
		nameW = max(nameW, ansi.StringWidth(p.Name))
	}
	nameW = min(nameW, inner/3)
	start, end := m.list.Visible()
	for i := start; i < end; i++ {
		p := m.shown[i]
		name := ansi.Truncate(p.Name, nameW, "…")
		cmd := ansi.Truncate(p.Value, max(inner-nameW-5, 0), "…")
		gap := max(inner-nameW-ansi.StringWidth(cmd)-4, 0)
		row := " " + theme.Fg().Render(name+strings.Repeat(" ", nameW-ansi.StringWidth(name))) + "  " +
			theme.Subtle().Render(cmd) + strings.Repeat(" ", gap) + " "
		if i == m.list.Selected() {
			row = draw.Selected(row, inner, true)
		}
		lines = append(lines, row)
	}
	if len(m.shown) == 0 {
		lines = append(lines, theme.Subtle().Render(" nothing matches"))
	}
	return draw.Frame(strings.Join(lines, "\n"), w, len(lines)+2, draw.FrameOpts{Title: m.title, Active: true})
}
