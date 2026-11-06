package ui

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/sectui/foam/confirm"
	"github.com/sectui/foam/notify"
	"github.com/sectui/foam/overlay"
	"github.com/sectui/foam/prompt"
	"github.com/sectui/foam/vim"

	"github.com/sectui/steep/internal/collection"
	"github.com/sectui/steep/internal/curl"
	"github.com/sectui/steep/internal/request"
	"github.com/sectui/steep/internal/run"
	"github.com/sectui/steep/internal/ui/results"
	"github.com/sectui/steep/internal/wordlist"
)

const newRequest = `GET / HTTP/1.1
Host: example.com

`

func (m *Model) reloadEntries() {
	es, err := m.col.Entries()
	if err != nil {
		m.notes.Update(notify.Msg{Level: notify.ErrorLevel, Text: err.Error()})
		return
	}
	m.tree.SetEntries(es)
}

// diskChanged follows what changed under requests/: the tree, and the open
// request if it was edited elsewhere.
func (m *Model) diskChanged() tea.Cmd {
	m.reloadEntries()
	name := m.editor.Name()
	if name == "" {
		return nil
	}
	text, err := m.col.Read(name)
	if err != nil {
		m.editor.Close()
		m.response.Clear()
		m.results.SetRun("", nil)
		return notify.Warning(name + " is gone")
	}
	if text == m.onDisk {
		return nil // steep's own write, or a change to another file
	}
	m.onDisk = text
	if m.editor.Text() != m.synced {
		// unsaved edits: they are kept, and win at the next save
		return notify.Warning(name + " changed on disk; your unsaved edits are kept")
	}
	m.editor.SetText(text)
	m.editor.Changed() // what is on disk needs no saving
	m.synced = m.editor.Text()
	return tea.Batch(notify.Info(name+" changed on disk"), m.recount())
}

func (m *Model) open(name string) tea.Cmd {
	text, err := m.col.Read(name)
	if err != nil {
		return notify.Err(err)
	}
	m.editor.Open(name, text)
	m.onDisk, m.synced = text, m.editor.Text()
	m.lastURL = ""
	m.tree.SetCurrent(name)
	m.response.Clear()
	m.shownHit = ""
	cmd := m.loadRun()
	m.setFocus(paneEditor)
	return tea.Batch(cmd, m.recount())
}

func (m *Model) loadRun() tea.Cmd {
	id := request.Parse(m.editor.Text()).ID()
	if id == "" {
		return m.results.SetRun("", nil)
	}
	dir := m.col.RunDir(id)
	_, hits, err := run.Load(dir)
	cmd := m.results.SetRun(dir, hits)
	if m.fz != nil && m.runName == m.editor.Name() {
		m.results.SetProgress(m.fz.Progress(), true, m.fz.Paused())
	}
	return tea.Batch(cmd, notify.Err(err))
}

func (m *Model) save() tea.Cmd {
	name := m.editor.Name()
	if name == "" {
		return nil
	}
	text := m.editor.Text()
	if err := m.col.Write(name, text); err != nil {
		return notify.Err(err)
	}
	m.onDisk, m.synced = text, text
	return nil
}

func (m *Model) askNew(dir string, folder bool, text string) tea.Cmd {
	value := ""
	if dir != "" {
		value = dir + "/"
	}
	if !folder {
		if text != "" {
			value = suggest(text)
		}
		if text == "" {
			text = newRequest
		}
		return m.askName(value, text)
	}
	p := prompt.New("New folder", value, func(name string) tea.Cmd {
		if err := m.col.Mkdir(name); err != nil {
			return notify.Err(err)
		}
		m.reloadEntries()
		return nil
	})
	m.validName(p)
	return overlay.Open(p)
}

// askName asks the name to save text under as a new request, then opens it.
func (m *Model) askName(value, text string) tea.Cmd {
	p := prompt.New("New request", value, func(name string) tea.Cmd {
		if err := m.col.Create(name, text); err != nil {
			return notify.Err(err)
		}
		m.reloadEntries()
		return m.open(name)
	})
	m.validName(p)
	return overlay.Open(p)
}

func (m *Model) validName(p *prompt.Model) {
	p.Hint = `"api/login" is login in the folder api`
	p.Validate = func(name string) error {
		if err := collection.ValidName(name); err != nil {
			return err
		}
		if m.col.Exists(name) {
			return fmt.Errorf("%s already exists", name)
		}
		return nil
	}
}

func suggest(text string) string {
	f := request.Parse(text)
	h, err := request.ParseHTTP(f.Body)
	if err != nil {
		return ""
	}
	proto, _ := f.Get("proto")
	if proto == "" {
		proto = "https"
	}
	return request.SuggestName(h, proto)
}

func (m *Model) askRename(name string, dir bool) tea.Cmd {
	p := prompt.New("Rename "+name, name, func(to string) tea.Cmd {
		if to == name {
			return nil
		}
		if err := m.col.Rename(name, dir, to); err != nil {
			return notify.Err(err)
		}
		cur := m.editor.Name()
		switch {
		case cur == name:
			m.editor.Rename(to)
		case dir && strings.HasPrefix(cur, name+"/"):
			m.editor.Rename(to + strings.TrimPrefix(cur, name))
		}
		if m.runName == name {
			m.runName = to
		}
		m.reloadEntries()
		m.tree.SetCurrent(m.editor.Name())
		return nil
	})
	p.Validate = func(to string) error {
		if to == name {
			return nil
		}
		return collection.ValidName(to)
	}
	return overlay.Open(p)
}

// askDuplicate copies a request under a name asked for, and opens the copy.
// The copy loses the id: its runs are its own.
func (m *Model) askDuplicate(name string) tea.Cmd {
	if name == m.editor.Name() {
		if cmd := m.save(); cmd != nil {
			return cmd
		}
	}
	text, err := m.col.Read(name)
	if err != nil {
		return notify.Err(err)
	}
	f := request.Parse(text)
	f.Unset("id")
	suggestion := name + "-copy"
	for i := 2; m.col.Exists(suggestion); i++ {
		suggestion = fmt.Sprintf("%s-copy%d", name, i)
	}
	p := prompt.New("Duplicate "+name, suggestion, func(to string) tea.Cmd {
		if err := m.col.Create(to, f.String()); err != nil {
			return notify.Err(err)
		}
		m.reloadEntries()
		return m.open(to)
	})
	p.Validate = func(to string) error {
		if err := collection.ValidName(to); err != nil {
			return err
		}
		if m.col.Exists(to) {
			return fmt.Errorf("%s already exists", to)
		}
		return nil
	}
	return overlay.Open(p)
}

type deleteMsg struct {
	name string
	dir  bool
}

func (m *Model) askDelete(name string, dir bool) tea.Cmd {
	q := "Delete the request " + name + " and its responses?"
	if dir {
		n := len(m.col.Requests(name, true))
		q = fmt.Sprintf("Delete the folder %s and the %d requests it holds?", name, n)
	}
	c := confirm.New("Delete", q, func() tea.Msg { return deleteMsg{name, dir} })
	c.Danger = true
	return overlay.Open(c)
}

func (m *Model) delete(d deleteMsg) tea.Cmd {
	if m.fz != nil && (m.runName == d.name || d.dir && strings.HasPrefix(m.runName, d.name+"/")) {
		m.fz.Stop() // its responses are about to go
	}
	cur := m.editor.Name()
	if err := m.col.Delete(d.name, d.dir); err != nil {
		return notify.Err(err)
	}
	if cur == d.name || d.dir && strings.HasPrefix(cur, d.name+"/") {
		m.editor.Close()
		m.response.Clear()
		m.results.SetRun("", nil)
	}
	m.reloadEntries()
	return notify.Success(d.name + " deleted")
}

type forgetMsg struct {
	dir string
	n   []int
}

// askForget forgets the selected hit at once, like a mark is set; every hit
// shown, once asked.
func (m *Model) askForget(f results.ForgetMsg) tea.Cmd {
	dir := m.results.Dir()
	if !f.All {
		return m.forget(forgetMsg{dir, f.N})
	}
	what := fmt.Sprintf("the %d hits shown", len(f.N))
	if len(f.N) == 1 {
		what = "the hit shown"
	}
	if !f.Filtered {
		what = fmt.Sprintf("every hit of %s (%d)", m.editor.Name(), len(f.N))
	}
	return overlay.Open(confirm.New("Forget", "Forget "+what+"?", func() tea.Msg { return forgetMsg{dir, f.N} }))
}

func (m *Model) forget(f forgetMsg) tea.Cmd {
	if f.dir == "" || f.dir != m.results.Dir() {
		return nil // another request was opened meanwhile
	}
	if m.fz != nil && m.fz.Dir() == f.dir {
		return notify.Err(errors.New("the run is still going"))
	}
	err := run.Forget(f.dir, f.n)
	m.response.Clear()
	m.shownHit = ""
	// the selection goes to the hit that followed
	cur := m.results.Cursor()
	cmd := m.loadRun()
	m.results.SetCursor(cur)
	switch {
	case err != nil:
		return tea.Batch(cmd, notify.Err(err))
	case len(f.n) == 1:
		return cmd // as quiet as a mark
	}
	return tea.Batch(cmd, notify.Success(fmt.Sprintf("%d hits forgotten", len(f.n))))
}

type forgetCollectionMsg struct{}

func (m *Model) askForgetCollection() tea.Cmd {
	return overlay.Open(confirm.New("Forget", "Forget every response stored in the collection?",
		func() tea.Msg { return forgetCollectionMsg{} }))
}

func (m *Model) forgetCollection() tea.Cmd {
	if m.fz != nil {
		m.fz.Stop()
	}
	if err := m.col.ClearRuns(); err != nil {
		return notify.Err(err)
	}
	m.results.SetRun("", nil)
	m.response.Clear()
	m.shownHit = ""
	return notify.Success("Responses forgotten")
}

// imported reads a pasted curl command or raw request.
func imported(s string) (string, bool) {
	s = strings.TrimSpace(s)
	if curl.LooksLikeCurl(s) {
		text, err := curl.Import(s)
		return text, err == nil
	}
	h, err := request.ParseHTTP(s)
	// with its version, the request line is one whatever the method's case
	if err != nil || !request.IsMethod(h.Method) || !strings.HasPrefix(h.Version, "HTTP/") {
		return "", false
	}
	return s + "\n", true
}

func (m *Model) paste(s string) tea.Cmd {
	if o := m.overlays.Top(); o != nil {
		return o.Update(tea.PasteMsg{Content: s})
	}
	switch {
	case m.focus == paneCollection && m.tree.Capturing():
		m.tree.Paste(s)
		return nil
	case m.focus == paneResults && m.results.Capturing():
		return m.results.Paste(s)
	case m.focus == paneResponse && m.response.Capturing():
		m.response.Paste(s)
		return nil
	case m.focus != paneEditor && m.focus != paneCollection:
		return nil
	}
	text, isRequest := imported(s)
	switch {
	case isRequest && (m.editor.Name() == "" || m.focus == paneCollection):
		return m.askNew(m.tree.Dir(), false, text)
	case isRequest:
		// The request replaces the open one; its `# steep` block stays.
		f := request.Parse(m.editor.Text())
		imp := request.Parse(text)
		for _, l := range imp.Header {
			if k, ok := request.Directive(l); ok && k != "id" {
				_, v, _ := strings.Cut(l, ":")
				f.Set(k, strings.TrimSpace(v))
			}
		}
		f.Body = imp.Body
		m.editor.SetText(f.String())
		return notify.Info("Request imported")
	case m.focus == paneEditor:
		m.editor.Paste(s)
	}
	return nil
}

type pickedMsg struct {
	pos  vim.Pos
	path string
	err  error
}

// pick runs the file picker for the file{} block at p, from where the path
// typed in it leads.
func (m *Model) pick(p vim.Pos) tea.Cmd {
	typed, _ := m.editor.BlockAt(p)
	start, prefix, err := wordlist.Browse(typed, m.cfg.Steep.Shortcuts, m.cwd, m.cfg.Steep.PickerDir)
	if err != nil {
		return notify.Err(err)
	}
	cmdline := m.cfg.Steep.FilePicker
	var out bytes.Buffer
	var chooser string
	if strings.Contains(cmdline, "{file}") {
		f, err := os.CreateTemp("", "steep-pick-*")
		if err != nil {
			return notify.Err(err)
		}
		f.Close()
		chooser = f.Name()
		cmdline = strings.ReplaceAll(cmdline, "{file}", "'"+strings.ReplaceAll(chooser, "'", `'\''`)+"'")
	}
	c := exec.Command("sh", "-c", cmdline)
	c.Dir = start
	// fzf with --height draws inline on the main screen, over the shell steep
	// was started from: force it fullscreen, on its alternate screen.
	c.Env = append(os.Environ(), "FZF_DEFAULT_OPTS="+os.Getenv("FZF_DEFAULT_OPTS")+" --no-height")
	if chooser == "" {
		c.Stdout = &out
	}
	return tea.ExecProcess(c, func(err error) tea.Msg {
		picked := out.String()
		if chooser != "" {
			b, _ := os.ReadFile(chooser)
			os.Remove(chooser)
			picked = string(b)
		}
		picked, _, _ = strings.Cut(picked, "\n")
		picked = strings.TrimSpace(picked)
		if picked != "" {
			picked = wordlist.Join(prefix, picked)
		}
		return pickedMsg{p, picked, err}
	})
}

func (m *Model) picked(p pickedMsg) tea.Cmd {
	if p.path == "" {
		if p.err != nil {
			return notify.Warning("file picker: " + p.err.Error())
		}
		return nil
	}
	if home, err := os.UserHomeDir(); err == nil && strings.HasPrefix(p.path, home+"/") {
		p.path = "~" + strings.TrimPrefix(p.path, home)
	}
	m.editor.SetBlock(p.pos, p.path)
	return nil
}

func (m *Model) syncResponse() tea.Cmd {
	hit, ok := m.results.Selected()
	if !ok {
		if m.shownHit != "" {
			m.response.Clear()
			m.shownHit = ""
		}
		return nil
	}
	key := m.results.Dir() + "#" + strconv.Itoa(hit.N)
	if key == m.shownHit {
		return nil
	}
	m.shownHit = key
	b, err := run.ReadResponse(m.results.Dir(), hit.N)
	if err != nil {
		m.response.Clear()
		return notify.Err(err)
	}
	return m.response.Show(b, results.InputComments(hit))
}
