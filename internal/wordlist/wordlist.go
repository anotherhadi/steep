// Package wordlist finds wordlists on disk and counts their lines.
package wordlist

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	fconfig "github.com/anotherhadi/foam/config"
)

// Resolve turns a wordlist path into a file path: "$name/rest" uses the
// shortcut name, "~/" the home directory, and a relative path is taken
// from base. Without an extension, "x.txt" is used when only it exists.
func Resolve(p string, shortcuts map[string]string, base string) (string, error) {
	if rest, ok := strings.CutPrefix(p, "$"); ok {
		name, tail, _ := strings.Cut(rest, "/")
		dir, ok := shortcuts[name]
		if !ok {
			return "", fmt.Errorf("unknown shortcut $%s (shortcuts in the config)", name)
		}
		p = filepath.Join(fconfig.ExpandHome(dir), tail)
	}
	p = fconfig.ExpandHome(p)
	if !filepath.IsAbs(p) {
		p = filepath.Join(base, p)
	}
	if _, err := os.Stat(p); err != nil && filepath.Ext(p) == "" {
		if _, err := os.Stat(p + ".txt"); err == nil {
			return p + ".txt", nil
		}
	}
	return p, nil
}

type stamp struct {
	size int64
	mod  time.Time
}

var (
	mu    sync.Mutex
	cache = map[string]struct {
		stamp
		n int
	}{}
)

// Count returns the number of lines ffuf reads from the file, cached until
// the file changes.
func Count(path string) (int, error) {
	st, err := os.Stat(path)
	if err != nil {
		return 0, err
	}
	if st.IsDir() {
		return 0, fmt.Errorf("%s is a directory", path)
	}
	s := stamp{st.Size(), st.ModTime()}
	mu.Lock()
	c, ok := cache[path]
	mu.Unlock()
	if ok && c.stamp == s {
		return c.n, nil
	}
	f, err := os.Open(path)
	if err != nil {
		return 0, err
	}
	defer f.Close()
	n, err := countLines(f)
	if err != nil {
		return 0, err
	}
	mu.Lock()
	cache[path] = struct {
		stamp
		n int
	}{s, n}
	mu.Unlock()
	return n, nil
}

func countLines(r io.Reader) (int, error) {
	buf := make([]byte, 256*1024)
	n, last := 0, byte('\n')
	for {
		k, err := r.Read(buf)
		if k > 0 {
			n += bytes.Count(buf[:k], []byte{'\n'})
			last = buf[k-1]
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return 0, err
		}
	}
	if last != '\n' {
		n++
	}
	return n, nil
}

// Browse tells where a file picker should start for the path typed so far in
// a block: the directory its last "/" closes ("~/", "$w/lists/"), or dir when
// nothing is typed. A shortcut or "~" alone is its directory: "$w" is "$w/".
// prefix is the typed part that the pick goes after.
func Browse(typed string, shortcuts map[string]string, base, dir string) (start, prefix string, err error) {
	if _, ok := shortcuts[strings.TrimPrefix(typed, "$")]; ok && strings.HasPrefix(typed, "$") || typed == "~" {
		typed += "/"
	}
	i := strings.LastIndex(typed, "/")
	if i < 0 {
		if dir == "" {
			return base, "", nil
		}
		start, err := Resolve(dir, shortcuts, base)
		return start, strings.TrimSuffix(dir, "/") + "/", err
	}
	prefix = typed[:i+1]
	start, err = Resolve(prefix, shortcuts, base)
	if err != nil {
		return "", "", err
	}
	if st, serr := os.Stat(start); serr != nil || !st.IsDir() {
		return "", "", fmt.Errorf("%s is not a directory", prefix)
	}
	return start, prefix, nil
}

// Join puts a pick after the prefix Browse returned; an absolute pick stands
// alone.
func Join(prefix, picked string) string {
	if filepath.IsAbs(picked) {
		return picked
	}
	return prefix + picked
}
