package wordlist

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCount(t *testing.T) {
	for _, c := range []struct {
		in string
		n  int
	}{{"", 0}, {"a", 1}, {"a\n", 1}, {"a\n\nb", 3}, {"a\nb\n", 2}} {
		if n, _ := countLines(strings.NewReader(c.in)); n != c.n {
			t.Errorf("countLines(%q) = %d, want %d", c.in, n, c.n)
		}
	}
}

func TestResolve(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "common.txt"), []byte("a\nb\n"), 0o644)
	p, err := Resolve("$word/common", map[string]string{"word": dir}, "/nowhere")
	if err != nil || p != filepath.Join(dir, "common.txt") {
		t.Fatalf("got %q, %v", p, err)
	}
	if p, _ := Resolve("lists/x", nil, "/base"); p != "/base/lists/x" {
		t.Fatalf("relative: %q", p)
	}
	if _, err := Resolve("$nope/x", nil, ""); err == nil {
		t.Fatal("unknown shortcut accepted")
	}
	if n, err := Count(filepath.Join(dir, "common.txt")); n != 2 || err != nil {
		t.Fatalf("count = %d, %v", n, err)
	}
}

func TestBrowse(t *testing.T) {
	dir := t.TempDir()
	os.Mkdir(filepath.Join(dir, "lists"), 0o755)
	sc := map[string]string{"w": dir}
	for _, c := range []struct{ typed, pickDir, start, prefix string }{
		{"", "", "/cwd", ""},
		{"", "$w", dir, "$w/"},
		{"$w/", "", dir, "$w/"},
		{"$w/lists/com", "", filepath.Join(dir, "lists"), "$w/lists/"},
		{"comm", "", "/cwd", ""},
		{"$w", "/elsewhere", dir, "$w/"},
	} {
		start, prefix, err := Browse(c.typed, sc, "/cwd", c.pickDir)
		if err != nil || start != c.start || prefix != c.prefix {
			t.Errorf("Browse(%q, %q) = %q, %q, %v", c.typed, c.pickDir, start, prefix, err)
		}
	}
	if _, _, err := Browse("$w/nope/", sc, "/cwd", ""); err == nil {
		t.Error("missing directory accepted")
	}
	if Join("$w/", "a.txt") != "$w/a.txt" || Join("$w/", "/abs") != "/abs" {
		t.Error("Join")
	}
}
