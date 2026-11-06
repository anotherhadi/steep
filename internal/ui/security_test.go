package ui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/sectui/foam/notify"

	"github.com/sectui/steep/internal/request"
	"github.com/sectui/steep/internal/run"
)

// No byte the outside world controls may reach the terminal as a control.
func TestNoControlReachesTerminal(t *testing.T) {
	m := newModel(t)
	evil := "x\x1b]0;pwned\x07\x1b[2J\u009b31m\xff\x00y"
	m.col.Create("ok", "# steep id: e1\n# steep match.size: "+evil+"\nGET /"+evil+" HTTP/1.1\nHost: h\n\n"+evil)
	m.col.Create("bad\x1b[31mname", "GET / HTTP/1.1\nHost: h\n\n")
	store, _ := run.Create(m.col.RunDir("e1"))
	for i := range 2 {
		store.Add(run.Hit{Status: 200, Input: map[string]string{request.Keyword(1): evil}, URL: "http://h/" + evil},
			[]byte("GET / HTTP/1.1\r\n\r\n"), []byte("HTTP/1.1 200 OK\r\nX: "+evil+"\r\n\r\n"+evil+strings.Repeat("a", i)))
	}
	store.Close(run.Meta{})
	m.reloadEntries()
	do(m, m.open("ok")())
	m.Update(notify.Msg{Text: "server said " + evil})
	key(m, 'o', tea.ModCtrl)
	typeKeys(m, "jjjjjjjjjjjjj")
	for _, s := range []string{m.View().Content} {
		clean := stripSGR(s)
		for _, bad := range []string{"\x1b]", "\x1b[2J", "\u009b", "\xff", "\x00", "\x07"} {
			if strings.Contains(clean, bad) {
				i := strings.Index(clean, bad)
				t.Errorf("%q reaches the terminal: %q", bad, clean[max(i-30, 0):min(i+30, len(clean))])
			}
		}
	}
}

// stripSGR drops steep's own color sequences, keeping anything else.
func stripSGR(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == 0x1b && i+1 < len(s) && s[i+1] == '[' {
			j := i + 2
			for j < len(s) && (s[j] >= '0' && s[j] <= '9' || s[j] == ';' || s[j] == ':') {
				j++
			}
			if j < len(s) && s[j] == 'm' {
				i = j
				continue
			}
		}
		b.WriteByte(s[i])
	}
	return b.String()
}
