// Package request reads and writes steep's request files: a block of
// `# steep key: value` directives, then a raw HTTP request.
//
//	# steep id: 3f9a1c
//	# steep match.status: 200-299,403
//	GET /file{$w/common.txt} HTTP/1.1
//	Host: example.com
package request

import (
	"crypto/rand"
	"encoding/hex"
	"regexp"
	"strings"
)

// File is a request file. Header holds its leading comment lines, the
// directives among them; any other comment is kept as it is.
type File struct {
	Header []string
	Body   string
}

var directiveRe = regexp.MustCompile(`^#\s*steep\s+([A-Za-z0-9._-]+)\s*:\s?(.*)$`)

func Parse(text string) File {
	var f File
	rest := text
	for rest != "" {
		line, after, found := strings.Cut(rest, "\n")
		if !strings.HasPrefix(line, "#") {
			break
		}
		f.Header = append(f.Header, strings.TrimSuffix(line, "\r"))
		rest = after
		if !found {
			rest = ""
		}
	}
	f.Body = rest
	return f
}

func (f File) String() string {
	if len(f.Header) == 0 {
		return f.Body
	}
	return strings.Join(f.Header, "\n") + "\n" + f.Body
}

func parseDirective(line string) (key, value string, ok bool) {
	m := directiveRe.FindStringSubmatch(line)
	if m == nil {
		return "", "", false
	}
	return m[1], strings.TrimSpace(m[2]), true
}

// Get returns the value of the last directive named key.
func (f File) Get(key string) (string, bool) {
	all := f.All(key)
	if len(all) == 0 {
		return "", false
	}
	return all[len(all)-1], true
}

func (f File) All(key string) []string {
	var out []string
	for _, l := range f.Header {
		if k, v, ok := parseDirective(l); ok && k == key {
			out = append(out, v)
		}
	}
	return out
}

// Set gives key a single value: the first directive is rewritten in place, the
// others dropped; without one, the directive is appended to the block.
func (f *File) Set(key, value string) {
	line := "# steep " + key + ": " + value
	done := false
	out := f.Header[:0:0]
	for _, l := range f.Header {
		if k, _, ok := parseDirective(l); ok && k == key {
			if !done {
				out = append(out, line)
				done = true
			}
			continue
		}
		out = append(out, l)
	}
	if !done {
		out = append(out, line)
	}
	f.Header = out
}

func (f *File) Add(key, value string) {
	f.Header = append(f.Header, "# steep "+key+": "+value)
}

func (f *File) Unset(key string) {
	out := f.Header[:0:0]
	for _, l := range f.Header {
		if k, _, ok := parseDirective(l); ok && k == key {
			continue
		}
		out = append(out, l)
	}
	f.Header = out
}

var idRe = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)

// ID is the request's id, which names the directory its runs are kept in.
// One that could lead out of it ("..", "a/b") counts as none.
func (f File) ID() string {
	if id, _ := f.Get("id"); idRe.MatchString(id) {
		return id
	}
	return ""
}

// EnsureID gives the file an id if it has none, and returns it.
func (f *File) EnsureID() string {
	if id := f.ID(); id != "" {
		return id
	}
	f.Unset("id")
	b := make([]byte, 6)
	_, _ = rand.Read(b)
	id := hex.EncodeToString(b)
	f.Header = append([]string{"# steep id: " + id}, f.Header...)
	return id
}

// NewID replaces the file's id by a fresh one, and returns it.
func (f *File) NewID() string {
	f.Unset("id")
	return f.EnsureID()
}

func Directive(line string) (key string, ok bool) {
	key, _, ok = parseDirective(line)
	return key, ok
}
