// Package oppicker is an overlay that finds an operation of
// github.com/sectui/operations, to put a block of it in the request.
package oppicker

import (
	"fmt"
	"slices"
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
	"github.com/sectui/operations"

	"github.com/sectui/steep/internal/request"
)

type Model struct {
	title string
	all   []*operations.Operation
	shown []*operations.Operation
	input input.Model
	list  list.Model
	pick  func(*operations.Operation) tea.Cmd
}

// New offers every operation; pick gets the chosen one, and its command
// runs once the overlay is closed.
func New(title string, pick func(*operations.Operation) tea.Cmd) *Model {
	all := append(slices.Clone(request.Ops[1:]), operations.All()...) // file{} has ctrl+t
	m := &Model{title: title, all: all, input: input.New("> "), pick: pick}
	m.input.Placeholder = "find an operation"
	m.filter()
	return m
}

func (m *Model) Capturing() bool { return true }

// searchText is what the query matches: the id first, then the name,
// aliases and category.
func searchText(op *operations.Operation) string {
	return op.ID + " " + op.Name + " " + strings.Join(op.Aliases, " ") + " " + string(op.Category)
}

func (m *Model) filter() {
	texts := make([]string, len(m.all))
	for i, op := range m.all {
		texts[i] = searchText(op)
	}
	m.shown = m.shown[:0]
	for _, i := range fuzzy.Filter(m.input.Value(), texts) {
		m.shown = append(m.shown, m.all[i])
	}
	m.list.SetLen(len(m.shown))
	m.list.Select(0)
}

// Selected is the operation under the cursor, nil when none matches.
func (m *Model) Selected() *operations.Operation {
	if len(m.shown) == 0 {
		return nil
	}
	return m.shown[m.list.Selected()]
}

func (m *Model) choose() tea.Cmd {
	op := m.Selected()
	if op == nil {
		return nil
	}
	// run here, in Update: pick edits the app's state
	return tea.Sequence(overlay.Close, m.pick(op))
}

func (m *Model) Scope() commands.Scope {
	return commands.Scope{Name: "Operations", Commands: []commands.Command{
		{ID: "operations.close", Help: "close", Run: func() tea.Cmd { return overlay.Close }},
		{ID: "operations.pick", Help: "insert", Run: m.choose},
		{ID: "operations.up", Run: func() tea.Cmd { m.list.Move(-1); return nil }},
		{ID: "operations.down", Run: func() tea.Cmd { m.list.Move(1); return nil }},
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

// argsText sums up the arguments: name=default, the choices of a select.
func argsText(op *operations.Operation) string {
	var parts []string
	for _, a := range op.Args {
		switch a.Type {
		case operations.Bool:
			parts = append(parts, a.ID)
		case operations.Select:
			parts = append(parts, a.ID+"="+strings.Join(a.Values(), "|"))
		default:
			parts = append(parts, fmt.Sprintf("%s=%v", a.ID, a.Default))
		}
	}
	return strings.Join(parts, ", ")
}

func (m *Model) View(maxW, maxH int) string {
	w := min(72, maxW)
	inner := max(w-2, 0)
	// input, rule, the rows, a rule and the details
	details := m.details(inner)
	rows := max(min(12, maxH-4-1-len(details)), 1)
	m.list.SetHeight(rows)

	lines := []string{m.input.View(inner, true), draw.Rule(inner)}
	idW := 0
	for _, op := range m.shown {
		idW = max(idW, len(op.ID))
	}
	idW = min(idW, inner/2)
	start, end := m.list.Visible()
	for i := start; i < end; i++ {
		op := m.shown[i]
		cat := string(op.Category)
		id := ansi.Truncate(op.ID, idW, "…")
		nameW := max(inner-idW-ansi.StringWidth(cat)-5, 0)
		name := ansi.Truncate(op.Name, nameW, "…")
		gap := max(inner-2-idW-2-ansi.StringWidth(name)-ansi.StringWidth(cat)-1, 1)
		row := " " + theme.Fg().Render(id+strings.Repeat(" ", idW-ansi.StringWidth(id))) + "  " +
			theme.Subtle().Render(name) + strings.Repeat(" ", gap) + theme.Muted().Render(cat) + " "
		if i == m.list.Selected() {
			row = draw.Selected(row, inner, true)
		}
		lines = append(lines, row)
	}
	if len(m.shown) == 0 {
		lines = append(lines, theme.Subtle().Render(" no operation"))
	}
	if len(details) > 0 {
		lines = append(lines, draw.Rule(inner))
		lines = append(lines, details...)
	}
	return draw.Frame(strings.Join(lines, "\n"), w, len(lines)+2, draw.FrameOpts{Title: m.title, Active: true})
}

// details are the description and arguments of the selected operation,
// wrapped to w.
func (m *Model) details(w int) []string {
	op := m.Selected()
	if op == nil || w < 4 {
		return nil
	}
	var out []string
	for _, l := range strings.Split(ansi.Wordwrap(op.Description, w-2, ""), "\n") {
		out = append(out, " "+theme.Fg().Render(l))
	}
	if a := argsText(op); a != "" {
		for _, l := range strings.Split(ansi.Wordwrap("("+a+")", w-2, ",|"), "\n") {
			out = append(out, " "+theme.Subtle().Render(l))
		}
	}
	return out[:min(len(out), 4)]
}
