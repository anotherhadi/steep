// Package ui is steep's root model: it owns the panes, decides the layout,
// routes keys and mouse, and carries out what the panes ask for.
package ui

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/anotherhadi/foam/commands"
	"github.com/anotherhadi/foam/geom"
	"github.com/anotherhadi/foam/notify"
	"github.com/anotherhadi/foam/overlay"

	"github.com/anotherhadi/steep/internal/collection"
	"github.com/anotherhadi/steep/internal/config"
	"github.com/anotherhadi/steep/internal/fuzz"
	"github.com/anotherhadi/steep/internal/remote"
	"github.com/anotherhadi/steep/internal/request"
	"github.com/anotherhadi/steep/internal/ui/editor"
	"github.com/anotherhadi/steep/internal/ui/response"
	"github.com/anotherhadi/steep/internal/ui/results"
	"github.com/anotherhadi/steep/internal/ui/tree"
)

type pane int

const (
	paneCollection pane = iota
	paneEditor
	paneResults
	paneResponse
	paneCount
)

var paneNames = [paneCount]string{"Collection", "Request", "Results", "Response"}

type Model struct {
	cfg *config.Config
	col *collection.Collection
	cwd string // where relative wordlist paths start
	reg *commands.Registry

	overlays overlay.Stack
	notes    notify.Center

	tree     *tree.Model
	editor   *editor.Model
	results  *results.Model
	response *response.Model

	focus          pane
	showCollection bool // the user's choice (ctrl+b)
	peek           bool // shown anyway until a request is picked
	zoomed         bool // the focused pane takes the whole body

	fz      *fuzz.Run
	runName string // the request being run

	onDisk     string // the open request as last read or written
	synced     string // the editor's text when it was onDisk: unsaved edits differ
	shownHit   string // dir and number of the hit in the response pane
	lastURL    string // the bar's URL, kept while the request line is typed
	lastMethod string
	count      countMsg
	countGen   int

	watch     <-chan struct{}
	stopWatch func()
	remote    *remote.Listener // orders of other tools: spilltea's focus

	pendingImport string // a request to name and save at start

	w, h       int
	rects      [paneCount]geom.Rect
	toastRects []toastRect
}

type toastRect struct {
	geom.Rect
	id int
}

func New(cfg *config.Config) (*Model, error) {
	request.Env = cfg.Steep.Env
	open := collection.Open
	if !cfg.Steep.CreateCollection {
		open = collection.OpenExisting
	}
	col, err := open(cfg.Steep.Collection)
	if err != nil {
		return nil, err
	}
	reg, err := commands.New(cfg.Keys)
	if err != nil {
		return nil, fmt.Errorf("invalid config:\n%w", err)
	}
	cwd, err := os.Getwd()
	if err != nil {
		return nil, err
	}
	m := &Model{
		cfg:            cfg,
		cwd:            cwd,
		col:            col,
		reg:            reg,
		tree:           tree.New(),
		editor:         editor.New(),
		results:        results.New(cfg.Steep.Filters, loadHistory()),
		response:       response.New(cfg.View.Wrap, cfg.View.PrettyPrint, cfg.Steep.Pipes),
		showCollection: cfg.Layout.Collection == "shown",
		focus:          paneEditor,
	}
	m.notes.Max = 100
	if cfg.Layout.Focus == "collection" {
		m.focus = paneCollection
	}
	if err := reg.Check(m.allScopes()); err != nil {
		return nil, fmt.Errorf("invalid config:\n%w", err)
	}
	_ = col.PruneRuns()
	m.reloadEntries()
	if ch, stop, err := col.Watch(150 * time.Millisecond); err == nil {
		m.watch, m.stopWatch = ch, stop
	}
	if l, err := remote.Listen(col.Root); err == nil {
		m.remote = l
	}
	return m, nil
}

type changedMsg struct{}

func (m *Model) waitChange() tea.Cmd {
	if m.watch == nil {
		return nil
	}
	ch := m.watch
	return func() tea.Msg {
		if _, ok := <-ch; !ok {
			return nil
		}
		return changedMsg{}
	}
}

type orderMsg struct{ remote.Order }

func (m *Model) waitOrder() tea.Cmd {
	if m.remote == nil {
		return nil
	}
	ch := m.remote.Orders()
	return func() tea.Msg {
		o, ok := <-ch
		if !ok {
			return nil
		}
		return orderMsg{o}
	}
}

// focusOrder opens the request another tool asks for: spilltea, which just
// wrote it. Inside tmux, steep's pane comes to the front too.
func (m *Model) focusOrder(name string) tea.Cmd {
	m.reloadEntries()
	return tea.Batch(m.open(name), raisePane)
}

// raisePane selects steep's tmux pane, and its window.
func raisePane() tea.Msg {
	pane := os.Getenv("TMUX_PANE")
	if pane == "" || os.Getenv("TMUX") == "" {
		return nil
	}
	_ = exec.Command("tmux", "select-window", "-t", pane, ";", "select-pane", "-t", pane).Run()
	return nil
}

func (m *Model) Init() tea.Cmd {
	var ask tea.Cmd
	if m.pendingImport != "" {
		ask = m.askNew(m.tree.Dir(), false, m.pendingImport)
	}
	cmds := []tea.Cmd{m.waitChange(), m.waitOrder(), ask}
	if m.cfg.Steep.CollectionConfigNotice && len(m.cfg.CollectionCommands) > 0 {
		cmds = append(cmds, notify.Warning("The collection's config.yaml sets "+
			strings.Join(m.cfg.CollectionCommands, ", ")+
			": steep runs these; collectionConfigNotice: false silences this"))
	}
	return tea.Batch(cmds...)
}

// Import asks, at start, for a name to save the request text under.
func (m *Model) Import(text string) { m.pendingImport = text }

// Close releases what the model holds: the watcher, the socket, a run in
// flight.
func (m *Model) Close() {
	if m.stopWatch != nil {
		m.stopWatch()
	}
	if m.remote != nil {
		m.remote.Close()
	}
	if m.fz != nil {
		m.fz.Stop()
	}
}

func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	cmd := m.update(msg)
	return m, tea.Batch(cmd, m.afterUpdate())
}

func (m *Model) update(msg tea.Msg) tea.Cmd {
	if m.overlays.Handle(msg) {
		return nil
	}
	if cmd, ok := m.notes.Update(msg); ok {
		return cmd
	}
	if m.results.Update(msg) || m.response.Update(msg) {
		return nil
	}
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.w, m.h = msg.Width, msg.Height
		m.layout()
	case tea.KeyPressMsg:
		return m.key(msg)
	case tea.PasteMsg:
		return m.paste(msg.Content)
	case tea.MouseMsg:
		return m.mouse(msg)
	case changedMsg:
		return tea.Batch(m.diskChanged(), m.waitChange())
	case orderMsg:
		return tea.Batch(m.focusOrder(msg.Focus), m.waitOrder())
	case tickMsg:
		return m.tick()
	case countMsg:
		if msg.gen == m.countGen {
			m.count = msg
		}
	case pickedMsg:
		return m.picked(msg)
	case runMsg:
		return m.launch()
	case runEndMsg:
		return m.ranEnd(msg)
	case shotMsg:
		return m.shot(msg)
	case deleteMsg:
		return m.delete(msg)
	case forgetCollectionMsg:
		return m.forgetCollection()
	case forgetMsg:
		return m.forget(msg)
	case copiedMsg:
		return m.copied(msg)
	case tree.OpenMsg:
		return m.open(msg.Name)
	case tree.NewMsg:
		return m.askNew(msg.Dir, msg.Folder, "")
	case tree.RenameMsg:
		return m.askRename(msg.Name, msg.Dir)
	case tree.DuplicateMsg:
		return m.askDuplicate(msg.Name)
	case tree.DeleteMsg:
		return m.askDelete(msg.Name, msg.Dir)
	case editor.PickMsg:
		return m.pick(msg.Pos)
	case results.OpenMsg:
		m.setFocus(paneResponse)
	case results.ForgetMsg:
		return m.askForget(msg)
	case results.ForgetCollectionMsg:
		return m.askForgetCollection()
	case results.HistoryMsg:
		return saveHistory(msg.History)
	}
	return nil
}

// afterUpdate keeps what derives from the panes in step: the file on disk,
// the request count, the response shown, the layout.
func (m *Model) afterUpdate() tea.Cmd {
	var cmds []tea.Cmd
	if m.editor.Changed() {
		cmds = append(cmds, m.save(), m.recount())
	}
	if s, ok := m.editor.Yanked(); ok {
		cmds = append(cmds, tea.SetClipboard(s))
	}
	cmds = append(cmds, m.syncResponse())
	m.layout()
	return tea.Batch(cmds...)
}

func (m *Model) key(msg tea.KeyPressMsg) tea.Cmd {
	if !minSize.Fits(m.w, m.h) {
		// nothing to act on but quitting
		if b, ok := m.reg.Match(m.path()[:1], msg); ok && b.ID == "app.quit" {
			return b.Run()
		}
		return nil
	}
	if m.overlays.Top() != nil {
		return m.overlays.Key(m.reg, msg)
	}
	switch m.focus {
	case paneCollection:
		if m.tree.Capturing() && m.tree.Input(msg) {
			return nil
		}
	case paneResults:
		if m.results.Capturing() {
			if ok, cmd := m.results.Input(msg); ok {
				return cmd
			}
		}
	case paneResponse:
		if m.response.Capturing() {
			if ok, cmd := m.response.Input(msg); ok {
				return cmd
			}
		}
	case paneEditor:
		// vim first while it is busy; in normal mode, steep's keys first.
		if m.editor.Busy() && m.editor.Key(msg) {
			return nil
		}
	}
	if b, ok := m.reg.Match(m.path(), msg); ok {
		return b.Run()
	}
	if m.focus == paneEditor {
		m.editor.Key(msg)
	}
	return nil
}

func (m *Model) mouse(msg tea.MouseMsg) tea.Cmd {
	if !minSize.Fits(m.w, m.h) {
		return nil
	}
	if m.overlays.Top() != nil {
		return m.overlays.Mouse(msg)
	}
	mm := msg.Mouse()
	if _, click := msg.(tea.MouseClickMsg); click {
		for _, t := range m.toastRects {
			if t.Contains(mm.X, mm.Y) {
				m.notes.DismissID(t.id)
				return nil
			}
		}
	}
	for p := range paneCount {
		r := m.rects[p]
		if r.Empty() || !r.Contains(mm.X, mm.Y) {
			continue
		}
		wasFocused := m.focus == p
		if _, click := msg.(tea.MouseClickMsg); click {
			m.setFocus(p)
		}
		mm.X, mm.Y = mm.X-r.X-1, mm.Y-r.Y-1
		var local tea.MouseMsg
		switch msg.(type) {
		case tea.MouseClickMsg:
			local = tea.MouseClickMsg(mm)
		case tea.MouseWheelMsg:
			local = tea.MouseWheelMsg(mm)
		default:
			return nil
		}
		switch p {
		case paneCollection:
			return m.tree.Mouse(local, wasFocused)
		case paneEditor:
			m.editor.Mouse(local)
		case paneResults:
			return m.results.Mouse(local, wasFocused)
		case paneResponse:
			m.response.Mouse(local)
		}
		return nil
	}
	return nil
}

// collectionShown tells whether the collection has a place on screen: when
// the user wants it and the window is wide enough, or for a while.
func (m *Model) collectionShown() bool {
	return m.peek || m.showCollection && m.w >= collectionHideBelow
}

// setFocus moves the focus to p, showing the collection for a while if p is
// it and it is hidden.
func (m *Model) setFocus(p pane) {
	if p == paneCollection && !m.collectionShown() {
		m.peek = true
	}
	if p != paneCollection {
		m.peek = false
	}
	m.focus = p
	m.layout()
}
