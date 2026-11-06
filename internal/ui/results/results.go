// Package results is the results pane: the hits of the open request's run,
// sorted and filtered with HTTPQL, and the progress of a run in flight.
package results

import (
	"fmt"
	"image/color"
	"maps"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"charm.land/bubbles/v2/progress"
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/sectui/foam/commands"
	"github.com/sectui/foam/draw"
	"github.com/sectui/foam/input"
	"github.com/sectui/foam/list"
	"github.com/sectui/foam/notify"
	"github.com/sectui/foam/overlay"
	"github.com/sectui/foam/theme"

	"github.com/sectui/steep/internal/config"
	"github.com/sectui/steep/internal/fuzz"
	"github.com/sectui/steep/internal/highlight"
	"github.com/sectui/steep/internal/httpql"
	"github.com/sectui/steep/internal/run"
	"github.com/sectui/steep/internal/ui/picker"
)

// OpenMsg asks to read the selected hit's response.
type OpenMsg struct{}

// ForgetMsg asks to forget hits, by number: the selected one, or All the
// hits shown, Filtered when a filter chose them.
type ForgetMsg struct {
	N        []int
	All      bool
	Filtered bool
}

// ForgetCollectionMsg asks to forget every run of the collection.
type ForgetCollectionMsg struct{}

// HistoryMsg carries the filters used, oldest first, to be kept.
type HistoryMsg struct{ History []string }

const historySize = 200

type filteredMsg struct {
	gen     int
	matches map[int]bool
}

var columns = []string{"order", "status", "size", "words", "lines", "duration"}

type Model struct {
	dir   string
	hits  []run.Hit
	seen  map[int]bool // hit numbers held
	shown []int
	list  list.Model

	sortCol int
	desc    bool

	filter    input.Model
	filtering bool
	query     *httpql.Query
	qerr      string
	matches   map[int]bool // by hit N; nil when every hit passes
	pending   bool         // a raw filter is being computed
	gen       int
	stale     *atomic.Bool // set when the raw filter computed is outdated

	marked  map[int]bool // by hit N, kept in the run's marks.json
	filters []config.Filter
	presets map[string]string
	history []string
	histPos int    // in history while going through it with up and down, else -1
	draft   string // the filter typed before going through the history

	progress fuzz.Progress
	running  bool
	paused   bool
}

func New(filters []config.Filter, history []string) *Model {
	m := &Model{filter: input.New("/ "), filters: filters, presets: map[string]string{}, history: history, histPos: -1}
	for _, f := range filters {
		m.presets[f.Name] = f.Query
	}
	m.filter.Placeholder = `resp.code.eq:200 AND req.path.cont:"admin"`
	return m
}

func (m *Model) SetRun(dir string, hits []run.Hit) tea.Cmd {
	m.dir = dir
	m.hits = hits
	m.marked = run.Marks(dir)
	m.seen = make(map[int]bool, len(hits))
	for _, h := range hits {
		m.seen[h.N] = true
	}
	m.shown = m.shown[:0] // indexes into the hits just replaced
	m.list.SetLen(0)
	m.running, m.paused, m.progress = false, false, fuzz.Progress{}
	return m.refilter()
}

// Add appends hits that just arrived. A hit already held is skipped: when a
// run is reopened from disk, what it wrote there may still be on its way.
func (m *Model) Add(hits []run.Hit) {
	sel, ok := m.Selected()
	if m.seen == nil {
		m.seen = map[int]bool{}
	}
	fresh := hits[:0:0]
	for _, h := range hits {
		if !m.seen[h.N] {
			m.seen[h.N] = true
			fresh = append(fresh, h)
		}
	}
	hits = fresh
	m.hits = append(m.hits, hits...)
	if m.matches != nil && m.query != nil {
		for _, h := range hits {
			if m.query.Match(m.record(h)) {
				m.matches[h.N] = true
			}
		}
	}
	m.reorder(sel.N, ok)
}

func (m *Model) SetProgress(p fuzz.Progress, running, paused bool) {
	m.progress, m.running, m.paused = p, running, paused
}

func (m *Model) Hits() int { return len(m.hits) }

// Visible tells whether the pane is worth its room: more than one hit, a
// filter in force, or a run of more than one request going.
func (m *Model) Visible() bool {
	return len(m.hits) > 1 || m.filter.Value() != "" || m.filtering || (m.running && m.progress.Total > 1)
}

func (m *Model) Selected() (run.Hit, bool) {
	i := m.list.Selected()
	if i >= len(m.shown) || m.shown[i] >= len(m.hits) {
		return run.Hit{}, false
	}
	return m.hits[m.shown[i]], true
}

// Shown are the hits the filter lets through, in the order shown.
func (m *Model) Shown() []run.Hit {
	out := make([]run.Hit, 0, len(m.shown))
	for _, i := range m.shown {
		if i < len(m.hits) {
			out = append(out, m.hits[i])
		}
	}
	return out
}

func (m *Model) Dir() string { return m.dir }

// Cursor is the place of the selection in the list shown.
func (m *Model) Cursor() int { return m.list.Selected() }

func (m *Model) SetCursor(i int) { m.list.Select(i) }

func (m *Model) less(a, b run.Hit) int {
	var x, y int64
	switch columns[m.sortCol] {
	case "status":
		x, y = int64(a.Status), int64(b.Status)
	case "size":
		x, y = a.Length, b.Length
	case "words":
		x, y = a.Words, b.Words
	case "lines":
		x, y = a.Lines, b.Lines
	case "duration":
		x, y = int64(a.Duration), int64(b.Duration)
	default:
		x, y = int64(a.N), int64(b.N)
	}
	d := 0
	switch {
	case x < y:
		d = -1
	case x > y:
		d = 1
	}
	if m.desc {
		d = -d
	}
	return d
}

// reorder recomputes what is shown, keeping the selection on hit n.
func (m *Model) reorder(n int, keep bool) {
	m.shown = m.shown[:0]
	for i, h := range m.hits {
		if m.matches == nil || m.matches[h.N] {
			m.shown = append(m.shown, i)
		}
	}
	if m.sortCol != 0 || m.desc {
		slices.SortStableFunc(m.shown, func(a, b int) int { return m.less(m.hits[a], m.hits[b]) })
	}
	m.list.SetLen(len(m.shown))
	if keep {
		for i, k := range m.shown {
			if m.hits[k].N == n {
				m.list.Select(i)
				break
			}
		}
	}
}

func Record(dir string, h run.Hit) httpql.Record {
	r := httpql.Record{
		ID: h.N, Method: h.Method, Code: h.Status, Len: h.Length,
		Roundtrip: h.Duration.Milliseconds(), Words: h.Words, Lines: h.Lines,
		ReqRaw: func() string {
			b, _ := run.ReadRequest(dir, h.N)
			return string(b)
		},
		RespRaw: func() string {
			b, _ := run.ReadResponse(dir, h.N)
			return string(b)
		},
	}
	for _, v := range h.Input {
		r.Inputs = append(r.Inputs, v)
	}
	if u, err := url.Parse(h.URL); err == nil {
		r.Host, r.Path, r.Query, r.TLS = u.Hostname(), u.Path, u.RawQuery, u.Scheme == "https"
		r.Port, _ = strconv.Atoi(u.Port())
		if r.Port == 0 {
			r.Port = 80
			if r.TLS {
				r.Port = 443
			}
		}
	}
	return r
}

func (m *Model) record(h run.Hit) httpql.Record {
	r := Record(m.dir, h)
	r.Marked = m.marked[h.N]
	return r
}

// refilter parses the filter and applies it; a query reading raw bytes is
// computed in the background.
func (m *Model) refilter() tea.Cmd {
	sel, ok := m.Selected()
	m.gen++
	m.qerr, m.pending = "", false
	if m.stale != nil {
		m.stale.Store(true) // its result would be dropped anyway
		m.stale = nil
	}
	q, err := httpql.ParsePresets(m.filter.Value(), m.presets)
	switch {
	case err != nil:
		m.qerr = err.Error()
		return nil // keep the last good filter
	case m.filter.Value() == "":
		m.query, m.matches = nil, nil
	case q.NeedsRaw() && len(m.hits) > 0:
		m.query, m.matches = q, map[int]bool{}
		m.pending = true
		m.reorder(sel.N, ok)
		gen, dir, hits, marked := m.gen, m.dir, slices.Clone(m.hits), maps.Clone(m.marked)
		stale := new(atomic.Bool)
		m.stale = stale
		return func() tea.Msg {
			matches := map[int]bool{}
			for _, h := range hits {
				if stale.Load() {
					return nil
				}
				r := Record(dir, h)
				r.Marked = marked[h.N]
				if q.Match(r) {
					matches[h.N] = true
				}
			}
			return filteredMsg{gen, matches}
		}
	default:
		m.query = q
		m.matches = map[int]bool{}
		for _, h := range m.hits {
			if q.Match(m.record(h)) {
				m.matches[h.N] = true
			}
		}
	}
	m.reorder(sel.N, ok)
	return nil
}

// Update takes the result of a background filter.
func (m *Model) Update(msg tea.Msg) bool {
	if f, ok := msg.(filteredMsg); ok {
		if f.gen == m.gen {
			sel, keep := m.Selected()
			// the hits added meanwhile were matched by Add, into m.matches
			for n := range m.matches {
				f.matches[n] = true
			}
			m.matches, m.pending, m.stale = f.matches, false, nil
			m.reorder(sel.N, keep)
		}
		return true
	}
	return false
}

func (m *Model) Capturing() bool { return m.filtering }

// Input handles a key while the filter is being typed.
func (m *Model) Input(msg tea.KeyPressMsg) (bool, tea.Cmd) {
	switch msg.String() {
	case "enter":
		m.filtering = false
		return true, m.remember()
	case "esc", "ctrl+c":
		m.filtering = false
		return true, nil
	case "up":
		return true, m.browse(-1)
	case "down":
		return true, m.browse(1)
	case "ctrl+k", "ctrl+p":
		m.list.Move(-1)
		return true, nil
	case "ctrl+j", "ctrl+n":
		m.list.Move(1)
		return true, nil
	}
	if !m.filter.Update(msg) {
		return false, nil
	}
	m.histPos = -1
	return true, m.refilter()
}

// browse moves through the history: d -1 to older filters, 1 back to the
// newer ones and then to what was being typed.
func (m *Model) browse(d int) tea.Cmd {
	if m.histPos == -1 {
		if d > 0 || len(m.history) == 0 {
			return nil
		}
		m.draft, m.histPos = m.filter.Value(), len(m.history)
	}
	m.histPos = max(m.histPos+d, 0)
	if m.histPos >= len(m.history) {
		m.histPos = -1
		m.filter.SetValue(m.draft)
	} else {
		m.filter.SetValue(m.history[m.histPos])
	}
	return m.refilter()
}

// remember puts the filter at the end of the history.
func (m *Model) remember() tea.Cmd {
	m.histPos = -1
	q := strings.TrimSpace(m.filter.Value())
	if q == "" || m.qerr != "" || len(m.history) > 0 && m.history[len(m.history)-1] == q {
		return nil
	}
	m.history = append(slices.DeleteFunc(m.history, func(h string) bool { return h == q }), q)
	if len(m.history) > historySize {
		m.history = m.history[len(m.history)-historySize:]
	}
	return send(HistoryMsg{slices.Clone(m.history)})
}

// pickFilter offers the saved filters, then the recent ones.
func (m *Model) pickFilter() tea.Cmd {
	var items []picker.Item
	for _, f := range m.filters {
		items = append(items, picker.Item{Name: f.Name, Value: f.Query})
	}
	for i := len(m.history) - 1; i >= 0; i-- {
		items = append(items, picker.Item{Name: "recent", Value: m.history[i]})
	}
	return overlay.Open(picker.New("Filters", "filters", items, func(it picker.Item) tea.Cmd {
		m.filtering = false
		m.filter.SetValue(it.Value)
		return tea.Batch(m.refilter(), m.remember())
	}))
}

func (m *Model) toggleMark() tea.Cmd {
	hit, ok := m.Selected()
	if !ok {
		return nil
	}
	if m.marked == nil {
		m.marked = map[int]bool{}
	}
	if m.marked[hit.N] {
		delete(m.marked, hit.N)
	} else {
		m.marked[hit.N] = true
	}
	m.list.Move(1)
	var cmd tea.Cmd
	if m.query != nil {
		cmd = m.refilter()
	}
	if err := run.SaveMarks(m.dir, m.marked); err != nil {
		return tea.Batch(cmd, notify.Err(err))
	}
	return cmd
}

func (m *Model) Paste(s string) tea.Cmd {
	if !m.filtering {
		return nil
	}
	m.filter.Update(tea.PasteMsg{Content: s})
	return m.refilter()
}

// note explains an empty result: a filter on a status that ffuf's
// matchers never let through.
func (m *Model) note() string {
	switch {
	case m.qerr != "":
		return m.qerr
	case m.pending:
		return "filtering"
	case len(m.shown) == 0 && m.query != nil && len(m.hits) > 0:
		return "no hit matches; only what ffuf's matchers accept is ever reported"
	}
	return ""
}

func send(v tea.Msg) tea.Cmd { return func() tea.Msg { return v } }

func (m *Model) Commands() []commands.Command {
	hasHits := func() bool { return len(m.hits) > 0 }
	return append(m.list.Commands(),
		commands.Command{ID: "results.open", Help: "response", When: hasHits, Run: func() tea.Cmd { return send(OpenMsg{}) }},
		commands.Command{ID: "results.filter", Help: "filter", Run: func() tea.Cmd { m.filtering, m.histPos = true, -1; return nil }},
		commands.Command{ID: "results.filters", Title: "Pick a saved or recent filter", Run: m.pickFilter},
		commands.Command{ID: "results.mark", Title: "Mark the hit or unmark it", Help: "mark", When: hasHits, Run: m.toggleMark},
		commands.Command{ID: "results.clear-filter", When: func() bool { return m.filter.Value() != "" },
			Run: func() tea.Cmd { m.filter.Reset(); return m.refilter() }},
		commands.Command{ID: "results.sort", Title: "Sort the results by the next column", Help: "sort",
			Run: func() tea.Cmd {
				sel, ok := m.Selected()
				m.sortCol = (m.sortCol + 1) % len(columns)
				m.reorder(sel.N, ok)
				return nil
			}},
		commands.Command{ID: "results.reverse", Title: "Reverse the order of the results",
			Run: func() tea.Cmd {
				sel, ok := m.Selected()
				m.desc = !m.desc
				m.reorder(sel.N, ok)
				return nil
			}},
		commands.Command{ID: "results.forget", Title: "Forget the selected hit",
			When: func() bool { _, ok := m.Selected(); return ok && !m.running },
			Run: func() tea.Cmd {
				hit, _ := m.Selected()
				return send(ForgetMsg{N: []int{hit.N}})
			}},
		commands.Command{ID: "results.forget-all", Title: "Forget the hits shown",
			// while the run writes, or the filter is not done, what is shown
			// is not what the filter says
			When: func() bool { return len(m.shown) > 0 && !m.running && !m.pending && m.qerr == "" },
			Run: func() tea.Cmd {
				hits := m.Shown()
				ns := make([]int, len(hits))
				for i, h := range hits {
					ns[i] = h.N
				}
				return send(ForgetMsg{N: ns, All: true, Filtered: m.filter.Value() != ""})
			}},
		commands.Command{ID: "results.forget-collection", Title: "Forget every stored response",
			Run: func() tea.Cmd { return send(ForgetCollectionMsg{}) }},
	)
}

// Title is "Results (18) · size ↓".
func (m *Model) Title() string {
	t := fmt.Sprintf("Results (%d)", len(m.shown))
	if len(m.shown) != len(m.hits) {
		t = fmt.Sprintf("Results (%d/%d)", len(m.shown), len(m.hits))
	}
	if m.sortCol != 0 || m.desc {
		arrow := "↑"
		if m.desc {
			arrow = "↓"
		}
		t += " · " + columns[m.sortCol] + " " + arrow
	}
	return t
}

func (m *Model) Scroll() draw.Scroll { return m.list.Scroll() }

func (m *Model) header() int {
	n := 1 // column titles
	if m.filtering || m.filter.Value() != "" {
		n += 2
		if m.note() != "" {
			n++
		}
	}
	return n
}

// Mouse selects on click (a second click opens), scrolls on the wheel.
func (m *Model) Mouse(ev tea.MouseMsg) tea.Cmd {
	mm := ev.Mouse()
	if _, click := ev.(tea.MouseClickMsg); click && mm.Y < m.header()-1 {
		// the filter line, its note or the rule under them
		m.filtering = true
		return nil
	}
	mm.Y -= m.header()
	switch ev.(type) {
	case tea.MouseClickMsg:
		before := m.list.Selected()
		if i := m.list.Mouse(tea.MouseClickMsg(mm)); i >= 0 && i == before {
			return send(OpenMsg{})
		}
	case tea.MouseWheelMsg:
		m.list.Mouse(tea.MouseWheelMsg(mm))
	}
	return nil
}

func size(n int64) string {
	switch {
	case n >= 1<<20:
		return fmt.Sprintf("%.1fM", float64(n)/(1<<20))
	case n >= 10*1024:
		return fmt.Sprintf("%.0fK", float64(n)/1024)
	}
	return strconv.FormatInt(n, 10)
}

func duration(d time.Duration) string {
	if d >= time.Second {
		return fmt.Sprintf("%.1fs", d.Seconds())
	}
	return fmt.Sprintf("%dms", d.Milliseconds())
}

// Inputs are the values that produced a hit: "admin", or "1=admin 2=x"
// with several blocks.
func Inputs(h run.Hit) string {
	keys := inputKeys(h)
	vals := make([]string, len(keys))
	for i, k := range keys {
		vals[i] = h.Input[k]
		if len(keys) > 1 {
			vals[i] = strings.Trim(k, "§") + "=" + vals[i]
		}
	}
	return draw.Clean(strings.Join(vals, " "))
}

// InputComments are the values that produced h, one comment line each:
// "# input: admin", or "# input 2: admin" with more than one wordlist.
func InputComments(h run.Hit) []string {
	keys := inputKeys(h)
	out := make([]string, len(keys))
	for i, k := range keys {
		name := "input"
		if len(keys) > 1 {
			name += " " + strings.Trim(k, "§")
		}
		out[i] = "# " + name + ": " + draw.Clean(h.Input[k])
	}
	return out
}

func inputKeys(h run.Hit) []string {
	keys := make([]string, 0, len(h.Input))
	for k := range h.Input {
		keys = append(keys, k)
	}
	slices.SortStableFunc(keys, func(a, b string) int {
		if len(a) != len(b) {
			return len(a) - len(b)
		}
		return strings.Compare(a, b)
	})
	return keys
}

// Progress is the run's progress line, "" when no run is going.
func (m *Model) Progress(w int) string {
	if !m.running {
		return ""
	}
	p := m.progress
	info := fmt.Sprintf(" %d/%d", p.Done, p.Total)
	if m.paused {
		info += " · paused"
	} else {
		info += fmt.Sprintf(" · %d req/s", p.ReqSec)
	}
	if p.Errors > 0 {
		info += fmt.Sprintf(" · %d errors", p.Errors)
	}
	colors := []color.Color{theme.Primary().Value(), theme.Accent().Value()}
	if m.paused {
		colors = []color.Color{theme.Warning().Value()}
	}
	bar := progress.New(progress.WithColors(colors...), progress.WithoutPercentage(),
		progress.WithFillCharacters('━', '━'), progress.WithWidth(max(w-ansi.StringWidth(info)-1, 4)))
	bar.EmptyColor = theme.Selection().Value()
	done := 0.0
	if p.Total > 0 {
		done = min(float64(p.Done)/float64(p.Total), 1)
	}
	return bar.ViewAs(done) + theme.Subtle().Render(info)
}

func (m *Model) View(w, h int, focused bool) string {
	var lines []string
	if m.filtering || m.filter.Value() != "" {
		f := m.filter
		if !m.filtering {
			f.Update(tea.KeyPressMsg{Code: tea.KeyHome}) // a long query shows its start
		}
		lines = append(lines, f.View(w, m.filtering))
		if n := m.note(); n != "" {
			c := theme.Subtle()
			if m.qerr != "" {
				c = theme.Danger()
			}
			lines = append(lines, c.Render(ansi.Truncate(n, w, "…")))
		}
		lines = append(lines, draw.Rule(w))
	}
	progress := m.Progress(w)
	rows := h - len(lines) - 1
	if progress != "" {
		rows--
	}
	m.list.SetHeight(max(rows, 0))

	cols := visibleColumns(w)
	head := " " + fmt.Sprintf("%5s %4s %7s", "#", "code", "size")
	for _, c := range cols {
		head += fmt.Sprintf(" %*s", c.width, c.name)
	}
	lines = append(lines, theme.Subtle().Render(ansi.Truncate(head+"  input", w, "")))
	start, end := m.list.Visible()
	for i := start; i < end; i++ {
		hit := m.hits[m.shown[i]]
		code := highlight.StatusColor(hit.Status).Style().Bold(true).Render(fmt.Sprintf("%4d", hit.Status))
		mark := " "
		if m.marked[hit.N] {
			mark = theme.Accent().Render("●")
		}
		row := mark + fmt.Sprintf("%5d %s %7s", hit.N, code, size(hit.Length))
		for _, c := range cols {
			row += fmt.Sprintf(" %*s", c.width, c.value(hit))
		}
		row = ansi.Truncate(row+"  "+Inputs(hit), w, "…")
		if i == m.list.Selected() {
			row = draw.Selected(row, w, focused)
		}
		lines = append(lines, row)
	}
	if len(m.hits) == 0 && !m.running {
		lines = append(lines, theme.Subtle().Render(" no hit"))
	}
	out := draw.Fit(strings.Join(lines, "\n"), w, h-boolInt(progress != ""))
	if progress != "" {
		out += "\n" + progress
	}
	return out
}

type column struct {
	name  string
	width int
	value func(run.Hit) string
}

// Optional columns, the first ones kept first when room runs short.
var optional = []column{
	{"time", 7, func(h run.Hit) string { return duration(h.Duration) }},
	{"words", 6, func(h run.Hit) string { return strconv.FormatInt(h.Words, 10) }},
	{"lines", 6, func(h run.Hit) string { return strconv.FormatInt(h.Lines, 10) }},
}

// visibleColumns keeps the optional columns that leave the input column at
// least 16 cells.
func visibleColumns(w int) []column {
	used := 19 + 2 + 16 // "#, code, size", the gap, the input
	var out []column
	for _, c := range optional {
		if used+c.width+1 > w {
			break
		}
		used += c.width + 1
		out = append(out, c)
	}
	// keep the order of the table: words, lines, time
	order := map[string]int{"words": 0, "lines": 1, "time": 2}
	slices.SortFunc(out, func(a, b column) int { return order[a.name] - order[b.name] })
	return out
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
