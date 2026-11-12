// Package collection stores requests on disk: one .http file per request
// under requests/, in folders, and what their runs left under runs/<id>/.
package collection

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"

	"github.com/anotherhadi/steep/internal/request"
)

const ext = ".http"

type Collection struct {
	Root string
}

func Open(root string) (*Collection, error) {
	c := &Collection{Root: root}
	for _, d := range []string{c.requestsDir(), c.runsDir()} {
		err := os.MkdirAll(d, 0o755)
		if d == c.runsDir() && err == nil {
			// the dumps hold cookies and tokens; runs made before 0o700 too
			err = os.Chmod(d, 0o700)
		}
		if err != nil {
			// the path of a PathError is under root, which is already said
			if pe := (*fs.PathError)(nil); errors.As(err, &pe) {
				err = pe.Err
			}
			return nil, fmt.Errorf("collection %s: %w", root, err)
		}
	}
	return c, nil
}

// OpenExisting opens a collection whose folder must already exist: a typo in
// its path is an error, not a new empty collection.
func OpenExisting(root string) (*Collection, error) {
	fi, err := os.Stat(root)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return nil, fmt.Errorf("collection %s does not exist (createCollection is false)", root)
	case err != nil:
		return nil, fmt.Errorf("collection %s: %w", root, err)
	case !fi.IsDir():
		return nil, fmt.Errorf("collection %s is not a folder", root)
	}
	return Open(root)
}

func (c *Collection) requestsDir() string { return filepath.Join(c.Root, "requests") }
func (c *Collection) runsDir() string     { return filepath.Join(c.Root, "runs") }

func (c *Collection) RunDir(id string) string { return filepath.Join(c.runsDir(), id) }

// Entry is a request or a folder; Name is its slash-separated path, without
// the .http of a request.
type Entry struct {
	Name   string
	Dir    bool
	Method string
	// Color is the PwnFox color of a request, "" without one.
	Color string
}

// Entries lists every folder and request, folders first at each level, then
// by name.
func (c *Collection) Entries() ([]Entry, error) {
	var out []Entry
	root := c.requestsDir()
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, p)
		if rel == "." || strings.HasPrefix(d.Name(), ".") {
			if d.IsDir() && rel != "." {
				return filepath.SkipDir
			}
			return nil
		}
		rel = filepath.ToSlash(rel)
		switch {
		case d.IsDir():
			out = append(out, Entry{Name: rel, Dir: true})
		case strings.HasSuffix(rel, ext):
			e := Entry{Name: strings.TrimSuffix(rel, ext)}
			if b, err := readHead(p); err == nil {
				e.Method = request.Method(b)
				e.Color = request.PwnFoxColor(b)
			}
			out = append(out, e)
		}
		return nil
	})
	slices.SortFunc(out, compare)
	return out, err
}

// compare orders entries as a tree: by path segment, folders before requests.
func compare(a, b Entry) int {
	as, bs := strings.Split(a.Name, "/"), strings.Split(b.Name, "/")
	for i := 0; i < len(as) && i < len(bs); i++ {
		if as[i] == bs[i] {
			continue
		}
		aDir := i < len(as)-1 || a.Dir
		bDir := i < len(bs)-1 || b.Dir
		if aDir != bDir {
			if aDir {
				return -1
			}
			return 1
		}
		if c := strings.Compare(strings.ToLower(as[i]), strings.ToLower(bs[i])); c != 0 {
			return c
		}
		return strings.Compare(as[i], bs[i])
	}
	if len(as) == len(bs) && a.Dir != b.Dir {
		// a folder and a request of the same name: a/ and a.http
		if a.Dir {
			return -1
		}
		return 1
	}
	return len(as) - len(bs)
}

func readHead(p string) (string, error) {
	f, err := os.Open(p)
	if err != nil {
		return "", err
	}
	defer f.Close()
	b := make([]byte, 4096)
	n, _ := f.Read(b)
	return normalize(string(b[:n])), nil
}

// ValidName checks a request or folder name: slash-separated, no empty,
// hidden or relative segment.
func ValidName(name string) error {
	if strings.TrimSpace(name) == "" {
		return errors.New("a name is needed")
	}
	for _, seg := range strings.Split(name, "/") {
		switch {
		case seg == "":
			return errors.New("empty folder name")
		case seg == "." || seg == "..":
			return fmt.Errorf("%q is not a name", seg)
		case strings.HasPrefix(seg, "."):
			return fmt.Errorf("%q: a name cannot start with a dot", seg)
		case strings.ContainsAny(seg, "\\\x00"):
			return fmt.Errorf("%q: invalid character", seg)
		}
	}
	return nil
}

func (c *Collection) file(name string) string {
	return filepath.Join(c.requestsDir(), filepath.FromSlash(name)+ext)
}

func (c *Collection) dir(name string) string {
	return filepath.Join(c.requestsDir(), filepath.FromSlash(name))
}

func (c *Collection) Exists(name string) bool {
	_, err1 := os.Stat(c.file(name))
	_, err2 := os.Stat(c.dir(name))
	return err1 == nil || err2 == nil
}

// Read returns a request's text as steep edits it: "\n" line ends, no byte
// order mark, whatever wrote the file.
func (c *Collection) Read(name string) (string, error) {
	b, err := os.ReadFile(c.file(name))
	return normalize(string(b)), err
}

func normalize(s string) string {
	s = strings.TrimPrefix(s, "\ufeff")
	return strings.ReplaceAll(s, "\r\n", "\n")
}

// Write saves a request, atomically.
func (c *Collection) Write(name, text string) error {
	p := c.file(name)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(p), ".steep-*")
	if err != nil {
		return err
	}
	if _, err := tmp.WriteString(text); err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmp.Name())
		return err
	}
	return os.Rename(tmp.Name(), p)
}

func (c *Collection) Create(name, text string) error {
	if err := ValidName(name); err != nil {
		return err
	}
	if c.Exists(name) {
		return fmt.Errorf("%s already exists", name)
	}
	return c.Write(name, text)
}

func (c *Collection) Mkdir(name string) error {
	if err := ValidName(name); err != nil {
		return err
	}
	if c.Exists(name) {
		return fmt.Errorf("%s already exists", name)
	}
	return os.MkdirAll(c.dir(name), 0o755)
}

func (c *Collection) Rename(old string, dir bool, name string) error {
	if err := ValidName(name); err != nil {
		return err
	}
	if c.Exists(name) {
		return fmt.Errorf("%s already exists", name)
	}
	from, to := c.file(old), c.file(name)
	if dir {
		from, to = c.dir(old), c.dir(name)
		if strings.HasPrefix(name+"/", old+"/") {
			return errors.New("a folder cannot move into itself")
		}
	}
	if err := os.MkdirAll(filepath.Dir(to), 0o755); err != nil {
		return err
	}
	return os.Rename(from, to)
}

// Requests lists the requests under a folder ("" is the whole collection),
// or the request itself.
func (c *Collection) Requests(name string, dir bool) []string {
	if !dir {
		return []string{name}
	}
	var out []string
	entries, _ := c.Entries()
	for _, e := range entries {
		if !e.Dir && (name == "" || strings.HasPrefix(e.Name, name+"/")) {
			out = append(out, e.Name)
		}
	}
	return out
}

// IDTaken reports whether a request other than name carries id: a copy
// made outside steep keeps the id of its original.
func (c *Collection) IDTaken(id, name string) bool {
	entries, _ := c.Entries()
	for _, e := range entries {
		if e.Dir || e.Name == name {
			continue
		}
		if text, err := c.Read(e.Name); err == nil && request.Parse(text).ID() == id {
			return true
		}
	}
	return false
}

// Delete removes a request or a folder, and the runs of what it held, unless
// a request left elsewhere still refers to them.
func (c *Collection) Delete(name string, dir bool) error {
	gone := c.Requests(name, dir)
	for _, r := range gone {
		text, err := c.Read(r)
		if err != nil {
			continue
		}
		id := request.Parse(text).ID()
		if id == "" {
			continue
		}
		shared := false
		for _, other := range c.Requests("", true) {
			if !slices.Contains(gone, other) {
				if t, err := c.Read(other); err == nil && request.Parse(t).ID() == id {
					shared = true
					break
				}
			}
		}
		if !shared {
			os.RemoveAll(c.RunDir(id))
		}
	}
	if dir {
		return os.RemoveAll(c.dir(name))
	}
	return os.Remove(c.file(name))
}

func (c *Collection) PruneRuns() error {
	live := map[string]bool{}
	entries, err := c.Entries()
	if err != nil {
		return err
	}
	for _, e := range entries {
		if e.Dir {
			continue
		}
		if text, err := c.Read(e.Name); err == nil {
			live[request.Parse(text).ID()] = true
		}
	}
	runs, err := os.ReadDir(c.runsDir())
	if err != nil {
		return err
	}
	for _, r := range runs {
		if r.IsDir() && !live[r.Name()] {
			os.RemoveAll(filepath.Join(c.runsDir(), r.Name()))
		}
	}
	return nil
}

func (c *Collection) ClearRuns() error {
	if err := os.RemoveAll(c.runsDir()); err != nil {
		return err
	}
	return os.MkdirAll(c.runsDir(), 0o700)
}

// Parent is the folder holding name, "" at the top.
func Parent(name string) string {
	if d := path.Dir(name); d != "." {
		return d
	}
	return ""
}
