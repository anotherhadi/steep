// Package highlight colors HTTP messages: a request file (its `# steep`
// block included) or a response. Styling never changes the width of a line.
package highlight

import (
	"strings"
	"unicode"
	"unicode/utf8"

	"charm.land/lipgloss/v2"

	"github.com/sectui/foam/theme"
	"github.com/sectui/operations/template"

	"github.com/sectui/steep/internal/request"
)

type role int

const (
	plain role = iota
	comment
	directiveKey
	method
	target
	version
	status
	headerName
	punct
	str
	key
	number
	literal
	tag
	keyword
	block
	path
	opName
	opArgs
	opBrace
)

func style(r role, code int) lipgloss.Style {
	switch r {
	case comment:
		return theme.Subtle().Style()
	case directiveKey:
		return theme.Accent().Style()
	case target:
		return theme.Fg().Style().Bold(true)
	case version, punct:
		return theme.Subtle().Style()
	case status:
		return StatusColor(code).Style().Bold(true)
	case headerName:
		return theme.Primary().Style()
	case str:
		return theme.Success().Style()
	case key:
		return theme.Primary().Style()
	case number:
		return theme.Orange().Style()
	case literal:
		return theme.Accent().Style()
	case tag, path:
		return theme.Info().Style()
	case block:
		return theme.Orange().Style().Bold(true)
	case opName, opBrace:
		return theme.Accent().Style().Bold(true)
	case opArgs:
		return theme.Accent().Style()
	case keyword:
		return lipgloss.NewStyle().Foreground(theme.Bg().Value()).Background(theme.Orange().Value()).Bold(true)
	}
	return theme.Fg().Style()
}

func MethodColor(m string) theme.Color {
	switch m {
	case "GET":
		return theme.Success()
	case "POST":
		return theme.Warning()
	case "PUT":
		return theme.Orange()
	case "DELETE":
		return theme.Danger()
	case "PATCH":
		return theme.Accent()
	}
	return theme.Info()
}

// PwnFoxColor is the theme's color for a PwnFox container color; ok is false
// for a color PwnFox does not have.
func PwnFoxColor(c string) (color theme.Color, ok bool) {
	switch c {
	case "blue":
		return theme.Primary(), true
	case "turquoise":
		return theme.Info(), true
	case "green":
		return theme.Success(), true
	case "yellow":
		return theme.Warning(), true
	case "orange":
		return theme.Orange(), true
	case "red":
		return theme.Danger(), true
	case "pink":
		// base24's bright magenta; a base16 palette has none
		if c := theme.Current().Colors.Base17; c != "" {
			return c, true
		}
		return theme.Accent(), true
	case "purple":
		return theme.Accent(), true
	}
	return "", false
}

// PwnFoxIcon is the mark of a request's PwnFox color, one cell wide.
func PwnFoxIcon() string { return theme.Icon("\uf111", "●") }

// PwnFoxDot is the mark of a request's PwnFox color in its color, "" without
// one.
func PwnFoxDot(c string) string {
	color, ok := PwnFoxColor(c)
	if !ok {
		return ""
	}
	return color.Render(PwnFoxIcon())
}

func StatusColor(code int) theme.Color {
	switch {
	case code >= 500:
		return theme.Danger()
	case code >= 400:
		return theme.Warning()
	case code >= 300:
		return theme.Info()
	case code >= 200:
		return theme.Success()
	}
	return theme.Subtle()
}

type Options struct {
	// Keywords are marked wherever they appear outside comments.
	Keywords []string
	// Blocks marks steep's blocks: wordlists and operations.
	Blocks bool
}

// blocks colors the blocks of the line s. A block going on below is closed
// for the parse, so that its head is colored all the same.
func blocks(l *line, s string) {
	var nodes []template.Node
	for closing := ""; len(closing) <= 8; closing += "}" {
		var err error
		if nodes, err = request.Engine.Parse(s + closing); err == nil {
			break
		}
	}
	at := func(i int) int { return runeIndex(s, min(i, len(s))) }
	var walk func([]template.Node)
	walk = func(nodes []template.Node) {
		for _, n := range nodes {
			b, ok := n.(*template.Block)
			if !ok {
				continue
			}
			if request.IsWordlist(b.Op) {
				l.set(at(b.Pos.Start), at(b.Pos.End), block)
				continue
			}
			name := b.Head.Start + len(b.Op.ID)
			l.set(at(b.Head.Start), at(name), opName)
			l.set(at(name), at(b.Head.End), opArgs)
			// the "|" to the next operation, or the "{"
			l.set(at(b.Head.End), at(b.Head.End+1), opBrace)
			l.set(at(b.Pos.End-1), at(b.Pos.End), opBrace)
			walk(b.Children)
		}
	}
	walk(nodes)
}

type line struct {
	text  []rune
	roles []role
	code  int
	meth  string
}

func (l *line) set(start, end int, r role) {
	for i := max(start, 0); i < end && i < len(l.roles); i++ {
		l.roles[i] = r
	}
}

// Message colors the lines of a request file or a response.
func Message(lines []string, o Options) []string { return (&Cache{}).Message(lines, o) }

// Cache keeps the colored lines of past calls: a line in the same place of
// the message (comments, headers, body of a kind) is not colored twice. It
// is for one caller with the same Options each time.
type Cache struct {
	lines map[cacheKey]string
}

type cacheKey struct {
	state, body int
	line        string
}

const cacheMax = 100_000

func (c *Cache) Message(lines []string, o Options) []string {
	if c.lines == nil || len(c.lines) > cacheMax {
		c.lines = make(map[cacheKey]string)
	}
	out := make([]string, len(lines))
	state := 0 // 0: comments, 1: first line, 2: headers, 3: body
	body := bodyText
	for i, s := range lines {
		trimmed := strings.TrimSpace(s)
		isComment := state == 0 && strings.HasPrefix(s, "#")
		k := cacheKey{state, body, s}
		if isComment {
			k.state = -1
		}
		styled, hit := c.lines[k]
		l := &line{text: []rune(s)}
		if !hit {
			l.roles = make([]role, len(l.text))
		}
		switch {
		case isComment:
			if !hit {
				l.set(0, len(l.text), comment)
				directive(l)
			}
		case state <= 1 && trimmed == "":
		case state <= 1:
			if !hit {
				firstLine(l)
			}
			state = 2
		case state == 2 && trimmed == "":
			state = 3
		case state == 2:
			if !hit {
				header(l)
			}
			if name, value, ok := strings.Cut(s, ":"); ok && strings.EqualFold(strings.TrimSpace(name), "content-type") {
				body = bodyKind(value)
			}
		default:
			if body == bodyText && (strings.HasPrefix(trimmed, "{") || strings.HasPrefix(trimmed, "[")) {
				body = bodyJSON
				k.body = body
				styled, hit = c.lines[k]
				if !hit {
					l.roles = make([]role, len(l.text))
				}
			}
			if !hit {
				switch body {
				case bodyJSON:
					json(l)
				case bodyMarkup:
					markup(l)
				}
			}
		}
		if hit {
			out[i] = styled
			continue
		}
		if !isComment {
			keywords(l, o.Keywords)
			if o.Blocks {
				blocks(l, s)
			}
		}
		out[i] = l.render()
		c.lines[k] = out[i]
	}
	return out
}

func (l *line) render() string {
	var b strings.Builder
	for i := 0; i < len(l.text); {
		j := i
		for j < len(l.text) && l.roles[j] == l.roles[i] {
			j++
		}
		st := style(l.roles[i], l.code)
		if l.roles[i] == method {
			st = MethodColor(l.meth).Style().Bold(true)
		}
		b.WriteString(st.Render(string(l.text[i:j])))
		i = j
	}
	return b.String()
}

// URL colors a URL: its scheme, host, path, query and blocks.
func URL(u string) string {
	l := &line{text: []rune(u)}
	l.roles = make([]role, len(l.text))
	r := l.text
	i := 0
	if k := strings.Index(u, "://"); k >= 0 {
		i = runeIndex(u, k+3)
		l.set(0, i, punct)
	}
	j := i
	for j < len(r) && r[j] != '/' && r[j] != '?' {
		j++
	}
	l.set(i, j, target)
	k := j
	for k < len(r) && r[k] != '?' {
		k++
	}
	l.set(j, k, path)
	if k < len(r) {
		query(l, k)
	}
	blocks(l, u)
	return l.render()
}

// query colors l's query string, from its "?" at i: keys, values and
// separators.
func query(l *line, i int) {
	r := l.text
	inValue := false
	for ; i < len(r); i++ {
		switch r[i] {
		case '?', '&':
			l.set(i, i+1, punct)
			inValue = false
		case '=':
			if !inValue {
				l.set(i, i+1, punct)
				inValue = true
				continue
			}
			l.set(i, i+1, str)
		default:
			if inValue {
				l.set(i, i+1, str)
			} else {
				l.set(i, i+1, key)
			}
		}
	}
}

func directive(l *line) {
	s := string(l.text)
	if !strings.HasPrefix(strings.TrimLeft(s[1:], " "), "steep ") {
		return
	}
	k := strings.Index(s, "steep ") + len("steep ")
	colon := strings.Index(s[k:], ":")
	if colon < 0 {
		return
	}
	l.set(runeIndex(s, k), runeIndex(s, k+colon), directiveKey)
	l.set(runeIndex(s, k+colon+1), len(l.text), plain)
}

func runeIndex(s string, byteIdx int) int { return utf8.RuneCountInString(s[:byteIdx]) }

func firstLine(l *line) {
	f := fields(l.text)
	if len(f) == 0 {
		return
	}
	first := string(l.text[f[0][0]:f[0][1]])
	if strings.HasPrefix(first, "HTTP/") {
		l.set(f[0][0], f[0][1], version)
		if len(f) > 1 {
			code := 0
			for _, r := range l.text[f[1][0]:f[1][1]] {
				if r < '0' || r > '9' || code > 999 {
					code = 0 // not a status code: no color of one
					break
				}
				code = code*10 + int(r-'0')
			}
			l.code = code
			l.set(f[1][0], len(l.text), status)
		}
		return
	}
	l.meth = first
	l.set(f[0][0], f[0][1], method)
	if len(f) > 1 {
		l.set(f[1][0], f[1][1], target)
	}
	if len(f) > 2 {
		l.set(f[2][0], len(l.text), version)
	}
}

func fields(r []rune) [][2]int {
	var out [][2]int
	for i := 0; i < len(r); {
		for i < len(r) && r[i] == ' ' {
			i++
		}
		j := i
		for j < len(r) && r[j] != ' ' {
			j++
		}
		if j > i {
			out = append(out, [2]int{i, j})
		}
		i = j
	}
	return out
}

func header(l *line) {
	for i, r := range l.text {
		if r == ':' {
			l.set(0, i, headerName)
			l.set(i, i+1, punct)
			return
		}
	}
}

const (
	bodyText = iota
	bodyJSON
	bodyMarkup
)

func bodyKind(contentType string) int {
	ct := strings.ToLower(contentType)
	switch {
	case strings.Contains(ct, "json"):
		return bodyJSON
	case strings.Contains(ct, "html"), strings.Contains(ct, "xml"):
		return bodyMarkup
	}
	return bodyText
}

func json(l *line) {
	r := l.text
	for i := 0; i < len(r); {
		c := r[i]
		switch {
		case c == '"':
			j := i + 1
			for j < len(r) && r[j] != '"' {
				if r[j] == '\\' {
					j++
				}
				j++
			}
			j = min(j+1, len(r))
			k := j
			for k < len(r) && r[k] == ' ' {
				k++
			}
			if k < len(r) && r[k] == ':' {
				l.set(i, j, key)
			} else {
				l.set(i, j, str)
			}
			i = j
		case c == '-' || unicode.IsDigit(c):
			j := i + 1
			for j < len(r) && (unicode.IsDigit(r[j]) || strings.ContainsRune(".eE+-", r[j])) {
				j++
			}
			l.set(i, j, number)
			i = j
		case unicode.IsLetter(c):
			j := i
			for j < len(r) && unicode.IsLetter(r[j]) {
				j++
			}
			if w := string(r[i:j]); w == "true" || w == "false" || w == "null" {
				l.set(i, j, literal)
			}
			i = j
		case strings.ContainsRune("{}[],:", c):
			l.set(i, i+1, punct)
			i++
		default:
			i++
		}
	}
}

func markup(l *line) {
	r := l.text
	for i := 0; i < len(r); i++ {
		if r[i] != '<' {
			continue
		}
		j := i + 1
		for j < len(r) && r[j] != '>' {
			j++
		}
		end := min(j+1, len(r))
		l.set(i, end, punct)
		k := i + 1
		for k < end && (r[k] == '/' || r[k] == '!' || r[k] == '?') {
			k++
		}
		n := k
		for n < end && !unicode.IsSpace(r[n]) && r[n] != '>' && r[n] != '/' {
			n++
		}
		l.set(k, n, tag)
		// attribute values
		for a := n; a < end; a++ {
			if r[a] == '"' {
				b := a + 1
				for b < end && r[b] != '"' {
					b++
				}
				l.set(a, min(b+1, end), str)
				a = b
			}
		}
		i = j
	}
}

func keywords(l *line, kws []string) {
	if len(kws) == 0 {
		return
	}
	s := string(l.text)
	for _, kw := range kws {
		if kw == "" {
			continue
		}
		for off := 0; ; {
			j := strings.Index(s[off:], kw)
			if j < 0 {
				break
			}
			start := runeIndex(s, off+j)
			l.set(start, start+utf8.RuneCountInString(kw), keyword)
			off += j + len(kw)
		}
	}
}
