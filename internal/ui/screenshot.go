package ui

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
	"unicode"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/adrg/xdg"

	fconfig "github.com/sectui/foam/config"
	"github.com/sectui/foam/draw"
	"github.com/sectui/foam/geom"
	"github.com/sectui/foam/notify"
	"github.com/sectui/foam/shot"
	"github.com/sectui/foam/theme"
)

// shotCanvas draws the request and its response over the whole window, under
// a bar telling where they come from: no collection, results nor help bar.
func (m *Model) shotCanvas() *lipgloss.Canvas {
	top := 1
	if theme.Spacious() {
		top++
	}
	body := geom.Rect{X: 0, Y: top, W: m.w, H: max(m.h-top, 0)}
	cols := body.Columns(geom.Ratio(1), geom.Ratio(1))
	m.editor.SetSize(max(cols[0].W-2, 0), max(cols[0].H-2, 0))
	defer m.layout()

	left := theme.Accent().Style().Bold(true).Render(" "+theme.Icon("󰶞 ", "")+"Captured with steep ") +
		theme.Subtle().Render("· ") + theme.Fg().Style().Bold(true).Render(draw.Clean(m.editor.Name())+" ")
	layers := []*lipgloss.Layer{lipgloss.NewLayer(m.bar(left, ""))}
	for i, p := range []pane{paneEditor, paneResponse} {
		r := cols[i]
		layers = append(layers, lipgloss.NewLayer(m.paneView(p, r, true)).X(r.X).Y(r.Y))
	}
	c := lipgloss.NewCanvas(m.w, m.h)
	c.Compose(lipgloss.NewCompositor(layers...))
	draw.FillRow(c, 0, theme.Surface())
	return c
}

type shotMsg struct {
	path    string
	err     error
	hookErr error
	hookOut string
}

// screenshot saves the shot of the open request, then runs
// screenshot.onCapture, in the background.
func (m *Model) screenshot() tea.Cmd {
	cfg := m.cfg.Screenshot
	c := m.shotCanvas()
	dir := m.shotDir()
	name := shotName(m.editor.Name(), time.Now(), cfg.Format)
	o := shot.Options{
		Font: fconfig.ExpandHome(cfg.Font), Size: cfg.FontSize, Scale: cfg.Scale,
		Fg: theme.Fg().Value(), Bg: theme.Bg().Value(),
	}
	cwd := m.cwd
	return func() tea.Msg {
		var buf bytes.Buffer
		var err error
		if cfg.Format == "svg" {
			err = shot.SVG(&buf, c, o)
		} else {
			err = shot.PNG(&buf, c, o)
		}
		if err != nil {
			return shotMsg{err: err}
		}
		path, err := writeNew(dir, name, buf.Bytes())
		if err != nil || cfg.OnCapture == "" {
			return shotMsg{path: path, err: err}
		}
		cmd := exec.Command("sh", "-c", cfg.OnCapture)
		cmd.Dir = cwd
		cmd.Env = append(os.Environ(), "SCREENSHOT="+path)
		out, err := cmd.CombinedOutput()
		return shotMsg{path: path, hookErr: err, hookOut: string(out)}
	}
}

func (m *Model) shot(msg shotMsg) tea.Cmd {
	if msg.err != nil {
		return notify.Error("screenshot: " + msg.err.Error())
	}
	cmds := []tea.Cmd{notify.Success("Screenshot saved: " + tildeHome(msg.path))}
	if msg.hookErr != nil {
		text := "screenshot.onCapture: " + msg.hookErr.Error()
		if out := strings.TrimSpace(msg.hookOut); out != "" {
			lines := strings.Split(out, "\n")
			text += ": " + lines[len(lines)-1]
		}
		cmds = append(cmds, notify.Warning(text))
	}
	return tea.Batch(cmds...)
}

// shotDir is screenshot.dir resolved: relative to the collection, and
// $XDG_PICTURES_DIR/screenshots when empty.
func (m *Model) shotDir() string {
	d := m.cfg.Screenshot.Dir
	if d == "" {
		return filepath.Join(xdg.UserDirs.Pictures, "screenshots")
	}
	d = fconfig.ExpandHome(os.ExpandEnv(d))
	if !filepath.IsAbs(d) {
		d = filepath.Join(m.cfg.Steep.Collection, d)
	}
	return d
}

// shotName is steep_<request>_<date>_<time>.<ext>, the request's folders
// and spaces made dashes.
func shotName(request string, t time.Time, format string) string {
	clean := strings.Map(func(r rune) rune {
		if r == '/' || r == '\\' || unicode.IsSpace(r) || unicode.IsControl(r) {
			return '-'
		}
		return r
	}, request)
	return fmt.Sprintf("steep_%s_%s.%s", clean, t.Format("2006-01-02_15-04-05"), format)
}

// writeNew writes data to name in dir, made if need be, adding -2, -3 to
// the name rather than replacing a file.
func writeNew(dir, name string, data []byte) (string, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	ext := filepath.Ext(name)
	base := strings.TrimSuffix(name, ext)
	for i := 1; ; i++ {
		path := filepath.Join(dir, name)
		if i > 1 {
			path = filepath.Join(dir, fmt.Sprintf("%s-%d%s", base, i, ext))
		}
		f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
		if errors.Is(err, fs.ErrExist) && i < 100 {
			continue
		}
		if err != nil {
			return "", err
		}
		_, err = f.Write(data)
		if cerr := f.Close(); err == nil {
			err = cerr
		}
		return path, err
	}
}

func tildeHome(path string) string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return path
	}
	if rest, ok := strings.CutPrefix(path, home+string(os.PathSeparator)); ok {
		return "~/" + rest
	}
	return path
}
