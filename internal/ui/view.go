package ui

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/sectui/foam/draw"
	"github.com/sectui/foam/geom"
	"github.com/sectui/foam/minsize"
	"github.com/sectui/foam/theme"

	"github.com/sectui/steep/internal/highlight"
	"github.com/sectui/steep/internal/request"
)

// The collection is 32 cells wide, 24 below 140 columns, and hidden below
// 100 unless asked for.
const (
	collectionWidth       = 32
	collectionNarrowWidth = 24
	collectionNarrowBelow = 140
	collectionHideBelow   = 100
)

// minSize is the smallest terminal steep draws in.
var minSize = minsize.Size{W: 80, H: 20}

// layout places the panes: the bar on top, the help bar at the bottom, and
// between them the collection, the request, then results over response.
func (m *Model) layout() {
	m.rects = [paneCount]geom.Rect{}
	if m.w == 0 || m.h == 0 {
		return
	}
	top, bottom := 1, 0
	if theme.HelpBar() {
		bottom = 1
	}
	if theme.Spacious() {
		top++
		if bottom > 0 {
			bottom++
		}
	}
	body := geom.Rect{X: 0, Y: top, W: m.w, H: max(m.h-top-bottom, 0)}

	var cols []geom.Rect
	if m.collectionShown() {
		cw := collectionWidth
		if m.w < collectionNarrowBelow {
			cw = collectionNarrowWidth
		}
		cols = body.Columns(geom.Fixed(min(cw, m.w/3)), geom.Ratio(1), geom.Ratio(1))
		m.rects[paneCollection] = cols[0]
		cols = cols[1:]
	} else {
		cols = body.Columns(geom.Ratio(1), geom.Ratio(1))
	}
	m.rects[paneEditor] = cols[0]
	if m.results.Visible() {
		right := cols[1].Rows(geom.Ratio(2), geom.Ratio(3))
		m.rects[paneResults], m.rects[paneResponse] = right[0], right[1]
	} else {
		m.rects[paneResponse] = cols[1]
	}
	if m.rects[m.focus].Empty() {
		m.focus = paneEditor
	}
	if m.zoomed {
		m.rects = [paneCount]geom.Rect{}
		m.rects[m.focus] = body
	}
	r := m.rects[paneEditor]
	m.editor.SetSize(max(r.W-2, 0), max(r.H-2, 0))
}

func (m *Model) View() tea.View {
	v := tea.NewView("")
	v.AltScreen = true
	v.WindowTitle = "steep"
	if theme.Mouse() {
		v.MouseMode = tea.MouseModeCellMotion
	}
	if m.w == 0 || m.h == 0 {
		return v
	}
	if !minSize.Fits(m.w, m.h) {
		v.SetContent(minSize.View(m.w, m.h))
		return v
	}
	m.layout()

	c := lipgloss.NewCanvas(m.w, m.h)
	layers := []*lipgloss.Layer{lipgloss.NewLayer(m.topBar())}
	for p := range paneCount {
		r := m.rects[p]
		if r.Empty() {
			continue
		}
		layers = append(layers, lipgloss.NewLayer(m.paneView(p, r, false)).X(r.X).Y(r.Y))
	}
	if theme.HelpBar() {
		layers = append(layers, lipgloss.NewLayer(m.helpBar()).Y(m.h-1))
	}
	c.Compose(lipgloss.NewCompositor(layers...))
	draw.FillRow(c, 0, theme.Surface())
	if theme.HelpBar() {
		draw.FillRow(c, m.h-1, theme.Surface())
	}
	m.overlays.Draw(c)
	if t := m.toasts(); len(t) > 0 {
		c.Compose(lipgloss.NewCompositor(t...))
	}
	v.SetContent(c.Render())

	if m.overlays.Top() == nil && m.focus == paneEditor {
		if x, y, ok := m.editor.CursorPosition(); ok {
			r := m.rects[paneEditor]
			cur := tea.NewCursor(r.X+1+x, r.Y+1+y)
			cur.Shape = tea.CursorBar
			v.Cursor = cur
		}
	}
	return v
}

// paneView draws pane p in r; for a screenshot, unfocused and without the
// vim mode.
func (m *Model) paneView(p pane, r geom.Rect, shot bool) string {
	w, h := max(r.W-2, 0), max(r.H-2, 0)
	focused := !shot && p == m.focus && m.overlays.Top() == nil
	o := draw.FrameOpts{Title: paneNames[p], Active: !shot && p == m.focus}
	var content string
	switch p {
	case paneCollection:
		content = m.tree.View(w, h, focused)
		o.Scroll = m.tree.Scroll()
	case paneEditor:
		content = m.editor.View(w, h, focused)
		if shot {
			content = m.editor.ShotView(w, h)
		}
		o.Title = m.editor.Title()
		if shot {
			o.Title = draw.Clean(m.editor.Name())
		}
		if m.editor.Name() != "" {
			// last: the title's style stops where the dot's color does
			if dot := highlight.PwnFoxDot(request.PwnFoxColor(m.editor.Text())); dot != "" {
				o.Title += " " + dot
			}
		}
		o.Scroll = m.editor.Scroll()
		o.Footer = m.countText()
	case paneResults:
		content = m.results.View(w, h, focused)
		o.Title = m.results.Title()
		o.Scroll = m.results.Scroll()
	case paneResponse:
		content = m.response.View(w, h, focused, m.emptyResponse())
		if hit, ok := m.results.Selected(); ok && m.results.Hits() > 1 {
			o.Title = fmt.Sprintf("Response #%d", hit.N)
		}
		o.Right = m.response.Status()
		o.Scroll = m.response.Scroll()
	}
	o.Title = paneIcons[p]() + o.Title
	return draw.Frame(content, r.W, r.H, o)
}

// paneIcons put a Nerd Font glyph before each pane's title, nothing without.
var paneIcons = [paneCount]func() string{
	paneCollection: func() string { return theme.Icon("\uf0e8 ", "") },
	paneEditor:     func() string { return theme.Icon("\uf1c9 ", "") },
	paneResults:    func() string { return theme.Icon("\uf0ce ", "") },
	paneResponse:   func() string { return theme.Icon("\uf01c ", "") },
}

func (m *Model) emptyResponse() string {
	switch {
	case m.editor.Name() == "":
		return "no request open"
	case m.fz != nil && m.runName == m.editor.Name():
		return "waiting for hits"
	case m.results.Hits() == 0:
		return "no response yet: run the request"
	}
	return "no hit selected"
}

// url is the method and where the open request goes, built from what is
// typed, its blocks shown as written. While the request line is being typed,
// the last ones stay.
func (m *Model) url() (method, url string) {
	if m.editor.Name() == "" {
		return "", ""
	}
	f := request.Parse(m.editor.Text())
	text, unmask := f.Body, func(s string) string { return s }
	if b, err := request.ParseBody(f.Body); err == nil {
		// blocks may hold spaces, which would split the request line
		text, unmask = b.Mask()
	}
	if strings.TrimSpace(text) == "" {
		// no request line at all: nothing is being typed, nothing to keep
		m.lastMethod, m.lastURL = "", ""
		return "", ""
	}
	method, target, host, ok := request.Peek(text)
	if !ok {
		return m.lastMethod, m.lastURL
	}
	h := request.HTTP{Target: target, Headers: []request.Header{{Name: "Host", Value: host}}}
	u := unmask(h.URL(f.Effective(m.cfg.Request.Values())["proto"]))
	m.lastMethod, m.lastURL = unmask(method), u
	return m.lastMethod, u
}

func (m *Model) topBar() string {
	left := theme.Accent().Style().Bold(true).Render(" " + theme.Icon("󰶞 ", "") + "steep ")
	return m.bar(left, m.runState())
}

func (m *Model) runState() string {
	right := ""
	if m.fz != nil {
		p := m.fz.Progress()
		right = theme.Primary().Render("running")
		if m.fz.Paused() {
			right = theme.Warning().Render("paused")
		}
		if p.Total > 0 {
			right += theme.Subtle().Render(fmt.Sprintf(" %d%%", p.Done*100/p.Total))
		}
		right += " "
	}
	return right
}

// bar is the top bar: left, the open request's URL in the middle, right.
func (m *Model) bar(left, right string) string {
	lw, rw := ansi.StringWidth(left), ansi.StringWidth(right)
	room := m.w - lw - rw - 2
	url := ""
	if method, u := m.url(); room > 8 && u != "" {
		url = highlight.MethodColor(method).Style().Bold(true).Render(draw.Clean(method)) + " " +
			highlight.URL(draw.Clean(u))
		url = ansi.Truncate(url, room, "…")
	}
	pad := max(room+2-ansi.StringWidth(url), 0)
	line := left + strings.Repeat(" ", pad/2) + url + strings.Repeat(" ", pad-pad/2) + right
	return ansi.Truncate(line, m.w, "")
}

// toasts draws the live notifications at the top right, newest first.
func (m *Model) toasts() []*lipgloss.Layer {
	var out []*lipgloss.Layer
	w := min(48, max(m.w/3, 24), m.w)
	y := 0
	m.toastRects = m.toastRects[:0]
	live := m.notes.Live()
	for i := len(live) - 1; i >= 0 && i >= len(live)-5; i-- {
		s := live[i].Toast(w)
		r := geom.Rect{X: max(m.w-w, 0), Y: y, W: w, H: lipgloss.Height(s)}
		if len(out) > 0 && y+r.H > m.h {
			break // the older ones would be cut
		}
		m.toastRects = append(m.toastRects, toastRect{r, live[i].ID})
		out = append(out, lipgloss.NewLayer(s).X(r.X).Y(r.Y).Z(20))
		y += r.H
	}
	return out
}

func (m *Model) helpBar() string {
	ks, ls := theme.Primary().Style(), theme.Subtle().Style()
	var parts []string
	for _, h := range m.reg.Hints(m.path()) {
		first, _, _ := strings.Cut(h.Keys, " / ") // the others are in the help
		parts = append(parts, ks.Render(first)+" "+ls.Render(h.Label))
	}
	return draw.Pad(ansi.Truncate(" "+strings.Join(parts, ls.Render("  ·  ")), m.w, "…"), m.w)
}
