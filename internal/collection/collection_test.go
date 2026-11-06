package collection

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func names(es []Entry) []string {
	out := make([]string, len(es))
	for i, e := range es {
		out[i] = e.Name
		if e.Dir {
			out[i] += "/"
		}
	}
	return out
}

func TestTree(t *testing.T) {
	c, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for _, n := range []string{"zeta", "api/login", "api/v2/token", "Alpha"} {
		if err := c.Create(n, "GET / HTTP/1.1\nHost: x\n\n"); err != nil {
			t.Fatal(err)
		}
	}
	if err := c.Create("api/login", ""); err == nil {
		t.Fatal("duplicate accepted")
	}
	es, err := c.Entries()
	if err != nil {
		t.Fatal(err)
	}
	got := names(es)
	want := []string{"api/", "api/v2/", "api/v2/token", "api/login", "Alpha", "zeta"}
	if len(got) != len(want) {
		t.Fatalf("got %v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v, want %v", got, want)
		}
	}
	if es[2].Method != "GET" {
		t.Fatalf("method = %q", es[2].Method)
	}
}

func TestOpenExisting(t *testing.T) {
	dir := t.TempDir()
	if _, err := OpenExisting(filepath.Join(dir, "typo")); err == nil {
		t.Fatal("a missing collection opened")
	}
	if _, err := os.Stat(filepath.Join(dir, "typo")); err == nil {
		t.Fatal("the missing collection was created")
	}
	file := filepath.Join(dir, "file")
	os.WriteFile(file, nil, 0o644)
	if _, err := OpenExisting(file); err == nil {
		t.Fatal("a file opened as a collection")
	}
	if _, err := OpenExisting(dir); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "requests")); err != nil {
		t.Fatal("requests/ not created in an existing folder")
	}
}

func TestEntryColor(t *testing.T) {
	c, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	c.Create("admin", "GET / HTTP/1.1\nHost: x\nX-PwnFox-Color: red\n\n")
	c.Create("anon", "GET / HTTP/1.1\nHost: x\n\n")
	es, err := c.Entries()
	if err != nil {
		t.Fatal(err)
	}
	if es[0].Color != "red" || es[1].Color != "" {
		t.Fatalf("colors = %q, %q", es[0].Color, es[1].Color)
	}
}

func TestDeleteTakesRuns(t *testing.T) {
	c, _ := Open(t.TempDir())
	c.Create("a/b", "# steep id: 42\nGET / HTTP/1.1\n\n")
	os.MkdirAll(c.RunDir("42"), 0o755)
	os.MkdirAll(c.RunDir("orphan"), 0o755)
	if err := c.PruneRuns(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(c.RunDir("orphan")); !os.IsNotExist(err) {
		t.Fatal("orphan run kept")
	}
	if n := len(c.Requests("a", true)); n != 1 {
		t.Fatalf("requests under a = %d", n)
	}
	if err := c.Delete("a", true); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(c.RunDir("42")); !os.IsNotExist(err) {
		t.Fatal("run of a deleted request kept")
	}
}

func TestRename(t *testing.T) {
	c, _ := Open(t.TempDir())
	c.Create("a/b", "x")
	if err := c.Rename("a", true, "a/c"); err == nil {
		t.Fatal("folder moved into itself")
	}
	if err := c.Rename("a/b", false, "d/e"); err != nil {
		t.Fatal(err)
	}
	if s, err := c.Read("d/e"); err != nil || s != "x" {
		t.Fatalf("read = %q, %v", s, err)
	}
}

func TestValidName(t *testing.T) {
	for _, n := range []string{"", "a//b", "../x", ".hidden", "a/./b"} {
		if ValidName(n) == nil {
			t.Errorf("%q accepted", n)
		}
	}
}

func TestWatch(t *testing.T) {
	c, _ := Open(t.TempDir())
	ch, stop, err := c.Watch(20 * time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	defer stop()
	c.Create("new/one", "x")
	select {
	case <-ch:
	case <-time.After(2 * time.Second):
		t.Fatal("no change reported")
	}
}

func TestForeignFiles(t *testing.T) {
	c, _ := Open(t.TempDir())
	os.WriteFile(c.file("win"), []byte("\ufeffPOST /a HTTP/1.1\r\nHost: h\r\n\r\nbody"), 0o644)
	text, err := c.Read("win")
	if err != nil || text != "POST /a HTTP/1.1\nHost: h\n\nbody" {
		t.Fatalf("read %q, %v", text, err)
	}
	es, _ := c.Entries()
	if es[0].Method != "POST" {
		t.Fatalf("method = %q", es[0].Method)
	}
}

func TestCopiedID(t *testing.T) {
	c, _ := Open(t.TempDir())
	c.Create("a", "# steep id: same\nGET / HTTP/1.1\n\n")
	c.Create("b", "# steep id: same\nGET / HTTP/1.1\n\n")
	os.MkdirAll(c.RunDir("same"), 0o755)
	if !c.IDTaken("same", "a") || c.IDTaken("other", "a") {
		t.Fatal("IDTaken")
	}
	c.Delete("a", false)
	if _, err := os.Stat(c.RunDir("same")); err != nil {
		t.Fatal("the run of b went with a")
	}
	c.Delete("b", false)
	if _, err := os.Stat(c.RunDir("same")); !os.IsNotExist(err) {
		t.Fatal("the run outlived its last request")
	}
}

func TestCompareSameName(t *testing.T) {
	dir, req := Entry{Name: "a", Dir: true}, Entry{Name: "a"}
	if compare(dir, req) >= 0 || compare(req, dir) <= 0 {
		t.Error("a folder goes before a request of its name")
	}
	if compare(Entry{Name: "A"}, Entry{Name: "a"}) == 0 {
		t.Error("names differing in case compare equal")
	}
}

func TestRunsPrivate(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "runs"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(root); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(filepath.Join(root, "runs"))
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o700 {
		t.Fatalf("runs/ is %o", perm)
	}
}
