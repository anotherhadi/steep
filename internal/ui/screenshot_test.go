package ui

import (
	"fmt"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

func TestScreenshotName(t *testing.T) {
	at := time.Date(2026, 11, 3, 14, 5, 22, 0, time.UTC)
	if got := shotName("api/login form", at, "png"); got != "steep_api-login-form_2026-11-03_14-05-22.png" {
		t.Fatalf("got %q", got)
	}
}

func TestScreenshotCanvas(t *testing.T) {
	m := newModel(t)
	openRequest(t, m, "GET /secret HTTP/1.1\nHost: example.com\n\n")
	c := m.shotCanvas()
	if c.Width() != 140 || c.Height() != 40 {
		t.Fatalf("size %dx%d", c.Width(), c.Height())
	}
	s := ansi.Strip(c.Render())
	lines := strings.Split(s, "\n")
	if !strings.Contains(lines[0], "Captured with steep · t") || !strings.Contains(lines[0], "https://example.com/secret") {
		t.Fatalf("bar: %q", lines[0])
	}
	if strings.Contains(s, "Collection") || strings.Contains(s, "NORMAL") {
		t.Fatalf("collection or vim mode shown:\n%s", s)
	}
	if !strings.Contains(lines[len(lines)-1], "╰") {
		t.Fatalf("the panes do not reach the bottom:\n%s", s)
	}
	// the window's own layout is back
	if !strings.Contains(screen(m), "Collection") {
		t.Fatalf("layout not restored:\n%s", screen(m))
	}
}

func TestScreenshot(t *testing.T) {
	if _, err := exec.LookPath("fc-match"); err != nil {
		t.Skip("no fontconfig")
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"user":"admin","role":"superuser","token":"eyJhbGciOi…"}`)
	}))
	defer srv.Close()
	m := newModel(t)
	m.cfg.Screenshot.Dir = "shots"
	hook := filepath.Join(t.TempDir(), "hook")
	m.cfg.Screenshot.OnCapture = `echo "$SCREENSHOT" > ` + hook
	openRequest(t, m, "# steep proto: http\nGET /api/me HTTP/1.1\nHost: "+strings.TrimPrefix(srv.URL, "http://")+"\n\n")
	runToEnd(t, m)

	for _, format := range []string{"png", "svg"} {
		m.cfg.Screenshot.Format = format
		key(m, 's', tea.ModCtrl)
		b, err := os.ReadFile(hook)
		if err != nil {
			t.Fatalf("%s: %v; notes = %+v", format, err, m.notes.History())
		}
		path := strings.TrimSpace(string(b))
		if filepath.Dir(path) != filepath.Join(m.cfg.Steep.Collection, "shots") || !strings.HasSuffix(path, "."+format) {
			t.Fatalf("saved at %s", path)
		}
		if format == "png" {
			f, _ := os.Open(path)
			img, err := png.Decode(f)
			f.Close()
			if err != nil {
				t.Fatal(err)
			}
			if b := img.Bounds(); b.Dx()%140 != 0 || b.Dy()%40 != 0 {
				t.Fatalf("not a 140x40 grid: %v", b)
			}
		}
	}
	if h := fmt.Sprint(m.notes.History()); !strings.Contains(h, "Screenshot saved") {
		t.Fatalf("notes = %s", h)
	}
}
