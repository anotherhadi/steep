package ui

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/sectui/steep/internal/ui/response"
)

// A response of megabytes on one line (minified JS) stays fluid.
func TestBigResponse(t *testing.T) {
	for _, wrap := range []bool{true, false} {
		r := response.New(wrap, true, nil)
		body := strings.Repeat(`var a=function(b){return b+1};`, 5<<20/30)
		r.Show([]byte("HTTP/1.1 200 OK\r\nContent-Type: application/javascript\r\n\r\n"+body), nil)
		start := time.Now()
		for range 10 {
			r.View(120, 40, true, "")
		}
		if d := time.Since(start); d > 2*time.Second {
			t.Errorf("wrap=%v: 10 frames took %v", wrap, d)
		}
	}
}

// A request of thousands of lines stays fluid to edit.
func TestBigRequest(t *testing.T) {
	m := newModel(t)
	body := strings.Repeat(`{"key": "value", "n": 1},`+"\n", 20000)
	m.col.Create("big", "POST / HTTP/1.1\nHost: x\n\n"+body)
	m.reloadEntries()
	do(m, m.open("big")())
	start := time.Now()
	for range 20 {
		do(m, tea.KeyPressMsg{Code: 'j', Text: "j"})
		m.View()
	}
	if d := time.Since(start); d > 2*time.Second {
		t.Errorf("20 moves took %v", d)
	}
	start = time.Now()
	typeKeys(m, "ix")
	key(m, tea.KeyEscape, 0)
	m.View()
	if d := time.Since(start); d > time.Second {
		t.Errorf("an edit took %v", d)
	}
}
