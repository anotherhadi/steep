package ui

import (
	"fmt"
	"net/url"
	"path/filepath"
	"slices"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/anotherhadi/foam/commands"
	"github.com/anotherhadi/foam/confirm"
	"github.com/anotherhadi/foam/keys"
	"github.com/anotherhadi/foam/notify"
	"github.com/anotherhadi/foam/overlay"

	"github.com/anotherhadi/steep/internal/curl"
	"github.com/anotherhadi/steep/internal/request"
	"github.com/anotherhadi/steep/internal/run"
	"github.com/anotherhadi/steep/internal/ui/copypicker"
	"github.com/anotherhadi/steep/internal/ui/response"
	"github.com/anotherhadi/steep/internal/ui/results"
)

// copyAskAbove is the size of a copy asked about first: terminals and tmux
// often cut what goes to the clipboard.
const copyAskAbove = 1 << 20

type copiedMsg struct {
	text string
	what string // "hit #12's request as curl", "18 URLs"
	err  error
}

func (m *Model) askCopy(all bool) tea.Cmd {
	hits := m.results.Shown()
	title := fmt.Sprintf("Copy %d hits", len(hits))
	if !all {
		hit, ok := m.results.Selected()
		if !ok {
			return nil
		}
		hits, title = []run.Hit{hit}, fmt.Sprintf("Copy hit #%d", hit.N)
	}
	if len(hits) == 0 {
		return nil
	}
	if len(hits) == 1 {
		all = false
	}
	dir, pretty := m.results.Dir(), m.response.Pretty()
	keysOf := func(id string) keys.List { return m.reg.KeysOf(commands.Command{ID: id}) }
	return overlay.Open(copypicker.New(title, keysOf, func(k copypicker.Kind) tea.Cmd {
		return func() tea.Msg {
			text, err := copyText(dir, hits, k, pretty)
			what := copyWhat(k, hits, all)
			return copiedMsg{text, what, err}
		}
	}))
}

func (m *Model) copied(c copiedMsg) tea.Cmd {
	if c.err != nil {
		return notify.Err(c.err)
	}
	done := tea.Batch(tea.SetClipboard(c.text), notify.Success("Copied "+c.what))
	if len(c.text) > copyAskAbove {
		q := fmt.Sprintf("Copy %s (%.1f MB)? Terminals and tmux may cut a copy that big.", c.what, float64(len(c.text))/(1<<20))
		return overlay.Open(confirm.New("Copy", q, done))
	}
	return done
}

// copyText is k of each hit, one per line for URLs and inputs, otherwise
// under a comment naming the hit when there are more than one.
func copyText(dir string, hits []run.Hit, k copypicker.Kind, pretty bool) (string, error) {
	parts := make([]string, 0, len(hits))
	for _, h := range hits {
		s, err := copyOne(dir, h, k, pretty)
		if err != nil {
			return "", err
		}
		if len(hits) > 1 && k != copypicker.URL && k != copypicker.Input {
			s = fmt.Sprintf("# #%d · %d · %s\n%s", h.N, h.Status, results.Inputs(h), s)
		}
		parts = append(parts, s)
	}
	sep := "\n\n"
	if k == copypicker.URL || k == copypicker.Input {
		sep = "\n"
	}
	return strings.Join(parts, sep), nil
}

func copyOne(dir string, h run.Hit, k copypicker.Kind, pretty bool) (string, error) {
	switch k {
	case copypicker.URL:
		return h.URL, nil
	case copypicker.Input:
		return results.Inputs(h), nil
	case copypicker.Curl, copypicker.HTTP:
		b, err := run.ReadRequest(dir, h.N)
		if err != nil {
			return "", err
		}
		if k == copypicker.HTTP {
			return string(b), nil
		}
		r, err := request.ParseHTTP(string(b))
		if err != nil {
			return "", fmt.Errorf("hit #%d: %w", h.N, err)
		}
		return curl.Format(r, h.URL), nil
	}
	b, err := run.ReadResponse(dir, h.N)
	if err != nil {
		return "", err
	}
	head, body, hasBody := response.Parts(b, pretty)
	switch {
	case k == copypicker.Headers:
		return head, nil
	case k == copypicker.Body:
		return body, nil
	case hasBody:
		return head + "\n\n" + body, nil
	}
	return head, nil
}

func copyWhat(k copypicker.Kind, hits []run.Hit, all bool) string {
	names := map[copypicker.Kind][2]string{ // one, many
		copypicker.Curl:     {"request as curl", "requests as curl"},
		copypicker.HTTP:     {"request as raw HTTP", "requests as raw HTTP"},
		copypicker.Response: {"response", "responses"},
		copypicker.Body:     {"response body", "response bodies"},
		copypicker.Headers:  {"response headers", "response headers"},
		copypicker.URL:      {"URL", "URLs"},
		copypicker.Input:    {"input", "inputs"},
	}[k]
	if !all {
		return fmt.Sprintf("hit #%d's %s", hits[0].N, names[0])
	}
	return fmt.Sprintf("%d %s", len(hits), names[1])
}

func (m *Model) copyPath(dir bool) tea.Cmd {
	hit, ok := m.results.Selected()
	if !ok {
		return nil
	}
	p, what := run.ResponsePath(m.results.Dir(), hit.N), fmt.Sprintf("the path of hit #%d's response", hit.N)
	if dir {
		p, what = m.results.Dir(), "the run's directory"
	}
	if abs, err := filepath.Abs(p); err == nil {
		p = abs
	}
	return tea.Batch(tea.SetClipboard(p), notify.Success("Copied "+what))
}

func (m *Model) copyCollectionPath() tea.Cmd {
	p := m.col.Root
	if abs, err := filepath.Abs(p); err == nil {
		p = abs
	}
	return tea.Batch(tea.SetClipboard(p), notify.Success("Copied the collection's path"))
}

// editHit asks a name for the hit's request, as sent, and saves it as a new
// request with the open request's settings.
func (m *Model) editHit() tea.Cmd {
	hit, ok := m.results.Selected()
	if !ok {
		return nil
	}
	b, err := run.ReadRequest(m.results.Dir(), hit.N)
	if err != nil {
		return notify.Err(err)
	}
	open := request.Parse(m.editor.Text())
	f := request.File{Header: slices.Clone(open.Header)}
	f.Unset("id")
	if u, err := url.Parse(hit.URL); err == nil && u.Scheme != open.Effective(m.cfg.Request.Values())["proto"] {
		f.Set("proto", u.Scheme)
	}
	f.Body = request.EscapeBlocks(strings.ReplaceAll(string(b), "\r\n", "\n"))
	return m.askName(fmt.Sprintf("%s-%d", m.editor.Name(), hit.N), f.String())
}
